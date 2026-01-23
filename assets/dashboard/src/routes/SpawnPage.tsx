import React, { useEffect, useMemo, useRef, useState, useCallback } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { getConfig, spawnSessions, getErrorMessage, suggestBranch } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useRequireConfig, useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import useLocalStorage from '../hooks/useLocalStorage';
import type { RepoResponse, RunTargetResponse, SpawnResult } from '../lib/types';
import { WORKSPACE_EXPANDED_KEY } from '../lib/constants';

const PROMPT_TEXTAREA_STYLE: React.CSSProperties = {
  width: '100%',
  height: '420px',
  resize: 'vertical',
  border: 'none',
  outline: 'none',
  boxShadow: 'none',
  padding: 'var(--spacing-md)',
  borderRadius: 'var(--radius-lg) var(--radius-lg) 0 0',
};


export default function SpawnPage() {
  useRequireConfig();
  const [screen, setScreen] = useState<'form' | 'confirm'>('form');
  const [repos, setRepos] = useState<RepoResponse[]>([]);
  const [promptableTargets, setPromptableTargets] = useState<RunTargetResponse[]>([]);
  const [commandTargets, setCommandTargets] = useState<RunTargetResponse[]>([]);
  const [selectedCommand, setSelectedCommand] = useState('');
  const [spawnMode, setSpawnMode] = useState<'promptable' | 'command'>('promptable');
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('');
  const [newRepoName, setNewRepoName] = useState('');
  const [prompt, setPrompt] = useState('');
  const [nickname, setNickname] = useState('');
  const [reviewing, setReviewing] = useState(false);
  const [prefillWorkspaceId, setPrefillWorkspaceId] = useState('');
  const [resolvedWorkspaceId, setResolvedWorkspaceId] = useState('');
  const prefillApplied = useRef(false);
  const [loading, setLoading] = useState(true);
  const [configError, setConfigError] = useState('');
  const [results, setResults] = useState<SpawnResult[] | null>(null);
  const [spawning, setSpawning] = useState(false);
  const [searchParams] = useSearchParams();
  const { error: toastError } = useToast();
  const { workspaces, loading: sessionsLoading, refresh } = useSessions();
  const { config, getRepoName } = useConfig();

  // Use useLocalStorage for last-used values (with cross-tab sync)
  const [lastRepo, setLastRepo] = useLocalStorage<string>('last-repo', '');
  const [lastTargets, setLastTargets] = useLocalStorage<Record<string, number>>('last-targets', {});

  const isMounted = useRef(true);
  const inExistingWorkspace = !!resolvedWorkspaceId;

  // Get branch suggest target from config
  const branchSuggestTarget = config?.branch_suggest?.target || '';

  useEffect(() => {
    return () => {
      isMounted.current = false;
    };
  }, []);

  // Load config and data
  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setConfigError('');
      try {
        const cfg = await getConfig();
        if (!active) return;
        setRepos(cfg.repos || []);

        const promptableItems = (cfg.run_targets || []).filter(t => t.type === 'promptable');
        const commandItems = (cfg.run_targets || []).filter(t => t.type === 'command');
        setPromptableTargets(promptableItems);
        setCommandTargets(commandItems);

      } catch (err) {
        if (!active) return;
        setConfigError(getErrorMessage(err, 'Failed to load config'));
      } finally {
        if (active) setLoading(false);
      }
    };

    load();
    return () => { active = false; };
  }, []);

  // Handle URL prefill
  useEffect(() => {
    if (prefillApplied.current) return;
    const workspaceId = searchParams.get('workspace_id');
    if (!workspaceId) return;
    setPrefillWorkspaceId(workspaceId);

    const urlRepo = searchParams.get('repo');
    const urlBranch = searchParams.get('branch');
    let prefillRepo = urlRepo;
    let prefillBranch = urlBranch;

    if ((!prefillRepo || !prefillBranch) && sessionsLoading) {
      return;
    }

    let workspaceFound = false;
    if (!prefillRepo || !prefillBranch) {
      const workspace = workspaces.find((ws) => ws.id === workspaceId);
      if (workspace) {
        workspaceFound = true;
        prefillRepo = prefillRepo || workspace.repo;
        prefillBranch = prefillBranch || workspace.branch;
      }
    }

    if (prefillRepo && prefillRepo !== repo) setRepo(prefillRepo);
    if (prefillBranch && prefillBranch !== branch) setBranch(prefillBranch);

    if (prefillRepo && prefillBranch) {
      prefillApplied.current = true;
      setResolvedWorkspaceId(workspaceId);
    } else if (workspaceFound) {
      prefillApplied.current = true;
      setResolvedWorkspaceId(workspaceId);
    } else if (!workspaceId) {
      setResolvedWorkspaceId('');
    } else {
      setResolvedWorkspaceId('');
    }
  }, [searchParams, workspaces, sessionsLoading, repo, branch]);

  // Initialize from last-used values (only if repo not already set by URL prefill)
  useEffect(() => {
    if (inExistingWorkspace) return; // Don't use last-values when spawning into existing workspace
    if (!repo && lastRepo && repos.length > 0) {
      // Check if lastRepo still exists
      const stillExists = repos.some(r => r.url === lastRepo);
      if (stillExists) {
        setRepo(lastRepo);
      }
    }
  }, [repos, lastRepo, repo, inExistingWorkspace]);

  type PromptableListItem = {
    name: string;
    label: string;
  };

  const promptableList = useMemo<PromptableListItem[]>(() => {
    return promptableTargets.map((target) => ({
      name: target.name,
      label: target.name,
    }));
  }, [promptableTargets]);

  // Initialize target counts from last-used values (only once, after promptableList is loaded)
  const [initialized, setInitialized] = useState(false);
  const [targetCounts, setTargetCounts] = useState<Record<string, number>>({});

  useEffect(() => {
    if (initialized || promptableList.length === 0) return;

    if (Object.keys(lastTargets).length > 0) {
      const filtered: Record<string, number> = {};
      promptableList.forEach((item) => {
        if (lastTargets[item.name] !== undefined) {
          filtered[item.name] = lastTargets[item.name];
        }
      });
      setTargetCounts(filtered);
    }
    setInitialized(true);
  }, [promptableList, lastTargets, initialized]);

  // Ensure all items are in targetCounts
  useEffect(() => {
    setTargetCounts((current) => {
      const next = { ...current };
      let changed = false;
      promptableList.forEach((item) => {
        if (next[item.name] === undefined) {
          next[item.name] = 0;
          changed = true;
        }
      });
      Object.keys(next).forEach((name) => {
        if (!promptableList.find((item) => item.name === name)) {
          delete next[name];
          changed = true;
        }
      });
      return changed ? next : current;
    });
  }, [promptableList]);

  const totalPromptableCount = useMemo(() => {
    return Object.values(targetCounts).reduce((sum, count) => sum + count, 0);
  }, [targetCounts]);

  const updateTargetCount = (name: string, delta: number) => {
    setTargetCounts((current) => {
      const next = Math.max(0, Math.min(10, (current[name] || 0) + delta));
      setLastTargets((currentTargets) => {
        const currentCount = currentTargets[name] || 0;
        const newCount = Math.max(0, Math.min(10, currentCount + delta));
        return { ...currentTargets, [name]: newCount };
      });
      return { ...current, [name]: next };
    });
  };

  const generateBranchName = useCallback(async (promptText: string) => {
    if (!promptText.trim()) {
      return null;
    }
    try {
      const result = await suggestBranch({ prompt: promptText });
      return result;
    } catch (err) {
      console.error('Failed to suggest branch:', err);
      return null;
    }
  }, []);

  const validateForm = () => {
    if (!repo) {
      toastError('Please select a repository');
      return false;
    }
    if (repo === '__new__' && !newRepoName.trim()) {
      toastError('Please enter a repository name');
      return false;
    }
    if (spawnMode === 'promptable') {
      if (totalPromptableCount === 0) {
        toastError('Please select at least one target');
        return false;
      }
      if (!prompt.trim()) {
        toastError('Please enter a prompt');
        return false;
      }
    }
    if (spawnMode === 'command' && !selectedCommand) {
      toastError('Please select a command');
      return false;
    }
    return true;
  };

  const handleNext = () => {
    if (!validateForm()) return;

    // Save to localStorage
    setLastRepo(repo);
    // lastTargets is already updated via updateTargetCount

    if (spawnMode === 'command') {
      setNickname('');
    }

    if (inExistingWorkspace || spawnMode !== 'promptable' || !prompt.trim()) {
      if (!inExistingWorkspace) {
        setBranch('main');
      }
      setScreen('confirm');
      return;
    }

    if (!branchSuggestTarget) {
      setBranch('main');
      setScreen('confirm');
      return;
    }

    setReviewing(true);
    generateBranchName(prompt).then((result) => {
      if (!isMounted.current) return;
      if (result) {
        setBranch(result.branch || 'main');
        setNickname(result.nickname || '');
      } else {
        toastError('Branch suggestion failed. Using "main".');
        setBranch('main');
        setNickname('');
      }
      setScreen('confirm');
      setReviewing(false);
    });
  };

  const handleBack = () => {
    setScreen('form');
  };

  const handleSpawn = async () => {
    const selectedTargets: Record<string, number> = {};

    if (spawnMode === 'command') {
      selectedTargets[selectedCommand] = 1;
    } else {
      Object.entries(targetCounts).forEach(([name, count]) => {
        if (count > 0) selectedTargets[name] = count;
      });
    }

    const actualRepo = repo === '__new__' ? `local:${newRepoName.trim()}` : repo;
    const actualBranch = inExistingWorkspace ? branch : (branch || 'main');

    setSpawning(true);

    try {
      const response = await spawnSessions({
        repo: actualRepo,
        branch: actualBranch,
        prompt: spawnMode === 'promptable' ? prompt : '',
        nickname: nickname.trim(),
        targets: selectedTargets,
        workspace_id: prefillWorkspaceId || ''
      });
      setResults(response);
      refresh(true);

      const workspaceIds = [...new Set(response.filter(r => !r.error).map(r => r.workspace_id).filter(Boolean))] as string[];
      let expanded: Record<string, boolean> = {};
      try {
        expanded = JSON.parse(localStorage.getItem(WORKSPACE_EXPANDED_KEY) || '{}') as Record<string, boolean>;
      } catch (err) {
        console.warn('Failed to parse workspace expanded state:', err);
        expanded = {};
      }
      let changed = false;
      workspaceIds.forEach(id => {
        if (expanded[id] !== true) {
          expanded[id] = true;
          changed = true;
        }
      });
      if (changed) {
        localStorage.setItem(WORKSPACE_EXPANDED_KEY, JSON.stringify(expanded));
      }
    } catch (err) {
      toastError(`Failed to spawn: ${getErrorMessage(err, 'Unknown error')}`);
    } finally {
      setSpawning(false);
    }
  };

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading configuration...</span>
      </div>
    );
  }

  if (configError) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Failed to load config</h3>
        <p className="empty-state__description">{configError}</p>
      </div>
    );
  }

  if (results) {
    const successCount = results.filter((r) => !r.error).length;
    const errorCount = results.filter((r) => r.error).length;

    return (
      <>
        <div className="page-header">
          <h1 className="page-header__title">
            Spawn Sessions{inExistingWorkspace ? ` into workspace: ${prefillWorkspaceId}` : ''}
          </h1>
        </div>
        <div>
          <h2 style={{ marginBottom: 'var(--spacing-lg)' }}>Results</h2>
          {successCount > 0 ? (
            <div className="results-panel" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="results-panel__title">Successfully spawned {successCount} session(s)</div>
              {results.filter((r) => !r.error).map((r, index) => (
                <div className="results-panel__item results-panel__item--success" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }} key={r.session_id}>
                  <div>
                    <span className="badge badge--primary" style={{ marginRight: 'var(--spacing-sm)' }}>{index + 1}</span>
                    <span className="mono">{r.workspace_id}</span>
                    <span style={{ color: 'var(--color-text-muted)', margin: '0 var(--spacing-sm)' }}>·</span>
                    <span>{r.target}</span>
                    {r.nickname && <span style={{ color: 'var(--color-text-muted)', margin: '0 var(--spacing-sm)' }}>·</span>}
                    {r.nickname && <span style={{ fontStyle: 'italic', color: 'var(--color-text-muted)' }}>{r.nickname}</span>}
                  </div>
                  <Link to={`/sessions/${r.session_id}`} className="btn btn--sm">View</Link>
                </div>
              ))}
            </div>
          ) : null}
          {errorCount > 0 ? (
            <div className="results-panel">
              <div className="results-panel__title text-error">{errorCount} error(s)</div>
              {results.filter((r) => r.error).map((r) => (
                <div className="results-panel__item results-panel__item--error" key={`${r.target}-${r.error}`}>
                  <div><strong>{r.target}:</strong> {r.error}</div>
                </div>
              ))}
            </div>
          ) : null}
        </div>
        <div style={{ marginTop: 'var(--spacing-lg)' }}>
          <Link to="/sessions" className="btn btn--primary">Back to Sessions</Link>
        </div>
      </>
    );
  }

  // Confirmation screen
  if (screen === 'confirm') {
    return (
      <>
        <div className="page-header">
          <h1 className="page-header__title">
            Spawn Sessions{inExistingWorkspace ? ` into workspace: ${prefillWorkspaceId}` : ''}
          </h1>
        </div>

        <div className="card">
          <div className="card__body">
            <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Repository</h3>
            <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
              <span className="metadata-field__value">
                {repo === '__new__' ? `New repository: ${newRepoName}` : getRepoName(repo)}
              </span>
            </div>

            {spawnMode === 'promptable' && (
              <>
                <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Prompt</h3>
                <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <span
                    className="metadata-field__value"
                    style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
                  >
                    {prompt}
                  </span>
                </div>

                <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Targets</h3>
                <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <span className="metadata-field__value">
                    {promptableList
                      .filter((item) => (targetCounts[item.name] || 0) > 0)
                      .map((item) => {
                        const count = targetCounts[item.name] || 0;
                        return `${item.label} ×${count}`;
                      })
                      .join(', ')}
                  </span>
                </div>
              </>
            )}

            {spawnMode === 'command' && (
              <>
                <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Command</h3>
                <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <span className="metadata-field__value">{selectedCommand}</span>
                </div>
              </>
            )}

            <div className="form-group">
              <label className="form-group__label">Branch</label>
              {inExistingWorkspace ? (
                <div className="metadata-field">
                  <span className="metadata-field__value">{repo === '__new__' ? 'main' : branch}</span>
                </div>
              ) : (
                <input
                  type="text"
                  className="input"
                  value={branch}
                  onChange={(event) => setBranch(event.target.value)}
                  required
                />
              )}
            </div>

            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="form-group__label">Nickname</label>
              {inExistingWorkspace ? (
                <div className="metadata-field">
                  <span className="metadata-field__value">{nickname || '—'}</span>
                </div>
              ) : (
                <>
                  <input
                    type="text"
                    className="input"
                    placeholder="e.g., 'Fix login bug', 'Refactor auth flow'"
                    maxLength={100}
                    value={nickname}
                    onChange={(event) => setNickname(event.target.value)}
                  />
                  {!branchSuggestTarget && (
                    <div className="banner banner--info" style={{ margin: 'var(--spacing-sm) 0', fontSize: '0.875rem' }}>
                      Auto-suggested branch names are disabled. Enable in config to use suggestions.
                    </div>
                  )}
                </>
              )}
            </div>
          </div>
        </div>

        <div style={{ marginTop: 'var(--spacing-lg)', display: 'flex', gap: 'var(--spacing-sm)' }}>
          <button className="btn" onClick={handleBack} disabled={spawning}>
            Back
          </button>
          <button
            className="btn btn--primary"
            onClick={handleSpawn}
            disabled={spawning}
            style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}
          >
            {spawning ? (
              <>
                <span className="spinner spinner--small"></span>
                Spawning...
              </>
            ) : 'Spawn'}
          </button>
        </div>
      </>
    );
  }

  // Form screen
  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">
          Spawn Sessions{inExistingWorkspace ? ` into workspace: ${prefillWorkspaceId}` : ''}
        </h1>
      </div>

      {/* Mode + Repository on same line */}
      <div className="card" style={{ marginBottom: 'var(--spacing-md)' }}>
        <div className="card__body" style={{ display: 'flex', gap: 'var(--spacing-md)', alignItems: 'flex-end' }}>
          <div style={{ flex: '0 0 auto' }}>
            <label className="form-group__label">Mode</label>
            <div className="button-group">
              <button
                type="button"
                className={`btn${spawnMode === 'promptable' ? ' btn--primary' : ''}`}
                onClick={() => setSpawnMode('promptable')}
              >
                Promptable
              </button>
              <button
                type="button"
                className={`btn${spawnMode === 'command' ? ' btn--primary' : ''}`}
                onClick={() => {
                  setSpawnMode('command');
                  setNickname('');
                }}
                disabled={commandTargets.length === 0}
              >
                Command
              </button>
            </div>
          </div>

          <div style={{ flex: 1 }}>
            <label htmlFor="repo" className="form-group__label">Repository</label>
            <select
              id="repo"
              className="select"
              required
              value={repo}
              onChange={(event) => {
                setRepo(event.target.value);
                if (event.target.value !== '__new__') {
                  setNewRepoName('');
                }
              }}
              disabled={inExistingWorkspace}
            >
              <option value="">Select repository...</option>
              {repos.map((item) => (
                <option key={item.url} value={item.url}>{item.name}</option>
              ))}
              <option value="__new__">+ Create New Repository</option>
            </select>

            {repo === '__new__' && (
              <div style={{ marginTop: 'var(--spacing-sm)' }}>
                <input
                  type="text"
                  id="newRepoName"
                  className="input"
                  value={newRepoName}
                  onChange={(event) => setNewRepoName(event.target.value)}
                  placeholder="Repository name"
                  required
                  disabled={inExistingWorkspace}
                />
              </div>
            )}
          </div>
        </div>

      </div>

      {/* Prompt area - big and centered */}
      <div className="card" style={{ marginBottom: 'var(--spacing-md)', padding: '0' }}>
        {spawnMode === 'promptable' ? (
          <textarea
            className="textarea"
            style={PROMPT_TEXTAREA_STYLE}
            placeholder="Describe the task you want the targets to work on..."
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
          />
        ) : (
          <div style={{ padding: 'var(--spacing-md)' }}>
            <label htmlFor="command" className="form-group__label">Command</label>
            <select
              id="command"
              className="select"
              required
              value={selectedCommand}
              onChange={(event) => setSelectedCommand(event.target.value)}
            >
              <option value="">Select command...</option>
              {commandTargets.map((cmd) => (
                <option key={cmd.name} value={cmd.name}>
                  {cmd.name}
                </option>
              ))}
            </select>
          </div>
        )}
      </div>

      {/* Agent selection - compact horizontal chips */}
      {spawnMode === 'promptable' && promptableList.length > 0 && (
        <div className="card" style={{ marginBottom: 'var(--spacing-md)' }}>
          <div className="card__body">
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--spacing-sm)', alignItems: 'center' }}>
              {promptableList.map((item) => {
                const count = targetCounts[item.name] || 0;
                return (
                  <div
                    key={item.name}
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 'var(--spacing-xs)',
                      border: '1px solid var(--color-border)',
                      borderRadius: 'var(--radius-sm)',
                      padding: 'var(--spacing-xs)',
                      backgroundColor: count > 0 ? 'var(--color-accent)' : 'var(--color-surface-alt)',
                    }}
                  >
                    <span style={{ fontSize: '0.875rem' }}>
                      {item.label}
                    </span>
                    <button
                      type="button"
                      className="btn"
                      onClick={() => updateTargetCount(item.name, -1)}
                      disabled={count === 0}
                      style={{
                        padding: '2px 16px',
                        fontSize: '0.75rem',
                        minHeight: '24px',
                        minWidth: '32px',
                        lineHeight: '1',
                        backgroundColor: count > 0 ? 'rgba(255,255,255,0.2)' : 'var(--color-surface)',
                        color: count > 0 ? 'white' : 'var(--color-text)',
                        border: 'none',
                        borderRadius: 'var(--radius-sm)'
                      }}
                    >
                      −
                    </button>
                    <span style={{ fontSize: '0.875rem', minWidth: '16px', textAlign: 'center' }}>
                      {count}
                    </span>
                    <button
                      type="button"
                      className="btn"
                      onClick={() => updateTargetCount(item.name, 1)}
                      style={{
                        padding: '2px 16px',
                        fontSize: '0.75rem',
                        minHeight: '24px',
                        minWidth: '32px',
                        lineHeight: '1',
                        backgroundColor: count > 0 ? 'rgba(255,255,255,0.2)' : 'var(--color-surface)',
                        color: count > 0 ? 'white' : 'var(--color-text)',
                        border: 'none',
                        borderRadius: 'var(--radius-sm)'
                      }}
                    >
                      +
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}

      <div style={{ marginTop: 'var(--spacing-lg)' }}>
        <button
          className="btn btn--primary"
          onClick={handleNext}
          disabled={reviewing}
          style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}
        >
          {reviewing ? (
            <>
              <span className="spinner spinner--small"></span>
              Reviewing...
            </>
          ) : 'Review'}
        </button>
      </div>
    </>
  );
}
