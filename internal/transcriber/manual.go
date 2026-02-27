package transcriber

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"transcription-cli/internal/logging"
)

// ManualTranscriber pauses the pipeline and waits for human transcription.
type ManualTranscriber struct{}

// NewManual creates a ManualTranscriber.
func NewManual() *ManualTranscriber {
	return &ManualTranscriber{}
}

// Transcribe prints paths and pauses for the user to complete transcription manually.
func (m *ManualTranscriber) Transcribe(ctx context.Context, cfg Config) (int, error) {
	logging.StepInfo("Templates are ready for transcribers.")
	logging.StepInfo(fmt.Sprintf("Audio:     %s", cfg.AudioDir))
	logging.StepInfo(fmt.Sprintf("Templates: %s", cfg.TemplateDir))
	logging.StepInfo(fmt.Sprintf("Output:    %s", cfg.OutputDir))
	fmt.Fprintf(os.Stderr, "\n       Press Enter when all transcription is complete...")

	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')

	// Count XLSX files in output dir
	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		return 0, fmt.Errorf("reading transcribed directory: %w", err)
	}

	var count int
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".xlsx") {
			count++
		}
	}

	if count == 0 {
		return 0, fmt.Errorf("no XLSX files found in %s — transcription may not be complete", cfg.OutputDir)
	}

	return count, nil
}
