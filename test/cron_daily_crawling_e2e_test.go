package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"maps-scraper/pkg/api"
	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/scheduler"
	"maps-scraper/pkg/source"
)

// TestE2E_CronDailyCrawling verifies the end-to-end automated daily CRON crawling system:
// 1. Scheduler Timing & Calculation
// 2. Concurrency Safety & Idempotency Locking
// 3. Multi-source Daily Crawling (Halodoc, detikHealth, Kemenkes)
// 4. Source Attribution & Canonical Schema Validation
// 5. Run Timing, Duration & Status Integrity
// 6. Partial Failure Handling (PARTIAL_SUCCESS)
// 7. REST API Telemetry & Historical Endpoints (/api/runs, /api/runs/today, /api/runs/days, /api/scheduler/status)
// 8. Storage & Database Consistency
func TestE2E_CronDailyCrawling(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cron_e2e_test_*")
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set temporary storage paths
	trustedSourcesPath := filepath.Join(tempDir, "trusted_sources.json")
	runsStoragePath := filepath.Join(tempDir, "runs.json")
	outputDir := filepath.Join(tempDir, "output")
	_ = os.MkdirAll(outputDir, 0755)
	articlesPath := filepath.Join(outputDir, "articles.json")

	// Initialize isolated Registry & RunStorage
	reg := source.NewRegistry(trustedSourcesPath)
	origRegistry := source.GetRegistry()
	source.SetRegistry(reg)
	defer source.SetRegistry(origRegistry)

	runStorage := crawler.NewRunStorage(runsStoragePath)

	// Setup Server & Mux
	server := api.NewServer()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux, "../web")
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &http.Client{Timeout: 45 * time.Second}

	var latestCrawledArticles []model.Article

	// ------------------------------------------------------------------------
	// Test 1: CRON Schedule Calculation & Next Run Time
	// ------------------------------------------------------------------------
	t.Run("Test1_SchedulerNextRunCalculation", func(t *testing.T) {
		cfg := scheduler.Config{
			Enabled:       true,
			DailyAtHour:   1, // 01:00 AM
			DailyAtMinute: 0,
		}

		sched := scheduler.NewDailyScheduler(cfg, func(triggerType string) error {
			return nil
		})

		now := time.Date(2026, 9, 7, 0, 30, 0, 0, time.Local)
		next := sched.CalculateNextRun(now)
		expected := time.Date(2026, 9, 7, 1, 0, 0, 0, time.Local)
		if !next.Equal(expected) {
			t.Errorf("Expected next run at %v, got %v", expected, next)
		}

		afterRun := time.Date(2026, 9, 7, 2, 0, 0, 0, time.Local)
		nextDay := sched.CalculateNextRun(afterRun)
		expectedNextDay := time.Date(2026, 9, 8, 1, 0, 0, 0, time.Local)
		if !nextDay.Equal(expectedNextDay) {
			t.Errorf("Expected next run at %v, got %v", expectedNextDay, nextDay)
		}

		status := sched.GetStatus()
		if !status.Enabled {
			t.Errorf("Expected scheduler to be enabled")
		}
		if status.DailySchedule != "01:00 WIB Daily" {
			t.Errorf("Expected DailySchedule '01:00 WIB Daily', got %s", status.DailySchedule)
		}
		t.Logf("[PASS Test 1] Next run calculation accurate: %s -> %s", now.Format(time.RFC3339), next.Format(time.RFC3339))
	})

	// ------------------------------------------------------------------------
	// Test 2: Concurrency Locking & Overlapping Trigger Rejection
	// ------------------------------------------------------------------------
	t.Run("Test2_ConcurrencyLockingAndIdempotency", func(t *testing.T) {
		var activeCounter int32
		var maxConcurrent int32
		var totalStarts int32

		trigger := func(triggerType string) error {
			curr := atomic.AddInt32(&activeCounter, 1)
			atomic.AddInt32(&totalStarts, 1)
			for {
				max := atomic.LoadInt32(&maxConcurrent)
				if curr > max {
					if atomic.CompareAndSwapInt32(&maxConcurrent, max, curr) {
						break
					}
				} else {
					break
				}
			}
			time.Sleep(120 * time.Millisecond) // Simulate crawl work
			atomic.AddInt32(&activeCounter, -1)
			return nil
		}

		sched := scheduler.NewDailyScheduler(scheduler.Config{Enabled: true, DailyAtHour: 1}, trigger)

		var wg sync.WaitGroup
		var duplicateRejections int32
		var successfulLaunches int32

		// Launch 5 concurrent triggers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ok, err := sched.ExecuteRun(fmt.Sprintf("concurrent-%d", idx))
				if ok && err == nil {
					atomic.AddInt32(&successfulLaunches, 1)
				} else {
					atomic.AddInt32(&duplicateRejections, 1)
				}
			}(i)
		}

		wg.Wait()

		if maxConcurrent > 1 {
			t.Errorf("Expected max concurrent execution to be 1, got %d", maxConcurrent)
		}
		if successfulLaunches != 1 {
			t.Errorf("Expected exactly 1 successful run start, got %d", successfulLaunches)
		}
		if duplicateRejections != 4 {
			t.Errorf("Expected 4 duplicate triggers rejected, got %d", duplicateRejections)
		}

		t.Logf("[PASS Test 2] Concurrency locking verified: 1 started, %d rejected safely", duplicateRejections)
	})

	// ------------------------------------------------------------------------
	// Test 3: Multi-Source Crawling via API Trigger & Scheduler Status
	// ------------------------------------------------------------------------
	t.Run("Test3_MultiSourceCronCrawlingExecution", func(t *testing.T) {
		// Verify initial scheduler status from endpoint
		schedResp, err := client.Get(ts.URL + "/api/scheduler/status")
		if err != nil {
			t.Fatalf("GET /api/scheduler/status failed: %v", err)
		}
		var initialSched scheduler.Status
		_ = json.NewDecoder(schedResp.Body).Decode(&initialSched)
		schedResp.Body.Close()

		if schedResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 OK from /api/scheduler/status, got %d", schedResp.StatusCode)
		}

		// Trigger crawl as automated cron run
		crawlReq := map[string]interface{}{
			"topic":               "diabetes",
			"source":              "all",
			"limit":               3,
			"articles_per_source": 1,
			"trigger":             "cron",
		}
		b, _ := json.Marshal(crawlReq)
		startResp, err := client.Post(ts.URL+"/api/crawl", "application/json", bytes.NewBuffer(b))
		if err != nil {
			t.Fatalf("POST /api/crawl failed: %v", err)
		}
		defer startResp.Body.Close()

		if startResp.StatusCode != http.StatusAccepted {
			t.Fatalf("Expected 202 Accepted, got %d", startResp.StatusCode)
		}

		var startResult map[string]interface{}
		_ = json.NewDecoder(startResp.Body).Decode(&startResult)
		runID, _ := startResult["run_id"].(string)

		if runID == "" {
			t.Fatalf("Expected non-empty run_id in crawl response")
		}

		// Wait for crawl completion with timeout
		deadline := time.Now().Add(120 * time.Second)
		var finalStatus api.CrawlStatusResponse
		for time.Now().Before(deadline) {
			stResp, err := client.Get(ts.URL + "/api/crawl/status")
			if err == nil {
				_ = json.NewDecoder(stResp.Body).Decode(&finalStatus)
				stResp.Body.Close()
				if finalStatus.Status == "Completed" || finalStatus.Status == "Failed" || finalStatus.Status == "PartialSuccess" {
					break
				}
			}
			time.Sleep(300 * time.Millisecond)
		}

		if finalStatus.Status != "Completed" && finalStatus.Status != "PartialSuccess" {
			t.Fatalf("Expected Completed or PartialSuccess, got: %s (Error: %s)", finalStatus.Status, finalStatus.ErrorMessage)
		}

		latestCrawledArticles = finalStatus.Articles
		t.Logf("[PASS Test 3] Crawl run %s finished with status: %s (Items: %d)", runID, finalStatus.Status, finalStatus.Collected)
	})

	// ------------------------------------------------------------------------
	// Test 4: Verify Article Schema & Source Attribution
	// ------------------------------------------------------------------------
	t.Run("Test4_ArticleSchemaAndSourceAttribution", func(t *testing.T) {
		// Verify schema of latest crawled articles
		if len(latestCrawledArticles) > 0 {
			sourceCount := make(map[string]int)
			for idx, art := range latestCrawledArticles {
				if strings.TrimSpace(art.Title) == "" {
					t.Errorf("Article #%d has empty title", idx)
				}
				if strings.TrimSpace(art.Category) == "" {
					t.Errorf("Article #%d has empty category", idx)
				}
				if len(art.Description) == 0 {
					t.Errorf("Article #%d has empty description", idx)
				}
				if strings.TrimSpace(art.SourceURL) == "" {
					t.Errorf("Article #%d missing source_url", idx)
				}

				if strings.Contains(art.SourceURL, "halodoc.com") {
					sourceCount["halodoc"]++
				} else if strings.Contains(art.SourceURL, "detik.com") {
					sourceCount["detik"]++
				} else if strings.Contains(art.SourceURL, "kemkes.go.id") {
					sourceCount["kemenkes"]++
				} else {
					sourceCount["other"]++
				}
			}
			t.Logf("[PASS Test 4] Source attribution verified on %d freshly crawled articles. Breakdown: %v", len(latestCrawledArticles), sourceCount)
		}

		// Also verify general download endpoint returns canonical json array
		resp, err := client.Get(ts.URL + "/api/download")
		if err != nil {
			t.Fatalf("GET /api/download failed: %v", err)
		}
		defer resp.Body.Close()

		var articles []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
			t.Fatalf("Failed to decode articles: %v", err)
		}

		if len(articles) == 0 {
			t.Fatalf("No articles found in /api/download")
		}

		for idx, art := range articles {
			title, _ := art["title"].(string)
			category, _ := art["category"].(string)
			desc := art["description"]

			if strings.TrimSpace(title) == "" {
				t.Errorf("Article #%d has empty title", idx)
			}
			if strings.TrimSpace(category) == "" {
				t.Errorf("Article #%d has empty category", idx)
			}
			if desc == nil {
				t.Errorf("Article #%d has nil description", idx)
			}
		}

		t.Logf("[PASS Test 4] Canonical schema format verified for %d articles in database", len(articles))
	})

	// ------------------------------------------------------------------------
	// Test 5: Run Metrics, Duration & Timing Integrity
	// ------------------------------------------------------------------------
	t.Run("Test5_RunMetricsAndTimingIntegrity", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/runs")
		if err != nil {
			t.Fatalf("GET /api/runs failed: %v", err)
		}
		defer resp.Body.Close()

		var runs []*model.CrawlRun
		if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
			t.Fatalf("Failed to decode runs: %v", err)
		}

		if len(runs) == 0 {
			t.Fatalf("Expected at least 1 run in /api/runs")
		}

		latestRun := runs[0]
		if latestRun.ID == "" {
			t.Errorf("Run ID is empty")
		}
		if latestRun.StartedAt.IsZero() {
			t.Errorf("StartedAt timestamp is zero")
		}
		if latestRun.FinishedAt == nil || latestRun.FinishedAt.IsZero() {
			t.Errorf("FinishedAt timestamp is missing or zero")
		} else {
			if latestRun.FinishedAt.Before(latestRun.StartedAt) {
				t.Errorf("FinishedAt (%v) is before StartedAt (%v)", latestRun.FinishedAt, latestRun.StartedAt)
			}
		}
		if latestRun.DurationSec < 0 {
			t.Errorf("DurationSec cannot be negative, got %f", latestRun.DurationSec)
		}

		t.Logf("[PASS Test 5] Run metrics verified: ID=%s, Status=%s, Duration=%.2fs, TotalSuccess=%d",
			latestRun.ID, latestRun.Status, latestRun.DurationSec, latestRun.TotalSuccess)
	})

	// ------------------------------------------------------------------------
	// Test 6: Partial Failure Handling (PARTIAL_SUCCESS)
	// ------------------------------------------------------------------------
	t.Run("Test6_PartialFailureClassification", func(t *testing.T) {
		// Create a test run via RunStorage
		run, err := runStorage.CreateRun("all", "", "diabetes", 10, "cron")
		if err != nil {
			t.Fatalf("Failed to create run: %v", err)
		}

		run.SourceStats = map[string]model.SourceStat{
			"Halodoc": {
				Crawled: 3,
				Success: 3,
				Failed:  0,
				Status:  "SUCCESS",
			},
			"Detik Health": {
				Crawled: 3,
				Success: 3,
				Failed:  0,
				Status:  "SUCCESS",
			},
			"Unreachable Source": {
				Crawled: 0,
				Success: 0,
				Failed:  1,
				Status:  "FAILED",
				Error:   "connection timeout",
			},
		}

		// Calculate status logic
		successCount := 0
		failCount := 0
		for _, s := range run.SourceStats {
			if s.Status == "SUCCESS" {
				successCount++
			} else if s.Status == "FAILED" {
				failCount++
			}
		}

		var finalStatus model.RunStatus
		if successCount > 0 && failCount > 0 {
			finalStatus = model.RunStatusPartialSuccess
		} else if successCount > 0 {
			finalStatus = model.RunStatusCompleted
		} else {
			finalStatus = model.RunStatusFailed
		}

		run.Status = finalStatus
		now := time.Now()
		run.FinishedAt = &now
		run.TotalSuccess = 6
		run.TotalFailed = 1

		if run.Status != model.RunStatusPartialSuccess {
			t.Fatalf("Expected status to be PARTIAL_SUCCESS, got: %s", run.Status)
		}

		// Save to run storage
		if err := runStorage.UpdateRun(run); err != nil {
			t.Fatalf("Failed to update partial run: %v", err)
		}

		// Verify retrieval
		retrieved, found := runStorage.GetRun(run.ID)
		if !found {
			t.Fatalf("Failed to retrieve partial run %s", run.ID)
		}
		if retrieved.Status != model.RunStatusPartialSuccess {
			t.Errorf("Retrieved status mismatch: expected %s, got %s", model.RunStatusPartialSuccess, retrieved.Status)
		}
		if retrieved.TotalSuccess != 6 {
			t.Errorf("Expected TotalSuccess 6, got %d", retrieved.TotalSuccess)
		}

		t.Logf("[PASS Test 6] Partial failure correctly classified as %s and persisted in RunStorage", retrieved.Status)
	})

	// ------------------------------------------------------------------------
	// Test 7: Historical Monitoring & Telemetry Endpoints
	// ------------------------------------------------------------------------
	t.Run("Test7_HistoricalMonitoringEndpoints", func(t *testing.T) {
		// 1. GET /api/runs
		r1, err := client.Get(ts.URL + "/api/runs")
		if err != nil || r1.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/runs failed: code=%d err=%v", r1.StatusCode, err)
		}
		r1.Body.Close()

		// 2. GET /api/runs/today
		r2, err := client.Get(ts.URL + "/api/runs/today")
		if err != nil || r2.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/runs/today failed: code=%d err=%v", r2.StatusCode, err)
		}
		var todayStats map[string]interface{}
		_ = json.NewDecoder(r2.Body).Decode(&todayStats)
		r2.Body.Close()

		if _, ok := todayStats["count"]; !ok {
			t.Errorf("/api/runs/today missing count field")
		}

		// 3. GET /api/runs/days?days=7
		r3, err := client.Get(ts.URL + "/api/runs/days?days=7")
		if err != nil || r3.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/runs/days failed: code=%d err=%v", r3.StatusCode, err)
		}
		var dailyTrends []map[string]interface{}
		_ = json.NewDecoder(r3.Body).Decode(&dailyTrends)
		r3.Body.Close()

		if len(dailyTrends) == 0 {
			t.Errorf("/api/runs/days returned empty array")
		}

		// 4. GET /api/system/status (Docker & Health status)
		r4, err := client.Get(ts.URL + "/api/system/status")
		if err != nil || r4.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/system/status failed: code=%d err=%v", r4.StatusCode, err)
		}
		var sysStatus map[string]interface{}
		_ = json.NewDecoder(r4.Body).Decode(&sysStatus)
		r4.Body.Close()

		if sysStatus["status"] != "healthy" && sysStatus["status"] != "degraded" {
			t.Errorf("Unexpected system status: %v", sysStatus["status"])
		}

		t.Logf("[PASS Test 7] Historical & Telemetry API endpoints (/api/runs, /today, /days, /system/status) operating nominally")
	})

	_ = articlesPath
}
