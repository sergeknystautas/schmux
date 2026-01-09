import React, { useEffect, useState, useRef } from 'react';
import { getConfig, updateConfig } from '../lib/api.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import { useConfig } from '../contexts/ConfigContext.jsx';

const TOTAL_STEPS = 4;
const STEPS = ['Workspace', 'Repositories', 'Agents', 'Advanced'];

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

  // Terminal state (refs for uncontrolled inputs)
  const [terminalWidth, setTerminalWidth] = useState('120');
  const [terminalHeight, setTerminalHeight] = useState('40');
  const [terminalSeedLines, setTerminalSeedLines] = useState('100');
  const terminalWidthRef = useRef(null);
  const terminalHeightRef = useRef(null);
  const terminalSeedLinesRef = useRef(null);

  // Internal settings state
  const [mtimePollInterval, setMtimePollInterval] = useState(5000);
  const [sessionsPollInterval, setSessionsPollInterval] = useState(5000);
  const [viewedBuffer, setViewedBuffer] = useState(5000);
  const [sessionSeenInterval, setSessionSeenInterval] = useState(2000);

  // Input states for new items
  const [newRepoName, setNewRepoName] = useState('');
  const [newRepoUrl, setNewRepoUrl] = useState('');
  const [newAgentName, setNewAgentName] = useState('');
  const [newAgentCommand, setNewAgentCommand] = useState('');

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
        setAgents(data.agents || []);
        setMtimePollInterval(data.internal?.mtime_poll_interval_ms || 5000);
        setSessionsPollInterval(data.internal?.sessions_poll_interval_ms || 5000);
        setViewedBuffer(data.internal?.viewed_buffer_ms || 5000);
        setSessionSeenInterval(data.internal?.session_seen_interval_ms || 2000);
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

  // Validation for each step
  const validateStep = (step) => {
    if (step === 1) { // Workspace
      if (!workspacePath.trim()) {
        toastError('Workspace path is required');
        return false;
      }
      return true;
    }
    if (step === 2) { // Repositories
      if (repos.length === 0) {
        toastError('Add at least one repository');
        return false;
      }
      return true;
    }
    if (step === 3) { // Agents
      if (agents.length === 0) {
        toastError('Add at least one agent');
        return false;
      }
      return true;
    }
    if (step === 4) { // Advanced
      const width = parseInt(terminalWidthRef.current?.value || '0');
      const height = parseInt(terminalHeightRef.current?.value || '0');
      const seedLines = parseInt(terminalSeedLinesRef.current?.value || '0');
      if (width <= 0 || height <= 0 || seedLines <= 0) {
        toastError('Terminal settings must be greater than 0');
        return false;
      }
      return true;
    }
    return true;
  };

  // Save current step
  const saveCurrentStep = async () => {
    if (!validateStep(currentStep)) return;

    setSaving(true);
    setWarning('');

    try {
      const width = parseInt(terminalWidthRef.current?.value || terminalWidth);
      const height = parseInt(terminalHeightRef.current?.value || terminalHeight);
      const seedLines = parseInt(terminalSeedLinesRef.current?.value || terminalSeedLines);

      const updateRequest = {
        workspace_path: workspacePath,
        terminal: { width, height, seed_lines: seedLines },
        repos: repos,
        agents: agents,
        internal: {
          mtime_poll_interval_ms: mtimePollInterval,
          sessions_poll_interval_ms: sessionsPollInterval,
          viewed_buffer_ms: viewedBuffer,
          session_seen_interval_ms: sessionSeenInterval,
        }
      };

      const result = await updateConfig(updateRequest);
      reloadConfig();

      if (result.warning) {
        setWarning(result.warning);
      } else {
        success('Configuration saved');
      }
    } catch (err) {
      toastError(err.message || 'Failed to save config');
    } finally {
      setSaving(false);
    }
  };

  const nextStep = async () => {
    if (!validateStep(currentStep)) return;

    // Save before moving to next step
    await saveCurrentStep();

    if (currentStep < TOTAL_STEPS) {
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
    setAgents([...agents, { name: newAgentName, command: newAgentCommand }]);
    setNewAgentName('');
    setNewAgentCommand('');
  };

  const removeAgent = async (name) => {
    const confirmed = await confirm('Remove agent?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setAgents(agents.filter(a => a.name !== name));
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

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Failed to load config</h3>
        <p className="empty-state__description">{error}</p>
      </div>
    );
  }

  // Render step content
  const renderStepContent = () => {
    switch (currentStep) {
      case 1: // Workspace
        return (
          <div className="wizard-step-content" data-step="1">
            <div className="card">
              <div className="card__body">
                <h2 style={{ marginBottom: 'var(--spacing-md)' }}>Workspace Directory</h2>
                <p className="text-muted" style={{ marginBottom: 'var(--spacing-lg)' }}>
                  This is where schmux will store cloned repositories. Each session gets its own workspace directory here.
                  Only affects new sessions - existing workspaces keep their current location.
                </p>

                <div className="form-group">
                  <label className="form-group__label">Workspace Path</label>
                  <input
                    type="text"
                    className="input"
                    value={workspacePath}
                    onChange={(e) => setWorkspacePath(e.target.value)}
                    placeholder="~/schmux-workspaces"
                  />
                  <p className="form-group__hint">
                    Directory where cloned repositories will be stored. Can use ~ for home directory.
                  </p>
                </div>

                <div className="wizard__actions" style={{ marginTop: 'var(--spacing-lg)', display: 'flex', gap: 'var(--spacing-sm)', justifyContent: 'space-between' }}>
                  {currentStep > 1 && (
                    <button
                      className="btn"
                      onClick={prevStep}
                      disabled={saving}
                    >
                      Back
                    </button>
                  )}
                  <button
                    className="btn btn--primary"
                    onClick={async () => {
                      await saveCurrentStep();
                      if (currentStep < TOTAL_STEPS) {
                        setCurrentStep((step) => step + 1);
                      }
                    }}
                    disabled={saving}
                  >
                    {saving ? 'Saving...' : currentStep === TOTAL_STEPS ? 'Finish' : 'Next'}
                  </button>
                </div>
              </div>
            </div>
          </div>
        );

      case 2: // Repositories
        return (
          <div className="wizard-step-content" data-step="2">
            <div className="card">
              <div className="card__body">
                <h2 style={{ marginBottom: 'var(--spacing-md)' }}>Repositories</h2>
                <p className="text-muted" style={{ marginBottom: 'var(--spacing-lg)' }}>
                  Add the Git repositories that AI agents will work on. Each repository you configure here will be available when spawning sessions.
                </p>

                {repos.length === 0 ? (
                  <p className="text-muted" style={{ marginBottom: 'var(--spacing-md)' }}>
                    No repositories configured. Add at least one to continue.
                  </p>
                ) : (
                  <div style={{ marginBottom: 'var(--spacing-md)' }}>
                    {repos.map((repo) => (
                      <div
                        key={repo.name}
                        style={{
                          display: 'flex',
                          justifyContent: 'space-between',
                          alignItems: 'center',
                          gap: 'var(--spacing-md)',
                          padding: 'var(--spacing-sm)',
                          backgroundColor: 'var(--color-bg-secondary)',
                          borderRadius: 'var(--border-radius)',
                          marginBottom: 'var(--spacing-xs)'
                        }}
                      >
                        <div style={{ flex: 1, fontWeight: 500 }}>{repo.name}</div>
                        <div className="text-muted" style={{ flex: 2, fontSize: 'var(--font-size-sm)' }}>{repo.url}</div>
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

                <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'flex-end' }}>
                  <div className="form-group" style={{ flex: 1, marginBottom: 0 }}>
                    <input
                      type="text"
                      className="input"
                      placeholder="Name"
                      value={newRepoName}
                      onChange={(e) => setNewRepoName(e.target.value)}
                    />
                  </div>
                  <div className="form-group" style={{ flex: 2, marginBottom: 0 }}>
                    <input
                      type="text"
                      className="input"
                      placeholder="git@github.com:user/repo.git"
                      value={newRepoUrl}
                      onChange={(e) => setNewRepoUrl(e.target.value)}
                    />
                  </div>
                  <button type="button" className="btn btn--sm" onClick={addRepo}>Add</button>
                </div>

                <div className="wizard__actions" style={{ marginTop: 'var(--spacing-lg)', display: 'flex', gap: 'var(--spacing-sm)', justifyContent: 'space-between' }}>
                  {currentStep > 1 && (
                    <button
                      className="btn"
                      onClick={prevStep}
                      disabled={saving}
                    >
                      Back
                    </button>
                  )}
                  <button
                    className="btn btn--primary"
                    onClick={async () => {
                      await saveCurrentStep();
                      if (currentStep < TOTAL_STEPS) {
                        setCurrentStep((step) => step + 1);
                      }
                    }}
                    disabled={saving}
                  >
                    {saving ? 'Saving...' : currentStep === TOTAL_STEPS ? 'Finish' : 'Next'}
                  </button>
                </div>
              </div>
            </div>
          </div>
        );

      case 3: // Agents
        return (
          <div className="wizard-step-content" data-step="3">
            <div className="card">
              <div className="card__body">
                <h2 style={{ marginBottom: 'var(--spacing-md)' }}>AI Agents</h2>
                <p className="text-muted" style={{ marginBottom: 'var(--spacing-lg)' }}>
                  Configure the AI coding agents you want to use. Each agent represents a different AI tool (Claude, Codex, etc.).
                </p>

                {agents.length === 0 ? (
                  <p className="text-muted" style={{ marginBottom: 'var(--spacing-md)' }}>
                    No agents configured. Add at least one to continue.
                  </p>
                ) : (
                  <div style={{ marginBottom: 'var(--spacing-md)' }}>
                    {agents.map((agent) => (
                      <div
                        key={agent.name}
                        style={{
                          display: 'flex',
                          justifyContent: 'space-between',
                          alignItems: 'center',
                          gap: 'var(--spacing-md)',
                          padding: 'var(--spacing-sm)',
                          backgroundColor: 'var(--color-bg-secondary)',
                          borderRadius: 'var(--border-radius)',
                          marginBottom: 'var(--spacing-xs)'
                        }}
                      >
                        <div style={{ flex: 1, fontWeight: 500 }}>{agent.name}</div>
                        <div className="text-muted" style={{ flex: 2, fontSize: 'var(--font-size-sm)' }}>{agent.command}</div>
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

                <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'flex-end' }}>
                  <div className="form-group" style={{ flex: 1, marginBottom: 0 }}>
                    <input
                      type="text"
                      className="input"
                      placeholder="Name"
                      value={newAgentName}
                      onChange={(e) => setNewAgentName(e.target.value)}
                    />
                  </div>
                  <div className="form-group" style={{ flex: 2, marginBottom: 0 }}>
                    <input
                      type="text"
                      className="input"
                      placeholder="Command (e.g., claude, codex)"
                      value={newAgentCommand}
                      onChange={(e) => setNewAgentCommand(e.target.value)}
                    />
                  </div>
                  <button type="button" className="btn btn--sm" onClick={addAgent}>Add</button>
                </div>

                <div className="wizard__actions" style={{ marginTop: 'var(--spacing-lg)', display: 'flex', gap: 'var(--spacing-sm)', justifyContent: 'space-between' }}>
                  {currentStep > 1 && (
                    <button
                      className="btn"
                      onClick={prevStep}
                      disabled={saving}
                    >
                      Back
                    </button>
                  )}
                  <button
                    className="btn btn--primary"
                    onClick={async () => {
                      await saveCurrentStep();
                      if (currentStep < TOTAL_STEPS) {
                        setCurrentStep((step) => step + 1);
                      }
                    }}
                    disabled={saving}
                  >
                    {saving ? 'Saving...' : currentStep === TOTAL_STEPS ? 'Finish' : 'Next'}
                  </button>
                </div>
              </div>
            </div>
          </div>
        );

      case 4: // Advanced
        return (
          <div className="wizard-step-content" data-step="4">
            <h2 style={{ marginBottom: 'var(--spacing-md)' }}>Advanced Settings</h2>
            <p className="text-muted" style={{ marginBottom: 'var(--spacing-lg)' }}>
              Terminal dimensions and internal timing intervals. You can leave these as defaults unless you have specific needs.
            </p>

            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card__header">
                <h3 className="card__title">Terminal Settings</h3>
              </div>
              <div className="card__body">
                <div style={{ display: 'flex', gap: 'var(--spacing-md)', flexWrap: 'wrap' }}>
                  <div className="form-group" style={{ flex: '1', minWidth: '150px', marginBottom: 0 }}>
                    <label className="form-group__label">Width</label>
                    <input
                      ref={terminalWidthRef}
                      type="number"
                      className="input"
                      min="1"
                      defaultValue={terminalWidth}
                    />
                    <p className="form-group__hint">Terminal width in columns</p>
                  </div>

                  <div className="form-group" style={{ flex: '1', minWidth: '150px', marginBottom: 0 }}>
                    <label className="form-group__label">Height</label>
                    <input
                      ref={terminalHeightRef}
                      type="number"
                      className="input"
                      min="1"
                      defaultValue={terminalHeight}
                    />
                    <p className="form-group__hint">Terminal height in rows</p>
                  </div>

                  <div className="form-group" style={{ flex: '1', minWidth: '150px', marginBottom: 0 }}>
                    <label className="form-group__label">Seed Lines</label>
                    <input
                      ref={terminalSeedLinesRef}
                      type="number"
                      className="input"
                      min="1"
                      defaultValue={terminalSeedLines}
                    />
                    <p className="form-group__hint">Lines to capture when reconnecting</p>
                  </div>
                </div>
              </div>
            </div>

            <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="card__header">
                <h3 className="card__title">Internal Settings</h3>
              </div>
              <div className="card__body">
                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label">Mtime Poll Interval (ms)</label>
                  <input
                    type="number"
                    className="input"
                    min="100"
                    value={mtimePollInterval}
                    onChange={(e) => setMtimePollInterval(parseInt(e.target.value) || 5000)}
                    style={{ width: '200px' }}
                  />
                  <p className="form-group__hint">How often to check log file mtimes for new output</p>
                </div>

                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label">Sessions Poll Interval (ms)</label>
                  <input
                    type="number"
                    className="input"
                    min="100"
                    value={sessionsPollInterval}
                    onChange={(e) => setSessionsPollInterval(parseInt(e.target.value) || 5000)}
                    style={{ width: '200px' }}
                  />
                  <p className="form-group__hint">How often to refresh sessions list</p>
                </div>

                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label">Viewed Buffer (ms)</label>
                  <input
                    type="number"
                    className="input"
                    min="100"
                    value={viewedBuffer}
                    onChange={(e) => setViewedBuffer(parseInt(e.target.value) || 5000)}
                    style={{ width: '200px' }}
                  />
                  <p className="form-group__hint">Time to keep session marked as "viewed" after last check</p>
                </div>

                <div className="form-group">
                  <label className="form-group__label">Session Seen Interval (ms)</label>
                  <input
                    type="number"
                    className="input"
                    min="100"
                    value={sessionSeenInterval}
                    onChange={(e) => setSessionSeenInterval(parseInt(e.target.value) || 2000)}
                    style={{ width: '200px' }}
                  />
                  <p className="form-group__hint">How often to check for session activity</p>
                </div>
              </div>
            </div>

            <div className="form-group">
              <button
                className="btn btn--primary"
                onClick={saveCurrentStep}
                disabled={saving}
              >
                {saving ? 'Saving...' : isNotConfigured ? 'Finish & Save' : 'Save'}
              </button>
            </div>
          </div>
        );
    }
  };

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
            <strong>Welcome to schmux!</strong> Let's get you set up. Complete these 4 steps to start spawning sessions.
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

      {/* Wizard steps indicator (wizard mode) or tabs (tab mode) */}
      {isNotConfigured ? (
        <div className="wizard__steps">
          {STEPS.map((step, index) => {
            const stepNum = index + 1;
            const className = stepNum === currentStep
              ? 'wizard__step wizard__step--active'
              : stepNum < currentStep
                ? 'wizard__step wizard__step--completed'
                : 'wizard__step';
            return (
              <div
                key={stepNum}
                className={className}
                data-step={stepNum}
                onClick={() => {
                  // Can go back to any completed step, or to the next step
                  if (stepNum < currentStep || stepNum === currentStep + 1) {
                    setCurrentStep(stepNum);
                  }
                }}
                style={stepNum > currentStep + 1 ? { cursor: 'not-allowed', opacity: '0.5' } : {}}
              >
                {stepNum}. {step}
              </div>
            );
          })}
        </div>
      ) : (
        <div className="tabs" style={{ marginBottom: 'var(--spacing-lg)' }}>
          {STEPS.map((step, index) => {
            const stepNum = index + 1;
            return (
              <button
                key={stepNum}
                className={`tab__button${currentStep === stepNum ? ' tab__button--active' : ''}`}
                onClick={() => setCurrentStep(stepNum)}
              >
                {step}
              </button>
            );
          })}
        </div>
      )}

      {renderStepContent()}
    </>
  );
}
