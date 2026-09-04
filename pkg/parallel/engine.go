package parallel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"maps-scraper/pkg/checkpoint"
	"maps-scraper/pkg/model"
)

var ErrNoPendingQueries = errors.New("all queries in the requested range are already completed")

type EngineConfig struct {
	StartID        int
	EndID          int
	Direction      string // "reverse" or "forward"
	NumWorkers     int
	Headless       bool
	MinDelayMs     int
	MaxDelayMs     int
	TimeoutMs      int
	MaxRetries     int
	TargetStores   int    // Stop automatically after finding this many stores (e.g. 200 or 250)
	OutputDir      string
	LogDir         string
	CheckpointPath string
	BaseCSVFile    string
	JobConfig      model.JobConfig
	OnItemFound    func(workerID int, rec model.StoreRecord)
	OnWorkerUpdate func(wState model.WorkerState)
	OnLog          func(workerID int, level, msg string)
	ForceRunRange  bool
	IsPaused       func() bool
}



type logForwarder struct {
	workerID int
	onLog    func(workerID int, level, msg string)
}

func (l *logForwarder) Write(p []byte) (n int, err error) {
	if l.onLog != nil {
		l.onLog(l.workerID, "INFO", string(p))
	}
	return len(p), nil
}

type Engine struct {
	cfg                    EngineConfig
	queries                []string
	checkpoint             *checkpoint.CheckpointManager
	storage                *StorageManager
	logger                 *log.Logger
	logFile                *os.File
	activeJobs             int32
	sessionQueriesProcessed int32
	sessionQueriesSuccess   int32
	sessionQueriesFailed    int32
}

func NewEngine(cfg EngineConfig, allQueries []string) (*Engine, error) {
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = 3
	}
	if cfg.StartID <= 0 || cfg.StartID > len(allQueries) {
		cfg.StartID = len(allQueries)
	}
	if cfg.EndID <= 0 || cfg.EndID > len(allQueries) {
		cfg.EndID = 1
	}
	if cfg.Direction == "" {
		cfg.Direction = "reverse"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "parallel_results"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = "logs"
	}
	if cfg.CheckpointPath == "" {
		cfg.CheckpointPath = "parallel_progress.json"
	}

	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log dir: %w", err)
	}

	logFilePath := filepath.Join(cfg.LogDir, "parallel_iterative.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	engineLogger := log.New(multiWriter, "", log.LstdFlags)

	cm, err := checkpoint.NewCheckpointManager(cfg.CheckpointPath, allQueries, cfg.StartID, cfg.EndID, cfg.Direction)
	if err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("failed to initialize checkpoint manager: %w", err)
	}

	if cfg.JobConfig.ID != "" || cfg.JobConfig.Location.GetSearchString() != "" {
		cm.SetMetadata(cfg.JobConfig.ID, cfg.JobConfig.Location.GetSearchString())
	}

	sm, err := NewStorageManagerWithTarget(cfg.OutputDir, cfg.BaseCSVFile, cfg.TargetStores)
	if err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("failed to initialize storage manager: %w", err)
	}

	return &Engine{
		cfg:        cfg,
		queries:    allQueries,
		checkpoint: cm,
		storage:    sm,
		logger:     engineLogger,
		logFile:    logFile,
	}, nil
}

type QueryTask struct {
	ID          int
	QueryString string
	Attempt     int
}

func (e *Engine) GetCheckpointData() *checkpoint.CheckpointData {
	if e.checkpoint != nil {
		data := e.checkpoint.GetData()
		return &data
	}
	return nil
}

func (e *Engine) GetSessionStats() (processed, success, failed, stores int) {
	p := int(atomic.LoadInt32(&e.sessionQueriesProcessed))
	s := int(atomic.LoadInt32(&e.sessionQueriesSuccess))
	f := int(atomic.LoadInt32(&e.sessionQueriesFailed))
	stores = 0
	if e.storage != nil {
		stores = e.storage.GetTotalSaved()
	}
	return p, s, f, stores
}

func (e *Engine) Run(parentCtx context.Context) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	startTime := time.Now()
	e.logger.Println("================================================================================")
	e.logger.Println("                PARALLEL ITERATIVE SCRAPING ENGINE STARTED                      ")
	e.logger.Println("================================================================================")
	e.logger.Printf("[CONFIG] Total Queries in System: %d\n", len(e.queries))
	e.logger.Printf("[CONFIG] Target Range: %d -> %d (Direction: %s)\n", e.cfg.StartID, e.cfg.EndID, e.cfg.Direction)
	e.logger.Printf("[CONFIG] Parallel Workers: %d | Headless: %v | Max Retries: %d\n", e.cfg.NumWorkers, e.cfg.Headless, e.cfg.MaxRetries)
	if e.cfg.TargetStores > 0 {
		e.logger.Printf("[CONFIG] Auto-Stop Target: %d new stores\n", e.cfg.TargetStores)
	} else {
		e.logger.Println("[CONFIG] Auto-Stop Target: None (continuous)")
	}
	e.logger.Printf("[CONFIG] Output Directory: %s | Checkpoint: %s\n", e.cfg.OutputDir, e.cfg.CheckpointPath)
	e.logger.Println("================================================================================")

	// Generate ordered query task sequence
	var orderedIDs []int
	if e.cfg.Direction == "reverse" {
		start := e.cfg.StartID
		end := e.cfg.EndID
		if start < end {
			start, end = end, start
		}
		for id := start; id >= end; id-- {
			orderedIDs = append(orderedIDs, id)
		}
	} else {
		start := e.cfg.StartID
		end := e.cfg.EndID
		if start > end {
			start, end = end, start
		}
		for id := start; id <= end; id++ {
			orderedIDs = append(orderedIDs, id)
		}
	}

	// Filter tasks that need processing
	taskQueue := make(chan QueryTask, len(orderedIDs)+100)
	totalQueued := 0

	for _, id := range orderedIDs {
		rec, exists := e.checkpoint.GetRecord(id)
		if e.cfg.ForceRunRange || !exists || (rec.Status != checkpoint.StateSuccess && rec.Status != checkpoint.StateFailedPermanent) {
			attempts := 0
			if exists {
				attempts = rec.Attempts
			}
			taskQueue <- QueryTask{
				ID:          id,
				QueryString: e.queries[id-1],
				Attempt:     attempts,
			}
			totalQueued++
		}
	}

	e.logger.Printf("[QUEUE] Loaded %d pending/retry query tasks into queue (out of %d total range).\n",
		totalQueued, len(orderedIDs))

	if totalQueued == 0 {
		e.logger.Println("[DONE] All queries in the requested range are already SUCCESS. Nothing to do.")
		return ErrNoPendingQueries
	}

	// Start Background Progress Monitor
	monitorStop := make(chan struct{})
	go e.startProgressMonitor(ctx, len(orderedIDs), startTime, monitorStop)

	var wg sync.WaitGroup
	var targetReached int32

	for i := 1; i <= e.cfg.NumWorkers; i++ {
		workerID := i
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Create dedicated worker logger
			workerLogPath := filepath.Join(e.cfg.LogDir, fmt.Sprintf("worker_%02d.log", workerID))
			wFile, err := os.OpenFile(workerLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			var wLogger *log.Logger
			if err == nil {
				defer wFile.Close()
				var writers []io.Writer
				writers = append(writers, os.Stdout, wFile, e.logFile)
				if e.cfg.OnLog != nil {
					writers = append(writers, &logForwarder{workerID: workerID, onLog: e.cfg.OnLog})
				}
				wLogger = log.New(io.MultiWriter(writers...), "", log.LstdFlags)
			} else {
				var writers []io.Writer
				writers = append(writers, os.Stdout, e.logFile)
				if e.cfg.OnLog != nil {
					writers = append(writers, &logForwarder{workerID: workerID, onLog: e.cfg.OnLog})
				}
				wLogger = log.New(io.MultiWriter(writers...), "", log.LstdFlags)
			}

			workerCfg := WorkerConfig{
				WorkerID:       workerID,
				Headless:       e.cfg.Headless,
				MinDelay:       time.Duration(e.cfg.MinDelayMs) * time.Millisecond,
				MaxDelay:       time.Duration(e.cfg.MaxDelayMs) * time.Millisecond,
				Timeout:        time.Duration(e.cfg.TimeoutMs) * time.Millisecond,
				Logger:         wLogger,
				JobConfig:      e.cfg.JobConfig,
				OnItemFound:    e.cfg.OnItemFound,
				OnWorkerUpdate: e.cfg.OnWorkerUpdate,
			}

			worker := NewWorker(workerCfg, e.storage)
			if err := worker.Init(); err != nil {
				wLogger.Printf("[Worker %02d FATAL] Failed to initialize: %v\n", workerID, err)
				if e.cfg.OnWorkerUpdate != nil {
					e.cfg.OnWorkerUpdate(model.WorkerState{
						ID:           workerID,
						Status:       model.WorkerStatusFailed,
						ErrorMessage: err.Error(),
					})
				}
				return
			}
			defer worker.Close()

			wLogger.Printf("[Worker %02d] Engine started and ready.\n", workerID)
			if e.cfg.OnWorkerUpdate != nil {
				e.cfg.OnWorkerUpdate(model.WorkerState{
					ID:         workerID,
					Status:     model.WorkerStatusRunning,
					MaxRetries: e.cfg.MaxRetries,
				})
			}

			workerStoresFound := 0
			workerErrors := 0
			workerStartTime := time.Now()

			for {
				if ctx.Err() != nil {
					wLogger.Printf("[Worker %02d] Received stop signal, shutting down worker cleanly.\n", workerID)
					if e.cfg.OnWorkerUpdate != nil {
						e.cfg.OnWorkerUpdate(model.WorkerState{
							ID:           workerID,
							Status:       model.WorkerStatusStopped,
							RecordsFound: workerStoresFound,
							RecordsValid: workerStoresFound,
							Errors:       workerErrors,
						})
					}
					return
				}

				// Check if session/job is paused by user
				for e.cfg.IsPaused != nil && e.cfg.IsPaused() {
					if e.cfg.OnWorkerUpdate != nil {
						e.cfg.OnWorkerUpdate(model.WorkerState{
							ID:           workerID,
							Status:       model.WorkerStatusPaused,
							RecordsFound: workerStoresFound,
							RecordsValid: workerStoresFound,
							Errors:       workerErrors,
						})
					}
					time.Sleep(500 * time.Millisecond)
					if ctx.Err() != nil {
						return
					}
				}

				select {
				case <-ctx.Done():
					wLogger.Printf("[Worker %02d] Received stop signal, shutting down worker cleanly.\n", workerID)
					if e.cfg.OnWorkerUpdate != nil {
						e.cfg.OnWorkerUpdate(model.WorkerState{
							ID:           workerID,
							Status:       model.WorkerStatusStopped,
							RecordsFound: workerStoresFound,
							RecordsValid: workerStoresFound,
							Errors:       workerErrors,
						})
					}
					return
				case task, ok := <-taskQueue:
					if !ok {
						if e.cfg.OnWorkerUpdate != nil {
							e.cfg.OnWorkerUpdate(model.WorkerState{
								ID:           workerID,
								Status:       model.WorkerStatusCompleted,
								Progress:     100.0,
								RecordsFound: workerStoresFound,
								RecordsValid: workerStoresFound,
								Errors:       workerErrors,
							})
						}
						return
					}

					if ctx.Err() != nil {
						e.checkpoint.MarkPending(task.ID)
						return
					}

					atomic.AddInt32(&e.activeJobs, 1)
					e.checkpoint.MarkRunning(task.ID, workerID)

					pct := 0.0
					if len(orderedIDs) > 0 {
						pct = (float64(task.ID) / float64(len(orderedIDs))) * 100.0
					}

					if e.cfg.OnWorkerUpdate != nil {
						elapsed := time.Since(workerStartTime).Round(time.Second).String()
						e.cfg.OnWorkerUpdate(model.WorkerState{
							ID:              workerID,
							Status:          model.WorkerStatusRunning,
							Progress:        pct,
							CurrentQuery:    task.QueryString,
							CurrentLocation: e.cfg.JobConfig.Location.GetSearchString(),
							CurrentBatch:    task.ID,
							RecordsFound:    workerStoresFound,
							RecordsValid:    workerStoresFound,
							Errors:          workerErrors,
							ElapsedTime:     elapsed,
							RetryCount:      task.Attempt,
							MaxRetries:      e.cfg.MaxRetries,
						})
					}

					storesFound, err := worker.ProcessQuery(ctx, task.ID, task.QueryString)
					atomic.AddInt32(&e.activeJobs, -1)

					if err != nil {
						if ctx.Err() != nil || errors.Is(err, context.Canceled) {
							// Query was interrupted due to target reached or cancellation - reset to pending for future iterations
							e.checkpoint.MarkPending(task.ID)
							wLogger.Printf("[Worker %02d] Query %d canceled / interrupted, reset to pending.\n", workerID, task.ID)
						} else {
							atomic.AddInt32(&e.sessionQueriesProcessed, 1)
							atomic.AddInt32(&e.sessionQueriesFailed, 1)
							workerErrors++
							wLogger.Printf("[Worker %02d] Query %d FAILED (Attempt %d/%d): %v\n",
								workerID, task.ID, task.Attempt+1, e.cfg.MaxRetries, err)

							if task.Attempt+1 < e.cfg.MaxRetries && ctx.Err() == nil {
								e.checkpoint.MarkFailed(task.ID, err.Error(), false)
								time.Sleep(time.Duration(2000+rand.Intn(3000)) * time.Millisecond)
								taskQueue <- QueryTask{
									ID:          task.ID,
									QueryString: task.QueryString,
									Attempt:     task.Attempt + 1,
								}
							} else {
								e.checkpoint.MarkFailed(task.ID, err.Error(), true)
								wLogger.Printf("[Worker %02d] Query %d PERMANENTLY FAILED after %d attempts.\n",
									workerID, task.ID, task.Attempt+1)
							}
						}
					} else {
						atomic.AddInt32(&e.sessionQueriesProcessed, 1)
						atomic.AddInt32(&e.sessionQueriesSuccess, 1)
						workerStoresFound += storesFound
						e.checkpoint.MarkSuccess(task.ID, storesFound)
					}

					// Check if Target Stores reached
					currentStores := e.storage.GetTotalSaved()
					if e.cfg.TargetStores > 0 && currentStores >= e.cfg.TargetStores {
						if atomic.CompareAndSwapInt32(&targetReached, 0, 1) {
							e.logger.Printf("\n🎯 [TARGET REACHED] Target of %d new stores reached (Current total: %d)! Gracefully stopping scraper...\n",
								e.cfg.TargetStores, currentStores)
							cancel()
							// Drain remaining tasks non-blockingly so other workers exit cleanly
							for {
								select {
								case <-taskQueue:
								default:
									return
								}
							}
						}
						return
					}

					// Worker politeness delay between consecutive queries
					delayMs := e.cfg.MinDelayMs
					if e.cfg.MaxDelayMs > e.cfg.MinDelayMs {
						delayMs = rand.Intn(e.cfg.MaxDelayMs-e.cfg.MinDelayMs) + e.cfg.MinDelayMs
					}
					time.Sleep(time.Duration(delayMs) * time.Millisecond)
				}
			}
		}()
	}

	// Close queue when context is done or wait for completion
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			if ctx.Err() != nil {
				return
			}
			if len(taskQueue) == 0 && atomic.LoadInt32(&e.activeJobs) == 0 {
				close(taskQueue)
				return
			}
		}
	}()

	wg.Wait()
	close(monitorStop)

	e.storage.Close()
	_ = e.logFile.Close()

	completed, running, pending, failed, stores := e.checkpoint.GetSummary()
	e.logger.Println("\n================================================================================")
	e.logger.Println("                  PARALLEL SCRAPING SESSION FINISHED                            ")
	e.logger.Println("================================================================================")
	e.logger.Printf("[SUMMARY] Completed: %d | Pending: %d | Running: %d | Failed: %d\n", completed, pending, running, failed)
	e.logger.Printf("[SUMMARY] New Stores Discovered in This Session: %d\n", e.storage.GetTotalSaved())
	e.logger.Printf("[SUMMARY] Total Stores in Output: %d\n", stores)
	e.logger.Printf("[SUMMARY] Elapsed Time: %s\n", time.Since(startTime).Round(time.Second))
	e.logger.Printf("[OUTPUT] Combined output stored at: %s/parallel_stores_combined.csv\n", e.cfg.OutputDir)
	e.logger.Println("================================================================================")

	return nil
}

func (e *Engine) startProgressMonitor(ctx context.Context, totalRange int, startTime time.Time, stopChan chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			completed, running, pending, failed, stores := e.checkpoint.GetSummary()
			pct := 0.0
			if totalRange > 0 {
				pct = (float64(completed+failed) / float64(totalRange)) * 100.0
			}
			elapsed := time.Since(startTime).Round(time.Second)

			e.logger.Printf("\n[PROGRESS MONITOR] Range: %d | Completed: %d | Running: %d | Pending: %d | Failed: %d | Stores: %d (%.2f%%) | Elapsed: %s\n",
				totalRange, completed, running, pending, failed, stores, pct, elapsed)
		}
	}
}
