import { NextResponse } from 'next/server';
import { getSafeBasename } from '@/lib/file';

export const maxDuration = 60;

type ElevenLabsWordV2 = {
  text: string;
  start?: number;
  end?: number;
  type?: string;
  speaker_id?: string;
};

type ElevenLabsResponseV2 = {
  text?: string;
  words?: ElevenLabsWordV2[];
};

function normalizeSpeaker(
  speakerId: string,
  speakerMap: Map<string, string>
): string {
  if (speakerMap.has(speakerId)) {
    return speakerMap.get(speakerId)!;
  }
  const label = `speaker_${String(speakerMap.size + 1).padStart(2, '0')}`;
  speakerMap.set(speakerId, label);
  return label;
}

/**
 * Python-style: merge consecutive words by speaker_id into segments,
 * then map to app utterance shape.
 */
function mapElevenLabsV2ToUtterances(
  data: ElevenLabsResponseV2,
  langCode: string
): Array<{
  speaker: string;
  start_time: number | null;
  end_time: number | null;
  transcript: string;
  emotion: string;
  language: string;
  locale: string;
  accent: string;
}> {
  const wordsList = data.words ?? [];
  const lang = langCode || 'en';
  const locale = lang;
  const accent = '';

  if (wordsList.length === 0) {
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
          locale,
          accent,
        },
      ];
    }
    return [];
  }

  type Segment = { speaker: string; start: number; end: number; text: string };
  const segments: Segment[] = [];
  let currentSegment: Segment | null = null;

  for (const w of wordsList) {
    const textChunk = (w.text ?? '').trim();
    const speakerId = w.speaker_id ?? 'Unknown';
    const start = typeof w.start === 'number' ? w.start : 0;
    const end = typeof w.end === 'number' ? w.end : start;

    if (currentSegment === null) {
      currentSegment = {
        speaker: speakerId,
        start,
        end,
        text: textChunk ? (textChunk + ' ') : '',
      };
    } else if (currentSegment.speaker === speakerId) {
      currentSegment.end = end;
      currentSegment.text += textChunk ? (textChunk + ' ') : '';
    } else {
      segments.push(currentSegment);
      currentSegment = {
        speaker: speakerId,
        start,
        end,
        text: textChunk ? (textChunk + ' ') : '',
      };
    }
  }
  if (currentSegment) {
    segments.push(currentSegment);
  }

  const speakerMap = new Map<string, string>();
  return segments.map((seg) => ({
    speaker: normalizeSpeaker(seg.speaker, speakerMap),
    start_time: seg.start,
    end_time: seg.end,
    transcript: seg.text.trim(),
    emotion: '',
    language: lang,
    locale,
    accent,
  }));
}

export async function POST(request: Request) {
  try {
    const apiKey = process.env.ELEVENLABS_API_KEY;
    if (!apiKey) {
      return NextResponse.json(
        {
          error:
            'ELEVENLABS_API_KEY not set. Add it to your .env or Vercel environment variables.',
        },
        { status: 500 }
      );
    }

    const formData = await request.formData();
    const audio = formData.get('audio');
    const languageCode = (formData.get('language_code') as string) || 'en';

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
    body.append('diarize', 'true');
    body.append('tag_audio_events', 'true');
    body.append('language_code', languageCode);
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
      const message =
        typeof detail === 'string'
          ? detail
          : detail && typeof detail === 'object' && 'message' in detail
            ? (detail as { message?: string }).message
            : errText || `ElevenLabs API error: ${res.status}`;
      return NextResponse.json(
        { error: message },
        { status: res.status >= 500 ? 502 : res.status }
      );
    }

    const data = (await res.json()) as ElevenLabsResponseV2;
    const utterances = mapElevenLabsV2ToUtterances(data, languageCode);

    return NextResponse.json({ utterances, rawResponse: data });
  } catch (err: unknown) {
    const e = err as { message?: string; code?: string };
    console.error('ElevenLabs v2 transcription error:', err);
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
