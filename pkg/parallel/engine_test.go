package parallel_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"maps-scraper/pkg/checkpoint"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/parallel"
)

// Test5WorkerDistribution verifies that 5 workers consume tasks from a shared pool
// without duplication, in a thread-safe manner.
func Test5WorkerDistribution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "worker_dist_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	const totalQueries = 2500
	const numWorkers = 5

	queries := make([]string, totalQueries)
	for i := 0; i < totalQueries; i++ {
		queries[i] = fmt.Sprintf("toko bangunan district_%d surabaya", i+1)
	}

	cpFile := filepath.Join(tempDir, "state.json")
	cm, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, totalQueries, "forward")
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	taskQueue := make(chan int, totalQueries)
	for id := 1; id <= totalQueries; id++ {
		taskQueue <- id
	}
	close(taskQueue)

	var (
		wg              sync.WaitGroup
		processedMu     sync.Mutex
		processedTasks  = make(map[int]int) // taskID -> workerID
		workerCounts    = make([]int, numWorkers+1)
		duplicateCount  int32
	)

	for w := 1; w <= numWorkers; w++ {
		workerID := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for taskID := range taskQueue {
				cm.MarkRunning(taskID, workerID)
				
				processedMu.Lock()
				if _, exists := processedTasks[taskID]; exists {
					atomic.AddInt32(&duplicateCount, 1)
				} else {
					processedTasks[taskID] = workerID
					workerCounts[workerID]++
				}
				processedMu.Unlock()

				// Simulate processing
				cm.MarkSuccess(taskID, 2)
			}
		}()
	}

	wg.Wait()

	if duplicateCount > 0 {
		t.Fatalf("Detected %d duplicate query executions across workers!", duplicateCount)
	}

	if len(processedTasks) != totalQueries {
		t.Fatalf("Expected %d processed queries, got %d", totalQueries, len(processedTasks))
	}

	// Verify all 5 workers contributed evenly
	for w := 1; w <= numWorkers; w++ {
		if workerCounts[w] == 0 {
			t.Errorf("Worker %d did not process any tasks", w)
		}
	}

	data := cm.GetData()
	if data.CompletedCount != totalQueries {
		t.Errorf("Expected checkpoint completed count %d, got %d", totalQueries, data.CompletedCount)
	}
}

// TestWorkerFailureRetry verifies that when a worker fails on a query,
// it is retried up to MaxRetries before becoming permanently failed.
func TestWorkerFailureRetry(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "retry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	queries := []string{"Q1_failing"}
	cpFile := filepath.Join(tempDir, "state.json")
	cm, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 1, "forward")
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	const maxRetries = 3
	taskQueue := make(chan int, 10)
	taskQueue <- 1

	attempts := 0
	for taskID := range taskQueue {
		attempts++
		cm.MarkRunning(taskID, 1)
		if attempts < maxRetries {
			cm.MarkFailed(taskID, "transient error", false)
			taskQueue <- taskID // requeue
		} else {
			cm.MarkFailed(taskID, "fatal error", true)
			close(taskQueue)
		}
	}

	rec, ok := cm.GetRecord(1)
	if !ok {
		t.Fatalf("task 1 not found in checkpoint")
	}

	if rec.Status != checkpoint.StateFailedPermanent {
		t.Errorf("expected final status %s, got %s", checkpoint.StateFailedPermanent, rec.Status)
	}
	if rec.Attempts != maxRetries {
		t.Errorf("expected %d attempts, got %d", maxRetries, rec.Attempts)
	}
}

// TestResumeSkipsCompletedQueries verifies that resuming a session does NOT repeat already SUCCESS queries.
func TestResumeSkipsCompletedQueries(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "resume_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	queries := []string{"Q1", "Q2", "Q3", "Q4", "Q5"}
	cpFile := filepath.Join(tempDir, "state.json")
	cm, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 5, "forward")
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	// Q1 and Q2 were completed in previous run
	cm.MarkSuccess(1, 10)
	cm.MarkSuccess(2, 5)
	// Q3 was permanently failed
	cm.MarkFailed(3, "bad data", true)
	// Q4 was running (interrupted)
	cm.MarkRunning(4, 1)

	// Simulate resumption: new manager loads file
	cmResumed, err := checkpoint.NewCheckpointManager(cpFile, queries, 1, 5, "forward")
	if err != nil {
		t.Fatalf("failed to load checkpoint manager on resume: %v", err)
	}

	// Collect tasks to run
	var queuedForRun []int
	for id := 1; id <= 5; id++ {
		rec, exists := cmResumed.GetRecord(id)
		if !exists || (rec.Status != checkpoint.StateSuccess && rec.Status != checkpoint.StateFailedPermanent) {
			queuedForRun = append(queuedForRun, id)
		}
	}

	// Only Q4 (interrupted -> reset to pending) and Q5 (pending) should be queued
	if len(queuedForRun) != 2 {
		t.Fatalf("expected 2 tasks to be queued for resume, got %d (%+v)", len(queuedForRun), queuedForRun)
	}
	if queuedForRun[0] != 4 || queuedForRun[1] != 5 {
		t.Errorf("unexpected queued tasks on resume: %+v", queuedForRun)
	}
}

// TestStorageManager_Deduplication verifies that duplicate records across workers are discarded.
func TestStorageManager_Deduplication(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outDir := filepath.Join(tempDir, "out")
	csvFile := filepath.Join(outDir, "stores.csv")
	sm, err := parallel.NewStorageManager(outDir, csvFile)
	if err != nil {
		t.Fatalf("failed to create storage manager: %v", err)
	}
	defer sm.Close()

	rec := &model.StoreRecord{
		PlaceID: "ChIJ_TEST_123",
		Name:    "Toko Bangunan Sumber Rejeki",
		Address: "Jl. Raya Darmo No 10, Surabaya",
		City:    "Surabaya",
	}

	savedFirst, err := sm.CheckAndSaveRecord(1, rec)
	if err != nil || !savedFirst {
		t.Fatalf("expected first save to succeed, err=%v", err)
	}

	// Try saving identical PlaceID from another worker
	savedSecond, err := sm.CheckAndSaveRecord(2, rec)
	if err != nil || savedSecond {
		t.Fatalf("expected second save to be discarded as duplicate, got saved=%v, err=%v", savedSecond, err)
	}

	if sm.GetTotalSaved() != 1 {
		t.Errorf("expected 1 unique saved store, got %d", sm.GetTotalSaved())
	}
}

// TestExactTargetStores_5Workers verifies that when target is set to 3 or 10,
// 5 concurrent workers attempting to save hundreds of items stop precisely at target.
func TestExactTargetStores_5Workers(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "target_stores_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testCases := []struct {
		name         string
		targetStores int
		numWorkers   int
		itemsToSpam  int
	}{
		{name: "Exact Target = 3", targetStores: 3, numWorkers: 5, itemsToSpam: 100},
		{name: "Exact Target = 10", targetStores: 10, numWorkers: 5, itemsToSpam: 200},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := filepath.Join(tempDir, tc.name)
			csvFile := filepath.Join(outDir, "out.csv")

			sm, err := parallel.NewStorageManagerWithTarget(outDir, csvFile, tc.targetStores)
			if err != nil {
				t.Fatalf("failed to create storage: %v", err)
			}
			defer sm.Close()

			var wg sync.WaitGroup
			var successfullySavedCount int32

			// Launch 5 workers bombarding the storage manager concurrently with valid unique records
			for w := 1; w <= tc.numWorkers; w++ {
				workerID := w
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < tc.itemsToSpam; i++ {
						rec := &model.StoreRecord{
							PlaceID: fmt.Sprintf("ChIJ_W%d_ITEM_%d", workerID, i),
							Name:    fmt.Sprintf("Store W%d Item %d", workerID, i),
							Address: fmt.Sprintf("Jl. Testing W%d No %d, Surabaya", workerID, i),
							City:    "Surabaya",
						}
						saved, _ := sm.CheckAndSaveRecord(workerID, rec)
						if saved {
							atomic.AddInt32(&successfullySavedCount, 1)
						}
					}
				}()
			}

			wg.Wait()

			// StorageManager must record EXACT target count
			totalSaved := sm.GetTotalSaved()
			if totalSaved != tc.targetStores {
				t.Fatalf("[%s] StorageManager.GetTotalSaved() = %d; expected EXACTLY %d (OVERSHOOT DETECTED!)",
					tc.name, totalSaved, tc.targetStores)
			}

			if int(successfullySavedCount) != tc.targetStores {
				t.Fatalf("[%s] Accepted return count = %d; expected EXACTLY %d",
					tc.name, successfullySavedCount, tc.targetStores)
			}
		})
	}
}

