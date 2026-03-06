import './set-public-dir.js'; // must run first so server.js sees VERCEL_PUBLIC_DIR
import app from '../server.js';

export default function handler(req, res) {
  req.url = req.url || '/';
  return app(req, res);
}
