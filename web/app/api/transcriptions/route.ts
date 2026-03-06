import { NextResponse } from 'next/server';
import fs from 'fs';
import path from 'path';
import {
  TRANSCRIPTIONS_DIR,
  ensureDirs,
} from '@/lib/storage';

export const maxDuration = 60;

export async function POST(request: Request) {
  try {
    ensureDirs();
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json({ error: e.message ?? 'Storage init failed' }, { status: 500 });
  }
  try {
    const formData = await request.formData();
    const audio = formData.get('audio');
    const transcriptionJson = formData.get('transcription') as string | null;

    if (!audio || !(audio instanceof File)) {
      return NextResponse.json(
        { error: 'No audio file provided' },
        { status: 400 }
      );
    }
    if (!transcriptionJson) {
      return NextResponse.json(
        { error: 'No transcription data provided' },
        { status: 400 }
      );
    }

    let transcription: unknown;
    try {
      transcription = JSON.parse(transcriptionJson);
    } catch {
      return NextResponse.json(
        { error: 'Invalid transcription JSON' },
        { status: 400 }
      );
    }

    const id = `t_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
    const dir = path.join(TRANSCRIPTIONS_DIR, id);
    fs.mkdirSync(dir, { recursive: true });

    const ext = path.extname(audio.name) || '.bin';
    const audioName = (audio.name || 'audio').replace(/\.[^.]+$/, '') || 'audio';
    const audioPath = path.join(dir, `audio${ext}`);
    const arrayBuffer = await audio.arrayBuffer();
    fs.writeFileSync(audioPath, Buffer.from(arrayBuffer));

    const meta = {
      id,
      audioName,
      audioFile: `audio${ext}`,
      createdAt: new Date().toISOString(),
      transcription,
    };
    fs.writeFileSync(
      path.join(dir, 'meta.json'),
      JSON.stringify(meta, null, 2)
    );

    return NextResponse.json({ ok: true, id });
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json({ error: e.message ?? 'Save failed' }, { status: 500 });
  }
}

export async function GET() {
  try {
    ensureDirs();
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json({ error: e.message ?? 'Storage init failed' }, { status: 500 });
  }
  try {
    const ids = fs.readdirSync(TRANSCRIPTIONS_DIR).filter((n) => {
      const p = path.join(TRANSCRIPTIONS_DIR, n);
      return fs.statSync(p).isDirectory() && !n.startsWith('.');
    });
    const items = ids
      .map((id) => {
        const metaPath = path.join(TRANSCRIPTIONS_DIR, id, 'meta.json');
        if (!fs.existsSync(metaPath)) return null;
        const meta = JSON.parse(
          fs.readFileSync(metaPath, 'utf8')
        ) as {
          audioName: string;
          audioFile: string;
          createdAt: string;
          transcription?: { utterances?: unknown[] };
        };
        const audioPath = path.join(TRANSCRIPTIONS_DIR, id, meta.audioFile);
        const size = fs.existsSync(audioPath)
          ? fs.statSync(audioPath).size
          : 0;
        return {
          id,
          audioName: meta.audioName,
          audioFile: meta.audioFile,
          createdAt: meta.createdAt,
          size,
          utteranceCount: meta.transcription?.utterances?.length ?? 0,
        };
      })
      .filter(Boolean) as Array<{
        id: string;
        audioName: string;
        audioFile: string;
        createdAt: string;
        size: number;
        utteranceCount: number;
      }>;
    items.sort(
      (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
    );
    return NextResponse.json({ items });
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json(
      { error: e.message },
      { status: 500 }
    );
  }
}
