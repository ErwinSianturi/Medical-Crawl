package model

import (
	"time"
)

// RunStatus represents the execution lifecycle status of a crawling run.
type RunStatus string

const (
	RunStatusQueued         RunStatus = "QUEUED"
	RunStatusRunning        RunStatus = "RUNNING"
	RunStatusCompleted      RunStatus = "COMPLETED"
	RunStatusPartialSuccess RunStatus = "PARTIAL_SUCCESS"
	RunStatusFailed         RunStatus = "FAILED"
	RunStatusStopped        RunStatus = "STOPPED"
)

// SourceStat captures granular crawling metrics for an individual source in a run.
type SourceStat struct {
	Target    int    `json:"target,omitempty"`
	Crawled   int    `json:"crawled"`
	Success   int    `json:"success"`
	Failed    int    `json:"failed"`
	New       int    `json:"new"`
	Duplicate int    `json:"duplicate"`
	Status    string `json:"status"` // "SUCCESS", "FAILED", "PARTIAL"
	Error     string `json:"error,omitempty"`
}

// CrawlRun captures all metadata, telemetry, and statistics for an individual crawling execution session or source-specific CRON entry.
type CrawlRun struct {
	ID                 string                `json:"id"`
	HistoryID          string                `json:"history_id,omitempty"`
	CronRunID          string                `json:"cron_run_id,omitempty"`
	RunNumber          int                   `json:"run_number"`
	DisplayName        string                `json:"display_name"`
	Source             string                `json:"source"`
	TrustedSource      string                `json:"trusted_source,omitempty"`
	SourceURL          string                `json:"source_url,omitempty"`
	Topic              string                `json:"topic,omitempty"`
	Limit              int                   `json:"limit,omitempty"`
	TargetArticles     int                   `json:"target_articles,omitempty"`
	Trigger            string                `json:"trigger"` // "manual" | "cron"
	Status             RunStatus             `json:"status"`  // QUEUED, RUNNING, COMPLETED, SUCCESS, FAILED, STOPPED
	StartedAt          time.Time             `json:"started_at"`
	FinishedAt         *time.Time            `json:"finished_at,omitempty"`
	DurationSec        float64               `json:"duration_sec"`
	Duration           float64               `json:"duration,omitempty"`
	TotalCrawled       int                   `json:"total_crawled"`
	DiscoveredArticles int                   `json:"discovered_articles,omitempty"`
	ValidArticles      int                   `json:"valid_articles,omitempty"`
	TotalSuccess       int                   `json:"total_success"`
	TotalNew           int                   `json:"total_new"`
	SavedArticles      int                   `json:"saved_articles,omitempty"`
	TotalDuplicate     int                   `json:"total_duplicate"`
	DuplicateArticles  int                   `json:"duplicate_articles,omitempty"`
	TotalFailed        int                   `json:"total_failed"`
	InvalidArticles    int                   `json:"invalid_articles,omitempty"`
	SourceStats        map[string]SourceStat `json:"source_stats,omitempty"`
	ErrorMessage       string                `json:"error_message,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
}

// SyncFields synchronizes spec fields and legacy fields for bidirectional consistency.
func (r *CrawlRun) SyncFields() {
	if r.ID == "" && r.HistoryID != "" {
		r.ID = r.HistoryID
	}
	if r.HistoryID == "" && r.ID != "" {
		r.HistoryID = r.ID
	}

	if r.Source == "" && r.TrustedSource != "" {
		r.Source = r.TrustedSource
	}
	if r.TrustedSource == "" && r.Source != "" {
		r.TrustedSource = r.Source
	}

	if r.TargetArticles == 0 && r.Limit > 0 {
		r.TargetArticles = r.Limit
	}
	if r.Limit == 0 && r.TargetArticles > 0 {
		r.Limit = r.TargetArticles
	}

	if r.DiscoveredArticles == 0 && r.TotalCrawled > 0 {
		r.DiscoveredArticles = r.TotalCrawled
	}
	if r.TotalCrawled == 0 && r.DiscoveredArticles > 0 {
		r.TotalCrawled = r.DiscoveredArticles
	}

	if r.SavedArticles == 0 && (r.TotalNew > 0 || r.TotalSuccess > 0) {
		if r.TotalNew > 0 {
			r.SavedArticles = r.TotalNew
		} else {
			r.SavedArticles = r.TotalSuccess
		}
	}
	if r.TotalSuccess == 0 && r.SavedArticles > 0 {
		r.TotalSuccess = r.SavedArticles
	}
	if r.TotalNew == 0 && r.SavedArticles > 0 {
		r.TotalNew = r.SavedArticles
	}

	if r.ValidArticles == 0 && r.TotalSuccess > 0 {
		r.ValidArticles = r.TotalSuccess
	}

	if r.DuplicateArticles == 0 && r.TotalDuplicate > 0 {
		r.DuplicateArticles = r.TotalDuplicate
	}
	if r.TotalDuplicate == 0 && r.DuplicateArticles > 0 {
		r.TotalDuplicate = r.DuplicateArticles
	}

	if r.InvalidArticles == 0 && r.TotalFailed > 0 {
		r.InvalidArticles = r.TotalFailed
	}
	if r.TotalFailed == 0 && r.InvalidArticles > 0 {
		r.TotalFailed = r.InvalidArticles
	}

	if r.Duration == 0 && r.DurationSec > 0 {
		r.Duration = r.DurationSec
	}
	if r.DurationSec == 0 && r.Duration > 0 {
		r.DurationSec = r.Duration
	}
}

