'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

type RecordingFile = {
  name: string;
  size: number;
  date: string;
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

function formatTime(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, '0')}`;
}

type RecordState = {
  recorder: MediaRecorder;
  stream: MediaStream;
  chunks: Blob[];
  timerId: ReturnType<typeof setInterval>;
};

export default function RecordPage() {
  const [recordStatus, setRecordStatus] = useState('Ready');
  const [recordTime, setRecordTime] = useState('0:00');
  const [files, setFiles] = useState<RecordingFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [recordBtnDisabled, setRecordBtnDisabled] = useState(false);
  const [stopBtnDisabled, setStopBtnDisabled] = useState(true);

  const recordStateRef = useRef<RecordState | null>(null);

  const loadRecordings = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/recordings');
      const data = (await res.json()) as { files?: RecordingFile[] };
      if (!res.ok) throw new Error((data as { error?: string }).error ?? 'Failed');
      setFiles(data.files ?? []);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadRecordings();
  }, [loadRecordings]);

  const startRecording = useCallback(async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const recorder = new MediaRecorder(stream);
      const chunks: Blob[] = [];
      recorder.ondataavailable = (e) => {
        if (e.data.size > 0) chunks.push(e.data);
      };
      recorder.start();
      const startTime = Date.now();
      const timerId = setInterval(() => {
        setRecordTime(formatTime((Date.now() - startTime) / 1000));
      }, 100);
      recordStateRef.current = { recorder, stream, chunks, timerId };
      setRecordBtnDisabled(true);
      setStopBtnDisabled(false);
      setRecordStatus('Recording...');
    } catch (err) {
      setRecordStatus('Recording failed: ' + (err as Error).message);
    }
  }, []);

  const stopRecording = useCallback(async () => {
    const state = recordStateRef.current;
    if (!state?.recorder || state.recorder.state === 'inactive') return;

    state.recorder.onstop = async () => {
      state.stream.getTracks().forEach((t) => t.stop());
      clearInterval(state.timerId);
      recordStateRef.current = null;
      const blob = new Blob(state.chunks, { type: 'audio/webm' });
      const arrayBuffer = await blob.arrayBuffer();
      const audioContext = new (window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext)();
      const audioBuffer = await audioContext.decodeAudioData(arrayBuffer);

      const numChannels = audioBuffer.numberOfChannels;
      const sampleRate = audioBuffer.sampleRate;
      const bitDepth = 16;
      const bytesPerSample = bitDepth / 8;
      const blockAlign = numChannels * bytesPerSample;
      const dataLength = audioBuffer.length * numChannels * bytesPerSample;
      const buffer = new ArrayBuffer(44 + dataLength);
      const view = new DataView(buffer);
      let offset = 0;
      const writeStr = (s: string) => {
        for (let i = 0; i < s.length; i++)
          view.setUint8(offset++, s.charCodeAt(i));
      };
      writeStr('RIFF');
      view.setUint32(offset, 36 + dataLength, true); offset += 4;
      writeStr('WAVE');
      writeStr('fmt ');
      view.setUint32(offset, 16, true); offset += 4;
      view.setUint16(offset, 1, true); offset += 2;
      view.setUint16(offset, numChannels, true); offset += 2;
      view.setUint32(offset, sampleRate, true); offset += 4;
      view.setUint32(offset, sampleRate * blockAlign, true); offset += 4;
      view.setUint16(offset, blockAlign, true); offset += 2;
      view.setUint16(offset, bitDepth, true); offset += 2;
      writeStr('data');
      view.setUint32(offset, dataLength, true); offset += 4;
      const channels: Float32Array[] = [];
      for (let c = 0; c < numChannels; c++)
        channels.push(audioBuffer.getChannelData(c));
      for (let i = 0; i < audioBuffer.length; i++) {
        for (let c = 0; c < numChannels; c++) {
          const s = Math.max(-1, Math.min(1, channels[c][i]));
          view.setInt16(offset, s < 0 ? s * 0x8000 : s * 0x7fff, true);
          offset += 2;
        }
      }

      const wavBlob = new Blob([buffer], { type: 'audio/wav' });
      const formData = new FormData();
      formData.append('audio', wavBlob, `recording_${Date.now()}.wav`);
      try {
        const res = await fetch('/api/recordings', {
          method: 'POST',
          body: formData,
        });
        if (!res.ok) {
          const err = await res.json().catch(() => ({}));
          throw new Error((err as { error?: string }).error ?? `Save failed: ${res.status}`);
        }
        setRecordStatus('Recording saved');
        loadRecordings();
      } catch (err) {
        setRecordStatus('Save failed: ' + (err as Error).message);
      }
      setRecordTime('0:00');
      setRecordBtnDisabled(false);
      setStopBtnDisabled(true);
    };

    state.recorder.stop();
    setRecordBtnDisabled(true);
    setStopBtnDisabled(true);
    setRecordStatus('Processing...');
  }, [loadRecordings]);

  return (
    <main className="main">
      <h1 className="title">Record audio</h1>
      <p className="instruction">
        Record audio and save it to the server
      </p>

      <section className="record-section">
        <h2>Record</h2>
        <div className="record-controls">
          <button
            type="button"
            id="recordBtn"
            className="btn-record"
            disabled={recordBtnDisabled}
            onClick={startRecording}
          >
            Start recording
          </button>
          <button
            type="button"
            id="stopBtn"
            className="btn-stop"
            disabled={stopBtnDisabled}
            onClick={stopRecording}
          >
            Stop
          </button>
          <span id="recordStatus" className="record-status">
            {recordStatus}
          </span>
        </div>
        <p id="recordTime" className="record-time">
          {recordTime}
        </p>
      </section>

      <section className="recordings-section">
        <h2>Saved recordings</h2>
        <button
          type="button"
          className="btn-secondary"
          onClick={loadRecordings}
        >
          Refresh
        </button>
        <div id="recordingsList" className="recordings-list">
          {loading && <p className="empty-message">Loading...</p>}
          {!loading && error && (
            <p className="empty-message error">{escapeHtml(error)}</p>
          )}
          {!loading && !error && files.length === 0 && (
            <p className="empty-message">No recordings yet</p>
          )}
          {!loading && !error && files.length > 0 &&
            files.map((f) => (
              <div key={f.name} className="recording-item">
                <span className="recording-name">{escapeHtml(f.name)}</span>
                <span className="recording-size">{formatSize(f.size)}</span>
                <span className="recording-date">{f.date ?? ''}</span>
                <div className="recording-actions">
                  <audio
                    controls
                    src={`/api/recordings/${encodeURIComponent(f.name)}`}
                  />
                  <a
                    href={`/api/recordings/${encodeURIComponent(f.name)}`}
                    download={f.name}
                    className="btn-download"
                  >
                    Download
                  </a>
                </div>
              </div>
            ))}
        </div>
      </section>
    </main>
  );
}