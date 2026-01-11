import React, { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { getConfig, getWorkspaces, spawnSessions } from '../lib/api.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useRequireConfig } from '../contexts/ConfigContext.jsx';

const STEPS = ['Target', 'Command', 'Review'];
const TOTAL_STEPS = STEPS.length;

export default function SpawnPage() {
  useRequireConfig();
  const [currentStep, setCurrentStep] = useState(1);
  const [repos, setRepos] = useState([]);
  const [agents, setAgents] = useState([]);
  const [commands, setCommands] = useState([]);
  const [agentCounts, setAgentCounts] = useState({});
  const [selectedCommand, setSelectedCommand] = useState('');
  const [spawnMode, setSpawnMode] = useState(null); // 'agent' | 'command' | null
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('main');
  const [prompt, setPrompt] = useState('');
  const [nickname, setNickname] = useState('');
  const [prefillWorkspaceId, setPrefillWorkspaceId] = useState('');
  const [loading, setLoading] = useState(true);
  const [configError, setConfigError] = useState('');
  const [results, setResults] = useState(null);
  const [spawning, setSpawning] = useState(false);
  const [showAgentError, setShowAgentError] = useState(false);
  const [searchParams] = useSearchParams();
  const { error: toastError } = useToast();

  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setConfigError('');
      try {
        const config = await getConfig();
        if (!active) return;
        setRepos(config.repos || []);

        // Separate agents and commands
        const agentItems = (config.agents || []).filter(a => a.agentic !== false);
        const commandItems = (config.agents || []).filter(a => a.agentic === false);
        setAgents(agentItems);
        setCommands(commandItems);

        const counts = {};
        agentItems.forEach((agent) => {
          counts[agent.name] = 0;
        });
        setAgentCounts(counts);
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
    let active = true;

    const checkParams = async () => {
      const workspaceId = searchParams.get('workspace_id');
      if (!workspaceId) return;
      setPrefillWorkspaceId(workspaceId);

      let prefillRepo = searchParams.get('repo');
      let prefillBranch = searchParams.get('branch');

      if (!prefillRepo || !prefillBranch) {
        try {
          const workspaces = await getWorkspaces();
          const workspace = workspaces.find((ws) => ws.id === workspaceId);
          if (workspace) {
            prefillRepo = prefillRepo || workspace.repo;
            prefillBranch = prefillBranch || workspace.branch;
          }
        } catch (err) {
          console.error('Failed to fetch workspace details:', err);
        }
      }

      if (!active) return;
      if (prefillRepo) setRepo(prefillRepo);
      if (prefillBranch) setBranch(prefillBranch);
    };

    checkParams();
    return () => {
      active = false;
    };
  }, [searchParams]);

  const agentList = useMemo(() => {
    return agents.map((agent) => ({
      ...agent,
      count: agentCounts[agent.name] || 0
    }));
  }, [agents, agentCounts]);

  const totalAgentCount = useMemo(() => {
    return Object.values(agentCounts).reduce((sum, count) => sum + count, 0);
  }, [agentCounts]);

  const updateAgentCount = (name, delta) => {
    setAgentCounts((current) => {
      const next = Math.max(0, Math.min(10, (current[name] || 0) + delta));
      return { ...current, [name]: next };
    });
    setShowAgentError(false);
  };

  const applyPreset = (preset) => {
    if (preset === 'each') {
      const next = {};
      agents.forEach((agent) => {
        next[agent.name] = 1;
      });
      setAgentCounts(next);
    } else if (preset === 'review') {
      const next = {};
      agents.forEach((agent) => {
        next[agent.name] = agent.name.includes('claude') ? 2 : 0;
      });
      setAgentCounts(next);
    } else if (preset === 'reset') {
      const next = {};
      agents.forEach((agent) => {
        next[agent.name] = 0;
      });
      setAgentCounts(next);
    }
    setShowAgentError(false);
  };

  const validateStep = () => {
    if (currentStep === 1) {
      if (!repo) {
        toastError('Please select a repository');
        return false;
      }
      if (!branch) {
        toastError('Please enter a branch');
        return false;
      }
    }

    if (currentStep === 2) {
      if (spawnMode === 'agent') {
        if (totalAgentCount === 0) {
          toastError('Please select at least one agent');
          setShowAgentError(true);
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
    let selectedAgents = {};

    if (spawnMode === 'command') {
      selectedAgents[selectedCommand] = 1;
    } else {
      Object.entries(agentCounts).forEach(([name, count]) => {
        if (count > 0) selectedAgents[name] = count;
      });
    }

    setSpawning(true);

    try {
      const response = await spawnSessions({
        repo,
        branch,
        prompt: spawnMode === 'agent' ? prompt : '',
        nickname: nickname.trim(),
        agents: selectedAgents,
        workspace_id: prefillWorkspaceId || ''
      });
      setResults(response);

      // Clear collapsed state for reused workspace IDs so new sessions are visible
      const workspaceIds = [...new Set(response.filter(r => !r.error).map(r => r.workspace_id))];
      const expandedKey = 'schmux:workspace-expanded';
      const expanded = JSON.parse(localStorage.getItem(expandedKey) || '{}');
      let changed = false;
      workspaceIds.forEach(id => {
        if (expanded[id] === false) {
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
                    <span>{r.agent}</span>
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
                <div className="results-panel__item results-panel__item--error" key={`${r.agent}-${r.error}`}>
                  <div><strong>{r.agent}:</strong> {r.error}</div>
                  {(r.prompt || repo || branch || r.workspace_id) && (
                    <div style={{ marginTop: 'var(--spacing-sm)', fontSize: 'var(--font-size-sm)', color: 'var(--color-text-muted)' }}>
                      {repo && <div>Repo: {repo}</div>}
                      {branch && <div>Branch: {branch}</div>}
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
                  onChange={(event) => setRepo(event.target.value)}
                  disabled={!!prefillWorkspaceId}
                >
                  <option value="">Select repository...</option>
                  {repos.map((item) => (
                    <option key={item.url} value={item.url}>{item.name}</option>
                  ))}
                </select>
              </div>
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
                    className={`btn${spawnMode === 'agent' ? ' btn--primary' : ''}`}
                    onClick={() => {
                      if (agents.length > 0) {
                        setSpawnMode('agent');
                        setShowAgentError(false);
                      }
                    }}
                    disabled={agents.length === 0}
                  >
                    Coding Agent
                  </button>
                  <button
                    type="button"
                    className={`btn${spawnMode === 'command' ? ' btn--primary' : ''}`}
                    onClick={() => {
                      if (commands.length > 0) setSpawnMode('command');
                    }}
                    disabled={commands.length === 0}
                  >
                    Command
                  </button>
                </div>
                {spawnMode === 'agent' && agents.length === 0 && (
                  <p className="form-group__hint">No agents configured. Add some in the configuration page first.</p>
                )}
                {spawnMode === 'command' && commands.length === 0 && (
                  <p className="form-group__hint">No commands configured. Add some in the configuration page first.</p>
                )}
              </div>

              {/* Content that shows AFTER selection */}
              {spawnMode === 'agent' && agents.length > 0 && (
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

                  {/* Agent Grid */}
                  <div className="agent-grid">
                    {agentList.map((agent) => (
                      <div className="agent-card" key={agent.name}>
                        <div className="agent-card__label">{agent.name}</div>
                        <div className="agent-card__control">
                          <button className="agent-card__btn" onClick={() => updateAgentCount(agent.name, -1)} aria-label={`Decrease ${agent.name} count`}>−</button>
                          <span className="agent-card__count">{agent.count}</span>
                          <button className="agent-card__btn" onClick={() => updateAgentCount(agent.name, 1)} aria-label={`Increase ${agent.name} count`}>+</button>
                        </div>
                      </div>
                    ))}
                  </div>

                  {showAgentError && (
                    <div className="form-group__error">
                      Please select at least one agent
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
                      placeholder="Describe the task you want the agents to work on..."
                      value={prompt}
                      onChange={(event) => setPrompt(event.target.value)}
                    />
                    <p className="form-group__hint">Be specific about what you want the agents to do. This prompt will be sent to all spawned agents.</p>
                  </div>
                </>
              )}

              {spawnMode === 'command' && commands.length > 0 && (
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
                    {commands.map((cmd) => (
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
                    <span className="metadata-field__value">{repo}</span>
                  </div>
                  <div className="metadata-field">
                    <span className="metadata-field__label">Branch</span>
                    <span className="metadata-field__value">{branch}</span>
                  </div>
                  <div className="metadata-field">
                    <span className="metadata-field__label">Workspace</span>
                    <span className="metadata-field__value">{prefillWorkspaceId || 'New workspace'}</span>
                  </div>
                  {spawnMode === 'agent' ? (
                    <>
                      <div className="metadata-field">
                        <span className="metadata-field__label">Agents</span>
                        <span className="metadata-field__value">
                          {Object.entries(agentCounts)
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
                      <span className="metadata-field__value mono">{commands.find(c => c.name === selectedCommand)?.command}</span>
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
