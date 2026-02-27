package gdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"google.golang.org/api/drive/v3"

	"transcription-cli/internal/logging"
)

// validLanguagePattern matches lang=xx folder names (ISO 639-1 two-letter codes).
var validLanguagePattern = regexp.MustCompile(`^lang=([a-z]{2})$`)

// audioMIMEPrefixes defines MIME types considered audio.
var audioMIMEPrefixes = []string{
	"audio/",
}

// audioExtensions maps file extensions to audio types (fallback for unknown MIME).
var audioExtensions = map[string]bool{
	".wav": true, ".mp3": true, ".flac": true, ".m4a": true, ".ogg": true,
	".aac": true, ".wma": true, ".opus": true,
}

// ScanResult contains the results of scanning a Drive folder.
type ScanResult struct {
	FolderID           string              `json:"folder_id"`
	FolderName         string              `json:"folder_name"`
	Languages          []string            `json:"languages"`
	FileCounts         map[string]int      `json:"file_counts"`
	TotalFiles         int                 `json:"total_files"`
	Issues             []ScanIssue         `json:"issues"`
	IsValid            bool                `json:"is_valid"`
	Files              []DriveFile         `json:"files"`
	NeedsZIPExtraction bool                `json:"needs_zip_extraction,omitempty"`
	ZIPFileID          string              `json:"zip_file_id,omitempty"`
	VerifiedAt         time.Time           `json:"verified_at"`
	LangFolderIDs      map[string]string   `json:"-"` // lang code -> folder ID
}

// ScanIssue represents a validation problem found during scanning.
type ScanIssue struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// DriveFile represents a file discovered in the Drive folder.
type DriveFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language"`
	MIMEType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// ScanFolder validates the structure of a Google Drive folder for audio intake.
// Expects: only lang=xx folders at top level, each containing only audio files.
func ScanFolder(ctx context.Context, srv *drive.Service, folderID string) (*ScanResult, error) {
	start := time.Now()
	slog.Info("scanning Drive folder", logging.FieldFolderID, folderID)

	folderName, err := GetFolderName(ctx, srv, folderID)
	if err != nil {
		return nil, err
	}

	result := &ScanResult{
		FolderID:      folderID,
		FolderName:    folderName,
		FileCounts:    make(map[string]int),
		IsValid:       true,
		VerifiedAt:    time.Now().UTC(),
		LangFolderIDs: make(map[string]string),
	}

	// List top-level items
	topItems, err := listFolder(ctx, srv, folderID)
	if err != nil {
		return nil, fmt.Errorf("listing folder contents: %w", err)
	}

	var hasLangFolders bool
	var zipFile *drive.File

	for _, item := range topItems {
		if item.MimeType == "application/vnd.google-apps.folder" {
			// Validate folder name
			matches := validLanguagePattern.FindStringSubmatch(item.Name)
			if matches == nil {
				result.Issues = append(result.Issues, ScanIssue{
					Type:    "invalid_lang_folder",
					Path:    item.Name,
					Message: fmt.Sprintf("folder %q does not match 'lang=xx' pattern", item.Name),
				})
				result.IsValid = false
				continue
			}
			hasLangFolders = true
			lang := matches[1]
			result.Languages = append(result.Languages, lang)
			result.LangFolderIDs[lang] = item.Id

			// Scan language folder contents
			if err := scanLangFolder(ctx, srv, item.Id, lang, result); err != nil {
				return nil, fmt.Errorf("scanning lang=%s: %w", lang, err)
			}
		} else if item.MimeType == "application/zip" || strings.HasSuffix(strings.ToLower(item.Name), ".zip") {
			zipFile = item
		} else {
			result.Issues = append(result.Issues, ScanIssue{
				Type:    "stray_file",
				Path:    item.Name,
				Message: fmt.Sprintf("unexpected file at top level: %q (type: %s)", item.Name, item.MimeType),
			})
			result.IsValid = false
		}
	}

	// Handle ZIP-only scenario
	if !hasLangFolders && zipFile != nil {
		result.NeedsZIPExtraction = true
		result.ZIPFileID = zipFile.Id
		result.IsValid = true // Will be re-validated after extraction
		result.Issues = append(result.Issues, ScanIssue{
			Type:    "zip_detected",
			Path:    zipFile.Name,
			Message: "ZIP archive detected — will extract and re-validate structure",
		})
		slog.Info("ZIP archive detected, will extract locally",
			logging.FieldFilename, zipFile.Name)
	}

	slog.Info("Drive scan completed",
		logging.FieldTotal, result.TotalFiles,
		"languages", len(result.Languages),
		"issues", len(result.Issues),
		logging.FieldDurationMs, time.Since(start).Milliseconds())

	return result, nil
}

// scanLangFolder validates the contents of a lang=xx folder.
func scanLangFolder(ctx context.Context, srv *drive.Service, folderID, lang string, result *ScanResult) error {
	items, err := listFolder(ctx, srv, folderID)
	if err != nil {
		return fmt.Errorf("listing lang=%s: %w", lang, err)
	}

	for _, item := range items {
		if item.MimeType == "application/vnd.google-apps.folder" {
			result.Issues = append(result.Issues, ScanIssue{
				Type:    "nested_folder",
				Path:    fmt.Sprintf("lang=%s/%s", lang, item.Name),
				Message: "nested folders not allowed inside lang=xx",
			})
			result.IsValid = false
			continue
		}

		if !isAudioFile(item) {
			result.Issues = append(result.Issues, ScanIssue{
				Type:    "non_audio",
				Path:    fmt.Sprintf("lang=%s/%s", lang, item.Name),
				Message: fmt.Sprintf("non-audio file: %q (type: %s)", item.Name, item.MimeType),
			})
			result.IsValid = false
			continue
		}

		result.Files = append(result.Files, DriveFile{
			ID:       item.Id,
			Name:     item.Name,
			Language: lang,
			MIMEType: item.MimeType,
			Size:     item.Size,
		})
		result.FileCounts[lang]++
		result.TotalFiles++
	}

	return nil
}

// isAudioFile checks if a Drive file is an audio file by MIME type or extension.
func isAudioFile(f *drive.File) bool {
	for _, prefix := range audioMIMEPrefixes {
		if strings.HasPrefix(f.MimeType, prefix) {
			return true
		}
	}
	// Fallback: check extension (some drives report application/octet-stream)
	name := strings.ToLower(f.Name)
	for ext := range audioExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// listFolder lists all files in a Drive folder with pagination.
func listFolder(ctx context.Context, srv *drive.Service, folderID string) ([]*drive.File, error) {
	var all []*drive.File
	pageToken := ""

	for {
		q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
		var result *drive.FileList

		err := withRetry(ctx, "list folder", func() error {
			call := srv.Files.List().
				Q(q).
				Fields("nextPageToken, files(id, name, mimeType, size)").
				PageSize(1000).
				SupportsAllDrives(true).
				IncludeItemsFromAllDrives(true).
				OrderBy("name").
				Context(ctx)

			if pageToken != "" {
				call = call.PageToken(pageToken)
			}

			var err error
			result, err = call.Do()
			return err
		})
		if err != nil {
			return nil, err
		}

		all = append(all, result.Files...)
		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return all, nil
}

// WriteIntakeReport writes the scan result as an intake report JSON file.
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
