import { useEffect, useRef, useState } from 'react';
import '../styles/logs.css';
import useLogsWebSocket from '../hooks/useLogsWebSocket';
import type { SpawnLogRecord } from '../lib/types.generated';

const SOURCES = [{ id: 'spawn', label: 'Spawn' }];

const STATUS_BADGE: Record<string, string> = {
  ok: 'badge--success',
  partial: 'badge--warning',
  failed: 'badge--danger',
};

export default function LogsPage() {
  const [source, setSource] = useState('spawn');
  const { records, connected } = useLogsWebSocket(source);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };

  // Auto-scroll to the newest line unless the user has scrolled up.
  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickToBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [records]);

  const copyAll = () =>
    navigator.clipboard.writeText(records.map((r) => JSON.stringify(r)).join('\n'));

  return (
    <div className="logs-page">
      <div className="logs-header">
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
        <span
          className={`status-pill ${connected ? 'status-pill--running' : 'status-pill--stopped'}`}
        >
          <span className="status-pill__dot" />
          {connected ? 'Live' : 'Disconnected'}
        </span>
        <button type="button" className="btn btn--sm" onClick={copyAll}>
          Copy all
        </button>
      </div>
      <div className="logs-body" ref={scrollRef} onScroll={onScroll}>
        {records.map((rec, i) => (
          <SpawnLogRow key={i} rec={rec} />
        ))}
      </div>
    </div>
  );
}

function SpawnLogRow({ rec }: { rec: SpawnLogRecord }) {
  const [expanded, setExpanded] = useState(false);
  const models = rec.targets ? Object.keys(rec.targets).join(', ') : rec.command || '';
  return (
    <div className={`logs-row status-${rec.status}`}>
      <div className="logs-row-head" onClick={() => setExpanded((v) => !v)}>
        <span className="logs-ts">{rec.ts}</span>
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
