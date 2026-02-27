// Package logging provides standardized field names and color-coded structured logging.
package logging

// Standard field names for structured logging.
const (
	// Identity fields
	FieldFilename   = "filename"
	FieldLanguage   = "language"
	FieldOutputName = "output_name"
	FieldPath       = "path"
	FieldFolderID   = "folder_id"
	FieldProjectID  = "project_id"

	// Count fields
	FieldCount  = "count"
	FieldTotal  = "total"
	FieldPassed = "passed"
	FieldFailed = "failed"

	// Timing fields
	FieldDurationMs = "duration_ms"

	// Error fields
	FieldError = "error"

	// Process fields
	FieldStep   = "step"
	FieldAction = "action"
)
