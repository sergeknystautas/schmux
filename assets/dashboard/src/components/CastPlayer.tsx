import { useRef, useEffect, useState, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { WebglAddon } from '@xterm/addon-webgl';
import '@xterm/xterm/css/xterm.css';

interface CastEvent {
  time: number;
  type: string;
  data: string;
}

interface CastHeader {
  width: number;
  height: number;
  timestamp?: number;
}

function parseCast(text: string): { header: CastHeader; events: CastEvent[] } {
  const lines = text.trim().split('\n');
  const header: CastHeader = JSON.parse(lines[0]);
  const events: CastEvent[] = [];
  for (let i = 1; i < lines.length; i++) {
    try {
      const arr = JSON.parse(lines[i]);
      events.push({ time: arr[0], type: arr[1], data: arr[2] });
    } catch {
      // skip malformed lines
    }
  }
  return { header, events };
}

type CastPlayerProps = {
  recordingId: string;
};

export default function CastPlayer({ recordingId }: CastPlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [events, setEvents] = useState<CastEvent[]>([]);
  const [header, setHeader] = useState<CastHeader | null>(null);
  const [cursor, setCursor] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Stable refs for the playback loop
  const cursorRef = useRef(cursor);
  const speedRef = useRef(speed);
  const playingRef = useRef(playing);
  cursorRef.current = cursor;
  speedRef.current = speed;
  playingRef.current = playing;
  const eventsRef = useRef(events);
  eventsRef.current = events;

  // Fetch and parse the .cast file
  useEffect(() => {
    (async () => {
      try {
        const id = encodeURIComponent(recordingId);
        // Ensure the compressed timelapse exists (creates it if needed).
        await fetch(`/api/timelapse/${id}/export`, { method: 'POST' });
        // Download the timelapse version (idle time removed).
        const resp = await fetch(`/api/timelapse/${id}/download?type=timelapse`);
        if (!resp.ok) throw new Error(`Failed to load recording: ${resp.status}`);
        const text = await resp.text();
        const { header: h, events: evts } = parseCast(text);
        setHeader(h);
        setEvents(evts);
      } catch (e) {
        setError(e instanceof Error ? e.message : 'Failed to load');
      } finally {
        setLoading(false);
      }
    })();
  }, [recordingId]);

  // Initialize terminal
  useEffect(() => {
    if (!containerRef.current || !header) return;

    const term = new Terminal({
      cols: header.width || 80,
      rows: header.height || 24,
      cursorBlink: false,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      allowProposedApi: true,
      disableStdin: true,
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#d4d4d4',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#ffffff',
      },
      scrollback: 5000,
      convertEol: true,
    });

    term.loadAddon(new WebLinksAddon());
    term.loadAddon(new Unicode11Addon());
    term.unicode.activeVersion = '11';

    term.open(containerRef.current);

    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      term.loadAddon(webgl);
    } catch {
      // canvas fallback
    }

    termRef.current = term;
    return () => {
      term.dispose();
      termRef.current = null;
    };
  }, [header]);

  // Playback loop
  const scheduleNext = useCallback(() => {
    const term = termRef.current;
    const evts = eventsRef.current;
    if (!term || !playingRef.current) return;

    const idx = cursorRef.current;
    if (idx >= evts.length) {
      setPlaying(false);
      return;
    }

    const event = evts[idx];
    if (event.type === 'o') {
      term.write(event.data);
    }

    const nextIdx = idx + 1;
    setCursor(nextIdx);
    cursorRef.current = nextIdx;

    if (nextIdx < evts.length) {
      const delay = Math.max(0, (evts[nextIdx].time - event.time) * 1000) / speedRef.current;
      // Cap per-frame delay to 2 seconds for a smoother experience
      timerRef.current = setTimeout(scheduleNext, Math.min(delay, 2000));
    } else {
      setPlaying(false);
    }
  }, []);

  useEffect(() => {
    if (playing) {
      scheduleNext();
    }
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [playing, scheduleNext]);

  const handlePlayPause = () => {
    if (cursor >= events.length && events.length > 0) {
      // Restart from beginning
      termRef.current?.reset();
      setCursor(0);
      cursorRef.current = 0;
      setPlaying(true);
    } else {
      setPlaying(!playing);
    }
  };

  const handleRestart = () => {
    if (timerRef.current) clearTimeout(timerRef.current);
    termRef.current?.reset();
    setCursor(0);
    cursorRef.current = 0;
    setPlaying(false);
  };

  const handleSeek = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (timerRef.current) clearTimeout(timerRef.current);
    const target = parseInt(e.target.value, 10);
    const term = termRef.current;
    if (!term) return;

    // Replay from start up to the target position
    term.reset();
    for (let i = 0; i < target && i < events.length; i++) {
      if (events[i].type === 'o') {
        term.write(events[i].data);
      }
    }
    setCursor(target);
    cursorRef.current = target;
    // If was playing, keep playing from new position
    if (playingRef.current) {
      setTimeout(scheduleNext, 0);
    }
  };

  const totalDuration = events.length > 0 ? events[events.length - 1].time : 0;
  const currentTime = cursor > 0 && cursor <= events.length ? events[cursor - 1].time : 0;

  const formatTime = (s: number) => {
    const m = Math.floor(s / 60);
    const sec = Math.floor(s % 60);
    return `${m}:${sec.toString().padStart(2, '0')}`;
  };

  if (loading) {
    return <div className="cast-player__loading">Loading recording...</div>;
  }

  if (error) {
    return <div className="cast-player__error">{error}</div>;
  }

  const finished = cursor >= events.length && events.length > 0;

  return (
    <div className="cast-player">
      <div className="cast-player__terminal" ref={containerRef} />
      <div className="cast-player__controls">
        <button
          className="cast-player__btn"
          onClick={handlePlayPause}
          title={finished ? 'Replay' : playing ? 'Pause' : 'Play'}
        >
          {finished ? (
            <svg
              viewBox="0 0 24 24"
              width="18"
              height="18"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <polyline points="1 4 1 10 7 10" />
              <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10" />
            </svg>
          ) : playing ? (
            <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
              <rect x="6" y="4" width="4" height="16" />
              <rect x="14" y="4" width="4" height="16" />
            </svg>
          ) : (
            <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
              <polygon points="5,3 19,12 5,21" />
            </svg>
          )}
        </button>

        <button className="cast-player__btn" onClick={handleRestart} title="Restart">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
            <rect x="4" y="4" width="3" height="16" />
            <polygon points="20,4 9,12 20,20" />
          </svg>
        </button>

        <input
          className="cast-player__scrubber"
          type="range"
          min={0}
          max={events.length}
          value={cursor}
          onChange={handleSeek}
        />

        <span className="cast-player__time">
          {formatTime(currentTime)} / {formatTime(totalDuration)}
        </span>

        <div className="cast-player__speed">
          {[1, 2, 4, 8].map((s) => (
            <button
              key={s}
              className={`cast-player__speed-btn${speed === s ? ' cast-player__speed-btn--active' : ''}`}
              onClick={() => setSpeed(s)}
            >
              {s}x
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
