package preprocess

import (
	"math"
	"regexp"
	"strings"
	"unicode"

	"maps-scraper/pkg/model"
)

var (
	nonAlphanumeric = regexp.MustCompile(`[^a-z0-9\s]`)
	multiSpace      = regexp.MustCompile(`\s+`)
)

// NormalizeText standardizes text for fuzzy matching
func NormalizeText(text string) string {
	lower := strings.ToLower(text)
	// Replace punctuation with space
	cleaned := nonAlphanumeric.ReplaceAllString(lower, " ")
	// Collapse multiple spaces
	trimmed := strings.TrimSpace(multiSpace.ReplaceAllString(cleaned, " "))
	return trimmed
}

// NormalizeStoreName removes common noise prefixes/suffixes (like "toko bangunan", "tb.", "ud.", "cv.", "pt.") for fuzzy matching
func NormalizeStoreName(name string) string {
	norm := NormalizeText(name)

	multiWordNoise := []string{
		"toko bahan bangunan", "toko bangunan", "toko cat", "toko besi", "toko material",
		"jawa timur", "surabaya", "jatim", "indonesia",
	}

	for _, mwn := range multiWordNoise {
		norm = strings.ReplaceAll(norm, mwn, " ")
	}
	norm = strings.TrimSpace(multiSpace.ReplaceAllString(norm, " "))

	singleWordNoise := map[string]bool{
		"toko": true, "tb": true, "ud": true, "cv": true, "pt": true,
		"depo": true, "supermarket": true, "distributor": true, "agen": true,
		"sby": true, "cabang": true, "outlet": true, "store": true,
	}

	words := strings.Fields(norm)
	var filtered []string
	for _, w := range words {
		if !singleWordNoise[w] {
			filtered = append(filtered, w)
		}
	}

	if len(filtered) == 0 {
		return NormalizeText(name) // fallback
	}
	return strings.Join(filtered, " ")
}

// LevenshteinSimilarity calculates string similarity between 0.0 and 1.0
func LevenshteinSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	r1 := []rune(s1)
	r2 := []rune(s2)
	l1 := len(r1)
	l2 := len(r2)

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

// TokenSetSimilarity calculates Jaccard / token overlap similarity
func TokenSetSimilarity(s1, s2 string) float64 {
	w1 := strings.Fields(s1)
	w2 := strings.Fields(s2)

	if len(w1) == 0 && len(w2) == 0 {
		return 1.0
	}
	if len(w1) == 0 || len(w2) == 0 {
		return 0.0
	}

	set1 := make(map[string]bool)
	for _, w := range w1 {
		set1[w] = true
	}

	intersection := 0
	set2 := make(map[string]bool)
	for _, w := range w2 {
		set2[w] = true
		if set1[w] {
			intersection++
		}
	}

	union := len(set1)
	for w := range set2 {
		if !set1[w] {
			union++
		}
	}

	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// CombinedSimilarity calculates a weighted fuzzy similarity score between two strings
func CombinedSimilarity(s1, s2 string) float64 {
	lev := LevenshteinSimilarity(s1, s2)
	tok := TokenSetSimilarity(s1, s2)
	// Weighted: 50% Levenshtein, 50% Token Set
	return (lev * 0.5) + (tok * 0.5)
}

// CleanPhoneNumber strips all formatting leaving only digits
func CleanPhoneNumber(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	// Standardize leading Indonesian country code 62 to 0
	if strings.HasPrefix(digits, "62") {
		digits = "0" + digits[2:]
	}
	return digits
}

// CalculateCompletenessScore calculates a quality score for record resolution
func CalculateCompletenessScore(rec model.StoreRecord) int {
	score := 0

	// 1. Place ID (critical anchor)
	if strings.TrimSpace(rec.PlaceID) != "" {
		score += 20
	}

	// 2. Address completeness
	addr := strings.TrimSpace(rec.Address)
	if len(addr) > 20 {
		score += 15
	} else if len(addr) > 0 {
		score += 5
	}

	// 3. Kecamatan
	if strings.TrimSpace(rec.Kecamatan) != "" {
		score += 5
	}

	// 4. Coordinates
	if strings.TrimSpace(rec.Latitude) != "" && strings.TrimSpace(rec.Longitude) != "" {
		score += 15
	}

	// 5. Phone
	if strings.TrimSpace(rec.Phone) != "" {
		score += 10
	}

	// 6. Opening Hours (Full 7 days check)
	hours := strings.TrimSpace(rec.OpeningHours)
	if hours != "" {
		if strings.Count(hours, "|") == 6 {
			score += 15 // Full 7-day schedule
		} else {
			score += 5
		}
	}

	// 7. Types
	if strings.TrimSpace(rec.Types) != "" {
		score += 5
	}

	// 8. Rating
	if strings.TrimSpace(rec.Rating) != "" {
		score += 5
	}

	// 9. Photo URL
	if strings.TrimSpace(rec.PhotoURL) != "" {
		score += 5
	}

	// 10. Website / Links
	if strings.TrimSpace(rec.WebsiteLinks) != "" {
		score += 5
	}

	return score
}
