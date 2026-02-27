package gdrive

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/api/drive/v3"

	"transcription-cli/internal/logging"
)

// DownloadOpts configures download behavior.
type DownloadOpts struct {
	Workers int // concurrent download goroutines (default 5)
}

func (o DownloadOpts) workers() int {
	if o.Workers <= 0 {
		return 5
	}
	return o.Workers
}

// DownloadReport summarizes a download operation.
type DownloadReport struct {
	TotalFiles      int            `json:"total_files"`
	Downloaded      int            `json:"downloaded"`
	Failed          int            `json:"failed"`
	ByLanguage      map[string]int `json:"by_language"`
	Errors          []FileError    `json:"errors,omitempty"`
	DurationMs      int64          `json:"duration_ms"`
}

// FileError records a per-file download failure.
type FileError struct {
	Filename string `json:"filename"`
	Language string `json:"language"`
	Error    string `json:"error"`
}

// Download downloads all audio files from a scan result to the local destination.
// Files are organized as destDir/lang=xx/filename.
func Download(ctx context.Context, srv *drive.Service, scan *ScanResult, destDir string, opts DownloadOpts) (*DownloadReport, error) {
	start := time.Now()
	slog.Info("download started",
		logging.FieldTotal, scan.TotalFiles,
		logging.FieldPath, destDir)

	// Handle ZIP-only case
	if scan.NeedsZIPExtraction {
		return downloadAndExtractZIP(ctx, srv, scan.ZIPFileID, destDir, opts)
	}

	report := &DownloadReport{
		TotalFiles: scan.TotalFiles,
		ByLanguage: make(map[string]int),
	}

	// Create language directories
	for _, lang := range scan.Languages {
		langDir := filepath.Join(destDir, "lang="+lang)
		if err := os.MkdirAll(langDir, 0755); err != nil {
			return nil, fmt.Errorf("creating dir %s: %w", langDir, err)
		}
	}

	// Worker pool for concurrent downloads
	type result struct {
		file DriveFile
		err  error
	}

	fileCh := make(chan DriveFile, len(scan.Files))
	resultCh := make(chan result, len(scan.Files))

	var wg sync.WaitGroup
	for i := 0; i < opts.workers(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range fileCh {
				destPath := filepath.Join(destDir, "lang="+f.Language, f.Name)
				err := downloadFile(ctx, srv, f.ID, destPath)
				resultCh <- result{file: f, err: err}
			}
		}()
	}

	// Feed files to workers
	for _, f := range scan.Files {
		fileCh <- f
	}
	close(fileCh)

	// Collect results in background
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Track per-language progress
	langProgress := make(map[string]*atomic.Int32)
	for _, lang := range scan.Languages {
		langProgress[lang] = &atomic.Int32{}
	}

	var mu sync.Mutex
	for r := range resultCh {
		if r.err != nil {
			mu.Lock()
			report.Failed++
			report.Errors = append(report.Errors, FileError{
				Filename: r.file.Name,
				Language: r.file.Language,
				Error:    r.err.Error(),
			})
			mu.Unlock()
			slog.Error("download failed",
				logging.FieldFilename, r.file.Name,
				logging.FieldLanguage, r.file.Language,
				logging.FieldError, r.err)
		} else {
			mu.Lock()
			report.Downloaded++
			report.ByLanguage[r.file.Language]++
			mu.Unlock()

			counter := langProgress[r.file.Language]
			n := counter.Add(1)
			total := scan.FileCounts[r.file.Language]
			logging.StepDetail(fmt.Sprintf("[lang=%s] %d/%d downloaded", r.file.Language, n, total))
		}
	}

	report.DurationMs = time.Since(start).Milliseconds()

	slog.Info("download completed",
		"downloaded", report.Downloaded,
		"failed", report.Failed,
		logging.FieldDurationMs, report.DurationMs)

	return report, nil
}

// downloadFile downloads a single file from Drive to the local path.
// Uses a temp file + rename for safe writes.
func downloadFile(ctx context.Context, srv *drive.Service, fileID, destPath string) error {
	tmpPath := destPath + ".incomplete"

	var resp *io.ReadCloser
	err := withRetry(ctx, "download "+filepath.Base(destPath), func() error {
		httpResp, err := srv.Files.Get(fileID).
			SupportsAllDrives(true).
			Context(ctx).
			Download()
		if err != nil {
			return err
		}
		resp = &httpResp.Body
		return nil
	})
	if err != nil {
		return err
	}
	defer (*resp).Close()

	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	if _, err := io.Copy(out, *resp); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing file: %w", err)
	}
	out.Close()

	return os.Rename(tmpPath, destPath)
}

// downloadAndExtractZIP downloads a ZIP file from Drive and extracts it.
func downloadAndExtractZIP(ctx context.Context, srv *drive.Service, zipFileID, destDir string, opts DownloadOpts) (*DownloadReport, error) {
	start := time.Now()
	slog.Info("downloading ZIP archive for extraction")

	zipPath := filepath.Join(destDir, "_temp_archive.zip")
	if err := downloadFile(ctx, srv, zipFileID, zipPath); err != nil {
		return nil, fmt.Errorf("downloading ZIP: %w", err)
	}
	defer os.Remove(zipPath)

	// Extract ZIP
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("opening ZIP: %w", err)
	}
	defer r.Close()

	report := &DownloadReport{
		ByLanguage: make(map[string]int),
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Validate path is within lang=xx/ structure
		// Clean the path to prevent zip slip
		cleanName := filepath.Clean(f.Name)
		if strings.Contains(cleanName, "..") {
			continue
		}

		parts := strings.SplitN(cleanName, string(filepath.Separator), 2)
		if len(parts) < 2 {
			// Try forward slash (ZIP standard)
			parts = strings.SplitN(cleanName, "/", 2)
		}
		if len(parts) < 2 {
			continue
		}

		destPath := filepath.Join(destDir, cleanName)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return nil, fmt.Errorf("creating dir for %s: %w", cleanName, err)
		}

		if err := extractZIPFile(f, destPath); err != nil {
			report.Failed++
			report.Errors = append(report.Errors, FileError{
				Filename: f.Name,
				Error:    err.Error(),
			})
			continue
		}

		report.Downloaded++
		report.TotalFiles++

		// Track language from folder name
		folderName := parts[0]
		if matches := validLanguagePattern.FindStringSubmatch(folderName); matches != nil {
			report.ByLanguage[matches[1]]++
		}
	}

	report.DurationMs = time.Since(start).Milliseconds()
	return report, nil
}

func extractZIPFile(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("opening zip entry: %w", err)
	}
	defer rc.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer out.Close()

	// Limit extraction size to prevent zip bombs (1GB per file)
	_, err = io.Copy(out, io.LimitReader(rc, 1<<30))
	return err
}

// DownloadCompleted downloads completed XLSX files from a Drive folder to a local directory.
// Used after transcription to collect filled-in templates from the Volga Drive folder.
func DownloadCompleted(ctx context.Context, srv *drive.Service, folderID, destDir string, opts DownloadOpts) (int, error) {
	slog.Info("downloading completed XLSX from Drive", logging.FieldFolderID, folderID)

	items, err := listFolder(ctx, srv, folderID)
	if err != nil {
		return 0, fmt.Errorf("listing completed folder: %w", err)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return 0, fmt.Errorf("creating dest dir: %w", err)
	}

	var downloaded int
	type dlResult struct {
		name string
		err  error
	}

	fileCh := make(chan *drive.File, len(items))
	resultCh := make(chan dlResult, len(items))

	var wg sync.WaitGroup
	for i := 0; i < opts.workers(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range fileCh {
				if !strings.HasSuffix(strings.ToLower(item.Name), ".xlsx") {
					continue
				}
				destPath := filepath.Join(destDir, item.Name)
				err := downloadFile(ctx, srv, item.Id, destPath)
				resultCh <- dlResult{name: item.Name, err: err}
			}
		}()
	}

	for _, item := range items {
		fileCh <- item
	}
	close(fileCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		if r.err != nil {
			slog.Error("failed to download completed XLSX",
				logging.FieldFilename, r.name,
				logging.FieldError, r.err)
		} else {
			downloaded++
		}
	}

	slog.Info("completed XLSX download finished", logging.FieldCount, downloaded)
	return downloaded, nil
}
