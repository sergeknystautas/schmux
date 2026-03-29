import { useState, useEffect, useCallback } from 'react';
import {
  getOverlays,
  getSessions,
  scanOverlayFiles,
  addOverlayFiles,
  getErrorMessage,
} from '../lib/api';
import { useConfig } from '../contexts/ConfigContext';
import { useOverlay } from '../contexts/OverlayContext';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import type {
  OverlayInfo,
  OverlayPathInfo,
  OverlayScanCandidate,
  OverlayAddRequest,
  OverlayChangeEvent,
  WorkspaceResponse,
} from '../lib/types';

type AddFlowState =
  | { step: 'closed' }
  | { step: 'pick-workspace'; workspaces: WorkspaceResponse[] }
  | { step: 'scanning' }
  | {
      step: 'results';
      workspaceId: string;
      candidates: OverlayScanCandidate[];
      selected: Set<string>;
      customPath: string;
      customPaths: string[];
    }
  | { step: 'adding' };

type ViewTab = 'paths' | 'activity';

export default function OverlayPage() {
  const { config } = useConfig();
  const repos = config?.repos || [];
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert } = useModal();
  const { overlayEvents, overlayUnreadCount, clearOverlayEvents, markOverlaysRead } = useOverlay();

  const [view, setView] = useState<ViewTab>('paths');
  const [activeRepo, setActiveRepo] = useState(repos[0]?.name || '');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [allOverlays, setAllOverlays] = useState<OverlayInfo[]>([]);
  const [addFlow, setAddFlow] = useState<AddFlowState>({ step: 'closed' });

  // Sync activeRepo when repos list changes
  useEffect(() => {
    if (repos.length > 0 && !repos.find((r) => r.name === activeRepo)) {
      setActiveRepo(repos[0].name);
    }
  }, [repos, activeRepo]);

  const loadOverlays = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await getOverlays();
      setAllOverlays(data.overlays || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load overlay info'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadOverlays();
  }, [loadOverlays]);

  // Mark overlays as read when switching to activity tab
  const handleTabChange = (tab: ViewTab) => {
    setView(tab);
    if (tab === 'activity') {
      markOverlaysRead();
    }
  };

  const overlay = allOverlays.find((o) => o.repo_name === activeRepo) || null;

  // Group declared paths by source
  const builtinPaths = overlay?.declared_paths.filter((p) => p.source === 'builtin') || [];
  const repoPaths = overlay?.declared_paths.filter((p) => p.source !== 'builtin') || [];

  // --- Add flow handlers ---

  const handleStartAdd = async () => {
    const activeRepoConfig = repos.find((r) => r.name === activeRepo);
    const activeRepoUrl = activeRepoConfig?.url || '';
    try {
      const sessions = await getSessions();
      const workspaces = sessions.filter(
        (ws) => ws.repo === activeRepoUrl || ws.repo_name === activeRepo
      );
      if (workspaces.length === 0) {
        toastError('No workspaces found for this repo. Spawn a workspace first.');
        return;
      }
      if (workspaces.length === 1) {
        handleScan(workspaces[0].id);
      } else {
        setAddFlow({ step: 'pick-workspace', workspaces });
      }
    } catch (err) {
      alert('Load Failed', getErrorMessage(err, 'Failed to load workspaces'));
    }
  };

  const handleScan = async (workspaceId: string) => {
    if (!activeRepo) return;
    setAddFlow({ step: 'scanning' });
    try {
      const result = await scanOverlayFiles(workspaceId, activeRepo);
      const builtinPathSet = new Set(builtinPaths.map((p) => p.path));
      const filtered = result.candidates.filter((c) => !builtinPathSet.has(c.path));
      const selected = new Set<string>(filtered.filter((c) => c.detected).map((c) => c.path));
      setAddFlow({
        step: 'results',
        workspaceId,
        candidates: filtered,
        selected,
        customPath: '',
        customPaths: [],
      });
    } catch (err) {
      alert('Scan Failed', getErrorMessage(err, 'Failed to scan overlay files'));
      setAddFlow({ step: 'closed' });
    }
  };

  const handleToggleCandidate = (path: string) => {
    if (addFlow.step !== 'results') return;
    const next = new Set(addFlow.selected);
    if (next.has(path)) {
      next.delete(path);
    } else {
      next.add(path);
    }
    setAddFlow({ ...addFlow, selected: next });
  };

  const handleAddCustomPath = () => {
    if (addFlow.step !== 'results') return;
    const trimmed = addFlow.customPath.trim();
    if (!trimmed) return;
    if (trimmed.startsWith('/') || trimmed.startsWith('\\') || trimmed.includes('..')) {
      toastError('Invalid path: must be relative without ".." traversal');
      return;
    }
    if (addFlow.customPaths.includes(trimmed)) {
      toastError('Path already added');
      return;
    }
    setAddFlow({
      ...addFlow,
      customPaths: [...addFlow.customPaths, trimmed],
      customPath: '',
    });
  };

  const handleRemoveCustomPath = (path: string) => {
    if (addFlow.step !== 'results') return;
    setAddFlow({
      ...addFlow,
      customPaths: addFlow.customPaths.filter((p) => p !== path),
    });
  };

  const handleConfirmAdd = async () => {
    if (addFlow.step !== 'results' || !activeRepo) return;
    const paths = Array.from(addFlow.selected);
    const customPaths = addFlow.customPaths;
    if (paths.length === 0 && customPaths.length === 0) {
      toastError('Select at least one file or add a custom path');
      return;
    }
    const req: OverlayAddRequest = {
      workspace_id: addFlow.workspaceId,
      repo_name: activeRepo,
      paths,
      custom_paths: customPaths,
    };
    setAddFlow({ step: 'adding' });
    try {
      const result = await addOverlayFiles(req);
      if (result.success) {
        const count = result.registered?.length || 0;
        toastSuccess(`Added ${count} overlay file${count !== 1 ? 's' : ''}`);
      }
      setAddFlow({ step: 'closed' });
      loadOverlays();
    } catch (err) {
      alert('Add Overlay Failed', getErrorMessage(err, 'Failed to add overlay files'));
      setAddFlow({ step: 'closed' });
    }
  };

  const handleCancelAdd = () => {
    setAddFlow({ step: 'closed' });
  };

  const handleRepoTabChange = (repoName: string) => {
    setActiveRepo(repoName);
    setAddFlow({ step: 'closed' });
  };

  // --- Render ---

  if (loading && view === 'paths') {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading overlay info...</span>
      </div>
    );
  }

  if (error && view === 'paths') {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">!</div>
        <h3 className="empty-state__title">Error</h3>
        <p className="empty-state__description">{error}</p>
        <button className="btn btn--primary" onClick={loadOverlays}>
          Retry
        </button>
      </div>
    );
  }

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Overlay Files</h1>
        </div>
      </div>

      {/* View tabs: Paths | Activity */}
      <div className="overlay-tabs">
        <button
          className={`overlay-tab${view === 'paths' ? ' overlay-tab--active' : ''}`}
          onClick={() => handleTabChange('paths')}
        >
          Paths
        </button>
        <button
          className={`overlay-tab${view === 'activity' ? ' overlay-tab--active' : ''}`}
          onClick={() => handleTabChange('activity')}
        >
          Activity
          {overlayUnreadCount > 0 && (
            <span className="nav-badge nav-badge--danger" style={{ marginLeft: 6 }}>
              {overlayUnreadCount}
            </span>
          )}
        </button>
      </div>

      {view === 'paths' && (
        <>
          {/* Repo tabs */}
          {repos.length > 1 && (
            <div className="repo-tabs">
              {repos.map((repo) => (
                <button
                  key={repo.name}
                  className={`repo-tab${activeRepo === repo.name ? ' repo-tab--active' : ''}`}
                  data-testid="repo-tab"
                  aria-selected={activeRepo === repo.name}
                  onClick={() => handleRepoTabChange(repo.name)}
                >
                  {repo.name}
                </button>
              ))}
            </div>
          )}

          <div className="spawn-content">
            <p className="mb-lg text-muted">
              Overlay files are shared across all workspaces for this repo. Agent configs, secrets,
              and dotfiles are automatically copied to new workspaces and kept in sync.
            </p>

            <SectionHeader title="Auto-managed" />
            {builtinPaths.length === 0 ? (
              <p
                className="text-faint"
                style={{
                  fontSize: '0.875rem',
                  padding: 'var(--spacing-sm) 0',
                }}
              >
                No auto-managed overlay paths.
              </p>
            ) : (
              <div className="flex-col gap-xs">
                {builtinPaths.map((p) => (
                  <PathRow key={p.path} info={p} />
                ))}
              </div>
            )}

            <SectionHeader title="Repo-specific" />
            {repoPaths.length === 0 ? (
              <p
                className="text-faint"
                style={{
                  fontSize: '0.875rem',
                  padding: 'var(--spacing-sm) 0',
                }}
              >
                No repo-specific overlay files configured.
              </p>
            ) : (
              <div className="flex-col gap-xs">
                {repoPaths.map((p) => (
                  <PathRow key={p.path} info={p} showStatus />
                ))}
              </div>
            )}

            <div className="text-center mt-lg">
              <button
                className="btn btn--primary"
                onClick={handleStartAdd}
                disabled={addFlow.step !== 'closed'}
              >
                + Add files
              </button>
            </div>
          </div>
        </>
      )}

      {view === 'activity' && (
        <div className="spawn-content">
          <OverlayActivityFeed events={overlayEvents} onClear={clearOverlayEvents} />
        </div>
      )}

      {/* Add flow modal */}
      {addFlow.step !== 'closed' && (
        <div className="modal-overlay" onClick={handleCancelAdd}>
          <div className="modal modal--wide" onClick={(e) => e.stopPropagation()}>
            <div className="modal__header">
              <h2 className="modal__title">Add Overlay Files</h2>
              <button className="modal__close" onClick={handleCancelAdd}>
                x
              </button>
            </div>
            <div className="modal__body">
              {addFlow.step === 'pick-workspace' && (
                <WorkspacePicker
                  workspaces={addFlow.workspaces}
                  onSelect={(id) => handleScan(id)}
                />
              )}
              {addFlow.step === 'scanning' && (
                <div className="loading-state">
                  <div className="spinner"></div>
                  <span>Scanning workspace for overlay files...</span>
                </div>
              )}
              {addFlow.step === 'results' && (
                <ScanResults
                  candidates={addFlow.candidates}
                  selected={addFlow.selected}
                  customPath={addFlow.customPath}
                  customPaths={addFlow.customPaths}
                  onToggle={handleToggleCandidate}
                  onCustomPathChange={(v) =>
                    addFlow.step === 'results' && setAddFlow({ ...addFlow, customPath: v })
                  }
                  onAddCustomPath={handleAddCustomPath}
                  onRemoveCustomPath={handleRemoveCustomPath}
                />
              )}
              {addFlow.step === 'adding' && (
                <div className="loading-state">
                  <div className="spinner"></div>
                  <span>Adding overlay files...</span>
                </div>
              )}
            </div>
            {addFlow.step === 'results' && (
              <div className="modal__footer">
                <button className="btn" onClick={handleCancelAdd}>
                  Cancel
                </button>
                <button className="btn btn--primary" onClick={handleConfirmAdd}>
                  Confirm
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
}

// --- Activity feed components ---

function OverlayActivityFeed({
  events,
  onClear,
}: {
  events: OverlayChangeEvent[];
  onClear: () => void;
}) {
  if (events.length === 0) {
    return (
      <div className="overlay-activity">
        <p className="overlay-activity__empty">
          No overlay changes yet. Changes will appear here in real-time as files are synced across
          workspaces.
        </p>
      </div>
    );
  }

  return (
    <div className="overlay-activity">
      <div className="overlay-activity__toolbar">
        <button className="btn btn--sm" onClick={onClear}>
          Clear all
        </button>
      </div>
      {events.map((event, i) => (
        <OverlayEventCard key={`${event.timestamp}-${event.rel_path}-${i}`} event={event} />
      ))}
    </div>
  );
}

function OverlayEventCard({ event }: { event: OverlayChangeEvent }) {
  const [expanded, setExpanded] = useState(false);

  const timeStr = new Date(event.timestamp * 1000).toLocaleTimeString();

  return (
    <div className="overlay-event">
      <div className="overlay-event__header" onClick={() => setExpanded(!expanded)}>
        <div>
          <span className="overlay-event__file">{event.rel_path}</span>
          <div className="overlay-event__meta">
            <span>
              from <strong>{event.source_branch || event.source_workspace_id.slice(0, 8)}</strong>
            </span>
            <span>
              &rarr; {event.target_workspace_ids.length} workspace
              {event.target_workspace_ids.length !== 1 ? 's' : ''}
            </span>
          </div>
        </div>
        <div className="flex-row gap-sm">
          <span className="overlay-event__time">{timeStr}</span>
          <svg
            className={`overlay-event__chevron${expanded ? ' overlay-event__chevron--open' : ''}`}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <polyline points="9 18 15 12 9 6"></polyline>
          </svg>
        </div>
      </div>
      {expanded && event.unified_diff && (
        <div className="overlay-event__diff">
          <pre>
            {event.unified_diff.split('\n').map((line, j) => {
              let cls = 'overlay-diff-line';
              if (line.startsWith('+') && !line.startsWith('+++')) cls += ' overlay-diff-line--add';
              else if (line.startsWith('-') && !line.startsWith('---'))
                cls += ' overlay-diff-line--del';
              else if (line.startsWith('@@')) cls += ' overlay-diff-line--hunk';
              else if (line.startsWith('---') || line.startsWith('+++'))
                cls += ' overlay-diff-line--header';
              return (
                <div key={j} className={cls}>
                  {line}
                </div>
              );
            })}
          </pre>
        </div>
      )}
      {expanded && !event.unified_diff && (
        <div className="overlay-event__diff">
          <pre
            className="text-faint"
            style={{
              padding: 'var(--spacing-sm) var(--spacing-md)',
            }}
          >
            No diff available (new file or binary content)
          </pre>
        </div>
      )}
    </div>
  );
}

// --- Existing sub-components ---

function SectionHeader({ title }: { title: string }) {
  return (
    <div
      className="flex-row gap-sm text-muted"
      style={{
        margin: 'var(--spacing-lg) 0 var(--spacing-sm)',
        fontSize: '0.75rem',
        fontWeight: 600,
        textTransform: 'uppercase',
        letterSpacing: '0.05em',
      }}
    >
      <span>{title}</span>
      <div className="flex-1" style={{ height: '1px', background: 'var(--color-border)' }} />
    </div>
  );
}

function PathRow({ info, showStatus }: { info: OverlayPathInfo; showStatus?: boolean }) {
  const statusBadge =
    info.source === 'builtin' ? (
      <span className="badge badge--neutral">Built-in</span>
    ) : showStatus ? (
      <span className={`badge ${info.status === 'synced' ? 'badge--success' : 'badge--indicator'}`}>
        {info.status === 'synced' ? 'Synced' : 'Pending'}
      </span>
    ) : null;

  return (
    <div
      className="flex-row"
      style={{
        justifyContent: 'space-between',
        padding: 'var(--spacing-sm) var(--spacing-md)',
        borderRadius: 'var(--radius-md)',
        background: 'var(--color-surface)',
        border: '1px solid var(--color-border)',
        fontSize: '0.875rem',
      }}
    >
      <div className="flex-row gap-sm">
        {info.source === 'builtin' && <span className="text-success">&#10003;</span>}
        <code style={{ fontSize: '0.8125rem' }}>{info.path}</code>
      </div>
      <div className="flex-row gap-sm">{statusBadge}</div>
    </div>
  );
}

function WorkspacePicker({
  workspaces,
  onSelect,
}: {
  workspaces: WorkspaceResponse[];
  onSelect: (id: string) => void;
}) {
  const [selectedId, setSelectedId] = useState(workspaces[0]?.id || '');

  return (
    <div>
      <p className="mb-md text-muted">Select a workspace to scan for overlay file candidates.</p>
      <div className="form-group">
        <label className="form-group__label" htmlFor="overlay-workspace-select">
          Workspace
        </label>
        <select
          id="overlay-workspace-select"
          className="select"
          value={selectedId}
          onChange={(e) => setSelectedId(e.target.value)}
        >
          {workspaces.map((ws) => (
            <option key={ws.id} value={ws.id}>
              {ws.branch} ({ws.id.slice(0, 8)})
            </option>
          ))}
        </select>
      </div>
      <div className="mt-lg text-right">
        <button className="btn btn--primary" onClick={() => onSelect(selectedId)}>
          Scan
        </button>
      </div>
    </div>
  );
}

function ScanResults({
  candidates,
  selected,
  customPath,
  customPaths,
  onToggle,
  onCustomPathChange,
  onAddCustomPath,
  onRemoveCustomPath,
}: {
  candidates: OverlayScanCandidate[];
  selected: Set<string>;
  customPath: string;
  customPaths: string[];
  onToggle: (path: string) => void;
  onCustomPathChange: (value: string) => void;
  onAddCustomPath: () => void;
  onRemoveCustomPath: (path: string) => void;
}) {
  return (
    <div>
      {candidates.length === 0 && customPaths.length === 0 ? (
        <p className="text-faint">
          No overlay file candidates found. You can add custom paths below.
        </p>
      ) : (
        <div className="flex-col gap-xs mb-md">
          {candidates.map((c) => (
            <label
              key={c.path}
              className="flex-row gap-sm cursor-pointer"
              style={{
                padding: 'var(--spacing-xs) var(--spacing-sm)',
                borderRadius: 'var(--radius-md)',
                fontSize: '0.875rem',
              }}
            >
              <input
                type="checkbox"
                checked={selected.has(c.path)}
                onChange={() => onToggle(c.path)}
              />
              <code className="flex-1" style={{ fontSize: '0.8125rem' }}>
                {c.path}
              </code>
              <span className="text-faint" style={{ fontSize: '0.75rem' }}>
                {formatSize(c.size)}
              </span>
              {c.detected && (
                <span className="badge badge--neutral" style={{ fontSize: '0.65rem' }}>
                  detected
                </span>
              )}
            </label>
          ))}
        </div>
      )}

      {customPaths.length > 0 && (
        <div className="mb-md">
          <SectionHeader title="Custom paths" />
          <div className="flex-col gap-xs">
            {customPaths.map((p) => (
              <div
                key={p}
                className="flex-row gap-sm"
                style={{
                  padding: 'var(--spacing-xs) var(--spacing-sm)',
                  fontSize: '0.875rem',
                }}
              >
                <code className="flex-1" style={{ fontSize: '0.8125rem' }}>
                  {p}
                </code>
                <button
                  className="btn btn--sm btn--danger"
                  onClick={() => onRemoveCustomPath(p)}
                  style={{ padding: '2px 8px', fontSize: '0.75rem' }}
                >
                  Remove
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'flex-end' }}>
        <div className="form-group flex-1">
          <label className="form-group__label" htmlFor="overlay-custom-path">
            Custom path
          </label>
          <input
            id="overlay-custom-path"
            className="input"
            type="text"
            value={customPath}
            onChange={(e) => onCustomPathChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                onAddCustomPath();
              }
            }}
            placeholder="e.g., .env.local"
          />
        </div>
        <button
          className="btn btn--sm mb-0"
          onClick={onAddCustomPath}
          disabled={!customPath.trim()}
        >
          Add
        </button>
      </div>
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
