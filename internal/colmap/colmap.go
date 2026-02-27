// Package colmap provides fuzzy column-name resolution for vendor-provided
// transcription spreadsheets. It normalises messy header names (different
// casing, punctuation, spacing) into canonical column names and returns a
// header-index map so the converter can read any vendor format.
package colmap

import (
	"regexp"
	"strings"
)

// Canonical column names used throughout the converter.
const (
	ColAudioName  = "audio_name"
	ColSpeaker    = "speaker"
	ColStartTime  = "start_time"
	ColEndTime    = "end_time"
	ColTranscript = "transcript"
	ColEmotion    = "emotion"
	ColLocale     = "locale"
)

// aliases maps each canonical name to the set of known header variations
// collected from the 683 and 751 Python scripts.
var aliases = map[string][]string{
	ColAudioName: {
		"audio_name", "audio name", "file_name", "file name",
		"audio", "audioname",
	},
	ColSpeaker: {
		"speaker",
	},
	ColStartTime: {
		"start time (decimal)", "start time decimal",
		"start_time_decimal", "starttime_decimal",
		"start_time", "start time",
	},
	ColEndTime: {
		"end time (decimal)", "end time decimal",
		"end_time_decimal", "endtime_decimal",
		"end_time", "end time",
	},
	ColTranscript: {
		"transcript", "text",
	},
	ColEmotion: {
		"emotion", "emotions",
	},
	ColLocale: {
		"locale",
	},
}

var nonAlpha = regexp.MustCompile(`[^a-z0-9]+`)

// normHeader lowercases a header, replaces all non-alphanumeric runs with
// a single underscore, and trims leading/trailing underscores.
func normHeader(h string) string {
	s := strings.ToLower(strings.TrimSpace(h))
	s = nonAlpha.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// Resolve takes a list of raw column headers (as they appear in a
// spreadsheet) and returns a map from canonical column name to the
// zero-based column index. A canonical name maps to -1 if no matching
// header was found.
func Resolve(headers []string) map[string]int {
	// Build a lookup of normalised header -> index (first occurrence wins).
	norm := make(map[string]int, len(headers))
	for i, h := range headers {
		key := normHeader(h)
		if key == "" {
			continue
		}
		if _, exists := norm[key]; !exists {
			norm[key] = i
		}
	}

	result := make(map[string]int, len(aliases))
	for canonical, alts := range aliases {
		result[canonical] = -1
		for _, alt := range alts {
			altKey := normHeader(alt)
			if idx, ok := norm[altKey]; ok {
				result[canonical] = idx
				break
			}
		}
	}
	return result
}

// Has returns true if the canonical column was found.
func Has(m map[string]int, canonical string) bool {
	idx, ok := m[canonical]
	return ok && idx >= 0
}
