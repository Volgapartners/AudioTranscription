# Transcription CLI

A Go CLI tool that automates the end-to-end transcription workflow — from ingesting client audio (Google Drive or local folder) through QA review to delivering validated JSON.

The only manual step is **QA review** (Step 11). Everything else — downloading/copying, template generation, uploading, conversion, validation, reconciliation, delivery, and rework — runs automatically.

---

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Google Cloud Setup](#google-cloud-setup)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
  - [Full Pipeline](#full-pipeline)
  - [Scan Only](#scan-only)
  - [Resume](#resume)
  - [Rework](#rework)
- [Pipeline Steps](#pipeline-steps)
- [Workspace Structure](#workspace-structure)
- [QA Review Guide](#qa-review-guide)
- [Pluggable Transcription](#pluggable-transcription)
- [Architecture](#architecture)
- [Development](#development)

---

## Features

- **Google OAuth2 with PKCE** — browser-based login, token caching, automatic refresh
- **Google Drive integration** — scan, download, upload with concurrent workers and rate limit handling
- **Local folder input** — `--local` flag bypasses all Drive operations for offline/GCP-free workflows
- **14-step automated pipeline** with a single manual QA pause
- **Crash recovery** — state persisted after every step; resume from where you left off
- **Cross-stage reconciliation** — 5-way count comparison (intake, manifest, templates, QA, JSON)
- **Pluggable transcription** — use manual mode or plug in your own script via `--transcribe-cmd`
- **Non-destructive rework loop** — failed files routed to rework, fixed files merged back without overwriting
- **Color-coded structured logging** — cyan/green/yellow/red terminal output; JSON mode for CI
- **ZIP support** — auto-extracts ZIP archives from Drive
- **Hard stops** — invalid folder structure, count mismatches, and incomplete QA all halt the pipeline

---

## Prerequisites

### To build from source

- **Go 1.23+** — [Install Go](https://go.dev/dl/)
- **Make** _(optional)_ — for using `Makefile` targets; you can also run `go build` directly

### To run the binary (Drive mode)

- **`credentials.json`** — Google OAuth credentials file (see [Google Cloud Setup](#google-cloud-setup))
- **Web browser** — required on first run for Google OAuth login (token is cached after that)
- **Internet access** — for Google Drive API calls (download, upload, scanning)
- **Google Cloud project** with the Google Drive API enabled (see [Google Cloud Setup](#google-cloud-setup))
- **Google account** with access to all three Drive folders used in the workflow (client source, Volga templates, client delivery)

### To run the binary (local mode)

- **No GCP, OAuth, or internet required** — just a local folder of audio files
- The folder must contain `lang=xx/` subfolders (e.g., `lang=en/`, `lang=fr/`) with audio files

---

## Google Cloud Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project (e.g., "Volga Transcription")
3. **Enable the Google Drive API**:
   - Navigate to **APIs & Services > Library**
   - Search for "Google Drive API" and click **Enable**
4. **Configure OAuth consent screen**:
   - Go to **APIs & Services > OAuth consent screen**
   - User type: **External** (or **Internal** if using Google Workspace)
   - App name: "Volga Transcription Tool"
   - Add scope: `https://www.googleapis.com/auth/drive`
   - Add your Google account as a **test user**
5. **Create OAuth credentials**:
   - Go to **APIs & Services > Credentials**
   - Click **Create Credentials > OAuth client ID**
   - Application type: **Desktop app**
   - Download the JSON file
   - Save it as `credentials.json` in the project root (this file is gitignored)

> **Note**: The authenticated user must have access to all three Drive folders used in the workflow (source, Volga, delivery). The tool validates access to each folder before starting any work.

---

## Installation

```bash
# Clone the repository
git clone https://github.com/VolgaPartners/transcription-backend.git
cd transcription-backend

# Install dependencies
go mod tidy

# Build the binary
make build
# Binary created at: ./bin/transcribe
```

---

## Configuration

Configuration is loaded from three sources (highest priority first):

1. **CLI flags** — e.g., `--source=URL`
2. **Environment variables** — loaded from `.env` file or shell environment
3. **Hardcoded defaults**

### Setting up `.env`

```bash
cp .env.example .env
# Edit .env with your values
```

### Environment Variables

| Variable                  | CLI Flag           | Default              | Description                               |
| ------------------------- | ------------------ | -------------------- | ----------------------------------------- |
| `TRANSCRIBE_CREDENTIALS`  | `--credentials`    | `./credentials.json` | Path to GCP OAuth credentials             |
| `TRANSCRIBE_WORKSPACE`    | `--workspace`      | `./workspace`        | Base directory for project workspaces     |
| `TRANSCRIBE_PROJECT_NAME` | `--project-name`   | Auto-generated       | Project name                              |
| `TRANSCRIBE_SOURCE`       | `--source`         | _(prompted)_         | Client audio Drive folder URL             |
| `TRANSCRIBE_VOLGA`        | `--volga`          | _(prompted)_         | Volga Drive folder for templates          |
| `TRANSCRIBE_DELIVERY`     | `--delivery`       | _(prompted)_         | Client delivery Drive folder URL          |
| `TRANSCRIBE_LOCAL`        | `--local`          | _(empty)_            | Local folder path (bypasses Google Drive) |
| `TRANSCRIBE_CMD`          | `--transcribe-cmd` | _(manual mode)_      | External transcription script             |
| `TRANSCRIBE_WORKERS`      | `--workers`        | `5`                  | Concurrent download/upload workers        |
| `TRANSCRIBE_VERBOSE`      | `--verbose`        | `false`              | Enable DEBUG-level logging                |
| `TRANSCRIBE_JSON_LOG`     | `--json-log`       | `false`              | Force JSON log output (no colors)         |

Any value not provided via flag or `.env` will use the default. Drive folder URLs that are not set will be prompted interactively at runtime.

---

## Usage

### Full Pipeline

Run the complete 14-step workflow:

```bash
# Interactive mode — prompts for Drive URLs
./bin/transcribe

# With flags — no prompts
./bin/transcribe \
  --source="https://drive.google.com/drive/folders/SOURCE_ID" \
  --volga="https://drive.google.com/drive/folders/VOLGA_ID" \
  --delivery="https://drive.google.com/drive/folders/DELIVERY_ID"

# With .env configured — just run
./bin/transcribe

# With an external transcription script
./bin/transcribe --transcribe-cmd="./scripts/transcribe.sh"

# Verbose logging
./bin/transcribe --verbose
```

On first run, a browser window opens for Google OAuth login. The token is cached in `.token.json` and automatically refreshed on subsequent runs.

### Local Mode (no Google Drive)

Use `--local` to ingest audio from a local folder, bypassing all Drive operations (OAuth, scan, download, upload):

```bash
# Run full pipeline from a local folder
./bin/transcribe --local ./audio-files

# With Deepgram AI transcription
DEEPGRAM_API_KEY=xxx ./bin/transcribe --local ./audio-files

# With an external transcription script
./bin/transcribe --local ./audio-files --transcribe-cmd="./scripts/transcribe.sh"
```

The local folder must have the same `lang=xx/` structure expected by the pipeline:

```
audio-files/
├── lang=en/
│   ├── interview_001.mp3
│   └── interview_002.mp3
└── lang=fr/
    └── recording_001.wav
```

In local mode, steps 0-3 (OAuth + Drive validation) are skipped, files are copied into the workspace instead of downloaded, template upload is skipped, and final JSON output remains local (no Drive upload). Resume and rework also work without Drive access for local-mode workspaces.

### Scan Only

Analyze a folder's structure without creating a workspace or downloading anything:

```bash
# Scan a Drive folder
./bin/transcribe scan "https://drive.google.com/drive/folders/FOLDER_ID"

# Scan a local folder (auto-detected or use --local)
./bin/transcribe scan ./audio-files
./bin/transcribe scan --local ./audio-files
```

This validates the `lang=xx/` folder structure and reports file counts per language.

### Resume

Resume a pipeline that was interrupted (crash, Ctrl+C, terminal closed):

```bash
./bin/transcribe resume ./workspace/project_2025-01-15_143022
```

The tool reads `state.json`, skips completed steps, and continues from where it left off. Drive folder IDs are loaded from state — no re-prompting needed.

### Rework

After fixing files in the rework directory, reprocess and deliver them:

```bash
./bin/transcribe rework ./workspace/project_2025-01-15_143022
```

This re-runs conversion and validation on fixed XLSX files, merges passing files into the main output, and uploads them to the client's Drive.

---

## Pipeline Steps

The tool automates 14 steps with a single manual pause at QA:

| Step | Description                                                | Local Mode                         |
| ---- | ---------------------------------------------------------- | ---------------------------------- |
| 0    | Authenticate via Google OAuth                              | Skipped                            |
| 1    | Validate access to source Drive folder                     | Skipped                            |
| 2    | Validate access to Volga Drive folder                      | Skipped                            |
| 3    | Validate access to delivery Drive folder                   | Skipped                            |
| 4    | Scan and analyze source folder structure                   | `intake.Scan()`                    |
| 5    | Create local workspace                                     | Same                               |
| 6    | Download/copy audio files                                  | Copy instead of download           |
| 7    | Verify local copy matches source scan                      | Same                               |
| 8    | Generate manifest (with SHA-256 checksums)                 | Same                               |
| 9    | Generate XLSX templates                                    | Same                               |
| 10   | Upload templates to Volga Drive                            | Skipped                            |
| 11   | Transcription (manual, script, or Deepgram)                | Same                               |
|      | _Download completed XLSX back from Drive (if manual mode)_ | N/A                                |
| 12   | QA Review                                                  | **Same (manual)**                  |
| 13   | Convert approved XLSX to JSON                              | Same                               |
| 14   | Validate JSON + cross-stage reconciliation                 | Same                               |
|      | Route failures to rework                                   | Same                               |
| 15   | Package and deliver                                        | Upload skipped; output stays local |

### Hard Stops

The pipeline halts (non-zero exit) on:

- Invalid Drive folder structure (no `lang=xx/` folders)
- Download count mismatch vs scan count
- QA checklist with files still marked "pending"
- Validation failures are routed to rework (pipeline continues for passing files)

---

## Workspace Structure

Each project creates a structured workspace:

```
workspace/project_2025-01-15_143022/
├── 01_audio/           # Downloaded audio files (lang=xx/filename.mp3)
├── 02_manifests/       # manifest.json (file listing + SHA-256 checksums)
├── 03_templates/       # XLSX templates for transcribers
├── 04_transcribed/     # Completed XLSX (from transcription)
├── 05_json/            # Validated JSON output (final deliverables)
├── 06_logs/            # All reports + pipeline state
│   ├── state.json              # Pipeline state (for crash recovery)
│   ├── intake_report.json      # Drive scan results
│   ├── qa_checklist.json       # QA review checklist
│   ├── validation_report.json  # JSON schema validation results
│   ├── reconciliation_report.json  # Cross-stage count comparison
│   ├── delivery_summary.json   # Machine-readable delivery summary
│   └── delivery_summary.txt    # Human-readable delivery summary
└── 07_rework/          # Files requiring correction
    ├── xlsx/                   # Failed XLSX files to fix
    └── rework_manifest.json    # Reason + source for each failure
```

### State Persistence

`state.json` is updated after every step, enabling crash recovery:

```json
{
  "version": "1.0",
  "project_name": "project_2025-01-15_143022",
  "source_folder_id": "1ABC...",
  "volga_folder_id": "2DEF...",
  "delivery_folder_id": "3GHI...",
  "current_step": 9,
  "completed_steps": [1, 2, 3, 4, 5, 6, 7, 8],
  "counts": {
    "intake": 47,
    "downloaded": 47,
    "manifest": 47,
    "templates": 47,
    "qa_approved": 0,
    "qa_rework": 0,
    "json_produced": 0,
    "json_passed": 0,
    "json_failed": 0,
    "delivered": 0
  }
}
```

---

## QA Review Guide

When the pipeline reaches the QA step, it pauses and displays:

```
═══════════════════════════════════════════
QA REVIEW — MANUAL STEP
═══════════════════════════════════════════
Checklist:  ./workspace/.../06_logs/qa_checklist.json
Files:      ./workspace/.../04_transcribed/

For each file, set status to "approved" or "rework".
Add notes for any rework files explaining the issue.

Press Enter when QA is complete...
```

### How to complete QA:

1. Open `06_logs/qa_checklist.json` in any text editor
2. For each file, change `"status"` from `"pending"` to either:
   - `"approved"` — file passes QA, will be converted to JSON
   - `"rework"` — file needs fixes, will be routed to `07_rework/`
3. For rework files, add a note in the `"notes"` field explaining the issue
4. Save the file and press Enter in the terminal

Example checklist entry:

```json
{
  "name": "en_sample_001.xlsx",
  "language": "en",
  "status": "approved",
  "notes": ""
}
```

```json
{
  "name": "en_sample_002.xlsx",
  "language": "en",
  "status": "rework",
  "notes": "Wrong speaker labels in segments 4-7"
}
```

> **Important**: All files must be marked as either "approved" or "rework". The pipeline will error if any files remain "pending".

---

## Pluggable Transcription

The tool supports three transcription modes (all work in both Drive and local mode):

### Manual Mode (default)

The tool pauses and waits for human transcription:

1. Templates are uploaded to Volga Drive (Step 10)
2. Transcribers fill in the XLSX templates on Drive
3. User presses Enter when done
4. Completed XLSX files are downloaded back from Drive

### Script Mode

Plug in your own transcription script with `--transcribe-cmd`:

```bash
./bin/transcribe --transcribe-cmd="./scripts/my-transcriber.sh"
```

The script is called with:

```bash
./scripts/my-transcriber.sh --audio <audio-dir> --templates <template-dir> --output <output-dir>
```

Your script should:

- Read audio files from `--audio`
- Use templates from `--templates` as the base format
- Write completed XLSX files to `--output`
- Exit with code 0 on success, non-zero on failure
- Write progress to stdout/stderr (streamed to terminal with `[script]` prefix)

### Deepgram AI Mode

Set `DEEPGRAM_API_KEY` to enable automatic transcription via Deepgram's pre-recorded API:

```bash
DEEPGRAM_API_KEY=your-key ./bin/transcribe --local ./audio-files
```

Deepgram mode skips template generation and upload (steps 9-10). See `.env.example` for all Deepgram configuration options (`DEEPGRAM_MODEL`, `DEEPGRAM_WORKERS`, `DEEPGRAM_DIARIZE`, etc.).

**Priority**: `--transcribe-cmd` (script) > `DEEPGRAM_API_KEY` (AI) > manual mode

---

## Architecture

```
Transcription-Backend/
├── cmd/
│   └── transcribe/
│       └── main.go                 # CLI entrypoint + 14-step orchestrator
├── internal/
│   ├── auth/
│   │   └── oauth.go                # Google OAuth2 (PKCE, token cache, browser flow)
│   ├── gdrive/
│   │   ├── client.go               # Drive service + folder URL parsing + access validation
│   │   ├── scanner.go              # Remote folder structure validation + intake report
│   │   ├── downloader.go           # Concurrent download, ZIP extraction, resume
│   │   ├── uploader.go             # Concurrent upload (templates + delivery)
│   │   └── ratelimit.go            # Exponential backoff with jitter (429/403)
│   ├── workspace/
│   │   └── workspace.go            # Directory structure + state.json persistence
│   ├── intake/
│   │   └── scanner.go              # Local directory validation (post-download)
│   ├── manifest/
│   │   ├── types.go                # Manifest data structures
│   │   └── generator.go            # Manifest generation with SHA-256 checksums
│   ├── xlsx/
│   │   └── generator.go            # XLSX template generation (excelize)
│   ├── transcriber/
│   │   ├── transcriber.go          # Transcriber interface
│   │   ├── manual.go               # ManualTranscriber — pause for human
│   │   └── script.go               # ScriptTranscriber — external command
│   ├── qa/
│   │   └── qa.go                   # QA checklist generation + approved/rework split
│   ├── converter/
│   │   └── converter.go            # XLSX -> JSON (file-level filtering for QA)
│   ├── validator/
│   │   └── validator.go            # JSON schema validation + per-language stats
│   ├── reconciler/
│   │   └── reconciler.go           # Cross-stage count reconciliation (5-way)
│   ├── delivery/
│   │   └── delivery.go             # Package output + upload to client Drive
│   ├── rework/
│   │   └── rework.go               # Route failures + reprocess + merge back
│   └── logging/
│       ├── fields.go               # Structured logging field constants
│       └── logger.go               # Color-coded slog handler
├── schemas/
│   └── transcription.schema.json   # JSON Schema for transcription output
├── .env.example                    # Environment variable template
├── .gitignore
├── Makefile
├── go.mod
└── go.sum
```

### Key Design Decisions

| Decision                                    | Rationale                                     |
| ------------------------------------------- | --------------------------------------------- |
| `drive.DriveScope` (full access)            | Required for uploading templates and delivery |
| Token cached in `.token.json`               | Avoids re-authentication on every run         |
| `ConvertFiles(paths)` not `ConvertAll(dir)` | Only QA-approved files are converted          |
| Atomic state writes (temp + rename)         | Prevents corrupted state on crash             |
| 5-way reconciliation                        | Catches file loss at any pipeline stage       |
| Non-destructive rework                      | Approved outputs never overwritten            |
| Hard stops on failures                      | No "continue anyway" — forces data integrity  |
| Worker pools (default 5)                    | Concurrent Drive I/O without overwhelming API |
| Exponential backoff (429/403)               | Respects Google Drive API rate limits         |

---

## Development

### Makefile Targets

```bash
make build        # Build binary to ./bin/transcribe
make run          # Build and run
make run-verbose  # Build and run with --verbose
make test         # Run all tests with race detector
make vet          # Run go vet
make fmt          # Format code with gofmt
make lint         # Run golangci-lint (requires installation)
make tidy         # Run go mod tidy
make check        # fmt + vet + test
make clean        # Remove build artifacts
```

### Dependencies

| Package                                    | Version  | Purpose                          |
| ------------------------------------------ | -------- | -------------------------------- |
| `golang.org/x/oauth2`                      | v0.35.0  | Google OAuth2 + PKCE             |
| `google.golang.org/api`                    | v0.266.0 | Google Drive API v3              |
| `github.com/xuri/excelize/v2`              | v2.10.0  | XLSX file generation and reading |
| `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.2   | JSON Schema validation           |
| `github.com/joho/godotenv`                 | v1.5.1   | `.env` file loading              |

### Logging

The CLI uses structured logging via `log/slog` with a custom color handler:

| Level   | Color    | Example                                           |
| ------- | -------- | ------------------------------------------------- |
| INFO    | Cyan     | `[5/14] Generating manifest...`                   |
| SUCCESS | Green    | `47 templates generated`                          |
| WARN    | Yellow   | `segment ordering non-chronological`              |
| ERROR   | Red      | `schema validation failed for en_sample_002.json` |
| DEBUG   | Gray/Dim | File-level download progress, API calls           |

- Automatic TTY detection — colors for interactive terminals, JSON for pipes/CI
- `--verbose` enables DEBUG level
- `--json-log` forces JSON output regardless of terminal

---

## License

Internal use only. Proprietary to Volga Partners.
