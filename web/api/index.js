import app from '../server.js';

export default function handler(req, res) {
  const url = req.url || '/';
  req.url = url.replace(/^\/api/, '') || '/';
  return app(req, res);
}
