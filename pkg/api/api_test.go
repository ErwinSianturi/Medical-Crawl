package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"maps-scraper/pkg/api"
	"maps-scraper/pkg/job"
	"maps-scraper/pkg/model"
)

func setupTestServer(t *testing.T) (*httptest.Server, *job.Manager, string) {
	tempDir, _ := os.MkdirTemp("", "api_test")
	dataDir := filepath.Join(tempDir, "data")
	logsDir := filepath.Join(tempDir, "logs")

	m, _ := job.NewManager(dataDir, logsDir)
	s := api.NewServer(m)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")
	ts := httptest.NewServer(mux)

	return ts, m, tempDir
}

func TestAPI_CreateJob(t *testing.T) {
	ts, _, tempDir := setupTestServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	payload := `{"target": "toko test", "location": {"province": "Jawa Timur", "city": "Surabaya"}, "headless": true}`
	resp, err := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("failed to post job: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 Created, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	
	// Print to see the response
	t.Logf("Response: %+v", result)
	
	// the response might just be the JobState object directly
	if result["config"] == nil {
		t.Errorf("expected job config in response")
	}
}

func TestAPI_InvalidJobAction(t *testing.T) {
	ts, _, tempDir := setupTestServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	// Try to start non-existent job
	resp, err := http.Post(ts.URL+"/api/jobs/invalid_job/start", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", resp.StatusCode)
	}
}

func TestAPI_ExportRegionAndAllData(t *testing.T) {
	ts, m, tempDir := setupTestServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	// Create a job in manager
	jobA, err := m.CreateJob(model.JobConfig{
		Target:   "toko semen",
		Location: model.LocationConfig{City: "Surabaya"},
		Queries:  []string{"q1"},
	})
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// 1. Test Region Export
	respRegion, err := http.Get(ts.URL + "/api/jobs/" + jobA.Config.ID + "/export?format=csv")
	if err != nil {
		t.Fatalf("failed to get region export: %v", err)
	}
	defer respRegion.Body.Close()
	if respRegion.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for region export, got %d", respRegion.StatusCode)
	}

	// 2. Test Export All Data
	respAll, err := http.Get(ts.URL + "/api/export/all?format=csv")
	if err != nil {
		t.Fatalf("failed to get export all: %v", err)
	}
	defer respAll.Body.Close()
	if respAll.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for export all, got %d", respAll.StatusCode)
	}
}

func TestAPI_DeleteJob(t *testing.T) {
	ts, m, tempDir := setupTestServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	job1, _ := m.CreateJob(model.JobConfig{Target: "toko 1", Location: model.LocationConfig{City: "Surabaya"}})
	job2, _ := m.CreateJob(model.JobConfig{Target: "toko 2", Location: model.LocationConfig{City: "Jakarta"}})

	client := &http.Client{}

	// 1. Delete job1 successfully
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/jobs/"+job1.Config.ID, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to delete job1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	var delResp map[string]string
	json.NewDecoder(resp.Body).Decode(&delResp)
	if delResp["status"] != "deleted" || delResp["id"] != job1.Config.ID {
		t.Errorf("unexpected delete response: %+v", delResp)
	}

	// 2. Verify job1 is gone and job2 remains
	if _, exists := m.GetJob(job1.Config.ID); exists {
		t.Errorf("job1 still exists after deletion")
	}
	if _, exists := m.GetJob(job2.Config.ID); !exists {
		t.Errorf("job2 was unexpectedly deleted")
	}

	// 3. Delete non-existent job returns 404
	req404, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/jobs/non_existent_id", nil)
	resp404, err := client.Do(req404)
	if err != nil {
		t.Fatalf("failed request 404: %v", err)
	}
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for non-existent job, got %d", resp404.StatusCode)
	}

	// 4. Delete with trailing slash
	reqSlash, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/jobs/"+job2.Config.ID+"/", nil)
	respSlash, err := client.Do(reqSlash)
	if err != nil {
		t.Fatalf("failed request with trailing slash: %v", err)
	}
	defer respSlash.Body.Close()
	if respSlash.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for trailing slash delete, got %d", respSlash.StatusCode)
	}
}

func TestAPI_WipeData(t *testing.T) {
	ts, m, tempDir := setupTestServer(t)
	defer ts.Close()
	defer os.RemoveAll(tempDir)

	m.CreateJob(model.JobConfig{Target: "toko A"})
	m.CreateJob(model.JobConfig{Target: "toko B"})

	resp, err := http.Post(ts.URL+"/api/system/wipe", "application/json", nil)
	if err != nil {
		t.Fatalf("wipe request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for wipe, got %d", resp.StatusCode)
	}

	if len(m.GetAllJobs()) != 0 {
		t.Errorf("expected 0 jobs after wipe, got %d", len(m.GetAllJobs()))
	}
}


