package transcriber

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"

	api "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/rest"
	dginterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/rest/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/listen"

	"transcription-cli/internal/logging"
	"transcription-cli/internal/manifest"
)

const (
	deepgramMaxRetries  = 3
	deepgramBaseBackoff = 1 * time.Second
	deepgramMaxBackoff  = 10 * time.Second
	deepgramJitter      = 0.3
)

// DeepgramConfig holds all configurable options for the Deepgram transcriber.
type DeepgramConfig struct {
	APIKey         string
	Model          string
	Workers        int
	Diarize        bool
	Punctuate      bool
	SmartFormat    bool
	Utterances     bool
	DetectLanguage bool
	UttSplit       float64
}

// TranscriptionFailure records a file that failed Deepgram transcription.
type TranscriptionFailure struct {
	Filename string `json:"filename"`
	Language string `json:"language"`
	Error    string `json:"error"`
}

// DeepgramTranscriber uses the Deepgram pre-recorded API to transcribe audio files.
type DeepgramTranscriber struct {
	cfg      DeepgramConfig
	mu       sync.Mutex
	failures []TranscriptionFailure
}

// NewDeepgram creates a DeepgramTranscriber with the given configuration.
func NewDeepgram(cfg DeepgramConfig) *DeepgramTranscriber {
	if cfg.Model == "" {
		cfg.Model = "nova-3"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 5
	}
	return &DeepgramTranscriber{cfg: cfg}
}

// Failures returns all transcription failures accumulated during Transcribe.
func (d *DeepgramTranscriber) Failures() []TranscriptionFailure {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]TranscriptionFailure, len(d.failures))
	copy(out, d.failures)
	return out
}

// Transcribe processes all manifest entries through the Deepgram API concurrently,
// writing filled XLSX files to cfg.OutputDir. Returns the count of successfully produced files.
func (d *DeepgramTranscriber) Transcribe(ctx context.Context, cfg Config) (int, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return 0, fmt.Errorf("creating output directory: %w", err)
	}

	// Initialize Deepgram SDK (suppress its verbose logging).
	// Temporarily clear os.Args to prevent the SDK's klog dependency from
	// calling flag.Parse() on os.Args, which fails on CLI flags (e.g. --local)
	// that aren't registered on the default flag.CommandLine.
	savedArgs := os.Args
	os.Args = os.Args[:1]
	client.Init(client.InitLib{
		LogLevel: client.LogLevelDefault,
	})
	os.Args = savedArgs

	c := client.NewREST(d.cfg.APIKey, &interfaces.ClientOptions{})
	dg := api.New(c)

	slog.Info("starting Deepgram transcription",
		"model", d.cfg.Model,
		logging.FieldTotal, len(cfg.Manifest.Entries),
		"workers", d.cfg.Workers)

	sem := make(chan struct{}, d.cfg.Workers)
	var wg sync.WaitGroup
	var successCount int
	var mu sync.Mutex

	for _, entry := range cfg.Manifest.Entries {
		// Check for context cancellation
		if ctx.Err() != nil {
			break
		}

		outputPath := filepath.Join(cfg.OutputDir, entry.OutputName+".xlsx")

		// Resume support: skip if already transcribed
		if _, err := os.Stat(outputPath); err == nil {
			slog.Debug("skipping already transcribed file",
				logging.FieldFilename, entry.Filename)
			mu.Lock()
			successCount++
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(e manifest.Entry, outPath string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			audioPath := filepath.Join(cfg.AudioDir, e.RelPath)

			res, err := d.transcribeWithRetry(ctx, dg, audioPath, e.Language)
			if err != nil {
				slog.Error("Deepgram transcription failed",
					logging.FieldFilename, e.Filename,
					logging.FieldError, err)
				d.mu.Lock()
				d.failures = append(d.failures, TranscriptionFailure{
					Filename: e.Filename,
					Language: e.Language,
					Error:    err.Error(),
				})
				d.mu.Unlock()
				return
			}

			if err := writeFilledXLSX(outPath, e, res); err != nil {
				slog.Error("failed to write transcription XLSX",
					logging.FieldFilename, e.Filename,
					logging.FieldError, err)
				d.mu.Lock()
				d.failures = append(d.failures, TranscriptionFailure{
					Filename: e.Filename,
					Language: e.Language,
					Error:    fmt.Sprintf("xlsx write: %v", err),
				})
				d.mu.Unlock()
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()

			slog.Info("transcribed",
				logging.FieldFilename, e.Filename,
				logging.FieldLanguage, e.Language)
		}(entry, outputPath)
	}

	wg.Wait()

	if successCount == 0 && len(cfg.Manifest.Entries) > 0 {
		return 0, fmt.Errorf("all %d files failed Deepgram transcription", len(cfg.Manifest.Entries))
	}

	logging.StepSuccess(fmt.Sprintf("Deepgram transcribed %d/%d files", successCount, len(cfg.Manifest.Entries)))
	return successCount, nil
}

// transcribeWithRetry calls the Deepgram API with exponential backoff retry.
func (d *DeepgramTranscriber) transcribeWithRetry(
	ctx context.Context,
	dg *api.Client,
	audioPath string,
	language string,
) (*dginterfaces.PreRecordedResponse, error) {
	opts := d.buildOptions(language)

	var lastErr error
	for attempt := 0; attempt < deepgramMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(float64(deepgramBaseBackoff) * math.Pow(2, float64(attempt-1)))
			if backoff > deepgramMaxBackoff {
				backoff = deepgramMaxBackoff
			}
			jitter := time.Duration(float64(backoff) * deepgramJitter * rand.Float64())
			wait := backoff + jitter

			slog.Warn("Deepgram API call failed, retrying",
				"attempt", attempt+1,
				"file", filepath.Base(audioPath),
				"backoff_ms", wait.Milliseconds(),
				logging.FieldError, lastErr)

			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		res, err := dg.FromFile(ctx, audioPath, opts)
		if err == nil {
			return res, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", deepgramMaxRetries, lastErr)
}

// buildOptions constructs Deepgram transcription options from the config and per-file language.
func (d *DeepgramTranscriber) buildOptions(language string) *interfaces.PreRecordedTranscriptionOptions {
	opts := &interfaces.PreRecordedTranscriptionOptions{
		Model:       d.cfg.Model,
		Diarize:     d.cfg.Diarize,
		Punctuate:   d.cfg.Punctuate,
		SmartFormat: d.cfg.SmartFormat,
		Utterances:  d.cfg.Utterances,
	}

	if d.cfg.UttSplit > 0 {
		opts.UttSplit = d.cfg.UttSplit
	}

	if d.cfg.DetectLanguage {
		opts.DetectLanguage = true
	} else if language != "" {
		opts.Language = language
	}

	return opts
}

// writeFilledXLSX creates an XLSX file with Deepgram transcription results,
// matching the template format expected by the converter (template mode).
func writeFilledXLSX(outputPath string, entry manifest.Entry, res *dginterfaces.PreRecordedResponse) error {
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			slog.Warn("failed to close xlsx file",
				logging.FieldFilename, entry.OutputName,
				logging.FieldError, err)
		}
	}()

	sheet := "Sheet1"

	// Write headers matching xlsx.Columns
	headers := []string{"segment_id", "start_time", "end_time", "speaker", "text", "overlap"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return fmt.Errorf("setting header %s: %w", h, err)
		}
	}

	// Write utterance data rows
	if res.Results != nil {
		for i, utt := range res.Results.Utterances {
			row := i + 2

			// segment_id
			cell, _ := excelize.CoordinatesToCellName(1, row)
			if err := f.SetCellValue(sheet, cell, fmt.Sprintf("%d", i+1)); err != nil {
				return fmt.Errorf("setting segment_id row %d: %w", row, err)
			}

			// start_time
			cell, _ = excelize.CoordinatesToCellName(2, row)
			if err := f.SetCellValue(sheet, cell, roundTo3(utt.Start)); err != nil {
				return fmt.Errorf("setting start_time row %d: %w", row, err)
			}

			// end_time
			cell, _ = excelize.CoordinatesToCellName(3, row)
			if err := f.SetCellValue(sheet, cell, roundTo3(utt.End)); err != nil {
				return fmt.Errorf("setting end_time row %d: %w", row, err)
			}

			// speaker (Deepgram is 0-indexed, pipeline uses 1-indexed)
			speaker := "speaker_01"
			if utt.Speaker != nil {
				speaker = fmt.Sprintf("speaker_%02d", *utt.Speaker+1)
			}
			cell, _ = excelize.CoordinatesToCellName(4, row)
			if err := f.SetCellValue(sheet, cell, speaker); err != nil {
				return fmt.Errorf("setting speaker row %d: %w", row, err)
			}

			// text
			cell, _ = excelize.CoordinatesToCellName(5, row)
			if err := f.SetCellValue(sheet, cell, utt.Transcript); err != nil {
				return fmt.Errorf("setting text row %d: %w", row, err)
			}

			// overlap — leave empty
		}
	}

	// Add hidden _metadata sheet
	if err := addMetadata(f, entry); err != nil {
		return fmt.Errorf("adding metadata sheet: %w", err)
	}

	if err := f.SaveAs(outputPath); err != nil {
		return fmt.Errorf("saving %s: %w", outputPath, err)
	}

	return nil
}

// addMetadata creates a hidden _metadata sheet matching the template format.
func addMetadata(f *excelize.File, entry manifest.Entry) error {
	sheetName := "_metadata"
	if _, err := f.NewSheet(sheetName); err != nil {
		return fmt.Errorf("creating metadata sheet: %w", err)
	}

	metadata := [][]string{
		{"audio_file", entry.Filename},
		{"language", entry.Language},
		{"output_name", entry.OutputName},
		{"sha256", entry.SHA256},
	}

	for row, pair := range metadata {
		keyCell, _ := excelize.CoordinatesToCellName(1, row+1)
		valCell, _ := excelize.CoordinatesToCellName(2, row+1)
		if err := f.SetCellValue(sheetName, keyCell, pair[0]); err != nil {
			return fmt.Errorf("setting metadata key: %w", err)
		}
		if err := f.SetCellValue(sheetName, valCell, pair[1]); err != nil {
			return fmt.Errorf("setting metadata value: %w", err)
		}
	}

	if err := f.SetSheetVisible(sheetName, false); err != nil {
		return fmt.Errorf("hiding metadata sheet: %w", err)
	}

	return nil
}

// countXLSX counts .xlsx files in a directory.
func countXLSX(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".xlsx") {
			count++
		}
	}
	return count
}

// roundTo3 rounds a float64 to 3 decimal places.
func roundTo3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
