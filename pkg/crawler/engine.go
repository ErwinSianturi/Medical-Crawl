package crawler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"maps-scraper/pkg/checkpoint"
	"maps-scraper/pkg/model"
)

// EngineConfig configures the execution of the CrawlerEngine.
type EngineConfig struct {
	NumWorkers     int           `json:"num_workers"`
	MinDelay       time.Duration `json:"min_delay"`
	MaxDelay       time.Duration `json:"max_delay"`
	Timeout        time.Duration `json:"timeout"`
	MaxRetries     int           `json:"max_retries"`
	TargetArticles int           `json:"target_articles"` // Stop after finding N articles (0 = unlimited)
	OutputPath     string        `json:"output_path"`
	CheckpointPath string        `json:"checkpoint_path"`
}

// EngineTelemetry represents real-time aggregate statistics of the crawling run.
type EngineTelemetry struct {
	TotalTasks      int               `json:"total_tasks"`
	TasksProcessed  int               `json:"tasks_processed"`
	ArticlesSaved   int               `json:"articles_saved"`
	ErrorsCount     int               `json:"errors_count"`
	ActiveWorkers   int               `json:"active_workers"`
	ProgressPercent float64           `json:"progress_percent"`
	ArticlesPerMin  float64           `json:"articles_per_min"`
	ElapsedTime     string            `json:"elapsed_time"`
	Workers         []WorkerTelemetry `json:"workers"`
}

// Engine coordinates the worker pool, task distribution, article processing, and persistence.
type Engine struct {
	cfg        EngineConfig
	taskQueue  *TaskQueue
	fetcher    HTTPFetcher
	extractor  ArticleExtractor
	processor  *ArticleProcessor
	storage    *JSONStorage
	checkpoint *checkpoint.CheckpointManager

	workers []*Worker
	wg      sync.WaitGroup

	startTime time.Time
	mu        sync.RWMutex
	isPaused  int32 // atomic boolean flag

	runCancel context.CancelFunc

	onArticleSaved func(workerID int, art model.Article)
	onTelemetry    func(telemetry EngineTelemetry)
	onLog          func(level, msg string)
}

// NewEngine creates an initialized Engine ready to run.
func NewEngine(
	cfg EngineConfig,
	tasks []Task,
	fetcher HTTPFetcher,
	extractor ArticleExtractor,
	cp *checkpoint.CheckpointManager,
) (*Engine, error) {
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = 3
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.OutputPath == "" {
		cfg.OutputPath = "articles.json"
	}

	queue := NewTaskQueue(tasks)

	storage, err := NewJSONStorage(cfg.OutputPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JSON storage: %w", err)
	}

	processor := NewArticleProcessor(DefaultArticleProcessorConfig())

	e := &Engine{
		cfg:        cfg,
		taskQueue:  queue,
		fetcher:    fetcher,
		extractor:  extractor,
		processor:  processor,
		storage:    storage,
		checkpoint: cp,
		workers:    make([]*Worker, cfg.NumWorkers),
	}

	// Initialize workers
	for i := 0; i < cfg.NumWorkers; i++ {
		workerID := i + 1
		workerCfg := WorkerConfig{
			ID:        workerID,
			Fetcher:   fetcher,
			Extractor: extractor,
			Processor: processor,
			Storage:   storage,
			Queue:     queue,
			MinDelay:  cfg.MinDelay,
			MaxDelay:  cfg.MaxDelay,
			Timeout:   cfg.Timeout,
			OnItemSaved: func(wID int, art model.Article) {
				if e.cfg.TargetArticles > 0 && e.storage.TotalSaved() >= e.cfg.TargetArticles {
					e.mu.RLock()
					cancelFn := e.runCancel
					e.mu.RUnlock()
					if cancelFn != nil {
						cancelFn()
					}
				}
				if e.onArticleSaved != nil {
					e.onArticleSaved(wID, art)
				}
			},
			OnWorkerUpdate: func(t WorkerTelemetry) {
				e.broadcastTelemetry()
			},
			OnLog: func(wID int, level, msg string) {
				if e.onLog != nil {
					e.onLog(level, fmt.Sprintf("[Worker %02d] %s", wID, msg))
				}
			},
		}

		e.workers[i] = NewWorker(workerCfg)
	}

	return e, nil
}

// SetCallbacks sets telemetry and event notification callbacks.
func (e *Engine) SetCallbacks(
	onArticleSaved func(workerID int, art model.Article),
	onTelemetry func(telemetry EngineTelemetry),
	onLog func(level, msg string),
) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onArticleSaved = onArticleSaved
	e.onTelemetry = onTelemetry
	e.onLog = onLog
}

// Pause pauses worker task consumption.
func (e *Engine) Pause() {
	atomic.StoreInt32(&e.isPaused, 1)
}

// Resume resumes worker task consumption.
func (e *Engine) Resume() {
	atomic.StoreInt32(&e.isPaused, 0)
}

// IsPaused returns whether the engine is currently paused.
func (e *Engine) IsPaused() bool {
	return atomic.LoadInt32(&e.isPaused) == 1
}

// Run executes the crawler engine until tasks are finished, target is reached, or context is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	e.startTime = time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	e.mu.Lock()
	e.runCancel = cancel
	e.mu.Unlock()

	// Launch worker pool
	for _, w := range e.workers {
		worker := w
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			worker.Run(runCtx, e.IsPaused)
		}()
	}

	// Monitor target records and task completion in background
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				totalSaved := e.storage.TotalSaved()
				if e.cfg.TargetArticles > 0 && totalSaved >= e.cfg.TargetArticles {
					if e.onLog != nil {
						e.onLog("INFO", fmt.Sprintf("Target articles reached: %d", totalSaved))
					}
					cancel()
					return
				}
				e.broadcastTelemetry()
			}
		}
	}()

	// Wait for workers to finish
	e.wg.Wait()
	cancel() // ensure monitor stops
	<-monitorDone

	// Finalize storage
	if err := e.storage.Close(); err != nil {
		return fmt.Errorf("failed to finalize storage: %w", err)
	}

	e.broadcastTelemetry()

	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}

	return nil
}

func (e *Engine) broadcastTelemetry() {
	e.mu.RLock()
	cb := e.onTelemetry
	e.mu.RUnlock()

	if cb == nil {
		return
	}

	totalSaved := e.storage.TotalSaved()
	totalTasks := e.taskQueue.TotalTasks()

	var tasksProcessed, errorsCount int
	workerTelem := make([]WorkerTelemetry, len(e.workers))
	activeCount := 0

	for i, w := range e.workers {
		t := w.Telemetry()
		workerTelem[i] = t
		tasksProcessed += t.TasksProcessed
		errorsCount += t.ErrorsCount
		if t.State == WorkerStateRunning {
			activeCount++
		}
	}

	elapsed := time.Since(e.startTime)
	var speed float64
	if elapsed.Minutes() > 0 {
		speed = float64(totalSaved) / elapsed.Minutes()
	}

	var progress float64
	if totalTasks > 0 {
		progress = (float64(tasksProcessed) / float64(totalTasks)) * 100.0
		if progress > 100.0 {
			progress = 100.0
		}
	}

	telem := EngineTelemetry{
		TotalTasks:      totalTasks,
		TasksProcessed:  tasksProcessed,
		ArticlesSaved:   totalSaved,
		ErrorsCount:     errorsCount,
		ActiveWorkers:   activeCount,
		ProgressPercent: progress,
		ArticlesPerMin:  speed,
		ElapsedTime:     fmt.Sprintf("%02d:%02d:%02d", int(elapsed.Hours()), int(elapsed.Minutes())%60, int(elapsed.Seconds())%60),
		Workers:         workerTelem,
	}

	cb(telem)
}

// Storage returns the engine's JSONStorage.
func (e *Engine) Storage() *JSONStorage {
	return e.storage
}
