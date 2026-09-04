package checkpoint_test

import (
	"os"
	"path/filepath"
	"testing"

	"maps-scraper/pkg/checkpoint"
)

func TestCheckpoint_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "checkpoint_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cpFile := filepath.Join(tempDir, "state.json")
	queries := []string{"Q1", "Q2", "Q3"}

	// 1. Create new checkpoint
	cm, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 3, "forward")
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	cm.SetMetadata("job_surabaya", "Surabaya, Jawa Timur")

	cm.MarkRunning(1, 1)
	if rec, ok := cm.GetRecord(1); !ok || rec.Status != checkpoint.StateRunning {
		t.Errorf("expected Q1 to be RUNNING")
	}

	cm.MarkSuccess(1, 10)
	cm.MarkFailed(2, "timeout", true)

	data := cm.GetData()
	if data.CompletedCount != 1 || data.FailedCount != 1 || data.StoresDiscovered != 10 {
		t.Errorf("unexpected counts: %+v", data)
	}
	if data.JobID != "job_surabaya" || data.Location != "Surabaya, Jawa Timur" {
		t.Errorf("metadata mismatch: %+v", data)
	}

	// 2. Load checkpoint
	cm2, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 3, "forward")
	if err != nil {
		t.Fatalf("failed to load manager: %v", err)
	}

	// Verify loaded state
	data2 := cm2.GetData()
	if data2.CompletedCount != 1 || data2.FailedCount != 1 {
		t.Errorf("loaded unexpected counts: %+v", data2)
	}

	rec2, _ := cm2.GetRecord(2)
	if rec2.Status != checkpoint.StateFailedPermanent {
		t.Errorf("expected Q2 to be FailedPermanent, got %s", rec2.Status)
	}

	// Test remaining count
	rem := cm2.GetRemainingQueryCount(1, 3)
	if rem != 1 { // Q3 is the only remaining PENDING query
		t.Errorf("expected 1 remaining query, got %d", rem)
	}
}

func TestCheckpoint_ResetRunningOnRestart(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "checkpoint_test")
	defer os.RemoveAll(tempDir)
	cpFile := filepath.Join(tempDir, "restart.json")
	queries := []string{"Q1", "Q2", "Q3"}

	cm, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 3, "forward")
	if err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	cm.MarkRunning(1, 1)
	cm.MarkRunning(2, 2)
	cm.MarkSuccess(3, 5)

	// Simulate sudden restart - new manager loading existing file
	cmRestart, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 3, "forward")
	if err != nil {
		t.Fatalf("failed to reload after restart: %v", err)
	}

	rec1, _ := cmRestart.GetRecord(1)
	rec2, _ := cmRestart.GetRecord(2)
	rec3, _ := cmRestart.GetRecord(3)

	if rec1.Status != checkpoint.StatePending {
		t.Errorf("expected Q1 to be reset to PENDING, got %s", rec1.Status)
	}
	if rec2.Status != checkpoint.StatePending {
		t.Errorf("expected Q2 to be reset to PENDING, got %s", rec2.Status)
	}
	if rec3.Status != checkpoint.StateSuccess {
		t.Errorf("expected Q3 to remain SUCCESS, got %s", rec3.Status)
	}
}

func TestCheckpoint_Corruption(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "checkpoint_test")
	defer os.RemoveAll(tempDir)
	cpFile := filepath.Join(tempDir, "corrupt.json")

	_ = os.WriteFile(cpFile, []byte("{invalid json"), 0644)

	queries := []string{"Q1"}
	_, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 1, "forward")
	if err == nil {
		t.Errorf("expected error when loading corrupt checkpoint")
	}
}
