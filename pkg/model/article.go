package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ArticleDescription holds cleaned article paragraphs.
// In Go, it acts as a slice of paragraphs ([]string).
// When serialized to JSON, it renders as a clean multi-paragraph string joined by "\n\n".
// When deserialized, it transparently accepts either a JSON string or an array of strings.
type ArticleDescription []string

// MarshalJSON serializes paragraphs as a single string delimited by double newlines.
func (d ArticleDescription) MarshalJSON() ([]byte, error) {
	var nonEmpties []string
	for _, p := range d {
		t := strings.TrimSpace(p)
		if t != "" {
			nonEmpties = append(nonEmpties, t)
		}
	}
	return json.Marshal(strings.Join(nonEmpties, "\n\n"))
}

// UnmarshalJSON parses both JSON string and array representations.
func (d *ArticleDescription) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*d = nil
		return nil
	}

	// Case 1: JSON string
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		rawParts := strings.Split(s, "\n\n")
		var paras []string
		for _, part := range rawParts {
			t := strings.TrimSpace(part)
			if t != "" {
				paras = append(paras, t)
			}
		}
		if len(paras) == 0 && strings.TrimSpace(s) != "" {
			paras = []string{strings.TrimSpace(s)}
		}
		*d = paras
		return nil
	}

	// Case 2: JSON array
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	var paras []string
	for _, item := range arr {
		t := strings.TrimSpace(item)
		if t != "" {
			paras = append(paras, t)
		}
	}
	*d = paras
	return nil
}

// String returns the full concatenated text of all paragraphs.
func (d ArticleDescription) String() string {
	return strings.Join(d, "\n\n")
}

// Article represents the canonical medical article data model.
type Article struct {
	ID           string             `json:"id,omitempty"`
	Title        string             `json:"title"`
	Image        string             `json:"image"`
	Category     string             `json:"category"`
	Description  ArticleDescription `json:"description"`
	SourceURL    string             `json:"source_url,omitempty"`
	ScrapedAt    string             `json:"scraped_at,omitempty"`
	CrawlSession string             `json:"crawl_session,omitempty"`
	RunID        string             `json:"-"`
}

// GenerateArticleID generates a deterministic 12-char hex unique identifier for an article.
func GenerateArticleID(title, sourceURL string) string {
	raw := strings.TrimSpace(title) + "|" + strings.TrimSpace(sourceURL)
	if raw == "|" {
		raw = fmt.Sprintf("article_%d", time.Now().UnixNano())
	}
	h := sha256.Sum256([]byte(raw))
	return "art_" + hex.EncodeToString(h[:])[:12]
}

// DiskArticle defines the strict 7-field schema persisted to output/articles.json.
type DiskArticle struct {
	ID          string             `json:"id,omitempty"`
	Title       string             `json:"title"`
	Image       string             `json:"image"`
	Category    string             `json:"category"`
	Description ArticleDescription `json:"description"`
	SourceURL   string             `json:"source_url,omitempty"`
	ScrapedAt   string             `json:"scraped_at,omitempty"`
}

// ToDiskArticle converts an Article to DiskArticle, guaranteeing no unauthorized fields on disk.
func (a Article) ToDiskArticle() DiskArticle {
	return DiskArticle{
		ID:          a.ID,
		Title:       a.Title,
		Image:       a.Image,
		Category:    a.Category,
		Description: a.Description,
		SourceURL:   a.SourceURL,
		ScrapedAt:   a.ScrapedAt,
	}
}

