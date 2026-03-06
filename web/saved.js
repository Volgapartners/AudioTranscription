const API_BASE =
  window.location.origin && window.location.origin.startsWith('http')
    ? window.location.origin
    : 'http://localhost:3000';

const refreshBtn = document.getElementById('refreshBtn');
const savedList = document.getElementById('savedList');

refreshBtn.addEventListener('click', loadSaved);

async function loadSaved() {
  savedList.innerHTML = '<p class="empty-message">Loading...</p>';
  try {
    const res = await fetch(`${API_BASE}/api/transcriptions`);
    const data = await res.json();
    if (!data.items || data.items.length === 0) {
      savedList.innerHTML = '<p class="empty-message">No saved transcriptions yet</p>';
      return;
    }
    savedList.innerHTML = data.items
      .map(
        (item) => `
      <div class="saved-item">
        <span class="saved-name">${escapeHtml(item.audioName)}</span>
        <span class="saved-meta">${item.utteranceCount} utterances · ${formatSize(item.size)} · ${formatDate(item.createdAt)}</span>
        <div class="saved-actions">
          <a href="${API_BASE}/api/transcriptions/${item.id}" class="btn-secondary" target="_blank">View JSON</a>
          <a href="${API_BASE}/api/transcriptions/${item.id}/audio" class="btn-download" download="${escapeHtml(item.audioName + (item.audioFile ? (item.audioFile.match(/\.[^.]+$/) || ['.wav'])[0] : '.wav'))}">Download audio</a>
          <button type="button" class="btn-secondary btn-load" data-id="${escapeHtml(item.id)}">Load</button>
        </div>
      </div>
    `
      )
      .join('');

    savedList.querySelectorAll('.btn-load').forEach((btn) => {
      btn.addEventListener('click', () => {
        window.location.href = `index.html?load=${encodeURIComponent(btn.dataset.id)}`;
      });
    });
  } catch (err) {
    savedList.innerHTML = `<p class="empty-message error">${escapeHtml(err.message)}</p>`;
  }
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function formatSize(bytes) {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
}

function formatDate(iso) {
  try {
    const d = new Date(iso);
    return d.toLocaleString();
  } catch {
    return iso || '';
  }
}

loadSaved();
