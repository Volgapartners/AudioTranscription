import { NextResponse } from 'next/server';
import { openai } from '@/lib/openai';
import { createMinimalWav } from '@/lib/wav';
import { toFile } from 'openai';

export async function GET() {
  if (!openai) {
    return NextResponse.json(
      { ok: false, error: 'OPENAI_API_KEY not set' },
      { status: 500 }
    );
  }
  try {
    const wavBuffer = createMinimalWav();
    const file = await toFile(wavBuffer, 'test-whisper.wav', { type: 'audio/wav' });
    await openai.audio.transcriptions.create({
      file,
      model: 'whisper-1',
    });
    return NextResponse.json({ ok: true, message: 'Whisper API reachable' });
  } catch (err: unknown) {
    const e = err as { message?: string; code?: string };
    console.error('Whisper test failed:', e);
    return NextResponse.json(
      { ok: false, error: e.message, code: e.code },
      { status: 500 }
    );
  }
}
