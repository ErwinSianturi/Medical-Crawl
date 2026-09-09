package kemenkes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/source/medlineplus"
)

// Test 1: New source alone
func TestKemenkes_NewSourceAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adapter := NewAdapter(nil)

	topics := []string{"diabetes", "kanker", "imunisasi"}
	for _, topic := range topics {
		articles, err := adapter.Search(ctx, topic)
		if err != nil {
			t.Fatalf("Search failed for topic %q: %v", topic, err)
		}

		if len(articles) == 0 {
			t.Errorf("Expected at least one article for topic %q, got 0", topic)
		}

		for i, art := range articles {
			validateArticleStrict(t, art, "Kemenkes", topic, i)
		}
	}
}

// Test 2: Existing source alone
func TestMedlinePlus_ExistingSourceAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adapter := medlineplus.NewAdapter(nil)

	articles, err := adapter.Search(ctx, "diabetes")
	if err != nil {
		t.Fatalf("MedlinePlus search failed: %v", err)
	}

	if len(articles) == 0 {
		t.Fatalf("Expected articles from MedlinePlus")
	}

	for i, art := range articles {
		validateArticleStrict(t, art, "MedlinePlus", "diabetes", i)
	}
}

// Test 3: Both sources together
func TestBothSources_CombinedCrawl(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	procCfg := crawler.DefaultArticleProcessorConfig()
	procCfg.AutoClassify = true
	proc := crawler.NewArticleProcessor(procCfg)

	kemenkesAdapter := NewAdapter(nil)
	medlineAdapter := medlineplus.NewAdapter(nil)

	// Fetch from Source #1
	medlineArticles, err := medlineAdapter.Search(ctx, "vaccine")
	if err != nil {
		t.Logf("MedlinePlus fetch warning: %v", err)
	}

	// Fetch from Source #2
	kemenkesArticles, err := kemenkesAdapter.Search(ctx, "imunisasi")
	if err != nil {
		t.Fatalf("Kemenkes fetch failed: %v", err)
	}

	combined := append(medlineArticles, kemenkesArticles...)
	if len(combined) == 0 {
		t.Fatalf("Expected articles from combined crawl")
	}

	var savedCount int
	for _, raw := range combined {
		cleaned, err := proc.Process(ctx, &raw, "")
		if err != nil {
			// Duplicate or validation filtered
			continue
		}
		savedCount++
		validateArticleStrict(t, *cleaned, "Combined", cleaned.Category, savedCount)
	}

	if savedCount == 0 {
		t.Errorf("Expected at least 1 saved article from combined crawl")
	}

	t.Logf("[SUCCESS] Combined crawl processed %d unique articles from both sources", savedCount)
}

// Test 4: Duplicate articles across sources
func TestBothSources_CrossSourceDeduplication(t *testing.T) {
	ctx := context.Background()
	procCfg := crawler.DefaultArticleProcessorConfig()
	procCfg.AutoClassify = true
	proc := crawler.NewArticleProcessor(procCfg)

	// Article from Source #1
	art1 := &model.Article{
		Title:       "Type 1 Diabetes Mellitus Overview",
		Category:    "Diabetes",
		Image:       "https://medlineplus.gov/images/diabetes.jpg",
		Description: []string{"Insulin therapy and glucose monitoring are essential for type 1 diabetes management."},
	}

	// Identical or near-identical article from Source #2
	art2 := &model.Article{
		Title:       "Type 1 Diabetes Mellitus Overview",
		Category:    "Diabetes",
		Image:       "https://ayosehat.kemkes.go.id/images/diabetes.jpg",
		Description: []string{"Different description about type 1 diabetes mellitus overview."},
	}

	cleaned1, err1 := proc.Process(ctx, art1, "https://medlineplus.gov/diabetes-1")
	if err1 != nil {
		t.Fatalf("First article should be accepted: %v", err1)
	}
	if cleaned1 == nil {
		t.Fatalf("Expected non-nil cleaned article")
	}

	// Second article with identical title must be detected and rejected as cross-source duplicate
	cleaned2, err2 := proc.Process(ctx, art2, "https://ayosehat.kemkes.go.id/diabetes-1")
	if err2 == nil {
		t.Errorf("Expected cross-source duplicate to be rejected, but it was accepted: %v", cleaned2)
	}
}

// Test 5: Missing image handled safely
func TestKemenkes_MissingImageSafeHandling(t *testing.T) {
	adapter := NewAdapter(nil)

	htmlWithoutImage := `
		<html>
		<head><title>Penyakit Jantung Koroner | Ayo Sehat</title></head>
		<body>
			<h1>Penyakit Jantung Koroner</h1>
			<p>Penyakit jantung koroner terjadi ketika pembuluh darah utama yang menyuplai jantung mengalami penyempitan akibat penumpukan kolesterol.</p>
		</body>
		</html>
	`

	art, err := adapter.ParseArticleHTML(htmlWithoutImage, "https://example.com/jantung", "Penyakit Jantung Koroner")
	if err != nil {
		t.Fatalf("ParseArticleHTML failed: %v", err)
	}

	if art.Image == "" {
		t.Errorf("Expected fallback image, got empty string")
	}
	if art.Image != DefaultImageURL {
		t.Errorf("Expected DefaultImageURL fallback, got %q", art.Image)
	}
}

// Test 6: Missing description handled safely
func TestKemenkes_MissingDescriptionSafeHandling(t *testing.T) {
	adapter := NewAdapter(nil)

	htmlWithoutDesc := `
		<html>
		<head><title>Penyakit Hipertensi</title></head>
		<body>
			<h1>Penyakit Hipertensi</h1>
		</body>
		</html>
	`

	art, err := adapter.ParseArticleHTML(htmlWithoutDesc, "https://example.com/hipertensi", "Penyakit Hipertensi")
	if err != nil {
		t.Fatalf("ParseArticleHTML failed: %v", err)
	}

	if len(art.Description) == 0 {
		t.Errorf("Expected fallback description array, got empty slice")
	}
	if len(art.Description[0]) < 10 {
		t.Errorf("Fallback description too short: %q", art.Description[0])
	}
}

// Test 7: Mock HTTP error handling
func TestKemenkes_HTTPErrorHandling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	adapter := NewAdapter(ts.Client())
	adapter.baseURL = ts.URL

	ctx := context.Background()
	articles, err := adapter.Search(ctx, "diabetes")
	if err != nil {
		t.Fatalf("Search with fallback should not fail fatally: %v", err)
	}

	if len(articles) == 0 {
		t.Errorf("Expected fallback articles when live HTTP returns 404")
	}
}

func validateArticleStrict(t *testing.T, art model.Article, source, topic string, idx int) {
	if strings.TrimSpace(art.Title) == "" {
		t.Errorf("[%s %s #%d] Title is empty", source, topic, idx)
	}
	if strings.TrimSpace(art.Category) == "" {
		t.Errorf("[%s %s #%d] Category is empty", source, topic, idx)
	}
	if strings.TrimSpace(art.Image) == "" {
		t.Errorf("[%s %s #%d] Image is empty", source, topic, idx)
	}
	if len(art.Description) < 1 || len(art.Description) > 3 {
		t.Errorf("[%s %s #%d] Description paragraph count not between 1 and 3: %d", source, topic, idx, len(art.Description))
	}

	// Strict JSON verification: canonical fields + optional id, source_url, scraped_at
	data, err := json.Marshal(art)
	if err != nil {
		t.Errorf("[%s %s #%d] JSON marshal error: %v", source, topic, idx, err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("[%s %s #%d] JSON unmarshal error: %v", source, topic, idx, err)
	}

	if len(m) < 4 || len(m) > 7 {
		t.Errorf("[%s %s #%d] JSON keys count not between 4 and 7: %v", source, topic, idx, m)
	}

	for _, k := range []string{"title", "image", "category", "description"} {
		if _, ok := m[k]; !ok {
			t.Errorf("[%s %s #%d] Missing required key %q in JSON output", source, topic, idx, k)
		}
	}

	if descStr, isStr := m["description"].(string); isStr {
		if strings.TrimSpace(descStr) == "" {
			t.Errorf("[%s %s #%d] JSON description is empty string", source, topic, idx)
		}
	} else if descVal, isSlice := m["description"].([]interface{}); isSlice {
		if len(descVal) < 1 {
			t.Errorf("[%s %s #%d] JSON description array is empty", source, topic, idx)
		}
	} else {
		t.Errorf("[%s %s #%d] JSON description is not string or array: %v", source, topic, idx, m["description"])
	}
}
