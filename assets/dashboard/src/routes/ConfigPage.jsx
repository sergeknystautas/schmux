import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getConfig, updateConfig, getVariants, configureVariantSecrets, removeVariantSecrets } from '../lib/api.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import { useConfig } from '../contexts/ConfigContext.jsx';
import SetupCompleteModal from '../components/SetupCompleteModal.jsx';

const TOTAL_STEPS = 6;
const TABS = ['Workspace', 'Repositories', 'Run Targets', 'Variants', 'Quick Launch', 'Advanced'];

export default function ConfigPage() {
  const navigate = useNavigate();
  const { isNotConfigured, isFirstRun, completeFirstRun, reloadConfig } = useConfig();
  const { confirm } = useModal();
  const [showSetupComplete, setShowSetupComplete] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [warning, setWarning] = useState('');
  const { success, error: toastError } = useToast();

  // Wizard state
  const [currentStep, setCurrentStep] = useState(1);

  // Form state
  const [workspacePath, setWorkspacePath] = useState('');
  const [repos, setRepos] = useState([]);
  const [promptableTargets, setPromptableTargets] = useState([]);
  const [commandTargets, setCommandTargets] = useState([]);
  const [detectedTargets, setDetectedTargets] = useState([]);
  const [quickLaunch, setQuickLaunch] = useState([]);
  const [availableVariants, setAvailableVariants] = useState([]);
  const [nudgenikTarget, setNudgenikTarget] = useState('');

  // Terminal state
  const [terminalWidth, setTerminalWidth] = useState('120');
  const [terminalHeight, setTerminalHeight] = useState('40');
  const [terminalSeedLines, setTerminalSeedLines] = useState('100');
  const [terminalBootstrapLines, setTerminalBootstrapLines] = useState('20000');

  // Internal settings state
  const [mtimePollInterval, setMtimePollInterval] = useState(5000);
  const [sessionsPollInterval, setSessionsPollInterval] = useState(5000);
  const [viewedBuffer, setViewedBuffer] = useState(5000);
  const [sessionSeenInterval, setSessionSeenInterval] = useState(2000);
  const [gitStatusPollInterval, setGitStatusPollInterval] = useState(10000);
  const [gitCloneTimeout, setGitCloneTimeout] = useState(300);
  const [gitStatusTimeout, setGitStatusTimeout] = useState(30);
  const [networkAccess, setNetworkAccess] = useState(false);
  const [originalNetworkAccess, setOriginalNetworkAccess] = useState(false);
  const [apiNeedsRestart, setApiNeedsRestart] = useState(false);

  // Input states for new items
  const [newRepoName, setNewRepoName] = useState('');
  const [newRepoUrl, setNewRepoUrl] = useState('');
  const [newPromptableName, setNewPromptableName] = useState('');
  const [newPromptableCommand, setNewPromptableCommand] = useState('');
  const [newCommandName, setNewCommandName] = useState('');
  const [newCommandCommand, setNewCommandCommand] = useState('');
  const [newQuickLaunchName, setNewQuickLaunchName] = useState('');
  const [newQuickLaunchTarget, setNewQuickLaunchTarget] = useState('');
  const [newQuickLaunchPrompt, setNewQuickLaunchPrompt] = useState('');

  // Validation state per step
  const [stepErrors, setStepErrors] = useState({ 1: null, 2: null, 3: null, 4: null, 5: null, 6: null });

  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setError('');
      try {
        const data = await getConfig();
        if (!active) return;
        setWorkspacePath(data.workspace_path || '');
        setTerminalWidth(String(data.terminal?.width || 120));
        setTerminalHeight(String(data.terminal?.height || 40));
        setTerminalSeedLines(String(data.terminal?.seed_lines || 100));
        setTerminalBootstrapLines(String(data.terminal?.bootstrap_lines || 20000));
        setRepos(data.repos || []);

        const detectedItems = (data.run_targets || []).filter(t => t.source === 'detected');
        const promptableItems = (data.run_targets || []).filter(t => t.type === 'promptable' && t.source !== 'detected');
        const commandItems = (data.run_targets || []).filter(t => t.type === 'command' && t.source !== 'detected');
        setPromptableTargets(promptableItems);
        setCommandTargets(commandItems);
        setDetectedTargets(detectedItems);
        setQuickLaunch(data.quick_launch || []);
        setNudgenikTarget(data.nudgenik?.target || '');

        setMtimePollInterval(data.internal?.mtime_poll_interval_ms || 5000);
        setSessionsPollInterval(data.internal?.sessions_poll_interval_ms || 5000);
        setViewedBuffer(data.internal?.viewed_buffer_ms || 5000);
        setSessionSeenInterval(data.internal?.session_seen_interval_ms || 2000);
        setGitStatusPollInterval(data.internal?.git_status_poll_interval_ms || 10000);
        setGitCloneTimeout(data.internal?.git_clone_timeout_seconds || 300);
        setGitStatusTimeout(data.internal?.git_status_timeout_seconds || 30);
        const netAccess = data.internal?.network_access || false;
        setNetworkAccess(netAccess);
        setOriginalNetworkAccess(netAccess);
        setApiNeedsRestart(data.needs_restart || false);
        const variantData = await getVariants();
        if (active) {
          setAvailableVariants(variantData.variants || []);
        }
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

  const reloadVariants = async () => {
    try {
      const variantData = await getVariants();
      setAvailableVariants(variantData.variants || []);
    } catch (err) {
      toastError(err.message || 'Failed to load variants');
    }
  };

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
      // Run targets are optional
    } else if (step === 4) {
      // Variants are optional
    } else if (step === 5) {
      // Quick launch is optional
    } else if (step === 6) {
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

      const runTargets = [
        ...promptableTargets.map(t => ({ ...t, type: 'promptable' })),
        ...commandTargets.map(t => ({ ...t, type: 'command' }))
      ];

      const updateRequest = {
        workspace_path: workspacePath,
        terminal: { width, height, seed_lines: seedLines, bootstrap_lines: parseInt(terminalBootstrapLines) },
        repos: repos,
        run_targets: runTargets,
        quick_launch: quickLaunch,
        nudgenik: { target: nudgenikTarget || '' },
        internal: {
          mtime_poll_interval_ms: mtimePollInterval,
          sessions_poll_interval_ms: sessionsPollInterval,
          viewed_buffer_ms: viewedBuffer,
          session_seen_interval_ms: sessionSeenInterval,
          git_status_poll_interval_ms: gitStatusPollInterval,
          git_clone_timeout_seconds: gitCloneTimeout,
          git_status_timeout_seconds: gitStatusTimeout,
          network_access: networkAccess,
        }
      };

      const result = await updateConfig(updateRequest);
      reloadConfig();

      // Reload config to get updated needs_restart flag from server
      const reloaded = await getConfig();
      setApiNeedsRestart(reloaded.needs_restart || false);
      setOriginalNetworkAccess(networkAccess);

      if (result.warning && !isFirstRun) {
        setWarning(result.warning);
      } else if (!isFirstRun) {
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

  const addPromptableTarget = () => {
    if (!newPromptableName.trim()) {
      toastError('Run target name is required');
      return;
    }
    if (!newPromptableCommand.trim()) {
      toastError('Run target command is required');
      return;
    }
    const nameExists = [...promptableTargets, ...commandTargets, ...detectedTargets]
      .some(t => t.name === newPromptableName);
    if (nameExists) {
      toastError('Run target name already exists');
      return;
    }
    setPromptableTargets([...promptableTargets, { name: newPromptableName, command: newPromptableCommand }]);
    setNewPromptableName('');
    setNewPromptableCommand('');
  };

  const checkTargetUsage = (targetName) => {
    const inQuickLaunch = quickLaunch.some((preset) => preset.target === targetName);
    const inNudgenik = nudgenikTarget && nudgenikTarget === targetName;
    return { inQuickLaunch, inNudgenik };
  };

  const removePromptableTarget = async (name) => {
    const usage = checkTargetUsage(name);
    if (usage.inQuickLaunch || usage.inNudgenik) {
      const reasons = [
        usage.inQuickLaunch ? 'quick launch preset' : null,
        usage.inNudgenik ? 'nudgenik target' : null
      ].filter(Boolean).join(' and ');
      toastError(`Cannot remove "${name}" while used by ${reasons}.`);
      return;
    }
    const confirmed = await confirm('Remove run target?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setPromptableTargets(promptableTargets.filter(t => t.name !== name));
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
    const nameExists = [...promptableTargets, ...commandTargets, ...detectedTargets]
      .some(t => t.name === newCommandName);
    if (nameExists) {
      toastError('Run target name already exists');
      return;
    }
    setCommandTargets([...commandTargets, { name: newCommandName, command: newCommandCommand }]);
    setNewCommandName('');
    setNewCommandCommand('');
  };

  const removeCommand = async (name) => {
    const usage = checkTargetUsage(name);
    if (usage.inQuickLaunch || usage.inNudgenik) {
      const reasons = [
        usage.inQuickLaunch ? 'quick launch preset' : null,
        usage.inNudgenik ? 'nudgenik target' : null
      ].filter(Boolean).join(' and ');
      toastError(`Cannot remove "${name}" while used by ${reasons}.`);
      return;
    }
    const confirmed = await confirm('Remove command?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setCommandTargets(commandTargets.filter(c => c.name !== name));
    }
  };

  const addQuickLaunch = () => {
    const targetName = newQuickLaunchTarget.trim();
    if (!targetName) {
      toastError('Quick launch target is required');
      return;
    }
    const name = newQuickLaunchName.trim() || targetName;
    if (quickLaunch.some(q => q.name === name)) {
      toastError('Quick launch name already exists');
      return;
    }
    const promptable = promptableTargetNames.has(targetName);
    if (!promptable && !commandTargetNames.has(targetName)) {
      toastError('Quick launch target not found');
      return;
    }
    const promptValue = newQuickLaunchPrompt.trim();
    if (promptable && promptValue === '') {
      toastError('Prompt is required for promptable targets');
      return;
    }
    if (!promptable && promptValue !== '') {
      toastError('Prompt is not allowed for command targets');
      return;
    }
    const prompt = promptValue === '' ? null : promptValue;
    setQuickLaunch([...quickLaunch, { name, target: targetName, prompt }]);
    setNewQuickLaunchName('');
    setNewQuickLaunchTarget('');
    setNewQuickLaunchPrompt('');
  };

  const removeQuickLaunch = async (name) => {
    const confirmed = await confirm('Remove quick launch?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setQuickLaunch(quickLaunch.filter(q => q.name !== name));
    }
  };

  const [variantModal, setVariantModal] = useState(null);

  const openVariantModal = (variant, mode) => {
    if (mode === 'remove') {
      const usage = checkTargetUsage(variant.name);
      if (usage.inQuickLaunch || usage.inNudgenik) {
        const reasons = [
          usage.inQuickLaunch ? 'quick launch preset' : null,
          usage.inNudgenik ? 'nudgenik target' : null
        ].filter(Boolean).join(' and ');
        toastError(`Cannot remove variant "${variant.display_name}" while used by ${reasons}.`);
        return;
      }
    }
    const values = {};
    for (const key of variant.required_secrets || []) {
      values[key] = '';
    }
    setVariantModal({ variant, mode, values, error: '' });
  };

  const closeVariantModal = () => {
    setVariantModal(null);
  };

  const updateVariantValue = (key, value) => {
    setVariantModal((current) => ({
      ...current,
      values: { ...current.values, [key]: value }
    }));
  };

  const saveVariantModal = async () => {
    if (!variantModal) return;
    const { variant, mode, values } = variantModal;

    if (mode === 'remove') {
      try {
        await removeVariantSecrets(variant.name);
        await reloadVariants();
        success(`Removed secrets for ${variant.display_name}`);
        closeVariantModal();
      } catch (err) {
        setVariantModal((current) => ({
          ...current,
          error: err.message || 'Failed to remove variant secrets'
        }));
      }
      return;
    }

    const missingKey = (variant.required_secrets || []).find((key) => !values[key]?.trim());
    if (missingKey) {
      setVariantModal((current) => ({
        ...current,
        error: `Missing required secret ${missingKey}`
      }));
      return;
    }

    try {
      await configureVariantSecrets(variant.name, values);
      await reloadVariants();
      success(`Saved secrets for ${variant.display_name}`);
      closeVariantModal();
    } catch (err) {
      setVariantModal((current) => ({
        ...current,
        error: err.message || 'Failed to save variant secrets'
      }));
    }
  };

  const promptableTargetNames = new Set([
    ...detectedTargets.map((target) => target.name),
    ...promptableTargets.map((target) => target.name),
    ...availableVariants.filter((variant) => variant.configured).map((variant) => variant.name)
  ]);

  const commandTargetNames = new Set(commandTargets.map((target) => target.name));
  const nudgenikTargetMissing = nudgenikTarget.trim() !== '' && !promptableTargetNames.has(nudgenikTarget.trim());

  // Map wizard step to tab number - now 1:1 mapping
  const getTabForStep = (step) => step;

  const getCurrentTab = () => currentStep;

  // Check if each step is valid
  const stepValid = {
    1: workspacePath.trim().length > 0,
    2: repos.length > 0,
    3: true, // Run targets are optional
    4: true, // Variants are optional
    5: true, // Quick launch is optional
    6: true // Advanced step is always valid (has defaults)
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
          {isFirstRun ? 'Setup schmux' : 'Configuration'}
        </h1>
      </div>

      {isFirstRun && (
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

      {apiNeedsRestart && (
        <div className="banner banner--warning" style={{ marginBottom: 'var(--spacing-lg)' }}>
          <p style={{ margin: 0 }}>
            <strong>Restart required:</strong> Network access setting has changed. Restart the daemon for this setting to take effect: <code>./schmux stop && ./schmux start</code>
          </p>
        </div>
      )}

      {/* Steps navigation */}
      {isFirstRun ? (
        <div className="wizard__steps">
          {Array.from({ length: TOTAL_STEPS }, (_, i) => i + 1).map((stepNum) => {
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
            const isCurrent = stepNum === currentStep;
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
                Add the Git repositories that run targets will work on.
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
              <h2 className="wizard-step-content__title">Run Targets</h2>
              <p className="wizard-step-content__description">
                Configure user-supplied run targets. Detected tools appear automatically in the spawn wizard.
              </p>

              <h3 style={{ marginTop: 'var(--spacing-lg)' }}>Detected Run Targets (Read-only)</h3>
              <p className="section-hint">
                Official tools we detected on this machine and confirmed working. These are read-only.
              </p>
              {detectedTargets.length === 0 ? (
                <div className="empty-state-hint">
                  No detected run targets. Use the detect endpoint or restart the daemon to refresh detection.
                </div>
              ) : (
                <div className="item-list item-list--two-col">
                  {detectedTargets.map((target) => (
                    <div className="item-list__item" key={target.name}>
                      <div className="item-list__item-primary item-list__item-row">
                        <span className="item-list__item-name">{target.name}</span>
                        <span className="item-list__item-detail item-list__item-detail--wide">{target.command}</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <h3 style={{ marginTop: 'var(--spacing-lg)' }}>Promptable Targets</h3>
              <p className="section-hint">
                Custom coding agents that accept prompts. We append the prompt to the command.
              </p>
              {promptableTargets.length === 0 ? (
                <div className="empty-state-hint">
                  No promptable targets configured. Add one to enable custom promptable commands.
                </div>
              ) : (
                <div className="item-list item-list--two-col">
                  {promptableTargets.map((target) => (
                    <div className="item-list__item" key={target.name}>
                      <div className="item-list__item-primary item-list__item-row">
                        <span className="item-list__item-name">{target.name}</span>
                        <span className="item-list__item-detail item-list__item-detail--wide">{target.command}</span>
                      </div>
                      <button
                        className="btn btn--sm btn--danger"
                        onClick={() => removePromptableTarget(target.name)}
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
                    value={newPromptableName}
                    onChange={(e) => setNewPromptableName(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addPromptableTarget()}
                  />
                  <input
                    type="text"
                    className="input"
                    placeholder="Command (prompt is appended as last arg)"
                    value={newPromptableCommand}
                    onChange={(e) => setNewPromptableCommand(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addPromptableTarget()}
                  />
                </div>
                <button type="button" className="btn btn--sm" onClick={addPromptableTarget}>Add</button>
              </div>

              <h3 style={{ marginTop: 'var(--spacing-lg)' }}>Command Targets</h3>
              <p className="section-hint">
                Shell commands you want to run quickly, like launching a terminal or starting the app.
              </p>
              {commandTargets.length === 0 ? (
                <div className="empty-state-hint">
                  No command targets configured. These run without prompts.
                </div>
              ) : (
                <div className="item-list item-list--two-col">
                  {commandTargets.map((cmd) => (
                    <div className="item-list__item" key={cmd.name}>
                      <div className="item-list__item-primary item-list__item-row">
                        <span className="item-list__item-name">{cmd.name}</span>
                        <span className="item-list__item-detail item-list__item-detail--wide">{cmd.command}</span>
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

          {currentTab === 4 && (
            <div className="wizard-step-content" data-step="4">
              <h2 className="wizard-step-content__title">Variants</h2>
              <p className="wizard-step-content__description">
                Add secrets to enable variants for quick launch and spawning.
              </p>

              {availableVariants.length === 0 ? (
                <div className="empty-state-hint">
                  No variants available. Install the base tool to enable variants.
                </div>
              ) : (
                <div className="item-list">
                  {availableVariants.map((variant) => (
                    <div className="item-list__item" key={variant.name}>
                      <div className="item-list__item-primary">
                        <span className="item-list__item-name">{variant.display_name}</span>
                        <span className="item-list__item-detail">
                          {variant.name} · base: {variant.base_tool}
                        </span>
                        {variant.usage_url && (
                          <a
                            className="item-list__item-detail link"
                            href={variant.usage_url}
                            target="_blank"
                            rel="noreferrer"
                          >
                            {variant.usage_url}
                          </a>
                        )}
                      </div>
                      {variant.configured ? (
                        <div style={{ display: 'flex', gap: 'var(--spacing-sm)' }}>
                          <button
                            className="btn btn--primary"
                            onClick={() => openVariantModal(variant, 'update')}
                          >
                            Update
                          </button>
                          <button
                            className="btn btn--danger"
                            onClick={() => openVariantModal(variant, 'remove')}
                          >
                            Remove
                          </button>
                        </div>
                      ) : (
                        <button
                          className="btn btn--primary"
                          onClick={() => openVariantModal(variant, 'add')}
                        >
                          Add
                        </button>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {currentTab === 5 && (
            <div className="wizard-step-content" data-step="5">
              <h2 className="wizard-step-content__title">Quick Launch</h2>
              <p className="wizard-step-content__description">
                Quick launch runs a target with a preset prompt. Promptable targets require a prompt.
              </p>

              <div className="quick-launch-editor">
                {quickLaunch.length === 0 ? (
                  <div className="quick-launch-editor__empty">
                    No quick launch presets yet.
                  </div>
                ) : (
                  <div className="quick-launch-editor__list">
                    {quickLaunch.map((preset) => (
                      <div className="quick-launch-editor__item" key={preset.name}>
                        <div className="quick-launch-editor__item-main">
                          <span className="quick-launch-editor__item-name">{preset.name}</span>
                          <span className="quick-launch-editor__item-detail">
                            {preset.target}{preset.prompt ? ` — ${preset.prompt}` : ''}
                          </span>
                        </div>
                        <button
                          className="btn btn--danger"
                          onClick={() => removeQuickLaunch(preset.name)}
                        >
                          Remove
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                <div className="quick-launch-editor__form">
                  <div className="quick-launch-editor__row">
                    <input
                      type="text"
                      className="input quick-launch-editor__name"
                      placeholder="Preset name (optional)"
                      value={newQuickLaunchName}
                      onChange={(e) => setNewQuickLaunchName(e.target.value)}
                    />
                    <select
                      className="input quick-launch-editor__select"
                      value={newQuickLaunchTarget}
                      onChange={(e) => {
                        const value = e.target.value;
                        setNewQuickLaunchTarget(value);
                        if (commandTargetNames.has(value)) {
                          setNewQuickLaunchPrompt('');
                        }
                      }}
                    >
                      <option value="">Select target...</option>
                      <optgroup label="Promptable Targets">
                        {[
                          ...detectedTargets.map((target) => ({ value: target.name, label: target.name })),
                          ...availableVariants.filter((variant) => variant.configured).map((variant) => ({
                            value: variant.name,
                            label: variant.display_name
                          })),
                          ...promptableTargets.map((target) => ({ value: target.name, label: target.name }))
                        ].map((option) => (
                          <option key={option.value} value={option.value}>{option.label}</option>
                        ))}
                      </optgroup>
                      <optgroup label="Command Targets">
                        {commandTargets.map((target) => (
                          <option key={target.name} value={target.name}>{target.name}</option>
                        ))}
                      </optgroup>
                    </select>
                    <button type="button" className="btn btn--primary" onClick={addQuickLaunch}>Add</button>
                  </div>

                  {promptableTargetNames.has(newQuickLaunchTarget) && (
                    <div className="quick-launch-editor__prompt">
                      <label className="form-group__label">Preset prompt</label>
                      <textarea
                        className="input quick-launch-editor__prompt-input"
                        placeholder="Prompt"
                        value={newQuickLaunchPrompt}
                        onChange={(e) => setNewQuickLaunchPrompt(e.target.value)}
                      />
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {currentTab === 6 && (
            <div className="wizard-step-content" data-step="6">
              <h2 className="wizard-step-content__title">Advanced Settings</h2>
              <p className="wizard-step-content__description">
                Terminal dimensions and internal timing intervals. You can leave these as defaults unless you have specific needs.
              </p>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">NudgeNik</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label className="form-group__label">Target</label>
                    <select
                      className="input"
                      value={nudgenikTarget}
                      onChange={(e) => setNudgenikTarget(e.target.value)}
                    >
                      <option value="">Auto (detected claude)</option>
                      <optgroup label="Detected Tools">
                        {detectedTargets.map((target) => (
                          <option key={target.name} value={target.name}>{target.name}</option>
                        ))}
                      </optgroup>
                      <optgroup label="Variants">
                        {availableVariants.filter((variant) => variant.configured).map((variant) => (
                          <option key={variant.name} value={variant.name}>{variant.display_name}</option>
                        ))}
                      </optgroup>
                      <optgroup label="User Promptable">
                        {promptableTargets.map((target) => (
                          <option key={target.name} value={target.name}>{target.name}</option>
                        ))}
                      </optgroup>
                    </select>
                    <p className="form-group__hint">
                      Used when schmuX asks NudgeNik for session feedback. Must be promptable.
                    </p>
                    {nudgenikTargetMissing && (
                      <p className="form-group__error">Selected target is not available or not promptable.</p>
                    )}
                  </div>
                </div>
              </div>

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

                    <div className="form-group">
                      <label className="form-group__label">Bootstrap Lines</label>
                      <input
                        type="number"
                        className="input"
                        min="1"
                        value={terminalBootstrapLines}
                        onChange={(e) => setTerminalBootstrapLines(e.target.value)}
                      />
                      <p className="form-group__hint">Lines to send on initial WebSocket connection (default: 20000)</p>
                    </div>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Network Access</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label className="form-group__label">Dashboard Access</label>
                    <div style={{ display: 'flex', gap: 'var(--spacing-md)', alignItems: 'center', fontSize: '0.9rem' }}>
                      <label style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)', cursor: 'pointer', fontSize: 'inherit' }}>
                        <input
                          type="radio"
                          name="networkAccess"
                          checked={!networkAccess}
                          onChange={() => setNetworkAccess(false)}
                        />
                        <span>Local access only</span>
                      </label>
                      <label style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)', cursor: 'pointer', fontSize: 'inherit' }}>
                        <input
                          type="radio"
                          name="networkAccess"
                          checked={networkAccess}
                          onChange={() => setNetworkAccess(true)}
                        />
                        <span>Local network access</span>
                      </label>
                    </div>
                    <p className="form-group__hint">
                      {!networkAccess
                        ? 'Dashboard accessible only from this computer (localhost).'
                        : 'Dashboard accessible from other devices on your local network.'}
                    </p>
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
              {stepErrors[6] && (
                <p className="form-group__error">{stepErrors[6]}</p>
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
            {!isFirstRun && currentStep === 1 && (
              <span className="wizard__hint">Use tabs above to navigate between sections</span>
            )}
          </div>
          <div className="wizard__actions-right">
            <button
              className="btn btn--primary"
              onClick={async () => {
                if (isFirstRun && currentStep < TOTAL_STEPS) {
                  nextStep();
                } else {
                  const saved = await saveCurrentStep();
                  if (saved && isFirstRun) {
                    completeFirstRun();
                    setShowSetupComplete(true);
                  }
                }
              }}
              disabled={saving}
            >
              {saving ? 'Saving...' : isFirstRun ?
                (currentStep === TOTAL_STEPS ? 'Finish Setup' : 'Next →') :
                'Save Changes'
              }
            </button>
          </div>
        </div>
      </div>

      {showSetupComplete && (
        <SetupCompleteModal
          onClose={() => navigate('/spawn')}
        />
      )}

      {variantModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="variant-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeVariantModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="variant-modal-title">
                {variantModal.mode === 'remove'
                  ? `Remove ${variantModal.variant.display_name}`
                  : `${variantModal.mode === 'add' ? 'Add' : 'Update'} ${variantModal.variant.display_name}`}
              </h2>
            </div>
            <div className="modal__body">
              {variantModal.mode === 'remove' ? (
                <p>Remove stored secrets for this variant?</p>
              ) : (
                <>
                  {(variantModal.variant.required_secrets || []).map((key, index) => (
                    <div className="form-group" key={key}>
                      <label className="form-group__label">{key}</label>
                      <input
                        type="password"
                        className="input"
                        autoFocus={index === 0}
                        value={variantModal.values[key] || ''}
                        onChange={(e) => updateVariantValue(key, e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') saveVariantModal();
                        }}
                      />
                    </div>
                  ))}
                </>
              )}
              {variantModal.error && (
                <p className="form-group__error" style={{ marginTop: 'var(--spacing-sm)' }}>
                  {variantModal.error}
                </p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeVariantModal}>Cancel</button>
              <button
                className={`btn ${variantModal.mode === 'remove' ? 'btn--danger' : 'btn--primary'}`}
                onClick={saveVariantModal}
              >
                {variantModal.mode === 'remove'
                  ? 'Remove'
                  : variantModal.mode === 'add'
                    ? 'Add'
                    : 'Update'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
