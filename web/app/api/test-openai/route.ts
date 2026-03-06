import { NextResponse } from 'next/server';
import { openai } from '@/lib/openai';

export async function GET() {
  if (!openai) {
    return NextResponse.json(
      { ok: false, error: 'OPENAI_API_KEY not set' },
      { status: 500 }
    );
  }
  try {
    await openai.models.list();
    return NextResponse.json({
      ok: true,
      message: 'OpenAI API reachable from this server',
    });
  } catch (err: unknown) {
    const e = err as { message?: string; code?: string };
    console.error('OpenAI connectivity test failed:', err);
    return NextResponse.json(
      {
        ok: false,
        error: e.message,
        code: e.code,
        hint:
          'If curl works but this fails, Node may not be using your proxy. Try: HTTP_PROXY=... HTTPS_PROXY=... npm run dev',
      },
      { status: 500 }
    );
  }
}
