// Package rework handles routing failed files and reprocessing them after fixes.
package rework

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"transcription-cli/internal/converter"
	"transcription-cli/internal/logging"
	"transcription-cli/internal/qa"
	"transcription-cli/internal/validator"
)

// ReworkManifest tracks files that need rework.
type ReworkManifest struct {
	CreatedAt   time.Time    `json:"created_at"`
	TotalRework int          `json:"total_rework"`
	Files       []ReworkFile `json:"files"`
}

// ReworkFile represents a single file needing rework.
type ReworkFile struct {
	Name       string `json:"name"`
	Reason     string `json:"reason"`      // "qa_rejected", "schema_violation", "client_feedback"
	Details    string `json:"details"`
	SourceStep string `json:"source_step"` // "qa", "validation", "client_feedback"
}

// ReworkResult summarizes reprocessing results.
type ReworkResult struct {
	Reprocessed int `json:"reprocessed"`
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
}

// TranscriptionFailure records a file that failed AI transcription.
type TranscriptionFailure struct {
	Filename string
	Language string
	Error    string
}

// RouteFailures copies failed files to the rework directory and creates a rework manifest.
// transcriptionFailures may be nil when not using AI transcription.
func RouteFailures(
	reworkDir string,
	transcribedDir string,
	qaResult *qa.QAResult,
	valSummary *validator.Summary,
	transcriptionFailures []TranscriptionFailure,
	reworkManifestPath string,
) error {
	slog.Info("routing failures to rework")

	xlsxDir := filepath.Join(reworkDir, "xlsx")
	if err := os.MkdirAll(xlsxDir, 0755); err != nil {
		return fmt.Errorf("creating rework xlsx dir: %w", err)
	}

	manifest := ReworkManifest{
		CreatedAt: time.Now().UTC(),
	}

	// Route QA-rejected files
	if qaResult != nil {
		for _, item := range qaResult.ReworkFiles {
			srcPath := filepath.Join(transcribedDir, item.Name)
			dstPath := filepath.Join(xlsxDir, item.Name)

			if err := copyFile(srcPath, dstPath); err != nil {
				slog.Warn("failed to copy rework file",
					logging.FieldFilename, item.Name,
					logging.FieldError, err)
				continue
			}

			manifest.Files = append(manifest.Files, ReworkFile{
				Name:       item.Name,
				Reason:     "qa_rejected",
				Details:    item.Notes,
				SourceStep: "qa",
			})
		}
	}

	// Route validation-failed files
	if valSummary != nil {
		failedFiles := validator.FailedFiles(valSummary)
		for _, filename := range failedFiles {
			// Find the corresponding XLSX
			xlsxName := strings.TrimSuffix(filename, ".json") + ".xlsx"
			srcPath := filepath.Join(transcribedDir, xlsxName)
			dstPath := filepath.Join(xlsxDir, xlsxName)

			if err := copyFile(srcPath, dstPath); err != nil {
				slog.Warn("failed to copy validation-failed file",
					logging.FieldFilename, xlsxName,
					logging.FieldError, err)
				continue
			}

			// Collect error details for this file
			var details []string
			for _, e := range valSummary.Errors {
				if e.File == filename {
					details = append(details, e.Message)
				}
			}

			manifest.Files = append(manifest.Files, ReworkFile{
				Name:       xlsxName,
				Reason:     "schema_violation",
				Details:    strings.Join(details, "; "),
				SourceStep: "validation",
			})
		}
	}

	// Route transcription failures (no XLSX to copy — just record in manifest)
	for _, tf := range transcriptionFailures {
		manifest.Files = append(manifest.Files, ReworkFile{
			Name:       tf.Filename,
			Reason:     "transcription_failed",
			Details:    tf.Error,
			SourceStep: "transcription",
		})
	}

	manifest.TotalRework = len(manifest.Files)

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling rework manifest: %w", err)
	}
	if err := os.WriteFile(reworkManifestPath, data, 0644); err != nil {
		return fmt.Errorf("writing rework manifest: %w", err)
	}

	slog.Info("rework routing complete",
		logging.FieldCount, manifest.TotalRework,
		logging.FieldPath, reworkManifestPath)

	return nil
}

// ReprocessRework re-runs conversion and validation on fixed files in the rework directory.
// Passing files are copied to the JSON output directory (non-destructive — never overwrites).
func ReprocessRework(reworkDir, jsonDir, schemaPath string) (*ReworkResult, error) {
	slog.Info("reprocessing rework files")

	xlsxDir := filepath.Join(reworkDir, "xlsx")
	reworkJSONDir := filepath.Join(reworkDir, "json")

	// Convert XLSX -> JSON
	jsonFiles, err := converter.ConvertAll(xlsxDir, reworkJSONDir)
	if err != nil {
		return nil, fmt.Errorf("converting rework files: %w", err)
	}

	result := &ReworkResult{
		Reprocessed: len(jsonFiles),
	}

	// Validate
	valSummary, err := validator.ValidateAll(reworkJSONDir, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("validating rework files: %w", err)
	}

	result.Passed = valSummary.Passed
	result.Failed = valSummary.Failed

	// Copy passing files to main JSON dir
	failedSet := make(map[string]bool)
	for _, f := range validator.FailedFiles(valSummary) {
		failedSet[f] = true
	}

	entries, err := os.ReadDir(reworkJSONDir)
	if err != nil {
		return nil, fmt.Errorf("reading rework json dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if failedSet[entry.Name()] {
			continue
		}

		srcPath := filepath.Join(reworkJSONDir, entry.Name())
		dstPath := filepath.Join(jsonDir, entry.Name())

		// Non-destructive: skip if already exists
		if _, err := os.Stat(dstPath); err == nil {
			slog.Warn("skipping rework file — already exists in output",
				logging.FieldFilename, entry.Name())
			continue
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			return nil, fmt.Errorf("copying rework result: %w", err)
		}
		logging.StepSuccess(fmt.Sprintf("rework passed: %s", entry.Name()))
	}

	slog.Info("rework reprocessing complete",
		"reprocessed", result.Reprocessed,
		logging.FieldPassed, result.Passed,
		logging.FieldFailed, result.Failed)

	return result, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
