package validator

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"maps-scraper/pkg/model"
	"maps-scraper/pkg/normalizer"
)

type ValidationReport struct {
	TotalRows          int
	ValidRows          int
	InvalidRows        int
	DuplicateCount     int
	NonBuildingStore   int
	OutsideSurabaya    int
	HeaderErrors       []string
	RowErrors          []string
	DistrictBreakdown  map[string]int
	FieldCoverage      map[string]int
}

// ValidateCSVFile validates any given CSV file against the reference contract
func ValidateCSVFile(filePath string) (*ValidationReport, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file %s: %w", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.LazyQuotes = true

	report := &ValidationReport{
		DistrictBreakdown: make(map[string]int),
		FieldCoverage:     make(map[string]int),
	}

	for _, k := range normalizer.SurabayaKecamatans {
		report.DistrictBreakdown[k] = 0
	}

	// 1. Validate Header
	expectedHeaders := model.Headers()
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	if len(header) != len(expectedHeaders) {
		report.HeaderErrors = append(report.HeaderErrors, fmt.Sprintf("Header column count mismatch: expected %d, got %d", len(expectedHeaders), len(header)))
	} else {
		for i, h := range header {
			cleanH := strings.TrimSpace(h)
			cleanExp := strings.TrimSpace(expectedHeaders[i])
			if cleanH != cleanExp {
				report.HeaderErrors = append(report.HeaderErrors, fmt.Sprintf("Header[%d] mismatch: expected '%s', got '%s'", i, cleanExp, cleanH))
			}
		}
	}

	seenPlaces := make(map[string]int)
	seenNamesAndAddr := make(map[string]int)
	rowNum := 1

	// 2. Validate Records
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		report.TotalRows++

		if err != nil {
			report.InvalidRows++
			report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: CSV parse error: %v", rowNum, err))
			continue
		}

		if len(row) != len(expectedHeaders) {
			report.InvalidRows++
			report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: Column count mismatch (expected %d, got %d)", rowNum, len(expectedHeaders), len(row)))
			continue
		}

		// Field-level validations
		placeID := strings.TrimSpace(row[0])
		name := strings.TrimSpace(row[1])
		address := strings.TrimSpace(row[2])
		city := strings.TrimSpace(row[3])
		kecamatan := strings.TrimSpace(row[4])
		lat := strings.TrimSpace(row[5])
		lon := strings.TrimSpace(row[6])
		types := strings.TrimSpace(row[7])
		dotLink := strings.TrimSpace(row[15])
		commaLink := strings.TrimSpace(row[16])

		// Check Required Fields
		if name == "" || address == "" || city == "" || kecamatan == "" || lat == "" || lon == "" || types == "" || dotLink == "" || commaLink == "" {
			report.InvalidRows++
			report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: Missing mandatory field", rowNum))
			continue
		}

		// Deduplication check Level 1: Place ID
		if placeID != "" {
			if prevRow, exists := seenPlaces[placeID]; exists {
				report.DuplicateCount++
				report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: Duplicate Place ID '%s' previously seen at row %d", rowNum, placeID, prevRow))
				continue
			} else {
				seenPlaces[placeID] = rowNum
			}
		}

		// Deduplication check Level 2: Name + Address
		nameAddrKey := strings.ToLower(name) + "||" + strings.ToLower(address)
		if prevRow, exists := seenNamesAndAddr[nameAddrKey]; exists {
			report.DuplicateCount++
			report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: Duplicate Name+Address '%s' previously seen at row %d", rowNum, name, prevRow))
			continue
		}
		seenNamesAndAddr[nameAddrKey] = rowNum

		// Building Store Relevance Verification
		if !normalizer.IsCementStoreRelevance(name, address, types) {
			report.NonBuildingStore++
			report.InvalidRows++
			report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: '%s' is not a valid building store", rowNum, name))
			continue
		}

		// Coordinate & Territory validation
		// If we need strict territory validation, we should use EvaluateLocation here, but we lack target context.
		// For now, we will skip hardcoded Surabaya rejection to allow multi-region files.
		// To keep stats, we can still track but not reject.
		if !normalizer.IsInSurabaya(address) && !normalizer.IsInSurabaya(kecamatan) {
			report.OutsideSurabaya++
			// report.InvalidRows++
			// report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: '%s' is outside Surabaya (%s)", rowNum, name, address))
			// continue
		}

		if lat != "" && lon != "" {
			_, errLat := normalizer.ParseCoordinateFromDotted(lat)
			_, errLon := normalizer.ParseCoordinateFromDotted(lon)
			if errLat != nil || errLon != nil {
				report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: Invalid coordinate format: lat='%s', lon='%s'", rowNum, lat, lon))
			}
		}

		if dotLink != "" && !strings.HasPrefix(dotLink, "https://www.google.com/maps?q=") {
			report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: Invalid dot link format: '%s'", rowNum, dotLink))
		}
		if commaLink != "" && !strings.HasPrefix(commaLink, "https://www.google.com/maps?q=") {
			report.RowErrors = append(report.RowErrors, fmt.Sprintf("Row %d: Invalid comma link format: '%s'", rowNum, commaLink))
		}

		// Record Kecamatan stats
		for _, validKec := range normalizer.SurabayaKecamatans {
			if strings.EqualFold(kecamatan, validKec) {
				report.DistrictBreakdown[validKec]++
				break
			}
		}

		// Field Coverage stats
		for i, colVal := range row {
			if strings.TrimSpace(colVal) != "" && i < len(expectedHeaders) {
				report.FieldCoverage[expectedHeaders[i]]++
			}
		}

		report.ValidRows++
	}

	return report, nil
}

// CompareCSVs compares two CSV files and reports differences in schema, column counts, record counts, and coverage
func CompareCSVs(refPath, targetPath string) (string, error) {
	refReport, err := ValidateCSVFile(refPath)
	if err != nil {
		return "", fmt.Errorf("reference validation error: %w", err)
	}

	targetReport, err := ValidateCSVFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("target validation error: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("====================================================\n")
	sb.WriteString("          CSV COMPARISON & COMPLIANCE REPORT        \n")
	sb.WriteString("====================================================\n")
	sb.WriteString(fmt.Sprintf("Reference File : %s\n", refPath))
	sb.WriteString(fmt.Sprintf("Target File    : %s\n\n", targetPath))

	sb.WriteString(fmt.Sprintf("Reference Total Rows: %d (Valid: %d, Invalid: %d, Duplicates: %d)\n", refReport.TotalRows, refReport.ValidRows, refReport.InvalidRows, refReport.DuplicateCount))
	sb.WriteString(fmt.Sprintf("Target Total Rows   : %d (Valid: %d, Invalid: %d, Duplicates: %d)\n\n", targetReport.TotalRows, targetReport.ValidRows, targetReport.InvalidRows, targetReport.DuplicateCount))

	// Schema check
	sb.WriteString("--- SCHEMA COMPLIANCE ---\n")
	if len(targetReport.HeaderErrors) == 0 {
		sb.WriteString(" [MATCH] Headers & Column sequence 100% match reference contract.\n")
	} else {
		sb.WriteString(" [MISMATCH] Header errors detected:\n")
		for _, errStr := range targetReport.HeaderErrors {
			sb.WriteString(fmt.Sprintf("  - %s\n", errStr))
		}
	}

	// Field coverage comparison
	sb.WriteString("\n--- FIELD COVERAGE COMPARISON ---\n")
	for _, h := range model.Headers() {
		refCov := refReport.FieldCoverage[h]
		targetCov := targetReport.FieldCoverage[h]
		sb.WriteString(fmt.Sprintf("%-28s | Ref: %4d (%5.1f%%) | Target: %4d (%5.1f%%)\n",
			h,
			refCov, float64(refCov)/float64(refReport.TotalRows)*100,
			targetCov, float64(targetCov)/float64(targetReport.TotalRows)*100,
		))
	}

	if len(targetReport.RowErrors) > 0 {
		sb.WriteString("\n--- TARGET ROW WARNINGS/ERRORS (Sample) ---\n")
		for i, errStr := range targetReport.RowErrors {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("  ... and %d more errors\n", len(targetReport.RowErrors)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", errStr))
		}
	}

	return sb.String(), nil
}

// GetSurabayaKecamatansList returns the 31 official districts of Surabaya
func GetSurabayaKecamatansList() []string {
	return normalizer.SurabayaKecamatans
}

