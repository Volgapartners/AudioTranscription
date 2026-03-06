'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';

type SavedItem = {
  id: string;
  audioName: string;
  audioFile: string;
  createdAt: string;
  size: number;
  utteranceCount: number;
};

function escapeHtml(str: string): string {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso || '';
  }
}

export default function SavedPage() {
  const [items, setItems] = useState<SavedItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadSaved = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/transcriptions');
      const data = (await res.json()) as { items?: SavedItem[] };
      if (!res.ok) throw new Error((data as { error?: string }).error ?? 'Failed');
      setItems(data.items ?? []);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSaved();
  }, [loadSaved]);

  return (
    <main className="main">
      <h1 className="title">Saved transcriptions</h1>
      <p className="instruction">
        Browse audio and transcriptions saved from the homepage
      </p>

      <section className="saved-section">
        <button
          type="button"
          className="btn-secondary"
          onClick={loadSaved}
        >
          Refresh
        </button>
        <div id="savedList" className="saved-list">
          {loading && <p className="empty-message">Loading...</p>}
          {!loading && error && (
            <p className="empty-message error">{escapeHtml(error)}</p>
          )}
          {!loading && !error && items.length === 0 && (
            <p className="empty-message">No saved transcriptions yet</p>
          )}
          {!loading && !error && items.length > 0 &&
            items.map((item) => (
              <div key={item.id} className="saved-item">
                <span className="saved-name">
                  {escapeHtml(item.audioName)}
                </span>
                <span className="saved-meta">
                  {item.utteranceCount} utterances · {formatSize(item.size)} ·{' '}
                  {formatDate(item.createdAt)}
                </span>
                <div className="saved-actions">
                  <Link
                    href={`/api/transcriptions/${item.id}`}
                    className="btn-secondary"
                    target="_blank"
                  >
                    View JSON
                  </Link>
                  <a
                    href={`/api/transcriptions/${item.id}/audio`}
                    className="btn-download"
                    download={
                      item.audioName +
                      (item.audioFile?.match(/\.[^.]+$/) ?? ['.wav'])[0]
                    }
                  >
                    Download audio
                  </a>
                  <Link
                    href={`/?load=${encodeURIComponent(item.id)}`}
                    className="btn-secondary"
                  >
                    Load
                  </Link>
                </div>
              </div>
            ))}
        </div>
      </section>
    </main>
  );
}
