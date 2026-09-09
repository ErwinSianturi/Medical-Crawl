package crawler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"maps-scraper/pkg/model"
)

// JobStatus represents the state of a crawler job.
type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusPaused    JobStatus = "PAUSED"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusStopped   JobStatus = "STOPPED"
	JobStatusFailed    JobStatus = "FAILED"
)

// JobConfig defines the specification for a crawling job.
type JobConfig struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	SourceNames    []string      `json:"source_names"`
	Tasks          []Task        `json:"tasks"`
	Workers        int           `json:"workers"`
	TargetArticles int           `json:"target_articles"`
	MinDelay       time.Duration `json:"min_delay"`
	MaxDelay       time.Duration `json:"max_delay"`
	Timeout        time.Duration `json:"timeout"`
	OutputPath     string        `json:"output_path"`
}

// JobState represents the dynamic runtime telemetry of a crawler job.
type JobState struct {
	Config       JobConfig       `json:"config"`
	Status       JobStatus       `json:"status"`
	Telemetry    EngineTelemetry `json:"telemetry"`
	StartedAt    string          `json:"started_at"`
	EndedAt      string          `json:"ended_at,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
}

// JobManager provides centralized orchestration for crawler jobs.
type JobManager struct {
	mu      sync.RWMutex
	jobs    map[string]*JobState
	engines map[string]*Engine
	cancels map[string]context.CancelFunc
	dataDir string
	logsDir string

	fetcher   HTTPFetcher
	extractor ArticleExtractor
}

// NewJobManager creates an initialized JobManager.
func NewJobManager(dataDir, logsDir string, fetcher HTTPFetcher, extractor ArticleExtractor) (*JobManager, error) {
	if dataDir == "" {
		dataDir = "data"
	}
	if logsDir == "" {
		logsDir = "logs"
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs dir: %w", err)
	}

	return &JobManager{
		jobs:      make(map[string]*JobState),
		engines:   make(map[string]*Engine),
		cancels:   make(map[string]context.CancelFunc),
		dataDir:   dataDir,
		logsDir:   logsDir,
		fetcher:   fetcher,
		extractor: extractor,
	}, nil
}

// CreateJob registers a new crawling job.
func (jm *JobManager) CreateJob(cfg JobConfig) (*JobState, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("job_%d", time.Now().UnixNano()/1e6)
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 3
	}
	if cfg.OutputPath == "" {
		cfg.OutputPath = filepath.Join(jm.dataDir, fmt.Sprintf("%s_articles.json", cfg.ID))
	}

	state := &JobState{
		Config: cfg,
		Status: JobStatusQueued,
		Telemetry: EngineTelemetry{
			TotalTasks: len(cfg.Tasks),
		},
		StartedAt: time.Now().Format(time.RFC3339),
	}

	jm.jobs[cfg.ID] = state
	return state, nil
}

// StartJob executes a queued or stopped job asynchronously.
func (jm *JobManager) StartJob(ctx context.Context, jobID string) error {
	jm.mu.Lock()
	state, exists := jm.jobs[jobID]
	if !exists {
		jm.mu.Unlock()
		return fmt.Errorf("job not found: %s", jobID)
	}

	if state.Status == JobStatusRunning {
		jm.mu.Unlock()
		return nil
	}

	engCfg := EngineConfig{
		NumWorkers:     state.Config.Workers,
		MinDelay:       state.Config.MinDelay,
		MaxDelay:       state.Config.MaxDelay,
		Timeout:        state.Config.Timeout,
		TargetArticles: state.Config.TargetArticles,
		OutputPath:     state.Config.OutputPath,
	}

	engine, err := NewEngine(engCfg, state.Config.Tasks, jm.fetcher, jm.extractor, nil)
	if err != nil {
		jm.mu.Unlock()
		return fmt.Errorf("failed to initialize crawler engine: %w", err)
	}

	jobCtx, cancel := context.WithCancel(ctx)
	jm.cancels[jobID] = cancel
	jm.engines[jobID] = engine

	state.Status = JobStatusRunning
	state.StartedAt = time.Now().Format(time.RFC3339)

	engine.SetCallbacks(
		func(wID int, art model.Article) {
			// Callback hook for streaming
		},
		func(t EngineTelemetry) {
			jm.mu.Lock()
			if s, ok := jm.jobs[jobID]; ok {
				s.Telemetry = t
			}
			jm.mu.Unlock()
		},
		nil,
	)

	jm.mu.Unlock()

	go func() {
		defer cancel()
		runErr := engine.Run(jobCtx)

		jm.mu.Lock()
		defer jm.mu.Unlock()

		if s, ok := jm.jobs[jobID]; ok {
			s.EndedAt = time.Now().Format(time.RFC3339)
			if runErr != nil && jobCtx.Err() == context.Canceled {
				s.Status = JobStatusStopped
			} else if runErr != nil {
				s.Status = JobStatusFailed
				s.ErrorMessage = runErr.Error()
			} else {
				s.Status = JobStatusCompleted
			}
		}
	}()

	return nil
}

// PauseJob pauses the execution of a running job.
func (jm *JobManager) PauseJob(jobID string) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	state, exists := jm.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	engine, ok := jm.engines[jobID]
	if !ok || state.Status != JobStatusRunning {
		return fmt.Errorf("job is not currently running")
	}

	engine.Pause()
	state.Status = JobStatusPaused
	return nil
}

// ResumeJob resumes a paused job.
func (jm *JobManager) ResumeJob(jobID string) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	state, exists := jm.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	engine, ok := jm.engines[jobID]
	if !ok || state.Status != JobStatusPaused {
		return fmt.Errorf("job is not currently paused")
	}

	engine.Resume()
	state.Status = JobStatusRunning
	return nil
}

// StopJob stops a running or paused job.
func (jm *JobManager) StopJob(jobID string) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	state, exists := jm.jobs[jobID]
	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if cancel, ok := jm.cancels[jobID]; ok {
		cancel()
		delete(jm.cancels, jobID)
	}

	state.Status = JobStatusStopped
	state.EndedAt = time.Now().Format(time.RFC3339)
	return nil
}

// GetJob returns a copy of the state of a job.
func (jm *JobManager) GetJob(jobID string) (*JobState, bool) {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	state, ok := jm.jobs[jobID]
	if !ok {
		return nil, false
	}
	sCopy := *state
	return &sCopy, true
}

// GetAllJobs returns snapshots of all registered jobs.
func (jm *JobManager) GetAllJobs() []*JobState {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	list := make([]*JobState, 0, len(jm.jobs))
	for _, s := range jm.jobs {
		sCopy := *s
		list = append(list, &sCopy)
	}
	return list
}
