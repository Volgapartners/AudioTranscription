// Package delivery handles packaging final output and uploading to the client's Drive.
package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"google.golang.org/api/drive/v3"

	"transcription-cli/internal/gdrive"
	"transcription-cli/internal/logging"
	"transcription-cli/internal/reconciler"
	"transcription-cli/internal/validator"
)

// DeliverySummary is the machine-readable delivery summary.
type DeliverySummary struct {
	DeliveredAt    time.Time      `json:"delivered_at"`
	ProjectName    string         `json:"project_name"`
	Formats        []string       `json:"formats"`
	CountsPerLang  map[string]int `json:"counts_per_language"`
	TotalDelivered int            `json:"total_delivered"`
	MatchesManifest bool          `json:"matches_manifest"`
	Exclusions     []string       `json:"exclusions,omitempty"`
	Notes          []string       `json:"notes,omitempty"`
}

// Package organizes the final validated output for delivery.
// Returns the delivery staging directory and summary.
func Package(
	projectName string,
	jsonDir string,
	reworkCount int,
	valSummary *validator.Summary,
	reconReport *reconciler.Report,
	summaryJSONPath string,
	summaryTextPath string,
) (*DeliverySummary, error) {
	slog.Info("packaging delivery")

	// Count JSON files by language
	countsPerLang := make(map[string]int)
	var totalDelivered int

	entries, err := os.ReadDir(jsonDir)
	if err != nil {
		return nil, fmt.Errorf("reading json dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		// Extract language from filename: lang_rest.json
		name := strings.TrimSuffix(entry.Name(), ".json")
		parts := strings.SplitN(name, "_", 2)
		if len(parts) >= 2 {
			countsPerLang[parts[0]]++
		}
		totalDelivered++
	}

	var exclusions []string
	if reworkCount > 0 {
		exclusions = append(exclusions, fmt.Sprintf("%d files in rework", reworkCount))
	}

	summary := &DeliverySummary{
		DeliveredAt:     time.Now().UTC(),
		ProjectName:     projectName,
		Formats:         []string{"json"},
		CountsPerLang:   countsPerLang,
		TotalDelivered:  totalDelivered,
		MatchesManifest: reconReport != nil && len(reconReport.Discrepancies) == 0,
		Exclusions:      exclusions,
	}

	// Write machine-readable summary
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling delivery summary: %w", err)
	}
	if err := os.WriteFile(summaryJSONPath, data, 0644); err != nil {
		return nil, fmt.Errorf("writing delivery summary JSON: %w", err)
	}

	// Write human-readable summary
	text := formatTextSummary(summary, reconReport)
	if err := os.WriteFile(summaryTextPath, []byte(text), 0644); err != nil {
		return nil, fmt.Errorf("writing delivery summary text: %w", err)
	}

	slog.Info("delivery packaged",
		logging.FieldCount, totalDelivered,
		logging.FieldPath, summaryJSONPath)

	return summary, nil
}

// Upload uploads the validated JSON files to the client's Drive folder.
func Upload(ctx context.Context, srv *drive.Service, jsonDir, deliveryFolderID, projectName string, opts gdrive.DownloadOpts) (*gdrive.UploadReport, error) {
	// Create dated delivery subfolder
	subfolderName := fmt.Sprintf("delivery_%s", time.Now().Format("2006-01-02_150405"))

	deliverySubID, err := gdrive.CreateFolder(ctx, srv, deliveryFolderID, subfolderName)
	if err != nil {
		return nil, fmt.Errorf("creating delivery subfolder: %w", err)
	}

	slog.Info("uploading delivery to Drive",
		"subfolder", subfolderName,
		logging.FieldFolderID, deliverySubID)

	report, err := gdrive.UploadFolder(ctx, srv, jsonDir, deliverySubID, opts)
	if err != nil {
		return nil, fmt.Errorf("uploading delivery: %w", err)
	}

	logging.StepSuccess(fmt.Sprintf("%d files uploaded to Drive/%s", report.Uploaded, subfolderName))
	return report, nil
}

func formatTextSummary(summary *DeliverySummary, recon *reconciler.Report) string {
	var b strings.Builder

	b.WriteString("DELIVERY SUMMARY\n")
	b.WriteString("================\n")
	b.WriteString(fmt.Sprintf("Project:    %s\n", summary.ProjectName))
	b.WriteString(fmt.Sprintf("Delivered:  %s\n\n", summary.DeliveredAt.Format("2006-01-02 15:04 MST")))

	b.WriteString(fmt.Sprintf("Formats:    %s\n\n", strings.Join(summary.Formats, ", ")))

	b.WriteString("Per Language:\n")
	for lang, count := range summary.CountsPerLang {
		b.WriteString(fmt.Sprintf("  %s:  %d files\n", lang, count))
	}
	b.WriteString("─────────────\n")
	b.WriteString(fmt.Sprintf("Total:  %d files\n\n", summary.TotalDelivered))

	if summary.MatchesManifest {
		b.WriteString("Manifest match: YES\n")
	} else {
		b.WriteString("Manifest match: NO (see reconciliation report)\n")
	}

	if len(summary.Exclusions) > 0 {
		b.WriteString(fmt.Sprintf("\nExclusions: %s\n", strings.Join(summary.Exclusions, "; ")))
	}

	if len(summary.Notes) > 0 {
		b.WriteString(fmt.Sprintf("\nNotes: %s\n", strings.Join(summary.Notes, "; ")))
	} else {
		b.WriteString("\nNotes: None\n")
	}

	return b.String()
}
