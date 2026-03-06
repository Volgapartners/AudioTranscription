import { NextResponse } from 'next/server';
import fs from 'fs';
import path from 'path';
import { RECORDINGS_DIR } from '@/lib/storage';

export async function GET(
  _request: Request,
  context: { params: Promise<{ filename?: string }> }
) {
  try {
    const params = await context.params;
    const raw = params?.filename;
    if (typeof raw !== 'string') {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }
    const name = path.basename(raw).replace(/[^a-zA-Z0-9._-]/g, '');
    if (!name) {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }
    const filePath = path.join(RECORDINGS_DIR, name);
    if (!fs.existsSync(filePath)) {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }
    const buffer = fs.readFileSync(filePath);
    return new NextResponse(buffer, {
      headers: {
        'Content-Type': 'audio/wav',
        'Content-Disposition': `inline; filename="${name}"`,
      },
    });
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json(
      { error: e.message ?? 'Internal error' },
      { status: 500 }
    );
  }
}
