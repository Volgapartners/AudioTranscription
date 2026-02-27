// Package validator provides JSON schema and business rule validation for transcription files.
package validator

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"transcription-cli/internal/converter"
	"transcription-cli/internal/logging"
)

// Summary contains the results of validating multiple files.
type Summary struct {
	ValidatedAt time.Time           `json:"validated_at"`
	Total       int                 `json:"total"`
	Passed      int                 `json:"passed"`
	Failed      int                 `json:"failed"`
	PerLanguage map[string]LangStat `json:"per_language"`
	Errors      []FileError         `json:"errors,omitempty"`
	Warnings    []FileWarning       `json:"warnings,omitempty"`
}

// LangStat tracks per-language validation counts.
type LangStat struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// FileError represents a validation error for a specific file.
type FileError struct {
	File    string `json:"file"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// FileWarning represents a non-fatal validation issue.
type FileWarning struct {
	File    string `json:"file"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// ValidateAll validates all JSON files in the given directory against the schema.
func ValidateAll(jsonDir, schemaPath string) (*Summary, error) {
	start := time.Now()
	slog.Info("validation started",
		logging.FieldPath, jsonDir,
		"schema", schemaPath)

	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("parsing embedded schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("transcription.schema.json", schema); err != nil {
		return nil, fmt.Errorf("adding schema resource: %w", err)
	}
	compiled, err := compiler.Compile("transcription.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compiling schema: %w", err)
	}

	entries, err := os.ReadDir(jsonDir)
	if err != nil {
		return nil, fmt.Errorf("reading json directory %s: %w", jsonDir, err)
	}

	summary := &Summary{
		ValidatedAt: time.Now().UTC(),
		PerLanguage: make(map[string]LangStat),
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		jsonPath := filepath.Join(jsonDir, entry.Name())
		summary.Total++

		errs, warns, lang := Validate(jsonPath, compiled)
		if lang != "" {
			stat := summary.PerLanguage[lang]
			stat.Total++
			if len(errs) > 0 {
				stat.Failed++
			} else {
				stat.Passed++
			}
			summary.PerLanguage[lang] = stat
		}

		if len(errs) > 0 {
			summary.Failed++
			for _, e := range errs {
				summary.Errors = append(summary.Errors, FileError{
					File:    entry.Name(),
					Message: e,
				})
			}
		} else {
			summary.Passed++
		}
		for _, w := range warns {
			summary.Warnings = append(summary.Warnings, FileWarning{
				File:    entry.Name(),
				Message: w,
			})
		}
	}

	slog.Info("validation completed",
		logging.FieldTotal, summary.Total,
		logging.FieldPassed, summary.Passed,
		logging.FieldFailed, summary.Failed,
		"warnings", len(summary.Warnings),
		logging.FieldDurationMs, time.Since(start).Milliseconds())

	return summary, nil
}

// Validate checks a single JSON file against the schema and business rules.
// Returns errors, warnings, and the detected language.
func Validate(jsonPath string, schema *jsonschema.Schema) (errors []string, warnings []string, lang string) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return []string{fmt.Sprintf("reading file: %v", err)}, nil, ""
	}

	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return []string{fmt.Sprintf("parsing json: %v", err)}, nil, ""
	}

	if err := schema.Validate(doc); err != nil {
		return []string{fmt.Sprintf("schema validation: %v", err)}, nil, ""
	}

	var trans converter.Transcription
	if err := json.Unmarshal(data, &trans); err != nil {
		return []string{fmt.Sprintf("unmarshaling transcription: %v", err)}, nil, ""
	}

	// Extract language from the first utterance's language_attributes
	if len(trans.Utterances) > 0 {
		lang = trans.Utterances[0].LanguageAttributes.Language
	}

	// Business rules: validate utterance timing
	for i, utt := range trans.Utterances {
		if utt.StartTime != nil && utt.EndTime != nil {
			if *utt.StartTime >= *utt.EndTime {
				errors = append(errors, fmt.Sprintf(
					"utterance %d: start_time (%.2f) >= end_time (%.2f)",
					i+1, *utt.StartTime, *utt.EndTime))
			}
		}

		if i > 0 {
			prev := trans.Utterances[i-1]
			if utt.StartTime != nil && prev.EndTime != nil {
				if *utt.StartTime < *prev.EndTime {
					warnings = append(warnings, fmt.Sprintf(
						"utterance %d: starts at %.2f before previous utterance ends at %.2f",
						i+1, *utt.StartTime, *prev.EndTime))
				}
			}
		}
	}

	return errors, warnings, lang
}

// WriteReport writes the validation summary to a JSON file.
func WriteReport(summary *Summary, outputPath string) error {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling validation report: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("writing validation report: %w", err)
	}
	slog.Info("validation report saved", logging.FieldPath, outputPath)
	return nil
}

// FailedFiles returns the list of filenames that failed validation.
func FailedFiles(summary *Summary) []string {
	seen := make(map[string]bool)
	var files []string
	for _, e := range summary.Errors {
		if !seen[e.File] {
			seen[e.File] = true
			files = append(files, e.File)
		}
	}
	return files
}

// Embedded schema for pre-compilation — v2 format matching Python scripts.
const schemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://volgapartners.com/schemas/transcription/v2",
  "title": "Transcription Output",
  "description": "Schema for validated transcription JSON files",
  "type": "object",
  "required": ["audio_name", "utterances"],
  "additionalProperties": false,
  "properties": {
    "audio_name": {
      "type": "string",
      "minLength": 1
    },
    "utterances": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "required": ["speaker", "start_time", "end_time", "transcript", "non_speech_events", "language_attributes"],
        "additionalProperties": false,
        "properties": {
          "speaker": {
            "type": "string",
            "pattern": "^speaker_\\d+$"
          },
          "start_time": {
            "type": ["number", "null"]
          },
          "end_time": {
            "type": ["number", "null"]
          },
          "transcript": {
            "type": "string"
          },
          "non_speech_events": {
            "type": "object",
            "required": ["emotions"],
            "additionalProperties": false,
            "properties": {
              "emotions": {
                "type": "string"
              }
            }
          },
          "language_attributes": {
            "type": "object",
            "required": ["language", "locale", "accent"],
            "additionalProperties": false,
            "properties": {
              "language": {
                "type": "string",
                "minLength": 1
              },
              "locale": {
                "type": "string"
              },
              "accent": {
                "type": "string"
              }
            }
          }
        }
      }
    }
  }
}`
