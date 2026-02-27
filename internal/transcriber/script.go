package transcriber

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"transcription-cli/internal/logging"
)

// ScriptTranscriber calls an external command/script for automated transcription.
type ScriptTranscriber struct {
	Command string // path to the script/binary
}

// NewScript creates a ScriptTranscriber with the given command.
func NewScript(cmd string) *ScriptTranscriber {
	return &ScriptTranscriber{Command: cmd}
}

// Transcribe executes the external script with --audio, --templates, --output flags.
func (s *ScriptTranscriber) Transcribe(ctx context.Context, cfg Config) (int, error) {
	slog.Info("running transcription script", "command", s.Command)

	cmd := exec.CommandContext(ctx, s.Command,
		"--audio", cfg.AudioDir,
		"--templates", cfg.TemplateDir,
		"--output", cfg.OutputDir,
	)

	// Stream output to stderr with prefix
	cmd.Stdout = &prefixWriter{prefix: "[script] ", w: os.Stderr}
	cmd.Stderr = &prefixWriter{prefix: "[script] ", w: os.Stderr}

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("transcription script failed: %w", err)
	}

	// Count produced XLSX files
	entries, err := os.ReadDir(cfg.OutputDir)
	if err != nil {
		return 0, fmt.Errorf("reading output directory: %w", err)
	}

	var count int
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".xlsx") {
			count++
		}
	}

	if count == 0 {
		return 0, fmt.Errorf("script completed but no XLSX files found in %s", cfg.OutputDir)
	}

	logging.StepSuccess(fmt.Sprintf("Script produced %d transcribed files", count))
	return count, nil
}

// prefixWriter adds a prefix to each line written.
type prefixWriter struct {
	prefix string
	w      *os.File
	buf    []byte
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	pw.buf = append(pw.buf, p...)
	for {
		idx := strings.IndexByte(string(pw.buf), '\n')
		if idx < 0 {
			break
		}
		line := string(pw.buf[:idx])
		pw.buf = pw.buf[idx+1:]
		fmt.Fprintf(pw.w, "       \033[2m%s%s\033[0m\n", pw.prefix, line)
	}
	return len(p), nil
}
