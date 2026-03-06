import { NextResponse } from 'next/server';
import fs from 'fs';
import path from 'path';
import { RECORDINGS_DIR, ensureDirs } from '@/lib/storage';

export async function POST(request: Request) {
  ensureDirs();
  const formData = await request.formData();
  const audio = formData.get('audio');
  if (!audio || !(audio instanceof File)) {
    return NextResponse.json(
      { error: 'No audio file provided' },
      { status: 400 }
    );
  }

  const base = audio.name || `recording_${Date.now()}.wav`;
  const safe = base.replace(/[^a-zA-Z0-9._-]/g, '_');
  const filename = safe.endsWith('.wav') ? safe : safe + '.wav';
  const destPath = path.join(RECORDINGS_DIR, filename);
  const arrayBuffer = await audio.arrayBuffer();
  fs.writeFileSync(destPath, Buffer.from(arrayBuffer));

  return NextResponse.json({ ok: true, filename });
}

export async function GET() {
  ensureDirs();
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
      .sort(
        (a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()
      );
    return NextResponse.json({ files });
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json(
      { error: e.message },
      { status: 500 }
    );
  }
}
