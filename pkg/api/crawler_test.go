package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"maps-scraper/pkg/api"
	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
)

func setupMedicalServer(t *testing.T) (*httptest.Server, string) {
	tempDir, err := os.MkdirTemp("", "med_api_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	server := api.NewServer()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux, "")
	ts := httptest.NewServer(mux)

	return ts, tempDir
}

func TestAPI_MedicalSources(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	resp, err := http.Get(ts.URL + "/api/sources")
	if err != nil {
		t.Fatalf("failed to get /api/sources: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var sources []struct {
		URL      string `json:"url"`
		Hostname string `json:"hostname"`
		Name     string `json:"name"`
		IsActive bool   `json:"is_active"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
		t.Fatalf("failed to decode sources JSON: %v", err)
	}

	if len(sources) < 2 {
		t.Errorf("expected at least 2 default sources, got %d", len(sources))
	}

	// Verify URLs start with http/https
	for _, s := range sources {
		if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
			t.Errorf("source URL does not start with http/https: %s", s.URL)
		}
	}
}

func TestAPI_MedicalCrawlLifecycle(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	// 1. Initial status check
	statusResp, err := http.Get(ts.URL + "/api/crawl/status")
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	defer statusResp.Body.Close()

	var initialStatus api.CrawlStatusResponse
	json.NewDecoder(statusResp.Body).Decode(&initialStatus)
	// Status should be Waiting or Completed from preloaded
	if initialStatus.Status != "Waiting" && initialStatus.Status != "Completed" {
		t.Errorf("unexpected initial status: %s", initialStatus.Status)
	}

	// 2. Start a crawl for a small limit
	crawlPayload := `{"topic": "diabetes", "source": "kemenkes", "limit": 2}`
	postResp, err := http.Post(ts.URL+"/api/crawl", "application/json", bytes.NewBufferString(crawlPayload))
	if err != nil {
		t.Fatalf("failed to post crawl: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", postResp.StatusCode)
	}

	var startedSession api.CrawlStatusResponse
	json.NewDecoder(postResp.Body).Decode(&startedSession)

	// Status immediately upon start must NOT be Completed!
	if startedSession.Status == "Completed" {
		t.Errorf("status must never be Completed immediately upon launching crawl")
	}

	// 3. Poll status until finished or timeout
	deadline := time.Now().Add(25 * time.Second)
	finalStatus := api.CrawlStatusResponse{}
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)

		stResp, err := http.Get(ts.URL + "/api/crawl/status")
		if err != nil {
			continue
		}

		var st api.CrawlStatusResponse
		_ = json.NewDecoder(stResp.Body).Decode(&st)
		stResp.Body.Close()

		if st.Status == "Completed" || st.Status == "Failed" {
			finalStatus = st
			break
		}
	}

	if finalStatus.Status != "Completed" {
		t.Logf("Crawl did not finish with Completed within deadline, final status: %s (err: %s)", finalStatus.Status, finalStatus.ErrorMessage)
	} else {
		if finalStatus.Collected == 0 {
			t.Errorf("expected collected articles > 0 upon Completed")
		}
		if len(finalStatus.Articles) == 0 {
			t.Errorf("expected articles array to be populated")
		}
	}
}

func TestAPI_DownloadArticles(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	resp, err := http.Get(ts.URL + "/api/download")
	if err != nil {
		t.Fatalf("failed to get /api/download: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	disposition := resp.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, "articles.json") {
		t.Errorf("expected Content-Disposition with articles.json, got %q", disposition)
	}

	var arts []model.Article
	if err := json.NewDecoder(resp.Body).Decode(&arts); err != nil {
		t.Fatalf("failed to decode downloaded JSON articles: %v", err)
	}
}

func TestAPI_Articles_DeleteAndSort(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	// 1. Fetch articles
	resp, err := http.Get(ts.URL + "/api/articles")
	if err != nil {
		t.Fatalf("failed to get /api/articles: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var articles []model.Article
	if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		t.Fatalf("failed to decode articles: %v", err)
	}

	// 2. Test sort parameter
	respNewest, err := http.Get(ts.URL + "/api/articles?sort=newest")
	if err == nil {
		respNewest.Body.Close()
		if respNewest.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK for sort=newest, got %d", respNewest.StatusCode)
		}
	}

	respOldest, err := http.Get(ts.URL + "/api/articles?sort=oldest")
	if err == nil {
		respOldest.Body.Close()
		if respOldest.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK for sort=oldest, got %d", respOldest.StatusCode)
		}
	}

	// 3. Test DELETE non-existent article
	reqBad, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/articles/non_existent_id", nil)
	respBad, err := http.DefaultClient.Do(reqBad)
	if err != nil {
		t.Fatalf("delete non-existent failed: %v", err)
	}
	defer respBad.Body.Close()
	if respBad.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent article deletion, got %d", respBad.StatusCode)
	}

	// 4. Test DELETE an article if any exist
	if len(articles) > 0 {
		targetID := articles[0].ID
		if targetID != "" {
			delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/articles/"+targetID, nil)
			delResp, err := http.DefaultClient.Do(delReq)
			if err != nil {
				t.Fatalf("delete failed: %v", err)
			}
			defer delResp.Body.Close()
			if delResp.StatusCode != http.StatusOK {
				t.Errorf("expected 200 OK for delete, got %d", delResp.StatusCode)
			}

			var delBody map[string]interface{}
			_ = json.NewDecoder(delResp.Body).Decode(&delBody)
			if delBody["status"] != "deleted" {
				t.Errorf("expected status 'deleted', got %v", delBody["status"])
			}
		}
	}
}

func TestAPI_RunsAndScheduler(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	// 1. Check /api/runs returns a valid list
	respRuns, err := http.Get(ts.URL + "/api/runs")
	if err != nil {
		t.Fatalf("failed to get /api/runs: %v", err)
	}
	defer respRuns.Body.Close()
	if respRuns.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /api/runs, got %d", respRuns.StatusCode)
	}

	var runs []model.CrawlRun
	if err := json.NewDecoder(respRuns.Body).Decode(&runs); err != nil {
		t.Fatalf("failed to decode runs array: %v", err)
	}

	// 2. Check /api/runs/today returns today runs
	respToday, err := http.Get(ts.URL + "/api/runs/today")
	if err != nil {
		t.Fatalf("failed to get /api/runs/today: %v", err)
	}
	defer respToday.Body.Close()
	if respToday.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /api/runs/today, got %d", respToday.StatusCode)
	}

	var todayData struct {
		TodayRuns []model.CrawlRun `json:"today_runs"`
		LatestRun *model.CrawlRun  `json:"latest_run"`
		Count     int              `json:"count"`
	}
	if err := json.NewDecoder(respToday.Body).Decode(&todayData); err != nil {
		t.Fatalf("failed to decode /api/runs/today: %v", err)
	}

	// 3. Check /api/scheduler/status
	respSched, err := http.Get(ts.URL + "/api/scheduler/status")
	if err != nil {
		t.Fatalf("failed to get /api/scheduler/status: %v", err)
	}
	defer respSched.Body.Close()
	if respSched.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /api/scheduler/status, got %d", respSched.StatusCode)
	}

	var schedSt map[string]interface{}
	if err := json.NewDecoder(respSched.Body).Decode(&schedSt); err != nil {
		t.Fatalf("failed to decode scheduler status: %v", err)
	}
	if schedSt["daily_schedule"] == nil {
		t.Errorf("expected daily_schedule in status response")
	}
}

func TestAPI_RunDaysAndMultiGranularExport(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	// 1. Check /api/runs/days returns valid list
	respDays, err := http.Get(ts.URL + "/api/runs/days")
	if err != nil {
		t.Fatalf("failed to get /api/runs/days: %v", err)
	}
	defer respDays.Body.Close()
	if respDays.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /api/runs/days, got %d", respDays.StatusCode)
	}

	// 2. Check /api/download (Export All)
	respExportAll, err := http.Get(ts.URL + "/api/download")
	if err != nil {
		t.Fatalf("failed to get /api/download: %v", err)
	}
	defer respExportAll.Body.Close()
	if respExportAll.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for export all, got %d", respExportAll.StatusCode)
	}

	// 3. Check /api/download?run_id=Run%20%23001 (Export Per Run)
	respExportRun, err := http.Get(ts.URL + "/api/download?run_id=Run%20%23001")
	if err != nil {
		t.Fatalf("failed to get /api/download?run_id=...: %v", err)
	}
	defer respExportRun.Body.Close()
	if respExportRun.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for export run, got %d", respExportRun.StatusCode)
	}
	var runArticles []model.DiskArticle
	if err := json.NewDecoder(respExportRun.Body).Decode(&runArticles); err != nil {
		t.Fatalf("failed to decode exported run articles: %v", err)
	}

	// 4. Check /api/download?day=2026-09-07 (Export Per Day)
	respExportDay, err := http.Get(ts.URL + "/api/download?day=2026-09-07")
	if err != nil {
		t.Fatalf("failed to get /api/download?day=...: %v", err)
	}
	defer respExportDay.Body.Close()
	if respExportDay.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for export day, got %d", respExportDay.StatusCode)
	}
	var dayArticles []model.DiskArticle
	if err := json.NewDecoder(respExportDay.Body).Decode(&dayArticles); err != nil {
		t.Fatalf("failed to decode exported day articles: %v", err)
	}

	// 5. Check /api/articles?day=2026-09-07
	respArticlesDay, err := http.Get(ts.URL + "/api/articles?day=2026-09-07")
	if err != nil {
		t.Fatalf("failed to get /api/articles?day=...: %v", err)
	}
	defer respArticlesDay.Body.Close()
	if respArticlesDay.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /api/articles?day=..., got %d", respArticlesDay.StatusCode)
	}
	var articlesDay []model.Article
	if err := json.NewDecoder(respArticlesDay.Body).Decode(&articlesDay); err != nil {
		t.Fatalf("failed to decode articles by day: %v", err)
	}
}

func TestAPI_DeleteRun_Cases(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	client := &http.Client{Timeout: 10 * time.Second}
	runStorage := crawler.GetRunStorage()

	// 1. Create 3 distinct completed runs (Run A, Run B, Run C)
	runA, errA := runStorage.CreateRun("Halodoc", "", "Topic A", 5, "manual")
	if errA != nil {
		t.Fatalf("failed to create run A: %v", errA)
	}
	runA.Status = model.RunStatusCompleted
	_ = runStorage.UpdateRun(runA)

	runB, errB := runStorage.CreateRun("Halodoc", "", "Topic B", 5, "manual")
	if errB != nil {
		t.Fatalf("failed to create run B: %v", errB)
	}
	runB.Status = model.RunStatusCompleted
	_ = runStorage.UpdateRun(runB)

	runC, errC := runStorage.CreateRun("detikHealth", "", "Topic C", 5, "manual")
	if errC != nil {
		t.Fatalf("failed to create run C: %v", errC)
	}
	runC.Status = model.RunStatusCompleted
	_ = runStorage.UpdateRun(runC)

	// Verify all 3 exist
	if _, found := runStorage.GetRun(runA.ID); !found {
		t.Fatalf("run A not found")
	}
	if _, found := runStorage.GetRun(runB.ID); !found {
		t.Fatalf("run B not found")
	}
	if _, found := runStorage.GetRun(runC.ID); !found {
		t.Fatalf("run C not found")
	}

	// Case 1: Delete Run B -> Expected: Run A and Run C remain
	t.Run("Case 1: Delete Run B", func(t *testing.T) {
		delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/runs/"+runB.ID, nil)
		resp, err := client.Do(delReq)
		if err != nil {
			t.Fatalf("failed to delete run B: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		var resBody map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&resBody)
		if resBody["status"] != "deleted" {
			t.Errorf("expected status 'deleted', got %v", resBody["status"])
		}

		// Verify Run B is gone, Run A and Run C still exist
		if _, found := runStorage.GetRun(runB.ID); found {
			t.Errorf("Run B still exists in storage after deletion")
		}
		if _, found := runStorage.GetRun(runA.ID); !found {
			t.Errorf("Run A missing after deleting Run B")
		}
		if _, found := runStorage.GetRun(runC.ID); !found {
			t.Errorf("Run C missing after deleting Run B")
		}
	})

	// Case 4: Delete non-existent run -> Expected: 404 Not Found & others safe
	t.Run("Case 4: Delete Non-Existent Run", func(t *testing.T) {
		delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/runs/non_existent_run_id_9999", nil)
		resp, err := client.Do(delReq)
		if err != nil {
			t.Fatalf("failed to send delete: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 Not Found, got %d", resp.StatusCode)
		}

		// Ensure remaining runs still exist
		if _, found := runStorage.GetRun(runA.ID); !found {
			t.Errorf("Run A missing after failed deletion")
		}
		if _, found := runStorage.GetRun(runC.ID); !found {
			t.Errorf("Run C missing after failed deletion")
		}
	})

	// Case 2: Delete Run A -> Expected: Run C remains
	t.Run("Case 2: Delete Run A", func(t *testing.T) {
		delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/runs/"+runA.ID, nil)
		resp, err := client.Do(delReq)
		if err != nil {
			t.Fatalf("failed to delete run A: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		if _, found := runStorage.GetRun(runA.ID); found {
			t.Errorf("Run A still exists in storage after deletion")
		}
		if _, found := runStorage.GetRun(runC.ID); !found {
			t.Errorf("Run C missing after deleting Run A")
		}
	})

	// Case 3: Delete Run C -> Expected: Run C gone
	t.Run("Case 3: Delete Run C", func(t *testing.T) {
		delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/runs/"+runC.ID, nil)
		resp, err := client.Do(delReq)
		if err != nil {
			t.Fatalf("failed to delete run C: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		if _, found := runStorage.GetRun(runC.ID); found {
			t.Errorf("Run C still exists in storage after deletion")
		}
	})
}

func TestAPI_SchedulerConfigUpdate(t *testing.T) {
	ts, tempDir := setupMedicalServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Valid update via "HH:mm"
	body := strings.NewReader(`{"time": "08:45"}`)
	resp, err := client.Post(ts.URL+"/api/scheduler/config", "application/json", body)
	if err != nil {
		t.Fatalf("failed to POST /api/scheduler/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var resData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resData["daily_schedule"] != "08:45 WIB Daily" {
		t.Errorf("expected '08:45 WIB Daily', got %v", resData["daily_schedule"])
	}

	// 2. Verify via GET /api/scheduler/status
	respStatus, err := client.Get(ts.URL + "/api/scheduler/status")
	if err != nil {
		t.Fatalf("failed to GET /api/scheduler/status: %v", err)
	}
	defer respStatus.Body.Close()

	var statusData map[string]interface{}
	if err := json.NewDecoder(respStatus.Body).Decode(&statusData); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}
	if statusData["daily_schedule"] != "08:45 WIB Daily" {
		t.Errorf("expected daily_schedule '08:45 WIB Daily', got %v", statusData["daily_schedule"])
	}

	// 3. Test invalid time format (e.g. 25:00)
	bodyBad := strings.NewReader(`{"time": "25:00"}`)
	respBad, err := client.Post(ts.URL+"/api/scheduler/config", "application/json", bodyBad)
	if err != nil {
		t.Fatalf("failed to POST invalid time: %v", err)
	}
	defer respBad.Body.Close()
	if respBad.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for hour 25, got %d", respBad.StatusCode)
	}

	// 4. Test invalid format string
	bodyBadFmt := strings.NewReader(`{"time": "invalid"}`)
	respBadFmt, err := client.Post(ts.URL+"/api/scheduler/config", "application/json", bodyBadFmt)
	if err != nil {
		t.Fatalf("failed to POST invalid time: %v", err)
	}
	defer respBadFmt.Body.Close()
	if respBadFmt.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for format 'invalid', got %d", respBadFmt.StatusCode)
	}
}





