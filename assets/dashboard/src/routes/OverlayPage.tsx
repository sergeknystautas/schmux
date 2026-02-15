import { useState, useEffect, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  getOverlays,
  getSessions,
  scanOverlayFiles,
  addOverlayFiles,
  getErrorMessage,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import type {
  OverlayInfo,
  OverlayPathInfo,
  OverlayScanCandidate,
  OverlayAddRequest,
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

export default function OverlayPage() {
  const { repoName } = useParams<{ repoName: string }>();
  const { success: toastSuccess, error: toastError } = useToast();

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [overlay, setOverlay] = useState<OverlayInfo | null>(null);
  const [addFlow, setAddFlow] = useState<AddFlowState>({ step: 'closed' });

  const loadOverlay = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await getOverlays();
      const found = data.overlays.find((o) => o.repo_name === repoName) || null;
      setOverlay(found);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load overlay info'));
    } finally {
      setLoading(false);
    }
  }, [repoName]);

  useEffect(() => {
    loadOverlay();
  }, [loadOverlay]);

  // Group declared paths by source
  const builtinPaths = overlay?.declared_paths.filter((p) => p.source === 'builtin') || [];
  const repoPaths = overlay?.declared_paths.filter((p) => p.source !== 'builtin') || [];

  // --- Add flow handlers ---

  const handleStartAdd = async () => {
    try {
      const sessions = await getSessions();
      const workspaces = sessions.filter((ws) => ws.repo_name === repoName || ws.repo === repoName);
      if (workspaces.length === 0) {
        toastError('No workspaces found for this repo. Spawn a workspace first.');
        return;
      }
      if (workspaces.length === 1) {
        // Skip picker — go straight to scan
        handleScan(workspaces[0].id);
      } else {
        setAddFlow({ step: 'pick-workspace', workspaces });
      }
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to load workspaces'));
    }
  };

  const handleScan = async (workspaceId: string) => {
    if (!repoName) return;
    setAddFlow({ step: 'scanning' });
    try {
      const result = await scanOverlayFiles(workspaceId, repoName);
      const selected = new Set<string>(
        result.candidates.filter((c) => c.detected).map((c) => c.path)
      );
      setAddFlow({
        step: 'results',
        workspaceId,
        candidates: result.candidates,
        selected,
        customPath: '',
        customPaths: [],
      });
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to scan overlay files'));
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
    // Validate: reject absolute paths, path traversal, and backslashes
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
    if (addFlow.step !== 'results' || !repoName) return;
    const paths = Array.from(addFlow.selected);
    const customPaths = addFlow.customPaths;
    if (paths.length === 0 && customPaths.length === 0) {
      toastError('Select at least one file or add a custom path');
      return;
    }
    const req: OverlayAddRequest = {
      workspace_id: addFlow.workspaceId,
      repo_name: repoName,
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
      loadOverlay();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to add overlay files'));
      setAddFlow({ step: 'closed' });
    }
  };

  const handleCancelAdd = () => {
    setAddFlow({ step: 'closed' });
  };

  // --- Render ---

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading overlay info...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">!</div>
        <h3 className="empty-state__title">Error</h3>
        <p className="empty-state__description">{error}</p>
        <button className="btn btn--primary" onClick={loadOverlay}>
          Retry
        </button>
      </div>
    );
  }

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Overlay Files{repoName ? ` \u2014 ${repoName}` : ''}</h1>
        </div>
        <div className="app-header__actions">
          <Link to="/" className="btn">
            Back
          </Link>
        </div>
      </div>

      <div className="spawn-content">
        {/* Description */}
        <p style={{ marginBottom: 'var(--spacing-lg)', color: 'var(--color-text-muted)' }}>
          Overlay files are shared across all workspaces for this repo. Agent configs, secrets, and
          dotfiles are automatically copied to new workspaces and kept in sync.
        </p>

        {/* Auto-managed section */}
        <SectionHeader title="Auto-managed" />
        {builtinPaths.length === 0 ? (
          <p
            style={{
              color: 'var(--color-text-faint)',
              fontSize: '0.875rem',
              padding: 'var(--spacing-sm) 0',
            }}
          >
            No auto-managed overlay paths.
          </p>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-xs)' }}>
            {builtinPaths.map((p) => (
              <PathRow key={p.path} info={p} />
            ))}
          </div>
        )}

        {/* Repo-specific section */}
        <SectionHeader title="Repo-specific" />
        {repoPaths.length === 0 ? (
          <p
            style={{
              color: 'var(--color-text-faint)',
              fontSize: '0.875rem',
              padding: 'var(--spacing-sm) 0',
            }}
          >
            No repo-specific overlay files configured.
          </p>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-xs)' }}>
            {repoPaths.map((p) => (
              <PathRow key={p.path} info={p} showStatus />
            ))}
          </div>
        )}

        {/* Add button */}
        <div style={{ textAlign: 'center', marginTop: 'var(--spacing-lg)' }}>
          <button
            className="btn btn--primary"
            onClick={handleStartAdd}
            disabled={addFlow.step !== 'closed'}
          >
            + Add files
          </button>
        </div>
      </div>

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

// --- Sub-components ---

function SectionHeader({ title }: { title: string }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--spacing-sm)',
        margin: 'var(--spacing-lg) 0 var(--spacing-sm)',
        color: 'var(--color-text-muted)',
        fontSize: '0.75rem',
        fontWeight: 600,
        textTransform: 'uppercase',
        letterSpacing: '0.05em',
      }}
    >
      <span>{title}</span>
      <div style={{ flex: 1, height: '1px', background: 'var(--color-border)' }} />
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
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: 'var(--spacing-sm) var(--spacing-md)',
        borderRadius: 'var(--radius-md)',
        background: 'var(--color-surface)',
        border: '1px solid var(--color-border)',
        fontSize: '0.875rem',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}>
        {info.source === 'builtin' && (
          <span style={{ color: 'var(--color-success)' }}>&#10003;</span>
        )}
        <code style={{ fontSize: '0.8125rem' }}>{info.path}</code>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}>
        {statusBadge}
      </div>
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
      <p style={{ marginBottom: 'var(--spacing-md)', color: 'var(--color-text-muted)' }}>
        Select a workspace to scan for overlay file candidates.
      </p>
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
      <div style={{ marginTop: 'var(--spacing-lg)', textAlign: 'right' }}>
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
        <p style={{ color: 'var(--color-text-faint)' }}>
          No overlay file candidates found. You can add custom paths below.
        </p>
      ) : (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 'var(--spacing-xs)',
            marginBottom: 'var(--spacing-md)',
          }}
        >
          {candidates.map((c) => (
            <label
              key={c.path}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-sm)',
                padding: 'var(--spacing-xs) var(--spacing-sm)',
                cursor: 'pointer',
                borderRadius: 'var(--radius-md)',
                fontSize: '0.875rem',
              }}
            >
              <input
                type="checkbox"
                checked={selected.has(c.path)}
                onChange={() => onToggle(c.path)}
              />
              <code style={{ flex: 1, fontSize: '0.8125rem' }}>{c.path}</code>
              <span style={{ color: 'var(--color-text-faint)', fontSize: '0.75rem' }}>
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

      {/* Custom paths already added */}
      {customPaths.length > 0 && (
        <div style={{ marginBottom: 'var(--spacing-md)' }}>
          <SectionHeader title="Custom paths" />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-xs)' }}>
            {customPaths.map((p) => (
              <div
                key={p}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--spacing-sm)',
                  padding: 'var(--spacing-xs) var(--spacing-sm)',
                  fontSize: '0.875rem',
                }}
              >
                <code style={{ flex: 1, fontSize: '0.8125rem' }}>{p}</code>
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

      {/* Custom path input */}
      <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'flex-end' }}>
        <div className="form-group" style={{ flex: 1 }}>
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
          className="btn btn--sm"
          onClick={onAddCustomPath}
          disabled={!customPath.trim()}
          style={{ marginBottom: 0 }}
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
