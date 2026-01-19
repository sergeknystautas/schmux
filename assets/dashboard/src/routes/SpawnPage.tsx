import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { getConfig, spawnSessions, detectTools, getVariants } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useRequireConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import type { DetectTool, RepoResponse, RunTargetResponse, SpawnResult, VariantResponse } from '../lib/types';

const STEPS = ['Repository', 'Targets', 'Review'];
const TOTAL_STEPS = STEPS.length;

export default function SpawnPage() {
  useRequireConfig();
  const [currentStep, setCurrentStep] = useState(1);
  const [repos, setRepos] = useState<RepoResponse[]>([]);
  const [promptableTargets, setPromptableTargets] = useState<RunTargetResponse[]>([]);
  const [commandTargets, setCommandTargets] = useState<RunTargetResponse[]>([]);
  const [detectedTools, setDetectedTools] = useState<DetectTool[]>([]);
  const [availableVariants, setAvailableVariants] = useState<VariantResponse[]>([]);
  const [targetCounts, setTargetCounts] = useState<Record<string, number>>({});
  const [selectedCommand, setSelectedCommand] = useState('');
  const [spawnMode, setSpawnMode] = useState<'promptable' | 'command' | null>(null); // 'promptable' | 'command' | null
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('main');
  const [newRepoName, setNewRepoName] = useState('');
  const [prompt, setPrompt] = useState('');
  const [nickname, setNickname] = useState('');
  const [prefillWorkspaceId, setPrefillWorkspaceId] = useState('');
  const prefillApplied = useRef(false);
  const [loading, setLoading] = useState(true);
  const [configError, setConfigError] = useState('');
  const [results, setResults] = useState<SpawnResult[] | null>(null);
  const [spawning, setSpawning] = useState(false);
  const [showTargetError, setShowTargetError] = useState(false);
  const [searchParams] = useSearchParams();
  const { error: toastError } = useToast();
  const { workspaces, loading: sessionsLoading, refresh } = useSessions();

  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setConfigError('');
      try {
        const config = await getConfig();
        if (!active) return;
        setRepos(config.repos || []);

        const promptableItems = (config.run_targets || []).filter(t => t.type === 'promptable' && t.source !== 'detected');
        const commandItems = (config.run_targets || []).filter(t => t.type === 'command' && t.source !== 'detected');
        setPromptableTargets(promptableItems);
        setCommandTargets(commandItems);

        const toolResult = await detectTools();
        if (!active) return;
        setDetectedTools(toolResult.tools || []);

        const variantResult = await getVariants();
        if (!active) return;
        setAvailableVariants(variantResult.variants || []);
      } catch (err) {
        if (!active) return;
        setConfigError(err.message || 'Failed to load config');
      } finally {
        if (active) setLoading(false);
      }
    };

    load();
    return () => {
      active = false;
    };
  }, []);

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
    }
  }, [searchParams, workspaces, sessionsLoading, repo, branch]);

  type PromptableListItem = {
    name: string;
    label: string;
    kind: 'tool' | 'variant' | 'run_target';
    configured?: boolean;
    count: number;
  };

  const promptableList = useMemo<PromptableListItem[]>(() => {
    const items = [
      ...detectedTools.map((tool) => ({
        name: tool.name,
        label: tool.name,
        kind: 'tool' as const
      })),
      ...availableVariants.filter((variant) => variant.configured).map((variant) => ({
        name: variant.name,
        label: variant.display_name,
        kind: 'variant' as const,
        configured: variant.configured
      })),
      ...promptableTargets.map((target) => ({
        name: target.name,
        label: target.name,
        kind: 'run_target' as const
      }))
    ];
    return items.map((item) => ({
      ...item,
      count: targetCounts[item.name] || 0
    }));
  }, [detectedTools, availableVariants, promptableTargets, targetCounts]);

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
      return { ...current, [name]: next };
    });
    setShowTargetError(false);
  };

  const applyPreset = (preset: 'each' | 'review' | 'reset') => {
    if (preset === 'each') {
      const next = {};
      promptableList.forEach((item) => {
        next[item.name] = 1;
      });
      setTargetCounts(next);
    } else if (preset === 'review') {
      const next = {};
      promptableList.forEach((item) => {
        next[item.name] = item.name.includes('claude') ? 2 : 0;
      });
      setTargetCounts(next);
    } else if (preset === 'reset') {
      const next = {};
      promptableList.forEach((item) => {
        next[item.name] = 0;
      });
      setTargetCounts(next);
    }
    setShowTargetError(false);
  };

  const validateStep = () => {
    if (currentStep === 1) {
      if (!repo) {
        toastError('Please select a repository');
        return false;
      }
      if (repo === '__new__' && !newRepoName.trim()) {
        toastError('Please enter a repository name');
        return false;
      }
      if (repo !== '__new__' && !branch) {
        toastError('Please enter a branch');
        return false;
      }
    }

    if (currentStep === 2) {
      if (!spawnMode) {
        toastError('Please select a mode (Promptable or Command)');
        return false;
      }
      if (spawnMode === 'promptable') {
        if (totalPromptableCount === 0) {
          toastError('Please select at least one target');
          setShowTargetError(true);
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
    }

    return true;
  };

  const nextStep = () => {
    if (!validateStep()) return;
    setCurrentStep((step) => Math.min(TOTAL_STEPS, step + 1));
  };

  const prevStep = () => {
    setCurrentStep((step) => Math.max(1, step - 1));
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

    // Determine the actual repo URL and branch to send
    // For "__new__", we'll send "local:{name}" and always use "main" branch
    const actualRepo = repo === '__new__' ? `local:${newRepoName.trim()}` : repo;
    const actualBranch = repo === '__new__' ? 'main' : branch;

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

      // Clear collapsed state for reused workspace IDs so new sessions are visible
      const workspaceIds = [...new Set(response.filter(r => !r.error).map(r => r.workspace_id).filter(Boolean))] as string[];
      const expandedKey = 'schmux:workspace-expanded';
      const expanded = JSON.parse(localStorage.getItem(expandedKey) || '{}') as Record<string, boolean>;
      let changed = false;
      workspaceIds.forEach(id => {
        if (expanded[id] !== true) {
          expanded[id] = true;
          changed = true;
        }
      });
      if (changed) {
        localStorage.setItem(expandedKey, JSON.stringify(expanded));
      }
    } catch (err) {
      toastError(`Failed to spawn: ${err.message}`);
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
          <h1 className="page-header__title">Spawn Sessions</h1>
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
                  {(r.prompt || repo || branch || r.workspace_id) && (
                    <div style={{ marginTop: 'var(--spacing-sm)', fontSize: 'var(--font-size-sm)', color: 'var(--color-text-muted)' }}>
                      {repo && <div>Repo: {repo === '__new__' ? `New repository: ${newRepoName}` : repo}</div>}
                      {branch && <div>Branch: {repo === '__new__' ? 'main' : branch}</div>}
                      {r.workspace_id && <div>Workspace: {r.workspace_id}</div>}
                      {r.prompt && (
                        <div style={{ marginTop: 'var(--spacing-sm)' }}>
                          <div>Prompt:</div>
                          <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontFamily: 'var(--font-mono)' }}>{r.prompt}</div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          ) : null}
        </div>
        <div className="wizard__actions" style={{ marginTop: 'var(--spacing-lg)' }}>
          <Link to="/sessions" className="btn btn--primary">Back to Sessions</Link>
        </div>
      </>
    );
  }

  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">Spawn Sessions</h1>
      </div>

      <div className="wizard">
        <div className="wizard__steps">
          {STEPS.map((label, index) => {
            const step = index + 1;
            const className = step === currentStep
              ? 'wizard__step wizard__step--active'
              : step < currentStep
                ? 'wizard__step wizard__step--completed'
                : 'wizard__step';
            return (
              <div className={className} data-step={step} key={step}>
                {step}. {label}
              </div>
            );
          })}
        </div>

        <div className="wizard__content">
          {currentStep === 1 && (
            <div className="wizard-step-content" data-step="1">
              <div className="form-group">
                <label htmlFor="repo" className="form-group__label">Repository</label>
                <select
                  id="repo"
                  className="select"
                  required
                  value={repo}
                  onChange={(event) => {
                    setRepo(event.target.value);
                    // Clear new repo name when switching away from __new__
                    if (event.target.value !== '__new__') {
                      setNewRepoName('');
                    }
                  }}
                  disabled={!!prefillWorkspaceId}
                >
                  <option value="">Select repository...</option>
                  {repos.map((item) => (
                    <option key={item.url} value={item.url}>{item.name}</option>
                  ))}
                  <option value="__new__">+ Create New Repository</option>
                </select>
              </div>

              {repo === '__new__' ? (
                <div className="form-group">
                  <label htmlFor="newRepoName" className="form-group__label">Repository Name</label>
                  <input
                    type="text"
                    id="newRepoName"
                    className="input"
                    value={newRepoName}
                    onChange={(event) => setNewRepoName(event.target.value)}
                    placeholder="e.g., myproject"
                    required
                    disabled={!!prefillWorkspaceId}
                  />
                  <p className="form-group__hint">A local repository will be created with an initial "main" branch. You can add a remote later.</p>
                </div>
              ) : (
                <div className="form-group">
                  <label htmlFor="branch" className="form-group__label">Branch</label>
                  <input
                    type="text"
                    id="branch"
                    className="input"
                    value={branch}
                    onChange={(event) => setBranch(event.target.value)}
                    required
                    disabled={!!prefillWorkspaceId}
                  />
                  <p className="form-group__hint">The branch to checkout for this workspace</p>
                </div>
              )}

              {prefillWorkspaceId ? (
                <div className="banner banner--info" style={{ display: 'flex' }}>
                  Spawning into existing workspace: {prefillWorkspaceId}
                </div>
              ) : null}
            </div>
          )}

          {currentStep === 2 && (
            <div className="wizard-step-content" data-step="2">
              {/* Mode Selection */}
              <div className="form-group">
                <label className="form-group__label">Mode</label>
                <div className="button-group">
                  <button
                    type="button"
                    className={`btn${spawnMode === 'promptable' ? ' btn--primary' : ''}`}
                    onClick={() => {
                      if (promptableList.length > 0) {
                        setSpawnMode('promptable');
                        setShowTargetError(false);
                      }
                    }}
                    disabled={promptableList.length === 0}
                  >
                    Promptable
                  </button>
                  <button
                    type="button"
                    className={`btn${spawnMode === 'command' ? ' btn--primary' : ''}`}
                    onClick={() => {
                      if (commandTargets.length > 0) setSpawnMode('command');
                    }}
                    disabled={commandTargets.length === 0}
                  >
                    Command
                  </button>
                </div>
                {spawnMode === 'promptable' && promptableList.length === 0 && (
                  <p className="form-group__hint">No promptable targets available. Add run targets or install a detected tool.</p>
                )}
                {spawnMode === 'command' && commandTargets.length === 0 && (
                  <p className="form-group__hint">No commands configured. Add some in the configuration page first.</p>
                )}
              </div>

              {/* Content that shows AFTER selection */}
              {spawnMode === 'promptable' && promptableList.length > 0 && (
                <>
                  {/* Presets - first, so you can quickly set then adjust */}
                  <div className="form-group">
                    <label className="form-group__label">Quick Select</label>
                    <div className="preset-buttons">
                      <button type="button" className="btn btn--sm" onClick={() => applyPreset('each')}>1 Each</button>
                      <button type="button" className="btn btn--sm" onClick={() => applyPreset('review')}>Review Squad</button>
                      <button type="button" className="btn btn--sm" onClick={() => applyPreset('reset')}>Reset</button>
                    </div>
                  </div>

                  {/* Run Target Grid */}
                  <div className="run-target-grid">
                    {promptableList.map((item) => (
                      <div className="run-target-card" key={item.name}>
                        <div className="run-target-card__label">{item.label}</div>
                        {item.kind === 'variant' && !item.configured && (
                          <div className="run-target-card__note">Configure in Settings</div>
                        )}
                        <div className="run-target-card__control">
                          <button
                            className="run-target-card__btn"
                            onClick={() => updateTargetCount(item.name, -1)}
                            aria-label={`Decrease ${item.label} count`}
                            disabled={item.kind === 'variant' && !item.configured}
                          >
                            −
                          </button>
                          <span className="run-target-card__count">{item.count}</span>
                          <button
                            className="run-target-card__btn"
                            onClick={() => updateTargetCount(item.name, 1)}
                            aria-label={`Increase ${item.label} count`}
                            disabled={item.kind === 'variant' && !item.configured}
                          >
                            +
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>

                  {showTargetError && (
                    <div className="form-group__error">
                      Please select at least one target
                    </div>
                  )}

                  {/* Separator before prompt */}
                  <hr style={{ border: 'none', borderTop: '1px solid var(--color-border)', margin: 'var(--spacing-xl) 0' }} />

                  {/* Prompt field */}
                  <div className="form-group">
                    <label htmlFor="prompt" className="form-group__label">Prompt</label>
                    <textarea
                      id="prompt"
                      className="textarea"
                      rows={8}
                      placeholder="Describe the task you want the targets to work on..."
                      value={prompt}
                      onChange={(event) => setPrompt(event.target.value)}
                    />
                    <p className="form-group__hint">This prompt will be sent to all spawned promptable targets.</p>
                  </div>
                </>
              )}

              {spawnMode === 'command' && commandTargets.length > 0 && (
                <div className="form-group">
                  <label htmlFor="command" className="form-group__label">Command</label>
                  <select
                    id="command"
                    className="select"
                    required
                    value={selectedCommand}
                    onChange={(event) => setSelectedCommand(event.target.value)}
                  >
                    <option value="">Select a command...</option>
                    {commandTargets.map((cmd) => (
                      <option key={cmd.name} value={cmd.name}>
                        {cmd.name}
                      </option>
                    ))}
                  </select>
                  <p className="form-group__hint">
                    This command will run directly in the workspace without a prompt.
                  </p>
                </div>
              )}
            </div>
          )}

          {currentStep === 3 && (
            <div className="wizard-step-content" data-step="3">
              <h2 style={{ marginBottom: 'var(--spacing-lg)' }}>Review and Spawn</h2>
              <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
                <div className="card__body">
                  <div className="metadata-field">
                    <span className="metadata-field__label">Repository</span>
                    <span className="metadata-field__value">
                      {repo === '__new__' ? `New repository: ${newRepoName}` : repo}
                    </span>
                  </div>
                  <div className="metadata-field">
                    <span className="metadata-field__label">Branch</span>
                    <span className="metadata-field__value">{repo === '__new__' ? 'main' : branch}</span>
                  </div>
                  <div className="metadata-field">
                    <span className="metadata-field__label">Workspace</span>
                    <span className="metadata-field__value">{prefillWorkspaceId || 'New workspace'}</span>
                  </div>
                  {spawnMode === 'promptable' ? (
                    <>
                      <div className="metadata-field">
                        <span className="metadata-field__label">Targets</span>
                        <span className="metadata-field__value">
                          {Object.entries(targetCounts)
                            .filter(([_, count]) => count > 0)
                            .map(([name, count]) => `${count}× ${name}`)
                            .join(', ')}
                        </span>
                      </div>
                      <div className="metadata-field">
                        <span className="metadata-field__label">Prompt</span>
                        <span className="metadata-field__value" style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{prompt}</span>
                      </div>
                    </>
                  ) : (
                    <div className="metadata-field">
                      <span className="metadata-field__label">Command</span>
                      <span className="metadata-field__value mono">{commandTargets.find(c => c.name === selectedCommand)?.command}</span>
                    </div>
                  )}
                </div>
              </div>

              {/* Nickname field */}
              <div className="form-group">
                <label htmlFor="nickname" className="form-group__label">Nickname <span className="text-muted">(optional)</span></label>
                <input
                  type="text"
                  id="nickname"
                  className="input"
                  placeholder="e.g., 'Fix login bug', 'Refactor auth flow'"
                  maxLength={100}
                  value={nickname}
                  onChange={(event) => setNickname(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') {
                      handleSpawn();
                    }
                  }}
                />
                <p className="form-group__hint">A human-friendly name to help you identify this session later.</p>
              </div>
            </div>
          )}
        </div>

        <div className="wizard__actions">
          <button type="button" className="btn" onClick={prevStep} disabled={currentStep === 1 || spawning}>
            Back
          </button>
          <button
            type="button"
            className="btn btn--primary"
            onClick={currentStep === TOTAL_STEPS ? handleSpawn : nextStep}
            disabled={spawning}
            style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}
          >
            {spawning ? (
              <>
                <span className="spinner spinner--small"></span>
                Spawning...
              </>
            ) : currentStep === TOTAL_STEPS ? 'Spawn' : 'Next'}
          </button>
        </div>
      </div>
    </>
  );
}
