import dotenv from 'dotenv';
import path from 'path';
import { fileURLToPath } from 'url';
import express from 'express';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const isVercel = process.env.VERCEL === '1';

// Load .env from web/ or project root (local only)
if (!isVercel) {
  dotenv.config();
  dotenv.config({ path: path.join(__dirname, '..', '.env') });
}

import fs from 'fs';
import multer from 'multer';
import OpenAI, { toFile } from 'openai';

const RECORDINGS_DIR = isVercel ? '/tmp/volga-recordings' : path.join(__dirname, 'recordings');
const TRANSCRIPTIONS_DIR = isVercel ? '/tmp/volga-transcriptions' : path.join(__dirname, 'transcriptions');
if (!fs.existsSync(RECORDINGS_DIR)) fs.mkdirSync(RECORDINGS_DIR, { recursive: true });
if (!fs.existsSync(TRANSCRIPTIONS_DIR)) fs.mkdirSync(TRANSCRIPTIONS_DIR, { recursive: true });

const app = express();
const upload = multer({ storage: multer.memoryStorage() });
const recordingsUpload = multer({
  storage: multer.diskStorage({
    destination: (req, file, cb) => cb(null, RECORDINGS_DIR),
    filename: (req, file, cb) => {
      const base = file.originalname || `recording_${Date.now()}.wav`;
      const safe = base.replace(/[^a-zA-Z0-9._-]/g, '_');
      cb(null, safe.endsWith('.wav') ? safe : safe + '.wav');
    },
  }),
});

const openai = process.env.OPENAI_API_KEY
  ? new OpenAI({
      apiKey: process.env.OPENAI_API_KEY,
      timeout: 120000, // 2 min for audio upload + transcription
      ...(process.env.OPENAI_BASE_URL && {
        baseURL: process.env.OPENAI_BASE_URL,
      }),
    })
  : null;

// Serve static files
app.use(express.static(path.join(__dirname)));

// CORS for local dev
app.use((req, res, next) => {
  res.header('Access-Control-Allow-Origin', '*');
  res.header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
  res.header('Access-Control-Allow-Headers', 'Content-Type');
  if (req.method === 'OPTIONS') return res.sendStatus(200);
  next();
});

// Create minimal valid WAV (0.1s silence, 16kHz mono) for Whisper connectivity test
function createMinimalWav() {
  const sampleRate = 16000;
  const duration = 0.1;
  const numSamples = Math.floor(sampleRate * duration);
  const dataSize = numSamples * 2; // 16-bit = 2 bytes per sample
  const fileSize = 36 + dataSize;
  const buffer = Buffer.alloc(44 + dataSize);
  let offset = 0;
  buffer.write('RIFF', offset); offset += 4;
  buffer.writeUInt32LE(fileSize, offset); offset += 4;
  buffer.write('WAVE', offset); offset += 4;
  buffer.write('fmt ', offset); offset += 4;
  buffer.writeUInt32LE(16, offset); offset += 4;
  buffer.writeUInt16LE(1, offset); offset += 2;
  buffer.writeUInt16LE(1, offset); offset += 2;
  buffer.writeUInt32LE(sampleRate, offset); offset += 4;
  buffer.writeUInt32LE(sampleRate * 2, offset); offset += 4;
  buffer.writeUInt16LE(2, offset); offset += 2;
  buffer.writeUInt16LE(16, offset); offset += 2;
  buffer.write('data', offset); offset += 4;
  buffer.writeUInt32LE(dataSize, offset); offset += 4;
  return buffer;
}

// Test Whisper specifically - sends minimal audio to transcription API
app.get('/api/test-whisper', async (req, res) => {
  if (!openai) {
    return res.status(500).json({ ok: false, error: 'OPENAI_API_KEY not set' });
  }
  try {
    const wavBuffer = createMinimalWav();
    const file = await toFile(wavBuffer, 'test-whisper.wav', { type: 'audio/wav' });
    await openai.audio.transcriptions.create({
      file,
      model: 'whisper-1',
    });
    res.json({ ok: true, message: 'Whisper API reachable' });
  } catch (err) {
    console.error('Whisper test failed:', err);
    res.status(500).json({
      ok: false,
      error: err.message,
      code: err.code,
    });
  }
});

// Connectivity test - hit GET /api/test-openai to verify Node can reach OpenAI
app.get('/api/test-openai', async (req, res) => {
  if (!openai) {
    return res.status(500).json({ ok: false, error: 'OPENAI_API_KEY not set' });
  }
  try {
    await openai.models.list();
    res.json({ ok: true, message: 'OpenAI API reachable from this server' });
  } catch (err) {
    console.error('OpenAI connectivity test failed:', err);
    res.status(500).json({
      ok: false,
      error: err.message,
      code: err.code,
      hint: 'If curl works but this fails, Node may not be using your proxy. Try: HTTP_PROXY=... HTTPS_PROXY=... npm start',
    });
  }
});

// Preview Whisper request (returns request structure without calling OpenAI)
app.post('/api/transcribe-preview', upload.single('audio'), (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No audio file provided. Upload a file first.' });
  }
  const key = process.env.OPENAI_API_KEY || '';
  const maskedKey = key ? `${key.slice(0, 7)}...${key.slice(-4)}` : '(not set)';
  const request = {
    method: 'POST',
    url: (process.env.OPENAI_BASE_URL || 'https://api.openai.com/v1') + '/audio/transcriptions',
    headers: {
      Authorization: `Bearer ${maskedKey}`,
      'Content-Type': 'multipart/form-data',
    },
    body: {
      file: {
        name: req.file.originalname,
        size: req.file.size,
        mimetype: req.file.mimetype,
      },
      model: 'whisper-1',
      response_format: 'verbose_json',
      timestamp_granularities: ['segment'],
    },
  };
  res.json({ request });
});

// Transcribe endpoint
app.post('/api/transcribe', upload.single('audio'), async (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No audio file provided' });
  }

  if (!openai) {
    return res.status(500).json({
      error: 'OPENAI_API_KEY not set. Add it to your .env or environment.',
    });
  }

  try {
    const file = await toFile(req.file.buffer, req.file.originalname, {
      type: req.file.mimetype,
    });

    let transcription;
    const maxRetries = 2;
    for (let attempt = 0; attempt <= maxRetries; attempt++) {
      try {
        transcription = await openai.audio.transcriptions.create({
          file,
          model: 'whisper-1',
          response_format: 'verbose_json',
          timestamp_granularities: ['segment'],
        });
        break;
      } catch (retryErr) {
        const isRetryable =
          retryErr.code === 'ECONNRESET' ||
          retryErr.code === 'ETIMEDOUT' ||
          retryErr.code === 'ECONNREFUSED';
        if (isRetryable && attempt < maxRetries) {
          console.warn(`Whisper attempt ${attempt + 1} failed, retrying...`, retryErr.message);
          await new Promise((r) => setTimeout(r, 1000 * (attempt + 1)));
        } else {
          throw retryErr;
        }
      }
    }

    // Map Whisper segments to utterance format
    const utterances = (transcription.segments || []).map((seg) => ({
      speaker: 'speaker_01',
      start_time: seg.start ?? null,
      end_time: seg.end ?? null,
      transcript: (seg.text || '').trim(),
      emotion: '',
      language: transcription.language || 'en',
      locale: transcription.language || 'en',
      accent: '',
    }));

    // If no segments, use full text as single utterance
    if (utterances.length === 0 && transcription.text) {
      utterances.push({
        speaker: 'speaker_01',
        start_time: null,
        end_time: null,
        transcript: transcription.text.trim(),
        emotion: '',
        language: transcription.language || 'en',
        locale: transcription.language || 'en',
        accent: '',
      });
    }

    res.json({ utterances });
  } catch (err) {
    console.error('Transcription error:', err);
    const isConnectionError =
      err.code === 'ECONNREFUSED' ||
      err.code === 'ETIMEDOUT' ||
      err.code === 'ENOTFOUND' ||
      err.code === 'ECONNRESET' ||
      err.message?.toLowerCase().includes('fetch failed');
    const hint = isConnectionError
      ? ' Could not reach OpenAI. Try: (1) different network/VPN, (2) check firewall, (3) if in a restricted region, use OPENAI_BASE_URL to route via a proxy.'
      : '';
    res.status(500).json({
      error: (err.message || 'Transcription failed') + hint,
      code: err.code,
    });
  }
});

// Transcriptions: save audio + transcription, list, and retrieve
app.post('/api/transcriptions', upload.single('audio'), (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No audio file provided' });
  }
  const transcriptionJson = req.body.transcription;
  if (!transcriptionJson) {
    return res.status(400).json({ error: 'No transcription data provided' });
  }
  let transcription;
  try {
    transcription = JSON.parse(transcriptionJson);
  } catch {
    return res.status(400).json({ error: 'Invalid transcription JSON' });
  }
  const id = `t_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
  const dir = path.join(TRANSCRIPTIONS_DIR, id);
  fs.mkdirSync(dir, { recursive: true });
  const ext = path.extname(req.file.originalname) || '.bin';
  const audioName = (req.file.originalname || 'audio').replace(/\.[^.]+$/, '') || 'audio';
  const audioPath = path.join(dir, `audio${ext}`);
  fs.writeFileSync(audioPath, req.file.buffer);
  const meta = {
    id,
    audioName,
    audioFile: `audio${ext}`,
    createdAt: new Date().toISOString(),
    transcription,
  };
  fs.writeFileSync(path.join(dir, 'meta.json'), JSON.stringify(meta, null, 2));
  res.json({ ok: true, id });
});

app.get('/api/transcriptions', (req, res) => {
  try {
    const ids = fs.readdirSync(TRANSCRIPTIONS_DIR).filter((n) => {
      const p = path.join(TRANSCRIPTIONS_DIR, n);
      return fs.statSync(p).isDirectory() && !n.startsWith('.');
    });
    const items = ids
      .map((id) => {
        const metaPath = path.join(TRANSCRIPTIONS_DIR, id, 'meta.json');
        if (!fs.existsSync(metaPath)) return null;
        const meta = JSON.parse(fs.readFileSync(metaPath, 'utf8'));
        const audioPath = path.join(TRANSCRIPTIONS_DIR, id, meta.audioFile);
        const size = fs.existsSync(audioPath) ? fs.statSync(audioPath).size : 0;
        return {
          id,
          audioName: meta.audioName,
          audioFile: meta.audioFile,
          createdAt: meta.createdAt,
          size,
          utteranceCount: meta.transcription?.utterances?.length ?? 0,
        };
      })
      .filter(Boolean)
      .sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt));
    res.json({ items });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

app.get('/api/transcriptions/:id', (req, res) => {
  const id = path.basename(req.params.id).replace(/[^a-zA-Z0-9_-]/g, '');
  if (!id || !id.startsWith('t_')) return res.status(404).json({ error: 'Not found' });
  const metaPath = path.join(TRANSCRIPTIONS_DIR, id, 'meta.json');
  if (!fs.existsSync(metaPath)) return res.status(404).json({ error: 'Not found' });
  const meta = JSON.parse(fs.readFileSync(metaPath, 'utf8'));
  res.json(meta);
});

app.get('/api/transcriptions/:id/audio', (req, res) => {
  const id = path.basename(req.params.id).replace(/[^a-zA-Z0-9_-]/g, '');
  if (!id || !id.startsWith('t_')) return res.status(404).json({ error: 'Not found' });
  const metaPath = path.join(TRANSCRIPTIONS_DIR, id, 'meta.json');
  if (!fs.existsSync(metaPath)) return res.status(404).json({ error: 'Not found' });
  const meta = JSON.parse(fs.readFileSync(metaPath, 'utf8'));
  const audioPath = path.join(TRANSCRIPTIONS_DIR, id, meta.audioFile);
  if (!fs.existsSync(audioPath)) return res.status(404).json({ error: 'Not found' });
  res.sendFile(path.resolve(audioPath));
});

// Recordings: save and list
app.post('/api/recordings', recordingsUpload.single('audio'), (req, res) => {
  if (!req.file) {
    return res.status(400).json({ error: 'No audio file provided' });
  }
  res.json({ ok: true, filename: req.file.filename });
});

app.get('/api/recordings', (req, res) => {
  try {
    const names = fs.readdirSync(RECORDINGS_DIR);
    const files = names
      .filter((n) => !n.startsWith('.'))
      .map((n) => {
        const stat = fs.statSync(path.join(RECORDINGS_DIR, n));
        return {
          name: n,
          size: stat.size,
          date: stat.mtime.toISOString(),
        };
      })
      .sort((a, b) => new Date(b.date) - new Date(a.date));
    res.json({ files });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

app.get('/api/recordings/:filename', (req, res) => {
  const name = path.basename(req.params.filename).replace(/[^a-zA-Z0-9._-]/g, '');
  if (!name) return res.status(404).json({ error: 'Not found' });
  const filePath = path.join(RECORDINGS_DIR, name);
  if (!fs.existsSync(filePath)) return res.status(404).json({ error: 'Not found' });
  res.setHeader('Content-Type', 'audio/wav');
  res.setHeader('Content-Disposition', `inline; filename="${name}"`);
  res.sendFile(path.resolve(filePath));
});

// SPA fallback
app.get('/', (req, res) => res.sendFile(path.join(__dirname, 'index.html')));
app.get('/record', (req, res) => res.sendFile(path.join(__dirname, 'record.html')));
app.get('/saved', (req, res) => res.sendFile(path.join(__dirname, 'saved.html')));
app.get('*', (req, res) => {
  res.sendFile(path.join(__dirname, 'index.html'));
});

const PORT = process.env.PORT || 3000;
if (!isVercel) {
  app.listen(PORT, () => {
    console.log(`Server running at http://localhost:${PORT}`);
    if (!openai) {
      console.warn('Warning: OPENAI_API_KEY not set. Transcribe will fail until configured.');
    }
  });
}

export default app;
