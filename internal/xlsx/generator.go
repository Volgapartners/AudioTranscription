// Package xlsx provides XLSX template generation for transcription workflows.
package xlsx

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/xuri/excelize/v2"

	"transcription-cli/internal/logging"
	"transcription-cli/internal/manifest"
)

// Columns defines the fixed template schema.
var Columns = []string{
	"segment_id",  // string, required
	"start_time",  // float, required (seconds)
	"end_time",    // float, required (seconds)
	"speaker",     // string, required (e.g., "speaker_01")
	"text",        // string, required
	"overlap",     // string, optional ("[OVERLAP]" or omit)
}

// GenerateAll creates XLSX templates for all manifest entries.
// Returns the count of templates generated.
func GenerateAll(m *manifest.Manifest, outputDir string) (int, error) {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return 0, fmt.Errorf("creating template directory %s: %w", outputDir, err)
	}

	slog.Info("template generation started",
		logging.FieldPath, outputDir,
		logging.FieldCount, len(m.Entries))

	for _, entry := range m.Entries {
		if err := Generate(entry, outputDir); err != nil {
			return 0, fmt.Errorf("generating template for %s: %w", entry.OutputName, err)
		}
	}

	slog.Info("template generation completed",
		logging.FieldCount, len(m.Entries),
		logging.FieldDurationMs, time.Since(start).Milliseconds())

	return len(m.Entries), nil
}

// Generate creates a single XLSX template for a manifest entry.
func Generate(entry manifest.Entry, outputDir string) error {
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			slog.Warn("failed to close xlsx file",
				logging.FieldOutputName, entry.OutputName,
				logging.FieldError, err)
		}
	}()

	// Header row only — transcribers add data rows
	for i, col := range Columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue("Sheet1", cell, col); err != nil {
			return fmt.Errorf("setting header cell %s: %w", cell, err)
		}
	}

	if err := addMetadata(f, entry); err != nil {
		return fmt.Errorf("adding metadata sheet: %w", err)
	}

	outputPath := filepath.Join(outputDir, entry.OutputName+".xlsx")
	if err := f.SaveAs(outputPath); err != nil {
		return fmt.Errorf("saving file %s: %w", outputPath, err)
	}

	return nil
}

// addMetadata creates a hidden sheet with file metadata.
func addMetadata(f *excelize.File, entry manifest.Entry) error {
	sheetName := "_metadata"
	if _, err := f.NewSheet(sheetName); err != nil {
		return fmt.Errorf("creating metadata sheet: %w", err)
	}

	metadata := [][]string{
		{"audio_file", entry.Filename},
		{"language", entry.Language},
		{"output_name", entry.OutputName},
		{"sha256", entry.SHA256},
	}

	for row, pair := range metadata {
		keyCell, _ := excelize.CoordinatesToCellName(1, row+1)
		valCell, _ := excelize.CoordinatesToCellName(2, row+1)
		if err := f.SetCellValue(sheetName, keyCell, pair[0]); err != nil {
			return fmt.Errorf("setting metadata key: %w", err)
		}
		if err := f.SetCellValue(sheetName, valCell, pair[1]); err != nil {
			return fmt.Errorf("setting metadata value: %w", err)
		}
	}

	if err := f.SetSheetVisible(sheetName, false); err != nil {
		return fmt.Errorf("hiding metadata sheet: %w", err)
	}

	return nil
}
