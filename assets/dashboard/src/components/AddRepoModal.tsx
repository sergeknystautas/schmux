import { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { scanLocalRepos, probeRepo, getConfig, updateConfig, getErrorMessage } from '../lib/api';
import type { LocalRepo } from '../lib/api';
import { useConfig } from '../contexts/ConfigContext';
import useFocusTrap from '../hooks/useFocusTrap';

interface AddRepoModalProps {
  onClose: () => void;
}

/** Derive a short repo name from a URL or path (last segment, minus .git). */
function deriveRepoName(url: string): string {
  const trimmed = url.replace(/\/+$/, '');
  const lastSegment = trimmed.split('/').pop() || trimmed;
  return lastSegment.replace(/\.git$/, '');
}

type Phase = 'input' | 'probing' | 'error';

export default function AddRepoModal({ onClose }: AddRepoModalProps) {
  const navigate = useNavigate();
  const { reloadConfig } = useConfig();
  const modalRef = useRef<HTMLDivElement>(null);
  const [inputValue, setInputValue] = useState('');
  const [phase, setPhase] = useState<Phase>('input');
  const [errorMessage, setErrorMessage] = useState('');
  const [localRepos, setLocalRepos] = useState<LocalRepo[]>([]);
  const [configuredUrls, setConfiguredUrls] = useState<Set<string>>(new Set());
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [selectedRemoteUrl, setSelectedRemoteUrl] = useState<string | null>(null);
  const abortRef = useRef(false);

  useFocusTrap(modalRef, true);

  // Scan local repos and fetch configured repos on first focus
  const scanned = useRef(false);
  const handleInputFocus = useCallback(() => {
    setShowSuggestions(true);
    if (!scanned.current) {
      scanned.current = true;
      scanLocalRepos()
        .then(setLocalRepos)
        .catch(() => {});
      getConfig()
        .then((cfg) => {
          const urls = new Set((cfg.repos || []).map((r) => r.url));
          // Also add paths that match URLs (local repos use path as URL)
          (cfg.repos || []).forEach((r) => urls.add(r.url));
          setConfiguredUrls(urls);
        })
        .catch(() => {});
    }
  }, []);

  // Close suggestions when clicking outside
  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (modalRef.current && !modalRef.current.contains(e.target as Node)) {
        setShowSuggestions(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  // Escape key closes modal
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleSelectRepo = (repo: LocalRepo) => {
    const url = repo.remote_url || repo.path;
    setInputValue(url);
    setSelectedRemoteUrl(repo.remote_url || null);
    setShowSuggestions(false);
    setErrorMessage('');
    setPhase('input');
  };

  const resolvedUrl = inputValue.trim();

  const handleSubmit = async () => {
    if (!resolvedUrl) return;

    setPhase('probing');
    setErrorMessage('');
    abortRef.current = false;

    try {
      const result = await probeRepo(resolvedUrl);

      if (abortRef.current) return;

      if (!result.accessible) {
        setPhase('error');
        setErrorMessage(result.error || 'Repository is not accessible');
        return;
      }

      // Read-modify-write config — only add if not already configured
      const config = await getConfig();

      if (abortRef.current) return;

      const alreadyExists = (config.repos || []).some((r) => r.url === resolvedUrl);
      if (!alreadyExists) {
        const repoName = deriveRepoName(resolvedUrl);
        const newRepo = {
          name: repoName,
          url: resolvedUrl,
          ...(result.vcs ? { vcs: result.vcs } : {}),
        };
        const existingRepos = (config.repos || []).map((r) => ({
          name: r.name,
          url: r.url,
          ...(r.vcs ? { vcs: r.vcs } : {}),
        }));

        await updateConfig({ repos: [...existingRepos, newRepo] } as Parameters<
          typeof updateConfig
        >[0]);
        await reloadConfig();
      }

      if (abortRef.current) return;

      onClose();
      navigate('/spawn', {
        state: { repo: resolvedUrl, branch: result.default_branch },
      });
    } catch (err) {
      if (abortRef.current) return;
      setPhase('error');
      setErrorMessage(getErrorMessage(err, 'Failed to add repository'));
    }
  };

  const handleCancel = () => {
    if (phase === 'probing') {
      abortRef.current = true;
      setPhase('input');
      setErrorMessage('');
    } else {
      onClose();
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && phase === 'input' && resolvedUrl) {
      e.preventDefault();
      handleSubmit();
    }
  };

  const filteredRepos = inputValue.trim()
    ? localRepos.filter(
        (r) =>
          r.name.toLowerCase().includes(inputValue.toLowerCase()) ||
          r.path.toLowerCase().includes(inputValue.toLowerCase()) ||
          (r.remote_url && r.remote_url.toLowerCase().includes(inputValue.toLowerCase()))
      )
    : localRepos;

  return (
    <div
      className="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-workspace-title"
    >
      <div ref={modalRef} className="modal modal--wide" onClick={(e) => e.stopPropagation()}>
        <div className="modal__header">
          <h2 className="modal__title" id="add-workspace-title">
            Add Repository
          </h2>
        </div>
        <div className="modal__body" style={{ overflow: 'visible', maxHeight: 'none' }}>
          <div className="form-group">
            <label className="form-group__label" htmlFor="repo-input">
              Clone from
            </label>
            <div style={{ position: 'relative' }}>
              <input
                id="repo-input"
                role="combobox"
                aria-expanded={showSuggestions && filteredRepos.length > 0}
                aria-autocomplete="list"
                className="input"
                type="text"
                value={inputValue}
                onChange={(e) => {
                  setInputValue(e.target.value);
                  setSelectedRemoteUrl(null);
                  setErrorMessage('');
                  if (phase === 'error') setPhase('input');
                  setShowSuggestions(true);
                }}
                onFocus={handleInputFocus}
                onKeyDown={handleKeyDown}
                placeholder="Repository URL or local path"
                disabled={phase === 'probing'}
                autoFocus
              />
              {showSuggestions && filteredRepos.length > 0 && phase !== 'probing' && (
                <ul
                  role="listbox"
                  style={{
                    position: 'absolute',
                    top: '100%',
                    left: 0,
                    right: 0,
                    zIndex: 10,
                    maxHeight: '300px',
                    overflowY: 'auto',
                    margin: 0,
                    padding: 0,
                    listStyle: 'none',
                    backgroundColor: 'var(--color-bg-secondary, #1e1e1e)',
                    border: '1px solid var(--color-border)',
                    borderRadius: 'var(--radius-sm)',
                    marginTop: '4px',
                  }}
                >
                  {filteredRepos.map((repo) => {
                    const isConfigured =
                      configuredUrls.has(repo.path) ||
                      (repo.remote_url ? configuredUrls.has(repo.remote_url) : false);
                    return (
                      <li
                        key={repo.path}
                        role="option"
                        aria-selected={false}
                        aria-disabled={isConfigured}
                        style={{
                          padding: 'var(--spacing-sm) var(--spacing-md)',
                          cursor: isConfigured ? 'default' : 'pointer',
                          borderBottom: '1px solid var(--color-border)',
                          opacity: isConfigured ? 0.4 : 1,
                        }}
                        onMouseDown={(e) => {
                          e.preventDefault();
                          if (!isConfigured) handleSelectRepo(repo);
                        }}
                      >
                        <div
                          style={{
                            display: 'flex',
                            justifyContent: 'space-between',
                            alignItems: 'center',
                          }}
                        >
                          <strong>{repo.name}</strong>
                          <span className="text-muted" style={{ fontSize: '0.8rem' }}>
                            {isConfigured ? 'already added' : repo.vcs}
                          </span>
                        </div>
                        <div className="text-muted" style={{ fontSize: '0.85rem' }}>
                          {repo.path}
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
            <p
              className="text-muted"
              style={{ fontSize: '0.85rem', marginTop: 'var(--spacing-xs)' }}
            >
              Each workspace gets its own isolated copy — your original stays untouched.
            </p>
          </div>

          {selectedRemoteUrl && phase !== 'probing' && (
            <p
              className="text-muted"
              style={{ fontSize: '0.85rem', marginTop: 'var(--spacing-sm)' }}
            >
              {selectedRemoteUrl}
            </p>
          )}

          {phase === 'probing' && (
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-sm)',
                marginTop: 'var(--spacing-md)',
              }}
            >
              <div className="spinner" />
              <span>Checking repository access...</span>
            </div>
          )}

          {phase === 'error' && errorMessage && (
            <p className="text-error" style={{ marginTop: 'var(--spacing-md)' }}>
              {errorMessage}
            </p>
          )}
        </div>
        <div className="modal__footer">
          <button className="btn" onClick={handleCancel}>
            Cancel
          </button>
          {phase !== 'probing' && (
            <button className="btn btn--primary" onClick={handleSubmit} disabled={!resolvedUrl}>
              Add
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
