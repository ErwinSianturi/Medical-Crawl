package preprocess

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"maps-scraper/pkg/model"
)

// PipelineReport holds all execution statistics
type PipelineReport struct {
	InputFile               string
	CleanOutputFile         string
	FlaggedOutputFile       string
	RejectedOutputFile      string
	TotalInputRows          int
	ValidSurabayaRows       int
	RejectedRows            int
	FlaggedRows             int
	DuplicateGroups         int
	DuplicateRecordsRemoved int
	PossibleDuplicates      int
	LocationConflicts       int
	MissingCoordinates      int
	IncompleteAddresses     int
	OutsideSurabaya         int
	ExecutionTime           time.Duration
}

// Pipeline orchestrates the entire validation & deduplication process
type Pipeline struct {
	cfg          Config
	validator    *Validator
	deduplicator *Deduplicator
}

// NewPipeline creates a new preprocessing pipeline
func NewPipeline(cfg Config) *Pipeline {
	return &Pipeline{
		cfg:          cfg,
		validator:    NewValidator(cfg),
		deduplicator: NewDeduplicator(cfg),
	}
}

// ProcessFile executes the full preprocessing pipeline on the input CSV file
func (p *Pipeline) ProcessFile(inputFile, cleanOut, flaggedOut, rejectedOut, reportOut string) (*PipelineReport, []*ProcessedRecord, error) {
	startTime := time.Now()

	// 1. Read input CSV
	file, err := os.Open(inputFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open input file %s: %w", inputFile, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.LazyQuotes = true

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read header from %s: %w", inputFile, err)
	}
	_ = header

	var rawRecords []*ProcessedRecord
	lineNum := 1 // Header is line 1

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		lineNum++
		if err != nil {
			continue
		}
		if len(row) < 17 {
			continue
		}

		rec := model.StoreRecord{
			PlaceID:           strings.TrimSpace(row[0]),
			Name:              strings.TrimSpace(row[1]),
			Address:           strings.TrimSpace(row[2]),
			City:              strings.TrimSpace(row[3]),
			Kecamatan:         strings.TrimSpace(row[4]),
			Latitude:          strings.TrimSpace(row[5]),
			Longitude:         strings.TrimSpace(row[6]),
			Types:             strings.TrimSpace(row[7]),
			Rating:            strings.TrimSpace(row[8]),
			Phone:             strings.TrimSpace(row[9]),
			Status:            strings.TrimSpace(row[10]),
			OpeningHours:      strings.TrimSpace(row[11]),
			PhotoURL:          strings.TrimSpace(row[12]),
			WebsiteLinks:      strings.TrimSpace(row[13]),
			SemenYangDijual:   strings.TrimSpace(row[14]),
			LinkSetinganTitik: strings.TrimSpace(row[15]),
			LinkSetinganKoma:  strings.TrimSpace(row[16]),
		}

		rawRecords = append(rawRecords, &ProcessedRecord{
			OriginalLine: lineNum,
			Record:       rec,
		})
	}

	report := &PipelineReport{
		InputFile:          inputFile,
		CleanOutputFile:    cleanOut,
		FlaggedOutputFile:  flaggedOut,
		RejectedOutputFile: rejectedOut,
		TotalInputRows:     len(rawRecords),
	}

	// 2. Step 1: Geographical & Consistency Validation
	var candidateForDedup []*ProcessedRecord

	for _, pr := range rawRecords {
		p.validator.ValidateRow(pr)

		// Audit stats
		if !pr.HasValidCoords {
			report.MissingCoordinates++
		}
		if len(strings.TrimSpace(pr.Record.Address)) < 20 {
			report.IncompleteAddresses++
		}

		switch pr.Status {
		case StatusValid:
			candidateForDedup = append(candidateForDedup, pr)
		case StatusInvalidOutsideSurabaya:
			report.OutsideSurabaya++
			report.RejectedRows++
		case StatusInvalidData:
			report.RejectedRows++
		case StatusFlaggedLocationConflict:
			report.LocationConflicts++
			report.FlaggedRows++
		case StatusFlaggedInsufficientLoc:
			report.FlaggedRows++
		}
	}

	// 3. Step 2: Deduplication on valid records
	candidates := p.deduplicator.DetectDuplicates(candidateForDedup)
	p.deduplicator.ResolveDuplicateGroups(candidateForDedup, candidates)

	// Count duplicate groups and resolved duplicates
	dupGroupSet := make(map[string]bool)
	var cleanRecords []*ProcessedRecord
	var flaggedRecords []*ProcessedRecord
	var rejectedRecords []*ProcessedRecord

	for _, pr := range rawRecords {
		if pr.DuplicateGroupID != "" {
			dupGroupSet[pr.DuplicateGroupID] = true
		}

		switch pr.Status {
		case StatusValid:
			cleanRecords = append(cleanRecords, pr)
		case StatusHighConfidenceDup:
			report.DuplicateRecordsRemoved++
			report.RejectedRows++
			rejectedRecords = append(rejectedRecords, pr)
		case StatusFlaggedPossibleDup:
			report.PossibleDuplicates++
			report.FlaggedRows++
			flaggedRecords = append(flaggedRecords, pr)
		case StatusFlaggedLocationConflict, StatusFlaggedInsufficientLoc:
			flaggedRecords = append(flaggedRecords, pr)
		case StatusInvalidOutsideSurabaya, StatusInvalidData:
			rejectedRecords = append(rejectedRecords, pr)
		}
	}

	report.ValidSurabayaRows = len(cleanRecords)
	report.DuplicateGroups = len(dupGroupSet)
	report.ExecutionTime = time.Since(startTime)

	// 4. Step 3: Write Output CSV Files
	// Write Clean Output
	if cleanOut != "" {
		if err := writeCleanCSV(cleanOut, cleanRecords); err != nil {
			return nil, nil, fmt.Errorf("failed to write clean CSV: %w", err)
		}
	}

	// Write Flagged Output
	if flaggedOut != "" {
		if err := writeFlaggedCSV(flaggedOut, flaggedRecords); err != nil {
			return nil, nil, fmt.Errorf("failed to write flagged CSV: %w", err)
		}
	}

	// Write Rejected Output
	if rejectedOut != "" {
		if err := writeRejectedCSV(rejectedOut, rejectedRecords); err != nil {
			return nil, nil, fmt.Errorf("failed to write rejected CSV: %w", err)
		}
	}

	// Write Report File
	if reportOut != "" {
		if err := writeReportText(reportOut, report); err != nil {
			return nil, nil, fmt.Errorf("failed to write report: %w", err)
		}
	}

	return report, rawRecords, nil
}

func writeCleanCSV(filePath string, records []*ProcessedRecord) error {
	_ = os.MkdirAll(filepath.Dir(filePath), 0755)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	w.Comma = ';'

	// Include standard headers + original_line as the first tracking column
	headers := append([]string{"original_line"}, model.Headers()...)
	if err := w.Write(headers); err != nil {
		return err
	}

	for _, pr := range records {
		row := append([]string{fmt.Sprintf("%d", pr.OriginalLine)}, pr.Record.ToRow()...)
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeFlaggedCSV(filePath string, records []*ProcessedRecord) error {
	_ = os.MkdirAll(filepath.Dir(filePath), 0755)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	w.Comma = ';'

	headers := append([]string{
		"original_line",
		"status",
		"reason",
		"confidence_score",
		"related_line",
		"duplicate_group_id",
		"recommended_action",
	}, model.Headers()...)

	if err := w.Write(headers); err != nil {
		return err
	}

	for _, pr := range records {
		relatedLineStr := ""
		if pr.RelatedLine > 0 {
			relatedLineStr = fmt.Sprintf("%d", pr.RelatedLine)
		}
		prefix := []string{
			fmt.Sprintf("%d", pr.OriginalLine),
			string(pr.Status),
			pr.Reason,
			fmt.Sprintf("%.2f", pr.ConfidenceScore),
			relatedLineStr,
			pr.DuplicateGroupID,
			pr.RecommendedAction,
		}
		row := append(prefix, pr.Record.ToRow()...)
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeRejectedCSV(filePath string, records []*ProcessedRecord) error {
	_ = os.MkdirAll(filepath.Dir(filePath), 0755)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	w.Comma = ';'

	headers := append([]string{
		"original_line",
		"status",
		"reason",
		"confidence_score",
		"related_line",
	}, model.Headers()...)

	if err := w.Write(headers); err != nil {
		return err
	}

	for _, pr := range records {
		relatedLineStr := ""
		if pr.RelatedLine > 0 {
			relatedLineStr = fmt.Sprintf("%d", pr.RelatedLine)
		}
		prefix := []string{
			fmt.Sprintf("%d", pr.OriginalLine),
			string(pr.Status),
			pr.Reason,
			fmt.Sprintf("%.2f", pr.ConfidenceScore),
			relatedLineStr,
		}
		row := append(prefix, pr.Record.ToRow()...)
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeReportText(filePath string, report *PipelineReport) error {
	_ = os.MkdirAll(filepath.Dir(filePath), 0755)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	content := fmt.Sprintf(`================================================================================
                    PREPROCESSING AUDIT REPORT
================================================================================
Input file:
%s

Output file:
%s

Flagged file:
%s

Rejected file:
%s

Total input rows:
%d

Valid Surabaya rows:
%d

Rejected rows:
%d

Flagged rows:
%d

Duplicate groups:
%d

Duplicate records removed:
%d

Possible duplicates:
%d

Location conflicts:
%d

Outside Surabaya:
%d

Records with missing coordinates:
%d

Records with incomplete addresses:
%d

Execution time:
%v
================================================================================
`,
		report.InputFile,
		report.CleanOutputFile,
		report.FlaggedOutputFile,
		report.RejectedOutputFile,
		report.TotalInputRows,
		report.ValidSurabayaRows,
		report.RejectedRows,
		report.FlaggedRows,
		report.DuplicateGroups,
		report.DuplicateRecordsRemoved,
		report.PossibleDuplicates,
		report.LocationConflicts,
		report.OutsideSurabaya,
		report.MissingCoordinates,
		report.IncompleteAddresses,
		report.ExecutionTime,
	)

	_, err = file.WriteString(content)
	return err
}
