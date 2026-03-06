import fs from 'fs';
import path from 'path';

const isVercel = process.env.VERCEL === '1';

export const RECORDINGS_DIR = isVercel
  ? '/tmp/volga-recordings'
  : path.join(process.cwd(), 'recordings');

export const TRANSCRIPTIONS_DIR = isVercel
  ? '/tmp/volga-transcriptions'
  : path.join(process.cwd(), 'transcriptions');

export function ensureDirs(): void {
  if (!fs.existsSync(RECORDINGS_DIR)) fs.mkdirSync(RECORDINGS_DIR, { recursive: true });
  if (!fs.existsSync(TRANSCRIPTIONS_DIR)) fs.mkdirSync(TRANSCRIPTIONS_DIR, { recursive: true });
}
