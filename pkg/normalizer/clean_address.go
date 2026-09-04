package normalizer

import (
	"regexp"
	"strings"
)

var (
	contactKeywords = []string{
		"wa", "whatsapp", "telp", "telepon", "phone", "hubungi",
		"contact", "admin", "cs", "marketing", "cp", "call", "hp", "order", "pesan",
		"info", "customer service",
	}
	promoKeywords = []string{
		"gratis ongkir", "cod", "order sekarang", "melayani pengiriman", "promo", "diskon",
	}
	socialKeywords = []string{
		"instagram", "facebook", "tiktok", "telegram", "ig", "fb",
	}
	urlKeywords = []string{
		"http", "https", "www", ".com", ".co.id",
	}
	addressKeywords = []string{
		"jl", "jalan", "gg", "gang", "rt", "rw", "no", "blok", "kav",
		"kec", "kecamatan", "kab", "kabupaten", "kota", "provinsi",
		"desa", "kelurahan", "dusun", "kampung", "perum", "komplek",
	}
	
	// standard phone regex
	phoneRegex = regexp.MustCompile(`(?i)(?:\+62|62|08)[0-9\-\s]{6,15}`)
)

func hasAnyKeyword(s string, keywords []string) bool {
	sLower := strings.ToLower(s)
	for _, kw := range keywords {
		// Escape keyword and add word boundaries
		// For keywords ending/starting with symbols (like :), \b might not work as expected,
		// but since we want to avoid substring matches in normal words, we can check word boundaries on letters.
		// A safer way without regex for simple substring: check if it's a standalone word.
		pattern := `(?i)(^|[^a-z])` + regexp.QuoteMeta(kw) + `([^a-z]|$)`
		if matched, _ := regexp.MatchString(pattern, sLower); matched {
			return true
		}
	}
	return false
}

type CleanResult struct {
	CleanedAddress string
	ExtractedPhone string
	RemovedParts   []string
	Confidence     string // HIGH, MEDIUM, LOW
	Reason         string
}

// CleanAddressResult performs boundary detection and contamination cleaning.
func CleanAddressResult(raw string) CleanResult {
	res := CleanResult{
		CleanedAddress: raw,
		Confidence:     "HIGH",
	}
	
	if strings.TrimSpace(raw) == "" {
		return res
	}

	// Tokenize by comma
	parts := strings.Split(raw, ",")
	var validParts []string
	var extractedPhone string
	
	// We will try to find the "core" address.
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		
		lower := strings.ToLower(trimmed)
		
		// Extract standard phone numbers from anywhere in the token
		if matches := phoneRegex.FindStringSubmatch(trimmed); len(matches) > 0 {
			if extractedPhone == "" {
				extractedPhone = strings.TrimSpace(matches[0])
			}
		}

		isContact := hasAnyKeyword(lower, contactKeywords)
		isPromo := hasAnyKeyword(lower, promoKeywords)
		isSocial := hasAnyKeyword(lower, socialKeywords)
		isUrl := hasAnyKeyword(lower, urlKeywords)
		hasAddr := hasAnyKeyword(lower, addressKeywords)
		
		isPlusCode := regexp.MustCompile(`^[A-Z0-9]{4}\+[A-Z0-9]{2,3}$`).MatchString(strings.ToUpper(trimmed))

		// If it's a contact or promo token AND doesn't have explicit address keywords
		if (isContact || isPromo || isSocial || isUrl) && !hasAddr && !isPlusCode {
			res.RemovedParts = append(res.RemovedParts, trimmed)
			if res.Reason == "" {
				res.Reason = "Non-address metadata detected"
			}
			continue
		}

		// What if it has contact keywords AND address keywords?
		// e.g. "Wa admin Jl. Raya" -> We should split it further!
		if isContact && hasAddr {
			// Find address boundary (e.g. index of "jl", "jalan", etc)
			earliestIdx := -1
			for _, kw := range addressKeywords {
				// use word boundary if possible, or just index
				idx := strings.Index(lower, kw + ".") // jl.
				if idx == -1 {
					idx = strings.Index(lower, kw + " ") // jl 
				}
				if idx == -1 {
					// exactly the word
					if lower == kw {
						idx = 0
					}
				}
				
				if idx != -1 {
					if earliestIdx == -1 || idx < earliestIdx {
						earliestIdx = idx
					}
				}
			}
			
			if earliestIdx > 5 { // Meaning there's contact info before the address keyword
				suspectStr := trimmed[:earliestIdx]
				suspectLower := strings.ToLower(suspectStr)
				
				if hasAnyKeyword(suspectLower, contactKeywords) || hasAnyKeyword(suspectLower, promoKeywords) {
					// Split it!
					res.RemovedParts = append(res.RemovedParts, strings.TrimSpace(suspectStr))
					trimmed = strings.TrimSpace(trimmed[earliestIdx:])
					if res.Reason == "" {
						res.Reason = "Prefix contact metadata removed"
					}
				}
			}
		}

		// Handle explicit obfuscated phone numbers like O&99I5OOO29 if it starts with contact kw
		if isContact && len(trimmed) < 40 && !hasAddr && !isPlusCode {
			res.RemovedParts = append(res.RemovedParts, trimmed)
			if res.Reason == "" {
				res.Reason = "Suspected obfuscated contact removed"
			}
			continue
		}

		validParts = append(validParts, trimmed)
	}

	// Reconstruct
	if len(validParts) == 0 {
		// Don't wipe it entirely if we stripped everything!
		res.CleanedAddress = raw
		res.Confidence = "LOW"
		res.Reason = "All parts flagged, rolling back"
		res.RemovedParts = nil
	} else {
		res.CleanedAddress = strings.Join(validParts, ", ")
		if len(res.RemovedParts) > 0 {
			res.ExtractedPhone = extractedPhone
		}
	}

	return res
}

// CleanAddress wrapper for legacy calls (returns cleaned string and extracted phone)
func CleanAddress(raw string) (string, string) {
	res := CleanAddressResult(raw)
	return res.CleanedAddress, res.ExtractedPhone
}
