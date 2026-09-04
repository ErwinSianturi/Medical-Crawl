package job_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maps-scraper/pkg/checkpoint"
	"maps-scraper/pkg/job"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/parallel"
)

func setupTestManager(t *testing.T) (*job.Manager, string) {
	tempDir, err := os.MkdirTemp("", "jobmanager_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dataDir := filepath.Join(tempDir, "data")
	logsDir := filepath.Join(tempDir, "logs")

	m, err := job.NewManager(dataDir, logsDir)
	if err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	return m, tempDir
}

func TestManager_CreateJob(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	cfg := model.JobConfig{
		Target:        "toko bangunan",
		Location:      model.LocationConfig{Province: "Jawa Timur", City: "Surabaya"},
		Workers:       3,
		TargetRecords: 50,
		Headless:      true,
		Queries:       []string{"toko test"},
	}

	state, err := m.CreateJob(cfg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if state.Status != model.StatusQueued {
		t.Errorf("Expected status Queued, got %s", state.Status)
	}
	if len(state.Workers) != 3 {
		t.Errorf("Expected 3 workers, got %d", len(state.Workers))
	}
	if state.Config.ID == "" {
		t.Errorf("Expected auto-generated Job ID")
	}

	// Verify retrieval
	fetched, exists := m.GetJob(state.Config.ID)
	if !exists {
		t.Errorf("Job should exist in manager")
	}
	if fetched.Config.ID != state.Config.ID {
		t.Errorf("Fetched job ID mismatch")
	}
}

func TestManager_InvalidJobOperations(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	err := m.StartJob("non_existent")
	if err == nil {
		t.Errorf("Expected error starting non-existent job")
	}

	err = m.PauseJob("non_existent")
	if err == nil {
		t.Errorf("Expected error pausing non-existent job")
	}
}

func TestManager_StateTransitions(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	cfg := model.JobConfig{Target: "test", Headless: true, Queries: []string{"test_query"}}
	state, _ := m.CreateJob(cfg)

	if state.Status != model.StatusQueued {
		t.Fatalf("Initial status should be queued")
	}
}

func TestManager_RegionIsolation(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	// Create Job A in Surabaya
	jobA, err := m.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{Province: "Jawa Timur", City: "Surabaya"},
		Queries:  []string{"toko bangunan surabaya 1", "toko bangunan surabaya 2"},
		Workers:  2,
	})
	if err != nil {
		t.Fatalf("failed to create Job A: %v", err)
	}

	// Create Job B in Sidoarjo
	jobB, err := m.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{Province: "Jawa Timur", City: "Sidoarjo"},
		Queries:  []string{"toko bangunan sidoarjo 1", "toko bangunan sidoarjo 2"},
		Workers:  2,
	})
	if err != nil {
		t.Fatalf("failed to create Job B: %v", err)
	}

	if jobA.Config.ID == jobB.Config.ID {
		t.Fatalf("Job IDs must be unique: %s vs %s", jobA.Config.ID, jobB.Config.ID)
	}

	// Verify checkpoint file paths are isolated
	cpA := filepath.Join(tempDir, "data", "checkpoint_"+jobA.Config.ID+".json")
	cpB := filepath.Join(tempDir, "data", "checkpoint_"+jobB.Config.ID+".json")

	// Manually initialize checkpoint A
	cmA, err := checkpoint.NewCheckpointManager(cpA, jobA.Config.Queries, 1, 2, "reverse")
	if err != nil {
		t.Fatalf("failed to create cmA: %v", err)
	}
	cmA.SetMetadata(jobA.Config.ID, "Surabaya")
	cmA.MarkSuccess(1, 15) // Job A completed query 1

	// Manually initialize checkpoint B
	cmB, err := checkpoint.NewCheckpointManager(cpB, jobB.Config.Queries, 1, 2, "reverse")
	if err != nil {
		t.Fatalf("failed to create cmB: %v", err)
	}
	cmB.SetMetadata(jobB.Config.ID, "Sidoarjo")
	cmB.MarkSuccess(2, 8) // Job B completed query 2

	// Check that Job A checkpoint was not affected by Job B
	dataA := cmA.GetData()
	dataB := cmB.GetData()

	if dataA.StoresDiscovered != 15 || dataA.CompletedCount != 1 {
		t.Errorf("Job A state contaminated: %+v", dataA)
	}
	if dataB.StoresDiscovered != 8 || dataB.CompletedCount != 1 {
		t.Errorf("Job B state contaminated: %+v", dataB)
	}

	// Verify query records isolation
	recA1, _ := cmA.GetRecord(1)
	recA2, _ := cmA.GetRecord(2)
	if recA1.Status != checkpoint.StateSuccess || recA2.Status != checkpoint.StatePending {
		t.Errorf("Job A query statuses incorrect: Q1=%s, Q2=%s", recA1.Status, recA2.Status)
	}

	recB1, _ := cmB.GetRecord(1)
	recB2, _ := cmB.GetRecord(2)
	if recB1.Status != checkpoint.StatePending || recB2.Status != checkpoint.StateSuccess {
		t.Errorf("Job B query statuses incorrect: Q1=%s, Q2=%s", recB1.Status, recB2.Status)
	}
}

func TestManager_ResumeAndSkipCompletedQueries(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "resume_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cpFile := filepath.Join(tempDir, "checkpoint_test_job.json")
	queries := []string{"Q1", "Q2", "Q3", "Q4", "Q5"}

	// Step 1: Initialize checkpoint and simulate partial completion
	cm, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 5, "reverse")
	if err != nil {
		t.Fatalf("failed to init checkpoint: %v", err)
	}

	// Q5 and Q4 finished
	cm.MarkSuccess(5, 12)
	cm.MarkSuccess(4, 8)
	// Q3 was running when interrupted
	cm.MarkRunning(3, 1)

	// Step 2: Simulate resume / reload after restart
	cmResume, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 5, "reverse")
	if err != nil {
		t.Fatalf("failed to reload checkpoint: %v", err)
	}

	// Q3 should be reset to PENDING
	rec3, _ := cmResume.GetRecord(3)
	if rec3.Status != checkpoint.StatePending {
		t.Errorf("expected Q3 to be PENDING on resume, got %s", rec3.Status)
	}

	// Q4 and Q5 must remain SUCCESS
	rec4, _ := cmResume.GetRecord(4)
	rec5, _ := cmResume.GetRecord(5)
	if rec4.Status != checkpoint.StateSuccess || rec5.Status != checkpoint.StateSuccess {
		t.Errorf("expected Q4 & Q5 to remain SUCCESS: Q4=%s, Q5=%s", rec4.Status, rec5.Status)
	}

	// Remaining queries to process should be exactly 3 (Q1, Q2, Q3)
	remaining := cmResume.GetRemainingQueryCount(1, 5)
	if remaining != 3 {
		t.Errorf("expected 3 remaining queries, got %d", remaining)
	}
}

func TestManager_BackendRestartRecovery(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "restart_mgr_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	logsDir := filepath.Join(tempDir, "logs")

	// 1. First server run
	m1, err := job.NewManager(dataDir, logsDir)
	if err != nil {
		t.Fatalf("failed to init m1: %v", err)
	}

	jobState, _ := m1.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{City: "Surabaya"},
		Queries:  []string{"Q1", "Q2"},
	})

	// Add an iteration to the job and mark it RUNNING
	iter := model.IterationState{
		ID:         "iter_001",
		JobID:      jobState.Config.ID,
		Number:     1,
		Status:     model.IterationRunning,
		StartQuery: 2,
		EndQuery:   1,
	}
	jobState.Iterations = append(jobState.Iterations, iter)
	jobState.ActiveIterationID = "iter_001"
	jobState.Status = model.StatusRunning

	// Simulate sudden server shutdown (save history as is)
	historyFile := filepath.Join(dataDir, "history.json")
	historyMap := map[string]*model.JobState{
		jobState.Config.ID: jobState,
	}
	historyBytes, _ := json.Marshal(historyMap)
	_ = os.WriteFile(historyFile, historyBytes, 0644)

	// 2. Second server run (Simulating restart)
	m2, err := job.NewManager(dataDir, logsDir)
	if err != nil {
		t.Fatalf("failed to init m2: %v", err)
	}

	recoveredJob, exists := m2.GetJob(jobState.Config.ID)
	if !exists {
		t.Fatalf("job not recovered after restart")
	}

	if recoveredJob.Status != model.StatusStopped {
		t.Errorf("expected running job status to become STOPPED after restart, got %s", recoveredJob.Status)
	}

	if recoveredJob.ActiveIterationID != "" {
		t.Errorf("active iteration ID should be cleared on restart")
	}

	if len(recoveredJob.Iterations) != 1 || recoveredJob.Iterations[0].Status != model.IterationInterrupted {
		t.Errorf("expected active iteration to become INTERRUPTED, got %+v", recoveredJob.Iterations)
	}
}

func TestManager_ConcurrentJobsIsolation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "concurrent_jobs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	logsDir := filepath.Join(tempDir, "logs")

	m, err := job.NewManager(dataDir, logsDir)
	if err != nil {
		t.Fatalf("failed to init manager: %v", err)
	}

	// 1. Create Job A (Surabaya)
	jobA, _ := m.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{City: "Surabaya"},
		Queries:  []string{"surabaya_q1", "surabaya_q2"},
		Workers:  2,
	})

	// 2. Create Job B (Sidoarjo)
	jobB, _ := m.CreateJob(model.JobConfig{
		Target:   "toko semen",
		Location: model.LocationConfig{City: "Sidoarjo"},
		Queries:  []string{"sidoarjo_q1", "sidoarjo_q2"},
		Workers:  2,
	})

	// 3. Create parallel StorageManagers for Job A & Job B
	outDirA := filepath.Join(dataDir, "exports", jobA.Config.ID)
	outDirB := filepath.Join(dataDir, "exports", jobB.Config.ID)
	csvA := filepath.Join(dataDir, "exports", jobA.Config.ID+".csv")
	csvB := filepath.Join(dataDir, "exports", jobB.Config.ID+".csv")

	smA, err := parallel.NewStorageManager(outDirA, csvA)
	if err != nil {
		t.Fatalf("failed to create storage A: %v", err)
	}
	defer smA.Close()

	smB, err := parallel.NewStorageManager(outDirB, csvB)
	if err != nil {
		t.Fatalf("failed to create storage B: %v", err)
	}
	defer smB.Close()

	// Simulate concurrent saving
	recA := &model.StoreRecord{PlaceID: "ChIJ_Surabaya_1", Name: "TB Surabaya Jaya", Address: "Jl. Pemuda No 1", City: "Surabaya"}
	recB := &model.StoreRecord{PlaceID: "ChIJ_Sidoarjo_1", Name: "TB Sidoarjo Makmur", Address: "Jl. Ahmad Yani No 1", City: "Sidoarjo"}

	savedA, errA := smA.CheckAndSaveRecord(1, recA)
	savedB, errB := smB.CheckAndSaveRecord(1, recB)

	if !savedA || errA != nil {
		t.Errorf("failed to save to Job A: %v", errA)
	}
	if !savedB || errB != nil {
		t.Errorf("failed to save to Job B: %v", errB)
	}

	// Verify total stores saved in memory
	if smA.GetTotalSaved() != 1 {
		t.Errorf("expected 1 record in smA, got %d", smA.GetTotalSaved())
	}
	if smB.GetTotalSaved() != 1 {
		t.Errorf("expected 1 record in smB, got %d", smB.GetTotalSaved())
	}
}

func TestManager_StartIteration_SimplifiedTargetStores(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	jobState, err := m.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{City: "Surabaya"},
		Queries:  []string{"q1", "q2", "q3"},
	})
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// Start iteration requesting only target_stores (no strategy specified)
	iterReq := model.IterationState{
		TargetStores: 10,
	}

	iter, err := m.StartIteration(jobState.Config.ID, iterReq)
	if err != nil {
		t.Fatalf("failed to start simplified iteration: %v", err)
	}

	if iter.TargetStores != 10 {
		t.Errorf("expected TargetStores=10, got %d", iter.TargetStores)
	}
	if iter.Status != model.IterationRunning {
		t.Errorf("expected status RUNNING, got %s", iter.Status)
	}
}

func TestManager_ExportRegionAndAllData(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	// Create Job A (Surabaya)
	jobA, _ := m.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{City: "Surabaya"},
		Queries:  []string{"q1"},
	})

	// Create Job B (Sidoarjo)
	jobB, _ := m.CreateJob(model.JobConfig{
		Target:   "toko semen",
		Location: model.LocationConfig{City: "Sidoarjo"},
		Queries:  []string{"q2"},
	})

	// Inject records into Job A CSV
	exportDirA := filepath.Join(tempDir, "data", "exports")
	_ = os.MkdirAll(exportDirA, 0755)
	csvA := filepath.Join(exportDirA, jobA.Config.ID+".csv")
	csvB := filepath.Join(exportDirA, jobB.Config.ID+".csv")

	fA, _ := os.Create(csvA)
	wA := csv.NewWriter(fA)
	wA.Comma = ';'
	_ = wA.Write(model.Headers())
	_ = wA.Write((&model.StoreRecord{PlaceID: "ChIJ_SBY_1", Name: "TB Surabaya 1", Address: "Jl. Pemuda", City: "Surabaya"}).ToRow())
	_ = wA.Write((&model.StoreRecord{PlaceID: "ChIJ_SBY_2", Name: "TB Surabaya 2", Address: "Jl. Basuki Rahmat", City: "Surabaya"}).ToRow())
	wA.Flush()
	fA.Close()

	fB, _ := os.Create(csvB)
	wB := csv.NewWriter(fB)
	wB.Comma = ';'
	_ = wB.Write(model.Headers())
	_ = wB.Write((&model.StoreRecord{PlaceID: "ChIJ_SDA_1", Name: "TB Sidoarjo 1", Address: "Jl. Pahlawan", City: "Sidoarjo"}).ToRow())
	wB.Flush()
	fB.Close()

	// Test 1: Export Region A only (Surabaya)
	contentA, filenameA, _, err := m.ExportData(jobA.Config.ID, "csv", false)
	if err != nil {
		t.Fatalf("failed to export Region A: %v", err)
	}
	if !strings.Contains(filenameA, "surabaya") {
		t.Errorf("expected filename to contain surabaya, got %s", filenameA)
	}
	strA := string(contentA)
	if !strings.Contains(strA, "TB Surabaya 1") || strings.Contains(strA, "TB Sidoarjo 1") {
		t.Errorf("Region A export contained contaminated data: %s", strA)
	}

	// Test 2: Export All Data combined
	contentAll, filenameAll, _, err := m.ExportAllData("csv", false)
	if err != nil {
		t.Fatalf("failed to export All Data: %v", err)
	}
	if !strings.Contains(filenameAll, "scrape_all_regions_combined") {
		t.Errorf("expected combined filename, got %s", filenameAll)
	}
	strAll := string(contentAll)
	if !strings.Contains(strAll, "TB Surabaya 1") || !strings.Contains(strAll, "TB Sidoarjo 1") {
		t.Errorf("All Data export missed records: %s", strAll)
	}
}

func TestManager_DeleteIndividualJob_DataLifecycle(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	// Create 3 separate jobs
	job1, err := m.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{Province: "Jawa Timur", City: "Surabaya"},
		Queries:  []string{"toko surabaya"},
		Workers:  2,
	})
	if err != nil {
		t.Fatalf("failed to create Job 1: %v", err)
	}

	job2, err := m.CreateJob(model.JobConfig{
		Target:   "toko cat",
		Location: model.LocationConfig{Province: "Jawa Timur", City: "Sidoarjo"},
		Queries:  []string{"toko cat sidoarjo"},
		Workers:  2,
	})
	if err != nil {
		t.Fatalf("failed to create Job 2: %v", err)
	}

	job3, err := m.CreateJob(model.JobConfig{
		Target:   "toko semen",
		Location: model.LocationConfig{Province: "Jawa Timur", City: "Gresik"},
		Queries:  []string{"toko semen gresik"},
		Workers:  2,
	})
	if err != nil {
		t.Fatalf("failed to create Job 3: %v", err)
	}

	// Create mock checkpoint and export files for all 3 jobs
	exportDir := filepath.Join(tempDir, "data", "exports")
	_ = os.MkdirAll(exportDir, 0755)

	cp1 := filepath.Join(tempDir, "data", "checkpoint_"+job1.Config.ID+".json")
	cp2 := filepath.Join(tempDir, "data", "checkpoint_"+job2.Config.ID+".json")
	cp3 := filepath.Join(tempDir, "data", "checkpoint_"+job3.Config.ID+".json")
	_ = os.WriteFile(cp1, []byte(`{"id":"1"}`), 0644)
	_ = os.WriteFile(cp2, []byte(`{"id":"2"}`), 0644)
	_ = os.WriteFile(cp3, []byte(`{"id":"3"}`), 0644)

	csv1 := filepath.Join(exportDir, job1.Config.ID+".csv")
	csv2 := filepath.Join(exportDir, job2.Config.ID+".csv")
	csv3 := filepath.Join(exportDir, job3.Config.ID+".csv")
	_ = os.WriteFile(csv1, []byte("Name;Surabaya"), 0644)
	_ = os.WriteFile(csv2, []byte("Name;Sidoarjo"), 0644)
	_ = os.WriteFile(csv3, []byte("Name;Gresik"), 0644)

	// Now delete ONLY Job 2
	if err := m.DeleteJob(job2.Config.ID); err != nil {
		t.Fatalf("failed to delete job 2: %v", err)
	}

	// 1. Verify Job 2 is gone from memory
	if _, exists := m.GetJob(job2.Config.ID); exists {
		t.Errorf("Job 2 should not exist in manager memory")
	}

	// 2. Verify Job 1 and Job 3 STILL EXIST and are untouched
	if j1, exists := m.GetJob(job1.Config.ID); !exists || j1.Config.Location.City != "Surabaya" {
		t.Errorf("Job 1 was altered or deleted unintentionally: %+v", j1)
	}
	if j3, exists := m.GetJob(job3.Config.ID); !exists || j3.Config.Location.City != "Gresik" {
		t.Errorf("Job 3 was altered or deleted unintentionally: %+v", j3)
	}

	// 3. Verify Job 2 files are gone
	if _, err := os.Stat(cp2); !os.IsNotExist(err) {
		t.Errorf("Job 2 checkpoint file still exists: %s", cp2)
	}
	if _, err := os.Stat(csv2); !os.IsNotExist(err) {
		t.Errorf("Job 2 CSV export still exists: %s", csv2)
	}

	// 4. Verify Job 1 and Job 3 files remain intact
	if _, err := os.Stat(cp1); os.IsNotExist(err) {
		t.Errorf("Job 1 checkpoint file was deleted unexpectedly!")
	}
	if _, err := os.Stat(csv1); os.IsNotExist(err) {
		t.Errorf("Job 1 CSV export file was deleted unexpectedly!")
	}
	if _, err := os.Stat(cp3); os.IsNotExist(err) {
		t.Errorf("Job 3 checkpoint file was deleted unexpectedly!")
	}
	if _, err := os.Stat(csv3); os.IsNotExist(err) {
		t.Errorf("Job 3 CSV export file was deleted unexpectedly!")
	}

	// 5. Test Manager restart persistence - load new manager from same dataDir
	m2, err := job.NewManager(filepath.Join(tempDir, "data"), filepath.Join(tempDir, "logs"))
	if err != nil {
		t.Fatalf("failed to reload manager: %v", err)
	}
	if _, exists := m2.GetJob(job2.Config.ID); exists {
		t.Errorf("Job 2 re-appeared after manager reload!")
	}
	if _, exists := m2.GetJob(job1.Config.ID); !exists {
		t.Errorf("Job 1 missing after manager reload!")
	}
	if _, exists := m2.GetJob(job3.Config.ID); !exists {
		t.Errorf("Job 3 missing after manager reload!")
	}
}

func TestManager_DeleteIndividualJob_RunningProtection(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	job1, err := m.CreateJob(model.JobConfig{
		Target:   "toko bangunan",
		Location: model.LocationConfig{Province: "Jawa Timur", City: "Surabaya"},
		Queries:  []string{"toko surabaya"},
		Workers:  1,
	})
	if err != nil {
		t.Fatalf("failed to create Job 1: %v", err)
	}

	// Start the job so it transitions to running
	if err := m.StartJob(job1.Config.ID); err != nil {
		t.Fatalf("failed to start job: %v", err)
	}

	// Attempt deletion should fail because job is running
	err = m.DeleteJob(job1.Config.ID)
	if err == nil {
		t.Fatalf("expected error deleting running job, got nil")
	}
	if !strings.Contains(err.Error(), "Stop it first") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Stop the job
	_ = m.StopJob(job1.Config.ID)

	// Now deletion should succeed
	if err := m.DeleteJob(job1.Config.ID); err != nil {
		t.Errorf("expected stopped job to be deleted successfully, got: %v", err)
	}

	// Check job no longer exists
	if _, exists := m.GetJob(job1.Config.ID); exists {
		t.Errorf("deleted job still exists")
	}
}

func TestManager_DeleteJob_SecurityAndEdgeCases(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	// 1. Empty ID
	if err := m.DeleteJob(""); err == nil {
		t.Error("expected error for empty job ID")
	}

	// 2. Whitespace ID
	if err := m.DeleteJob("   "); err == nil {
		t.Error("expected error for whitespace job ID")
	}

	// 3. Path traversal ID
	if err := m.DeleteJob("../some_file"); err == nil {
		t.Error("expected error for path traversal job ID")
	}
	if err := m.DeleteJob("sub/dir/job"); err == nil {
		t.Error("expected error for slash in job ID")
	}

	// 4. Non-existent ID
	if err := m.DeleteJob("non_existent_job_999"); err == nil {
		t.Error("expected error for non-existent job ID")
	} else if !strings.Contains(err.Error(), "job not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManager_GetAllJobs_DeterministicSorting(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	// Create 5 jobs with distinct timestamps
	for i := 1; i <= 5; i++ {
		_, err := m.CreateJob(model.JobConfig{
			ID:        fmt.Sprintf("job_test_%02d", i),
			Title:     fmt.Sprintf("Job %d", i),
			Target:    "toko",
			CreatedAt: fmt.Sprintf("2026-09-04T12:00:%02dZ", i),
		})
		if err != nil {
			t.Fatalf("failed to create job %d: %v", i, err)
		}
	}

	// GetAllJobs should return newest first: job_test_05 down to job_test_01
	jobs := m.GetAllJobs()
	if len(jobs) != 5 {
		t.Fatalf("expected 5 jobs, got %d", len(jobs))
	}

	for i, j := range jobs {
		expectedID := fmt.Sprintf("job_test_%02d", 5-i)
		if j.Config.ID != expectedID {
			t.Errorf("at index %d: expected %s, got %s", i, expectedID, j.Config.ID)
		}
	}
}

func TestManager_SequentialIndividualDeletions(t *testing.T) {
	m, tempDir := setupTestManager(t)
	defer os.RemoveAll(tempDir)

	job1, _ := m.CreateJob(model.JobConfig{ID: "job_seq_1", Target: "toko 1"})
	job2, _ := m.CreateJob(model.JobConfig{ID: "job_seq_2", Target: "toko 2"})
	job3, _ := m.CreateJob(model.JobConfig{ID: "job_seq_3", Target: "toko 3"})

	// Delete first
	if err := m.DeleteJob(job1.Config.ID); err != nil {
		t.Fatalf("failed to delete job 1: %v", err)
	}
	if len(m.GetAllJobs()) != 2 {
		t.Fatalf("expected 2 jobs remaining, got %d", len(m.GetAllJobs()))
	}

	// Delete last
	if err := m.DeleteJob(job3.Config.ID); err != nil {
		t.Fatalf("failed to delete job 3: %v", err)
	}
	remaining := m.GetAllJobs()
	if len(remaining) != 1 || remaining[0].Config.ID != job2.Config.ID {
		t.Fatalf("expected only job 2 remaining, got %+v", remaining)
	}

	// Delete middle / only remaining
	if err := m.DeleteJob(job2.Config.ID); err != nil {
		t.Fatalf("failed to delete job 2: %v", err)
	}
	if len(m.GetAllJobs()) != 0 {
		t.Fatalf("expected 0 jobs remaining, got %d", len(m.GetAllJobs()))
	}
}


