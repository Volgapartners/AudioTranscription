import { NextResponse } from 'next/server';
import { getSafeBasename } from '@/lib/file';

export const maxDuration = 60;

type ElevenLabsWord = {
  text: string;
  start?: number;
  end?: number;
  type?: string;
};

type ElevenLabsResponse = {
  text?: string;
  words?: ElevenLabsWord[];
};

function mapElevenLabsToUtterances(data: ElevenLabsResponse): Array<{
  speaker: string;
  start_time: number | null;
  end_time: number | null;
  transcript: string;
  emotion: string;
  language: string;
  locale: string;
  accent: string;
}> {
  const words = data.words ?? [];
  const lang = 'en';

  if (words.length === 0) {
    const fullText = (data.text ?? '').trim();
    if (fullText) {
      return [
        {
          speaker: 'speaker_01',
          start_time: null,
          end_time: null,
          transcript: fullText,
          emotion: '',
          language: lang,
          locale: lang,
          accent: '',
        },
      ];
    }
    return [];
  }

  return words.map((w) => ({
    speaker: 'speaker_01',
    start_time: typeof w.start === 'number' ? w.start : null,
    end_time: typeof w.end === 'number' ? w.end : null,
    transcript: (w.text ?? '').trim(),
    emotion: '',
    language: lang,
    locale: lang,
    accent: '',
  }));
}

export async function POST(request: Request) {
  try {
    const apiKey = process.env.ELEVENLABS_API_KEY;
    if (!apiKey) {
      return NextResponse.json(
        { error: 'ELEVENLABS_API_KEY not set. Add it to your .env or Vercel environment variables.' },
        { status: 500 }
      );
    }

    const formData = await request.formData();
    const audio = formData.get('audio');
    const isFileOrBlob =
      audio !== null &&
      typeof audio === 'object' &&
      typeof (audio as Blob).arrayBuffer === 'function' &&
      ((audio as object) instanceof File || (audio as object) instanceof Blob);
    if (!isFileOrBlob) {
      return NextResponse.json(
        { error: 'No audio file provided' },
        { status: 400 }
      );
    }

    const fileOrBlob = audio as File | Blob;
    const arrayBuffer = await fileOrBlob.arrayBuffer();
    const name = fileOrBlob instanceof File ? fileOrBlob.name : undefined;
    const type = fileOrBlob instanceof File ? fileOrBlob.type : undefined;
    const safeName = getSafeBasename(name);
    const body = new FormData();
    body.append('model_id', 'scribe_v2');
    body.append('file', new Blob([arrayBuffer], { type: type || 'audio/mpeg' }), safeName);

    const res = await fetch('https://api.elevenlabs.io/v1/speech-to-text', {
      method: 'POST',
      headers: {
        'xi-api-key': apiKey,
      },
      body,
    });

    if (!res.ok) {
      const errText = await res.text();
      let errJson: { detail?: { message?: string } | string } = {};
      try {
        errJson = JSON.parse(errText);
      } catch {
        // ignore
      }
      const detail = errJson.detail;
      const message = typeof detail === 'string'
        ? detail
        : (detail && typeof detail === 'object' && 'message' in detail)
          ? (detail as { message?: string }).message
          : errText || `ElevenLabs API error: ${res.status}`;
      return NextResponse.json(
        { error: message },
        { status: res.status >= 500 ? 502 : res.status }
      );
    }

    const data = (await res.json()) as ElevenLabsResponse;
    const utterances = mapElevenLabsToUtterances(data);

    return NextResponse.json({ utterances, rawResponse: data });
  } catch (err: unknown) {
    const e = err as { message?: string; code?: string };
    console.error('ElevenLabs transcription error:', err);
    const isConnectionError =
      e.code === 'ECONNREFUSED' ||
      e.code === 'ETIMEDOUT' ||
      e.code === 'ENOTFOUND' ||
      e.code === 'ECONNRESET' ||
      e.message?.toLowerCase().includes('fetch failed');
    const hint = isConnectionError
      ? ' Could not reach ElevenLabs. Check network and ELEVENLABS_API_KEY.'
      : '';
    return NextResponse.json(
      { error: (e.message || 'Transcription failed') + hint, code: e.code },
      { status: 500 }
    );
  }
}
