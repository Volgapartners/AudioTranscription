// Package gdrive provides Google Drive operations: scanning, downloading, and uploading.
package gdrive

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// NewService creates a Google Drive API service from an authenticated HTTP client.
func NewService(ctx context.Context, httpClient *http.Client) (*drive.Service, error) {
	srv, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("creating Drive service: %w", err)
	}
	return srv, nil
}

// folderIDPattern matches Google Drive folder/file IDs (alphanumeric, hyphens, underscores).
var folderIDPattern = regexp.MustCompile(`^[\w-]{10,}$`)

// foldersPathPattern extracts folder ID from URL paths like /drive/folders/ID or /drive/u/0/folders/ID
var foldersPathPattern = regexp.MustCompile(`/folders/([^/?]+)`)

// ParseFolderURL extracts a Google Drive folder ID from a URL or raw ID string.
// Supported formats:
//   - https://drive.google.com/drive/folders/FOLDER_ID
//   - https://drive.google.com/drive/u/0/folders/FOLDER_ID
//   - https://drive.google.com/drive/u/0/folders/FOLDER_ID?resourcekey=...
//   - https://drive.google.com/open?id=FOLDER_ID
//   - Raw folder ID string
func ParseFolderURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	// Check if it's already a raw folder ID
	if !strings.Contains(input, "/") && !strings.Contains(input, "?") {
		if folderIDPattern.MatchString(input) {
			return input, nil
		}
		return "", fmt.Errorf("input does not look like a valid folder ID: %s", input)
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

	// Format: /open?id=FOLDER_ID
	if strings.HasSuffix(parsed.Path, "/open") || parsed.Path == "/open" {
		id := parsed.Query().Get("id")
		if id != "" {
			return id, nil
		}
	}

	// Format: /drive/folders/FOLDER_ID or /drive/u/0/folders/FOLDER_ID
	matches := foldersPathPattern.FindStringSubmatch(parsed.Path)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("could not extract folder ID from URL: %s", input)
}

// ValidateAccess checks that the authenticated user can access the given folder.
// For write access, it attempts to get folder metadata with write fields.
func ValidateAccess(ctx context.Context, srv *drive.Service, folderID string, needWrite bool) error {
	file, err := srv.Files.Get(folderID).
		SupportsAllDrives(true).
		Fields("id, name, mimeType, capabilities").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("cannot access folder %s: %w", folderID, err)
	}

	if file.MimeType != "application/vnd.google-apps.folder" {
		return fmt.Errorf("%s is not a folder (type: %s)", folderID, file.MimeType)
	}

	if needWrite && file.Capabilities != nil && !file.Capabilities.CanAddChildren {
		return fmt.Errorf("no write access to folder %q (%s)", file.Name, folderID)
	}

	return nil
}

// GetFolderName returns the name of a Drive folder.
func GetFolderName(ctx context.Context, srv *drive.Service, folderID string) (string, error) {
	file, err := srv.Files.Get(folderID).
		SupportsAllDrives(true).
		Fields("name").
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("getting folder name: %w", err)
	}
	return file.Name, nil
}
