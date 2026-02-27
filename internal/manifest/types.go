// Package manifest provides types and generation for file manifests.
package manifest

import "time"

// Manifest represents a complete inventory of audio files to be processed.
type Manifest struct {
	Version     string         `json:"version"`
	GeneratedAt time.Time      `json:"generated_at"`
	TotalFiles  int            `json:"total_files"`
	Languages   map[string]int `json:"language_counts"`
	Entries     []Entry        `json:"entries"`
	OutputPath  string         `json:"-"` // Not serialized to JSON
}

// Entry represents a single audio file in the manifest.
type Entry struct {
	Filename   string `json:"filename"`
	Language   string `json:"language"`
	RelPath    string `json:"relative_path"`
	Size       int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
	OutputName string `json:"output_name"` // "{lang}_{filename}" without extension
}
