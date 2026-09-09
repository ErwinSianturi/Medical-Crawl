package cleaner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode"

	"maps-scraper/pkg/model"
)

// DeduplicationResult provides internal reason details when an article is identified as duplicate.
type DeduplicationResult struct {
	IsDuplicate bool   `json:"-"`
	Reason      string `json:"-"`
}

// Deduplicator maintains thread-safe in-memory hashes and fuzzy title signatures.
// NOTE: All computed hashes and normalized identifiers are internal only and never serialized to JSON.
type Deduplicator struct {
	mu           sync.RWMutex
	simThreshold float64
	seenHashes   map[string]bool   // sha256 -> true (exact content / title hashes)
	seenTitles   map[string]string // normalizedTitle -> originalTitle
	seenURLs     map[string]bool   // normalizedURL -> true
}

// NewDeduplicator creates a Deduplicator with a fuzzy title similarity threshold (e.g. 0.85).
func NewDeduplicator(simThreshold float64) *Deduplicator {
	if simThreshold <= 0 || simThreshold > 1.0 {
		simThreshold = 0.85
	}
	return &Deduplicator{
		simThreshold: simThreshold,
		seenHashes:   make(map[string]bool),
		seenTitles:   make(map[string]string),
		seenURLs:     make(map[string]bool),
	}
}

// CheckArticle inspects an Article against known records and registers it if unique.
func (d *Deduplicator) CheckArticle(art *model.Article, sourceURL string) DeduplicationResult {
	if art == nil {
		return DeduplicationResult{IsDuplicate: false}
	}
	descText := strings.Join(art.Description, " ")
	return d.CheckAndRecord(art.Title, descText, sourceURL)
}

// CheckAndRecord checks if title, description, or URL are duplicates and records them if new.
func (d *Deduplicator) CheckAndRecord(title, description, sourceURL string) DeduplicationResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 1. URL Duplication check
	if sourceURL != "" {
		normURL := strings.ToLower(strings.TrimSpace(sourceURL))
		if d.seenURLs[normURL] {
			return DeduplicationResult{
				IsDuplicate: true,
				Reason:      fmt.Sprintf("duplicate source URL: %s", sourceURL),
			}
		}
	}

	normTitle := d.normalizeForComparison(title)
	normDesc := d.normalizeForComparison(description)

	// 2. Exact Title Hash Check (SHA-256)
	titleHash := d.computeHash(normTitle)
	if d.seenHashes[titleHash] {
		return DeduplicationResult{
			IsDuplicate: true,
			Reason:      fmt.Sprintf("exact title duplicate (hash: %s...)", titleHash[:8]),
		}
	}

	// 3. Exact Content Hash Check (SHA-256 of Title + Description)
	contentHash := d.computeHash(normTitle + "||" + normDesc)
	if d.seenHashes[contentHash] {
		return DeduplicationResult{
			IsDuplicate: true,
			Reason:      fmt.Sprintf("exact content duplicate (hash: %s...)", contentHash[:8]),
		}
	}

	// 4. Exact Normalized Title String Check
	if existingTitle, exists := d.seenTitles[normTitle]; exists {
		return DeduplicationResult{
			IsDuplicate: true,
			Reason:      fmt.Sprintf("title matches existing article %q", existingTitle),
		}
	}

	// 5. Fuzzy Title Similarity Check (Levenshtein)
	for seenNorm, origTitle := range d.seenTitles {
		sim := LevenshteinSimilarity(normTitle, seenNorm)
		if sim >= d.simThreshold {
			return DeduplicationResult{
				IsDuplicate: true,
				Reason:      fmt.Sprintf("title is %.0f%% similar to existing %q", sim*100, origTitle),
			}
		}
	}

	// Not a duplicate: register in internal maps
	if sourceURL != "" {
		d.seenURLs[strings.ToLower(strings.TrimSpace(sourceURL))] = true
	}
	d.seenHashes[titleHash] = true
	d.seenHashes[contentHash] = true
	d.seenTitles[normTitle] = title

	return DeduplicationResult{
		IsDuplicate: false,
		Reason:      "",
	}
}

// computeHash generates a SHA-256 hex string from normalized text.
func (d *Deduplicator) computeHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// normalizeForComparison normalizes text for reliable matching (lowercased, alphanumeric only, single spaces).
func (d *Deduplicator) normalizeForComparison(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(lower))

	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}

	// Collapse whitespace
	words := strings.Fields(b.String())
	return strings.Join(words, " ")
}

// Reset clears all in-memory hashes and titles (useful between test runs or job restarts).
func (d *Deduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seenHashes = make(map[string]bool)
	d.seenTitles = make(map[string]string)
	d.seenURLs = make(map[string]bool)
}

// Count returns the number of uniquely recorded articles.
func (d *Deduplicator) Count() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.seenTitles)
}

// LevenshteinSimilarity calculates normalized edit distance similarity between 0.0 and 1.0.
func LevenshteinSimilarity(s1, s2 string) float64 {
	r1, r2 := []rune(s1), []rune(s2)
	l1, l2 := len(r1), len(r2)

	if l1 == 0 && l2 == 0 {
		return 1.0
	}
	if l1 == 0 || l2 == 0 {
		return 0.0
	}

	d := make([][]int, l1+1)
	for i := range d {
		d[i] = make([]int, l2+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}

	for i := 1; i <= l1; i++ {
		for j := 1; j <= l2; j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}
			d[i][j] = min(
				d[i-1][j]+1,      // deletion
				d[i][j-1]+1,      // insertion
				d[i-1][j-1]+cost, // substitution
			)
		}
	}

	distance := d[l1][l2]
	maxLen := math.Max(float64(l1), float64(l2))
	return 1.0 - (float64(distance) / maxLen)
}

