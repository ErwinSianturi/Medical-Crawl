package crawler_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
)

func TestRunStorage_LifecycleAndOrdering(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "run_storage_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "runs.json")
	rs := crawler.NewRunStorage(storagePath)

	// 1. Create Run 1
	run1, err := rs.CreateRun("Halodoc", "https://www.halodoc.com/artikel", "All Topics", 20, "manual")
	if err != nil {
		t.Fatalf("failed to create run 1: %v", err)
	}
	if run1.RunNumber != 1 {
		t.Errorf("expected run 1 to have number 1, got %d", run1.RunNumber)
	}
	if run1.DisplayName != "Run #001" {
		t.Errorf("expected Run #001, got %s", run1.DisplayName)
	}

	// 2. Create Run 2 on same day (different time/id)
	run2, err := rs.CreateRun("Halodoc", "https://www.halodoc.com/artikel", "Diabetes", 10, "cron")
	if err != nil {
		t.Fatalf("failed to create run 2: %v", err)
	}
	if run2.RunNumber != 2 {
		t.Errorf("expected run 2 to have number 2, got %d", run2.RunNumber)
	}
	if run2.DisplayName != "Run #002" {
		t.Errorf("expected Run #002, got %s", run2.DisplayName)
	}
	if run1.ID == run2.ID {
		t.Errorf("expected distinct Run IDs on same day, got identical: %s", run1.ID)
	}

	// 3. Verify ordering (latest first)
	all := rs.GetAllRuns()
	if len(all) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(all))
	}
	if all[0].ID != run2.ID {
		t.Errorf("expected newest run (Run 2) first, got %s", all[0].ID)
	}

	// 4. Update Run 2 to Completed
	now := time.Now()
	run2.Status = model.RunStatusCompleted
	run2.FinishedAt = &now
	run2.TotalCrawled = 15
	run2.TotalSuccess = 10
	run2.TotalNew = 8
	run2.TotalDuplicate = 2
	if err := rs.UpdateRun(run2); err != nil {
		t.Fatalf("failed to update run 2: %v", err)
	}

	// 5. Reload from disk to verify persistence
	rs2 := crawler.NewRunStorage(storagePath)
	reloaded, found := rs2.GetRun(run2.ID)
	if !found {
		t.Fatalf("run 2 not found after reload")
	}
	if reloaded.Status != model.RunStatusCompleted {
		t.Errorf("expected reloaded status COMPLETED, got %s", reloaded.Status)
	}
	if reloaded.TotalSuccess != 10 || reloaded.TotalNew != 8 {
		t.Errorf("reloaded metrics mismatch: success=%d, new=%d", reloaded.TotalSuccess, reloaded.TotalNew)
	}

	// 6. Verify date querying
	todayStr := time.Now().Format("2006-01-02")
	byDate := rs2.GetRunsByDate(todayStr)
	if len(byDate) != 2 {
		t.Errorf("expected 2 runs for date %s, got %d", todayStr, len(byDate))
	}

	days := rs2.GetRunDates()
	if len(days) < 1 {
		t.Errorf("expected at least 1 date in GetRunDates, got %d", len(days))
	}
	if days[0].Date != todayStr {
		t.Errorf("expected date %s, got %s", todayStr, days[0].Date)
	}
}

func TestRunStorage_DeleteRun(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "run_storage_del_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "runs.json")
	rs := crawler.NewRunStorage(storagePath)

	// Create Run A, Run B, Run C
	runA, err := rs.CreateRun("Halodoc", "https://www.halodoc.com/artikel", "Topic A", 10, "manual")
	if err != nil {
		t.Fatalf("failed to create run A: %v", err)
	}
	runB, err := rs.CreateRun("Halodoc", "https://www.halodoc.com/artikel", "Topic B", 10, "manual")
	if err != nil {
		t.Fatalf("failed to create run B: %v", err)
	}
	runC, err := rs.CreateRun("Halodoc", "https://www.halodoc.com/artikel", "Topic C", 10, "manual")
	if err != nil {
		t.Fatalf("failed to create run C: %v", err)
	}

	// Link articles
	_ = rs.RecordArticleRun(runA.ID, "art_a1")
	_ = rs.RecordArticleRun(runB.ID, "art_b1")
	_ = rs.RecordArticleRun(runC.ID, "art_c1")

	// Delete Run B
	deleted, err := rs.DeleteRun(runB.ID)
	if err != nil {
		t.Fatalf("failed to delete run B: %v", err)
	}
	if !deleted {
		t.Errorf("expected deleted=true for run B")
	}

	// Verify Run A and Run C remain, Run B is gone
	if _, found := rs.GetRun(runB.ID); found {
		t.Errorf("expected run B to be deleted")
	}
	if _, found := rs.GetRun(runA.ID); !found {
		t.Errorf("expected run A to still exist")
	}
	if _, found := rs.GetRun(runC.ID); !found {
		t.Errorf("expected run C to still exist")
	}

	// Verify article mapping cleanup
	if rs.GetRunForArticle("art_b1") != "" {
		t.Errorf("expected art_b1 mapping to be cleaned up, got %s", rs.GetRunForArticle("art_b1"))
	}
	if rs.GetRunForArticle("art_a1") != runA.ID {
		t.Errorf("expected art_a1 mapping to be intact")
	}
	if rs.GetRunForArticle("art_c1") != runC.ID {
		t.Errorf("expected art_c1 mapping to be intact")
	}

	// Delete non-existent run ID -> should return (false, nil)
	deletedBad, err := rs.DeleteRun("run_non_existent")
	if err != nil {
		t.Errorf("unexpected error on deleting non-existent run: %v", err)
	}
	if deletedBad {
		t.Errorf("expected deleted=false for non-existent run")
	}

	// Reload from disk to verify persistence of deletion
	rs2 := crawler.NewRunStorage(storagePath)
	allRuns := rs2.GetAllRuns()
	if len(allRuns) != 2 {
		t.Errorf("expected 2 runs after reload, got %d", len(allRuns))
	}
}


