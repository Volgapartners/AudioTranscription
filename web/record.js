const API_BASE =
  window.location.origin && window.location.origin.startsWith('http')
    ? window.location.origin
    : 'http://localhost:3000';

let mediaRecorder = null;
let currentStream = null;
let audioChunks = [];
let recordStartTime = null;
let recordTimerId = null;

const recordBtn = document.getElementById('recordBtn');
const stopBtn = document.getElementById('stopBtn');
const recordStatus = document.getElementById('recordStatus');
const recordTime = document.getElementById('recordTime');
const refreshBtn = document.getElementById('refreshBtn');
const recordingsList = document.getElementById('recordingsList');

recordBtn.addEventListener('click', startRecording);
stopBtn.addEventListener('click', stopRecording);
refreshBtn.addEventListener('click', loadRecordings);

function formatTime(seconds) {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, '0')}`;
}

async function startRecording() {
  try {
    currentStream = await navigator.mediaDevices.getUserMedia({ audio: true });
    mediaRecorder = new MediaRecorder(currentStream);
    audioChunks = [];

    mediaRecorder.ondataavailable = (e) => {
      if (e.data.size > 0) audioChunks.push(e.data);
    };

    mediaRecorder.start();
    recordStartTime = Date.now();
    recordTimerId = setInterval(() => {
      recordTime.textContent = formatTime((Date.now() - recordStartTime) / 1000);
    }, 100);

    recordBtn.disabled = true;
    stopBtn.disabled = false;
    recordStatus.textContent = 'Recording...';
    recordStatus.style.color = '#c62828';
  } catch (err) {
    recordStatus.textContent = 'Recording failed: ' + err.message;
    recordStatus.style.color = '#c62828';
  }
}

function stopRecording() {
  if (!mediaRecorder || mediaRecorder.state === 'inactive') return;

  mediaRecorder.onstop = async () => {
    if (currentStream) currentStream.getTracks().forEach((t) => t.stop());
    const blob = new Blob(audioChunks, { type: 'audio/webm' });
    try {
      const wavBlob = await blobToWav(blob);
      await saveRecording(wavBlob);
      recordStatus.textContent = 'Recording saved';
      recordStatus.style.color = '#2e7d32';
      loadRecordings();
    } catch (err) {
      recordStatus.textContent = 'Save failed: ' + err.message;
      recordStatus.style.color = '#c62828';
    }
    recordTime.textContent = '0:00';
    recordBtn.disabled = false;
    stopBtn.disabled = true;
  };

  mediaRecorder.stop();
  clearInterval(recordTimerId);
  recordBtn.disabled = true;
  stopBtn.disabled = true;
  recordStatus.textContent = 'Processing...';
}

async function blobToWav(blob) {
  const arrayBuffer = await blob.arrayBuffer();
  const audioContext = new (window.AudioContext || window.webkitAudioContext)();
  const audioBuffer = await audioContext.decodeAudioData(arrayBuffer);
  const wav = encodeWav(audioBuffer);
  return new Blob([wav], { type: 'audio/wav' });
}

function encodeWav(audioBuffer) {
  const numChannels = audioBuffer.numberOfChannels;
  const sampleRate = audioBuffer.sampleRate;
  const format = 1; // PCM
  const bitDepth = 16;
  const bytesPerSample = bitDepth / 8;
  const blockAlign = numChannels * bytesPerSample;
  const dataLength = audioBuffer.length * numChannels * bytesPerSample;
  const buffer = new ArrayBuffer(44 + dataLength);
  const view = new DataView(buffer);
  let offset = 0;

  const write = (data, len) => {
    for (let i = 0; i < len; i++) view.setUint8(offset++, data[i]);
  };
  const writeStr = (s) => write([...s].map((c) => c.charCodeAt(0)), s.length);

  writeStr('RIFF');
  view.setUint32(offset, 36 + dataLength, true); offset += 4;
  writeStr('WAVE');
  writeStr('fmt ');
  view.setUint32(offset, 16, true); offset += 4;
  view.setUint16(offset, format, true); offset += 2;
  view.setUint16(offset, numChannels, true); offset += 2;
  view.setUint32(offset, sampleRate, true); offset += 4;
  view.setUint32(offset, sampleRate * blockAlign, true); offset += 4;
  view.setUint16(offset, blockAlign, true); offset += 2;
  view.setUint16(offset, bitDepth, true); offset += 2;
  writeStr('data');
  view.setUint32(offset, dataLength, true); offset += 4;

  const channels = [];
  for (let c = 0; c < numChannels; c++) channels.push(audioBuffer.getChannelData(c));
  for (let i = 0; i < audioBuffer.length; i++) {
    for (let c = 0; c < numChannels; c++) {
      const s = Math.max(-1, Math.min(1, channels[c][i]));
      view.setInt16(offset, s < 0 ? s * 0x8000 : s * 0x7fff, true);
      offset += 2;
    }
  }
  return buffer;
}

async function saveRecording(blob) {
  const formData = new FormData();
  const filename = `recording_${Date.now()}.wav`;
  formData.append('audio', blob, filename);

  const res = await fetch(`${API_BASE}/api/recordings`, {
    method: 'POST',
    body: formData,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Save failed: ${res.status}`);
  }
}

async function loadRecordings() {
  recordingsList.innerHTML = '<p class="empty-message">Loading...</p>';
  try {
    const res = await fetch(`${API_BASE}/api/recordings`);
    const data = await res.json();
    if (!data.files || data.files.length === 0) {
      recordingsList.innerHTML = '<p class="empty-message">No recordings yet</p>';
      return;
    }
    recordingsList.innerHTML = data.files
      .map(
        (f) => `
      <div class="recording-item">
        <span class="recording-name">${escapeHtml(f.name)}</span>
        <span class="recording-size">${formatSize(f.size)}</span>
        <span class="recording-date">${f.date || ''}</span>
        <div class="recording-actions">
          <audio controls src="${API_BASE}/api/recordings/${encodeURIComponent(f.name)}"></audio>
          <a href="${API_BASE}/api/recordings/${encodeURIComponent(f.name)}" download="${escapeHtml(f.name)}" class="btn-download">Download</a>
        </div>
      </div>
    `
      )
      .join('');
  } catch (err) {
    recordingsList.innerHTML = `<p class="empty-message error">${escapeHtml(err.message)}</p>`;
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

loadRecordings();
