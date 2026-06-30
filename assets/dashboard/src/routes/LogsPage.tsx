import { useEffect, useRef, useState } from 'react';
import '../styles/logs.css';
import useLogsWebSocket from '../hooks/useLogsWebSocket';
import useFenceLogWebSocket from '../hooks/useFenceLogWebSocket';
import useOneshotLogWebSocket from '../hooks/useOneshotLogWebSocket';
import { useSessions } from '../contexts/SessionsContext';
import { parseFenceLine } from '../lib/fenceLog';
import type { SpawnLogRecord, OneshotLogRecord } from '../lib/types.generated';

const SOURCES = [
  { id: 'spawn', label: 'Spawn' },
  { id: 'fence', label: 'Fence' },
  { id: 'oneshot', label: 'Oneshot' },
];

const STATUS_BADGE: Record<string, string> = {
  ok: 'badge--success',
  partial: 'badge--warning',
  failed: 'badge--danger',
};

type SourceProps = { source: string; setSource: (s: string) => void };

export default function LogsPage() {
  const [source, setSource] = useState('spawn');
  return (
    <div className="logs-page">
      {source === 'fence' ? (
        <FenceLogView source={source} setSource={setSource} />
      ) : source === 'oneshot' ? (
        <OneshotLogView source={source} setSource={setSource} />
      ) : (
        <SpawnLogView source={source} setSource={setSource} />
      )}
    </div>
  );
}

// Header for every source: title on the left; the connection pill and the
// source select right-aligned on the same line.
function LogsHeader({ source, setSource, connected }: SourceProps & { connected: boolean }) {
  return (
    <div className="app-header logs-header">
      <div className="app-header__info">
        <h1 className="app-header__meta">Logs</h1>
      </div>
      <div className="app-header__actions">
        <ConnPill connected={connected} />
        <select
          className="select"
          value={source}
          onChange={(e) => setSource(e.target.value)}
          aria-label="Log source"
        >
          {SOURCES.map((s) => (
            <option key={s.id} value={s.id}>
              {s.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

function SpawnLogView({ source, setSource }: SourceProps) {
  const { records, connected } = useLogsWebSocket('spawn');
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickToBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [records]);

  return (
    <>
      <LogsHeader source={source} setSource={setSource} connected={connected} />
      <div className="logs-body" ref={scrollRef} onScroll={onScroll}>
        {records.map((rec, i) => (
          <SpawnLogRow key={i} rec={rec} />
        ))}
      </div>
    </>
  );
}

function FenceLogView({ source, setSource }: SourceProps) {
  const { workspaces } = useSessions();
  const [sessionId, setSessionId] = useState<string | null>(null);
  const { lines, connected } = useFenceLogWebSocket(sessionId);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const fenced = workspaces.flatMap((ws) =>
    (ws.sessions ?? [])
      .filter((sx) => sx.fence)
      .map((sx) => ({ id: sx.id, label: `${ws.label || ws.branch} — ${sx.nickname || sx.id}` }))
  );

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickToBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [lines]);

  return (
    <>
      <LogsHeader source={source} setSource={setSource} connected={connected} />
      <div className="logs-subheader">
        <select
          className="select"
          value={sessionId ?? ''}
          onChange={(e) => setSessionId(e.target.value || null)}
          aria-label="Fenced session"
        >
          <option value="">Pick a fenced session…</option>
          {fenced.map((f) => (
            <option key={f.id} value={f.id}>
              {f.label}
            </option>
          ))}
        </select>
      </div>
      {sessionId && (
        <div className="logs-body" ref={scrollRef} onScroll={onScroll}>
          {lines.map((line, i) => {
            const f = parseFenceLine(line);
            return (
              <div key={i} className="logs-fence-row">
                <span className="logs-ts">{f.time}</span>
                <span className={`badge logs-fence-badge--${f.kind}`}>{f.kind}</span>
                <span className="logs-fence-msg">{f.message}</span>
              </div>
            );
          })}
        </div>
      )}
    </>
  );
}

function ConnPill({ connected }: { connected: boolean }) {
  return (
    <span className={`status-pill ${connected ? 'status-pill--running' : 'status-pill--stopped'}`}>
      <span className="status-pill__dot" />
      {connected ? 'Live' : 'Disconnected'}
    </span>
  );
}

function SpawnLogRow({ rec }: { rec: SpawnLogRecord }) {
  const [expanded, setExpanded] = useState(false);
  const models = rec.targets ? Object.keys(rec.targets).join(', ') : rec.command || '';
  return (
    <div className={`logs-row status-${rec.status}`}>
      <div className="logs-row-head" onClick={() => setExpanded((v) => !v)}>
        <span className="logs-ts">{new Date(rec.ts).toISOString().slice(11, 19)}</span>
        <span className="logs-repo">{rec.repo}</span>
        <span className="logs-branch">{rec.branch}</span>
        {rec.workspace_id && <span className="logs-ws">{rec.workspace_id}</span>}
        <span className="logs-models">{models}</span>
        <span className={`badge ${STATUS_BADGE[rec.status] ?? 'badge--neutral'}`}>
          {rec.status}
        </span>
      </div>
      {expanded && (
        <div className="logs-row-body">
          {rec.prompt && <pre className="logs-prompt">{rec.prompt}</pre>}
          {rec.prompt && (
            <button
              type="button"
              className="btn btn--sm"
              onClick={() => navigator.clipboard.writeText(rec.prompt ?? '')}
            >
              Copy prompt
            </button>
          )}
          <ul className="logs-results">
            {rec.results?.map((r, i) => (
              <li key={i}>
                {r.target || r.command}: {r.error ? `failed — ${r.error}` : `ok (${r.session_id})`}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function OneshotLogView({ source, setSource }: SourceProps) {
  const { records, connected } = useOneshotLogWebSocket();
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickToBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [records]);

  return (
    <>
      <LogsHeader source={source} setSource={setSource} connected={connected} />
      <div className="logs-body" ref={scrollRef} onScroll={onScroll}>
        {records.map((rec, i) => (
          <OneshotLogRow key={i} rec={rec} />
        ))}
      </div>
    </>
  );
}

function OneshotLogRow({ rec }: { rec: OneshotLogRecord }) {
  const [expanded, setExpanded] = useState(false);
  const status = rec.ok ? 'ok' : 'failed';
  return (
    <div className={`logs-row status-${status}`}>
      <div className="logs-row-head" onClick={() => setExpanded((v) => !v)}>
        <span className="logs-ts">{new Date(rec.ts).toISOString().slice(11, 19)}</span>
        <span className="logs-oneshot-transport">{rec.transport}</span>
        <span className="logs-oneshot-model">{rec.model}</span>
        {rec.workspace && <span className="logs-ws">{rec.workspace}</span>}
        <span className={`badge logs-oneshot-badge logs-oneshot-badge--${rec.type}`}>
          {rec.type}
        </span>
        <span className={`badge ${STATUS_BADGE[status]}`}>{status}</span>
      </div>
      {expanded && (
        <div className="logs-row-body">
          <ul className="logs-results">
            <li>type: {rec.type}</li>
            <li>transport: {rec.transport}</li>
            <li>model: {rec.model}</li>
            <li>prompt: {rec.prompt_chars ?? 0} chars</li>
            <li>elapsed: {rec.elapsed_ms ?? 0} ms</li>
            {rec.error && <li className="logs-oneshot-error">error: {rec.error}</li>}
          </ul>
        </div>
      )}
    </div>
  );
}
