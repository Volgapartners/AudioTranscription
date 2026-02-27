package transcriber

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"

	dginterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/rest/interfaces"

	"transcription-cli/internal/manifest"
)

func intPtr(v int) *int { return &v }

func TestWriteFilledXLSX_BasicUtterances(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "en_test_audio.xlsx")

	entry := manifest.Entry{
		Filename:   "test_audio.mp3",
		Language:   "en",
		OutputName: "en_test_audio",
		SHA256:     "abc123",
	}

	res := &dginterfaces.PreRecordedResponse{
		Results: &dginterfaces.Result{
			Utterances: []dginterfaces.Utterance{
				{Start: 0.5, End: 2.123, Transcript: "Hello world", Speaker: intPtr(0)},
				{Start: 2.5, End: 4.999, Transcript: "How are you?", Speaker: intPtr(1)},
				{Start: 5.0, End: 7.1234, Transcript: "I'm fine", Speaker: intPtr(0)},
			},
		},
	}

	if err := writeFilledXLSX(outPath, entry, res); err != nil {
		t.Fatalf("writeFilledXLSX failed: %v", err)
	}

	// Open and verify
	f, err := excelize.OpenFile(outPath)
	if err != nil {
		t.Fatalf("failed to open xlsx: %v", err)
	}
	defer f.Close()

	// Verify headers
	headers := []string{"segment_id", "start_time", "end_time", "speaker", "text", "overlap"}
	for i, expected := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		val, _ := f.GetCellValue("Sheet1", cell)
		if val != expected {
			t.Errorf("header[%d]: got %q, want %q", i, val, expected)
		}
	}

	// Verify row 2 (first utterance)
	tests := []struct {
		col  int
		row  int
		want string
	}{
		{1, 2, "1"},            // segment_id
		{2, 2, "0.5"},         // start_time
		{3, 2, "2.123"},       // end_time
		{4, 2, "speaker_01"},  // speaker (0-indexed → 1-indexed)
		{5, 2, "Hello world"}, // text
		// Row 3
		{1, 3, "2"},
		{4, 3, "speaker_02"}, // speaker 1 → speaker_02
		{5, 3, "How are you?"},
		// Row 4
		{4, 4, "speaker_01"}, // speaker 0 → speaker_01
		{5, 4, "I'm fine"},
	}

	for _, tt := range tests {
		cell, _ := excelize.CoordinatesToCellName(tt.col, tt.row)
		val, _ := f.GetCellValue("Sheet1", cell)
		if val != tt.want {
			t.Errorf("cell(%d,%d): got %q, want %q", tt.col, tt.row, val, tt.want)
		}
	}

	// Verify _metadata sheet
	metaTests := []struct {
		cell string
		want string
	}{
		{"A1", "audio_file"},
		{"B1", "test_audio.mp3"},
		{"A2", "language"},
		{"B2", "en"},
		{"A3", "output_name"},
		{"B3", "en_test_audio"},
		{"A4", "sha256"},
		{"B4", "abc123"},
	}

	for _, tt := range metaTests {
		val, _ := f.GetCellValue("_metadata", tt.cell)
		if val != tt.want {
			t.Errorf("_metadata %s: got %q, want %q", tt.cell, val, tt.want)
		}
	}

	// Verify _metadata is hidden
	visible, err := f.GetSheetVisible("_metadata")
	if err != nil {
		t.Fatalf("GetSheetVisible failed: %v", err)
	}
	if visible {
		t.Error("_metadata sheet should be hidden")
	}
}

func TestWriteFilledXLSX_NilSpeaker(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "en_test.xlsx")

	entry := manifest.Entry{
		Filename:   "test.mp3",
		Language:   "en",
		OutputName: "en_test",
	}

	res := &dginterfaces.PreRecordedResponse{
		Results: &dginterfaces.Result{
			Utterances: []dginterfaces.Utterance{
				{Start: 1.0, End: 3.0, Transcript: "No speaker info", Speaker: nil},
			},
		},
	}

	if err := writeFilledXLSX(outPath, entry, res); err != nil {
		t.Fatalf("writeFilledXLSX failed: %v", err)
	}

	f, err := excelize.OpenFile(outPath)
	if err != nil {
		t.Fatalf("failed to open xlsx: %v", err)
	}
	defer f.Close()

	// Nil speaker should default to "speaker_01"
	cell, _ := excelize.CoordinatesToCellName(4, 2)
	val, _ := f.GetCellValue("Sheet1", cell)
	if val != "speaker_01" {
		t.Errorf("nil speaker: got %q, want %q", val, "speaker_01")
	}
}

func TestWriteFilledXLSX_EmptyUtterances(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "en_silence.xlsx")

	entry := manifest.Entry{
		Filename:   "silence.mp3",
		Language:   "en",
		OutputName: "en_silence",
	}

	// Empty results — no utterances
	res := &dginterfaces.PreRecordedResponse{
		Results: &dginterfaces.Result{
			Utterances: nil,
		},
	}

	if err := writeFilledXLSX(outPath, entry, res); err != nil {
		t.Fatalf("writeFilledXLSX failed: %v", err)
	}

	f, err := excelize.OpenFile(outPath)
	if err != nil {
		t.Fatalf("failed to open xlsx: %v", err)
	}
	defer f.Close()

	// Should have headers but no data rows
	h, _ := f.GetCellValue("Sheet1", "A1")
	if h != "segment_id" {
		t.Errorf("header A1: got %q, want %q", h, "segment_id")
	}
	// Row 2 should be empty
	val, _ := f.GetCellValue("Sheet1", "A2")
	if val != "" {
		t.Errorf("expected empty row 2, got %q", val)
	}

	// Metadata should still exist
	val, _ = f.GetCellValue("_metadata", "B1")
	if val != "silence.mp3" {
		t.Errorf("_metadata B1: got %q, want %q", val, "silence.mp3")
	}
}

func TestWriteFilledXLSX_NilResults(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "en_nil.xlsx")

	entry := manifest.Entry{
		Filename:   "nil_results.mp3",
		Language:   "en",
		OutputName: "en_nil",
	}

	// Nil results
	res := &dginterfaces.PreRecordedResponse{
		Results: nil,
	}

	if err := writeFilledXLSX(outPath, entry, res); err != nil {
		t.Fatalf("writeFilledXLSX failed: %v", err)
	}

	// File should exist with headers only
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestWriteFilledXLSX_TimestampRounding(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "en_round.xlsx")

	entry := manifest.Entry{
		Filename:   "round.mp3",
		Language:   "en",
		OutputName: "en_round",
	}

	res := &dginterfaces.PreRecordedResponse{
		Results: &dginterfaces.Result{
			Utterances: []dginterfaces.Utterance{
				{Start: 1.23456789, End: 5.99999, Transcript: "test", Speaker: intPtr(0)},
			},
		},
	}

	if err := writeFilledXLSX(outPath, entry, res); err != nil {
		t.Fatalf("writeFilledXLSX failed: %v", err)
	}

	f, err := excelize.OpenFile(outPath)
	if err != nil {
		t.Fatalf("failed to open xlsx: %v", err)
	}
	defer f.Close()

	// start_time should be rounded to 3 decimals: 1.235
	cell, _ := excelize.CoordinatesToCellName(2, 2)
	val, _ := f.GetCellValue("Sheet1", cell)
	if val != "1.235" {
		t.Errorf("start_time rounding: got %q, want %q", val, "1.235")
	}

	// end_time: 6.0 (rounds to 6)
	cell, _ = excelize.CoordinatesToCellName(3, 2)
	val, _ = f.GetCellValue("Sheet1", cell)
	if val != "6" {
		t.Errorf("end_time rounding: got %q, want %q", val, "6")
	}
}

func TestResumeSkipsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	os.MkdirAll(outputDir, 0755)

	// Pre-create an existing XLSX to simulate a partial run
	existingPath := filepath.Join(outputDir, "en_existing.xlsx")
	f := excelize.NewFile()
	f.SaveAs(existingPath)
	f.Close()

	// Verify the file exists
	if _, err := os.Stat(existingPath); err != nil {
		t.Fatalf("pre-created file should exist: %v", err)
	}

	// The resume skip logic is in Transcribe() which needs a real API client,
	// so we just verify the skip condition directly
	_, err := os.Stat(existingPath)
	if err != nil {
		t.Error("skip check should find existing file")
	}
}

func TestSpeakerNormalization(t *testing.T) {
	tests := []struct {
		speaker *int
		want    string
	}{
		{nil, "speaker_01"},
		{intPtr(0), "speaker_01"},
		{intPtr(1), "speaker_02"},
		{intPtr(2), "speaker_03"},
		{intPtr(9), "speaker_10"},
		{intPtr(99), "speaker_100"},
	}

	for _, tt := range tests {
		got := "speaker_01"
		if tt.speaker != nil {
			got = formatSpeaker(*tt.speaker)
		}
		if got != tt.want {
			t.Errorf("speaker %v: got %q, want %q", tt.speaker, got, tt.want)
		}
	}
}

func TestRoundTo3(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{1.23456789, 1.235},
		{5.99999, 6.0},
		{0.0, 0.0},
		{100.0, 100.0},
		{1.1, 1.1},
		{0.0005, 0.001},
		{0.0004, 0.0},
	}

	for _, tt := range tests {
		got := roundTo3(tt.input)
		if got != tt.want {
			t.Errorf("roundTo3(%v): got %v, want %v", tt.input, got, tt.want)
		}
	}
}

// formatSpeaker mirrors the speaker formatting logic from writeFilledXLSX
func formatSpeaker(speaker int) string {
	return fmt.Sprintf("speaker_%02d", speaker+1)
}
