import React, { useEffect, useState } from 'react';
import { getConfig, updateConfig } from '../lib/api.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import { useConfig } from '../contexts/ConfigContext.jsx';

const TOTAL_STEPS = 4;
const TABS = ['Workspace', 'Repositories', 'Agents', 'Commands', 'Advanced'];

export default function ConfigPage() {
  const { isNotConfigured, reloadConfig } = useConfig();
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [warning, setWarning] = useState('');
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();

  // Wizard state
  const [currentStep, setCurrentStep] = useState(1);

  // Form state
  const [workspacePath, setWorkspacePath] = useState('');
  const [repos, setRepos] = useState([]);
  const [agents, setAgents] = useState([]);
  const [commands, setCommands] = useState([]);

  // Terminal state
  const [terminalWidth, setTerminalWidth] = useState('120');
  const [terminalHeight, setTerminalHeight] = useState('40');
  const [terminalSeedLines, setTerminalSeedLines] = useState('100');

  // Internal settings state
  const [mtimePollInterval, setMtimePollInterval] = useState(5000);
  const [sessionsPollInterval, setSessionsPollInterval] = useState(5000);
  const [viewedBuffer, setViewedBuffer] = useState(5000);
  const [sessionSeenInterval, setSessionSeenInterval] = useState(2000);
  const [gitStatusPollInterval, setGitStatusPollInterval] = useState(10000);
  const [gitCloneTimeout, setGitCloneTimeout] = useState(300);
  const [gitStatusTimeout, setGitStatusTimeout] = useState(30);

  // Input states for new items
  const [newRepoName, setNewRepoName] = useState('');
  const [newRepoUrl, setNewRepoUrl] = useState('');
  const [newAgentName, setNewAgentName] = useState('');
  const [newAgentCommand, setNewAgentCommand] = useState('');
  const [newCommandName, setNewCommandName] = useState('');
  const [newCommandCommand, setNewCommandCommand] = useState('');

  // Validation state per step
  const [stepErrors, setStepErrors] = useState({ 1: null, 2: null, 3: null, 4: null, 5: null });

  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setError('');
      try {
        const data = await getConfig();
        if (!active) return;
        setConfig(data);
        setWorkspacePath(data.workspace_path || '');
        setTerminalWidth(String(data.terminal?.width || 120));
        setTerminalHeight(String(data.terminal?.height || 40));
        setTerminalSeedLines(String(data.terminal?.seed_lines || 100));
        setRepos(data.repos || []);

        // Separate agents and commands based on agentic field
        const agentItems = (data.agents || []).filter(a => a.agentic !== false);
        const commandItems = (data.agents || []).filter(a => a.agentic === false);
        setAgents(agentItems);
        setCommands(commandItems);

        setMtimePollInterval(data.internal?.mtime_poll_interval_ms || 5000);
        setSessionsPollInterval(data.internal?.sessions_poll_interval_ms || 5000);
        setViewedBuffer(data.internal?.viewed_buffer_ms || 5000);
        setSessionSeenInterval(data.internal?.session_seen_interval_ms || 2000);
        setGitStatusPollInterval(data.internal?.git_status_poll_interval_ms || 10000);
        setGitCloneTimeout(data.internal?.git_clone_timeout_seconds || 300);
        setGitStatusTimeout(data.internal?.git_status_timeout_seconds || 30);
      } catch (err) {
        if (!active) return;
        setError(err.message || 'Failed to load config');
      } finally {
        if (active) setLoading(false);
      }
    };

    load();
    return () => { active = false };
  }, []);

  // Validation for each step - returns true if valid, also sets error state
  const validateStep = (step) => {
    let error = null;

    if (step === 1) {
      if (!workspacePath.trim()) {
        error = 'Workspace path is required';
      }
    } else if (step === 2) {
      if (repos.length === 0) {
        error = 'Add at least one repository';
      }
    } else if (step === 3) {
      if (agents.length === 0) {
        error = 'Add at least one agent';
      }
    } else if (step === 4) {
      // Commands are optional
    } else if (step === 5) {
      const width = parseInt(terminalWidth);
      const height = parseInt(terminalHeight);
      const seedLines = parseInt(terminalSeedLines);
      if (!width || !height || !seedLines || width <= 0 || height <= 0 || seedLines <= 0) {
        error = 'Terminal settings must be greater than 0';
      }
    }

    setStepErrors(prev => ({ ...prev, [step]: error }));
    return !error;
  };

  const saveCurrentStep = async () => {
    if (!validateStep(currentStep)) {
      if (stepErrors[currentStep]) {
        toastError(stepErrors[currentStep]);
      }
      return false;
    }

    setSaving(true);
    setWarning('');

    try {
      const width = parseInt(terminalWidth);
      const height = parseInt(terminalHeight);
      const seedLines = parseInt(terminalSeedLines);

      // Combine agents and commands for the API
      const allAgents = [
        ...agents.map(a => ({ ...a, agentic: true })),
        ...commands.map(c => ({ ...c, agentic: false }))
      ];

      const updateRequest = {
        workspace_path: workspacePath,
        terminal: { width, height, seed_lines: seedLines },
        repos: repos,
        agents: allAgents,
        internal: {
          mtime_poll_interval_ms: mtimePollInterval,
          sessions_poll_interval_ms: sessionsPollInterval,
          viewed_buffer_ms: viewedBuffer,
          session_seen_interval_ms: sessionSeenInterval,
          git_status_poll_interval_ms: gitStatusPollInterval,
          git_clone_timeout_seconds: gitCloneTimeout,
          git_status_timeout_seconds: gitStatusTimeout,
        }
      };

      const result = await updateConfig(updateRequest);
      reloadConfig();

      if (result.warning) {
        setWarning(result.warning);
      } else {
        success('Configuration saved');
      }
      return true;
    } catch (err) {
      toastError(err.message || 'Failed to save config');
      return false;
    } finally {
      setSaving(false);
    }
  };

  const nextStep = async () => {
    if (!validateStep(currentStep)) {
      if (stepErrors[currentStep]) {
        toastError(stepErrors[currentStep]);
      }
      return;
    }

    const saved = await saveCurrentStep();
    if (saved && currentStep < TOTAL_STEPS) {
      setCurrentStep((step) => step + 1);
    }
  };

  const prevStep = () => {
    setCurrentStep((step) => Math.max(1, step - 1));
  };

  const addRepo = () => {
    if (!newRepoName.trim()) {
      toastError('Repo name is required');
      return;
    }
    if (!newRepoUrl.trim()) {
      toastError('Repo URL is required');
      return;
    }
    if (repos.some(r => r.name === newRepoName)) {
      toastError('Repo name already exists');
      return;
    }
    setRepos([...repos, { name: newRepoName, url: newRepoUrl }]);
    setNewRepoName('');
    setNewRepoUrl('');
  };

  const removeRepo = async (name) => {
    const confirmed = await confirm('Remove repo?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setRepos(repos.filter(r => r.name !== name));
    }
  };

  const addAgent = () => {
    if (!newAgentName.trim()) {
      toastError('Agent name is required');
      return;
    }
    if (!newAgentCommand.trim()) {
      toastError('Agent command is required');
      return;
    }
    if (agents.some(a => a.name === newAgentName)) {
      toastError('Agent name already exists');
      return;
    }
    setAgents([...agents, { name: newAgentName, command: newAgentCommand, agentic: true }]);
    setNewAgentName('');
    setNewAgentCommand('');
  };

  const removeAgent = async (name) => {
    const confirmed = await confirm('Remove agent?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setAgents(agents.filter(a => a.name !== name));
    }
  };

  const addCommand = () => {
    if (!newCommandName.trim()) {
      toastError('Command name is required');
      return;
    }
    if (!newCommandCommand.trim()) {
      toastError('Command is required');
      return;
    }
    if (commands.some(c => c.name === newCommandName)) {
      toastError('Command name already exists');
      return;
    }
    setCommands([...commands, { name: newCommandName, command: newCommandCommand, agentic: false }]);
    setNewCommandName('');
    setNewCommandCommand('');
  };

  const removeCommand = async (name) => {
    const confirmed = await confirm('Remove command?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setCommands(commands.filter(c => c.name !== name));
    }
  };

  // Map step number to tab index (skip commands step in setup wizard)
  const getTabForStep = (step) => {
    if (step <= 3) return step;
    return step + 1; // Skip commands in initial setup
  };

  const getCurrentTab = () => {
    // For normal config mode, tab = step directly
    return currentStep;
  };

  // Check if each step is valid
  const stepValid = {
    1: workspacePath.trim().length > 0,
    2: repos.length > 0,
    3: agents.length > 0,
    4: true, // Commands are optional
    5: true // Advanced step is always valid (has defaults)
  };

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading configuration...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Failed to load config</h3>
        <p className="empty-state__description">{error}</p>
      </div>
    );
  }

  const currentTab = getCurrentTab();

  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">
          {isNotConfigured ? 'Setup schmux' : 'Configuration'}
        </h1>
      </div>

      {isNotConfigured && (
        <div className="banner banner--info" style={{ marginBottom: 'var(--spacing-lg)' }}>
          <p style={{ margin: 0 }}>
            <strong>Welcome to schmux!</strong> Complete these steps to start spawning sessions.
          </p>
        </div>
      )}

      {warning && (
        <div className="banner banner--warning" style={{ marginBottom: 'var(--spacing-lg)' }}>
          <p style={{ margin: 0 }}>
            <strong>Warning:</strong> {warning}
          </p>
        </div>
      )}

      {/* Steps navigation */}
      {isNotConfigured ? (
        <div className="wizard__steps">
          {[1, 2, 3, 5].map((stepNum) => {
            const isCompleted = stepNum < currentStep;
            const isCurrent = stepNum === currentStep;
            const isValid = stepValid[stepNum];
            const stepLabel = TABS[stepNum - 1];

            return (
              <div
                key={stepNum}
                className={`wizard__step ${isCurrent ? 'wizard__step--active' : ''} ${isCompleted ? 'wizard__step--completed' : ''}`}
                data-step={stepNum}
                onClick={() => {
                  if (isCompleted || (isValid && stepNum === currentStep + 1)) {
                    setCurrentStep(stepNum);
                  }
                }}
                style={{
                  cursor: (isCompleted || (isValid && stepNum === currentStep + 1)) ? 'pointer' : 'not-allowed',
                  opacity: (!isCompleted && !isValid && stepNum !== currentStep) ? 0.5 : 1
                }}
              >
                <span className="wizard__step-number">{stepNum}</span>
                <span className="wizard__step-label">{stepLabel}</span>
                {isCompleted && <span className="wizard__step-check">✓</span>}
              </div>
            );
          })}
        </div>
      ) : (
        <div className="config-tabs" style={{ marginBottom: 'var(--spacing-lg)' }}>
          {TABS.map((tab, index) => {
            const stepNum = index + 1;
            const isCurrent = stepNum === currentTab;
            const isValid = stepValid[stepNum];

            return (
              <button
                key={stepNum}
                className={`config-tab ${isCurrent ? 'config-tab--active' : ''}`}
                onClick={() => setCurrentStep(stepNum)}
              >
                <span className="config-tab__label">{tab}</span>
                {isValid && <span className="config-tab__check">✓</span>}
              </button>
            );
          })}
        </div>
      )}

      {/* Wizard content */}
      <div className="wizard">
        <div className="wizard__content">
          {currentTab === 1 && (
            <div className="wizard-step-content" data-step="1">
              <h2 className="wizard-step-content__title">Workspace Directory</h2>
              <p className="wizard-step-content__description">
                This is where schmux will store cloned repositories. Each session gets its own workspace directory here.
                Only affects new sessions - existing workspaces keep their current location.
              </p>

              <div className="form-group">
                <label className="form-group__label">Workspace Path</label>
                <input
                  type="text"
                  className="input"
                  value={workspacePath}
                  onChange={(e) => {
                    setWorkspacePath(e.target.value);
                    if (e.target.value.trim()) {
                      setStepErrors(prev => ({ ...prev, 1: null }));
                    }
                  }}
                  placeholder="~/schmux-workspaces"
                  autoFocus
                />
                <p className="form-group__hint">
                  Directory where cloned repositories will be stored. Can use ~ for home directory.
                </p>
                {stepErrors[1] && (
                  <p className="form-group__error">{stepErrors[1]}</p>
                )}
              </div>
            </div>
          )}

          {currentTab === 2 && (
            <div className="wizard-step-content" data-step="2">
              <h2 className="wizard-step-content__title">Repositories</h2>
              <p className="wizard-step-content__description">
                Add the Git repositories that AI agents will work on.
              </p>

              {repos.length === 0 ? (
                <div className="empty-state-hint">
                  No repositories configured. Add at least one to continue.
                </div>
              ) : (
                <div className="item-list">
                  {repos.map((repo) => (
                    <div className="item-list__item" key={repo.name}>
                      <div className="item-list__item-primary">
                        <span className="item-list__item-name">{repo.name}</span>
                        <span className="item-list__item-detail">{repo.url}</span>
                      </div>
                      <button
                        className="btn btn--sm btn--danger"
                        onClick={() => removeRepo(repo.name)}
                      >
                        Remove
                      </button>
                    </div>
                  ))}
                </div>
              )}

              <div className="add-item-form">
                <div className="add-item-form__inputs">
                  <input
                    type="text"
                    className="input"
                    placeholder="Name"
                    value={newRepoName}
                    onChange={(e) => setNewRepoName(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addRepo()}
                  />
                  <input
                    type="text"
                    className="input"
                    placeholder="git@github.com:user/repo.git"
                    value={newRepoUrl}
                    onChange={(e) => setNewRepoUrl(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addRepo()}
                  />
                </div>
                <button type="button" className="btn btn--sm" onClick={addRepo}>Add</button>
              </div>
              {stepErrors[2] && (
                <p className="form-group__error" style={{ marginTop: 'var(--spacing-md)' }}>{stepErrors[2]}</p>
              )}
            </div>
          )}

          {currentTab === 3 && (
            <div className="wizard-step-content" data-step="3">
              <h2 className="wizard-step-content__title">AI Agents</h2>
              <p className="wizard-step-content__description">
                Configure the AI coding agents that take prompts and spawn multiple parallel sessions.
              </p>

              {agents.length === 0 ? (
                <div className="empty-state-hint">
                  No agents configured. Add at least one to continue.
                </div>
              ) : (
                <div className="item-list">
                  {agents.map((agent) => (
                    <div className="item-list__item" key={agent.name}>
                      <div className="item-list__item-primary">
                        <span className="item-list__item-name">{agent.name}</span>
                        <span className="item-list__item-detail">{agent.command}</span>
                      </div>
                      <button
                        className="btn btn--sm btn--danger"
                        onClick={() => removeAgent(agent.name)}
                      >
                        Remove
                      </button>
                    </div>
                  ))}
                </div>
              )}

              <div className="add-item-form">
                <div className="add-item-form__inputs">
                  <input
                    type="text"
                    className="input"
                    placeholder="Name"
                    value={newAgentName}
                    onChange={(e) => setNewAgentName(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addAgent()}
                  />
                  <input
                    type="text"
                    className="input"
                    placeholder="Command (e.g., claude, codex)"
                    value={newAgentCommand}
                    onChange={(e) => setNewAgentCommand(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addAgent()}
                  />
                </div>
                <button type="button" className="btn btn--sm" onClick={addAgent}>Add</button>
              </div>
              {stepErrors[3] && (
                <p className="form-group__error" style={{ marginTop: 'var(--spacing-md)' }}>{stepErrors[3]}</p>
              )}
            </div>
          )}

          {currentTab === 4 && (
            <div className="wizard-step-content" data-step="4">
              <h2 className="wizard-step-content__title">Commands</h2>
              <p className="wizard-step-content__description">
                Configure shorthand commands that run without prompts (e.g., build, test, lint).
              </p>

              {commands.length === 0 ? (
                <div className="empty-state-hint">
                  No commands configured. Commands are optional - you can add them later.
                </div>
              ) : (
                <div className="item-list">
                  {commands.map((cmd) => (
                    <div className="item-list__item" key={cmd.name}>
                      <div className="item-list__item-primary">
                        <span className="item-list__item-name">{cmd.name}</span>
                        <span className="item-list__item-detail">{cmd.command}</span>
                      </div>
                      <button
                        className="btn btn--sm btn--danger"
                        onClick={() => removeCommand(cmd.name)}
                      >
                        Remove
                      </button>
                    </div>
                  ))}
                </div>
              )}

              <div className="add-item-form">
                <div className="add-item-form__inputs">
                  <input
                    type="text"
                    className="input"
                    placeholder="Name"
                    value={newCommandName}
                    onChange={(e) => setNewCommandName(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addCommand()}
                  />
                  <input
                    type="text"
                    className="input"
                    placeholder="Command (e.g., go build ./...)"
                    value={newCommandCommand}
                    onChange={(e) => setNewCommandCommand(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addCommand()}
                  />
                </div>
                <button type="button" className="btn btn--sm" onClick={addCommand}>Add</button>
              </div>
            </div>
          )}

          {currentTab === 5 && (
            <div className="wizard-step-content" data-step="5">
              <h2 className="wizard-step-content__title">Advanced Settings</h2>
              <p className="wizard-step-content__description">
                Terminal dimensions and internal timing intervals. You can leave these as defaults unless you have specific needs.
              </p>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Terminal Settings</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Width</label>
                      <input
                        type="number"
                        className="input"
                        min="1"
                        value={terminalWidth}
                        onChange={(e) => setTerminalWidth(e.target.value)}
                      />
                      <p className="form-group__hint">Terminal width in columns</p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Height</label>
                      <input
                        type="number"
                        className="input"
                        min="1"
                        value={terminalHeight}
                        onChange={(e) => setTerminalHeight(e.target.value)}
                      />
                      <p className="form-group__hint">Terminal height in rows</p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Seed Lines</label>
                      <input
                        type="number"
                        className="input"
                        min="1"
                        value={terminalSeedLines}
                        onChange={(e) => setTerminalSeedLines(e.target.value)}
                      />
                      <p className="form-group__hint">Lines to capture when reconnecting</p>
                    </div>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Internal Settings</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label className="form-group__label">Mtime Poll Interval (ms)</label>
                    <input
                      type="number"
                      className="input input--compact"
                      min="100"
                      value={mtimePollInterval}
                      onChange={(e) => setMtimePollInterval(parseInt(e.target.value) || 5000)}
                    />
                    <p className="form-group__hint">How often to check log file mtimes for new output</p>
                  </div>

                  <div className="form-group">
                    <label className="form-group__label">Sessions Poll Interval (ms)</label>
                    <input
                      type="number"
                      className="input input--compact"
                      min="100"
                      value={sessionsPollInterval}
                      onChange={(e) => setSessionsPollInterval(parseInt(e.target.value) || 5000)}
                    />
                    <p className="form-group__hint">How often to refresh sessions list</p>
                  </div>

                  <div className="form-group">
                    <label className="form-group__label">Viewed Buffer (ms)</label>
                    <input
                      type="number"
                      className="input input--compact"
                      min="100"
                      value={viewedBuffer}
                      onChange={(e) => setViewedBuffer(parseInt(e.target.value) || 5000)}
                    />
                    <p className="form-group__hint">Time to keep session marked as "viewed" after last check</p>
                  </div>

                  <div className="form-group">
                    <label className="form-group__label">Session Seen Interval (ms)</label>
                    <input
                      type="number"
                      className="input input--compact"
                      min="100"
                      value={sessionSeenInterval}
                      onChange={(e) => setSessionSeenInterval(parseInt(e.target.value) || 2000)}
                    />
                    <p className="form-group__hint">How often to check for session activity</p>
                  </div>

                  <div className="form-group">
                    <label className="form-group__label">Git Status Poll Interval (ms)</label>
                    <input
                      type="number"
                      className="input input--compact"
                      min="100"
                      value={gitStatusPollInterval}
                      onChange={(e) => setGitStatusPollInterval(parseInt(e.target.value) || 10000)}
                    />
                    <p className="form-group__hint">How often to refresh git status (dirty, ahead, behind)</p>
                  </div>

                  <div className="form-group">
                    <label className="form-group__label">Git Clone Timeout (seconds)</label>
                    <input
                      type="number"
                      className="input input--compact"
                      min="10"
                      value={gitCloneTimeout}
                      onChange={(e) => setGitCloneTimeout(parseInt(e.target.value) || 300)}
                    />
                    <p className="form-group__hint">Maximum time to wait for git clone when spawning sessions (default: 300s = 5 min)</p>
                  </div>

                  <div className="form-group">
                    <label className="form-group__label">Git Status Timeout (seconds)</label>
                    <input
                      type="number"
                      className="input input--compact"
                      min="5"
                      value={gitStatusTimeout}
                      onChange={(e) => setGitStatusTimeout(parseInt(e.target.value) || 30)}
                    />
                    <p className="form-group__hint">Maximum time to wait for git status/diff operations (default: 30s)</p>
                  </div>
                </div>
              </div>
              {stepErrors[5] && (
                <p className="form-group__error">{stepErrors[5]}</p>
              )}
            </div>
          )}
        </div>

        {/* Unified wizard footer navigation */}
        <div className="wizard__actions">
          <div className="wizard__actions-left">
            {currentStep > 1 && (
              <button
                className="btn"
                onClick={prevStep}
                disabled={saving}
              >
                ← Back
              </button>
            )}
            {!isNotConfigured && currentStep === 1 && (
              <span className="wizard__hint">Use tabs above to navigate between sections</span>
            )}
          </div>
          <div className="wizard__actions-right">
            <button
              className="btn btn--primary"
              onClick={isNotConfigured && currentStep < TOTAL_STEPS ? nextStep : saveCurrentStep}
              disabled={saving}
            >
              {saving ? 'Saving...' : isNotConfigured ?
                (currentStep === TOTAL_STEPS ? 'Finish Setup' : 'Next →') :
                'Save Changes'
              }
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
