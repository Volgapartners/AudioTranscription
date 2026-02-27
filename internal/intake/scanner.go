// Package intake provides local directory scanning and validation for audio file intake.
// Used to validate the local working copy after downloading from Drive.
package intake

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"transcription-cli/internal/logging"
)

// ValidLanguagePattern matches ISO 639-1 two-letter codes only.
var ValidLanguagePattern = regexp.MustCompile(`^lang=([a-z]{2})$`)

// ValidAudioExtensions lists supported audio file types.
var ValidAudioExtensions = map[string]bool{
	".wav": true, ".mp3": true, ".flac": true, ".m4a": true,
	".ogg": true, ".aac": true, ".wma": true, ".opus": true,
}

// ScanResult contains the results of scanning a local input directory.
type ScanResult struct {
	RootPath   string
	Languages  []string
	FileCounts map[string]int
	TotalFiles int
	Issues     []Issue
	IsValid    bool
	Files      []AudioFile
}

// Issue represents a validation problem found during scanning.
type Issue struct {
	Type    string // "nested_folder", "invalid_lang", "non_audio"
	Path    string
	Message string
}

// AudioFile represents a discovered audio file.
type AudioFile struct {
	Filename string
	Language string
	RelPath  string
	Size     int64
}

// Scan validates and catalogs audio files in the given directory.
// It expects a flat structure with lang=xx folders containing audio files.
func Scan(rootPath string) (*ScanResult, error) {
	start := time.Now()
	slog.Info("local intake scan started", logging.FieldPath, rootPath)

	result := &ScanResult{
		RootPath:   rootPath,
		FileCounts: make(map[string]int),
		IsValid:    true,
	}

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", rootPath, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		matches := ValidLanguagePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			result.Issues = append(result.Issues, Issue{
				Type:    "invalid_lang",
				Path:    entry.Name(),
				Message: "folder must match 'lang=xx' pattern (2-letter code)",
			})
			result.IsValid = false
			continue
		}

		lang := matches[1]
		result.Languages = append(result.Languages, lang)

		langPath := filepath.Join(rootPath, entry.Name())
		if err := scanLanguageFolder(langPath, lang, result); err != nil {
			return nil, fmt.Errorf("scanning language folder %s: %w", langPath, err)
		}
	}

	slog.Info("local intake scan completed",
		logging.FieldCount, result.TotalFiles,
		"languages", len(result.Languages),
		"issues", len(result.Issues),
		logging.FieldDurationMs, time.Since(start).Milliseconds())

	return result, nil
}

// WriteIntakeReport serializes a ScanResult to JSON at the given path.
func WriteIntakeReport(result *ScanResult, outputPath string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling intake report: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("writing intake report: %w", err)
	}
	slog.Info("intake report saved", logging.FieldPath, outputPath)
	return nil
}

// CopyToWorkspace copies all audio files from the scan result into destDir,
// preserving the lang=xx/ directory structure. Uses hard copy for workspace isolation.
func CopyToWorkspace(scan *ScanResult, destDir string) (int, error) {
	copied := 0
	for _, f := range scan.Files {
		srcPath := filepath.Join(scan.RootPath, f.RelPath)
		dstPath := filepath.Join(destDir, f.RelPath)

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return copied, fmt.Errorf("creating directory for %s: %w", f.RelPath, err)
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			return copied, fmt.Errorf("copying %s: %w", f.RelPath, err)
		}
		copied++
	}

	slog.Info("files copied to workspace",
		logging.FieldCount, copied,
		logging.FieldPath, destDir)
	return copied, nil
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

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func scanLanguageFolder(langPath, lang string, result *ScanResult) error {
	entries, err := os.ReadDir(langPath)
	if err != nil {
		return fmt.Errorf("reading language directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			result.Issues = append(result.Issues, Issue{
				Type:    "nested_folder",
				Path:    filepath.Join(langPath, entry.Name()),
				Message: "nested folders not allowed inside lang=xx",
			})
			result.IsValid = false
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !ValidAudioExtensions[ext] {
			result.Issues = append(result.Issues, Issue{
				Type:    "non_audio",
				Path:    filepath.Join(langPath, entry.Name()),
				Message: fmt.Sprintf("unsupported extension %s", ext),
			})
			result.IsValid = false
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("getting file info for %s: %w", entry.Name(), err)
		}

		result.Files = append(result.Files, AudioFile{
			Filename: entry.Name(),
			Language: lang,
			RelPath:  filepath.Join("lang="+lang, entry.Name()),
			Size:     info.Size(),
		})
		result.FileCounts[lang]++
		result.TotalFiles++
	}

	return nil
}
