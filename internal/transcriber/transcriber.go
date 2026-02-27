// Package transcriber defines the pluggable transcription interface.
package transcriber

import (
	"context"

	"transcription-cli/internal/manifest"
)

// Config holds the paths needed for transcription.
type Config struct {
	AudioDir    string             // 01_audio/ — source audio files
	TemplateDir string             // 03_templates/ — empty XLSX templates
	OutputDir   string             // 04_transcribed/ — where filled XLSX go
	Manifest    *manifest.Manifest // manifest for reference
}

// Transcriber defines how transcription is performed.
// Implementations can be manual (wait for human) or automated (call external scripts).
type Transcriber interface {
	// Transcribe fills templates with transcription data.
	// Returns the count of transcribed files.
	Transcribe(ctx context.Context, cfg Config) (int, error)
}
