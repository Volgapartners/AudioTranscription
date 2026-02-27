package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"transcription-cli/internal/intake"
	"transcription-cli/internal/logging"
)

// Generate creates a manifest from local scan results and writes it to the output directory.
func Generate(scan *intake.ScanResult, outputDir string) (*Manifest, error) {
	start := time.Now()
	slog.Info("manifest generation started",
		logging.FieldPath, outputDir,
		logging.FieldCount, scan.TotalFiles)

	m := &Manifest{
		Version:     "1.0",
		GeneratedAt: time.Now().UTC(),
		TotalFiles:  scan.TotalFiles,
		Languages:   scan.FileCounts,
		Entries:     make([]Entry, 0, len(scan.Files)),
	}

	for _, f := range scan.Files {
		checksum, err := computeSHA256(filepath.Join(scan.RootPath, f.RelPath))
		if err != nil {
			return nil, fmt.Errorf("computing checksum for %s: %w", f.Filename, err)
		}

		baseName := strings.TrimSuffix(f.Filename, filepath.Ext(f.Filename))
		outputName := fmt.Sprintf("%s_%s", f.Language, baseName)

		m.Entries = append(m.Entries, Entry{
			Filename:   f.Filename,
			Language:   f.Language,
			RelPath:    f.RelPath,
			Size:       f.Size,
			SHA256:     checksum,
			OutputName: outputName,
		})
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output directory %s: %w", outputDir, err)
	}

	m.OutputPath = filepath.Join(outputDir, "manifest.json")
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling manifest: %w", err)
	}

	if err := os.WriteFile(m.OutputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("writing manifest to %s: %w", m.OutputPath, err)
	}

	slog.Info("manifest generation completed",
		logging.FieldPath, m.OutputPath,
		logging.FieldCount, m.TotalFiles,
		logging.FieldDurationMs, time.Since(start).Milliseconds())

	return m, nil
}

func computeSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
