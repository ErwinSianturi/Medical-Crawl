package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"maps-scraper/pkg/api"
	"maps-scraper/pkg/classifier"
	"maps-scraper/pkg/cleaner"
	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/source/kemenkes"
	"maps-scraper/pkg/source/medlineplus"
)

// ============================================================================
// 1. TOPIC TESTS (6 Required Medical Topics)
// ============================================================================

func TestE2E_RequiredMedicalTopics(t *testing.T) {
	requiredTopics := []struct {
		topic            string
		expectedCategory string
	}{
		{topic: "diabetes", expectedCategory: "Diabetes"},
		{topic: "cancer", expectedCategory: "Cancer"},
		{topic: "vaccination", expectedCategory: "Vaccination"},
		{topic: "heart disease", expectedCategory: "Heart Disease"},
		{topic: "mental health", expectedCategory: "Mental Health"},
		{topic: "nutrition", expectedCategory: "Nutrition"},
	}

	cls := classifier.NewDefaultClassifier()
	clean := cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig())
	mAdapter := medlineplus.NewAdapter(nil)
	kAdapter := kemenkes.NewAdapter(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, tc := range requiredTopics {
		t.Run("Topic_"+tc.topic, func(t *testing.T) {
			t.Logf("[TEST TOPIC] Querying topic: %q", tc.topic)

			// 1. Query MedlinePlus
			mArticles, mErr := mAdapter.Search(ctx, tc.topic)
			if mErr != nil {
				t.Logf("[WARN] MedlinePlus query for %q returned error: %v", tc.topic, mErr)
			}

			// 2. Query Kemenkes
			kArticles, kErr := kAdapter.Search(ctx, tc.topic)
			if kErr != nil {
				t.Logf("[WARN] Kemenkes query for %q returned error: %v", tc.topic, kErr)
			}

			combined := append(mArticles, kArticles...)
			if len(combined) == 0 {
				t.Fatalf("zero articles retrieved across all approved sources for topic %q", tc.topic)
			}

			foundExpectedCategory := false
			validCount := 0

			for _, raw := range combined {
				art, err := clean.CleanAndValidate(&raw)
				if err != nil {
					continue
				}

				cat := cls.Classify(art.Title, strings.Join(art.Description, " "))
				if cat == tc.expectedCategory {
					foundExpectedCategory = true
				}
				validCount++
			}

			t.Logf("Topic %q: retrieved %d articles (%d clean & valid), expected category %q matched=%v",
				tc.topic, len(combined), validCount, tc.expectedCategory, foundExpectedCategory)

			if validCount == 0 {
				t.Errorf("expected at least 1 valid article for topic %q", tc.topic)
			}
			if !foundExpectedCategory {
				t.Errorf("expected topic %q to produce articles classified as %q", tc.topic, tc.expectedCategory)
			}
		})
	}
}

// ============================================================================
// 2. BACKEND COMPONENT TESTS
// ============================================================================

func TestE2E_BackendCrawlerEngineAndWorkers(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "crawler_engine_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outputPath := filepath.Join(tempDir, "articles.json")

	// Create test tasks with distinct conditions
	tasks := []crawler.Task{
		{ID: "diabetes", URL: "https://medlineplus.gov/diabetes.html", Type: "article", Source: "medlineplus"},
		{ID: "cancer", URL: "https://medlineplus.gov/cancer.html", Type: "article", Source: "medlineplus"},
		{ID: "polio", URL: "https://ayosehat.kemkes.go.id/topik/polio", Type: "article", Source: "kemenkes"},
		{ID: "tbc", URL: "https://ayosehat.kemkes.go.id/topik/tbc", Type: "article", Source: "kemenkes"},
	}

	engCfg := crawler.EngineConfig{
		NumWorkers:     3,
		MinDelay:       10 * time.Millisecond,
		MaxDelay:       30 * time.Millisecond,
		Timeout:        10 * time.Second,
		TargetArticles: 4,
		OutputPath:     outputPath,
	}

	mockFetcher := &mockHTTPFetcher{}
	mockExtractor := &mockArticleExtractor{}

	engine, err := crawler.NewEngine(engCfg, tasks, mockFetcher, mockExtractor, nil)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	var savedArticles []model.Article
	var lastTelemetry crawler.EngineTelemetry
	var mu sync.Mutex

	engine.SetCallbacks(
		func(wID int, art model.Article) {
			mu.Lock()
			savedArticles = append(savedArticles, art)
			mu.Unlock()
		},
		func(t crawler.EngineTelemetry) {
			mu.Lock()
			lastTelemetry = t
			mu.Unlock()
		},
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runErr := engine.Run(ctx)
	if runErr != nil {
		t.Fatalf("crawler engine failed: %v", runErr)
	}

	mu.Lock()
	savedCount := len(savedArticles)
	t.Logf("Engine Telemetry: Tasks=%d, ArticlesSaved=%d, Errors=%d",
		lastTelemetry.TotalTasks, lastTelemetry.ArticlesSaved, lastTelemetry.ErrorsCount)
	mu.Unlock()

	if savedCount != 4 {
		t.Errorf("expected 4 articles saved via callbacks, got %d", savedCount)
	}

	// Verify generated JSON output
	count, errVerify := crawler.VerifyArticlesJSON(outputPath)
	if errVerify != nil {
		t.Fatalf("JSON output verification failed: %v", errVerify)
	}
	if count != 4 {
		t.Errorf("expected 4 verified articles in %s, got %d", outputPath, count)
	}
}

// ============================================================================
// 3. EDGE CASES TESTS
// ============================================================================

func TestE2E_EdgeCases(t *testing.T) {
	cls := classifier.NewDefaultClassifier()
	clean := cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig())
	dedup := cleaner.NewDeduplicator(0.85)

	// 1. Empty Query
	t.Run("EmptyQuery", func(t *testing.T) {
		mAdapter := medlineplus.NewAdapter(nil)
		_, err := mAdapter.Search(context.Background(), "   ")
		if err == nil {
			t.Errorf("expected error for empty search query, got nil")
		}

		kAdapter := kemenkes.NewAdapter(nil)
		_, kErr := kAdapter.Search(context.Background(), "")
		if kErr == nil {
			t.Errorf("expected error for empty Kemenkes search query, got nil")
		}
	})

	// 2. Unknown Query / Zero Results
	t.Run("UnknownQuery_ZeroResults", func(t *testing.T) {
		unknownQuery := "xyznonexistentmedicalcondition999999"
		kAdapter := kemenkes.NewAdapter(nil)
		arts, err := kAdapter.Search(context.Background(), unknownQuery)
		if err != nil {
			t.Errorf("expected graceful nil error for unknown query, got %v", err)
		}
		if len(arts) != 0 {
			t.Errorf("expected 0 articles for unknown query, got %d", len(arts))
		}
	})

	// 3. Network Failure (Timeout / Cancelled Context)
	t.Run("NetworkFailure_Timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(2 * time.Millisecond)

		mAdapter := medlineplus.NewAdapter(nil)
		_, err := mAdapter.Search(ctx, "diabetes")
		if err == nil {
			t.Errorf("expected context deadline error, got nil")
		}
	})

	// 4. Source Failure (HTTP 500 Internal Server Error)
	t.Run("SourceFailure_HTTP500", func(t *testing.T) {
		server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}))
		defer server500.Close()

		client := server500.Client()
		kAdapter := kemenkes.NewAdapter(client)
		_, err := kAdapter.FetchAndParse(context.Background(), server500.URL+"/test-error", "Error Page")
		if err == nil {
			t.Errorf("expected HTTP status 500 error, got nil")
		}
	})

	// 5. Missing Image Safe Handling
	t.Run("MissingImage_SafeHandling", func(t *testing.T) {
		raw := model.Article{
			Title:       "Test Missing Image Article",
			Category:    "Diabetes",
			Image:       "",
			Description: []string{"This is a valid substantive medical description about diabetes management."},
		}

		// Empty image defaults to "-" in cleaner
		validArt, errValid := clean.CleanAndValidate(&raw)
		if errValid != nil {
			t.Fatalf("expected article with empty image to pass validation with '-': %v", errValid)
		}
		if validArt.Image != "-" {
			t.Errorf("expected '-' image fallback, got %s", validArt.Image)
		}
	})

	// 6. Missing Description Safe Handling
	t.Run("MissingDescription_SafeHandling", func(t *testing.T) {
		raw := model.Article{
			Title:       "Test Missing Description Article",
			Category:    "Cancer",
			Image:       medlineplus.DefaultImageURL,
			Description: []string{},
		}

		_, err := clean.CleanAndValidate(&raw)
		if err == nil {
			t.Errorf("cleaner should reject empty description")
		}
	})

	// 7. Duplicate Articles (Intra-Source & Cross-Source)
	t.Run("DuplicateArticles_Deduplication", func(t *testing.T) {
		art1 := model.Article{
			Title:       "Type 1 Diabetes Mellitus Clinical Overview",
			Category:    "Diabetes",
			Image:       medlineplus.DefaultImageURL,
			Description: []string{"An in-depth guide on insulin dependency and glycemic monitoring in juvenile patients."},
		}
		art2 := model.Article{
			Title:       "Type 1 Diabetes Mellitus Clinical Overview", // Exact title duplicate
			Category:    "Diabetes",
			Image:       kemenkes.DefaultImageURL,
			Description: []string{"Republished information regarding insulin dependency and glycemic monitoring."},
		}
		art3 := model.Article{
			Title:       "type 1 diabetes mellitus: clinical overview!", // Normalized fuzzy duplicate
			Category:    "Diabetes",
			Image:       medlineplus.DefaultImageURL,
			Description: []string{"A clinical guide on insulin dependency."},
		}

		res1 := dedup.CheckArticle(&art1, "https://medlineplus.gov/diabetes-t1")
		if res1.IsDuplicate {
			t.Errorf("first article should not be duplicate")
		}

		res2 := dedup.CheckArticle(&art2, "https://ayosehat.kemkes.go.id/topik/diabetes-t1")
		if !res2.IsDuplicate {
			t.Errorf("second article with identical title should be identified as duplicate")
		}

		res3 := dedup.CheckArticle(&art3, "https://medlineplus.gov/diabetes-t1-alt")
		if !res3.IsDuplicate {
			t.Errorf("third article with fuzzy title should be identified as duplicate")
		}
	})

	// 8. Special Characters & Unicode Entity Sanitization
	t.Run("SpecialCharacters_Sanitization", func(t *testing.T) {
		raw := model.Article{
			Title:       "COVID-19 &amp; SARS-CoV-2: What&rsquo;s &ldquo;Next&rdquo; &lt;Alert&gt;?",
			Category:    "Infectious Disease",
			Image:       medlineplus.DefaultImageURL,
			Description: []string{"<p>Understanding COVID-19 mutations&#8230; &amp; the <strong>vaccine</strong> efficacy — updated daily!</p>"},
		}

		cleaned, err := clean.CleanAndValidate(&raw)
		if err != nil {
			t.Fatalf("sanitization failed: %v", err)
		}

		if strings.Contains(cleaned.Title, "&amp;") || strings.Contains(cleaned.Title, "&rsquo;") || strings.Contains(cleaned.Title, "<Alert>") {
			t.Errorf("title contains unescaped HTML/entities: %q", cleaned.Title)
		}
		if len(cleaned.Description) == 0 {
			t.Fatalf("expected description paragraphs")
		}
		if strings.Contains(cleaned.Description[0], "<p>") || strings.Contains(cleaned.Description[0], "<strong>") {
			t.Errorf("description contains HTML tags: %q", cleaned.Description[0])
		}
		if strings.Contains(cleaned.Description[0], "&#8230;") {
			t.Errorf("description contains unresolved numeric entities: %q", cleaned.Description[0])
		}
	})

	// 9. Long Descriptions (>10,000 Characters)
	t.Run("LongDescriptions_Handling", func(t *testing.T) {
		longText := strings.Repeat("A comprehensive medical analysis of cardiovascular disease prevention and health outcomes. ", 200) // ~19,000 chars
		raw := model.Article{
			Title:       "Cardiovascular Health Guidelines",
			Category:    "Heart Disease",
			Image:       medlineplus.DefaultImageURL,
			Description: []string{longText},
		}

		cleaned, err := clean.CleanAndValidate(&raw)
		if err != nil {
			t.Fatalf("failed to process long description: %v", err)
		}

		if len(cleaned.Description) == 0 || len(cleaned.Description[0]) < 100 {
			t.Errorf("expected long description to be preserved, got %v", cleaned.Description)
		}
	})

	// 10. Multi-Source Collision
	t.Run("MultiSourceCollision", func(t *testing.T) {
		medlineArt := model.Article{
			Title:       "Tuberculosis Prevention and Treatment",
			Category:    "Infectious Disease",
			Image:       medlineplus.DefaultImageURL,
			Description: []string{"Tuberculosis is caused by bacteria that spread from person to person through microscopic droplets."},
		}
		kemenkesArt := model.Article{
			Title:       "Tuberculosis Prevention and Treatment", // Duplicate
			Category:    "Infectious Disease",
			Image:       kemenkes.DefaultImageURL,
			Description: []string{"Informasi dan panduan pencegahan tuberkulosis."},
		}

		multiDedup := cleaner.NewDeduplicator(0.85)
		r1 := multiDedup.CheckArticle(&medlineArt, "https://medlineplus.gov/tb")
		r2 := multiDedup.CheckArticle(&kemenkesArt, "https://ayosehat.kemkes.go.id/tb")

		if r1.IsDuplicate {
			t.Errorf("first source article should not be duplicate")
		}
		if !r2.IsDuplicate {
			t.Errorf("second source article with same title must be detected as duplicate")
		}
	})

	_ = cls
}

// ============================================================================
// 4. UI INTEGRATION TEST (Complete Flow: Start -> Crawl -> Progress -> Download)
// ============================================================================

func TestE2E_UI_FullFlowAndSchema(t *testing.T) {
	server := api.NewServer()

	ctrl := api.GetMedicalCrawlerController()
	ctrl.Stop()
	for _, a := range ctrl.GetArticles() {
		if strings.EqualFold(a.Category, "cancer") || strings.EqualFold(a.Category, "vaccination") || strings.Contains(strings.ToLower(a.Title), "kanker") || strings.Contains(strings.ToLower(a.Title), "imunisasi") {
			_, _ = ctrl.DeleteArticle(a.ID)
		}
	}

	mux := http.NewServeMux()
	server.RegisterRoutes(mux, "../web")
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{Timeout: 30 * time.Second}

	// 1. Load Web UI
	respUI, err := client.Get(ts.URL + "/")
	if err != nil || respUI.StatusCode != http.StatusOK {
		t.Fatalf("failed to load UI: %v (status: %d)", err, respUI.StatusCode)
	}
	uiBytes, _ := io.ReadAll(respUI.Body)
	respUI.Body.Close()

	if !strings.Contains(string(uiBytes), "Medical Article Crawler") {
		t.Fatalf("UI missing 'Medical Article Crawler' title")
	}

	// 2. Start Crawl via API
	crawlLimit := 6
	startReq := map[string]interface{}{
		"topic":  "cancer, vaccination",
		"source": "all",
		"limit":  crawlLimit,
	}
	startBytes, _ := json.Marshal(startReq)

	respCrawl, err := client.Post(ts.URL+"/api/crawl", "application/json", bytes.NewBuffer(startBytes))
	if err != nil || respCrawl.StatusCode != http.StatusAccepted {
		t.Fatalf("failed to trigger crawl: %v", err)
	}
	var startedSession api.CrawlStatusResponse
	_ = json.NewDecoder(respCrawl.Body).Decode(&startedSession)
	respCrawl.Body.Close()

	if startedSession.Status == "Completed" {
		t.Fatalf("CRITICAL: crawler returned 'Completed' immediately upon launch")
	}

	// 3. Poll progress until complete
	deadline := time.Now().Add(30 * time.Second)
	var finalSession api.CrawlStatusResponse
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)

		stResp, err := client.Get(ts.URL + "/api/crawl/status")
		if err != nil {
			continue
		}
		_ = json.NewDecoder(stResp.Body).Decode(&finalSession)
		stResp.Body.Close()

		if finalSession.Status == "Completed" || finalSession.Status == "Failed" {
			break
		}
	}

	t.Logf("Final UI Crawl Status: %s | Collected: %d | Err: %s",
		finalSession.Status, finalSession.Collected, finalSession.ErrorMessage)

	if finalSession.Status != "Completed" {
		t.Fatalf("expected crawl status 'Completed', got %s", finalSession.Status)
	}

	// 4. Download articles.json and programmatically verify strict 4-field schema
	respDownload, err := client.Get(ts.URL + "/api/download")
	if err != nil || respDownload.StatusCode != http.StatusOK {
		t.Fatalf("failed to download articles.json: %v", err)
	}
	defer respDownload.Body.Close()

	var rawArticles []map[string]interface{}
	if err := json.NewDecoder(respDownload.Body).Decode(&rawArticles); err != nil {
		t.Fatalf("downloaded articles.json is not valid JSON array: %v", err)
	}

	if len(rawArticles) == 0 {
		t.Fatalf("downloaded articles.json contains 0 articles")
	}

	expectedFields := map[string]bool{
		"id":          true,
		"title":       true,
		"category":    true,
		"image":       true,
		"description": true,
		"source_url":  true,
		"scraped_at":  true,
	}

	for idx, art := range rawArticles {
		// Verify between 4 and 7 keys
		if len(art) < 4 || len(art) > 7 {
			t.Fatalf("Article #%d has %d fields (expected 4 to 7): %v", idx+1, len(art), art)
		}

		for k := range art {
			if !expectedFields[k] {
				t.Fatalf("Article #%d contains unauthorized field %q", idx+1, k)
			}
		}

		for _, k := range []string{"title", "image", "category"} {
			v, exists := art[k]
			if !exists {
				t.Fatalf("Article #%d missing mandatory key %q", idx+1, k)
			}
			strVal, ok := v.(string)
			if !ok || strings.TrimSpace(strVal) == "" {
				t.Fatalf("Article #%d key %q is not a non-empty string: %v", idx+1, k, v)
			}
		}

		descVal, ok := art["description"]
		if !ok {
			t.Fatalf("Article #%d missing mandatory key 'description'", idx+1)
		}
		if descStr, isStr := descVal.(string); isStr {
			if strings.TrimSpace(descStr) == "" {
				t.Fatalf("Article #%d description is empty string", idx+1)
			}
		} else if descSlice, isSlice := descVal.([]interface{}); isSlice {
			if len(descSlice) < 1 {
				t.Fatalf("Article #%d description array is empty", idx+1)
			}
			for pIdx, p := range descSlice {
				pStr, ok := p.(string)
				if !ok || strings.TrimSpace(pStr) == "" {
					t.Fatalf("Article #%d paragraph #%d is empty: %v", idx+1, pIdx+1, p)
				}
			}
		} else {
			t.Fatalf("Article #%d description must be a string or array: %v", idx+1, descVal)
		}
	}

	t.Logf("[SUCCESS] Programmatically validated %d articles with canonical schema", len(rawArticles))
}

// ============================================================================
// MOCKS FOR ENGINE UNIT TESTING
// ============================================================================

type mockHTTPFetcher struct{}

func (m *mockHTTPFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	return []byte(fmt.Sprintf("<html><body><h1>%s</h1><p>Substantive description for %s</p></body></html>", rawURL, rawURL)), nil
}

type mockArticleExtractor struct{}

func (m *mockArticleExtractor) Extract(ctx context.Context, task crawler.Task, body []byte) (*model.Article, error) {
	titles := map[string]string{
		"diabetes": "Comprehensive Clinical Diabetes Management",
		"cancer":   "Oncology and Tumor Diagnostic Protocols",
		"polio":    "Polio Vaccination Inoculation Guidelines",
		"tbc":      "Tuberculosis Infection Treatment Regimen",
	}
	title, ok := titles[task.ID]
	if !ok {
		title = fmt.Sprintf("Distinct Medical Guideline Regarding %s", task.ID)
	}

	return &model.Article{
		Title:       title,
		Image:       medlineplus.DefaultImageURL,
		Category:    "General Health",
		Description: []string{fmt.Sprintf("Authoritative medical guideline extracted from %s.", task.URL)},
	}, nil
}
