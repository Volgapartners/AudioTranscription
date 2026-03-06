# Audio Transcription Landing Page

A Volga interface for uploading audio, transcribing with Whisper, and editing utterances.

## Pages

- **Homepage** (`/` or `index.html`) — Upload audio, transcribe with Whisper, edit utterances, save to server
- **Saved** (`/saved` or `saved.html`) — Browse saved transcriptions (audio + transcription), download or load back
- **Record** (`/record` or `record.html`) — Record audio in the browser and save to the server; browse saved recordings

## Deploy to Vercel

1. Install Vercel CLI: `npm i -g vercel`
2. From the **web** directory: `cd web && vercel`
3. Set **Root Directory** to `web` (if deploying from repo root: Project Settings → General → Root Directory).
4. Add environment variable in Vercel: **OPENAI_API_KEY** (Project Settings → Environment Variables).
5. Redeploy. Your app will be at `https://your-project.vercel.app`.

**Note:** On Vercel, recordings and transcriptions are stored in `/tmp` and are **ephemeral** (lost between invocations or after idle). For persistent storage, add Vercel Blob or another store.

## Local setup

1. Install dependencies:
   ```bash
   cd web && npm install
   ```

2. Add your OpenAI API key (for Whisper transcription):
   ```bash
   cp .env.example .env
   # Edit .env and add your OPENAI_API_KEY
   ```

3. Start the server:
   ```bash
   npm start
   ```

4. Open http://localhost:3000

## Features

- **Upload audio** — Choose or drag-and-drop an audio file (mp3, wav, m4a, etc.)
- **Transcribe** — Sends the file to OpenAI's Whisper API and populates the utterances table
- **Edit** — All utterance fields (speaker, times, transcript, emotion, language, locale, accent) are editable
- **Playback** — Built-in audio player with speed and volume controls
- **Submit** — Collects edited data (logs to console; wire to your API as needed)
