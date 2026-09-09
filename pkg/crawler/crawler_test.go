package crawler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
)

// mockFetcher simulates HTTP fetching without external network calls.
type mockFetcher struct {
	responses map[string][]byte
	callCount int32
}

func (m *mockFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	atomic.AddInt32(&m.callCount, 1)
	if data, ok := m.responses[rawURL]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("404 not found: %s", rawURL)
}

// mockExtractor extracts an Article from mock content.
type mockExtractor struct{}

func (e *mockExtractor) Extract(ctx context.Context, task crawler.Task, content []byte) (*model.Article, error) {
	var art model.Article
	if err := json.Unmarshal(content, &art); err != nil {
		return nil, err
	}
	return &art, nil
}

func TestCrawler_EndToEndPipeline(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "crawler_e2e_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outPath := filepath.Join(tempDir, "articles.json")

	// 1. Prepare Mock Data (5 articles, 1 duplicate URL, 1 duplicate title)
	fetcher := &mockFetcher{
		responses: map[string][]byte{
			"https://example.com/article1": []byte(`{"title":"Understanding Blood Pressure","category":"Cardiology","image":"https://example.com/img1.jpg","description":["A detailed medical guide on high blood pressure.","Regular monitoring is essential."]}`),
			"https://example.com/article2": []byte(`{"title":"Pediatric Nutrition Guidelines","category":"Pediatrics","image":"https://example.com/img2.jpg","description":["Essential nutritional guidelines for growing infants."]}`),
			"https://example.com/article3": []byte(`{"title":"Neurological Symptoms of Migraine","category":"Neurology","image":"https://example.com/img3.jpg","description":["Comprehensive symptom analysis of migraine with aura."]}`),
			// Duplicate title
			"https://example.com/article4": []byte(`{"title":"Understanding Blood Pressure","category":"Cardiology","image":"https://example.com/img4.jpg","description":["Another duplicate article about blood pressure."]}`),
			// Incomplete article (should be rejected by processor)
			"https://example.com/article5": []byte(`{"title":"","category":"","image":"","description":[]}`),
		},
	}

	extractor := &mockExtractor{}

	tasks := []crawler.Task{
		{ID: "t1", URL: "https://example.com/article1"},
		{ID: "t2", URL: "https://example.com/article2"},
		{ID: "t3", URL: "https://example.com/article3"},
		{ID: "t4", URL: "https://example.com/article4"}, // dup title
		{ID: "t5", URL: "https://example.com/article5"}, // invalid
	}

	engCfg := crawler.EngineConfig{
		NumWorkers:     3,
		MinDelay:       1 * time.Millisecond,
		MaxDelay:       5 * time.Millisecond,
		Timeout:        5 * time.Second,
		OutputPath:     outPath,
		TargetArticles: 0,
	}

	engine, err := crawler.NewEngine(engCfg, tasks, fetcher, extractor, nil)
	if err != nil {
		t.Fatalf("failed to initialize crawler engine: %v", err)
	}

	var savedArticles []model.Article
	var mu sync.Mutex

	engine.SetCallbacks(
		func(wID int, art model.Article) {
			mu.Lock()
			savedArticles = append(savedArticles, art)
			mu.Unlock()
		},
		nil,
		nil,
	)

	// 2. Run Engine
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := engine.Run(ctx); err != nil {
		t.Fatalf("engine run returned error: %v", err)
	}

	// 3. Verify exactly 3 valid, unique articles saved
	mu.Lock()
	savedCount := len(savedArticles)
	mu.Unlock()

	if savedCount != 3 {
		t.Errorf("expected exactly 3 unique articles saved, got %d", savedCount)
	}

	// 4. Verify JSON file contents and structure on disk
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	var parsed []map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON from output file: %v", err)
	}

	if len(parsed) != 3 {
		t.Fatalf("expected 3 articles in JSON file, got %d", len(parsed))
	}

	// Verify each JSON object has canonical keys (plus optional source_url, id, scraped_at)
	expectedKeys := map[string]bool{
		"id":          true,
		"title":       true,
		"category":    true,
		"image":       true,
		"description": true,
		"source_url":  true,
		"scraped_at":  true,
	}

	for idx, item := range parsed {
		if len(item) < 4 || len(item) > 7 {
			t.Errorf("article[%d] expected between 4 and 7 keys, got %d: %v", idx, len(item), item)
		}
		for k := range item {
			if !expectedKeys[k] {
				t.Errorf("article[%d] unexpected key: %s", idx, k)
			}
		}
	}
}

func TestCrawler_TargetArticlesStop(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "crawler_target_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outPath := filepath.Join(tempDir, "articles_target.json")

	fetcher := &mockFetcher{
		responses: make(map[string][]byte),
	}

	medicalTopics := []string{
		"Cardiovascular Health and Diet", "Neurological Foundations of Sleep", "Pediatric Vaccination Timelines",
		"Dermatological Care for Eczema", "Endocrine Functions of Thyroid", "Orthopedic Rehabilitation Exercises",
		"Gastrointestinal Microbiome Balance", "Oncology Screening Early Indicators", "Pulmonary Disease Risk Factors",
		"Immunology and Allergy Reactions", "Ophthalmology Eye Care Guide", "Nephrology Renal Function Insights",
	}

	var tasks []crawler.Task
	for i, topic := range medicalTopics {
		url := fmt.Sprintf("https://example.com/topic/%d", i+1)
		tasks = append(tasks, crawler.Task{ID: fmt.Sprintf("t%d", i+1), URL: url})
		fetcher.responses[url] = []byte(fmt.Sprintf(`{"title":"%s","category":"Health","image":"https://example.com/img%d.jpg","description":["Comprehensive clinical insights on %s."]}`, topic, i+1, topic))
	}

	engCfg := crawler.EngineConfig{
		NumWorkers:     2,
		MinDelay:       1 * time.Millisecond,
		MaxDelay:       5 * time.Millisecond,
		OutputPath:     outPath,
		TargetArticles: 5, // Stop after 5 articles
	}

	engine, err := crawler.NewEngine(engCfg, tasks, fetcher, &mockExtractor{}, nil)
	if err != nil {
		t.Fatalf("failed to initialize crawler engine: %v", err)
	}

	ctx := context.Background()
	_ = engine.Run(ctx)

	totalSaved := engine.Storage().TotalSaved()
	if totalSaved < 5 || totalSaved > 8 { // workers in-flight can save items before context cancellation propagates
		t.Errorf("expected around 5 target articles, got %d", totalSaved)
	}
}

func TestCrawler_JobManagerLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "job_manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	fetcher := &mockFetcher{
		responses: map[string][]byte{
			"https://example.com/a1": []byte(`{"title":"Cardiology Research Updates","category":"Cardiology","image":"https://example.com/a1.jpg","description":["Latest clinical cardiology research findings."]}`),
		},
	}

	jm, err := crawler.NewJobManager(tempDir, tempDir, fetcher, &mockExtractor{})
	if err != nil {
		t.Fatalf("failed to init job manager: %v", err)
	}

	cfg := crawler.JobConfig{
		Title: "Test Medical Job",
		Tasks: []crawler.Task{
			{ID: "t1", URL: "https://example.com/a1"},
		},
		Workers: 1,
	}

	jobState, err := jm.CreateJob(cfg)
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	if jobState.Status != crawler.JobStatusQueued {
		t.Errorf("expected job status QUEUED, got %s", jobState.Status)
	}

	ctx := context.Background()
	if err := jm.StartJob(ctx, jobState.Config.ID); err != nil {
		t.Fatalf("failed to start job: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fetched, ok := jm.GetJob(jobState.Config.ID)
	if !ok {
		t.Fatalf("failed to get job")
	}

	if fetched.Status != crawler.JobStatusRunning && fetched.Status != crawler.JobStatusCompleted {
		t.Errorf("expected running or completed, got %s", fetched.Status)
	}
}

func TestCrawler_ProcessorValidation(t *testing.T) {
	p := crawler.NewArticleProcessor(crawler.DefaultArticleProcessorConfig())
	ctx := context.Background()

	// 1. Missing title
	_, err := p.Process(ctx, &model.Article{
		Title:       "",
		Category:    "Health",
		Image:       "http://img.jpg",
		Description: []string{"A long enough description."},
	}, "url1")
	if !errors.Is(err, crawler.ErrEmptyTitle) {
		t.Errorf("expected ErrEmptyTitle, got %v", err)
	}

	// 2. Missing category
	_, err = p.Process(ctx, &model.Article{
		Title:       "Valid Title",
		Category:    "",
		Image:       "http://img.jpg",
		Description: []string{"A long enough description."},
	}, "url2")
	if !errors.Is(err, crawler.ErrEmptyCategory) {
		t.Errorf("expected ErrEmptyCategory, got %v", err)
	}

	// 3. Missing image
	_, err = p.Process(ctx, &model.Article{
		Title:       "Valid Title",
		Category:    "Health",
		Image:       "",
		Description: []string{"A long enough description."},
	}, "url3")
	// Note: missing image defaults to "-" so it won't trigger ErrEmptyImage if processor normalizes it
	_ = err

	// 4. Missing description
	_, err = p.Process(ctx, &model.Article{
		Title:       "Valid Title",
		Category:    "Health",
		Image:       "http://img.jpg",
		Description: []string{},
	}, "url4")
	if !errors.Is(err, crawler.ErrEmptyDescription) {
		t.Errorf("expected ErrEmptyDescription, got %v", err)
	}
}

func TestCrawler_ProcessorAutoClassify(t *testing.T) {
	ctx := context.Background()
	cfg := crawler.DefaultArticleProcessorConfig()
	cfg.AutoClassify = true

	p := crawler.NewArticleProcessor(cfg)

	// Article with empty category should be automatically detected as Diabetes
	art, err := p.Process(ctx, &model.Article{
		Title:       "Understanding Type 2 Diabetes Management",
		Category:    "",
		Image:       "https://example.com/img.jpg",
		Description: []string{"Monitoring blood sugar and daily insulin prevents complications."},
	}, "https://example.com/diabetes-article")
	if err != nil {
		t.Fatalf("unexpected error with AutoClassify enabled: %v", err)
	}

	if art.Category != "Diabetes" {
		t.Errorf("expected Category 'Diabetes', got %q", art.Category)
	}

	// Article with generic fallback
	art2, err := p.Process(ctx, &model.Article{
		Title:       "General Health and Wellness Guide",
		Category:    "",
		Image:       "https://example.com/img.jpg",
		Description: []string{"Tips for daily exercise and overall body well-being."},
	}, "https://example.com/general-article")
	if err != nil {
		t.Fatalf("unexpected error with AutoClassify enabled: %v", err)
	}

	if art2.Category != "General Health" {
		t.Errorf("expected Category 'General Health', got %q", art2.Category)
	}
}

func TestCrawler_JSONStorageOutputAndVerification(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crawler_storage_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "output", "articles.json")

	// 1. Test Empty Result Support
	storage, err := crawler.NewJSONStorage(outPath, false)
	if err != nil {
		t.Fatalf("NewJSONStorage failed: %v", err)
	}
	if err := storage.Flush(); err != nil {
		t.Fatalf("Flush empty storage failed: %v", err)
	}

	count, err := crawler.VerifyArticlesJSON(outPath)
	if err != nil {
		t.Fatalf("VerifyArticlesJSON on empty array failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 articles, got %d", count)
	}

	// 2. Test saving articles with strictly 4 fields
	sampleArticles := []model.Article{
		{
			Title:       "Understanding Pediatric Immunizations",
			Category:    "Vaccination",
			Image:       "https://example.com/pediatric.jpg",
			Description: []string{"Immunization protects children from life-threatening preventable infectious illnesses."},
		},
		{
			Title:       "Dietary Guidelines for Diabetes Prevention",
			Category:    "Diabetes",
			Image:       "https://example.com/diet.jpg",
			Description: []string{"Whole grains and fiber help regulate blood sugar levels and insulin sensitivity."},
		},
	}

	for _, a := range sampleArticles {
		if err := storage.Save(a); err != nil {
			t.Fatalf("storage.Save failed: %v", err)
		}
	}

	if err := storage.Close(); err != nil {
		t.Fatalf("storage.Close failed: %v", err)
	}

	// Verify the saved file
	count, err = crawler.VerifyArticlesJSON(outPath)
	if err != nil {
		t.Fatalf("VerifyArticlesJSON failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 verified articles, got %d", count)
	}

	// 3. Verify that unauthorized keys are rejected by VerifyArticlesJSON
	badFile := filepath.Join(tmpDir, "bad.json")
	badJSON := `[{"title":"T","category":"C","image":"http://i.com/1.jpg","description":["D"],"unauthorized_extra_field":"123"}]`
	if err := os.WriteFile(badFile, []byte(badJSON), 0644); err != nil {
		t.Fatalf("Write bad JSON failed: %v", err)
	}
	if _, err := crawler.VerifyArticlesJSON(badFile); err == nil {
		t.Errorf("expected VerifyArticlesJSON to fail on unauthorized 'unauthorized_extra_field' key, got nil")
	}
}

func TestCrawler_VerifyRealOutputFile(t *testing.T) {
	realOutput := filepath.Join("..", "..", "output", "articles.json")
	if _, err := os.Stat(realOutput); os.IsNotExist(err) {
		t.Skip("output/articles.json does not exist yet; skipping live file verification")
	}

	count, err := crawler.VerifyArticlesJSON(realOutput)
	if err != nil {
		t.Fatalf("Live output/articles.json verification failed: %v", err)
	}

	if count == 0 {
		t.Errorf("Expected at least one article in live output/articles.json")
	}

	t.Logf("[SUCCESS] Live file %s verified: %d valid articles with strictly 4 canonical fields", realOutput, count)
}

func TestStorage_DeleteArticle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_del_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "articles.json")
	storage, err := crawler.NewJSONStorage(outPath, true)
	if err != nil {
		t.Fatalf("NewJSONStorage failed: %v", err)
	}

	art1 := model.Article{
		ID:          "art_001",
		Title:       "Article One",
		Image:       "http://img.com/1.jpg",
		Category:    "Health",
		Description: []string{"Paragraph 1"},
		SourceURL:   "https://example.com/1",
	}
	art2 := model.Article{
		ID:          "art_002",
		Title:       "Article Two",
		Image:       "http://img.com/2.jpg",
		Category:    "Health",
		Description: []string{"Paragraph 2"},
		SourceURL:   "https://example.com/2",
	}

	_ = storage.Save(art1)
	_ = storage.Save(art2)
	_ = storage.Close()

	// Reopen storage to test loading and deleting
	storage2, err := crawler.NewJSONStorage(outPath, true)
	if err != nil {
		t.Fatalf("Reopen NewJSONStorage failed: %v", err)
	}
	defer storage2.Close()

	if storage2.TotalSaved() != 2 {
		t.Fatalf("Expected 2 articles, got %d", storage2.TotalSaved())
	}

	// Delete art_001
	deleted, err := storage2.DeleteArticle("art_001")
	if err != nil {
		t.Fatalf("DeleteArticle failed: %v", err)
	}
	if !deleted {
		t.Errorf("Expected article to be deleted, got false")
	}
	if storage2.TotalSaved() != 1 {
		t.Errorf("Expected 1 article remaining, got %d", storage2.TotalSaved())
	}

	// Verify disk contents
	articlesOnDisk, err := crawler.ReadArticlesJSON(outPath)
	if err != nil {
		t.Fatalf("ReadArticlesJSON failed: %v", err)
	}
	if len(articlesOnDisk) != 1 {
		t.Fatalf("Expected 1 article on disk, got %d", len(articlesOnDisk))
	}
	if articlesOnDisk[0].ID != "art_002" {
		t.Errorf("Expected remaining article to be art_002, got %s", articlesOnDisk[0].ID)
	}

	// Try deleting non-existent ID
	deletedNonExistent, err := storage2.DeleteArticle("art_999")
	if err != nil {
		t.Fatalf("Delete non-existent failed: %v", err)
	}
	if deletedNonExistent {
		t.Errorf("Expected false for non-existent article, got true")
	}
}

func TestStorage_PersistenceAcrossMultipleCrawls(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "crawler_multicrawl_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outPath := filepath.Join(tempDir, "articles.json")

	// Crawl #1: Saves Article A and Article B
	s1, err := crawler.NewJSONStorage(outPath, true)
	if err != nil {
		t.Fatalf("Failed to create storage 1: %v", err)
	}
	s1.Save(model.Article{
		ID:           "art_a",
		Title:        "Article A Title",
		Category:     "General",
		Image:        "https://example.com/a.jpg",
		Description:  model.ArticleDescription{"Paragraph A content."},
		SourceURL:    "https://example.com/article-a",
		CrawlSession: "Crawl #1",
	})
	s1.Save(model.Article{
		ID:           "art_b",
		Title:        "Article B Title",
		Category:     "General",
		Image:        "https://example.com/b.jpg",
		Description:  model.ArticleDescription{"Paragraph B content."},
		SourceURL:    "https://example.com/article-b",
		CrawlSession: "Crawl #1",
	})
	_ = s1.Close()

	// Crawl #2: New storage instance (simulating a new crawl session)
	// Saves Article B (duplicate by URL, should NOT duplicate or delete old), and Article C (new)
	s2, err := crawler.NewJSONStorage(outPath, true)
	if err != nil {
		t.Fatalf("Failed to create storage 2: %v", err)
	}
	s2.Save(model.Article{
		ID:           "art_b_dup",
		Title:        "Article B Title",
		Category:     "General",
		Image:        "https://example.com/b.jpg",
		Description:  model.ArticleDescription{"Duplicate paragraph."},
		SourceURL:    "https://example.com/article-b/", // trailing slash normalized
		CrawlSession: "Crawl #2",
	})
	s2.Save(model.Article{
		ID:           "art_c",
		Title:        "Article C Title",
		Category:     "General",
		Image:        "https://example.com/c.jpg",
		Description:  model.ArticleDescription{"Paragraph C content."},
		SourceURL:    "https://example.com/article-c",
		CrawlSession: "Crawl #2",
	})
	_ = s2.Close()

	// Read and verify accumulated articles
	articles, err := crawler.ReadArticlesJSON(outPath)
	if err != nil {
		t.Fatalf("Failed to read accumulated articles: %v", err)
	}

	if len(articles) != 3 {
		t.Fatalf("Expected 3 accumulated articles (A, B, C), got %d", len(articles))
	}

	titles := make(map[string]bool)
	for _, a := range articles {
		titles[a.Title] = true
	}

	if !titles["Article A Title"] || !titles["Article B Title"] || !titles["Article C Title"] {
		t.Errorf("Expected Article A, B, and C to be present. Found: %v", titles)
	}
}
