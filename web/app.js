// Use current origin when served over http(s); fallback for file:// or when origin is null
const API_BASE =
  window.location.origin && window.location.origin.startsWith('http')
    ? window.location.origin
    : 'http://localhost:3000';

let audioFile = null;
let audioElement = null;
let audioObjectURL = null;

// DOM elements
const uploadBtn = document.getElementById('uploadBtn');
const audioFileInput = document.getElementById('audioFile');
const fileNameEl = document.getElementById('fileName');
const openAudioLink = document.getElementById('openAudioLink');
const jsonStatus = document.getElementById('jsonStatus');
const playBtn = document.getElementById('playBtn');
const rewindBtn = document.getElementById('rewindBtn');
const forwardBtn = document.getElementById('forwardBtn');
const currentTimeEl = document.getElementById('currentTime');
const totalTimeEl = document.getElementById('totalTime');
const speedSelect = document.getElementById('speedSelect');
const volumeSlider = document.getElementById('volumeSlider');
const audioStatus = document.getElementById('audioStatus');
const progressFill = document.getElementById('progressFill');
const utterancesBody = document.getElementById('utterancesBody');
const transcribeBtn = document.getElementById('transcribeBtn');
const transcribeStatus = document.getElementById('transcribeStatus');
const submitBtn = document.getElementById('submitBtn');
const saveBtn = document.getElementById('saveBtn');
const testOpenAiBtn = document.getElementById('testOpenAiBtn');
const openAiStatus = document.getElementById('openAiStatus');
const testWhisperBtn = document.getElementById('testWhisperBtn');
const whisperStatus = document.getElementById('whisperStatus');
const showRequestBtn = document.getElementById('showRequestBtn');
const requestDisplay = document.getElementById('requestDisplay');

// Show Whisper request (what gets sent to OpenAI)
showRequestBtn.addEventListener('click', async () => {
  if (!audioFile) {
    requestDisplay.textContent = 'Upload an audio file first.';
    return;
  }
  requestDisplay.textContent = 'Loading...';
  try {
    const formData = new FormData();
    formData.append('audio', audioFile);
    const res = await fetch(`${API_BASE}/api/transcribe-preview`, {
      method: 'POST',
      body: formData,
    });
    const data = await res.json();
    if (data.request) {
      requestDisplay.textContent = JSON.stringify(data.request, null, 2);
    } else {
      requestDisplay.textContent = data.error || 'Failed to get request';
    }
  } catch (err) {
    requestDisplay.textContent = err.message || 'Request failed';
  }
});

// Test OpenAI key (uses key from server .env)
testOpenAiBtn.addEventListener('click', async () => {
  openAiStatus.textContent = 'Checking...';
  openAiStatus.style.color = '';
  whisperStatus.textContent = '';
  try {
    const res = await fetch(`${API_BASE}/api/test-openai`);
    const data = await res.json();
    if (data.ok) {
      openAiStatus.textContent = data.message || 'OpenAI API reachable';
      openAiStatus.style.color = '#2e7d32';
    } else {
      openAiStatus.textContent = data.error || 'Failed';
      openAiStatus.style.color = '#c62828';
    }
  } catch (err) {
    openAiStatus.textContent = err.message || 'Connection failed';
    openAiStatus.style.color = '#c62828';
  }
});

// Test Whisper specifically (sends minimal audio to transcription API)
testWhisperBtn.addEventListener('click', async () => {
  whisperStatus.textContent = 'Checking Whisper...';
  whisperStatus.style.color = '';
  try {
    const res = await fetch(`${API_BASE}/api/test-whisper`);
    const data = await res.json();
    if (data.ok) {
      whisperStatus.textContent = data.message || 'Whisper reachable';
      whisperStatus.style.color = '#2e7d32';
    } else {
      whisperStatus.textContent = data.error || 'Failed';
      whisperStatus.style.color = '#c62828';
    }
  } catch (err) {
    whisperStatus.textContent = err.message || 'Connection failed';
    whisperStatus.style.color = '#c62828';
  }
});

// Upload handling
uploadBtn.addEventListener('click', () => audioFileInput.click());

audioFileInput.addEventListener('change', (e) => {
  const file = e.target.files[0];
  if (!file) return;

  if (!file.type.startsWith('audio/')) {
    transcribeStatus.textContent = 'Please select an audio file (mp3, wav, etc.)';
    transcribeStatus.style.color = '#c62828';
    return;
  }

  // Clean up previous
  if (audioObjectURL) URL.revokeObjectURL(audioObjectURL);
  if (audioElement) audioElement.remove();

  audioFile = file;
  fileNameEl.textContent = file.name;
  transcribeBtn.disabled = false;
  transcribeStatus.textContent = '';
  jsonStatus.textContent = 'JSON status: No transcription yet';
  requestDisplay.textContent = '';

  // Create audio element for playback
  audioObjectURL = URL.createObjectURL(file);
  audioElement = new Audio(audioObjectURL);

  openAudioLink.href = audioObjectURL;
  openAudioLink.style.display = 'inline';
  audioStatus.textContent = 'Audio loaded';

  setupAudioListeners();
  clearUtterances();
});

// Drag and drop
uploadBtn.addEventListener('dragover', (e) => {
  e.preventDefault();
  uploadBtn.style.background = '#e3f2fd';
});

uploadBtn.addEventListener('dragleave', () => {
  uploadBtn.style.background = '';
});

uploadBtn.addEventListener('drop', (e) => {
  e.preventDefault();
  uploadBtn.style.background = '';
  if (e.dataTransfer.files.length) {
    audioFileInput.files = e.dataTransfer.files;
    audioFileInput.dispatchEvent(new Event('change'));
  }
});

function setupAudioListeners() {
  audioElement.addEventListener('timeupdate', () => {
    currentTimeEl.textContent = formatTime(audioElement.currentTime);
    progressFill.style.width = `${(audioElement.currentTime / audioElement.duration) * 100}%`;
  });

  audioElement.addEventListener('loadedmetadata', () => {
    totalTimeEl.textContent = formatTime(audioElement.duration);
  });

  audioElement.addEventListener('ended', () => {
    playBtn.textContent = 'Play';
  });

  speedSelect.addEventListener('change', () => {
    audioElement.playbackRate = parseFloat(speedSelect.value);
  });

  volumeSlider.addEventListener('input', () => {
    audioElement.volume = volumeSlider.value / 100;
  });
}

function formatTime(seconds) {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, '0')}`;
}

// Playback controls
playBtn.addEventListener('click', () => {
  if (!audioElement) return;
  if (audioElement.paused) {
    audioElement.play();
    playBtn.textContent = 'Pause';
  } else {
    audioElement.pause();
    playBtn.textContent = 'Play';
  }
});

rewindBtn.addEventListener('click', () => {
  if (audioElement) {
    audioElement.currentTime = Math.max(0, audioElement.currentTime - 2);
  }
});

forwardBtn.addEventListener('click', () => {
  if (audioElement) {
    audioElement.currentTime = Math.min(audioElement.duration, audioElement.currentTime + 2);
  }
});

// Progress bar click to seek
document.querySelector('.progress-bar').addEventListener('click', (e) => {
  if (!audioElement) return;
  const rect = e.currentTarget.getBoundingClientRect();
  const percent = (e.clientX - rect.left) / rect.width;
  audioElement.currentTime = percent * audioElement.duration;
});

// Transcribe
transcribeBtn.addEventListener('click', async () => {
  if (!audioFile) return;

  transcribeBtn.disabled = true;
  transcribeStatus.textContent = 'Transcribing...';
  transcribeStatus.style.color = '';

  try {
    const formData = new FormData();
    formData.append('audio', audioFile);

    const res = await fetch(`${API_BASE}/api/transcribe`, {
      method: 'POST',
      body: formData,
    });

    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      throw new Error(err.error || `Server error: ${res.status}`);
    }

    const data = await res.json();
    renderUtterances(data.utterances);
    jsonStatus.textContent = `JSON status: Loaded (${data.utterances.length} utterances)`;
    transcribeStatus.textContent = 'Transcription complete';
  } catch (err) {
    const isConnectionError =
      err.name === 'TypeError' &&
      (err.message.includes('fetch') || err.message.includes('network'));
    transcribeStatus.textContent = isConnectionError
      ? 'Connection failed. Is the server running? Run: cd web && npm start, then open http://localhost:3000'
      : err.message || 'Transcription failed';
    transcribeStatus.style.color = '#c62828';
  } finally {
    transcribeBtn.disabled = false;
  }
});

function renderUtterances(utterances) {
  utterancesBody.innerHTML = '';

  utterances.forEach((u, i) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td><input type="text" value="${escapeHtml(u.speaker || 'speaker_01')}" data-field="speaker"></td>
      <td><input type="text" value="${escapeHtml(String(u.start_time ?? ''))}" data-field="start_time"></td>
      <td><input type="text" value="${escapeHtml(String(u.end_time ?? ''))}" data-field="end_time"></td>
      <td><input type="text" value="${escapeHtml(u.transcript || '')}" data-field="transcript"></td>
      <td><input type="text" value="${escapeHtml(u.emotion || '')}" data-field="emotion"></td>
      <td><input type="text" value="${escapeHtml(u.language || 'en')}" data-field="language"></td>
      <td><input type="text" value="${escapeHtml(u.locale || 'en')}" data-field="locale"></td>
      <td><input type="text" value="${escapeHtml(u.accent || '')}" data-field="accent"></td>
    `;
    utterancesBody.appendChild(tr);
  });
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function clearUtterances() {
  utterancesBody.innerHTML = `
    <tr class="empty-row">
      <td colspan="8">Upload an audio file and click "Transcribe" to generate utterances</td>
    </tr>
  `;
}

function getUtterancesFromTable() {
  const rows = utterancesBody.querySelectorAll('tr:not(.empty-row)');
  return Array.from(rows).map((tr) => {
    const inputs = tr.querySelectorAll('input');
    return {
      speaker: inputs[0]?.value || 'speaker_01',
      start_time: parseFloat(inputs[1]?.value) ?? null,
      end_time: parseFloat(inputs[2]?.value) ?? null,
      transcript: inputs[3]?.value || '',
      emotion: inputs[4]?.value || '',
      language: inputs[5]?.value || 'en',
      locale: inputs[6]?.value || 'en',
      accent: inputs[7]?.value || '',
    };
  });
}

// Save - upload audio + transcription to server
saveBtn.addEventListener('click', async () => {
  if (!audioFile) {
    alert('Upload an audio file first.');
    return;
  }
  const utterances = getUtterancesFromTable();
  if (utterances.length === 0) {
    alert('Transcribe first to save. No utterances in the table.');
    return;
  }
  const transcription = {
    audio_name: audioFile.name.replace(/\.[^.]+$/, '') || 'unknown',
    utterances,
  };
  try {
    const formData = new FormData();
    formData.append('audio', audioFile);
    formData.append('transcription', JSON.stringify(transcription));
    const res = await fetch(`${API_BASE}/api/transcriptions`, {
      method: 'POST',
      body: formData,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      throw new Error(err.error || `Save failed: ${res.status}`);
    }
    await res.json();
    alert('Saved. View in the Saved tab.');
  } catch (err) {
    alert('Save failed: ' + err.message);
  }
});

// Submit - collect edited data
submitBtn.addEventListener('click', () => {
  const utterances = getUtterancesFromTable();
  if (utterances.length === 0) {
    alert('No utterances to submit. Transcribe first.');
    return;
  }

  const output = {
    audio_name: audioFile?.name?.replace(/\.[^.]+$/, '') || 'unknown',
    utterances,
  };

  console.log('Submit payload:', output);
  alert('Submission logged to console. In production, this would POST to your API.');
});

// Load saved transcription from URL ?load=id
(async function initLoad() {
  const params = new URLSearchParams(window.location.search);
  const loadId = params.get('load');
  if (!loadId) return;
  try {
    const [metaRes, audioRes] = await Promise.all([
      fetch(`${API_BASE}/api/transcriptions/${encodeURIComponent(loadId)}`),
      fetch(`${API_BASE}/api/transcriptions/${encodeURIComponent(loadId)}/audio`),
    ]);
    if (!metaRes.ok || !audioRes.ok) throw new Error('Not found');
    const meta = await metaRes.json();
    const audioBlob = await audioRes.blob();
    const ext = meta.audioFile ? (meta.audioFile.match(/\.[^.]+$/) || ['.wav'])[0] : '.wav';
    const file = new File([audioBlob], (meta.audioName || 'audio') + ext, { type: audioBlob.type || 'audio/wav' });
    if (audioObjectURL) URL.revokeObjectURL(audioObjectURL);
    if (audioElement) audioElement.remove();
    audioFile = file;
    fileNameEl.textContent = file.name;
    transcribeBtn.disabled = false;
    audioObjectURL = URL.createObjectURL(file);
    audioElement = new Audio(audioObjectURL);
    openAudioLink.href = audioObjectURL;
    openAudioLink.style.display = 'inline';
    audioStatus.textContent = 'Audio loaded';
    setupAudioListeners();
    renderUtterances(meta.transcription?.utterances || []);
    jsonStatus.textContent = `JSON status: Loaded (${(meta.transcription?.utterances || []).length} utterances)`;
    window.history.replaceState({}, '', 'index.html');
  } catch (err) {
    alert('Could not load: ' + err.message);
  }
})();
