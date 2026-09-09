package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

var (
	// WIBLocation is the canonical time.Location for Western Indonesia Time (UTC+7).
	WIBLocation = time.FixedZone("WIB", 7*3600)
)

func init() {
	if loc, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		WIBLocation = loc
	}
}

// Config defines the scheduling behavior for daily crawling.
type Config struct {
	Enabled           bool          `json:"enabled"`
	DailyAtHour       int           `json:"daily_at_hour"`        // 0-23, e.g. 1 for 01:00 AM
	DailyAtMinute     int           `json:"daily_at_minute"`      // 0-59
	ArticlesPerSource int           `json:"articles_per_source"`  // Target per trusted source (default 100)
	Interval          time.Duration `json:"interval"`             // Optional periodic interval fallback if > 0
}

// Status represents the runtime status of the scheduler.
type Status struct {
	Enabled           bool      `json:"enabled"`
	IsRunning         bool      `json:"is_running"`
	IsJobActive       bool      `json:"is_job_active"`
	DailySchedule     string    `json:"daily_schedule"`
	DailyAtHour       int       `json:"daily_at_hour"`
	DailyAtMinute     int       `json:"daily_at_minute"`
	ArticlesPerSource int       `json:"articles_per_source"`
	NextRunTime       time.Time `json:"next_run_time"`
	LastRunTime       time.Time `json:"last_run_time,omitempty"`
	LastRunStatus     string    `json:"last_run_status,omitempty"`
}

// DailyScheduler manages cron/periodic execution with safety locking and idempotency.
type DailyScheduler struct {
	mu            sync.RWMutex
	cfg           Config
	storagePath   string
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	isJobActive   bool // mutual exclusion lock flag to prevent duplicate/overlapping runs
	jobMu         sync.Mutex
	nextRunTime   time.Time
	lastRunTime   time.Time
	lastRunStatus string
	triggerFunc   func(triggerType string) error
}

// NewDailyScheduler creates a new scheduler instance.
func NewDailyScheduler(cfg Config, triggerFunc func(triggerType string) error, storagePath ...string) *DailyScheduler {
	var sPath string
	if len(storagePath) > 0 && storagePath[0] != "" {
		sPath = storagePath[0]
		// Load persisted config if file exists
		if data, err := os.ReadFile(sPath); err == nil {
			var loadedCfg Config
			if err := json.Unmarshal(data, &loadedCfg); err == nil {
				cfg.Enabled = loadedCfg.Enabled
				cfg.DailyAtHour = loadedCfg.DailyAtHour
				cfg.DailyAtMinute = loadedCfg.DailyAtMinute
				if loadedCfg.ArticlesPerSource > 0 {
					cfg.ArticlesPerSource = loadedCfg.ArticlesPerSource
				}
			}
		}
	}

	// Environment variable override CRON_ARTICLES_PER_SOURCE
	if envVal := os.Getenv("CRON_ARTICLES_PER_SOURCE"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			cfg.ArticlesPerSource = val
		}
	}

	if cfg.DailyAtHour < 0 || cfg.DailyAtHour > 23 {
		cfg.DailyAtHour = 1 // default 01:00 AM
	}
	if cfg.DailyAtMinute < 0 || cfg.DailyAtMinute > 59 {
		cfg.DailyAtMinute = 0
	}
	if cfg.ArticlesPerSource <= 0 {
		cfg.ArticlesPerSource = 100 // default 100 articles per source
	}

	return &DailyScheduler{
		cfg:         cfg,
		storagePath: sPath,
		triggerFunc: triggerFunc,
	}
}

// CalculateNextRun computes the upcoming execution time based on DailyAtHour and DailyAtMinute in WIB.
func (s *DailyScheduler) CalculateNextRun(from time.Time) time.Time {
	fromWIB := from.In(WIBLocation)
	target := time.Date(fromWIB.Year(), fromWIB.Month(), fromWIB.Day(), s.cfg.DailyAtHour, s.cfg.DailyAtMinute, 0, 0, WIBLocation)
	if !target.After(fromWIB) {
		target = target.Add(24 * time.Hour)
	}
	return target
}

// Start activates the scheduler loop in a background goroutine.
func (s *DailyScheduler) Start() {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	s.isRunning = true
	s.nextRunTime = s.CalculateNextRun(time.Now())
	s.mu.Unlock()

	go s.loop(ctx)
}

// Stop halts the scheduler.
func (s *DailyScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.isRunning = false
}

func (s *DailyScheduler) loop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.RLock()
			enabled := s.cfg.Enabled
			next := s.nextRunTime
			s.mu.RUnlock()

			if !enabled {
				continue
			}

			if now.After(next) || now.Equal(next) {
				s.mu.Lock()
				s.nextRunTime = s.CalculateNextRun(now.Add(2 * time.Minute))
				s.mu.Unlock()

				s.ExecuteRun("cron")
			}
		}
	}
}

// ExecuteRun executes the crawl job with concurrency locking (idempotency safety).
// If a job is already in progress, duplicate runs are skipped safely.
func (s *DailyScheduler) ExecuteRun(triggerType string) (bool, error) {
	s.jobMu.Lock()
	s.mu.Lock()
	if s.isJobActive {
		s.mu.Unlock()
		s.jobMu.Unlock()
		log.Printf("[SCHEDULER] Skipping run (%s): previous job is still running", triggerType)
		return false, fmt.Errorf("job is already running, skipped duplicate trigger")
	}

	s.isJobActive = true
	s.lastRunTime = time.Now()
	s.lastRunStatus = "RUNNING"
	s.mu.Unlock()
	s.jobMu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isJobActive = false
		s.mu.Unlock()
	}()

	log.Printf("[SCHEDULER] Executing scheduled crawl run (%s)...", triggerType)
	var err error
	if s.triggerFunc != nil {
		err = s.triggerFunc(triggerType)
	}

	s.mu.Lock()
	if err != nil {
		s.lastRunStatus = fmt.Sprintf("FAILED: %v", err)
	} else {
		s.lastRunStatus = "COMPLETED"
	}
	s.mu.Unlock()

	return true, err
}

// GetStatus returns the current snapshot of scheduler telemetry.
func (s *DailyScheduler) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	schedStr := fmt.Sprintf("%02d:%02d WIB Daily", s.cfg.DailyAtHour, s.cfg.DailyAtMinute)
	return Status{
		Enabled:           s.cfg.Enabled,
		IsRunning:         s.isRunning,
		IsJobActive:       s.isJobActive,
		DailySchedule:     schedStr,
		DailyAtHour:       s.cfg.DailyAtHour,
		DailyAtMinute:     s.cfg.DailyAtMinute,
		ArticlesPerSource: s.cfg.ArticlesPerSource,
		NextRunTime:       s.nextRunTime,
		LastRunTime:       s.lastRunTime,
		LastRunStatus:     s.lastRunStatus,
	}
}

// GetArticlesPerSource returns the current per-source article target.
func (s *DailyScheduler) GetArticlesPerSource() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.ArticlesPerSource <= 0 {
		return 100
	}
	return s.cfg.ArticlesPerSource
}

// UpdateArticlesPerSource dynamically updates the per-source article limit.
func (s *DailyScheduler) UpdateArticlesPerSource(limit int) error {
	if limit <= 0 {
		return errors.New("articles_per_source must be greater than 0")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg.ArticlesPerSource = limit
	if s.storagePath != "" {
		_ = s.saveConfigLocked()
	}

	log.Printf("[SCHEDULER] Articles per source target updated to %d", limit)
	return nil
}

// UpdateSchedule updates the daily execution time and recalculates nextRunTime immediately.
func (s *DailyScheduler) UpdateSchedule(hour, minute int) error {
	if hour < 0 || hour > 23 {
		return errors.New("hour must be between 0 and 23")
	}
	if minute < 0 || minute > 59 {
		return errors.New("minute must be between 0 and 59")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg.DailyAtHour = hour
	s.cfg.DailyAtMinute = minute
	s.nextRunTime = s.CalculateNextRun(time.Now())

	if s.storagePath != "" {
		_ = s.saveConfigLocked()
	}

	log.Printf("[SCHEDULER] Daily schedule updated to %02d:%02d WIB (Next run: %s)",
		hour, minute, s.nextRunTime.In(WIBLocation).Format(time.RFC3339))
	return nil
}

func (s *DailyScheduler) saveConfigLocked() error {
	if s.storagePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.storagePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmpPath := fmt.Sprintf("%s.tmp.%d", s.storagePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	_ = os.Remove(s.storagePath)
	if err := os.Rename(tmpPath, s.storagePath); err != nil {
		_ = os.Remove(tmpPath)
		return os.WriteFile(s.storagePath, data, 0644)
	}
	return nil
}

// SetEnabled dynamically enables or disables the scheduled execution.
func (s *DailyScheduler) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Enabled = enabled
	if enabled && s.nextRunTime.IsZero() {
		s.nextRunTime = s.CalculateNextRun(time.Now())
	}
	if s.storagePath != "" {
		_ = s.saveConfigLocked()
	}
}
