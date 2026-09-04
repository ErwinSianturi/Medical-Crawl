package normalizer

import (
	"fmt"
	"strings"
)

type ValidationResult struct {
	IsValid    bool
	Reason     string
	Confidence int
	Expected   string
	Detected   string
}

// EvaluateLocation provides a comprehensive, multi-layered check for location matches.
func EvaluateLocation(recAddress, recCity, recKecamatan string, lat, lon float64, targetCity, targetProvince, targetRegency, targetDistrict, customLoc string) ValidationResult {
	if recAddress == "" {
		return ValidationResult{IsValid: false, Reason: "INSUFFICIENT_DATA", Confidence: 0, Expected: targetCity, Detected: "UNKNOWN"}
	}
	
	addrLower := strings.ToLower(recAddress)
	recCityLower := strings.ToLower(recCity)
	recKecLower := strings.ToLower(recKecamatan)
	
	expected := []string{}
	if targetDistrict != "" { expected = append(expected, targetDistrict) }
	if targetRegency != "" { expected = append(expected, targetRegency) }
	if targetCity != "" { expected = append(expected, targetCity) }
	if targetProvince != "" { expected = append(expected, targetProvince) }
	if customLoc != "" { expected = append(expected, customLoc) }
	expectedStr := strings.Join(expected, ", ")

	detected := []string{}
	if recKecamatan != "" && recKecamatan != "UNKNOWN" { detected = append(detected, recKecamatan) }
	if recCity != "" && recCity != "UNKNOWN" { detected = append(detected, recCity) }
	detectedStr := strings.Join(detected, ", ")
	if detectedStr == "" { detectedStr = recAddress }

	// 1. STRICT ADMINISTRATIVE MISMATCH CHECK (The Final Gate)
	// If a target city/regency is specified, but we detected a DIFFERENT city/regency, REJECT immediately.
	checkCityTarget := targetCity
	if checkCityTarget == "" { checkCityTarget = targetRegency }
	
	if checkCityTarget != "" && recCity != "" && recCity != "UNKNOWN" {
		// e.g. target is "KOTA SURABAYA" (or "Surabaya"), and recCity is "Sidoarjo".
		targetClean := strings.ReplaceAll(strings.ToLower(checkCityTarget), "kota ", "")
		targetClean = strings.ReplaceAll(targetClean, "kabupaten ", "")
		targetClean = strings.ReplaceAll(targetClean, "kab. ", "")
		targetClean = strings.TrimSpace(targetClean)

		recClean := strings.ReplaceAll(recCityLower, "kota ", "")
		recClean = strings.ReplaceAll(recClean, "kabupaten ", "")
		recClean = strings.ReplaceAll(recClean, "kab. ", "")
		recClean = strings.TrimSpace(recClean)

		// If they don't match, and one doesn't contain the other, it's a mismatch!
		if targetClean != "" && recClean != "" && !strings.Contains(recClean, targetClean) && !strings.Contains(targetClean, recClean) {
			return ValidationResult{IsValid: false, Reason: "CITY_MISMATCH", Confidence: 0, Expected: checkCityTarget, Detected: recCity}
		}
	}

	score := 0
	
	// Coordinate validation (Level 1)
	if lat != 0 && lon != 0 {
		checkCity := checkCityTarget
		if checkCity == "" && customLoc != "" {
			checkCity = customLoc
		}
		
		if checkCity != "" {
			targetCleanForBox := strings.ReplaceAll(strings.ToLower(checkCity), "kota ", "")
			targetCleanForBox = strings.ReplaceAll(targetCleanForBox, "kabupaten ", "")
			targetCleanForBox = strings.TrimSpace(targetCleanForBox)
			
			// We only score it if we ACTUALLY found a bounding box and it's inside.
			// We DO NOT give free points if the city is not in the bounding box list.
			foundBox := false
			for cName, box := range CityBoundingBoxes {
				if strings.EqualFold(targetCleanForBox, cName) {
					foundBox = true
					if lat >= box.MinLat && lat <= box.MaxLat && lon >= box.MinLon && lon <= box.MaxLon {
						score += 40
					} else {
						// Outside known bounding box!
						return ValidationResult{IsValid: false, Reason: "COORDINATE_OUTSIDE_BOUNDARY", Confidence: 0, Expected: expectedStr, Detected: fmt.Sprintf("%f,%f", lat, lon)}
					}
					break
				}
			}
			if !foundBox {
				// We don't have a bounding box for this city, so we can't strictly validate coordinates.
				// No penalty, but no free points.
			}
		}
	}
	
	// Address text matching (Level 2)
	matchFound := false
	specificMatch := false
	
	checkList := []struct{ value, level string }{
		{targetDistrict, "district"},
		{targetRegency, "regency"},
		{targetCity, "city"},
		{targetProvince, "province"},
		{customLoc, "custom"},
	}
	
	for _, target := range checkList {
		if target.value == "" { continue }
		
		targetClean := strings.ReplaceAll(strings.ToLower(target.value), "kota ", "")
		targetClean = strings.ReplaceAll(targetClean, "kabupaten ", "")
		targetClean = strings.TrimSpace(targetClean)
		
		matched := false
		
		if strings.Contains(addrLower, targetClean) || strings.Contains(recCityLower, targetClean) || strings.Contains(recKecLower, targetClean) {
			matched = true
		}
		
		if matched {
			if target.level == "province" {
				score += 10
			} else {
				score += 30
				specificMatch = true
			}
			matchFound = true
		}
	}

	if specificMatch {
		score += 20 // Extra points for matching specific location (city/district) explicitly
	}
	
	// Administrative check for Surabaya specifically (legacy compatibility for fallback)
	if !matchFound && (checkCityTarget == "Surabaya" || checkCityTarget == "KOTA SURABAYA") && customLoc == "" {
		for _, kec := range SurabayaKecamatans {
			if strings.Contains(addrLower, strings.ToLower(kec)) {
				score += 30
				specificMatch = true
				matchFound = true
				break
			}
		}
	}

	// Must have a specific match or coordinate match to be accepted
	if score >= 60 {
		return ValidationResult{IsValid: true, Reason: "HIGH_CONFIDENCE", Confidence: score, Expected: expectedStr, Detected: detectedStr}
	} else if score >= 40 {
		return ValidationResult{IsValid: true, Reason: "ACCEPT_REVIEW", Confidence: score, Expected: expectedStr, Detected: detectedStr}
	}
	
	return ValidationResult{IsValid: false, Reason: "LOCATION_MISMATCH", Confidence: score, Expected: expectedStr, Detected: detectedStr}
}
