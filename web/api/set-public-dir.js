// Run before server.js is loaded so static files are found on Vercel.
// Parent of api/ = project root (where index.html, styles.css live).
import path from 'path';
import { fileURLToPath } from 'url';
if (process.env.VERCEL === '1') {
  const apiDir = path.dirname(fileURLToPath(import.meta.url));
  process.env.VERCEL_PUBLIC_DIR = path.join(apiDir, '..');
}
