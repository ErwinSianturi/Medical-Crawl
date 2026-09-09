package crawler

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"maps-scraper/pkg/model"
)

// WorkerState represents the real-time execution state of a crawler worker.
type WorkerState string

const (
	WorkerStateQueued    WorkerState = "QUEUED"
	WorkerStateStarting  WorkerState = "STARTING"
	WorkerStateRunning   WorkerState = "RUNNING"
	WorkerStateIdle      WorkerState = "IDLE"
	WorkerStatePaused    WorkerState = "PAUSED"
	WorkerStateCompleted WorkerState = "COMPLETED"
	WorkerStateStopped   WorkerState = "STOPPED"
	WorkerStateFailed    WorkerState = "FAILED"
)

// WorkerTelemetry captures runtime metrics for a single crawler worker.
type WorkerTelemetry struct {
	ID             int         `json:"id"`
	State          WorkerState `json:"state"`
	CurrentTaskID  string      `json:"current_task_id"`
	CurrentURL     string      `json:"current_url"`
	TasksProcessed int         `json:"tasks_processed"`
	ArticlesSaved  int         `json:"articles_saved"`
	ErrorsCount    int         `json:"errors_count"`
	LastActiveAt   string      `json:"last_active_at"`
	ErrorMessage   string      `json:"error_message,omitempty"`
}

// Worker handles the execution of individual crawl tasks.
type Worker struct {
	id        int
	fetcher   HTTPFetcher
	extractor ArticleExtractor
	processor *ArticleProcessor
	storage   *JSONStorage
	queue     *TaskQueue

	minDelay time.Duration
	maxDelay time.Duration
	timeout  time.Duration

	mu        sync.RWMutex
	telemetry WorkerTelemetry

	onItemSaved    func(workerID int, art model.Article)
	onWorkerUpdate func(telemetry WorkerTelemetry)
	onLog          func(workerID int, level, msg string)
}

// WorkerConfig holds initialization parameters for a Worker.
type WorkerConfig struct {
	ID             int
	Fetcher        HTTPFetcher
	Extractor      ArticleExtractor
	Processor      *ArticleProcessor
	Storage        *JSONStorage
	Queue          *TaskQueue
	MinDelay       time.Duration
	MaxDelay       time.Duration
	Timeout        time.Duration
	OnItemSaved    func(workerID int, art model.Article)
	OnWorkerUpdate func(telemetry WorkerTelemetry)
	OnLog          func(workerID int, level, msg string)
}

// NewWorker initializes a new crawler Worker.
func NewWorker(cfg WorkerConfig) *Worker {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	w := &Worker{
		id:             cfg.ID,
		fetcher:        cfg.Fetcher,
		extractor:      cfg.Extractor,
		processor:      cfg.Processor,
		storage:        cfg.Storage,
		queue:          cfg.Queue,
		minDelay:       cfg.MinDelay,
		maxDelay:       cfg.MaxDelay,
		timeout:        cfg.Timeout,
		onItemSaved:    cfg.OnItemSaved,
		onWorkerUpdate: cfg.OnWorkerUpdate,
		onLog:          cfg.OnLog,
		telemetry: WorkerTelemetry{
			ID:           cfg.ID,
			State:        WorkerStateQueued,
			LastActiveAt: time.Now().Format(time.RFC3339),
		},
	}

	return w
}

// Run executes the worker processing loop until the task queue is exhausted or context is cancelled.
func (w *Worker) Run(ctx context.Context, isPaused func() bool) {
	w.setState(WorkerStateRunning, "", "")
	w.log("INFO", fmt.Sprintf("Worker %02d started", w.id))

	defer func() {
		w.setState(WorkerStateCompleted, "", "")
		w.log("INFO", fmt.Sprintf("Worker %02d stopped", w.id))
	}()

	for {
		// 1. Check pause condition
		for isPaused != nil && isPaused() {
			w.setState(WorkerStatePaused, "", "")
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}

		// 2. Select next task from queue or handle shutdown
		select {
		case <-ctx.Done():
			w.setState(WorkerStateStopped, "", "")
			return

		case task, ok := <-w.queue.Channel():
			if !ok {
				// Queue closed and empty
				return
			}

			w.executeTask(ctx, task)
		}
	}
}

func (w *Worker) executeTask(ctx context.Context, task Task) {
	if ctx.Err() != nil {
		return
	}
	w.setState(WorkerStateRunning, task.ID, task.URL)
	w.applyPoliteDelay()

	taskCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	// 1. Fetch
	content, err := w.fetcher.Fetch(taskCtx, task.URL)
	if err != nil {
		w.recordError(task, fmt.Sprintf("Fetch failed: %v", err))
		return
	}

	// 2. Extract
	rawArticle, err := w.extractor.Extract(taskCtx, task, content)
	if err != nil {
		w.recordError(task, fmt.Sprintf("Extraction failed: %v", err))
		return
	}

	if rawArticle == nil {
		w.recordError(task, "Extractor returned nil article")
		return
	}

	// 3. Process (Validate, Sanitize, Deduplicate)
	processed, err := w.processor.Process(taskCtx, rawArticle, task.URL)
	if err != nil {
		w.recordError(task, fmt.Sprintf("Processing/Dedup rejected: %v", err))
		return
	}

	// 4. Save to JSON Storage
	if err := w.storage.Save(*processed); err != nil {
		w.recordError(task, fmt.Sprintf("Storage save failed: %v", err))
		return
	}

	// 5. Update success telemetry
	w.mu.Lock()
	w.telemetry.TasksProcessed++
	w.telemetry.ArticlesSaved++
	w.telemetry.LastActiveAt = time.Now().Format(time.RFC3339)
	tCopy := w.telemetry
	w.mu.Unlock()

	if w.onItemSaved != nil {
		w.onItemSaved(w.id, *processed)
	}
	if w.onWorkerUpdate != nil {
		w.onWorkerUpdate(tCopy)
	}

	w.log("INFO", fmt.Sprintf("Saved article: '%s'", processed.Title))
}

func (w *Worker) applyPoliteDelay() {
	if w.minDelay <= 0 && w.maxDelay <= 0 {
		return
	}
	delay := w.minDelay
	if w.maxDelay > w.minDelay {
		delta := w.maxDelay - w.minDelay
		delay += time.Duration(rand.Int63n(int64(delta)))
	}
	time.Sleep(delay)
}

func (w *Worker) recordError(task Task, msg string) {
	w.mu.Lock()
	w.telemetry.TasksProcessed++
	w.telemetry.ErrorsCount++
	w.telemetry.ErrorMessage = msg
	w.telemetry.LastActiveAt = time.Now().Format(time.RFC3339)
	tCopy := w.telemetry
	w.mu.Unlock()

	w.log("WARN", fmt.Sprintf("Task %s (%s): %s", task.ID, task.URL, msg))

	if w.onWorkerUpdate != nil {
		w.onWorkerUpdate(tCopy)
	}
}

func (w *Worker) setState(state WorkerState, taskID, url string) {
	w.mu.Lock()
	w.telemetry.State = state
	w.telemetry.CurrentTaskID = taskID
	w.telemetry.CurrentURL = url
	w.telemetry.LastActiveAt = time.Now().Format(time.RFC3339)
	tCopy := w.telemetry
	w.mu.Unlock()

	if w.onWorkerUpdate != nil {
		w.onWorkerUpdate(tCopy)
	}
}

func (w *Worker) log(level, msg string) {
	if w.onLog != nil {
		w.onLog(w.id, level, msg)
	}
}

// Telemetry returns a snapshot of the worker telemetry.
func (w *Worker) Telemetry() WorkerTelemetry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.telemetry
}
