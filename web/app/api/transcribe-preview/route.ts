import { NextResponse } from 'next/server';

export async function POST(request: Request) {
  try {
    const formData = await request.formData();
    const audio = formData.get('audio');
    if (!audio || !(audio instanceof File)) {
      return NextResponse.json(
        { error: 'No audio file provided. Upload a file first.' },
        { status: 400 }
      );
    }
    const key = process.env.OPENAI_API_KEY || '';
    const maskedKey = key ? `${key.slice(0, 7)}...${key.slice(-4)}` : '(not set)';
    const requestPreview = {
      method: 'POST',
      url:
        (process.env.OPENAI_BASE_URL || 'https://api.openai.com/v1') +
        '/audio/transcriptions',
      headers: {
        Authorization: `Bearer ${maskedKey}`,
        'Content-Type': 'multipart/form-data',
      },
      body: {
        file: {
          name: audio.name,
          size: audio.size,
          mimetype: audio.type,
        },
        model: 'whisper-1',
        response_format: 'verbose_json',
        timestamp_granularities: ['segment'],
      },
    };
    return NextResponse.json({ request: requestPreview });
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json(
      { error: e.message ?? 'Request failed' },
      { status: 500 }
    );
  }
}
