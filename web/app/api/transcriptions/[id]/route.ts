import { NextResponse } from 'next/server';
import fs from 'fs';
import path from 'path';
import { TRANSCRIPTIONS_DIR } from '@/lib/storage';

export async function GET(
  _request: Request,
  context: { params: Promise<{ id?: string }> }
) {
  try {
    const params = await context.params;
    const rawId = params?.id;
    if (typeof rawId !== 'string') {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }
    const id = path.basename(rawId).replace(/[^a-zA-Z0-9_-]/g, '');
    if (!id || !id.startsWith('t_')) {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }
    const metaPath = path.join(TRANSCRIPTIONS_DIR, id, 'meta.json');
    if (!fs.existsSync(metaPath)) {
      return NextResponse.json({ error: 'Not found' }, { status: 404 });
    }
    const meta = JSON.parse(fs.readFileSync(metaPath, 'utf8'));
    return NextResponse.json(meta);
  } catch (err: unknown) {
    const e = err as { message?: string };
    return NextResponse.json(
      { error: e.message ?? 'Internal error' },
      { status: 500 }
    );
  }
}
