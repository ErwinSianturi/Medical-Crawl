package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"maps-scraper/pkg/api"
	"maps-scraper/pkg/model"
)

func TestUI_EndToEndLifecycle(t *testing.T) {
	server := api.NewServer()

	mux := http.NewServeMux()
	// Serve real web directory
	server.RegisterRoutes(mux, "../../web")
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{Timeout: 30 * time.Second}

	// 1. Open application: GET /
	t.Run("Step 1: Open Application UI", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("failed to load root UI: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected HTTP 200 for index.html, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read UI body: %v", err)
		}
		html := string(body)

		// Assert required UI components exist
		requiredStrings := []string{
			"Medical Article Crawler",
			"Crawler Configuration",
			"Trusted Source",
			"Article Limit",
			"Start Crawl",
			"articles.json",
			"Add Source",
			"delete-run-confirm-modal",
		}
		for _, s := range requiredStrings {
			if !strings.Contains(html, s) {
				t.Errorf("UI missing required text/component: %q", s)
			}
		}

		// Assert removed legacy concepts do NOT exist
		prohibitedStrings := []string{
			"Google Maps",
			"Toko Bangunan",
			"Semen Gresik",
			"Pilih Provinsi",
			"Pilih Kota",
			"Pilih Kecamatan",
			"Leaflet",
		}
		for _, s := range prohibitedStrings {
			if strings.Contains(html, s) {
				t.Errorf("UI contains legacy prohibited concept: %q", s)
			}
		}
	})

	// 2. Select Source: GET /api/sources
	t.Run("Step 2: Inspect Approved Sources", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/sources")
		if err != nil {
			t.Fatalf("failed to get sources: %v", err)
		}
		defer resp.Body.Close()

		var sources []struct {
			URL      string `json:"url"`
			Hostname string `json:"hostname"`
			Name     string `json:"name"`
			IsActive bool   `json:"is_active"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
			t.Fatalf("failed to decode sources: %v", err)
		}

		if len(sources) < 2 {
			t.Fatalf("expected at least 2 sources, got %d", len(sources))
		}
	})

	// 3. Enter Topic & Start Crawl: POST /api/crawl
	t.Run("Step 3: Start Crawl and Observe Progress", func(t *testing.T) {
		targetLimit := 4
		payload := map[string]interface{}{
			"topic":  "diabetes",
			"source": "all",
			"limit":  targetLimit,
		}
		payloadBytes, _ := json.Marshal(payload)

		resp, err := client.Post(ts.URL+"/api/crawl", "application/json", bytes.NewBuffer(payloadBytes))
		if err != nil {
			t.Fatalf("failed to start crawl: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
		}

		var initialStatus api.CrawlStatusResponse
		_ = json.NewDecoder(resp.Body).Decode(&initialStatus)

		// Critical check: Never show Completed while still running
		if initialStatus.Status == "Completed" {
			t.Fatalf("CRITICAL: status must never be Completed upon starting crawl")
		}

		// 4. Observe progress and wait for completion
		deadline := time.Now().Add(25 * time.Second)
		var lastStatus api.CrawlStatusResponse
		sawRunning := false

		for time.Now().Before(deadline) {
			stResp, err := client.Get(ts.URL + "/api/crawl/status")
			if err != nil {
				time.Sleep(300 * time.Millisecond)
				continue
			}

			var cur api.CrawlStatusResponse
			_ = json.NewDecoder(stResp.Body).Decode(&cur)
			stResp.Body.Close()
			lastStatus = cur

			if cur.Status == "Running" {
				sawRunning = true
			}

			if cur.Status == "Completed" || cur.Status == "Failed" {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}

		t.Logf("Crawl Lifecycle: SawRunning=%v, FinalStatus=%s, Collected=%d, Error=%s",
			sawRunning, lastStatus.Status, lastStatus.Collected, lastStatus.ErrorMessage)
		for i, a := range lastStatus.Articles {
			t.Logf("Article #%d: %s (cat: %s)", i+1, a.Title, a.Category)
		}

		if lastStatus.Status != "Completed" {
			t.Fatalf("expected crawl to reach Completed, got %s (err: %s)", lastStatus.Status, lastStatus.ErrorMessage)
		}

		if lastStatus.Collected < 2 {
			t.Errorf("expected at least %d articles collected, got %d", 2, lastStatus.Collected)
		}
	})

	// 5. Inspect Articles: GET /api/articles
	t.Run("Step 4: Inspect Articles Schema", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/articles")
		if err != nil {
			t.Fatalf("failed to get articles: %v", err)
		}
		defer resp.Body.Close()

		var rawArticles []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rawArticles); err != nil {
			t.Fatalf("failed to decode raw articles: %v", err)
		}

		if len(rawArticles) == 0 {
			t.Fatalf("expected articles to be returned, got 0")
		}

		expectedKeys := map[string]bool{
			"id":            true,
			"title":         true,
			"image":         true,
			"category":      true,
			"description":   true,
			"source_url":    true,
			"scraped_at":    true,
			"crawl_session": true,
		}

		for i, art := range rawArticles {
			if len(art) < 4 || len(art) > 8 {
				t.Errorf("article #%d has %d fields (expected 4 to 8): %v", i+1, len(art), art)
			}
			for k := range art {
				if !expectedKeys[k] {
					t.Errorf("article #%d contains unapproved key %q", i+1, k)
				}
			}
			for _, k := range []string{"title", "image", "category"} {
				s, ok := art[k].(string)
				if !ok || strings.TrimSpace(s) == "" {
					t.Errorf("article #%d field %q is empty or not string: %v", i+1, k, art[k])
				}
			}

			// Validate description is non-empty string or array of strings
			if descStr, isStr := art["description"].(string); isStr {
				if strings.TrimSpace(descStr) == "" {
					t.Errorf("article #%d description is empty string", i+1)
				}
			} else if descSlice, isSlice := art["description"].([]interface{}); isSlice {
				if len(descSlice) < 1 {
					t.Errorf("article #%d description array is empty", i+1)
				} else {
					for pIdx, p := range descSlice {
						pStr, isStr := p.(string)
						if !isStr || strings.TrimSpace(pStr) == "" {
							t.Errorf("article #%d paragraph #%d is empty or not string: %v", i+1, pIdx+1, p)
						}
					}
				}
			} else {
				t.Errorf("article #%d description is neither string nor array: %v", i+1, art["description"])
			}
		}
	})

	// 6. Download JSON: GET /api/download
	t.Run("Step 5: Download articles.json", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/download")
		if err != nil {
			t.Fatalf("failed to download articles: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected HTTP 200 for download, got %d", resp.StatusCode)
		}

		contentDisposition := resp.Header.Get("Content-Disposition")
		if !strings.Contains(contentDisposition, "articles.json") {
			t.Errorf("expected Content-Disposition with articles.json, got %q", contentDisposition)
		}

		var downloadedArticles []model.Article
		if err := json.NewDecoder(resp.Body).Decode(&downloadedArticles); err != nil {
			t.Fatalf("failed to parse downloaded JSON: %v", err)
		}

		if len(downloadedArticles) == 0 {
			t.Errorf("downloaded articles.json is empty")
		}

		t.Logf("[SUCCESS] Successfully downloaded %d valid articles via articles.json", len(downloadedArticles))
	})

	// 7. Delete 1 Run from UI: DELETE /api/runs/{id}
	t.Run("Step 6: Delete 1 Run from History UI", func(t *testing.T) {
		// Fetch current runs
		resp, err := client.Get(ts.URL + "/api/runs")
		if err != nil {
			t.Fatalf("failed to fetch runs: %v", err)
		}
		defer resp.Body.Close()

		var runs []model.CrawlRun
		if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
			t.Fatalf("failed to decode runs: %v", err)
		}

		if len(runs) == 0 {
			t.Fatalf("expected at least 1 run in history, got 0")
		}

		initialCount := len(runs)
		targetRunID := runs[0].ID

		// Delete the target run
		delReq, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/runs/"+targetRunID, nil)
		if err != nil {
			t.Fatalf("failed to create delete request: %v", err)
		}

		delResp, err := client.Do(delReq)
		if err != nil {
			t.Fatalf("failed to execute delete run: %v", err)
		}
		defer delResp.Body.Close()

		if delResp.StatusCode != http.StatusOK {
			t.Fatalf("expected HTTP 200 OK for delete run, got %d", delResp.StatusCode)
		}

		// Verify run is removed and remaining runs are isolated and preserved
		respAfter, err := client.Get(ts.URL + "/api/runs")
		if err != nil {
			t.Fatalf("failed to fetch runs after deletion: %v", err)
		}
		defer respAfter.Body.Close()

		var runsAfter []model.CrawlRun
		_ = json.NewDecoder(respAfter.Body).Decode(&runsAfter)

		if len(runsAfter) != initialCount-1 {
			t.Errorf("expected %d runs after deletion, got %d", initialCount-1, len(runsAfter))
		}

		for _, r := range runsAfter {
			if r.ID == targetRunID {
				t.Errorf("deleted run %s still exists in history", targetRunID)
			}
		}

		t.Logf("[SUCCESS] Run %s successfully deleted, remaining runs: %d", targetRunID, len(runsAfter))
	})
}

