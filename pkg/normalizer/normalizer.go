package normalizer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	nonDigitsRegex = regexp.MustCompile(`[^\d]`)
)

// FormatCoordinate formats a float coordinate into Indonesian decimal format with thousand dots like -7.268.714 or 112.655.352
func FormatCoordinate(val float64) string {
	// Format to 6 decimal places standard in Maps coordinates
	str := fmt.Sprintf("%.6f", val)
	return FormatCoordinateString(str)
}

// FormatCoordinateString formats a decimal coordinate string to ensure it has 6 decimal places (e.g. "-7.26871" -> "-7.268710")
func FormatCoordinateString(str string) string {
	str = strings.TrimSpace(str)
	if str == "" {
		return ""
	}
	
	// If it contains multiple dots (the old wrong format e.g. -7.379.223), we need to fix it.
	parts := strings.Split(str, ".")
	if len(parts) == 3 {
		integerPart := parts[0]
		fractionPart := parts[1] + parts[2]
		str = integerPart + "." + fractionPart
	}

	val, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return str // fallback
	}

	return fmt.Sprintf("%.6f", val)
}

// ParseCoordinateFromDotted converts coordinate string back to standard float (handles both standard and old dotted format)
func ParseCoordinateFromDotted(str string) (float64, error) {
	str = strings.TrimSpace(str)
	if str == "" {
		return 0, fmt.Errorf("empty coordinate")
	}
	
	// Fast path for standard coordinate
	if val, err := strconv.ParseFloat(str, 64); err == nil {
		return val, nil
	}

	// Remove dots except keep decimal logic
	isNegative := strings.HasPrefix(str, "-")
	clean := strings.TrimPrefix(str, "-")
	parts := strings.Split(clean, ".")
	if len(parts) == 3 {
		combined := parts[0] + "." + parts[1] + parts[2]
		if isNegative {
			combined = "-" + combined
		}
		return strconv.ParseFloat(combined, 64)
	} else if len(parts) == 2 {
		if isNegative {
			clean = "-" + clean
		}
		return strconv.ParseFloat(clean, 64)
	}
	return strconv.ParseFloat(str, 64)
}

// GenerateMapsQueryLinks generates both dot and comma parameter URLs from decimal lat/lon
func GenerateMapsQueryLinks(lat, lon float64) (dotLink, commaLink string) {
	dotLat := fmt.Sprintf("%.6f", lat)
	dotLon := fmt.Sprintf("%.6f", lon)

	commaLat := strings.ReplaceAll(dotLat, ".", ",")
	commaLon := strings.ReplaceAll(dotLon, ".", ",")

	dotLink = fmt.Sprintf("https://www.google.com/maps?q=%s,%s", dotLat, dotLon)
	commaLink = fmt.Sprintf("https://www.google.com/maps?q=%s,%s", commaLat, commaLon)
	return dotLink, commaLink
}

// FormatRating ensures rating has 1 decimal point e.g. "5.0", "4.7"
func FormatRating(r float64) string {
	if r <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f", r)
}

// FormatPhone standardizes Indonesian phone number strings (e.g., 0812-7308-4689 or (031) 7401921)
func FormatPhone(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Return raw if it's already well-formatted
	return raw
}

// DetectSemenBrand inspects store name and categories to detect cement brands
func DetectSemenBrand(name, details string) string {
	text := strings.ToLower(name + " " + details)
	if strings.Contains(text, "semen gresik") {
		return "Semen Gresik"
	}
	if strings.Contains(text, "semen padang") {
		return "Semen Padang"
	}
	if strings.Contains(text, "semen tonasa") {
		return "Semen Tonasa"
	}
	if strings.Contains(text, "semen indonesia") {
		return "Semen Indonesia"
	}
	if strings.Contains(text, "dynamix") || strings.Contains(text, "holcim") {
		return "Dynamix"
	}
	if strings.Contains(text, "tiga roda") {
		return "Tiga Roda"
	}
	return ""
}
