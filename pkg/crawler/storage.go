package crawler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"maps-scraper/pkg/model"
)

// DefaultOutputPath defines the standard primary output file for medical articles.
const DefaultOutputPath = "output/articles.json"

// JSONStorage manages thread-safe, asynchronous persistence of crawled Article items to JSON files.
type JSONStorage struct {
	mu           sync.RWMutex
	outputPath   string
	articles     []model.Article
	writeChan    chan model.Article
	closeChan    chan struct{}
	wg           sync.WaitGroup
	autoFlush     bool
	deterministic bool
	closeOnce     sync.Once
}

// NewJSONStorage initializes a JSON storage manager writing to the given outputPath.
func NewJSONStorage(outputPath string, autoFlush bool) (*JSONStorage, error) {
	if outputPath == "" {
		outputPath = DefaultOutputPath
	}

	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", outputPath, err)
		}
	}

	js := &JSONStorage{
		outputPath:   outputPath,
		articles:     make([]model.Article, 0),
		writeChan:    make(chan model.Article, 1000),
		closeChan:    make(chan struct{}),
		autoFlush:    autoFlush,
		deterministic: true,
	}

	// If file exists, preload existing records
	if _, err := os.Stat(outputPath); err == nil {
		_ = js.loadExisting()
	}

	js.wg.Add(1)
	go js.writerLoop()

	return js, nil
}

func (js *JSONStorage) loadExisting() error {
	data, err := os.ReadFile(js.outputPath)
	if err != nil {
		return err
	}
	var existing []model.Article
	if err := json.Unmarshal(data, &existing); err == nil {
		js.articles = make([]model.Article, 0, len(existing))
		for _, a := range existing {
			if a.ID == "" {
				a.ID = model.GenerateArticleID(a.Title, a.SourceURL)
			}
			if a.CrawlSession == "" {
				a.CrawlSession = "Crawl #1"
			}
			if a.RunID == "" {
				a.RunID = "run_baseline"
			}
			js.appendUniqueLocked(a)
		}
	}
	return nil
}

func (js *JSONStorage) appendUniqueLocked(art model.Article) bool {
	if art.ID == "" {
		art.ID = model.GenerateArticleID(art.Title, art.SourceURL)
	}
	if art.CrawlSession == "" {
		art.CrawlSession = "Crawl #1"
	}
	if art.RunID == "" {
		art.RunID = "run_baseline"
	}

	// Deduplication Priority 1: source_url
	normURL := strings.TrimRight(strings.ToLower(strings.TrimSpace(art.SourceURL)), "/")
	if normURL != "" {
		for _, a := range js.articles {
			existURL := strings.TrimRight(strings.ToLower(strings.TrimSpace(a.SourceURL)), "/")
			if existURL != "" && existURL == normURL {
				return false
			}
		}
	}

	// Deduplication Priority 2: title
	normTitle := strings.ToLower(strings.TrimSpace(art.Title))
	for _, a := range js.articles {
		if strings.ToLower(strings.TrimSpace(a.Title)) == normTitle {
			return false
		}
	}

	js.articles = append(js.articles, art)
	return true
}

// SaveWithResult synchronously attempts to store an article and reports whether it was newly added or a duplicate.
func (js *JSONStorage) SaveWithResult(art model.Article) (bool, error) {
	js.mu.Lock()
	defer js.mu.Unlock()

	added := js.appendUniqueLocked(art)
	if added && js.autoFlush {
		if err := js.flushLocked(); err != nil {
			return false, err
		}
	}
	return added, nil
}

func (js *JSONStorage) writerLoop() {
	defer js.wg.Done()

	for {
		select {
		case art := <-js.writeChan:
			js.mu.Lock()
			added := js.appendUniqueLocked(art)
			if added && js.autoFlush {
				_ = js.flushLocked()
			}
			js.mu.Unlock()

		case <-js.closeChan:
			// Drain remaining articles
			for len(js.writeChan) > 0 {
				art := <-js.writeChan
				js.mu.Lock()
				js.appendUniqueLocked(art)
				js.mu.Unlock()
			}
			js.mu.Lock()
			_ = js.flushLocked()
			js.mu.Unlock()
			return
		}
	}
}

func toDiskArticles(articles []model.Article) []model.DiskArticle {
	diskItems := make([]model.DiskArticle, len(articles))
	for i, a := range articles {
		diskItems[i] = a.ToDiskArticle()
	}
	return diskItems
}

func (js *JSONStorage) flushLocked() error {
	items := js.articles
	if items == nil {
		items = []model.Article{}
	}

	// Stable deterministic sorting by title
	if js.deterministic && len(items) > 1 {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Title < items[j].Title
		})
	}

	diskItems := toDiskArticles(items)
	data, err := json.MarshalIndent(diskItems, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal articles JSON: %w", err)
	}
	// Append newline for clean POSIX/UTF-8 file formatting
	data = append(data, '\n')

	return SafeWriteFile(js.outputPath, data)
}

// SafeWriteFile performs atomic-safe file writing with temp file creation and Windows fallback.
func SafeWriteFile(filePath string, data []byte) error {
	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", filePath, err)
		}
	}

	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file %s: %w", tmpFile, err)
	}

	// Remove destination first to prevent Windows rename conflicts
	_ = os.Remove(filePath)
	if err := os.Rename(tmpFile, filePath); err != nil {
		// Fallback to direct write if rename fails on locked/sandboxed filesystem
		_ = os.Remove(tmpFile)
		return os.WriteFile(filePath, data, 0644)
	}

	return nil
}

// Save sends an article to the asynchronous storage writer.
func (js *JSONStorage) Save(art model.Article) error {
	select {
	case js.writeChan <- art:
		return nil
	default:
		// Channel full, handle synchronously under lock
		js.mu.Lock()
		js.articles = append(js.articles, art)
		err := js.flushLocked()
		js.mu.Unlock()
		return err
	}
}

// GetAll returns a copy of all stored articles.
func (js *JSONStorage) GetAll() []model.Article {
	js.mu.RLock()
	defer js.mu.RUnlock()

	result := make([]model.Article, len(js.articles))
	copy(result, js.articles)
	return result
}

// TotalSaved returns the total count of articles saved.
func (js *JSONStorage) TotalSaved() int {
	js.mu.RLock()
	defer js.mu.RUnlock()
	return len(js.articles)
}

// Flush explicitly flushes in-memory articles to disk.
func (js *JSONStorage) Flush() error {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.flushLocked()
}

// DeleteArticle removes an article identified by its unique ID, and atomically persists the change to disk.
// Returns true if found and deleted, false if not found.
func (js *JSONStorage) DeleteArticle(id string) (bool, error) {
	if strings.TrimSpace(id) == "" {
		return false, errors.New("empty article id")
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	targetIdx := -1
	for i, a := range js.articles {
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
	js.articles = append(js.articles[:targetIdx], js.articles[targetIdx+1:]...)

	if err := js.flushLocked(); err != nil {
		return false, fmt.Errorf("failed to save articles after deletion: %w", err)
	}

	return true, nil
}

// Close gracefully closes the storage manager and ensures all articles are written to disk.
func (js *JSONStorage) Close() error {
	js.closeOnce.Do(func() {
		close(js.closeChan)
		js.wg.Wait()
	})
	return nil
}

// VerifyArticlesJSON programmatically verifies that a JSON file is a valid array of Article objects
// where EVERY object contains the canonical keys: title, category, image, description,
// and optionally source_url, id, and scraped_at.
func VerifyArticlesJSON(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read JSON file %s: %w", path, err)
	}

	var rawItems []map[string]interface{}
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return 0, fmt.Errorf("invalid JSON array in %s: %w", path, err)
	}

	expectedKeys := map[string]bool{
		"id":          true,
		"title":       true,
		"category":    true,
		"image":       true,
		"description": true,
		"source_url":  true,
		"scraped_at":  true,
	}

	for i, item := range rawItems {
		if len(item) < 4 || len(item) > 7 {
			return 0, fmt.Errorf("article #%d has %d keys (expected between 4 and 7): %v", i+1, len(item), item)
		}

		for k := range item {
			if !expectedKeys[k] {
				return 0, fmt.Errorf("article #%d contains unauthorized key %q", i+1, k)
			}
		}

		for _, k := range []string{"title", "image", "category"} {
			val, ok := item[k]
			if !ok {
				return 0, fmt.Errorf("article #%d missing mandatory key %q", i+1, k)
			}
			strVal, isStr := val.(string)
			if !isStr {
				return 0, fmt.Errorf("article #%d key %q is not a string: %v", i+1, k, val)
			}
			if strVal == "" {
				return 0, fmt.Errorf("article #%d key %q has empty string value", i+1, k)
			}
		}

		// Verify description is non-empty (either a JSON string or an array of strings)
		descVal, ok := item["description"]
		if !ok {
			return 0, fmt.Errorf("article #%d missing mandatory key %q", i+1, "description")
		}
		if descStr, isStr := descVal.(string); isStr {
			if strings.TrimSpace(descStr) == "" {
				return 0, fmt.Errorf("article #%d description is empty string", i+1)
			}
		} else if descSlice, isSlice := descVal.([]interface{}); isSlice {
			if len(descSlice) < 1 {
				return 0, fmt.Errorf("article #%d description array is empty", i+1)
			}
			for pIdx, p := range descSlice {
				pStr, ok := p.(string)
				if !ok || strings.TrimSpace(pStr) == "" {
					return 0, fmt.Errorf("article #%d description paragraph #%d is empty or not a string", i+1, pIdx+1)
				}
			}
		} else {
			return 0, fmt.Errorf("article #%d key \"description\" is neither string nor array: %v", i+1, descVal)
		}
	}

	return len(rawItems), nil
}

// ReadArticlesJSON reads and parses a JSON file containing an array of Articles.
// It assigns deterministic unique IDs to any legacy articles missing an ID.
func ReadArticlesJSON(path string) ([]model.Article, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var articles []model.Article
	if err := json.Unmarshal(data, &articles); err != nil {
		return nil, fmt.Errorf("failed to parse JSON from %s: %w", path, err)
	}

	for i := range articles {
		if articles[i].ID == "" {
			articles[i].ID = model.GenerateArticleID(articles[i].Title, articles[i].SourceURL)
		}
		if articles[i].CrawlSession == "" {
			articles[i].CrawlSession = "Crawl #1"
		}
		if articles[i].RunID == "" {
			articles[i].RunID = "run_baseline"
		}
	}

	return articles, nil
}

