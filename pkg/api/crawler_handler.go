package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"maps-scraper/pkg/classifier"
	"maps-scraper/pkg/cleaner"
	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/scheduler"
	"maps-scraper/pkg/source"
	"maps-scraper/pkg/source/detik"
	"maps-scraper/pkg/source/halodoc"
	"maps-scraper/pkg/source/kemenkes"
	"maps-scraper/pkg/source/kompas"
	"maps-scraper/pkg/source/medlineplus"
)


// MedicalSourceInfo describes an approved trusted medical source.
type MedicalSourceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// CrawlRequest specifies the user input parameters for a medical crawling session.
type CrawlRequest struct {
	Topic             string `json:"topic"`
	Source            string `json:"source"`
	Limit             int    `json:"limit"`
	ArticlesPerSource int    `json:"articles_per_source,omitempty"`
	Trigger           string `json:"trigger,omitempty"` // "manual" | "cron"
}

// CrawlStatusResponse conveys the runtime state, telemetry, and articles of a crawling session.
type CrawlStatusResponse struct {
	ID                    string                      `json:"id"`
	RunID                 string                      `json:"run_id,omitempty"`
	SessionName           string                      `json:"session_name,omitempty"`
	SessionNumber         int                         `json:"session_number,omitempty"`
	DisplayName           string                      `json:"display_name,omitempty"`
	Topic                 string                      `json:"topic"`
	Source                string                      `json:"source"`
	Limit                 int                         `json:"limit"`
	ArticlesPerSource     int                         `json:"articles_per_source,omitempty"`
	Status                string                      `json:"status"` // "Waiting", "Running", "Completed", "Failed", "Stopped"
	Collected             int                         `json:"collected"`
	Total                 int                         `json:"total"`
	TotalDatabaseArticles int                         `json:"total_database_articles,omitempty"`
	Percentage            int                         `json:"percentage"`
	TotalNew              int                         `json:"total_new"`
	TotalDuplicate        int                         `json:"total_duplicate"`
	TotalFailed           int                         `json:"total_failed"`
	StartedAt             string                      `json:"started_at,omitempty"`
	FinishedAt            string                      `json:"finished_at,omitempty"`
	DurationSec           float64                     `json:"duration_sec,omitempty"`
	CurrentSource         string                      `json:"current_source"`
	CurrentTitle          string                      `json:"current_title"`
	ErrorMessage          string                      `json:"error_message,omitempty"`
	SourceStats           map[string]model.SourceStat `json:"source_stats,omitempty"`
	Articles              []model.Article             `json:"articles"`
}

// MedicalCrawlerController coordinates live UI crawling requests with the crawler pipeline.
type MedicalCrawlerController struct {
	mu                sync.RWMutex
	current           *CrawlStatusResponse
	sessionHistory    []*CrawlStatusResponse
	articleSessionMap map[string]string
	articleRunMap     map[string]string
	sessionCounter    int
	cancelFunc        context.CancelFunc
	outputPath        string
	classifier        *classifier.Classifier
	cleaner           *cleaner.ArticleCleaner
	runStorage        *crawler.RunStorage
	scheduler         *scheduler.DailyScheduler
}

var (
	defaultController     *MedicalCrawlerController
	defaultControllerOnce sync.Once
)

// GetMedicalCrawlerController provides a singleton controller instance.
func GetMedicalCrawlerController() *MedicalCrawlerController {
	defaultControllerOnce.Do(func() {
		runStorage := crawler.GetRunStorage()

		ctrl := &MedicalCrawlerController{
			outputPath:        "output/articles.json",
			classifier:        classifier.NewDefaultClassifier(),
			cleaner:           cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig()),
			sessionHistory:    make([]*CrawlStatusResponse, 0),
			articleSessionMap: make(map[string]string),
			articleRunMap:     make(map[string]string),
			sessionCounter:    0,
			runStorage:        runStorage,
			current: &CrawlStatusResponse{
				Status:    "Waiting",
				Articles:  []model.Article{},
				Collected: 0,
			},
		}

		// Initialize DailyScheduler (default: 01:00 AM daily WIB, default 100 articles per source)
		schedCfg := scheduler.Config{
			Enabled:           true,
			DailyAtHour:       1,
			DailyAtMinute:     0,
			ArticlesPerSource: 100,
		}
		ctrl.scheduler = scheduler.NewDailyScheduler(schedCfg, func(triggerType string) error {
			req := CrawlRequest{
				Topic:             "All Topics",
				Source:            "all",
				ArticlesPerSource: ctrl.scheduler.GetArticlesPerSource(),
				Trigger:           triggerType,
			}
			_, err := ctrl.StartCrawl(req)
			return err
		}, "data/scheduler_config.json")
		ctrl.scheduler.Start()

		// Preload articles if output file already exists
		if existing, err := crawler.ReadArticlesJSON("output/articles.json"); err == nil && len(existing) > 0 {
			// Find or seed initial baseline run
			latestRun := runStorage.GetLatestRun()
			if latestRun == nil {
				baselineRun, _ := runStorage.CreateRun("Halodoc", "https://www.halodoc.com/artikel", "All Topics", len(existing), "manual")
				if baselineRun != nil {
					baselineRun.Status = model.RunStatusCompleted
					baselineRun.TotalCrawled = len(existing)
					baselineRun.TotalSuccess = len(existing)
					baselineRun.TotalNew = len(existing)
					now := time.Now()
					baselineRun.FinishedAt = &now
					_ = runStorage.UpdateRun(baselineRun)
					latestRun = baselineRun
				}
			}

			ctrl.sessionCounter = 1
			if latestRun != nil {
				ctrl.sessionCounter = latestRun.RunNumber
				ctrl.current.ID = latestRun.ID
				ctrl.current.RunID = latestRun.ID
				ctrl.current.DisplayName = latestRun.DisplayName
				ctrl.current.SessionName = latestRun.DisplayName
				ctrl.current.SessionNumber = latestRun.RunNumber
				ctrl.current.TotalNew = latestRun.TotalNew
				ctrl.current.TotalDuplicate = latestRun.TotalDuplicate
				ctrl.current.TotalFailed = latestRun.TotalFailed
				ctrl.current.DurationSec = latestRun.DurationSec
				ctrl.current.StartedAt = latestRun.StartedAt.Format(time.RFC3339)
				if latestRun.FinishedAt != nil {
					ctrl.current.FinishedAt = latestRun.FinishedAt.Format(time.RFC3339)
				}
			} else {
				ctrl.current.SessionName = "Run #001"
				ctrl.current.SessionNumber = 1
				ctrl.current.DisplayName = "Run #001"
				ctrl.current.RunID = "run_baseline"
				ctrl.current.ID = "run_baseline"
			}

			ctrl.current.Articles = existing
			ctrl.current.Collected = len(existing)
			ctrl.current.TotalDatabaseArticles = len(existing)
			ctrl.current.Status = "Completed"
			ctrl.current.Percentage = 100

			for _, a := range existing {
				rID := runStorage.GetRunForArticle(a.ID)
				if rID == "" {
					rID = "run_20260907_152057_d0f08c8c" // baseline Run #001
				}
				ctrl.articleRunMap[a.ID] = rID

				sName := "Run #001"
				if run, found := runStorage.GetRun(rID); found {
					sName = run.DisplayName
				}
				ctrl.articleSessionMap[a.ID] = sName
			}
		}

		defaultController = ctrl
	})
	return defaultController
}

// handleSources handles GET, POST, DELETE for trusted sources registry.
func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	reg := source.GetRegistry()

	switch r.Method {
	case http.MethodGet:
		items := reg.GetSourceItems()
		_ = json.NewEncoder(w).Encode(items)

	case http.MethodPost:
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid JSON payload: %v", err)})
			return
		}
		trimmedURL := strings.TrimSpace(payload.URL)
		if err := reg.AddSource(trimmedURL); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "added",
			"url":     trimmedURL,
			"sources": reg.GetSourceItems(),
		})

	case http.MethodDelete:
		targetURL := strings.TrimSpace(r.URL.Query().Get("url"))
		if targetURL == "" {
			var payload struct {
				URL string `json:"url"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			targetURL = strings.TrimSpace(payload.URL)
		}
		if targetURL == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "parameter URL tidak boleh kosong"})
			return
		}
		if err := reg.DeleteSource(targetURL); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "deleted",
			"url":     targetURL,
			"sources": reg.GetSourceItems(),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCrawl initiates a new medical article crawl session.
func (s *Server) handleCrawl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json payload: %v", err), http.StatusBadRequest)
		return
	}

	req.Topic = strings.TrimSpace(req.Topic)
	if req.Topic == "" || strings.EqualFold(req.Topic, "all") || strings.EqualFold(req.Topic, "all topics") || strings.EqualFold(req.Topic, "all categories") {
		req.Topic = "All Topics"
	}
	if req.Source == "" {
		req.Source = "all"
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	ctrl := GetMedicalCrawlerController()
	state, err := ctrl.StartCrawl(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(state)
}

// handleCrawlStatus returns the current status and articles of the active or completed session.
func (s *Server) handleCrawlStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctrl := GetMedicalCrawlerController()
	state := ctrl.GetStatus()
	_ = json.NewEncoder(w).Encode(state)
}

// handleCrawlStop halts the active crawl session.
func (s *Server) handleCrawlStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctrl := GetMedicalCrawlerController()
	ctrl.Stop()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

// CrawlSessionItem describes a crawl session and its article count.
type CrawlSessionItem struct {
	Name      string `json:"name"`
	Count     int    `json:"count"`
	IsCurrent bool   `json:"is_current"`
}

// handleCrawlSessions returns an aggregated list of all crawl sessions found in the database and history, optionally filtered by day.
func (s *Server) handleCrawlSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	dayFilter := strings.TrimSpace(r.URL.Query().Get("day"))
	ctrl := GetMedicalCrawlerController()
	sessions := ctrl.GetSessions(dayFilter)
	_ = json.NewEncoder(w).Encode(sessions)
}

// handleRuns returns the list of all historical CrawlRun sessions or handles deletion via query/body.
func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctrl := GetMedicalCrawlerController()

	if r.Method == http.MethodDelete {
		runID := strings.TrimSpace(r.URL.Query().Get("id"))
		if runID == "" && r.Body != nil {
			var bodyReq struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&bodyReq)
			runID = strings.TrimSpace(bodyReq.ID)
		}

		if runID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "run ID is required", "status": "error"})
			return
		}

		deleted, err := ctrl.DeleteRun(runID)
		if err != nil {
			if strings.Contains(err.Error(), "running") {
				w.WriteHeader(http.StatusConflict)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "id": runID, "status": "error"})
			return
		}

		if !deleted {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "run not found", "id": runID, "status": "not_found"})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "deleted",
			"id":      runID,
			"message": "Run successfully deleted",
		})
		return
	}

	runs := ctrl.runStorage.GetAllRuns()
	sourceFilter := strings.TrimSpace(r.URL.Query().Get("source"))
	cronRunIDFilter := strings.TrimSpace(r.URL.Query().Get("cron_run_id"))
	if sourceFilter != "" || cronRunIDFilter != "" {
		filtered := make([]*model.CrawlRun, 0)
		for _, run := range runs {
			if sourceFilter != "" && !strings.EqualFold(run.Source, sourceFilter) && !strings.EqualFold(run.TrustedSource, sourceFilter) {
				continue
			}
			if cronRunIDFilter != "" && !strings.EqualFold(run.CronRunID, cronRunIDFilter) {
				continue
			}
			filtered = append(filtered, run)
		}
		runs = filtered
	}
	_ = json.NewEncoder(w).Encode(runs)
}

// handleRunsToday returns only the runs started on the current local calendar date.
func (s *Server) handleRunsToday(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctrl := GetMedicalCrawlerController()
	runs := ctrl.runStorage.GetTodayRuns()
	latest := ctrl.runStorage.GetLatestRun()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"today_runs": runs,
		"latest_run": latest,
		"count":      len(runs),
	})
}

// handleRunDays returns all distinct calendar dates having crawl runs for Export per Day UI.
func (s *Server) handleRunDays(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctrl := GetMedicalCrawlerController()
	days := ctrl.runStorage.GetRunDates()
	_ = json.NewEncoder(w).Encode(days)
}


// handleRunDetail returns a single run by ID or deletes it if method is DELETE.
func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	runID := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	runID = strings.Trim(runID, "/")

	ctrl := GetMedicalCrawlerController()

	if r.Method == http.MethodDelete {
		if strings.TrimSpace(runID) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "run ID is required", "status": "error"})
			return
		}

		deleted, err := ctrl.DeleteRun(runID)
		if err != nil {
			if strings.Contains(err.Error(), "running") {
				w.WriteHeader(http.StatusConflict)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "id": runID, "status": "error"})
			return
		}

		if !deleted {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "run not found", "id": runID, "status": "not_found"})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "deleted",
			"id":      runID,
			"message": "Run successfully deleted",
		})
		return
	}

	// GET
	run, exists := ctrl.runStorage.GetRun(runID)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "run not found", "id": runID})
		return
	}

	_ = json.NewEncoder(w).Encode(run)
}


// handleSchedulerStatus returns current configuration and execution state of the daily cron scheduler.
func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctrl := GetMedicalCrawlerController()
	st := ctrl.scheduler.GetStatus()
	_ = json.NewEncoder(w).Encode(st)
}

// handleSchedulerTrigger triggers an immediate scheduled crawl job (useful for tests and admin control).
func (s *Server) handleSchedulerTrigger(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctrl := GetMedicalCrawlerController()
	executed, err := ctrl.scheduler.ExecuteRun("cron_manual_trigger")
	if err != nil && !executed {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "skipped",
			"message": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "triggered",
		"message": "Scheduled crawl executed successfully",
	})
}

// SchedulerConfigRequest defines the payload for updating the daily schedule and per-source targets.
type SchedulerConfigRequest struct {
	Time              string `json:"time,omitempty"` // e.g. "02:00", "08:30"
	Hour              *int   `json:"hour,omitempty"`
	Minute            *int   `json:"minute,omitempty"`
	ArticlesPerSource *int   `json:"articles_per_source,omitempty"`
}

// handleSchedulerConfig allows updating the daily schedule hour, minute, and articles_per_source.
func (s *Server) handleSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctrl := GetMedicalCrawlerController()

	if r.Method == http.MethodGet {
		st := ctrl.scheduler.GetStatus()
		_ = json.NewEncoder(w).Encode(st)
		return
	}

	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req SchedulerConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body: " + err.Error()})
		return
	}

	if req.ArticlesPerSource != nil && *req.ArticlesPerSource > 0 {
		_ = ctrl.scheduler.UpdateArticlesPerSource(*req.ArticlesPerSource)
	}

	var hour, minute int
	parsedTime := false

	if strings.TrimSpace(req.Time) != "" {
		parts := strings.Split(strings.TrimSpace(req.Time), ":")
		if len(parts) == 2 {
			h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 == nil && err2 == nil {
				hour = h
				minute = m
				parsedTime = true
			}
		}
		if !parsedTime {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid time format. Expected 'HH:mm' (e.g. 02:00, 08:30)"})
			return
		}
	} else if req.Hour != nil && req.Minute != nil {
		hour = *req.Hour
		minute = *req.Minute
		parsedTime = true
	}

	if parsedTime {
		if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Hour must be between 0-23 and minute between 0-59"})
			return
		}

		if err := ctrl.scheduler.UpdateSchedule(hour, minute); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}

	st := ctrl.scheduler.GetStatus()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":             true,
		"message":             fmt.Sprintf("Daily schedule configuration updated (Schedule: %02d:%02d WIB, Target per source: %d)", st.DailyAtHour, st.DailyAtMinute, st.ArticlesPerSource),
		"daily_schedule":      st.DailySchedule,
		"daily_at_hour":       st.DailyAtHour,
		"daily_at_minute":     st.DailyAtMinute,
		"articles_per_source": st.ArticlesPerSource,
		"next_run_time":       st.NextRunTime,
		"status":              st,
	})
}

// sortArticles sorts a slice of articles by scraped_at (newest or oldest).
func sortArticles(articles []model.Article, sortOrder string) []model.Article {
	res := make([]model.Article, len(articles))
	copy(res, articles)

	if strings.EqualFold(sortOrder, "oldest") {
		sort.SliceStable(res, func(i, j int) bool {
			if res[i].ScrapedAt == "" && res[j].ScrapedAt == "" {
				return res[i].Title < res[j].Title
			}
			if res[i].ScrapedAt == "" {
				return false
			}
			if res[j].ScrapedAt == "" {
				return true
			}
			return res[i].ScrapedAt < res[j].ScrapedAt
		})
	} else {
		// Default: newest first
		sort.SliceStable(res, func(i, j int) bool {
			if res[i].ScrapedAt == "" && res[j].ScrapedAt == "" {
				return res[i].Title < res[j].Title
			}
			if res[i].ScrapedAt == "" {
				return false
			}
			if res[j].ScrapedAt == "" {
				return true
			}
			return res[i].ScrapedAt > res[j].ScrapedAt
		})
	}
	return res
}

// handleArticles handles GET (list with optional sorting and session filter) and DELETE (delete by unique ID).
func (s *Server) handleArticles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctrl := GetMedicalCrawlerController()

	if r.Method == http.MethodDelete {
		w.Header().Set("Content-Type", "application/json")
		// 1. Extract ID from path: /api/articles/{id}
		articleID := strings.TrimPrefix(r.URL.Path, "/api/articles/")
		articleID = strings.TrimPrefix(articleID, "/api/articles")
		articleID = strings.Trim(articleID, "/")

		// 2. Fallback to query parameter: ?id=...
		if articleID == "" {
			articleID = r.URL.Query().Get("id")
		}

		// 3. Fallback to JSON request body
		if articleID == "" && r.Body != nil {
			var bodyReq struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&bodyReq); err == nil {
				articleID = bodyReq.ID
			}
		}

		if strings.TrimSpace(articleID) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "article id is required",
				"status": "error",
			})
			return
		}

		deleted, err := ctrl.DeleteArticle(articleID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  err.Error(),
				"id":     articleID,
				"status": "error",
			})
			return
		}

		if !deleted {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "article not found",
				"id":     articleID,
				"status": "not_found",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "deleted",
			"id":        articleID,
			"remaining": len(ctrl.GetArticles()),
			"message":   "Article successfully deleted",
		})
		return
	}

	// GET: Return articles with optional sorting and session/run_id filtering
	w.Header().Set("Content-Type", "application/json")
	articles := ctrl.GetArticles()

	runIDFilter := strings.TrimSpace(r.URL.Query().Get("run_id"))
	sessionFilter := strings.TrimSpace(r.URL.Query().Get("session"))
	dayFilter := strings.TrimSpace(r.URL.Query().Get("day"))
	filterKey := runIDFilter
	if filterKey == "" {
		filterKey = sessionFilter
	}

	if filterKey != "" && !strings.EqualFold(filterKey, "all") && !strings.EqualFold(filterKey, "all crawls") {
		actualRunID := filterKey
		displayName := filterKey
		if rObj, found := ctrl.runStorage.GetRunByDisplayName(filterKey); found {
			actualRunID = rObj.ID
			displayName = rObj.DisplayName
		} else if rObj, found := ctrl.runStorage.GetRun(filterKey); found {
			actualRunID = rObj.ID
			displayName = rObj.DisplayName
		}

		runArtIDs := ctrl.runStorage.GetArticlesForRun(actualRunID)
		runArtIDMap := make(map[string]bool)
		for _, id := range runArtIDs {
			runArtIDMap[id] = true
		}

		var filtered []model.Article
		for _, a := range articles {
			if strings.EqualFold(a.RunID, actualRunID) || strings.EqualFold(a.RunID, filterKey) || strings.EqualFold(a.CrawlSession, displayName) || strings.EqualFold(a.CrawlSession, filterKey) || runArtIDMap[a.ID] {
				filtered = append(filtered, a)
			}
		}
		if len(filtered) == 0 && ctrl.current != nil && (ctrl.current.RunID == actualRunID || ctrl.current.SessionName == filterKey || ctrl.current.DisplayName == filterKey || ctrl.current.DisplayName == displayName) {
			filtered = ctrl.current.Articles
		}
		articles = filtered
	} else if dayFilter != "" {
		runsOnDay := ctrl.runStorage.GetRunsByDate(dayFilter)
		runIDSet := make(map[string]bool)
		dayArtIDs := make(map[string]bool)
		for _, run := range runsOnDay {
			runIDSet[run.ID] = true
			runIDSet[run.DisplayName] = true
			for _, aID := range ctrl.runStorage.GetArticlesForRun(run.ID) {
				dayArtIDs[aID] = true
			}
		}

		var filtered []model.Article
		for _, a := range articles {
			if runIDSet[a.RunID] || runIDSet[a.CrawlSession] || dayArtIDs[a.ID] {
				filtered = append(filtered, a)
			}
		}
		articles = filtered
	}

	sortParam := r.URL.Query().Get("sort")
	articles = sortArticles(articles, sortParam)

	_ = json.NewEncoder(w).Encode(articles)
}

// handleDownloadArticles streams articles.json as a direct file download.
// Supports:
// - /api/download (Export All, default canonical file)
// - /api/download?run_id=run_... or ?run_id=Run%20%23001 (Export Per Run)
// - /api/download?day=YYYY-MM-DD (Export Per Day)
func (s *Server) handleDownloadArticles(w http.ResponseWriter, r *http.Request) {
	ctrl := GetMedicalCrawlerController()
	runIDParam := strings.TrimSpace(r.URL.Query().Get("run_id"))
	if runIDParam == "" {
		runIDParam = strings.TrimSpace(r.URL.Query().Get("session"))
	}
	dayParam := strings.TrimSpace(r.URL.Query().Get("day"))

	// Case 1 & 2: Filtered export by Run ID or Day
	if (runIDParam != "" && !strings.EqualFold(runIDParam, "all")) || dayParam != "" {
		allArticles := ctrl.GetArticles()
		filtered := make([]model.DiskArticle, 0)

		if runIDParam != "" && !strings.EqualFold(runIDParam, "all") {
			actualRunID := runIDParam
			displayName := runIDParam
			if rObj, found := ctrl.runStorage.GetRunByDisplayName(runIDParam); found {
				actualRunID = rObj.ID
				displayName = rObj.DisplayName
			} else if rObj, found := ctrl.runStorage.GetRun(runIDParam); found {
				actualRunID = rObj.ID
				displayName = rObj.DisplayName
			}

			runArtIDs := ctrl.runStorage.GetArticlesForRun(actualRunID)
			runArtIDMap := make(map[string]bool)
			for _, id := range runArtIDs {
				runArtIDMap[id] = true
			}

			for _, a := range allArticles {
				if strings.EqualFold(a.RunID, actualRunID) || strings.EqualFold(a.RunID, runIDParam) || strings.EqualFold(a.CrawlSession, displayName) || strings.EqualFold(a.CrawlSession, runIDParam) || runArtIDMap[a.ID] {
					filtered = append(filtered, a.ToDiskArticle())
				}
			}

			if len(filtered) == 0 && ctrl.current != nil && (ctrl.current.RunID == actualRunID || ctrl.current.SessionName == runIDParam || ctrl.current.DisplayName == displayName) {
				for _, a := range ctrl.current.Articles {
					filtered = append(filtered, a.ToDiskArticle())
				}
			}

			filename := fmt.Sprintf("articles_%s.json", strings.ReplaceAll(displayName, " ", "_"))
			encoded, _ := json.MarshalIndent(filtered, "", "  ")
			encoded = append(encoded, '\n')

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
			w.Header().Set("Content-Length", strconv.Itoa(len(encoded)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(encoded)
			return
		}

		if dayParam != "" {
			runsOnDay := ctrl.runStorage.GetRunsByDate(dayParam)
			runIDSet := make(map[string]bool)
			dayArtIDs := make(map[string]bool)
			for _, run := range runsOnDay {
				runIDSet[run.ID] = true
				runIDSet[run.DisplayName] = true
				for _, aID := range ctrl.runStorage.GetArticlesForRun(run.ID) {
					dayArtIDs[aID] = true
				}
			}

			for _, a := range allArticles {
				if runIDSet[a.RunID] || runIDSet[a.CrawlSession] || dayArtIDs[a.ID] {
					filtered = append(filtered, a.ToDiskArticle())
				}
			}

			filename := fmt.Sprintf("articles_%s.json", dayParam)
			encoded, _ := json.MarshalIndent(filtered, "", "  ")
			encoded = append(encoded, '\n')

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
			w.Header().Set("Content-Length", strconv.Itoa(len(encoded)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(encoded)
			return
		}
	}

	// Case 3: Export All (Default canonical output)
	outputPath := ctrl.outputPath
	data, err := os.ReadFile(outputPath)
	if err != nil {
		// Fallback to in-memory serialized articles
		articles := ctrl.GetArticles()
		diskArts := make([]model.DiskArticle, len(articles))
		for i, a := range articles {
			diskArts[i] = a.ToDiskArticle()
		}
		encoded, errMarshal := json.MarshalIndent(diskArts, "", "  ")
		if errMarshal != nil {
			http.Error(w, "no articles available to download", http.StatusNotFound)
			return
		}
		data = append(encoded, '\n')
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"articles.json\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}


// StartCrawl initiates the crawler session.
func (ctrl *MedicalCrawlerController) StartCrawl(req CrawlRequest) (*CrawlStatusResponse, error) {
	ctrl.mu.Lock()
	if ctrl.current != nil && ctrl.current.Status == "Running" {
		ctrl.mu.Unlock()
		return nil, fmt.Errorf("a crawler job is already running")
	}

	// Archive previous completed/failed session into history if not already archived
	if ctrl.current != nil && ctrl.current.SessionName != "" {
		alreadyArchived := false
		for _, h := range ctrl.sessionHistory {
			if h.ID == ctrl.current.ID {
				alreadyArchived = true
				break
			}
		}
		if !alreadyArchived {
			ctrl.sessionHistory = append(ctrl.sessionHistory, ctrl.current)
		}
	}

	trigger := req.Trigger
	if trigger == "" {
		trigger = "manual"
	}

	srcFilter := strings.TrimSpace(req.Source)
	isAllSources := srcFilter == "" ||
		strings.EqualFold(srcFilter, "all") ||
		strings.EqualFold(srcFilter, "all sources") ||
		strings.EqualFold(srcFilter, "all_sources") ||
		strings.EqualFold(srcFilter, "semua sumber")

	// Calculate overall limit and per-source targets
	reg := source.GetRegistry()
	activeURLs := reg.GetSources()
	numSources := len(activeURLs)
	if numSources == 0 {
		numSources = 1
	}

	var articlesPerSource int
	var limit int

	isCron := strings.Contains(strings.ToLower(trigger), "cron")
	if isCron {
		articlesPerSource = req.ArticlesPerSource
		if articlesPerSource <= 0 {
			if ctrl.scheduler != nil {
				articlesPerSource = ctrl.scheduler.GetArticlesPerSource()
			}
			if articlesPerSource <= 0 {
				articlesPerSource = 100
			}
		}
		limit = numSources * articlesPerSource
	} else if req.ArticlesPerSource > 0 {
		articlesPerSource = req.ArticlesPerSource
		if isAllSources {
			limit = numSources * articlesPerSource
		} else {
			limit = articlesPerSource
		}
	} else {
		limit = req.Limit
		if limit <= 0 {
			limit = 20
		}
		articlesPerSource = 0 // Manual limit mode
	}

	isMultiSource := isCron || (isAllSources && numSources > 1)
	var cronRunID string
	var runID string
	var displayName string
	var runNum int
	var crawlRun *model.CrawlRun

	startedTime := time.Now()

	if isCron {
		cronRunID = fmt.Sprintf("CRON-%s", startedTime.Format("20060102-150405"))
		displayName = cronRunID
		runID = cronRunID
		ctrl.sessionCounter++
		runNum = ctrl.sessionCounter
	} else if isMultiSource {
		cronRunID = fmt.Sprintf("BATCH-%s", startedTime.Format("20060102-150405"))
		displayName = cronRunID
		runID = cronRunID
		ctrl.sessionCounter++
		runNum = ctrl.sessionCounter
	} else {
		// Single-source manual run
		var errRun error
		crawlRun, errRun = ctrl.runStorage.CreateRun(req.Source, "", req.Topic, limit, trigger)
		if errRun == nil && crawlRun != nil {
			runID = crawlRun.ID
			displayName = crawlRun.DisplayName
			runNum = crawlRun.RunNumber
			ctrl.sessionCounter = runNum
		} else {
			ctrl.sessionCounter++
			runNum = ctrl.sessionCounter
			displayName = fmt.Sprintf("Run #%03d", runNum)
			runID = crawler.GenerateRunID(startedTime)
		}
	}

	// Get total existing articles in DB
	totalExisting := 0
	if existing, err := crawler.ReadArticlesJSON(ctrl.outputPath); err == nil {
		totalExisting = len(existing)
	}

	session := &CrawlStatusResponse{
		ID:                    runID,
		RunID:                 runID,
		SessionName:           displayName,
		SessionNumber:         runNum,
		DisplayName:           displayName,
		Topic:                 req.Topic,
		Source:                req.Source,
		Limit:                 limit,
		ArticlesPerSource:     articlesPerSource,
		Status:                "Waiting",
		Collected:             0,
		Total:                 limit,
		TotalDatabaseArticles: totalExisting,
		Percentage:            0,
		StartedAt:             startedTime.Format(time.RFC3339),
		CurrentSource:         "Initializing pipeline...",
		CurrentTitle:          "Preparing workers...",
		SourceStats:           make(map[string]model.SourceStat),
		Articles:              make([]model.Article, 0, limit),
	}
	ctrl.current = session

	ctx, cancel := context.WithCancel(context.Background())
	ctrl.cancelFunc = cancel
	ctrl.mu.Unlock()

	go ctrl.runCrawl(ctx, session, crawlRun, cronRunID, isMultiSource)

	return session, nil
}

func (ctrl *MedicalCrawlerController) runCrawl(ctx context.Context, session *CrawlStatusResponse, crawlRun *model.CrawlRun, cronRunID string, isMultiSource bool) {
	if session.ArticlesPerSource > 0 && (session.Source == "all" || session.Source == "") {
		fmt.Println("[CRON] Starting daily scraping...")
	}

	// Transition to Running
	ctrl.mu.Lock()
	session.Status = "Running"
	session.CurrentSource = "Connecting to approved sources..."
	ctrl.mu.Unlock()

	// Prepare storage
	_ = os.MkdirAll(filepath.Dir(ctrl.outputPath), 0755)
	storage, err := crawler.NewJSONStorage(ctrl.outputPath, true)
	if err != nil {
		ctrl.mu.Lock()
		session.Status = "Failed"
		session.ErrorMessage = fmt.Sprintf("storage init failed: %v", err)
		if crawlRun != nil {
			crawlRun.Status = model.RunStatusFailed
			crawlRun.ErrorMessage = session.ErrorMessage
			now := time.Now()
			crawlRun.FinishedAt = &now
			_ = ctrl.runStorage.UpdateRun(crawlRun)
		}
		ctrl.mu.Unlock()
		return
	}

	dedup := cleaner.NewDeduplicator(0.85)
	existingArts := storage.GetAll()
	for _, ea := range existingArts {
		_ = dedup.CheckArticle(&ea, ea.SourceURL)
	}

	// Dynamically read active trusted source URLs from persistent registry
	reg := source.GetRegistry()
	activeURLs := reg.GetSources()
	if len(activeURLs) == 0 {
		ctrl.mu.Lock()
		session.Status = "Failed"
		session.ErrorMessage = "Daftar Trusted Sources kosong. Silakan tambahkan minimal satu URL sumber terpercaya."
		if crawlRun != nil {
			crawlRun.Status = model.RunStatusFailed
			crawlRun.ErrorMessage = session.ErrorMessage
			now := time.Now()
			crawlRun.FinishedAt = &now
			_ = ctrl.runStorage.UpdateRun(crawlRun)
		}
		ctrl.mu.Unlock()
		return
	}

	type sourceWrapper struct {
		name   string
		search func(ctx context.Context, query string, maxArticles int) ([]model.Article, error)
	}

	var activeSources []sourceWrapper
	srcFilter := strings.TrimSpace(session.Source)
	isAllSources := srcFilter == "" ||
		strings.EqualFold(srcFilter, "all") ||
		strings.EqualFold(srcFilter, "all sources") ||
		strings.EqualFold(srcFilter, "all_sources") ||
		strings.EqualFold(srcFilter, "semua sumber")

	// Prioritize specific article URLs over broad homepage/listing URLs
	sortedURLs := make([]string, len(activeURLs))
	copy(sortedURLs, activeURLs)
	sort.SliceStable(sortedURLs, func(i, j int) bool {
		isSpecificI := (strings.Contains(sortedURLs[i], "/artikel/") && len(sortedURLs[i]) > len("https://www.halodoc.com/artikel")) || strings.Contains(sortedURLs[i], "/d-")
		isSpecificJ := (strings.Contains(sortedURLs[j], "/artikel/") && len(sortedURLs[j]) > len("https://www.halodoc.com/artikel")) || strings.Contains(sortedURLs[j], "/d-")
		if isSpecificI && !isSpecificJ {
			return true
		}
		return false
	})

	for _, u := range sortedURLs {
		// If user specified a specific source filter and this URL doesn't match, skip
		if !isAllSources {
			if !strings.EqualFold(u, srcFilter) && !strings.Contains(strings.ToLower(u), strings.ToLower(srcFilter)) {
				continue
			}
		}

		uLower := strings.ToLower(u)
		if strings.Contains(uLower, "halodoc.com") {
			hAdapter := halodoc.NewAdapter(nil)
			hAdapter.SetSourceURL(u)
			activeSources = append(activeSources, sourceWrapper{
				name:   "Halodoc",
				search: hAdapter.SearchWithLimit,
			})
		} else if strings.Contains(uLower, "detik.com") {
			dAdapter := detik.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name:   "Detik Health",
				search: dAdapter.SearchWithLimit,
			})
		} else if strings.Contains(uLower, "kemkes.go.id") {
			kAdapter := kemenkes.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name: "Kemenkes RI",
				search: func(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
					return kAdapter.Search(ctx, query)
				},
			})
		} else if strings.Contains(uLower, "kompas.com") {
			kmpAdapter := kompas.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name:   "Kompas Health",
				search: kmpAdapter.SearchWithLimit,
			})
		} else if strings.Contains(uLower, "medlineplus.gov") {
			mAdapter := medlineplus.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name: "NIH MedlinePlus",
				search: func(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
					return mAdapter.Search(ctx, query)
				},
			})
		} else {
			targetURL := u
			activeSources = append(activeSources, sourceWrapper{
				name: targetURL,
				search: func(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
					return ctrl.crawlGenericURL(ctx, targetURL, query)
				},
			})
		}
	}

	// Fallback: If user specified a specific source URL or name directly that wasn't in activeURLs
	if len(activeSources) == 0 && !isAllSources && srcFilter != "" {
		srcLower := strings.ToLower(srcFilter)
		if strings.Contains(srcLower, "detik") {
			dAdapter := detik.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name:   "Detik Health",
				search: dAdapter.SearchWithLimit,
			})
		} else if strings.Contains(srcLower, "halodoc") {
			hAdapter := halodoc.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name:   "Halodoc",
				search: hAdapter.SearchWithLimit,
			})
		} else if strings.Contains(srcLower, "kemkes") {
			kAdapter := kemenkes.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name: "Kemenkes RI",
				search: func(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
					return kAdapter.Search(ctx, query)
				},
			})
		} else if strings.Contains(srcLower, "kompas") {
			kmpAdapter := kompas.NewAdapter(nil)
			activeSources = append(activeSources, sourceWrapper{
				name:   "Kompas Health",
				search: kmpAdapter.SearchWithLimit,
			})
		} else if strings.HasPrefix(srcLower, "http://") || strings.HasPrefix(srcLower, "https://") {
			targetURL := srcFilter
			activeSources = append(activeSources, sourceWrapper{
				name: targetURL,
				search: func(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
					return ctrl.crawlGenericURL(ctx, targetURL, query)
				},
			})
		}
	}

	if len(activeSources) == 0 {
		ctrl.mu.Lock()
		session.Status = "Failed"
		session.ErrorMessage = "no valid sources matched the current selection"
		if crawlRun != nil {
			crawlRun.Status = model.RunStatusFailed
			crawlRun.ErrorMessage = session.ErrorMessage
			now := time.Now()
			crawlRun.FinishedAt = &now
			_ = ctrl.runStorage.UpdateRun(crawlRun)
		}
		ctrl.mu.Unlock()
		return
	}

	// Split topics if comma-separated
	rawTopics := strings.Split(session.Topic, ",")
	var topics []string
	for _, t := range rawTopics {
		tr := strings.TrimSpace(t)
		if tr != "" {
			topics = append(topics, tr)
		}
	}
	if len(topics) == 0 || (len(topics) == 1 && (topics[0] == "" || strings.EqualFold(topics[0], "all") || strings.EqualFold(topics[0], "all topics") || strings.EqualFold(topics[0], "all categories"))) {
		topics = []string{"all"}
	}

	// Determine per-source target
	targetPerSource := session.ArticlesPerSource
	if targetPerSource <= 0 || !isAllSources {
		targetPerSource = session.Limit
	}

	// Process each source INDEPENDENTLY
	for _, src := range activeSources {
		if ctx.Err() != nil {
			break
		}

		ctrl.mu.RLock()
		if session.ArticlesPerSource <= 0 && session.Collected >= session.Limit {
			ctrl.mu.RUnlock()
			break
		}
		ctrl.mu.RUnlock()

		ctrl.mu.Lock()
		session.CurrentSource = src.name
		session.CurrentTitle = fmt.Sprintf("[%s] Target: %d | Starting extraction...", src.name, targetPerSource)

		stat := model.SourceStat{
			Target: targetPerSource,
			Status: "RUNNING",
		}
		if crawlRun != nil {
			if crawlRun.SourceStats == nil {
				crawlRun.SourceStats = make(map[string]model.SourceStat)
			}
			crawlRun.SourceStats[src.name] = stat
		}
		if session.SourceStats == nil {
			session.SourceStats = make(map[string]model.SourceStat)
		}
		session.SourceStats[src.name] = stat
		ctrl.mu.Unlock()

		var currentSourceRun *model.CrawlRun
		if isMultiSource {
			runTrigger := "manual"
			if strings.HasPrefix(cronRunID, "CRON-") {
				runTrigger = "cron"
			} else if crawlRun != nil && crawlRun.Trigger != "" {
				runTrigger = crawlRun.Trigger
			}
			srcRun, errSrcRun := ctrl.runStorage.CreateSourceRun(cronRunID, src.name, "", session.Topic, targetPerSource, runTrigger)
			if errSrcRun == nil && srcRun != nil {
				currentSourceRun = srcRun
			}
		} else {
			currentSourceRun = crawlRun
		}

		sourceStartTime := time.Now()
		if currentSourceRun != nil {
			currentSourceRun.StartedAt = sourceStartTime
			currentSourceRun.Status = model.RunStatusRunning
			_ = ctrl.runStorage.UpdateRun(currentSourceRun)
		}

		maxAttempts := 5
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if ctx.Err() != nil {
				break
			}

			ctrl.mu.RLock()
			currentSuccess := stat.Success
			totalCollected := session.Collected
			ctrl.mu.RUnlock()

			if currentSuccess >= targetPerSource || (session.ArticlesPerSource <= 0 && totalCollected >= session.Limit) {
				break
			}

			neededForSource := targetPerSource - currentSuccess
			collectedInAttempt := 0

			for _, topic := range topics {
				if ctx.Err() != nil || currentSuccess+collectedInAttempt >= targetPerSource {
					break
				}

				fetchLimit := (neededForSource - collectedInAttempt) + stat.Duplicate + 25
				if fetchLimit < 20 {
					fetchLimit = 20
				}
				if fetchLimit > targetPerSource*3+50 {
					fetchLimit = targetPerSource*3 + 50
				}

				articles, err := src.search(ctx, topic, fetchLimit)
				if err != nil {
					// Error isolation: set source status to FAILED, record in source run, and log error
					stat.Status = "FAILED"
					stat.Error = err.Error()
					ctrl.mu.Lock()
					if crawlRun != nil {
						crawlRun.SourceStats[src.name] = stat
					}
					session.SourceStats[src.name] = stat
					if currentSourceRun != nil {
						currentSourceRun.Status = model.RunStatusFailed
						currentSourceRun.ErrorMessage = err.Error()
						now := time.Now()
						currentSourceRun.FinishedAt = &now
						currentSourceRun.DurationSec = now.Sub(sourceStartTime).Seconds()
						currentSourceRun.Duration = currentSourceRun.DurationSec
						_ = ctrl.runStorage.UpdateRun(currentSourceRun)
					}
					ctrl.mu.Unlock()

					fmt.Printf("[%s] Search failed: %v\n", src.name, err)
					break // Break pagination loop for this failed source and continue to next source
				}

				for _, rawArt := range articles {
					ctrl.mu.RLock()
					stopManual := session.ArticlesPerSource <= 0 && session.Collected >= session.Limit
					ctrl.mu.RUnlock()
					if ctx.Err() != nil || currentSuccess+collectedInAttempt >= targetPerSource || stopManual {
						break
					}

					ctrl.mu.Lock()
					stat.Crawled++
					if crawlRun != nil {
						crawlRun.TotalCrawled++
					}
					if currentSourceRun != nil {
						currentSourceRun.TotalCrawled = stat.Crawled
						currentSourceRun.DiscoveredArticles = stat.Crawled
					}
					ctrl.mu.Unlock()

					cleanArt, errClean := ctrl.cleaner.CleanAndValidate(&rawArt)
					if errClean != nil {
						ctrl.mu.Lock()
						stat.Failed++
						session.TotalFailed++
						if crawlRun != nil {
							crawlRun.TotalFailed++
							crawlRun.SourceStats[src.name] = stat
						}
						if currentSourceRun != nil {
							currentSourceRun.TotalFailed = stat.Failed
							currentSourceRun.InvalidArticles = stat.Failed
						}
						session.SourceStats[src.name] = stat
						ctrl.mu.Unlock()
						continue
					}

					if dedupRes := dedup.CheckArticle(cleanArt, cleanArt.SourceURL); dedupRes.IsDuplicate {
						ctrl.mu.Lock()
						stat.Duplicate++
						session.TotalDuplicate++
						if crawlRun != nil {
							crawlRun.TotalDuplicate++
							crawlRun.SourceStats[src.name] = stat
						}
						if currentSourceRun != nil {
							currentSourceRun.TotalDuplicate = stat.Duplicate
							currentSourceRun.DuplicateArticles = stat.Duplicate
						}
						session.SourceStats[src.name] = stat
						ctrl.mu.Unlock()
						continue
					}

					if cleanArt.Category == "" {
						cleanArt.Category = ctrl.classifier.Classify(cleanArt.Title, strings.Join(cleanArt.Description, " "))
					}
					cleanArt.CrawlSession = session.SessionName
					runIDToRecord := session.RunID
					if currentSourceRun != nil {
						runIDToRecord = currentSourceRun.ID
					}
					cleanArt.RunID = runIDToRecord

					isNew, _ := storage.SaveWithResult(*cleanArt)
					if !isNew {
						ctrl.mu.Lock()
						stat.Duplicate++
						session.TotalDuplicate++
						if crawlRun != nil {
							crawlRun.TotalDuplicate++
							crawlRun.SourceStats[src.name] = stat
						}
						if currentSourceRun != nil {
							currentSourceRun.TotalDuplicate = stat.Duplicate
							currentSourceRun.DuplicateArticles = stat.Duplicate
						}
						session.SourceStats[src.name] = stat
						ctrl.mu.Unlock()
						continue
					}

					_ = ctrl.runStorage.RecordArticleRun(runIDToRecord, cleanArt.ID)

					ctrl.mu.Lock()
					ctrl.articleSessionMap[cleanArt.ID] = session.SessionName
					ctrl.articleRunMap[cleanArt.ID] = runIDToRecord
					session.Articles = append(session.Articles, *cleanArt)
					session.Collected = len(session.Articles)
					session.TotalNew++
					session.TotalDatabaseArticles++

					stat.Success++
					stat.New++
					collectedInAttempt++

					if crawlRun != nil {
						crawlRun.TotalSuccess = session.Collected
						crawlRun.TotalNew = session.TotalNew
						crawlRun.TotalDuplicate = session.TotalDuplicate
						crawlRun.SourceStats[src.name] = stat
					}
					if currentSourceRun != nil {
						currentSourceRun.TotalSuccess = stat.Success
						currentSourceRun.ValidArticles = stat.Success
						currentSourceRun.TotalNew = stat.New
						currentSourceRun.SavedArticles = stat.New
						currentSourceRun.TotalDuplicate = stat.Duplicate
						currentSourceRun.DuplicateArticles = stat.Duplicate
					}
					session.SourceStats[src.name] = stat

					session.CurrentTitle = cleanArt.Title
					if session.Limit > 0 {
						session.Percentage = int(float64(session.Collected) / float64(session.Limit) * 100)
						if session.Percentage > 100 {
							session.Percentage = 100
						}
					}
					ctrl.mu.Unlock()

					time.Sleep(50 * time.Millisecond)
				}
			}

			if collectedInAttempt == 0 {
				// No new unique articles were collected in this pagination pass for this source
				break
			}
		}

		// Finalize source status
		ctrl.mu.Lock()
		if stat.Status != "FAILED" {
			if stat.Success >= targetPerSource {
				stat.Status = "SUCCESS"
			} else if stat.Success > 0 {
				stat.Status = "PARTIAL"
			} else {
				stat.Status = "NO_NEW_ARTICLES"
			}
		}
		if crawlRun != nil {
			crawlRun.SourceStats[src.name] = stat
		}
		session.SourceStats[src.name] = stat

		sourceEndTime := time.Now()
		if currentSourceRun != nil {
			currentSourceRun.FinishedAt = &sourceEndTime
			currentSourceRun.DurationSec = sourceEndTime.Sub(sourceStartTime).Seconds()
			currentSourceRun.Duration = currentSourceRun.DurationSec
			currentSourceRun.TotalCrawled = stat.Crawled
			currentSourceRun.DiscoveredArticles = stat.Crawled
			currentSourceRun.TotalSuccess = stat.Success
			currentSourceRun.ValidArticles = stat.Success
			currentSourceRun.TotalNew = stat.New
			currentSourceRun.SavedArticles = stat.New
			currentSourceRun.TotalDuplicate = stat.Duplicate
			currentSourceRun.DuplicateArticles = stat.Duplicate
			currentSourceRun.TotalFailed = stat.Failed
			currentSourceRun.InvalidArticles = stat.Failed
			if stat.Status == "FAILED" {
				currentSourceRun.Status = model.RunStatusFailed
				currentSourceRun.ErrorMessage = stat.Error
			} else {
				currentSourceRun.Status = model.RunStatusCompleted
			}
			_ = ctrl.runStorage.UpdateRun(currentSourceRun)
		}
		ctrl.mu.Unlock()

		// Structured per-source log line
		fmt.Printf("[%s] Target: %d | Scraped: %d | Success: %d | Duplicate: %d | Failed: %d | Status: %s\n",
			src.name, stat.Target, stat.Crawled, stat.Success, stat.Duplicate, stat.Failed, stat.Status)
	}

	if session.ArticlesPerSource > 0 && (session.Source == "all" || session.Source == "") {
		fmt.Printf("[CRON] Daily scraping completed. Total New: %d, Total Duplicate: %d, Total Failed: %d\n",
			session.TotalNew, session.TotalDuplicate, session.TotalFailed)
	}

	// Flush and close storage
	_ = storage.Close()

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	finishedTime := time.Now()
	session.FinishedAt = finishedTime.Format(time.RFC3339)
	if session.StartedAt != "" {
		if st, parseErr := time.Parse(time.RFC3339, session.StartedAt); parseErr == nil {
			session.DurationSec = finishedTime.Sub(st).Seconds()
		}
	}

	if ctx.Err() != nil {
		session.Status = "Failed"
		session.ErrorMessage = "crawl canceled by user"
		if crawlRun != nil {
			crawlRun.Status = model.RunStatusStopped
			crawlRun.ErrorMessage = session.ErrorMessage
			crawlRun.FinishedAt = &finishedTime
			_ = ctrl.runStorage.UpdateRun(crawlRun)
		}
		return
	}

	if session.Collected == 0 {
		if session.TotalDuplicate > 0 {
			// All discovered articles for this query already exist in database (idempotent run)
			session.Status = "Completed"
			session.Percentage = 100
			session.CurrentSource = "Finished"
			if allInDB, errRead := crawler.ReadArticlesJSON(ctrl.outputPath); errRead == nil {
				session.TotalDatabaseArticles = len(allInDB)
			}
			session.CurrentTitle = fmt.Sprintf("All %d discovered articles already exist in database (0 new added)", session.TotalDuplicate)
			if crawlRun != nil {
				crawlRun.Status = model.RunStatusCompleted
				crawlRun.TotalSuccess = 0
				crawlRun.TotalNew = 0
				crawlRun.TotalDuplicate = session.TotalDuplicate
				crawlRun.FinishedAt = &finishedTime
				_ = ctrl.runStorage.UpdateRun(crawlRun)
			}
			return
		}

		session.Status = "Failed"
		session.ErrorMessage = "no articles found for the specified topic and sources"
		if crawlRun != nil {
			crawlRun.Status = model.RunStatusFailed
			crawlRun.ErrorMessage = session.ErrorMessage
			crawlRun.FinishedAt = &finishedTime
			_ = ctrl.runStorage.UpdateRun(crawlRun)
		}
		return
	}

	// Verify the final JSON output
	if _, errVerify := crawler.VerifyArticlesJSON(ctrl.outputPath); errVerify != nil {
		session.Status = "Failed"
		session.ErrorMessage = fmt.Sprintf("output verification failed: %v", errVerify)
		if crawlRun != nil {
			crawlRun.Status = model.RunStatusFailed
			crawlRun.ErrorMessage = session.ErrorMessage
			crawlRun.FinishedAt = &finishedTime
			_ = ctrl.runStorage.UpdateRun(crawlRun)
		}
		return
	}

	// Strictly set to Completed ONLY after storage and verification succeed
	session.Status = "Completed"
	session.Percentage = 100
	session.CurrentSource = "Finished"
	if allInDB, errRead := crawler.ReadArticlesJSON(ctrl.outputPath); errRead == nil {
		session.TotalDatabaseArticles = len(allInDB)
	}
	session.CurrentTitle = fmt.Sprintf("Saved %d clean articles to articles.json (Total in database: %d)", session.Collected, session.TotalDatabaseArticles)

	if crawlRun != nil {
		hasFailedSource := false
		hasSuccessSource := false
		if crawlRun.SourceStats != nil {
			for _, st := range crawlRun.SourceStats {
				if st.Status == "FAILED" || (st.Failed > 0 && st.Success == 0) {
					hasFailedSource = true
				}
				if st.Status == "SUCCESS" || st.Success > 0 {
					hasSuccessSource = true
				}
			}
		}

		if hasFailedSource && hasSuccessSource {
			crawlRun.Status = model.RunStatusPartialSuccess
		} else {
			crawlRun.Status = model.RunStatusCompleted
		}

		crawlRun.FinishedAt = &finishedTime
		crawlRun.TotalSuccess = session.Collected
		crawlRun.TotalNew = session.TotalNew
		crawlRun.TotalDuplicate = session.TotalDuplicate
		crawlRun.TotalFailed = session.TotalFailed
		_ = ctrl.runStorage.UpdateRun(crawlRun)
	}
}

// GetStatus returns a snapshot of the current crawler state.
func (ctrl *MedicalCrawlerController) GetStatus() CrawlStatusResponse {
	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()

	if ctrl.current == nil {
		return CrawlStatusResponse{
			Status:    "Waiting",
			Articles:  []model.Article{},
			Collected: 0,
		}
	}

	// Return copy
	resp := *ctrl.current
	resp.Articles = make([]model.Article, len(ctrl.current.Articles))
	copy(resp.Articles, ctrl.current.Articles)
	return resp
}

func (ctrl *MedicalCrawlerController) annotateSessionsLocked(articles []model.Article) {
	for i := range articles {
		rID := ""
		if r, ok := ctrl.articleRunMap[articles[i].ID]; ok && r != "" {
			rID = r
		} else if storedR := ctrl.runStorage.GetRunForArticle(articles[i].ID); storedR != "" {
			rID = storedR
			ctrl.articleRunMap[articles[i].ID] = storedR
		} else if articles[i].RunID != "" {
			rID = articles[i].RunID
		}
		articles[i].RunID = rID

		if s, ok := ctrl.articleSessionMap[articles[i].ID]; ok && s != "" {
			articles[i].CrawlSession = s
		} else if rID != "" {
			if run, found := ctrl.runStorage.GetRun(rID); found {
				articles[i].CrawlSession = run.DisplayName
				ctrl.articleSessionMap[articles[i].ID] = run.DisplayName
			}
		}
	}
}

// GetArticles returns all accumulated articles from persistent database storage.
func (ctrl *MedicalCrawlerController) GetArticles() []model.Article {
	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()

	var arts []model.Article
	if fromDisk, err := crawler.ReadArticlesJSON(ctrl.outputPath); err == nil && len(fromDisk) > 0 {
		arts = fromDisk
	} else if ctrl.current != nil && len(ctrl.current.Articles) > 0 {
		arts = make([]model.Article, len(ctrl.current.Articles))
		copy(arts, ctrl.current.Articles)
	} else {
		return []model.Article{}
	}

	ctrl.annotateSessionsLocked(arts)
	return arts
}

// RunSessionItem describes a crawl run session with display name, ID, and article count.
type RunSessionItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	IsCurrent bool   `json:"is_current"`
}

// GetSessions returns an aggregated summary of all crawl runs with counts, optionally filtered by day (YYYY-MM-DD).
func (ctrl *MedicalCrawlerController) GetSessions(dayFilter ...string) []CrawlSessionItem {
	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()

	var articles []model.Article
	if arts, err := crawler.ReadArticlesJSON(ctrl.outputPath); err == nil {
		articles = arts
	} else if ctrl.current != nil && len(ctrl.current.Articles) > 0 {
		articles = ctrl.current.Articles
	}

	ctrl.annotateSessionsLocked(articles)

	targetDay := ""
	if len(dayFilter) > 0 {
		targetDay = strings.TrimSpace(dayFilter[0])
	}

	if targetDay != "" {
		runsOnDay := ctrl.runStorage.GetRunsByDate(targetDay)
		runIDSet := make(map[string]bool)
		dayArtIDs := make(map[string]bool)
		for _, run := range runsOnDay {
			runIDSet[run.ID] = true
			runIDSet[run.DisplayName] = true
			for _, aID := range ctrl.runStorage.GetArticlesForRun(run.ID) {
				dayArtIDs[aID] = true
			}
		}

		var filtered []model.Article
		for _, a := range articles {
			if runIDSet[a.RunID] || runIDSet[a.CrawlSession] || dayArtIDs[a.ID] {
				filtered = append(filtered, a)
			}
		}
		articles = filtered
	}

	sessionCounts := make(map[string]int)
	var sessionOrder []string

	for _, a := range articles {
		sName := a.CrawlSession
		if sName == "" {
			sName = "Run #001"
		}
		if _, exists := sessionCounts[sName]; !exists {
			sessionOrder = append(sessionOrder, sName)
		}
		sessionCounts[sName]++
	}

	allLabel := "All Crawls"
	if targetDay != "" {
		allLabel = "All Runs on Date"
	}

	res := []CrawlSessionItem{
		{
			Name:  allLabel,
			Count: len(articles),
		},
	}

	for _, sName := range sessionOrder {
		res = append(res, CrawlSessionItem{
			Name:      sName,
			Count:     sessionCounts[sName],
			IsCurrent: ctrl.current != nil && (ctrl.current.DisplayName == sName || ctrl.current.SessionName == sName),
		})
	}

	return res
}

// DeleteArticle atomically removes an article by its unique ID from persistent storage and in-memory state.
func (ctrl *MedicalCrawlerController) DeleteArticle(id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, fmt.Errorf("article ID cannot be empty")
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// Load existing articles
	arts, err := crawler.ReadArticlesJSON(ctrl.outputPath)
	if err != nil {
		if ctrl.current != nil && len(ctrl.current.Articles) > 0 {
			arts = ctrl.current.Articles
		} else {
			return false, fmt.Errorf("failed to read articles from %s: %w", ctrl.outputPath, err)
		}
	}

	targetIdx := -1
	for i, a := range arts {
		artID := a.ID
		if artID == "" {
			artID = model.GenerateArticleID(a.Title, a.SourceURL)
		}
		if artID == id {
			targetIdx = i
			break
		}
	}

	if targetIdx == -1 {
		return false, nil
	}

	// Remove item while preserving order
	updated := make([]model.Article, 0, len(arts)-1)
	updated = append(updated, arts[:targetIdx]...)
	updated = append(updated, arts[targetIdx+1:]...)

	delete(ctrl.articleSessionMap, id)

	diskItems := make([]model.DiskArticle, len(updated))
	for i, a := range updated {
		diskItems[i] = a.ToDiskArticle()
	}

	// Marshal indented JSON
	data, err := json.MarshalIndent(diskItems, "", "  ")
	if err != nil {
		return false, fmt.Errorf("failed to marshal articles: %w", err)
	}
	data = append(data, '\n')

	// Atomically write file
	if err := crawler.SafeWriteFile(ctrl.outputPath, data); err != nil {
		return false, fmt.Errorf("failed to persist updated articles: %w", err)
	}

	// Update in-memory session without resetting job progress or other articles
	if ctrl.current != nil {
		var filteredCurrent []model.Article
		for _, a := range ctrl.current.Articles {
			artID := a.ID
			if artID == "" {
				artID = model.GenerateArticleID(a.Title, a.SourceURL)
			}
			if artID != id {
				filteredCurrent = append(filteredCurrent, a)
			}
		}
		ctrl.current.Articles = filteredCurrent
		ctrl.current.Collected = len(filteredCurrent)
		ctrl.current.TotalDatabaseArticles = len(updated)
	}

	return true, nil
}

// DeleteRun removes a specific crawl run session, cleans its article mappings, and removes its isolated articles.
func (ctrl *MedicalCrawlerController) DeleteRun(runID string) (bool, error) {
	normID := strings.TrimSpace(runID)
	if normID == "" {
		return false, fmt.Errorf("run ID cannot be empty")
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// Check if run is currently executing
	if ctrl.current != nil && (ctrl.current.RunID == normID || ctrl.current.ID == normID || strings.EqualFold(ctrl.current.DisplayName, normID)) && ctrl.current.Status == "Running" {
		return false, fmt.Errorf("cannot delete an actively running crawl session (%s)", normID)
	}

	// Lookup actual run object to get canonical ID and display name
	var actualID string
	var displayName string
	if runObj, found := ctrl.runStorage.GetRun(normID); found {
		actualID = runObj.ID
		displayName = runObj.DisplayName
	} else if runObj, found := ctrl.runStorage.GetRunByDisplayName(normID); found {
		actualID = runObj.ID
		displayName = runObj.DisplayName
	}

	if actualID == "" {
		return false, nil
	}

	// Get all article IDs associated with this run
	targetArtIDs := ctrl.runStorage.GetArticlesForRun(actualID)
	targetArtMap := make(map[string]bool, len(targetArtIDs))
	for _, id := range targetArtIDs {
		targetArtMap[id] = true
	}

	// Delete from RunStorage (runs.json and run_articles.json)
	deleted, err := ctrl.runStorage.DeleteRun(actualID)
	if err != nil {
		return false, fmt.Errorf("failed to delete run from storage: %w", err)
	}
	if !deleted {
		return false, nil
	}

	// Clean in-memory session mappings
	for _, id := range targetArtIDs {
		delete(ctrl.articleSessionMap, id)
		delete(ctrl.articleRunMap, id)
	}

	// Remove from sessionHistory
	var updatedHistory []*CrawlStatusResponse
	for _, h := range ctrl.sessionHistory {
		if h.ID != actualID && h.RunID != actualID && !strings.EqualFold(h.DisplayName, displayName) {
			updatedHistory = append(updatedHistory, h)
		}
	}
	ctrl.sessionHistory = updatedHistory

	// Filter articles from output/articles.json
	allArticles, err := crawler.ReadArticlesJSON(ctrl.outputPath)
	if err == nil {
		var remainingArticles []model.Article
		for _, a := range allArticles {
			// If article was produced specifically by this run, omit it
			if strings.EqualFold(a.RunID, actualID) || strings.EqualFold(a.CrawlSession, displayName) || targetArtMap[a.ID] {
				continue
			}
			remainingArticles = append(remainingArticles, a)
		}

		diskItems := make([]model.DiskArticle, len(remainingArticles))
		for i, a := range remainingArticles {
			diskItems[i] = a.ToDiskArticle()
		}

		data, errMarshal := json.MarshalIndent(diskItems, "", "  ")
		if errMarshal == nil {
			data = append(data, '\n')
			_ = crawler.SafeWriteFile(ctrl.outputPath, data)
		}

		// If current points to deleted run, update current
		if ctrl.current != nil && (ctrl.current.RunID == actualID || ctrl.current.ID == actualID || strings.EqualFold(ctrl.current.DisplayName, displayName)) {
			latestRun := ctrl.runStorage.GetLatestRun()
			if latestRun != nil {
				ctrl.current.ID = latestRun.ID
				ctrl.current.RunID = latestRun.ID
				ctrl.current.DisplayName = latestRun.DisplayName
				ctrl.current.SessionName = latestRun.DisplayName
				ctrl.current.SessionNumber = latestRun.RunNumber
				ctrl.current.TotalNew = latestRun.TotalNew
				ctrl.current.TotalDuplicate = latestRun.TotalDuplicate
				ctrl.current.TotalFailed = latestRun.TotalFailed
				ctrl.current.DurationSec = latestRun.DurationSec
				ctrl.current.StartedAt = latestRun.StartedAt.Format(time.RFC3339)
				if latestRun.FinishedAt != nil {
					ctrl.current.FinishedAt = latestRun.FinishedAt.Format(time.RFC3339)
				}
				ctrl.current.Status = string(latestRun.Status)
			} else {
				ctrl.current.ID = ""
				ctrl.current.RunID = ""
				ctrl.current.DisplayName = ""
				ctrl.current.SessionName = ""
				ctrl.current.Status = "Waiting"
			}
			ctrl.current.Articles = remainingArticles
			ctrl.current.Collected = len(remainingArticles)
			ctrl.current.TotalDatabaseArticles = len(remainingArticles)
		}
	}

	return true, nil
}

// Stop cancels the running crawl.

func (ctrl *MedicalCrawlerController) Stop() {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	if ctrl.cancelFunc != nil {
		ctrl.cancelFunc()
	}
}

func (ctrl *MedicalCrawlerController) crawlGenericURL(ctx context.Context, pageURL, query string) ([]model.Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MedicalArticleCrawler/2.0 (Authoritative Health Aggregator)")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	bodyStr := string(bodyBytes)
	hAdapter := halodoc.NewAdapter(nil)
	art, err := hAdapter.ParseArticleHTML(bodyStr, pageURL, query)
	if err != nil {
		return nil, err
	}
	return []model.Article{art}, nil
}
