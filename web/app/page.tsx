'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

type Utterance = {
  speaker: string;
  start_time: number | null;
  end_time: number | null;
  transcript: string;
  emotion: string;
  language: string;
  locale: string;
  accent: string;
};

function escapeHtml(str: string): string {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function formatTime(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, '0')}`;
}

export default function HomePage() {
  const [audioFile, setAudioFile] = useState<File | null>(null);
  const [fileName, setFileName] = useState('');
  const [audioUrl, setAudioUrl] = useState<string | null>(null);
  const [jsonStatus, setJsonStatus] = useState('JSON status: No transcription yet');
  const [transcribeStatus, setTranscribeStatus] = useState('');
  const [transcribeDisabled, setTranscribeDisabled] = useState(true);
  const [openAiStatus, setOpenAiStatus] = useState('');
  const [whisperStatus, setWhisperStatus] = useState('');
  const [requestDisplay, setRequestDisplay] = useState('');
  const [utterances, setUtterances] = useState<Utterance[]>([]);
  const [currentTime, setCurrentTime] = useState('0:00');
  const [totalTime, setTotalTime] = useState('0:00');
  const [progressPercent, setProgressPercent] = useState(0);
  const [audioStatus, setAudioStatus] = useState('No audio loaded');
  const [playing, setPlaying] = useState(false);

  const audioRef = useRef<HTMLAudioElement | null>(null);
  const utterancesBodyRef = useRef<HTMLTableSectionElement>(null);

  const getUtterancesFromTable = useCallback((): Utterance[] => {
    const tbody = utterancesBodyRef.current;
    if (!tbody) return [];
    const rows = tbody.querySelectorAll('tr:not(.empty-row)');
    return Array.from(rows).map((tr) => {
      const inputs = tr.querySelectorAll('input');
      return {
        speaker: inputs[0]?.value ?? 'speaker_01',
        start_time: parseFloat(inputs[1]?.value ?? '') ?? null,
        end_time: parseFloat(inputs[2]?.value ?? '') ?? null,
        transcript: inputs[3]?.value ?? '',
        emotion: inputs[4]?.value ?? '',
        language: inputs[5]?.value ?? 'en',
        locale: inputs[6]?.value ?? 'en',
        accent: inputs[7]?.value ?? '',
      };
    });
  }, []);

  const renderUtterances = useCallback((list: Utterance[]) => {
    setUtterances(list);
  }, []);

  const handleFileChange = useCallback(
    (file: File | null) => {
      if (!file) return;
      if (!file.type.startsWith('audio/')) {
        setTranscribeStatus('Please select an audio file (mp3, wav, etc.)');
        return;
      }
      if (audioUrl) URL.revokeObjectURL(audioUrl);
      setAudioFile(file);
      setFileName(file.name);
      setTranscribeDisabled(false);
      setTranscribeStatus('');
      setJsonStatus('JSON status: No transcription yet');
      setRequestDisplay('');
      setUtterances([]);
      const url = URL.createObjectURL(file);
      setAudioUrl(url);
      setAudioStatus('Audio loaded');
      setProgressPercent(0);
      setCurrentTime('0:00');
      setTotalTime('0:00');
      setPlaying(false);
    },
    [audioUrl]
  );

  const onTranscribe = async () => {
    if (!audioFile) return;
    setTranscribeDisabled(true);
    setTranscribeStatus('Transcribing...');
    try {
      const formData = new FormData();
      formData.append('audio', audioFile);
      const res = await fetch('/api/transcribe', {
        method: 'POST',
        body: formData,
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Server error: ${res.status}`);
      }
      const data = (await res.json()) as { utterances: Utterance[] };
      renderUtterances(data.utterances);
      setJsonStatus(`JSON status: Loaded (${data.utterances.length} utterances)`);
      setTranscribeStatus('Transcription complete');
    } catch (err) {
      const e = err as Error;
      setTranscribeStatus(
        e.message?.includes('fetch') || e.message?.includes('network')
          ? 'Connection failed. Is the server running?'
          : e.message ?? 'Transcription failed'
      );
    } finally {
      setTranscribeDisabled(false);
    }
  };

  const onSave = async () => {
    if (!audioFile) {
      alert('Upload an audio file first.');
      return;
    }
    const list = getUtterancesFromTable();
    if (list.length === 0) {
      alert('Transcribe first to save. No utterances in the table.');
      return;
    }
    const transcription = {
      audio_name: audioFile.name.replace(/\.[^.]+$/, '') || 'unknown',
      utterances: list,
    };
    try {
      const formData = new FormData();
      formData.append('audio', audioFile);
      formData.append('transcription', JSON.stringify(transcription));
      const res = await fetch('/api/transcriptions', {
        method: 'POST',
        body: formData,
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Save failed: ${res.status}`);
      }
      alert('Saved. View in the Saved tab.');
    } catch (err) {
      alert('Save failed: ' + (err as Error).message);
    }
  };

  const onTestOpenAi = async () => {
    setOpenAiStatus('Checking...');
    try {
      const res = await fetch('/api/test-openai');
      const data = (await res.json()) as { ok?: boolean; message?: string; error?: string };
      if (data.ok) {
        setOpenAiStatus(data.message ?? 'OpenAI API reachable');
      } else {
        setOpenAiStatus(data.error ?? 'Failed');
      }
    } catch (err) {
      setOpenAiStatus((err as Error).message ?? 'Connection failed');
    }
  };

  const onTestWhisper = async () => {
    setWhisperStatus('Checking Whisper...');
    try {
      const res = await fetch('/api/test-whisper');
      const data = (await res.json()) as { ok?: boolean; message?: string; error?: string };
      if (data.ok) {
        setWhisperStatus(data.message ?? 'Whisper reachable');
      } else {
        setWhisperStatus(data.error ?? 'Failed');
      }
    } catch (err) {
      setWhisperStatus((err as Error).message ?? 'Connection failed');
    }
  };

  const onShowRequest = async () => {
    if (!audioFile) {
      setRequestDisplay('Upload an audio file first.');
      return;
    }
    setRequestDisplay('Loading...');
    try {
      const formData = new FormData();
      formData.append('audio', audioFile);
      const res = await fetch('/api/transcribe-preview', {
        method: 'POST',
        body: formData,
      });
      const data = (await res.json()) as { request?: unknown; error?: string };
      if (data.request) {
        setRequestDisplay(JSON.stringify(data.request, null, 2));
      } else {
        setRequestDisplay(data.error ?? 'Failed to get request');
      }
    } catch (err) {
      setRequestDisplay((err as Error).message ?? 'Request failed');
    }
  };

  const onProgressClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!audioRef.current) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const percent = (e.clientX - rect.left) / rect.width;
    audioRef.current.currentTime = percent * audioRef.current.duration;
  };

  useEffect(() => {
    const params = new URLSearchParams(
      typeof window !== 'undefined' ? window.location.search : ''
    );
    const loadId = params.get('load');
    if (!loadId) return;
    let cancelled = false;
    (async () => {
      try {
        const [metaRes, audioRes] = await Promise.all([
          fetch(`/api/transcriptions/${encodeURIComponent(loadId)}`),
          fetch(`/api/transcriptions/${encodeURIComponent(loadId)}/audio`),
        ]);
        if (!metaRes.ok || !audioRes.ok) throw new Error('Not found');
        const meta = (await metaRes.json()) as {
          audioFile?: string;
          audioName?: string;
          transcription?: { utterances?: Utterance[] };
        };
        const audioBlob = await audioRes.blob();
        const ext = meta.audioFile?.match(/\.[^.]+$/)?.[0] ?? '.wav';
        const file = new File(
          [audioBlob],
          (meta.audioName ?? 'audio') + ext,
          { type: audioBlob.type || 'audio/wav' }
        );
        if (cancelled) return;
        handleFileChange(file);
        const list = meta.transcription?.utterances ?? [];
        renderUtterances(list);
        setJsonStatus(`JSON status: Loaded (${list.length} utterances)`);
        if (typeof window !== 'undefined')
          window.history.replaceState({}, '', '/');
      } catch {
        if (!cancelled) alert('Could not load transcription.');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [handleFileChange, renderUtterances]);

  const showEmptyRow = utterances.length === 0;

  return (
    <main className="main">
      <h1 className="title">Audio transcription test</h1>
      <p className="instruction">
        Edit the transcript while listening to the audio
      </p>

      <section className="audio-source">
        <h2>Audio source</h2>
        <div className="upload-area">
          <input
            type="file"
            id="audioFile"
            accept="audio/*"
            hidden
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) handleFileChange(f);
            }}
          />
          <button
            type="button"
            className="btn-upload"
            onClick={() =>
              document.getElementById('audioFile')?.click()
            }
          >
            Choose or drop audio file
          </button>
          <span className="file-name">{fileName}</span>
        </div>
        {audioUrl && (
          <a
            href={audioUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="link"
          >
            Open audio in new tab
          </a>
        )}
        <p id="jsonStatus" className="json-status">
          {jsonStatus}
        </p>
        <div className="api-test">
          <button
            type="button"
            className="btn-secondary"
            onClick={onTestOpenAi}
          >
            Test OpenAI Key
          </button>
          <button
            type="button"
            className="btn-secondary"
            onClick={onTestWhisper}
          >
            Test Whisper
          </button>
          <span className="openai-status">{openAiStatus}</span>
          <span className="openai-status">{whisperStatus}</span>
        </div>
        <div className="request-preview">
          <button
            type="button"
            className="btn-secondary"
            onClick={onShowRequest}
          >
            Show Whisper Request
          </button>
          {requestDisplay && (
            <pre className="request-display">{requestDisplay}</pre>
          )}
        </div>
      </section>

      {audioUrl && (
        <audio
          ref={audioRef}
          src={audioUrl}
          onLoadedMetadata={() => {
            if (audioRef.current)
              setTotalTime(formatTime(audioRef.current.duration));
          }}
          onTimeUpdate={() => {
            if (audioRef.current) {
              setCurrentTime(formatTime(audioRef.current.currentTime));
              setProgressPercent(
                (audioRef.current.currentTime / audioRef.current.duration) * 100
              );
            }
          }}
          onEnded={() => setPlaying(false)}
        />
      )}

      <section className="audio-player">
        <div className="player-controls">
          <button
            type="button"
            className="btn-play"
            onClick={() => {
              if (!audioRef.current) return;
              if (audioRef.current.paused) {
                audioRef.current.play();
                setPlaying(true);
              } else {
                audioRef.current.pause();
                setPlaying(false);
              }
            }}
          >
            {playing ? 'Pause' : 'Play'}
          </button>
          <button
            type="button"
            className="btn-small"
            onClick={() => {
              if (audioRef.current)
                audioRef.current.currentTime = Math.max(
                  0,
                  audioRef.current.currentTime - 2
                );
            }}
          >
            -2s
          </button>
          <button
            type="button"
            className="btn-small"
            onClick={() => {
              if (audioRef.current)
                audioRef.current.currentTime = Math.min(
                  audioRef.current.duration,
                  audioRef.current.currentTime + 2
                );
            }}
          >
            +2s
          </button>
          <span className="time-display">
            Time: {currentTime} / {totalTime}
          </span>
          <label>
            Speed:
            <select
              onChange={(e) => {
                if (audioRef.current)
                  audioRef.current.playbackRate = parseFloat(
                    e.target.value
                  );
              }}
            >
              <option value="0.5">0.5x</option>
              <option value="0.75">0.75x</option>
              <option value="1">1.0x</option>
              <option value="1.25">1.25x</option>
              <option value="1.5">1.5x</option>
              <option value="2">2.0x</option>
            </select>
          </label>
          <label className="volume-label">
            Volume
            <input
              type="range"
              min="0"
              max="100"
              defaultValue="100"
              onChange={(e) => {
                if (audioRef.current)
                  audioRef.current.volume =
                    Number(e.target.value) / 100;
              }}
            />
          </label>
          <span className="audio-status">{audioStatus}</span>
        </div>
        <div
          className="progress-bar"
          role="button"
          tabIndex={0}
          onClick={onProgressClick}
          onKeyDown={() => {}}
        >
          <div
            className="progress-fill"
            style={{ width: `${progressPercent}%` }}
          />
        </div>
      </section>

      <section className="utterances-section">
        <h2>Utterances table (editable)</h2>
        <p className="output-hint">
          Output column: transcription_final (JSON string)
        </p>
        <div className="table-wrapper">
          <table className="utterances-table">
            <thead>
              <tr>
                <th>speaker</th>
                <th>start_time</th>
                <th>end_time</th>
                <th>transcript</th>
                <th>emotion</th>
                <th>Language</th>
                <th>Locale</th>
                <th>Accent</th>
              </tr>
            </thead>
            <tbody ref={utterancesBodyRef}>
              {showEmptyRow && (
                <tr className="empty-row">
                  <td colSpan={8}>
                    Upload an audio file and click &quot;Transcribe&quot; to
                    generate utterances
                  </td>
                </tr>
              )}
              {utterances.map((u, i) => (
                <tr key={i}>
                  <td>
                    <input
                      type="text"
                      defaultValue={u.speaker ?? 'speaker_01'}
                      data-field="speaker"
                    />
                  </td>
                  <td>
                    <input
                      type="text"
                      defaultValue={String(u.start_time ?? '')}
                      data-field="start_time"
                    />
                  </td>
                  <td>
                    <input
                      type="text"
                      defaultValue={String(u.end_time ?? '')}
                      data-field="end_time"
                    />
                  </td>
                  <td>
                    <input
                      type="text"
                      defaultValue={u.transcript ?? ''}
                      data-field="transcript"
                    />
                  </td>
                  <td>
                    <input
                      type="text"
                      defaultValue={u.emotion ?? ''}
                      data-field="emotion"
                    />
                  </td>
                  <td>
                    <input
                      type="text"
                      defaultValue={u.language ?? 'en'}
                      data-field="language"
                    />
                  </td>
                  <td>
                    <input
                      type="text"
                      defaultValue={u.locale ?? 'en'}
                      data-field="locale"
                    />
                  </td>
                  <td>
                    <input
                      type="text"
                      defaultValue={u.accent ?? ''}
                      data-field="accent"
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="transcribe-actions">
          <button
            type="button"
            className="btn-transcribe"
            disabled={transcribeDisabled}
            onClick={onTranscribe}
          >
            Transcribe with Whisper
          </button>
          <span className="transcribe-status">{transcribeStatus}</span>
        </div>
        <div className="footer-actions">
          <label>
            <input type="checkbox" id="cannotJudge" /> Cannot judge
          </label>
          <button type="button" className="btn-save" onClick={onSave}>
            Save
          </button>
          <button
            type="button"
            className="btn-submit"
            onClick={() => {
              const list = getUtterancesFromTable();
              if (list.length === 0) {
                alert('No utterances to submit. Transcribe first.');
                return;
              }
              console.log('Submit payload:', {
                audio_name: audioFile?.name?.replace(/\.[^.]+$/, '') ?? 'unknown',
                utterances: list,
              });
              alert(
                'Submission logged to console. In production, this would POST to your API.'
              );
            }}
          >
            Submit
          </button>
        </div>
      </section>
    </main>
  );
}
