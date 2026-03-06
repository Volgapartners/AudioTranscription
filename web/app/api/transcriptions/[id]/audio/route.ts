import { NextResponse } from 'next/server';
import fs from 'fs';
import path from 'path';
import { TRANSCRIPTIONS_DIR } from '@/lib/storage';

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const { id: rawId } = await params;
  const id = path.basename(rawId).replace(/[^a-zA-Z0-9_-]/g, '');
  if (!id || !id.startsWith('t_')) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 });
  }
  const metaPath = path.join(TRANSCRIPTIONS_DIR, id, 'meta.json');
  if (!fs.existsSync(metaPath)) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 });
  }
  const meta = JSON.parse(fs.readFileSync(metaPath, 'utf8')) as {
    audioFile: string;
  };
  const audioPath = path.join(TRANSCRIPTIONS_DIR, id, meta.audioFile);
  if (!fs.existsSync(audioPath)) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 });
  }
  const buffer = fs.readFileSync(audioPath);
  return new NextResponse(buffer, {
    headers: {
      'Content-Type': 'audio/wav',
      'Content-Disposition': `inline; filename="${meta.audioFile}"`,
    },
  });
}
