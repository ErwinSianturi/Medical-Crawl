package crawler

import (
	"sync"
	"time"

	"maps-scraper/pkg/model"
)

// TaskStatus represents the lifecycle state of a crawling task.
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "PENDING"
	TaskStatusRunning TaskStatus = "RUNNING"
	TaskStatusSuccess TaskStatus = "SUCCESS"
	TaskStatusFailed  TaskStatus = "FAILED"
	TaskStatusRetry   TaskStatus = "RETRY"
)

// Task represents a generic unit of crawling work, such as a webpage URL to fetch and parse.
// It contains no store, location, or building-material dependencies.
type Task struct {
	ID         string            `json:"id"`
	URL        string            `json:"url"`
	Type       string            `json:"type"` // e.g. "article", "category", "feed"
	Source     string            `json:"source"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Attempts   int               `json:"attempts"`
	MaxRetries int               `json:"max_retries"`
	CreatedAt  time.Time         `json:"created_at"`
}

// TaskResult captures the result of executing a crawling task by a worker.
type TaskResult struct {
	Task      Task
	Article   *model.Article
	Error     error
	Duration  time.Duration
	WorkerID  int
}

// TaskQueue is a thread-safe in-memory FIFO task queue for distributing crawling work.
type TaskQueue struct {
	mu     sync.RWMutex
	tasks  []Task
	ch     chan Task
	closed bool
}

// NewTaskQueue creates a new TaskQueue with an optional initial slice of tasks.
func NewTaskQueue(initialTasks []Task) *TaskQueue {
	capacity := len(initialTasks)
	if capacity < 100 {
		capacity = 100
	}

	q := &TaskQueue{
		tasks: make([]Task, 0, capacity),
		ch:    make(chan Task, capacity),
	}

	for _, t := range initialTasks {
		q.Push(t)
	}

	if len(initialTasks) > 0 {
		q.Close()
	}

	return q
}

// Push adds a task to the queue.
func (q *TaskQueue) Push(task Task) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return false
	}

	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}

	q.tasks = append(q.tasks, task)
	select {
	case q.ch <- task:
		return true
	default:
		return false
	}
}

// Channel returns the receive-only channel for workers to consume tasks.
func (q *TaskQueue) Channel() <-chan Task {
	return q.ch
}

// Len returns the current count of buffered tasks.
func (q *TaskQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.ch)
}

// TotalTasks returns the total number of tasks submitted.
func (q *TaskQueue) TotalTasks() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tasks)
}

// Close closes the task queue channel.
func (q *TaskQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.closed {
		q.closed = true
		close(q.ch)
	}
}

// IsClosed returns true if the queue has been closed.
func (q *TaskQueue) IsClosed() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.closed
}
