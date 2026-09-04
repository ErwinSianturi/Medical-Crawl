package normalizer

import (
	"fmt"
	"regexp"
	"strings"

	"maps-scraper/pkg/model"
)

type MatchMethod string

const (
	MethodExact        MatchMethod = "EXACT_MATCH"
	MethodNormalized   MatchMethod = "NORMALIZED_MATCH"
	MethodAbbreviation MatchMethod = "ABBREVIATION_RESOLUTION"
	MethodFuzzy        MatchMethod = "FUZZY_MATCH"
	MethodAmbiguous    MatchMethod = "AMBIGUOUS"
)

type ResolutionResult struct {
	RawValue       string
	CanonicalValue string
	Confidence     float64
	Method         MatchMethod
}

type Resolver struct {
	canonicalList []string
}

func NewResolver(canonicalList []string) *Resolver {
	return &Resolver{
		canonicalList: canonicalList,
	}
}

// Common abbreviations in Indonesian administrative names
var abbreviationMap = map[string]string{
	"tim":   "timur",
	"sel":   "selatan",
	"bar":   "barat",
	"br":    "barat",
	"utr":   "utara",
	"tgh":   "tengah",
	"sltn":  "selatan",
	"kodya": "kota",
	"kab":   "kabupaten",
	"kec":   "kecamatan",
	"prov":  "provinsi",
	"sby":   "surabaya",
	"bdg":   "bandung",
	"jkt":   "jakarta",
	"smg":   "semarang",
}

var prefixRegex = regexp.MustCompile(`^(?i)(kecamatan|kec\.|kec|kabupaten|kab\.|kab|kota|provinsi|prov\.|prov)\s+`)
var punctRegex = regexp.MustCompile(`[,\.\-\_]`)

func (r *Resolver) normalizeString(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = prefixRegex.ReplaceAllString(s, "")
	s = punctRegex.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ") // collapse multiple spaces
	return s
}

func (r *Resolver) Resolve(rawName string) ResolutionResult {
	if rawName == "" {
		return ResolutionResult{RawValue: "", CanonicalValue: "", Confidence: 0.0, Method: MethodAmbiguous}
	}

	normRaw := r.normalizeString(rawName)

	// 1. Exact Match (Case-Insensitive) on original list
	for _, canonical := range r.canonicalList {
		if strings.EqualFold(rawName, canonical) {
			return ResolutionResult{
				RawValue:       rawName,
				CanonicalValue: canonical,
				Confidence:     1.0,
				Method:         MethodExact,
			}
		}
	}

	// 2. Normalized Match
	for _, canonical := range r.canonicalList {
		normCan := r.normalizeString(canonical)
		if normRaw == normCan {
			return ResolutionResult{
				RawValue:       rawName,
				CanonicalValue: canonical,
				Confidence:     1.0,
				Method:         MethodNormalized,
			}
		}
	}

	// 3. Abbreviation Resolution
	// Expand abbreviations in the raw name safely
	tokens := strings.Fields(normRaw)
	expandedTokens := make([]string, len(tokens))
	for i, t := range tokens {
		if expanded, ok := abbreviationMap[t]; ok {
			expandedTokens[i] = expanded
		} else {
			expandedTokens[i] = t
		}
	}
	expandedRaw := strings.Join(expandedTokens, " ")

	if expandedRaw != normRaw {
		for _, canonical := range r.canonicalList {
			normCan := r.normalizeString(canonical)
			if expandedRaw == normCan {
				return ResolutionResult{
					RawValue:       rawName,
					CanonicalValue: canonical,
					Confidence:     0.97,
					Method:         MethodAbbreviation,
				}
			}
		}
	}

	// 4. Context-Aware Abbreviation Substring Match (e.g. Denpasar Tim matches Denpasar Timur)
	// Only apply this if we can firmly match the prefix
	for _, canonical := range r.canonicalList {
		normCan := r.normalizeString(canonical)
		if strings.HasPrefix(normCan, normRaw) && len(normRaw) > 3 {
			// Simplified: if the raw string is a prefix of the canonical string
			// e.g. "denpasar tim" is prefix of "denpasar timur"
			return ResolutionResult{
				RawValue:       rawName,
				CanonicalValue: canonical,
				Confidence:     0.95,
				Method:         MethodAbbreviation,
			}
		}
	}

	// 5. Fuzzy Matching (Levenshtein)
	var bestMatch string
	var bestScore float64 = 0.0

	for _, canonical := range r.canonicalList {
		normCan := r.normalizeString(canonical)

		// Use expanded raw for better distance calculation if applicable
		score := levenshteinSimilarity(expandedRaw, normCan)

		if score > bestScore {
			bestScore = score
			bestMatch = canonical
		}
	}

	if bestScore >= 0.95 {
		return ResolutionResult{
			RawValue:       rawName,
			CanonicalValue: bestMatch,
			Confidence:     bestScore,
			Method:         MethodFuzzy,
		}
	} else if bestScore >= 0.85 {
		return ResolutionResult{
			RawValue:       rawName,
			CanonicalValue: bestMatch,
			Confidence:     bestScore,
			Method:         MethodFuzzy,
		}
	} else if bestScore >= 0.70 {
		return ResolutionResult{
			RawValue:       rawName,
			CanonicalValue: "", // Ambiguous, flagged for review
			Confidence:     bestScore,
			Method:         MethodAmbiguous,
		}
	}

	return ResolutionResult{
		RawValue:       rawName,
		CanonicalValue: "",
		Confidence:     bestScore,
		Method:         MethodAmbiguous,
	}
}

func levenshteinSimilarity(s1, s2 string) float64 {
	r1, r2 := []rune(s1), []rune(s2)
	n, m := len(r1), len(r2)

	if n == 0 && m == 0 {
		return 1.0
	}
	if n == 0 || m == 0 {
		return 0.0
	}

	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
		d[i][0] = i
	}
	for j := 0; j <= m; j++ {
		d[0][j] = j
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}
			min := d[i-1][j] + 1
			if d[i][j-1]+1 < min {
				min = d[i][j-1] + 1
			}
			if d[i-1][j-1]+cost < min {
				min = d[i-1][j-1] + cost
			}
			d[i][j] = min
		}
	}

	maxLen := float64(n)
	if float64(m) > maxLen {
		maxLen = float64(m)
	}

	return 1.0 - float64(d[n][m])/maxLen
}

// Wrapper for existing address package to use Resolver
func ExtractKecamatan(address, city string) string {
	if address == "" {
		return ""
	}

	// We need to parse raw kecamatan candidates from the address string
	// since Google Maps usually puts it in the format:
	// Jl. Name, Kec. XYZ, City ...
	// OR just "XYZ, City"

	var rawCandidates []string

	// Attempt to extract explicitly using Regex
	if matches := kecamatanRegex.FindStringSubmatch(address); len(matches) > 1 {
		kec := strings.TrimSpace(matches[1])
		kec = strings.Trim(kec, ",.- ")
		if len(kec) > 2 {
			rawCandidates = append(rawCandidates, kec)
		}
	}

	// Try extracting tokens before the city if exact match fails
	// Address often ends with City. E.g. "... Denpasar Tim., Kota Denpasar"
	// This is complex, so we rely heavily on explicit extraction and fallback.

	canonicalKecamatans := model.GetDistrictsForCity(city)

	// If no explicit 'Kec.' found in address, maybe the address itself IS the raw candidate
	// or part of it is. For now, we will add the whole address as a candidate (normalized)
	// and see if substring context matches.
	// Actually, the previous implementation checked if the canonical name was contained in the address.
	// We should retain this but enhance it with abbreviations!

	if len(rawCandidates) > 0 {
		resolver := NewResolver(canonicalKecamatans)
		for _, raw := range rawCandidates {
			res := resolver.Resolve(raw)
			if res.CanonicalValue != "" && res.Confidence >= 0.85 {
				fmt.Printf("[RESOLVER] RAW: %s | NORMALIZED: %s | RESOLVED: %s | METHOD: %s | CONFIDENCE: %.2f\n", 
					res.RawValue, resolver.normalizeString(res.RawValue), res.CanonicalValue, res.Method, res.Confidence)
				return res.CanonicalValue
			} else if res.Method == MethodAmbiguous {
				fmt.Printf("[RESOLVER] RAW: %s | RESULT: AMBIGUOUS | CANDIDATES: %s | CONFIDENCE: %.2f\n", 
					res.RawValue, "Review Needed", res.Confidence)
			}
		}
	}

	// Fallback 2: The address might contain the abbreviation without "Kec."
	// E.g., "Jl. Sudirman, Denpasar Tim, 80232"
	// We can loop over the canonical districts and see if they, or their abbreviations, are in the address
	resolver := NewResolver(canonicalKecamatans)
	addrNorm := resolver.normalizeString(address)

	for _, canonical := range canonicalKecamatans {
		canNorm := resolver.normalizeString(canonical)
		if strings.Contains(addrNorm, canNorm) {
			return canonical
		}

		// Create abbreviations of the canonical name
		// E.g. "denpasar timur" -> ["denpasar tim", "denpasar tim."]
		tokens := strings.Fields(canNorm)
		for i, t := range tokens {
			for abbrev, full := range abbreviationMap {
				if t == full {
					// Swap full for abbrev
					tokens[i] = abbrev
					abbrevString := strings.Join(tokens, " ")
					// Check word boundaries
					pattern := `(?i)\b` + regexp.QuoteMeta(abbrevString) + `\b`
					if matched, _ := regexp.MatchString(pattern, resolver.normalizeString(address)); matched {
						fmt.Printf("[RESOLVER] CONTEXT-MATCH: %s | RESOLVED: %s | METHOD: %s\n", abbrevString, canonical, MethodAbbreviation)
						return canonical
					}
					// restore
					tokens[i] = full
				}
			}
		}
	}

	// Fallback 3: Contextual Lookup (Kelurahan Maps) using Sorted Keys
	// Retained for backward compatibility
	var keys []string
	var targetMap map[string]string

	cityLower := strings.ToLower(city)
	if strings.Contains(cityLower, "surabaya") {
		keys = sortedSurabayaKeys
		targetMap = surabayaKelurahanMap
	} else if strings.Contains(cityLower, "jakarta") {
		keys = sortedJakartaKeys
		targetMap = jakartaKelurahanMap
	}

	if targetMap != nil {
		for _, kel := range keys {
			if strings.Contains(strings.ToLower(address), kel) {
				pattern := `(?i)\b` + regexp.QuoteMeta(kel) + `\b`
				if matched, _ := regexp.MatchString(pattern, address); matched {
					return targetMap[kel]
				}
			}
		}
	}

	return ""
}
