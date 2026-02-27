package gdrive

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/drive/v3"

	"transcription-cli/internal/logging"
)

// UploadReport summarizes an upload operation.
type UploadReport struct {
	TotalFiles int   `json:"total_files"`
	Uploaded   int   `json:"uploaded"`
	Failed     int   `json:"failed"`
	DurationMs int64 `json:"duration_ms"`
}

// CreateFolder creates a folder in Google Drive. Returns the new folder's ID.
// If a folder with the same name already exists under the parent, returns its ID (idempotent).
func CreateFolder(ctx context.Context, srv *drive.Service, parentFolderID, name string) (string, error) {
	// Check if folder already exists
	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", parentFolderID, name)

	var existing *drive.FileList
	err := withRetry(ctx, "check existing folder", func() error {
		var err error
		existing, err = srv.Files.List().
			Q(q).
			Fields("files(id)").
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Context(ctx).
			Do()
		return err
	})
	if err != nil {
		return "", fmt.Errorf("checking for existing folder: %w", err)
	}

	if len(existing.Files) > 0 {
		return existing.Files[0].Id, nil
	}

	// Create new folder
	folder := &drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentFolderID},
	}

	var created *drive.File
	err = withRetry(ctx, "create folder", func() error {
		var err error
		created, err = srv.Files.Create(folder).
			SupportsAllDrives(true).
			Fields("id").
			Context(ctx).
			Do()
		return err
	})
	if err != nil {
		return "", fmt.Errorf("creating folder %q: %w", name, err)
	}

	slog.Info("Drive folder created", "name", name, logging.FieldFolderID, created.Id)
	return created.Id, nil
}

// UploadFolder uploads all files from a local directory to a Drive folder.
// Preserves subfolder structure. Uses concurrent workers.
func UploadFolder(ctx context.Context, srv *drive.Service, localDir, parentFolderID string, opts DownloadOpts) (*UploadReport, error) {
	start := time.Now()
	slog.Info("upload started", logging.FieldPath, localDir, logging.FieldFolderID, parentFolderID)

	// Collect all files to upload
	type uploadItem struct {
		localPath    string
		parentFolder string // Drive folder ID for this file
		relPath      string
	}

	var items []uploadItem
	// Map of local subdirectory -> Drive folder ID
	folderMap := map[string]string{
		".": parentFolderID,
	}

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(localDir, path)
		if relPath == "." {
			return nil
		}

		if info.IsDir() {
			// Create corresponding Drive folder
			parentRel := filepath.Dir(relPath)
			if parentRel == "" {
				parentRel = "."
			}
			parentDriveID := folderMap[parentRel]

			driveID, createErr := CreateFolder(ctx, srv, parentDriveID, info.Name())
			if createErr != nil {
				return createErr
			}
			folderMap[relPath] = driveID
			return nil
		}

		parentRel := filepath.Dir(relPath)
		if parentRel == "" {
			parentRel = "."
		}
		parentDriveID := folderMap[parentRel]

		items = append(items, uploadItem{
			localPath:    path,
			parentFolder: parentDriveID,
			relPath:      relPath,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	report := &UploadReport{
		TotalFiles: len(items),
	}

	if len(items) == 0 {
		return report, nil
	}

	// Upload files concurrently
	type result struct {
		relPath string
		err     error
	}

	itemCh := make(chan uploadItem, len(items))
	resultCh := make(chan result, len(items))

	var wg sync.WaitGroup
	for i := 0; i < opts.workers(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range itemCh {
				_, err := UploadFile(ctx, srv, item.localPath, item.parentFolder)
				resultCh <- result{relPath: item.relPath, err: err}
			}
		}()
	}

	for _, item := range items {
		itemCh <- item
	}
	close(itemCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		if r.err != nil {
			report.Failed++
			slog.Error("upload failed",
				logging.FieldFilename, r.relPath,
				logging.FieldError, r.err)
		} else {
			report.Uploaded++
			logging.StepDetail(fmt.Sprintf("uploaded: %s", r.relPath))
		}
	}

	report.DurationMs = time.Since(start).Milliseconds()

	slog.Info("upload completed",
		"uploaded", report.Uploaded,
		"failed", report.Failed,
		logging.FieldDurationMs, report.DurationMs)

	return report, nil
}

// UploadFile uploads a single local file to a Drive folder.
func UploadFile(ctx context.Context, srv *drive.Service, localPath, parentFolderID string) (string, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	name := filepath.Base(localPath)

	// Determine MIME type from extension
	mimeType := "application/octet-stream"
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".json":
		mimeType = "application/json"
	case ".xlsx":
		mimeType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".txt":
		mimeType = "text/plain"
	}

	driveFile := &drive.File{
		Name:    name,
		Parents: []string{parentFolderID},
	}

	var created *drive.File
	err = withRetry(ctx, "upload "+name, func() error {
		// Re-seek to beginning on retry
		if _, seekErr := f.Seek(0, 0); seekErr != nil {
			return seekErr
		}
		var uploadErr error
		created, uploadErr = srv.Files.Create(driveFile).
			Media(f).
			SupportsAllDrives(true).
			Fields("id").
			Context(ctx).
			Do()
		return uploadErr
	})
	if err != nil {
		return "", fmt.Errorf("uploading %s: %w", name, err)
	}

	_ = mimeType // MIME type auto-detected by Drive API from content
	return created.Id, nil
}
