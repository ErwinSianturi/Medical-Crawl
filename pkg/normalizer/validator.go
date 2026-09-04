package normalizer

import (
	"strings"
)

// IsOpeningHoursValid checks if the OpeningHours format contains exactly 7 valid days.
func IsOpeningHoursValid(hours string) bool {
    hours = strings.TrimSpace(hours)
    if hours == "" || hours == "Buka" || hours == "Tutup" {
        return false
    }
    
    parts := strings.Split(hours, "|")
    if len(parts) != 7 {
        return false
    }

    daysFound := make(map[string]bool)
    for _, part := range parts {
        part = strings.TrimSpace(part)
        
        subParts := strings.SplitN(part, ",", 2)
        if len(subParts) != 2 {
            return false
        }
        
        day := strings.TrimSpace(subParts[0])
        timeVal := strings.TrimSpace(subParts[1])
        
        if day == "" || timeVal == "" {
            return false
        }
        
        validDay := false
        for _, d := range daysOrderID {
            if strings.EqualFold(day, d) {
                validDay = true
                daysFound[d] = true
                break
            }
        }
        
        if !validDay {
            return false
        }
        
        if strings.Contains(timeVal, "â€“") || strings.Contains(timeVal, "?\"") {
            return false
        }
    }
    
    if len(daysFound) != 7 {
        return false
    }
    
    return true
}

func IsSuspiciouslyPartial(hours string) bool {
    parts := strings.Split(hours, "|")
    if len(parts) != 7 {
        return false
    }
    
    tutupCount := 0
    for _, p := range parts {
        if strings.Contains(strings.ToLower(p), "tutup") {
            tutupCount++
        }
    }
    
    return tutupCount >= 5
}

func FixEncodingHours(hours string) string {
    hours = strings.ReplaceAll(hours, "â€“", "–")
    hours = strings.ReplaceAll(hours, "?\"", "–")
    return hours
}
