import { NextResponse } from 'next/server';
import fs from 'fs';
import path from 'path';
import { RECORDINGS_DIR } from '@/lib/storage';

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ filename: string }> }
) {
  const { filename: raw } = await params;
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
}
