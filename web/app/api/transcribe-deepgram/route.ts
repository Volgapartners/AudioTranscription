import { NextResponse } from 'next/server';
import path from 'path';
import { config as loadEnv } from 'dotenv';

export const maxDuration = 60;

function loadEnvIfNeeded(): void {
  if (process.env.DEEPGRAM_API_KEY) return;
  const cwd = process.cwd();
  const pathsToTry = [
    path.join(cwd, '.env'),
    path.join(cwd, '.env.local'),
    path.join(cwd, 'web', '.env'),
    path.join(cwd, 'web', '.env.local'),
  ];
  for (const envPath of pathsToTry) {
    const result = loadEnv({ path: envPath });
    if (result.parsed?.DEEPGRAM_API_KEY) {
      Object.assign(process.env, result.parsed);
      break;
    }
  }
}

type DeepgramWord = {
  word: string;
  start: number;
  end: number;
  punctuated_word?: string;
};

type DeepgramAlternative = {
  transcript?: string;
  words?: DeepgramWord[];
};

type DeepgramResponse = {
  results?: {
    channels?: Array<{
      alternatives?: DeepgramAlternative[];
    }>;
  };
};

function mapDeepgramToUtterances(data: DeepgramResponse): Array<{
  speaker: string;
  start_time: number | null;
  end_time: number | null;
  transcript: string;
  emotion: string;
  language: string;
  locale: string;
  accent: string;
}> {
  const alternatives = data.results?.channels?.[0]?.alternatives;
  const alt = alternatives?.[0];
  const words = alt?.words ?? [];
  const lang = 'en';

  if (words.length === 0) {
    const fullText = (alt?.transcript ?? '').trim();
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
    start_time: w.start,
    end_time: w.end,
    transcript: (w.punctuated_word ?? w.word ?? '').trim(),
    emotion: '',
    language: lang,
    locale: lang,
    accent: '',
  }));
}

export async function POST(request: Request) {
  try {
    loadEnvIfNeeded();
    const apiKey = process.env.DEEPGRAM_API_KEY;
    if (!apiKey) {
      return NextResponse.json(
        { error: 'DEEPGRAM_API_KEY not set. Add it to web/.env and restart the dev server (npm run dev from the web folder).' },
        { status: 500 }
      );
    }

    const formData = await request.formData();
    const audio = formData.get('audio');
    if (!audio || !(audio instanceof File)) {
      return NextResponse.json(
        { error: 'No audio file provided' },
        { status: 400 }
      );
    }

    const arrayBuffer = await audio.arrayBuffer();
    const buffer = Buffer.from(arrayBuffer);
    const contentType = audio.type || 'audio/wav';

    const params = new URLSearchParams({
      model: 'nova-2',
      smart_format: 'true',
      punctuate: 'true',
    });

    const deepgramRes = await fetch(
      `https://api.deepgram.com/v1/listen?${params.toString()}`,
      {
        method: 'POST',
        headers: {
          Authorization: `Token ${apiKey}`,
          'Content-Type': contentType,
        },
        body: buffer,
      }
    );

    if (!deepgramRes.ok) {
      const errText = await deepgramRes.text();
      let errJson: { err_msg?: string } = {};
      try {
        errJson = JSON.parse(errText);
      } catch {
        // ignore
      }
      const message = errJson.err_msg ?? (errText || `Deepgram API error: ${deepgramRes.status}`);
      return NextResponse.json(
        { error: message },
        { status: deepgramRes.status >= 500 ? 502 : 400 }
      );
    }

    const data = (await deepgramRes.json()) as DeepgramResponse;
    const utterances = mapDeepgramToUtterances(data);

    return NextResponse.json({ utterances, rawResponse: data });
  } catch (err: unknown) {
    const e = err as { message?: string; code?: string };
    console.error('Deepgram transcription error:', err);
    const isConnectionError =
      e.code === 'ECONNREFUSED' ||
      e.code === 'ETIMEDOUT' ||
      e.code === 'ENOTFOUND' ||
      e.code === 'ECONNRESET' ||
      e.message?.toLowerCase().includes('fetch failed');
    const hint = isConnectionError
      ? ' Could not reach Deepgram. Check network and DEEPGRAM_API_KEY.'
      : '';
    return NextResponse.json(
      { error: (e.message || 'Transcription failed') + hint, code: e.code },
      { status: 500 }
    );
  }
}
