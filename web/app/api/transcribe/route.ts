import { NextResponse } from 'next/server';
import { openai } from '@/lib/openai';
import { toFile } from 'openai';

export const maxDuration = 60;
const maxRetries = 2;

export async function POST(request: Request) {
  try {
    const formData = await request.formData();
    const audio = formData.get('audio');
    if (!audio || !(audio instanceof File)) {
      return NextResponse.json(
        { error: 'No audio file provided' },
        { status: 400 }
      );
    }

    if (!openai) {
      return NextResponse.json(
        {
          error:
            'OPENAI_API_KEY not set. Add it to your .env or environment.',
        },
        { status: 500 }
      );
    }

    const arrayBuffer = await audio.arrayBuffer();
    const buffer = Buffer.from(arrayBuffer);
    const file = await toFile(buffer, audio.name, { type: audio.type });

    let transcription;
    for (let attempt = 0; attempt <= maxRetries; attempt++) {
      try {
        transcription = await openai.audio.transcriptions.create({
          file,
          model: 'whisper-1',
          response_format: 'verbose_json',
          timestamp_granularities: ['segment'],
        });
        break;
      } catch (retryErr: unknown) {
        const e = retryErr as { code?: string };
        const isRetryable =
          e.code === 'ECONNRESET' ||
          e.code === 'ETIMEDOUT' ||
          e.code === 'ECONNREFUSED';
        if (isRetryable && attempt < maxRetries) {
          await new Promise((r) =>
            setTimeout(r, 1000 * (attempt + 1))
          );
        } else {
          throw retryErr;
        }
      }
    }

    const segments = (transcription as { segments?: Array<{ start?: number; end?: number; text?: string }>; text?: string; language?: string }).segments || [];
    const lang = (transcription as { language?: string }).language || 'en';

    const utterances = segments.map((seg) => ({
      speaker: 'speaker_01',
      start_time: seg.start ?? null,
      end_time: seg.end ?? null,
      transcript: (seg.text || '').trim(),
      emotion: '',
      language: lang,
      locale: lang,
      accent: '',
    }));

    const text = (transcription as { text?: string }).text;
    if (utterances.length === 0 && text) {
      utterances.push({
        speaker: 'speaker_01',
        start_time: null,
        end_time: null,
        transcript: text.trim(),
        emotion: '',
        language: lang,
        locale: lang,
        accent: '',
      });
    }

    return NextResponse.json({ utterances });
  } catch (err: unknown) {
    const e = err as { message?: string; code?: string };
    console.error('Transcription error:', err);
    const isConnectionError =
      e.code === 'ECONNREFUSED' ||
      e.code === 'ETIMEDOUT' ||
      e.code === 'ENOTFOUND' ||
      e.code === 'ECONNRESET' ||
      e.message?.toLowerCase().includes('fetch failed');
    const hint = isConnectionError
      ? ' Could not reach OpenAI. Try: (1) different network/VPN, (2) check firewall, (3) if in a restricted region, use OPENAI_BASE_URL to route via a proxy.'
      : '';
    return NextResponse.json(
      {
        error: (e.message || 'Transcription failed') + hint,
        code: e.code,
      },
      { status: 500 }
    );
  }
}
