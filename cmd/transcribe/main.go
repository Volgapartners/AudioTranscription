// Transcription Backend CLI — fully automated 17-step transcription workflow.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"google.golang.org/api/drive/v3"

	"transcription-cli/internal/auth"
	"transcription-cli/internal/converter"
	"transcription-cli/internal/delivery"
	"transcription-cli/internal/gdrive"
	"transcription-cli/internal/intake"
	"transcription-cli/internal/logging"
	"transcription-cli/internal/manifest"
	"transcription-cli/internal/qa"
	"transcription-cli/internal/reconciler"
	"transcription-cli/internal/rework"
	"transcription-cli/internal/transcriber"
	"transcription-cli/internal/validator"
	"transcription-cli/internal/workspace"
	"transcription-cli/internal/xlsx"
)

const version = "1.0.0"
const totalSteps = 14

func main() {
	// Load .env file (silently skip if not present)
	_ = godotenv.Load()

	// Subcommand detection
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "scan":
			cmdScan()
			return
		case "resume":
			cmdResume()
			return
		case "rework":
			cmdRework()
			return
		case "version":
			fmt.Printf("transcribe v%s\n", version)
			return
		}
	}

	cmdRun()
}

func cmdRun() {
	fs := flag.NewFlagSet("transcribe", flag.ExitOnError)
	credentials := fs.String("credentials", envOr("TRANSCRIBE_CREDENTIALS", "./credentials.json"), "path to Google OAuth credentials JSON")
	workspaceDir := fs.String("workspace", envOr("TRANSCRIBE_WORKSPACE", "./workspace"), "base directory for workspaces")
	projectName := fs.String("project-name", envOr("TRANSCRIBE_PROJECT_NAME", ""), "project name (auto-generated if empty)")
	transcribeCmd := fs.String("transcribe-cmd", envOr("TRANSCRIBE_CMD", ""), "external transcription script command")
	sourceURL := fs.String("source", envOr("TRANSCRIBE_SOURCE", ""), "Google Drive source folder URL")
	volgaURL := fs.String("volga", envOr("TRANSCRIBE_VOLGA", ""), "Google Drive Volga templates folder URL")
	deliveryURL := fs.String("delivery", envOr("TRANSCRIBE_DELIVERY", ""), "Google Drive delivery folder URL")
	verbose := fs.Bool("verbose", envOrBool("TRANSCRIBE_VERBOSE", false), "enable DEBUG-level logging")
	jsonLog := fs.Bool("json-log", envOrBool("TRANSCRIBE_JSON_LOG", false), "force JSON log output (no colors)")
	workers := fs.Int("workers", envOrInt("TRANSCRIBE_WORKERS", 5), "concurrent download/upload workers")
	localSource := fs.String("local", envOr("TRANSCRIBE_LOCAL", ""), "local folder path (bypasses Google Drive)")

	// Deepgram options
	deepgramModel := fs.String("deepgram-model", envOr("DEEPGRAM_MODEL", "nova-3"), "Deepgram model name")
	deepgramWorkers := fs.Int("deepgram-workers", envOrInt("DEEPGRAM_WORKERS", 5), "concurrent Deepgram API calls")
	deepgramDiarize := fs.Bool("deepgram-diarize", envOrBool("DEEPGRAM_DIARIZE", true), "enable speaker diarization")
	deepgramPunctuate := fs.Bool("deepgram-punctuate", envOrBool("DEEPGRAM_PUNCTUATE", true), "enable punctuation")
	deepgramSmartFmt := fs.Bool("deepgram-smart-format", envOrBool("DEEPGRAM_SMART_FORMAT", true), "enable smart formatting")
	deepgramUtterances := fs.Bool("deepgram-utterances", envOrBool("DEEPGRAM_UTTERANCES", true), "split into utterances")
	deepgramDetectLang := fs.Bool("deepgram-detect-language", envOrBool("DEEPGRAM_DETECT_LANGUAGE", false), "auto-detect language instead of using manifest")
	deepgramUttSplit := fs.Float64("deepgram-utt-split", envOrFloat("DEEPGRAM_UTT_SPLIT", 0.8), "utterance split threshold in seconds")

	_ = fs.Parse(os.Args[1:])

	// Setup logging
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logging.SetupLogger(level, *jsonLog)

	logging.Banner(fmt.Sprintf("Transcription Backend v%s", version))

	// Determine transcription mode: --transcribe-cmd > DEEPGRAM_API_KEY > manual
	deepgramAPIKey := os.Getenv("DEEPGRAM_API_KEY")
	isDeepgramMode := deepgramAPIKey != "" && *transcribeCmd == ""
	if isDeepgramMode {
		logging.StepInfo("Deepgram mode detected (DEEPGRAM_API_KEY set)")
	}

	isLocalMode := *localSource != ""
	if isLocalMode {
		logging.StepInfo("Local mode detected (--local flag set)")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := gdrive.DownloadOpts{Workers: *workers}

	var (
		ws               *workspace.Workspace
		localScan        *intake.ScanResult
		srv              *drive.Service
		volgaFolderID    string
		deliveryFolderID string
		step             int
	)

	if isLocalMode {
		// --- Local mode: skip Drive steps, ingest from local folder ---

		absLocal, err := filepath.Abs(*localSource)
		if err != nil {
			fatal("Invalid local path: %v", err)
		}
		info, err := os.Stat(absLocal)
		if err != nil {
			fatal("Cannot access local path: %v", err)
		}
		if !info.IsDir() {
			fatal("Local path is not a directory: %s", absLocal)
		}

		// Steps 0-3: skipped in local mode
		for i := 0; i <= 3; i++ {
			logging.StepHeader(i, totalSteps, "Skipped (local mode)")
		}
		step = 3

		// --- Step 4: Scan local folder ---
		step++
		logging.StepHeader(step, totalSteps, "Scanning local folder...")
		sourceScan, err := intake.Scan(absLocal)
		if err != nil {
			fatal("Local scan failed: %v", err)
		}
		if !sourceScan.IsValid {
			logging.StepError("Structure validation FAILED")
			for _, issue := range sourceScan.Issues {
				logging.StepError(fmt.Sprintf("  %s: %s", issue.Path, issue.Message))
			}
			fatal("Fix the folder structure and re-run.")
		}
		for _, lang := range sourceScan.Languages {
			logging.StepDetail(fmt.Sprintf("lang=%s: %d files", lang, sourceScan.FileCounts[lang]))
		}
		logging.StepInfo(fmt.Sprintf("Total: %d files", sourceScan.TotalFiles))
		logging.StepSuccess("Structure valid")

		// --- Step 5: Create workspace ---
		step++
		if *projectName == "" {
			*projectName = fmt.Sprintf("project_%s", time.Now().Format("2006-01-02_150405"))
		}
		logging.StepHeader(step, totalSteps, fmt.Sprintf("Creating workspace: %s/%s/", *workspaceDir, *projectName))

		ws, err = workspace.Create(*workspaceDir, *projectName)
		if err != nil {
			fatal("Workspace creation failed: %v", err)
		}

		ws.State.LocalMode = true
		ws.State.LocalSource = absLocal
		ws.State.Counts.Intake = sourceScan.TotalFiles
		if err := ws.SaveState(); err != nil {
			fatal("Failed to save state: %v", err)
		}

		if err := intake.WriteIntakeReport(sourceScan, ws.IntakeReportPath()); err != nil {
			fatal("Failed to write intake report: %v", err)
		}
		logging.StepSuccess("Workspace created — state saved")

		// --- Step 6: Copy audio to workspace ---
		step++
		logging.StepHeader(step, totalSteps, "Copying audio to workspace...")
		copied, err := intake.CopyToWorkspace(sourceScan, ws.Audio)
		if err != nil {
			fatal("Copy failed: %v", err)
		}
		if copied != sourceScan.TotalFiles {
			fatal("Copy count mismatch: copied %d but expected %d", copied, sourceScan.TotalFiles)
		}

		ws.State.Counts.Downloaded = copied
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess(fmt.Sprintf("%d files copied", copied))

		// --- Step 7: Verify workspace copy ---
		step++
		logging.StepHeader(step, totalSteps, "Verifying workspace copy...")
		localScan, err = intake.Scan(ws.Audio)
		if err != nil {
			fatal("Workspace verification failed: %v", err)
		}
		if localScan.TotalFiles != sourceScan.TotalFiles {
			fatal("Workspace count %d != source count %d", localScan.TotalFiles, sourceScan.TotalFiles)
		}
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess(fmt.Sprintf("Workspace copy verified (%d files)", localScan.TotalFiles))

	} else {
		// --- Drive mode (existing flow) ---

		reader := bufio.NewReader(os.Stdin)

		sourceFolderID := promptOrFlag(reader, *sourceURL,
			"Enter Google Drive folder link (client audio):")
		sourceFolderIDParsed, err := gdrive.ParseFolderURL(sourceFolderID)
		if err != nil {
			fatal("Invalid source folder URL: %v", err)
		}

		// --- Authenticate ---
		logging.StepHeader(0, totalSteps, "Authenticating...")
		httpClient, err := auth.Authenticate(ctx, *credentials, ".token.json")
		if err != nil {
			fatal("Authentication failed: %v", err)
		}

		srv, err = gdrive.NewService(ctx, httpClient)
		if err != nil {
			fatal("Failed to create Drive service: %v", err)
		}
		logging.StepSuccess("Authenticated")

		// --- Validate source access ---
		step = 1
		logging.StepHeader(step, totalSteps, "Validating access to source folder...")
		if err := gdrive.ValidateAccess(ctx, srv, sourceFolderIDParsed, false); err != nil {
			fatal("Source folder access: %v", err)
		}
		logging.StepSuccess("Read access confirmed")

		// Prompt for Volga folder
		volgaFolderInput := promptOrFlag(reader, *volgaURL,
			"Enter Volga Drive folder for templates (or press Enter to skip upload):")
		if volgaFolderInput != "" {
			step++
			logging.StepHeader(step, totalSteps, "Validating access to Volga folder...")
			volgaFolderID, err = gdrive.ParseFolderURL(volgaFolderInput)
			if err != nil {
				fatal("Invalid Volga folder URL: %v", err)
			}
			if err := gdrive.ValidateAccess(ctx, srv, volgaFolderID, true); err != nil {
				fatal("Volga folder access: %v", err)
			}
			logging.StepSuccess("Read+write access confirmed")
		}

		// Prompt for delivery folder
		deliveryFolderInput := promptOrFlag(reader, *deliveryURL,
			"Enter client delivery Drive folder:")
		deliveryFolderID, err = gdrive.ParseFolderURL(deliveryFolderInput)
		if err != nil {
			fatal("Invalid delivery folder URL: %v", err)
		}

		step++
		logging.StepHeader(step, totalSteps, "Validating access to delivery folder...")
		if err := gdrive.ValidateAccess(ctx, srv, deliveryFolderID, true); err != nil {
			fatal("Delivery folder access: %v", err)
		}
		logging.StepSuccess("Read+write access confirmed")

		// --- Step: Scan Drive folder ---
		step++
		logging.StepHeader(step, totalSteps, "Analyzing source Drive folder...")
		scanResult, err := gdrive.ScanFolder(ctx, srv, sourceFolderIDParsed)
		if err != nil {
			fatal("Drive scan failed: %v", err)
		}

		if !scanResult.IsValid && !scanResult.NeedsZIPExtraction {
			logging.StepError("Structure validation FAILED")
			for _, issue := range scanResult.Issues {
				logging.StepError(fmt.Sprintf("  %s: %s", issue.Path, issue.Message))
			}
			fatal("Fix the Drive folder structure and re-run.")
		}

		for _, lang := range scanResult.Languages {
			logging.StepDetail(fmt.Sprintf("lang=%s: %d files", lang, scanResult.FileCounts[lang]))
		}
		logging.StepInfo(fmt.Sprintf("Total: %d files", scanResult.TotalFiles))
		logging.StepSuccess("Structure valid")

		// --- Step: Create workspace ---
		step++
		if *projectName == "" {
			*projectName = fmt.Sprintf("project_%s", time.Now().Format("2006-01-02_150405"))
		}
		logging.StepHeader(step, totalSteps, fmt.Sprintf("Creating workspace: %s/%s/", *workspaceDir, *projectName))

		ws, err = workspace.Create(*workspaceDir, *projectName)
		if err != nil {
			fatal("Workspace creation failed: %v", err)
		}

		// Save initial state
		ws.State.SourceFolderID = sourceFolderIDParsed
		ws.State.VolgaFolderID = volgaFolderID
		ws.State.DeliveryFolderID = deliveryFolderID
		ws.State.Counts.Intake = scanResult.TotalFiles
		if err := ws.SaveState(); err != nil {
			fatal("Failed to save state: %v", err)
		}

		// Write intake report
		if err := gdrive.WriteIntakeReport(scanResult, ws.IntakeReportPath()); err != nil {
			fatal("Failed to write intake report: %v", err)
		}
		logging.StepSuccess("Workspace created — state saved")

		// --- Step: Download audio ---
		step++
		logging.StepHeader(step, totalSteps, "Downloading audio...")
		dlReport, err := gdrive.Download(ctx, srv, scanResult, ws.Audio, opts)
		if err != nil {
			fatal("Download failed: %v", err)
		}

		if dlReport.Failed > 0 {
			fatal("Download had %d failures — cannot proceed", dlReport.Failed)
		}
		if dlReport.Downloaded != scanResult.TotalFiles {
			fatal("Count mismatch: downloaded %d but expected %d", dlReport.Downloaded, scanResult.TotalFiles)
		}

		ws.State.Counts.Downloaded = dlReport.Downloaded
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess(fmt.Sprintf("%d files downloaded — counts verified", dlReport.Downloaded))

		// --- Step: Verify local copy ---
		step++
		logging.StepHeader(step, totalSteps, "Verifying local copy...")
		localScan, err = intake.Scan(ws.Audio)
		if err != nil {
			fatal("Local verification failed: %v", err)
		}
		if localScan.TotalFiles != scanResult.TotalFiles {
			fatal("Local count %d != Drive count %d", localScan.TotalFiles, scanResult.TotalFiles)
		}
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess(fmt.Sprintf("Local scan matches Drive scan (%d files)", localScan.TotalFiles))
	}

	// --- Step: Generate manifest ---
	step++
	logging.StepHeader(step, totalSteps, "Generating manifest...")
	m, err := manifest.Generate(localScan, ws.Manifests)
	if err != nil {
		fatal("Manifest generation failed: %v", err)
	}

	ws.State.Counts.Manifest = m.TotalFiles
	if err := ws.CompleteStep(step); err != nil {
		slog.Warn("failed to save state", logging.FieldError, err)
	}
	logging.StepSuccess(fmt.Sprintf("manifest.json saved (%d entries, counts match intake)", m.TotalFiles))

	// --- Step: Generate XLSX templates ---
	step++
	if isDeepgramMode {
		logging.StepHeader(step, totalSteps, "Skipping template generation (Deepgram mode)")
		ws.State.Counts.Templates = m.TotalFiles
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
	} else {
		logging.StepHeader(step, totalSteps, "Generating XLSX templates...")
		templateCount, err := xlsx.GenerateAll(m, ws.Templates)
		if err != nil {
			fatal("Template generation failed: %v", err)
		}

		ws.State.Counts.Templates = templateCount
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess(fmt.Sprintf("%d templates generated", templateCount))
	}

	// --- Step: Upload templates to Volga Drive ---
	step++
	if isDeepgramMode || isLocalMode {
		reason := "Deepgram mode"
		if isLocalMode {
			reason = "local mode"
		}
		logging.StepHeader(step, totalSteps, fmt.Sprintf("Skipping template upload (%s)", reason))
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
	} else if volgaFolderID != "" {
		logging.StepHeader(step, totalSteps, "Uploading templates to Volga Drive...")
		subfolderName := fmt.Sprintf("%s_templates", *projectName)
		subfolderID, err := gdrive.CreateFolder(ctx, srv, volgaFolderID, subfolderName)
		if err != nil {
			fatal("Failed to create templates subfolder: %v", err)
		}
		ws.State.VolgaTemplatesSubfolder = subfolderID

		ulReport, err := gdrive.UploadFolder(ctx, srv, ws.Templates, subfolderID, opts)
		if err != nil {
			fatal("Template upload failed: %v", err)
		}
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess(fmt.Sprintf("%d templates uploaded", ulReport.Uploaded))
	} else {
		logging.StepHeader(step, totalSteps, "Skipping template upload (no Volga folder)")
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
	}

	// --- Step: Transcription ---
	step++
	logging.StepHeader(step, totalSteps, "Transcription...")
	var t transcriber.Transcriber
	if *transcribeCmd != "" {
		t = transcriber.NewScript(*transcribeCmd)
	} else if isDeepgramMode {
		t = transcriber.NewDeepgram(transcriber.DeepgramConfig{
			APIKey:         deepgramAPIKey,
			Model:          *deepgramModel,
			Workers:        *deepgramWorkers,
			Diarize:        *deepgramDiarize,
			Punctuate:      *deepgramPunctuate,
			SmartFormat:    *deepgramSmartFmt,
			Utterances:     *deepgramUtterances,
			DetectLanguage: *deepgramDetectLang,
			UttSplit:       *deepgramUttSplit,
		})
	} else {
		t = transcriber.NewManual()
	}

	transcribedCount, err := t.Transcribe(ctx, transcriber.Config{
		AudioDir:    ws.Audio,
		TemplateDir: ws.Templates,
		OutputDir:   ws.Transcribed,
		Manifest:    m,
	})
	if err != nil {
		fatal("Transcription failed: %v", err)
	}

	// Report Deepgram transcription failures (if any)
	var transcriptionFailures []rework.TranscriptionFailure
	if dt, ok := t.(*transcriber.DeepgramTranscriber); ok && len(dt.Failures()) > 0 {
		for _, f := range dt.Failures() {
			logging.StepError(fmt.Sprintf("  %s: %s", f.Filename, f.Error))
			transcriptionFailures = append(transcriptionFailures, rework.TranscriptionFailure{
				Filename: f.Filename,
				Language: f.Language,
				Error:    f.Error,
			})
		}
		logging.StepWarn(fmt.Sprintf("%d files failed Deepgram transcription", len(transcriptionFailures)))
	}

	// If manual mode + Volga Drive was used, download completed XLSX back
	if *transcribeCmd == "" && !isDeepgramMode && ws.State.VolgaTemplatesSubfolder != "" {
		logging.StepHeader(step, totalSteps, "Downloading completed XLSX from Volga Drive...")
		downloadedCount, dlErr := gdrive.DownloadCompleted(ctx, srv, ws.State.VolgaTemplatesSubfolder, ws.Transcribed, opts)
		if dlErr != nil {
			fatal("Failed to download completed XLSX: %v", dlErr)
		}
		transcribedCount = downloadedCount
		logging.StepSuccess(fmt.Sprintf("%d transcribed files collected", downloadedCount))
	}

	ws.State.Counts.Transcribed = transcribedCount
	if err := ws.CompleteStep(step); err != nil {
		slog.Warn("failed to save state", logging.FieldError, err)
	}
	logging.StepSuccess(fmt.Sprintf("%d transcribed files ready", transcribedCount))

	// --- Step: QA Review (MANUAL) ---
	step++
	logging.StepHeader(step, totalSteps, "QA Review — MANUAL STEP")

	if err := qa.GenerateChecklist(ws.Transcribed, ws.QAChecklistPath()); err != nil {
		fatal("Failed to generate QA checklist: %v", err)
	}

	if err := qa.InteractiveQA(ws.QAChecklistPath(), ws.Transcribed); err != nil {
		fatal("QA review failed: %v", err)
	}

	qaResult, err := qa.ProcessChecklist(ws.QAChecklistPath(), ws.Transcribed)
	if err != nil {
		fatal("QA checklist processing failed: %v", err)
	}

	ws.State.Counts.QAApproved = qaResult.ApprovedCount
	ws.State.Counts.QARework = qaResult.ReworkCount
	if err := ws.CompleteStep(step); err != nil {
		slog.Warn("failed to save state", logging.FieldError, err)
	}
	logging.StepSuccess(fmt.Sprintf("%d approved, %d rework", qaResult.ApprovedCount, qaResult.ReworkCount))

	// --- Step: Convert approved XLSX → JSON ---
	step++
	logging.StepHeader(step, totalSteps, "Converting approved XLSX → JSON...")
	jsonFiles, err := converter.ConvertFiles(qaResult.ApprovedFiles, ws.JSON, 0)
	if err != nil {
		fatal("Conversion failed: %v", err)
	}

	ws.State.Counts.JSONProduced = len(jsonFiles)
	if err := ws.CompleteStep(step); err != nil {
		slog.Warn("failed to save state", logging.FieldError, err)
	}
	logging.StepSuccess(fmt.Sprintf("%d files converted (%d skipped — in rework)",
		len(jsonFiles), qaResult.ReworkCount))

	// --- Step: Validate + Reconcile ---
	step++
	logging.StepHeader(step, totalSteps, "Validating + Reconciling...")

	valSummary, err := validator.ValidateAll(ws.JSON, "")
	if err != nil {
		fatal("Validation failed: %v", err)
	}

	if err := validator.WriteReport(valSummary, ws.ValidationReportPath()); err != nil {
		slog.Warn("failed to write validation report", logging.FieldError, err)
	}

	ws.State.Counts.JSONPassed = valSummary.Passed
	ws.State.Counts.JSONFailed = valSummary.Failed

	if valSummary.Passed > 0 {
		logging.StepSuccess(fmt.Sprintf("%d passed", valSummary.Passed))
	}
	if valSummary.Failed > 0 {
		logging.StepError(fmt.Sprintf("%d failed", valSummary.Failed))
		for _, e := range valSummary.Errors {
			logging.StepError(fmt.Sprintf("  %s: %s", e.File, e.Message))
		}
	}
	if len(valSummary.Warnings) > 0 {
		logging.StepWarn(fmt.Sprintf("%d warnings", len(valSummary.Warnings)))
	}

	// Cross-stage reconciliation
	counts := reconciler.StageCount{
		IntakeTotal:    ws.State.Counts.Intake,
		ManifestTotal:  ws.State.Counts.Manifest,
		TemplatesTotal: ws.State.Counts.Templates,
		QAApproved:     ws.State.Counts.QAApproved,
		QARework:       ws.State.Counts.QARework,
		JSONProduced:   ws.State.Counts.JSONProduced,
		JSONPassed:     ws.State.Counts.JSONPassed,
		JSONFailed:     ws.State.Counts.JSONFailed,
	}

	reconReport, err := reconciler.Reconcile(counts, ws.ManifestPath(), ws.JSON)
	if err != nil {
		fatal("Reconciliation failed: %v", err)
	}

	if err := reconciler.WriteReport(reconReport, ws.ReconciliationReportPath()); err != nil {
		slog.Warn("failed to write reconciliation report", logging.FieldError, err)
	}

	logging.StepDetail(fmt.Sprintf("Intake: %d → Manifest: %d → Templates: %d → QA: %d+%d → JSON: %d+%d",
		counts.IntakeTotal, counts.ManifestTotal, counts.TemplatesTotal,
		counts.QAApproved, counts.QARework,
		counts.JSONPassed, counts.JSONFailed))

	if err := ws.CompleteStep(step); err != nil {
		slog.Warn("failed to save state", logging.FieldError, err)
	}
	logging.StepSuccess("Reports saved to 06_logs/")

	// Route failures to rework
	totalRework := qaResult.ReworkCount + valSummary.Failed + len(transcriptionFailures)
	if totalRework > 0 {
		if routeErr := rework.RouteFailures(
			ws.Rework, ws.Transcribed, qaResult, valSummary, transcriptionFailures, ws.ReworkManifestPath(),
		); routeErr != nil {
			slog.Warn("failed to route rework files", logging.FieldError, routeErr)
		}
	}

	// --- Step: Deliver ---
	step++
	if isLocalMode {
		logging.StepHeader(step, totalSteps, "Packaging delivery (local mode)...")

		_, err = delivery.Package(
			*projectName, ws.JSON, totalRework,
			valSummary, reconReport,
			ws.DeliverySummaryPath(), ws.DeliverySummaryTextPath(),
		)
		if err != nil {
			fatal("Delivery packaging failed: %v", err)
		}

		ws.State.Counts.Delivered = valSummary.Passed
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess("Delivery summary saved (JSON + text)")
		logging.StepInfo(fmt.Sprintf("Output:     %s", ws.JSON))
	} else {
		logging.StepHeader(step, totalSteps, "Delivering to client Drive...")

		_, err = delivery.Package(
			*projectName, ws.JSON, totalRework,
			valSummary, reconReport,
			ws.DeliverySummaryPath(), ws.DeliverySummaryTextPath(),
		)
		if err != nil {
			fatal("Delivery packaging failed: %v", err)
		}

		ulReport, err := delivery.Upload(ctx, srv, ws.JSON, deliveryFolderID, *projectName, opts)
		if err != nil {
			fatal("Delivery upload failed: %v", err)
		}

		ws.State.Counts.Delivered = ulReport.Uploaded
		if err := ws.CompleteStep(step); err != nil {
			slog.Warn("failed to save state", logging.FieldError, err)
		}
		logging.StepSuccess("Delivery summary saved (JSON + text)")
	}

	// --- Final summary ---
	logging.Banner("SUMMARY")
	if isLocalMode {
		logging.StepSuccess(fmt.Sprintf("Output:     %d files → %s", valSummary.Passed, ws.JSON))
	} else {
		logging.StepSuccess(fmt.Sprintf("Delivered:  %d files → client Drive", ws.State.Counts.Delivered))
	}
	if totalRework > 0 {
		logging.StepWarn(fmt.Sprintf("Rework:     %d files (%d QA + %d validation)",
			totalRework, qaResult.ReworkCount, valSummary.Failed))
		logging.StepDetail(fmt.Sprintf("Details:    %s", ws.ReworkManifestPath()))
		logging.StepDetail(fmt.Sprintf("To fix:     transcribe rework %s", ws.Root))
	}
	logging.StepDetail(fmt.Sprintf("Logs:       %s", ws.Logs))
	fmt.Fprintln(os.Stderr)
}

// cmdScan runs folder analysis (Drive or local, no download, no workspace).
func cmdScan() {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	credentials := fs.String("credentials", envOr("TRANSCRIBE_CREDENTIALS", "./credentials.json"), "path to credentials")
	localFlag := fs.Bool("local", false, "treat argument as a local folder path")
	verbose := fs.Bool("verbose", envOrBool("TRANSCRIBE_VERBOSE", false), "enable DEBUG-level logging")
	jsonLog := fs.Bool("json-log", envOrBool("TRANSCRIBE_JSON_LOG", false), "force JSON log output")
	_ = fs.Parse(os.Args[2:])

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logging.SetupLogger(level, *jsonLog)

	if fs.NArg() < 1 {
		fatal("Usage: transcribe scan [--local] <path-or-drive-url>")
	}
	target := fs.Arg(0)

	// Auto-detect local path if --local not explicitly set
	if *localFlag || isLocalPath(target) {
		logging.StepHeader(1, 1, "Scanning local folder...")
		absPath, err := filepath.Abs(target)
		if err != nil {
			fatal("Invalid path: %v", err)
		}

		result, err := intake.Scan(absPath)
		if err != nil {
			fatal("Scan failed: %v", err)
		}

		fmt.Fprintf(os.Stderr, "\n")
		logging.StepInfo(fmt.Sprintf("Folder: %s", absPath))
		logging.StepInfo(fmt.Sprintf("Valid:  %v", result.IsValid))
		for _, lang := range result.Languages {
			logging.StepDetail(fmt.Sprintf("lang=%s: %d files", lang, result.FileCounts[lang]))
		}
		logging.StepInfo(fmt.Sprintf("Total:  %d files", result.TotalFiles))

		if len(result.Issues) > 0 {
			fmt.Fprintln(os.Stderr)
			logging.StepWarn(fmt.Sprintf("%d issues:", len(result.Issues)))
			for _, issue := range result.Issues {
				logging.StepError(fmt.Sprintf("  [%s] %s: %s", issue.Type, issue.Path, issue.Message))
			}
		}
		return
	}

	// Drive mode
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	folderID, err := gdrive.ParseFolderURL(target)
	if err != nil {
		fatal("Invalid URL: %v", err)
	}

	httpClient, err := auth.Authenticate(ctx, *credentials, ".token.json")
	if err != nil {
		fatal("Auth failed: %v", err)
	}

	srv, err := gdrive.NewService(ctx, httpClient)
	if err != nil {
		fatal("Drive service: %v", err)
	}

	logging.StepHeader(1, 1, "Scanning Drive folder...")
	result, err := gdrive.ScanFolder(ctx, srv, folderID)
	if err != nil {
		fatal("Scan failed: %v", err)
	}

	fmt.Fprintf(os.Stderr, "\n")
	logging.StepInfo(fmt.Sprintf("Folder: %s (%s)", result.FolderName, result.FolderID))
	logging.StepInfo(fmt.Sprintf("Valid:  %v", result.IsValid))
	for _, lang := range result.Languages {
		logging.StepDetail(fmt.Sprintf("lang=%s: %d files", lang, result.FileCounts[lang]))
	}
	logging.StepInfo(fmt.Sprintf("Total:  %d files", result.TotalFiles))

	if len(result.Issues) > 0 {
		fmt.Fprintln(os.Stderr)
		logging.StepWarn(fmt.Sprintf("%d issues:", len(result.Issues)))
		for _, issue := range result.Issues {
			logging.StepError(fmt.Sprintf("  [%s] %s: %s", issue.Type, issue.Path, issue.Message))
		}
	}
}

// cmdResume resumes a pipeline from the last completed step.
func cmdResume() {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	credentials := fs.String("credentials", envOr("TRANSCRIBE_CREDENTIALS", "./credentials.json"), "path to credentials")
	transcribeCmd := fs.String("transcribe-cmd", envOr("TRANSCRIBE_CMD", ""), "external transcription script")
	verbose := fs.Bool("verbose", envOrBool("TRANSCRIBE_VERBOSE", false), "enable DEBUG logging")
	jsonLog := fs.Bool("json-log", envOrBool("TRANSCRIBE_JSON_LOG", false), "force JSON output")
	workers := fs.Int("workers", envOrInt("TRANSCRIBE_WORKERS", 5), "concurrent workers")
	_ = fs.Parse(os.Args[2:])

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logging.SetupLogger(level, *jsonLog)

	if fs.NArg() < 1 {
		fatal("Usage: transcribe resume <workspace-path>")
	}

	ws, err := workspace.Load(fs.Arg(0))
	if err != nil {
		fatal("Failed to load workspace: %v", err)
	}

	logging.Banner(fmt.Sprintf("Resuming project: %s", ws.State.ProjectName))
	logging.StepInfo(fmt.Sprintf("Last completed step: %d", ws.State.CurrentStep-1))
	logging.StepInfo(fmt.Sprintf("Resuming from step: %d", ws.State.CurrentStep))
	if ws.State.LocalMode {
		logging.StepInfo("Mode: local")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if !ws.State.LocalMode {
		httpClient, err := auth.Authenticate(ctx, *credentials, ".token.json")
		if err != nil {
			fatal("Auth failed: %v", err)
		}

		srv, err := gdrive.NewService(ctx, httpClient)
		if err != nil {
			fatal("Drive service: %v", err)
		}

		_ = srv
	}

	_ = transcribeCmd
	_ = workers

	logging.StepInfo("Resume logic will re-run from the current step.")
	logging.StepInfo("For now, use the full 'transcribe' command with the same workspace.")
}

// cmdRework reprocesses fixed files in the rework directory.
func cmdRework() {
	fs := flag.NewFlagSet("rework", flag.ExitOnError)
	credentials := fs.String("credentials", envOr("TRANSCRIBE_CREDENTIALS", "./credentials.json"), "path to credentials")
	verbose := fs.Bool("verbose", envOrBool("TRANSCRIBE_VERBOSE", false), "enable DEBUG logging")
	jsonLog := fs.Bool("json-log", envOrBool("TRANSCRIBE_JSON_LOG", false), "force JSON output")
	workers := fs.Int("workers", envOrInt("TRANSCRIBE_WORKERS", 5), "concurrent workers")
	_ = fs.Parse(os.Args[2:])

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logging.SetupLogger(level, *jsonLog)

	if fs.NArg() < 1 {
		fatal("Usage: transcribe rework <workspace-path>")
	}

	ws, err := workspace.Load(fs.Arg(0))
	if err != nil {
		fatal("Failed to load workspace: %v", err)
	}

	logging.Banner(fmt.Sprintf("Rework: %s", ws.State.ProjectName))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Reprocess
	logging.StepHeader(1, 3, "Reprocessing rework files...")
	result, err := rework.ReprocessRework(ws.Rework, ws.JSON, "")
	if err != nil {
		fatal("Rework reprocessing failed: %v", err)
	}

	logging.StepSuccess(fmt.Sprintf("Reprocessed: %d, Passed: %d, Failed: %d",
		result.Reprocessed, result.Passed, result.Failed))

	// Re-validate all JSON
	logging.StepHeader(2, 3, "Re-validating all JSON...")
	valSummary, err := validator.ValidateAll(ws.JSON, "")
	if err != nil {
		fatal("Re-validation failed: %v", err)
	}
	logging.StepSuccess(fmt.Sprintf("Total: %d, Passed: %d, Failed: %d",
		valSummary.Total, valSummary.Passed, valSummary.Failed))

	// Upload fixed files to delivery if configured (skip in local mode)
	if ws.State.LocalMode {
		logging.StepHeader(3, 3, "Skipping upload (local mode)")
		logging.StepInfo(fmt.Sprintf("Rework output: %s", filepath.Join(ws.Rework, "json")))
	} else if ws.State.DeliveryFolderID != "" {
		logging.StepHeader(3, 3, "Uploading fixed files to delivery...")
		httpClient, err := auth.Authenticate(ctx, *credentials, ".token.json")
		if err != nil {
			fatal("Auth failed: %v", err)
		}

		srv, err := gdrive.NewService(ctx, httpClient)
		if err != nil {
			fatal("Drive service: %v", err)
		}

		opts := gdrive.DownloadOpts{Workers: *workers}
		reworkJSONDir := filepath.Join(ws.Rework, "json")

		_, err = delivery.Upload(ctx, srv, reworkJSONDir, ws.State.DeliveryFolderID, ws.State.ProjectName+"_rework", opts)
		if err != nil {
			fatal("Rework delivery upload failed: %v", err)
		}
		logging.StepSuccess("Rework files delivered to client Drive")
	}

	logging.Banner("REWORK COMPLETE")
}

// promptOrFlag returns the flag value if set, otherwise prompts the user.
func promptOrFlag(reader *bufio.Reader, flagVal, prompt string) string {
	if flagVal != "" {
		return flagVal
	}
	fmt.Fprintf(os.Stderr, "\n%s\n> ", prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// isLocalPath returns true if the string looks like a local file path rather than a URL.
func isLocalPath(s string) bool {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "~") {
		return true
	}
	info, err := os.Stat(s)
	return err == nil && info.IsDir()
}

func fatal(format string, args ...interface{}) {
	logging.StepError(fmt.Sprintf(format, args...))
	os.Exit(1)
}

// envOr returns the environment variable value or the fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envOrBool returns the environment variable as a bool or the fallback.
func envOrBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// envOrInt returns the environment variable as an int or the fallback.
func envOrInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// envOrFloat returns the environment variable as a float64 or the fallback.
func envOrFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

// Ensure interfaces are satisfied at compile time.
var (
	_ transcriber.Transcriber = (*transcriber.ManualTranscriber)(nil)
	_ transcriber.Transcriber = (*transcriber.ScriptTranscriber)(nil)
	_ transcriber.Transcriber = (*transcriber.DeepgramTranscriber)(nil)
	_ *drive.Service          // used to avoid "imported and not used"
)
