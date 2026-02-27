// Package reconciler provides cross-stage count verification for the transcription pipeline.
package reconciler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"transcription-cli/internal/converter"
	"transcription-cli/internal/logging"
	"transcription-cli/internal/manifest"
)

// StageCount holds counts from every stage for cross-stage reconciliation.
type StageCount struct {
	IntakeTotal    int            `json:"intake_total"`
	ManifestTotal  int            `json:"manifest_total"`
	TemplatesTotal int            `json:"templates_total"`
	QAApproved     int            `json:"qa_approved"`
	QARework       int            `json:"qa_rework"`
	JSONProduced   int            `json:"json_produced"`
	JSONPassed     int            `json:"json_passed"`
	JSONFailed     int            `json:"json_failed"`
	ByLanguage     map[string]int `json:"by_language,omitempty"`
}

// Report contains the results of cross-stage reconciliation.
type Report struct {
	ReconciledAt   time.Time     `json:"reconciled_at"`
	Stages         StageCount    `json:"stages"`
	ManifestCount  int           `json:"manifest_count"`
	JSONCount      int           `json:"json_count"`
	IsComplete     bool          `json:"is_complete"`
	Missing        []MissingFile `json:"missing,omitempty"`
	Extra          []string      `json:"extra,omitempty"`
	TotalSegments  int           `json:"total_segments"`
	UniqueSpeakers int           `json:"unique_speakers"`
	Discrepancies  []Discrepancy `json:"discrepancies,omitempty"`
}

// MissingFile represents a file expected from manifest but not found in output.
type MissingFile struct {
	Language   string `json:"language"`
	Filename   string `json:"filename"`
	OutputName string `json:"output_name"`
}

// Discrepancy records where counts don't match between stages.
type Discrepancy struct {
	Between  string `json:"between"`
	Expected int    `json:"expected"`
	Actual   int    `json:"actual"`
	Message  string `json:"message"`
}

// Reconcile verifies counts across all pipeline stages and checks manifest against JSON output.
func Reconcile(counts StageCount, manifestPath, jsonDir string) (*Report, error) {
	start := time.Now()
	slog.Info("reconciliation started",
		"manifest", manifestPath,
		logging.FieldPath, jsonDir)

	// Load manifest
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", manifestPath, err)
	}

	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	// Build expected set from manifest
	expected := make(map[string]manifest.Entry)
	for _, entry := range m.Entries {
		expected[entry.OutputName] = entry
	}

	// Scan JSON directory for actual files
	actual := make(map[string]bool)
	entries, err := os.ReadDir(jsonDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading json directory %s: %w", jsonDir, err)
	}

	report := &Report{
		ReconciledAt:  time.Now().UTC(),
		Stages:        counts,
		ManifestCount: len(m.Entries),
	}

	speakers := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		outputName := strings.TrimSuffix(entry.Name(), ".json")
		actual[outputName] = true
		report.JSONCount++

		jsonPath := filepath.Join(jsonDir, entry.Name())
		jsonData, err := os.ReadFile(jsonPath)
		if err != nil {
			continue
		}

		var trans converter.Transcription
		if err := json.Unmarshal(jsonData, &trans); err != nil {
			continue
		}

		report.TotalSegments += len(trans.Utterances)
		for _, utt := range trans.Utterances {
			speakers[utt.Speaker] = true
		}
	}

	report.UniqueSpeakers = len(speakers)

	// Find missing files
	for outputName, entry := range expected {
		if !actual[outputName] {
			report.Missing = append(report.Missing, MissingFile{
				Language:   entry.Language,
				Filename:   entry.Filename,
				OutputName: entry.OutputName,
			})
		}
	}

	// Find extra files
	for outputName := range actual {
		if _, ok := expected[outputName]; !ok {
			report.Extra = append(report.Extra, outputName)
		}
	}

	// Cross-stage discrepancy checks
	if counts.IntakeTotal > 0 && counts.ManifestTotal != counts.IntakeTotal {
		report.Discrepancies = append(report.Discrepancies, Discrepancy{
			Between:  "intake → manifest",
			Expected: counts.IntakeTotal,
			Actual:   counts.ManifestTotal,
			Message:  fmt.Sprintf("intake has %d files but manifest has %d", counts.IntakeTotal, counts.ManifestTotal),
		})
	}
	if counts.TemplatesTotal > 0 && counts.TemplatesTotal != counts.ManifestTotal {
		report.Discrepancies = append(report.Discrepancies, Discrepancy{
			Between:  "manifest → templates",
			Expected: counts.ManifestTotal,
			Actual:   counts.TemplatesTotal,
			Message:  fmt.Sprintf("manifest has %d entries but %d templates generated", counts.ManifestTotal, counts.TemplatesTotal),
		})
	}
	if counts.QAApproved+counts.QARework > 0 {
		qaTotal := counts.QAApproved + counts.QARework
		if qaTotal != counts.TemplatesTotal {
			report.Discrepancies = append(report.Discrepancies, Discrepancy{
				Between:  "templates → QA",
				Expected: counts.TemplatesTotal,
				Actual:   qaTotal,
				Message:  fmt.Sprintf("templates: %d, QA approved+rework: %d", counts.TemplatesTotal, qaTotal),
			})
		}
	}
	if counts.JSONProduced > 0 && counts.JSONProduced != counts.QAApproved {
		report.Discrepancies = append(report.Discrepancies, Discrepancy{
			Between:  "QA approved → JSON produced",
			Expected: counts.QAApproved,
			Actual:   counts.JSONProduced,
			Message:  fmt.Sprintf("QA approved: %d, JSON produced: %d", counts.QAApproved, counts.JSONProduced),
		})
	}

	report.IsComplete = len(report.Missing) == 0 && len(report.Extra) == 0 && len(report.Discrepancies) == 0

	slog.Info("reconciliation completed",
		"manifest_count", report.ManifestCount,
		"json_count", report.JSONCount,
		"is_complete", report.IsComplete,
		"discrepancies", len(report.Discrepancies),
		"total_segments", report.TotalSegments,
		"unique_speakers", report.UniqueSpeakers,
		logging.FieldDurationMs, time.Since(start).Milliseconds())

	return report, nil
}

// WriteReport writes the reconciliation report to a JSON file.
func WriteReport(report *Report, outputPath string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling reconciliation report: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("writing reconciliation report: %w", err)
	}
	slog.Info("reconciliation report saved", logging.FieldPath, outputPath)
	return nil
}
