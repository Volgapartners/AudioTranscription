import './set-public-dir.js'; // must run first so server.js sees VERCEL_PUBLIC_DIR
import app from '../server.js';

export default function handler(req, res) {
  const url = req.url || '/';
  req.url = url.replace(/^\/api/, '') || '/';
  return app(req, res);
}
