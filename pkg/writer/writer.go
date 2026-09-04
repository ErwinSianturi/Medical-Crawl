package writer

import (
	"encoding/csv"
	"fmt"
	"os"

	"maps-scraper/pkg/model"
)

// CSVWriter writes StoreRecord slices to a strict semicolon-delimited CSV file
type CSVWriter struct {
	filePath string
}

// NewCSVWriter creates a new CSVWriter instance
func NewCSVWriter(filePath string) *CSVWriter {
	return &CSVWriter{filePath: filePath}
}

// WriteRecords writes records to the CSV file using exact reference CSV formatting
func (w *CSVWriter) WriteRecords(records []model.StoreRecord) error {
	file, err := os.Create(w.filePath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", w.filePath, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	writer.Comma = ';'
	writer.UseCRLF = false // Standard LF or CRLF match

	// Write Header
	if err := writer.Write(model.Headers()); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write Rows
	for _, rec := range records {
		if err := writer.Write(rec.ToRow()); err != nil {
			return fmt.Errorf("failed to write row for %s: %w", rec.Name, err)
		}
	}

	writer.Flush()
	return writer.Error()
}
