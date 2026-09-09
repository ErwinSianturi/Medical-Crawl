package crawler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"maps-scraper/pkg/model"
)

// DefaultRunStoragePath defines the persistent file location for crawling runs.
const DefaultRunStoragePath = "data/runs.json"
const DefaultRunArticlesPath = "data/run_articles.json"

// RunStorage manages thread-safe, persistent storage of CrawlRun metadata and Run-to-Article mappings.
type RunStorage struct {
	mu              sync.RWMutex
	storagePath     string
	articlesMapPath string
	runs            map[string]*model.CrawlRun
	runList         []*model.CrawlRun // sorted newest first
	runArticleIDs   map[string][]string // run_id -> []article_id
	articleToRun    map[string]string   // article_id -> run_id
	counter         int
}

var (
	defaultRunStorage     *RunStorage
	defaultRunStorageOnce sync.Once
)

// GetRunStorage returns the singleton instance of RunStorage.
func GetRunStorage() *RunStorage {
	defaultRunStorageOnce.Do(func() {
		defaultRunStorage = NewRunStorage(DefaultRunStoragePath)
	})
	return defaultRunStorage
}

// NewRunStorage creates a new RunStorage instance.
func NewRunStorage(storagePath string) *RunStorage {
	if storagePath == "" {
		storagePath = DefaultRunStoragePath
	}
	dir := filepath.Dir(storagePath)
	mapPath := filepath.Join(dir, "run_articles.json")

	rs := &RunStorage{
		storagePath:     storagePath,
		articlesMapPath: mapPath,
		runs:            make(map[string]*model.CrawlRun),
		runList:         make([]*model.CrawlRun, 0),
		runArticleIDs:   make(map[string][]string),
		articleToRun:    make(map[string]string),
		counter:         0,
	}

	_ = rs.load()
	_ = rs.loadArticleMappings()
	return rs
}

func (rs *RunStorage) load() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	data, err := os.ReadFile(rs.storagePath)
	if err != nil {
		return err
	}

	var loaded []*model.CrawlRun
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	rs.runs = make(map[string]*model.CrawlRun)
	maxNum := 0
	for _, r := range loaded {
		if r.ID != "" || r.HistoryID != "" {
			r.SyncFields()
			rs.runs[r.ID] = r
			if r.RunNumber > maxNum {
				maxNum = r.RunNumber
			}
		}
	}
	rs.counter = maxNum
	rs.rebuildSortedListLocked()
	return nil
}

func (rs *RunStorage) loadArticleMappings() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	data, err := os.ReadFile(rs.articlesMapPath)
	if err != nil {
		return err
	}

	var loaded map[string][]string
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	rs.runArticleIDs = loaded
	rs.articleToRun = make(map[string]string)
	for rID, artIDs := range loaded {
		for _, aID := range artIDs {
			rs.articleToRun[aID] = rID
		}
	}
	return nil
}

func (rs *RunStorage) saveLocked() error {
	dir := filepath.Dir(rs.storagePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", rs.storagePath, err)
		}
	}

	data, err := json.MarshalIndent(rs.runList, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpFile := rs.storagePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	_ = os.Remove(rs.storagePath)
	if err := os.Rename(tmpFile, rs.storagePath); err != nil {
		_ = os.Remove(tmpFile)
		return os.WriteFile(rs.storagePath, data, 0644)
	}

	return nil
}

func (rs *RunStorage) saveArticleMappingsLocked() error {
	dir := filepath.Dir(rs.articlesMapPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", rs.articlesMapPath, err)
		}
	}

	data, err := json.MarshalIndent(rs.runArticleIDs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpFile := rs.articlesMapPath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	_ = os.Remove(rs.articlesMapPath)
	if err := os.Rename(tmpFile, rs.articlesMapPath); err != nil {
		_ = os.Remove(tmpFile)
		return os.WriteFile(rs.articlesMapPath, data, 0644)
	}

	return nil
}

func (rs *RunStorage) rebuildSortedListLocked() {
	list := make([]*model.CrawlRun, 0, len(rs.runs))
	for _, r := range rs.runs {
		list = append(list, r)
	}
	// Sort newest first by StartedAt or RunNumber
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].StartedAt.Equal(list[j].StartedAt) {
			return list[i].RunNumber > list[j].RunNumber
		}
		return list[i].StartedAt.After(list[j].StartedAt)
	})
	rs.runList = list
}

// GenerateRunID produces a unique run identifier containing timestamp and random hex.
func GenerateRunID(t time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("run_%s_%s", t.Format("20060102_150405"), hex.EncodeToString(b))
}

// CreateRun initializes and persists a new CrawlRun session.
func (rs *RunStorage) CreateRun(source, sourceURL, topic string, limit int, trigger string) (*model.CrawlRun, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	now := time.Now()
	rs.counter++
	runNum := rs.counter

	runID := GenerateRunID(now)
	displayName := fmt.Sprintf("Run #%03d", runNum)

	if strings.TrimSpace(source) == "" {
		source = "Halodoc"
	}
	if strings.TrimSpace(trigger) == "" {
		trigger = "manual"
	}

	run := &model.CrawlRun{
		ID:             runID,
		HistoryID:      runID,
		RunNumber:      runNum,
		DisplayName:    displayName,
		Source:         source,
		TrustedSource:  source,
		SourceURL:      sourceURL,
		Topic:          topic,
		Limit:          limit,
		TargetArticles: limit,
		Trigger:        trigger,
		Status:         model.RunStatusRunning,
		StartedAt:      now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	run.SyncFields()

	rs.runs[runID] = run
	rs.rebuildSortedListLocked()
	if err := rs.saveLocked(); err != nil {
		return nil, err
	}

	return run, nil
}

// CreateSourceRun initializes and persists a new CrawlRun specifically for a source within a CRON execution or batch session.
func (rs *RunStorage) CreateSourceRun(cronRunID, source, sourceURL, topic string, targetArticles int, trigger string) (*model.CrawlRun, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	now := time.Now()
	rs.counter++
	runNum := rs.counter

	runID := GenerateRunID(now)
	displayName := fmt.Sprintf("Run #%03d", runNum)
	if strings.Contains(strings.ToLower(trigger), "cron") && cronRunID != "" {
		displayName = fmt.Sprintf("%s — %s", cronRunID, source)
	}

	if strings.TrimSpace(source) == "" {
		source = "Halodoc"
	}
	if strings.TrimSpace(trigger) == "" {
		trigger = "cron"
	}

	run := &model.CrawlRun{
		ID:             runID,
		HistoryID:      runID,
		CronRunID:      cronRunID,
		RunNumber:      runNum,
		DisplayName:    displayName,
		Source:         source,
		TrustedSource:  source,
		SourceURL:      sourceURL,
		Topic:          topic,
		Limit:          targetArticles,
		TargetArticles: targetArticles,
		Trigger:        trigger,
		Status:         model.RunStatusRunning,
		StartedAt:      now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	run.SyncFields()

	rs.runs[runID] = run
	rs.rebuildSortedListLocked()
	if err := rs.saveLocked(); err != nil {
		return nil, err
	}

	return run, nil
}

// RecordArticleRun links an article ID to a specific Run ID persistently.
func (rs *RunStorage) RecordArticleRun(runID, articleID string) error {
	if runID == "" || articleID == "" {
		return nil
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.articleToRun[articleID] = runID
	existing := rs.runArticleIDs[runID]
	for _, id := range existing {
		if id == articleID {
			return nil
		}
	}
	rs.runArticleIDs[runID] = append(existing, articleID)
	return rs.saveArticleMappingsLocked()
}

// GetRunForArticle returns the Run ID that produced the given article ID.
func (rs *RunStorage) GetRunForArticle(articleID string) string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.articleToRun[articleID]
}

// GetArticlesForRun returns all article IDs associated with a specific run ID or display name.
func (rs *RunStorage) GetArticlesForRun(runID string) []string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Direct lookup by runID
	if ids, ok := rs.runArticleIDs[runID]; ok && len(ids) > 0 {
		res := make([]string, len(ids))
		copy(res, ids)
		return res
	}

	// Lookup by display name or alternate ID
	norm := strings.ToLower(strings.TrimSpace(runID))
	for id, r := range rs.runs {
		if strings.ToLower(strings.TrimSpace(r.DisplayName)) == norm || strings.ToLower(strings.TrimSpace(id)) == norm {
			ids := rs.runArticleIDs[id]
			res := make([]string, len(ids))
			copy(res, ids)
			return res
		}
	}

	return []string{}
}

// GetRunByDisplayName retrieves a run by its display name (e.g. "Run #006").
func (rs *RunStorage) GetRunByDisplayName(name string) (*model.CrawlRun, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	normName := strings.ToLower(strings.TrimSpace(name))
	for _, r := range rs.runs {
		if strings.ToLower(strings.TrimSpace(r.DisplayName)) == normName || strings.ToLower(strings.TrimSpace(r.ID)) == normName {
			cp := *r
			return &cp, true
		}
	}
	return nil, false
}

// UpdateRun saves mutated run metrics and state.
func (rs *RunStorage) UpdateRun(run *model.CrawlRun) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if run == nil || run.ID == "" {
		return fmt.Errorf("invalid run object")
	}

	run.UpdatedAt = time.Now()
	if run.FinishedAt != nil && !run.StartedAt.IsZero() {
		run.DurationSec = run.FinishedAt.Sub(run.StartedAt).Seconds()
		if run.DurationSec < 0 {
			run.DurationSec = 0
		}
	}
	run.SyncFields()

	rs.runs[run.ID] = run
	rs.rebuildSortedListLocked()
	return rs.saveLocked()
}

// GetRun retrieves a run by its ID.
func (rs *RunStorage) GetRun(runID string) (*model.CrawlRun, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	r, ok := rs.runs[runID]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// GetAllRuns returns all recorded runs sorted from newest to oldest.
func (rs *RunStorage) GetAllRuns() []*model.CrawlRun {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	result := make([]*model.CrawlRun, len(rs.runList))
	for i, r := range rs.runList {
		cp := *r
		result[i] = &cp
	}
	return result
}

// GetLatestRun returns the most recent run, if any.
func (rs *RunStorage) GetLatestRun() *model.CrawlRun {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if len(rs.runList) == 0 {
		return nil
	}
	cp := *rs.runList[0]
	return &cp
}

// GetTodayRuns returns all runs started on the current local calendar date.
func (rs *RunStorage) GetTodayRuns() []*model.CrawlRun {
	now := time.Now()
	return rs.GetRunsByDate(now.Format("2006-01-02"))
}

// GetRunsByDate returns all runs started on a specific date (format: YYYY-MM-DD).
func (rs *RunStorage) GetRunsByDate(dateStr string) []*model.CrawlRun {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	parsed, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return []*model.CrawlRun{}
	}
	targetY, targetM, targetD := parsed.Date()

	var matched []*model.CrawlRun
	for _, r := range rs.runList {
		ry, rm, rd := r.StartedAt.Date()
		if ry == targetY && rm == targetM && rd == targetD {
			cp := *r
			matched = append(matched, &cp)
		}
	}
	return matched
}

// RunDateSummary summarizes all runs and total articles for a particular date.
type RunDateSummary struct {
	Date       string   `json:"date"`        // YYYY-MM-DD
	Display    string   `json:"display"`     // e.g. "07 September 2026"
	RunsCount  int      `json:"runs_count"`
	TotalItems int      `json:"total_items"`
	RunIDs     []string `json:"run_ids"`
}

// GetRunDates returns a list of distinct calendar dates that have recorded crawl runs.
func (rs *RunStorage) GetRunDates() []RunDateSummary {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	datesMap := make(map[string]*RunDateSummary)
	var orderedDates []string

	for _, r := range rs.runList {
		dStr := r.StartedAt.Format("2006-01-02")
		summary, exists := datesMap[dStr]
		if !exists {
			summary = &RunDateSummary{
				Date:       dStr,
				Display:    r.StartedAt.Format("02 January 2006"),
				RunsCount:  0,
				TotalItems: 0,
				RunIDs:     make([]string, 0),
			}
			datesMap[dStr] = summary
			orderedDates = append(orderedDates, dStr)
		}
		summary.RunsCount++
		summary.RunIDs = append(summary.RunIDs, r.ID)
	}

	var result []RunDateSummary
	for _, d := range orderedDates {
		summary := datesMap[d]
		totalItems := 0
		for _, rID := range summary.RunIDs {
			if r, ok := rs.runs[rID]; ok {
				if r.TotalNew > 0 {
					totalItems += r.TotalNew
				} else {
					totalItems += r.TotalSuccess
				}
			}
		}
		summary.TotalItems = totalItems
		result = append(result, *summary)
	}
	return result
}

// DeleteRun removes a single run by its unique ID (or display name) and cleans up its article mappings.
// Returns (true, nil) if deleted, (false, nil) if not found.
func (rs *RunStorage) DeleteRun(runID string) (bool, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	normID := strings.TrimSpace(runID)
	if normID == "" {
		return false, fmt.Errorf("empty run id")
	}

	targetID := ""
	if _, exists := rs.runs[normID]; exists {
		targetID = normID
	} else {
		normLower := strings.ToLower(normID)
		for id, r := range rs.runs {
			if strings.ToLower(strings.TrimSpace(r.DisplayName)) == normLower || strings.ToLower(strings.TrimSpace(id)) == normLower {
				targetID = id
				break
			}
		}
	}

	if targetID == "" {
		return false, nil
	}

	// Remove from runs map
	delete(rs.runs, targetID)
	rs.rebuildSortedListLocked()
	if err := rs.saveLocked(); err != nil {
		return false, fmt.Errorf("failed to persist runs after deletion: %w", err)
	}

	// Clean up article mappings for this run
	if artIDs, exists := rs.runArticleIDs[targetID]; exists {
		for _, artID := range artIDs {
			if rs.articleToRun[artID] == targetID {
				delete(rs.articleToRun, artID)
			}
		}
		delete(rs.runArticleIDs, targetID)
		_ = rs.saveArticleMappingsLocked()
	}

	return true, nil
}


