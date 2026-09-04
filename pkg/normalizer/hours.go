package normalizer

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var daysOrderID = []string{"Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu", "Minggu"}

func cleanGlyphs(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xE000 && r <= 0xF8FF {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NormalizeOpeningHours parses Google Maps raw opening hours strings into exact reference CSV pipe format:
// "Senin,08.30–16.00 | Selasa,08.30–16.00 | Rabu,08.30–16.00 | Kamis,08.30–16.00 | Jumat,08.30–16.00 | Sabtu,08.30–14.00 | Minggu,Tutup"
// or "Senin 08.30–16.00 | Selasa 08.30–16.00 | ..." depending on separator requirement.
func NormalizeOpeningHours(rawHoursText string) string {
	rawHoursText = cleanGlyphs(rawHoursText)
	rawHoursText = strings.TrimSpace(rawHoursText)
	if rawHoursText == "" {
		return ""
	}

	lines := strings.Split(rawHoursText, "\n")
	dayTimes := make(map[string]string)

	dayNames := []struct {
		stdName string
		tokens  []string
	}{
		{"Senin", []string{"senin", "monday"}},
		{"Selasa", []string{"selasa", "tuesday"}},
		{"Rabu", []string{"rabu", "wednesday"}},
		{"Kamis", []string{"kamis", "thursday"}},
		{"Jumat", []string{"jumat", "friday"}},
		{"Sabtu", []string{"sabtu", "saturday"}},
		{"Minggu", []string{"minggu", "sunday"}},
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lineLower := strings.ToLower(line)

		for _, d := range dayNames {
			matched := false
			for _, tok := range d.tokens {
				// Format 1: Exact day token line
				if lineLower == tok {
					for j := i + 1; j < len(lines) && j <= i+3; j++ {
						next := strings.TrimSpace(lines[j])
						if next != "" {
							timePart := formatTimeRange(next)
							if timePart != "" {
								dayTimes[d.stdName] = timePart
								matched = true
								break
							}
						}
					}
				} else if strings.HasPrefix(lineLower, tok) {
					// Format 2: "Senin,08.30–16.00" or "Senin 08.30–16.00"
					rawTime := line[len(tok):]
					rawTime = strings.TrimFunc(rawTime, func(r rune) bool {
						return unicode.IsSpace(r) || r == ':' || r == '\t' || r == '-' || r == ','
					})
					if rawTime != "" {
						dayTimes[d.stdName] = formatTimeRange(rawTime)
						matched = true
					}
				}
				if matched {
					break
				}
			}
			if matched {
				break
			}
		}
	}

	if len(dayTimes) < 7 {
		// Do not invent "Tutup" for unread days. It must be explicitly stated in the scraped text.
		// If we don't have exactly 7 days parsed, the extraction is incomplete/invalid.
		return ""
	}

	var formattedList []string
	for _, d := range daysOrderID {
		tVal := dayTimes[d]
		if strings.EqualFold(tVal, "closed") || strings.EqualFold(tVal, "tutup") {
			formattedList = append(formattedList, fmt.Sprintf("%s,Tutup", d))
		} else {
			formattedList = append(formattedList, fmt.Sprintf("%s,%s", d, tVal))
		}
	}

	return strings.Join(formattedList, " | ")
}

func formatTimeRange(raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	if strings.EqualFold(lower, "closed") || strings.EqualFold(lower, "tutup") || strings.Contains(lower, "tutup") {
		return "Tutup"
	}
	if strings.Contains(lower, "24") {
		return "Buka 24 jam"
	}

	raw = strings.ReplaceAll(raw, "hingga", "–")
	raw = strings.ReplaceAll(raw, "sampai", "–")
	raw = strings.ReplaceAll(raw, "to", "–")
	raw = strings.ReplaceAll(raw, ":", ".")
	raw = regexp.MustCompile(`\s*[-–—]\s*`).ReplaceAllString(raw, "–")
	return strings.TrimSpace(raw)
}

func extractDay(line string) string {
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "senin") || strings.HasPrefix(lower, "monday") {
		return "Senin"
	}
	if strings.HasPrefix(lower, "selasa") || strings.HasPrefix(lower, "tuesday") {
		return "Selasa"
	}
	if strings.HasPrefix(lower, "rabu") || strings.HasPrefix(lower, "wednesday") {
		return "Rabu"
	}
	if strings.HasPrefix(lower, "kamis") || strings.HasPrefix(lower, "thursday") {
		return "Kamis"
	}
	if strings.HasPrefix(lower, "jumat") || strings.HasPrefix(lower, "friday") {
		return "Jumat"
	}
	if strings.HasPrefix(lower, "sabtu") || strings.HasPrefix(lower, "saturday") {
		return "Sabtu"
	}
	if strings.HasPrefix(lower, "minggu") || strings.HasPrefix(lower, "sunday") {
		return "Minggu"
	}
	return ""
}
