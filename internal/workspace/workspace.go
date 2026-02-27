// Package workspace manages the local directory structure and state for a transcription project.
package workspace

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"transcription-cli/internal/logging"
)

// Directory names for the workspace structure.
const (
	DirAudio       = "01_audio"
	DirManifests   = "02_manifests"
	DirTemplates   = "03_templates"
	DirTranscribed = "04_transcribed"
	DirJSON        = "05_json"
	DirLogs        = "06_logs"
	DirRework      = "07_rework"
)

// Workspace represents a project workspace with typed paths.
type Workspace struct {
	Root       string `json:"root"`
	Audio      string `json:"audio"`
	Manifests  string `json:"manifests"`
	Templates  string `json:"templates"`
	Transcribed string `json:"transcribed"`
	JSON       string `json:"json"`
	Logs       string `json:"logs"`
	Rework     string `json:"rework"`
	State      *State `json:"-"`
}

// State tracks pipeline progress for crash recovery and resume.
type State struct {
	Version                string         `json:"version"`
	ProjectName            string         `json:"project_name"`
	CreatedAt              time.Time      `json:"created_at"`
	SourceFolderID         string         `json:"source_folder_id"`
	VolgaFolderID          string         `json:"volga_folder_id,omitempty"`
	DeliveryFolderID       string         `json:"delivery_folder_id,omitempty"`
	VolgaTemplatesSubfolder string        `json:"volga_templates_subfolder_id,omitempty"`
	LocalMode              bool           `json:"local_mode,omitempty"`
	LocalSource            string         `json:"local_source,omitempty"`
	CurrentStep            int            `json:"current_step"`
	CompletedSteps         []int          `json:"completed_steps"`
	StepTimestamps         map[int]string `json:"step_timestamps"`
	Counts                 Counts         `json:"counts"`
}

// Counts tracks file counts at each stage.
type Counts struct {
	Intake      int `json:"intake"`
	Downloaded  int `json:"downloaded"`
	Manifest    int `json:"manifest"`
	Templates   int `json:"templates"`
	Transcribed int `json:"transcribed"`
	QAApproved  int `json:"qa_approved"`
	QARework    int `json:"qa_rework"`
	JSONProduced int `json:"json_produced"`
	JSONPassed  int `json:"json_passed"`
	JSONFailed  int `json:"json_failed"`
	Delivered   int `json:"delivered"`
}

// Create creates a new workspace with the standard directory structure.
// Idempotent — safe to call on an existing workspace.
func Create(baseDir, projectName string) (*Workspace, error) {
	root := filepath.Join(baseDir, projectName)

	ws := &Workspace{
		Root:        root,
		Audio:       filepath.Join(root, DirAudio),
		Manifests:   filepath.Join(root, DirManifests),
		Templates:   filepath.Join(root, DirTemplates),
		Transcribed: filepath.Join(root, DirTranscribed),
		JSON:        filepath.Join(root, DirJSON),
		Logs:        filepath.Join(root, DirLogs),
		Rework:      filepath.Join(root, DirRework),
	}

	dirs := []string{
		ws.Audio, ws.Manifests, ws.Templates,
		ws.Transcribed, ws.JSON, ws.Logs, ws.Rework,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	ws.State = &State{
		Version:        "1.0",
		ProjectName:    projectName,
		CreatedAt:      time.Now().UTC(),
		CompletedSteps: []int{},
		StepTimestamps: make(map[int]string),
	}

	slog.Info("workspace created", logging.FieldPath, root)
	return ws, nil
}

// Load loads an existing workspace from its root directory.
func Load(workspacePath string) (*Workspace, error) {
	root, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	ws := &Workspace{
		Root:        root,
		Audio:       filepath.Join(root, DirAudio),
		Manifests:   filepath.Join(root, DirManifests),
		Templates:   filepath.Join(root, DirTemplates),
		Transcribed: filepath.Join(root, DirTranscribed),
		JSON:        filepath.Join(root, DirJSON),
		Logs:        filepath.Join(root, DirLogs),
		Rework:      filepath.Join(root, DirRework),
	}

	// Load state
	statePath := filepath.Join(ws.Logs, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w (is this a valid workspace?)", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}
	ws.State = &state

	slog.Info("workspace loaded",
		logging.FieldPath, root,
		"current_step", state.CurrentStep,
		"completed", len(state.CompletedSteps))

	return ws, nil
}

// SaveState persists the current state to disk (atomic write).
func (ws *Workspace) SaveState() error {
	if ws.State == nil {
		return fmt.Errorf("no state to save")
	}

	data, err := json.MarshalIndent(ws.State, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	statePath := filepath.Join(ws.Logs, "state.json")
	tmpPath := statePath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing temp state: %w", err)
	}

	if err := os.Rename(tmpPath, statePath); err != nil {
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// CompleteStep marks a step as completed and saves state.
func (ws *Workspace) CompleteStep(step int) error {
	ws.State.CompletedSteps = append(ws.State.CompletedSteps, step)
	ws.State.StepTimestamps[step] = time.Now().UTC().Format(time.RFC3339)
	ws.State.CurrentStep = step + 1
	return ws.SaveState()
}

// IsStepCompleted checks if a given step has been completed.
func (ws *Workspace) IsStepCompleted(step int) bool {
	for _, s := range ws.State.CompletedSteps {
		if s == step {
			return true
		}
	}
	return false
}

// ManifestPath returns the path to the manifest file.
func (ws *Workspace) ManifestPath() string {
	return filepath.Join(ws.Manifests, "manifest.json")
}

// IntakeReportPath returns the path to the intake report.
func (ws *Workspace) IntakeReportPath() string {
	return filepath.Join(ws.Logs, "intake_report.json")
}

// QAChecklistPath returns the path to the QA checklist.
func (ws *Workspace) QAChecklistPath() string {
	return filepath.Join(ws.Logs, "qa_checklist.json")
}

// ValidationReportPath returns the path to the validation report.
func (ws *Workspace) ValidationReportPath() string {
	return filepath.Join(ws.Logs, "validation_report.json")
}

// ReconciliationReportPath returns the path to the reconciliation report.
func (ws *Workspace) ReconciliationReportPath() string {
	return filepath.Join(ws.Logs, "reconciliation_report.json")
}

// DeliverySummaryPath returns the path to the delivery summary JSON.
func (ws *Workspace) DeliverySummaryPath() string {
	return filepath.Join(ws.Logs, "delivery_summary.json")
}

// DeliverySummaryTextPath returns the path to the human-readable delivery summary.
func (ws *Workspace) DeliverySummaryTextPath() string {
	return filepath.Join(ws.Logs, "delivery_summary.txt")
}

// ReworkManifestPath returns the path to the rework manifest.
func (ws *Workspace) ReworkManifestPath() string {
	return filepath.Join(ws.Rework, "rework_manifest.json")
}
