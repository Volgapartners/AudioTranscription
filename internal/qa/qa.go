// Package qa provides the QA gate: checklist generation, review, and approved/rework splitting.
package qa

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"transcription-cli/internal/logging"
)

// Checklist is the QA review checklist written for human review.
type Checklist struct {
	GeneratedAt time.Time       `json:"generated_at"`
	TotalFiles  int             `json:"total_files"`
	Files       []ChecklistItem `json:"files"`
}

// ChecklistItem represents a single file in the QA checklist.
type ChecklistItem struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	Status   string `json:"status"` // "pending", "approved", "rework"
	Notes    string `json:"notes"`
}

// QAResult contains the outcome of processing the QA checklist.
type QAResult struct {
	ApprovedFiles []string     `json:"approved_files"` // full paths to approved XLSX
	ReworkFiles   []ReworkItem `json:"rework_files"`
	ApprovedCount int          `json:"approved_count"`
	ReworkCount   int          `json:"rework_count"`
}

// ReworkItem represents a file marked for rework.
type ReworkItem struct {
	Name   string `json:"name"`
	Notes  string `json:"notes"`
}

// GenerateChecklist creates a QA checklist from the transcribed XLSX files.
func GenerateChecklist(transcribedDir, outputPath string) error {
	entries, err := os.ReadDir(transcribedDir)
	if err != nil {
		return fmt.Errorf("reading transcribed directory: %w", err)
	}

	checklist := Checklist{
		GeneratedAt: time.Now().UTC(),
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".xlsx") {
			continue
		}

		// Derive language from filename (lang_rest.xlsx -> lang)
		name := strings.TrimSuffix(entry.Name(), ".xlsx")
		lang := ""
		if parts := strings.SplitN(name, "_", 2); len(parts) >= 2 {
			lang = parts[0]
		}

		checklist.Files = append(checklist.Files, ChecklistItem{
			Name:     entry.Name(),
			Language: lang,
			Status:   "pending",
			Notes:    "",
		})
		checklist.TotalFiles++
	}

	data, err := json.MarshalIndent(checklist, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling checklist: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("writing checklist: %w", err)
	}

	slog.Info("QA checklist generated",
		logging.FieldPath, outputPath,
		logging.FieldCount, checklist.TotalFiles)

	return nil
}

// WaitForQA displays the QA prompt and waits for the user to press Enter.
func WaitForQA(checklistPath, transcribedDir string) {
	logging.Banner("QA REVIEW — MANUAL STEP")
	logging.StepInfo(fmt.Sprintf("Checklist: %s", checklistPath))
	logging.StepInfo(fmt.Sprintf("Files:     %s", transcribedDir))
	fmt.Fprintln(os.Stderr)
	logging.StepInfo("For each file, set status to \"approved\" or \"rework\".")
	logging.StepInfo("Add notes for any rework files explaining the issue.")
	fmt.Fprintf(os.Stderr, "\n       Press Enter when QA is complete...")

	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
	fmt.Fprintln(os.Stderr)
}

// ProcessChecklist reads the completed QA checklist and splits files into approved/rework.
// Returns an error if any files are still "pending".
func ProcessChecklist(checklistPath, transcribedDir string) (*QAResult, error) {
	data, err := os.ReadFile(checklistPath)
	if err != nil {
		return nil, fmt.Errorf("reading checklist: %w", err)
	}

	var checklist Checklist
	if err := json.Unmarshal(data, &checklist); err != nil {
		return nil, fmt.Errorf("parsing checklist: %w", err)
	}

	result := &QAResult{}

	var pendingFiles []string
	for _, item := range checklist.Files {
		switch strings.ToLower(item.Status) {
		case "approved":
			result.ApprovedFiles = append(result.ApprovedFiles,
				filepath.Join(transcribedDir, item.Name))
			result.ApprovedCount++
		case "rework":
			result.ReworkFiles = append(result.ReworkFiles, ReworkItem{
				Name:  item.Name,
				Notes: item.Notes,
			})
			result.ReworkCount++
		case "pending":
			pendingFiles = append(pendingFiles, item.Name)
		default:
			return nil, fmt.Errorf("unknown status %q for file %s (must be approved/rework)", item.Status, item.Name)
		}
	}

	if len(pendingFiles) > 0 {
		return nil, fmt.Errorf("QA incomplete: %d files still pending: %s",
			len(pendingFiles), strings.Join(pendingFiles, ", "))
	}

	slog.Info("QA checklist processed",
		"approved", result.ApprovedCount,
		"rework", result.ReworkCount)

	return result, nil
}
