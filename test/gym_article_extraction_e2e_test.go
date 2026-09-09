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
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/source"
)

func TestE2E_GymArticleExtraction_ReferenceCase(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gym_e2e_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	storagePath := filepath.Join(tempDir, "trusted_sources.json")

	reg := source.NewRegistry(storagePath)
	origRegistry := source.GetRegistry()
	source.SetRegistry(reg)
	defer source.SetRegistry(origRegistry)

	// Clean up downloaded images at the end of test
	defer func() {
		_ = os.Remove("gambar/Nge-Gym Artinya: Pahami Tujuan Sehatmu Sekarang.jpg")
		_ = os.Remove("gambar/Nge-Gym Artinya - Pahami Tujuan Sehatmu Sekarang.jpg")
	}()

	server := api.NewServer()
	api.GetMedicalCrawlerController().Stop()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux, "../web")
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctrl := api.GetMedicalCrawlerController()
	for _, a := range ctrl.GetArticles() {
		if strings.Contains(strings.ToLower(a.Title), "nge-gym") || strings.Contains(strings.ToLower(a.SourceURL), "nge-gym") {
			_, _ = ctrl.DeleteArticle(a.ID)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}

	targetURL := "https://www.halodoc.com/artikel/nge-gym-artinya-pahami-tujuan-sehatmu-sekarang?srsltid=AfmBOoo9lLhqhlCVc3ltAXaRKE6L5-Jzp8PTsTOGfBLDd0EZQtsf6sy7"

	// 1. Add Target Source URL via API
	t.Run("Step1_AddTrustedSourceURL", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"url": targetURL})
		resp, err := client.Post(ts.URL+"/api/sources", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("POST /api/sources failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			respBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201 Created, got %d: %s", resp.StatusCode, string(respBytes))
		}
	})

	// 2. Trigger Crawl for "gym"
	t.Run("Step2_TriggerCrawlAndVerifyResults", func(t *testing.T) {
		crawlPayload, _ := json.Marshal(map[string]interface{}{
			"topic":  "gym",
			"source": "all",
			"limit":  1,
		})

		resp, err := client.Post(ts.URL+"/api/crawl", "application/json", bytes.NewBuffer(crawlPayload))
		if err != nil {
			t.Fatalf("POST /api/crawl failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			t.Fatalf("Expected 200 OK or 202 Accepted for crawl start, got %d", resp.StatusCode)
		}

		// Wait for crawl completion
		deadline := time.Now().Add(25 * time.Second)
		var lastStatus struct {
			Status    string          `json:"status"`
			Articles  []model.Article `json:"articles"`
			Collected int             `json:"collected"`
		}

		for time.Now().Before(deadline) {
			statusResp, err := client.Get(ts.URL + "/api/crawl/status")
			if err == nil {
				_ = json.NewDecoder(statusResp.Body).Decode(&lastStatus)
				statusResp.Body.Close()
				if lastStatus.Status == "Completed" || lastStatus.Status == "Failed" {
					break
				}
			}
			time.Sleep(300 * time.Millisecond)
		}

		if lastStatus.Status != "Completed" {
			t.Fatalf("Crawl did not complete successfully. Final status: %s", lastStatus.Status)
		}

		if len(lastStatus.Articles) == 0 {
			t.Fatalf("Expected at least 1 article crawled, got 0")
		}

		art := lastStatus.Articles[0]

		// Verify Title
		if !strings.Contains(art.Title, "Nge-gym Artinya") && !strings.Contains(art.Title, "Nge-Gym Artinya") {
			t.Errorf("Title: got %q, expected containing 'Nge-gym Artinya'", art.Title)
		}

		// Verify Category (genuine article tag "Hidup Sehat" / "Kebugaran")
		if !strings.Contains(strings.ToLower(art.Category), "hidup sehat") && !strings.Contains(strings.ToLower(art.Category), "kebugaran") && !strings.Contains(strings.ToLower(art.Category), "gym") {
			t.Errorf("Category: got %q, expected containing 'Hidup Sehat' or 'Kebugaran'", art.Category)
		}

		// Verify Image URL (direct remote CDN image link)
		if !strings.HasPrefix(art.Image, "http://") && !strings.HasPrefix(art.Image, "https://") && !strings.HasPrefix(art.Image, "gambar/") {
			t.Errorf("Image URL: got %q, expected valid remote image link", art.Image)
		}

		// Verify Description
		if len(art.Description) < 1 {
			t.Fatalf("Description must have at least 1 paragraph, got %d", len(art.Description))
		}
		for i, p := range art.Description {
			if strings.TrimSpace(p) == "" {
				t.Errorf("Paragraph #%d is empty", i+1)
			}
		}

		// Verify SourceURL
		if art.SourceURL == "" || !strings.Contains(art.SourceURL, "halodoc.com/artikel/nge-gym-artinya") {
			t.Errorf("SourceURL: got %q, expected containing target URL", art.SourceURL)
		}
	})
}
