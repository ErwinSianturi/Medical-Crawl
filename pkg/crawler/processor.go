package crawler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"maps-scraper/pkg/classifier"
	"maps-scraper/pkg/cleaner"
	"maps-scraper/pkg/model"
)

var (
	ErrEmptyTitle       = cleaner.ErrEmptyTitle
	ErrEmptyCategory    = cleaner.ErrEmptyCategory
	ErrEmptyImage       = cleaner.ErrEmptyImage
	ErrEmptyDescription = cleaner.ErrEmptyDescription
	ErrDuplicateURL     = errors.New("article URL already processed")
	ErrDuplicateTitle   = errors.New("article title is a duplicate of a previously extracted article")
)

// ArticleProcessorConfig configures validation, sanitization, and deduplication thresholds.
type ArticleProcessorConfig struct {
	MinTitleLen        int     `json:"min_title_len"`
	MinDescLen         int     `json:"min_desc_len"`
	TitleSimThreshold  float64 `json:"title_sim_threshold"` // e.g. 0.85 for fuzzy duplicate detection
	EnableFuzzyDedup   bool    `json:"enable_fuzzy_dedup"`
	AutoClassify       bool    `json:"auto_classify"`
}

// DefaultArticleProcessorConfig returns standard production defaults.
func DefaultArticleProcessorConfig() ArticleProcessorConfig {
	return ArticleProcessorConfig{
		MinTitleLen:       5,
		MinDescLen:        10,
		TitleSimThreshold: 0.85,
		EnableFuzzyDedup:  true,
		AutoClassify:      false,
	}
}

// ArticleProcessor provides thread-safe validation, sanitization, and deduplication for Article items.
type ArticleProcessor struct {
	cfg        ArticleProcessorConfig
	mu         sync.RWMutex
	cleaner    *cleaner.ArticleCleaner
	dedup      *cleaner.Deduplicator
	classifier *classifier.Classifier
}

// NewArticleProcessor creates an initialized ArticleProcessor.
func NewArticleProcessor(cfg ArticleProcessorConfig) *ArticleProcessor {
	if cfg.MinTitleLen <= 0 {
		cfg.MinTitleLen = 5
	}
	if cfg.MinDescLen <= 0 {
		cfg.MinDescLen = 10
	}
	if cfg.TitleSimThreshold <= 0 {
		cfg.TitleSimThreshold = 0.85
	}

	cleanerCfg := cleaner.CleanerConfig{
		MinTitleLen: cfg.MinTitleLen,
		MinDescLen:  cfg.MinDescLen,
	}

	return &ArticleProcessor{
		cfg:        cfg,
		cleaner:    cleaner.NewArticleCleaner(cleanerCfg),
		dedup:      cleaner.NewDeduplicator(cfg.TitleSimThreshold),
		classifier: classifier.NewDefaultClassifier(),
	}
}

// SetClassifier sets a custom category classifier on the processor.
func (p *ArticleProcessor) SetClassifier(c *classifier.Classifier) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.classifier = c
}

// Process sanitizes, validates, and deduplicates an incoming Article.
func (p *ArticleProcessor) Process(ctx context.Context, raw *model.Article, sourceURL string) (*model.Article, error) {
	if raw == nil {
		return nil, errors.New("nil article provided")
	}

	cleanImg := strings.TrimSpace(raw.Image)
	if cleanImg == "" {
		cleanImg = "-"
	}

	targetURL := sourceURL
	if targetURL == "" {
		targetURL = raw.SourceURL
	}

	scrapedAt := raw.ScrapedAt
	if scrapedAt == "" {
		scrapedAt = time.Now().UTC().Format(time.RFC3339)
	}

	articleID := raw.ID
	if articleID == "" {
		articleID = model.GenerateArticleID(raw.Title, targetURL)
	}

	// 1. Sanitize text fields using ArticleCleaner (HTML, cookies, ads, malformed encoding stripped)
	cleaned := &model.Article{
		ID:          articleID,
		Title:       p.cleaner.CleanTitle(raw.Title),
		Image:       cleanImg,
		Category:    p.cleaner.CleanCategory(raw.Category),
		Description: p.cleaner.CleanParagraphs(raw.Description),
		SourceURL:   targetURL,
		ScrapedAt:   scrapedAt,
		CrawlSession: raw.CrawlSession,
	}

	descText := strings.Join(cleaned.Description, " ")

	// 1b. Automatically detect category if AutoClassify is enabled
	if p.classifier != nil && p.cfg.AutoClassify {
		detected := p.classifier.Classify(cleaned.Title, descText)
		if detected != "" {
			cleaned.Category = detected
		}
	}

	// 2. Validate mandatory fields
	if len(cleaned.Title) < p.cfg.MinTitleLen {
		return nil, fmt.Errorf("%w (minimum length %d, got %d)", ErrEmptyTitle, p.cfg.MinTitleLen, len(cleaned.Title))
	}
	if cleaned.Category == "" {
		return nil, ErrEmptyCategory
	}
	if cleaned.Image == "" {
		return nil, ErrEmptyImage
	}
	if len(cleaned.Description) == 0 || len(descText) < p.cfg.MinDescLen {
		return nil, fmt.Errorf("%w (minimum length %d, got %d)", ErrEmptyDescription, p.cfg.MinDescLen, len(descText))
	}

	// 3. Multi-tier thread-safe deduplication (Exact hash, Normalized title, Fuzzy Levenshtein)
	dedupRes := p.dedup.CheckAndRecord(cleaned.Title, descText, sourceURL)
	if dedupRes.IsDuplicate {
		if strings.Contains(dedupRes.Reason, "URL") {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateURL, sourceURL)
		}
		return nil, fmt.Errorf("%w: %s", ErrDuplicateTitle, dedupRes.Reason)
	}

	return cleaned, nil
}

// Reset clears deduplication cache (useful for testing or job restarts).
func (p *ArticleProcessor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dedup.Reset()
}
