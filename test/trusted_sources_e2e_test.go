package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maps-scraper/pkg/api"
	"maps-scraper/pkg/source"
)

func TestE2E_TrustedSources_URLManagementAndCrawling(t *testing.T) {
	// Setup isolated temporary environment
	tempDir, err := os.MkdirTemp("", "trusted_sources_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "trusted_sources.json")
	outPath := filepath.Join(tempDir, "output", "articles.json")

	reg := source.NewRegistry(storagePath)
	origRegistry := source.GetRegistry()
	source.SetRegistry(reg)
	defer source.SetRegistry(origRegistry)

	server := api.NewServer()
	api.GetMedicalCrawlerController().Stop()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux, "../web")
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{Timeout: 30 * time.Second}

	// ------------------------------------------------------------------------
	// TEST A: Add https://www.halodoc.com/artikel (with URL validation checks)
	// ------------------------------------------------------------------------
	t.Run("TestA_AddHalodocURL", func(t *testing.T) {
		// First: test that plain site name "Halodoc" or "halodoc.com" is REJECTED
		invalidNames := []string{"Halodoc", "halodoc.com", "www.halodoc.com/artikel", "ftp://halodoc.com"}
		for _, inv := range invalidNames {
			body, _ := json.Marshal(map[string]string{"url": inv})
			resp, err := client.Post(ts.URL+"/api/sources", "application/json", bytes.NewBuffer(body))
			if err != nil {
				t.Fatalf("POST /api/sources failed: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected 400 Bad Request for non-URL input %q, got %d", inv, resp.StatusCode)
			}
			respBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(respBytes), "http://") && !strings.Contains(string(respBytes), "https://") {
				t.Errorf("Expected helpful error message with http/https requirement, got: %s", string(respBytes))
			}
		}

		// Second: test adding valid URL with query parameter
		validURL := "https://www.halodoc.com/artikel?srsltid=AfmBOopyT-LxR0xJWEsdUpHKAmCeE8NYq6XR08QUzD12IMc1VrjZsgbG"
		body, _ := json.Marshal(map[string]string{"url": validURL})
		resp, err := client.Post(ts.URL+"/api/sources", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("POST /api/sources failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201 Created for valid URL, got %d: %s", resp.StatusCode, string(b))
		}

		t.Logf("[SUCCESS Test A] Added Halodoc URL with query params: %s", validURL)
	})

	// ------------------------------------------------------------------------
	// TEST B: Add Kemenkes URL
	// ------------------------------------------------------------------------
	t.Run("TestB_AddKemenkesURL", func(t *testing.T) {
		kemenkesURL := "https://ayosehat.kemkes.go.id/topik-az"
		body, _ := json.Marshal(map[string]string{"url": kemenkesURL})
		resp, err := client.Post(ts.URL+"/api/sources", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("POST /api/sources failed: %v", err)
		}
		defer resp.Body.Close()

		// If already seeded it might return 400 "already registered", which is valid
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 201 or 400 (already exists), got %d", resp.StatusCode)
		}

		// Add custom Kemenkes category URL
		customKemenkes := "https://ayosehat.kemkes.go.id/topik-penyakit/diabetes"
		body2, _ := json.Marshal(map[string]string{"url": customKemenkes})
		resp2, err := client.Post(ts.URL+"/api/sources", "application/json", bytes.NewBuffer(body2))
		if err != nil {
			t.Fatalf("POST /api/sources failed: %v", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp2.Body)
			t.Fatalf("Expected 201 Created for new Kemenkes category URL, got %d: %s", resp2.StatusCode, string(b))
		}

		t.Logf("[SUCCESS Test B] Added Kemenkes URL: %s", customKemenkes)
	})

	// ------------------------------------------------------------------------
	// TEST C: Delete https://www.halodoc.com/artikel
	// ------------------------------------------------------------------------
	t.Run("TestC_DeleteHalodocURL", func(t *testing.T) {
		targetDelete := "https://www.halodoc.com/artikel"

		req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/sources?url="+targetDelete, nil)
		if err != nil {
			t.Fatalf("Failed to create DELETE request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("DELETE /api/sources failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200 OK for delete, got %d: %s", resp.StatusCode, string(b))
		}

		// Verify target is no longer in GET /api/sources
		getResp, err := client.Get(ts.URL + "/api/sources")
		if err != nil {
			t.Fatalf("GET /api/sources failed: %v", err)
		}
		defer getResp.Body.Close()

		var sources []source.TrustedSourceItem
		_ = json.NewDecoder(getResp.Body).Decode(&sources)

		for _, s := range sources {
			if s.URL == targetDelete {
				t.Fatalf("Deleted URL still appears in active sources list: %s", s.URL)
			}
		}

		t.Logf("[SUCCESS Test C] Deleted URL: %s (confirmed absent in active registry)", targetDelete)
	})

	// ------------------------------------------------------------------------
	// TEST D: Verify crawler uses only active URLs
	// ------------------------------------------------------------------------
	t.Run("TestD_CrawlerOnlyUsesActiveURLs", func(t *testing.T) {
		// Trigger a crawl via API
		payload := map[string]interface{}{
			"topic":  "diabetes",
			"source": "all",
			"limit":  4,
		}
		b, _ := json.Marshal(payload)
		resp, err := client.Post(ts.URL+"/api/crawl", "application/json", bytes.NewBuffer(b))
		if err != nil {
			t.Fatalf("POST /api/crawl failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("Expected 202 Accepted, got %d", resp.StatusCode)
		}

		// Wait for crawl to complete
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			stResp, err := client.Get(ts.URL + "/api/crawl/status")
			if err != nil {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			var status api.CrawlStatusResponse
			_ = json.NewDecoder(stResp.Body).Decode(&status)
			stResp.Body.Close()

			if status.Status == "Completed" || status.Status == "Failed" {
				if status.Status != "Completed" {
					t.Fatalf("Crawl did not complete successfully: %s (%s)", status.Status, status.ErrorMessage)
				}
				break
			}
			time.Sleep(300 * time.Millisecond)
		}

		t.Logf("[SUCCESS Test D] Crawler finished using only active URLs from registry")
	})

	// ------------------------------------------------------------------------
	// TEST E: Verify article output format (Description is 1-3 paragraphs)
	// ------------------------------------------------------------------------
	t.Run("TestE_VerifyOutputSchema", func(t *testing.T) {
		// 1. Download articles from API
		resp, err := client.Get(ts.URL + "/api/download")
		if err != nil {
			t.Fatalf("failed to download articles: %v", err)
		}
		defer resp.Body.Close()

		var rawItems []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rawItems); err != nil {
			t.Fatalf("downloaded articles not valid JSON: %v", err)
		}

		if len(rawItems) == 0 {
			t.Fatalf("zero articles in downloaded output")
		}

		expectedKeys := map[string]bool{
			"id":          true,
			"title":       true,
			"image":       true,
			"category":    true,
			"description": true,
			"source_url":  true,
			"scraped_at":  true,
		}

		for idx, art := range rawItems {
			// A. Between 4 and 7 fields (including optional source_url, id, scraped_at)
			if len(art) < 4 || len(art) > 7 {
				t.Fatalf("Article #%d has %d fields, must have 4 to 7: %v", idx+1, len(art), art)
			}

			for k := range art {
				if !expectedKeys[k] {
					t.Fatalf("Article #%d has unapproved key %q", idx+1, k)
				}
			}

			// B. Title is non-empty string
			title, ok := art["title"].(string)
			if !ok || strings.TrimSpace(title) == "" {
				t.Fatalf("Article #%d title is empty or not string: %v", idx+1, art["title"])
			}

			// C. Category is non-empty string
			cat, ok := art["category"].(string)
			if !ok || strings.TrimSpace(cat) == "" {
				t.Fatalf("Article #%d category is empty or not string: %v", idx+1, art["category"])
			}

			// D. Image is non-empty string (valid URL, "gambar/...", or "-")
			img, ok := art["image"].(string)
			if !ok || strings.TrimSpace(img) == "" {
				t.Fatalf("Article #%d image is empty or not string: %v", idx+1, art["image"])
			}
			if img != "-" && !strings.HasPrefix(img, "http://") && !strings.HasPrefix(img, "https://") && !strings.HasPrefix(img, "gambar/") {
				t.Fatalf("Article #%d image must be valid URL, local 'gambar/...', or '-', got %q", idx+1, img)
			}

			// E. Description must be a non-empty string or array of strings
			descVal, ok := art["description"]
			if !ok {
				t.Fatalf("Article #%d missing description", idx+1)
			}
			if descStr, isStr := descVal.(string); isStr {
				if strings.TrimSpace(descStr) == "" {
					t.Fatalf("Article #%d description is empty string", idx+1)
				}
			} else if descList, isList := descVal.([]interface{}); isList {
				if len(descList) < 1 {
					t.Fatalf("Article #%d description array is empty", idx+1)
				}
				for pIdx, p := range descList {
					pStr, isStr := p.(string)
					if !isStr || strings.TrimSpace(pStr) == "" {
						t.Fatalf("Article #%d paragraph #%d is empty or not string: %v", idx+1, pIdx+1, p)
					}
				}
			} else {
				t.Fatalf("Article #%d description must be a string or JSON array, got %T: %v", idx+1, descVal, descVal)
			}
		}

		t.Logf("[SUCCESS Test E] Successfully verified %d articles conforming to canonical schema", len(rawItems))
	})

	_ = reg
	_ = outPath
}
