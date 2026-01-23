import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { getConfig, updateConfig, getVariants, configureVariantSecrets, removeVariantSecrets, getOverlays, getBuiltinQuickLaunch, getAuthSecretsStatus, saveAuthSecrets, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import SetupCompleteModal from '../components/SetupCompleteModal';
import { CONFIG_UPDATED_KEY } from '../lib/constants';
import type {
  BuiltinQuickLaunchCookbook,
  ConfigResponse,
  ConfigUpdateRequest,
  OverlayInfo,
  QuickLaunchPreset,
  RepoResponse,
  RunTargetResponse,
  VariantResponse,
} from '../lib/types';

const TOTAL_STEPS = 5;
const TABS = ['Workspace', 'Run Targets', 'Variants', 'Quick Launch', 'Advanced'];

// Map step number to URL slug
const TAB_SLUGS = ['workspace', 'targets', 'variants', 'quicklaunch', 'advanced'];

// Helper: step number -> slug
const stepToSlug = (step: number) => TAB_SLUGS[step - 1];

// Helper: slug -> step number
const slugToStep = (slug: string | null) => {
  const index = TAB_SLUGS.indexOf(slug);
  return index >= 0 ? index + 1 : 1;
};

type ConfigSnapshot = {
  workspacePath: string;
  repos: RepoResponse[];
  promptableTargets: RunTargetResponse[];
  commandTargets: RunTargetResponse[];
  quickLaunch: QuickLaunchPreset[];
  nudgenikTarget: string;
  terminalWidth: string;
  terminalHeight: string;
  terminalSeedLines: string;
  terminalBootstrapLines: string;
  mtimePollInterval: number;
  dashboardPollInterval: number;
  viewedBuffer: number;
  nudgenikSeenInterval: number;
  gitStatusPollInterval: number;
  gitCloneTimeout: number;
  gitStatusTimeout: number;
  xtermQueryTimeout: number;
  xtermOperationTimeout: number;
  maxLogSizeMB: number;
  rotatedLogSizeMB: number;
  networkAccess: boolean;
  authEnabled: boolean;
  authProvider: string;
  authPublicBaseURL: string;
  authSessionTTLMinutes: number;
  authTlsCertPath: string;
  authTlsKeyPath: string;
};

type VariantModalState = {
  variant: VariantResponse;
  mode: 'add' | 'remove' | 'update';
  values: Record<string, string>;
  error: string;
} | null;

type RunTargetEditModalState = {
  target: RunTargetResponse;
  command: string;
  error: string;
} | null;

type QuickLaunchEditModalState = {
  item: QuickLaunchPreset;
  prompt: string;
  isCommandTarget: boolean;
  error: string;
} | null;

export default function ConfigPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { isNotConfigured, isFirstRun, completeFirstRun, reloadConfig } = useConfig();
  const { confirm } = useModal();
  const [showSetupComplete, setShowSetupComplete] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [warning, setWarning] = useState('');
  const { success, error: toastError } = useToast();

  // Wizard state
  const [currentStep, setCurrentStep] = useState(() => {
    // Initialize from URL on mount
    const tabFromUrl = searchParams.get('tab');
    return tabFromUrl ? slugToStep(tabFromUrl) : 1;
  });

  // Sync currentStep with URL (only in non-wizard mode)
  useEffect(() => {
    if (!isFirstRun) {
      const slug = stepToSlug(currentStep);
      setSearchParams({ tab: slug });
    }
  }, [currentStep, isFirstRun, setSearchParams]);

  // Browser close/refresh warning
  useEffect(() => {
    const handleBeforeUnload = (e) => {
      if (!isFirstRun && hasChanges()) {
        e.preventDefault();
        e.returnValue = '';
      }
    };

    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [isFirstRun]); // Dependency doesn't include hasChanges values - function reads current state

  // Form state
  const [workspacePath, setWorkspacePath] = useState('');
  const [repos, setRepos] = useState<RepoResponse[]>([]);
  const [promptableTargets, setPromptableTargets] = useState<RunTargetResponse[]>([]);
  const [commandTargets, setCommandTargets] = useState<RunTargetResponse[]>([]);
  const [detectedTargets, setDetectedTargets] = useState<RunTargetResponse[]>([]);
  const [quickLaunch, setQuickLaunch] = useState<QuickLaunchPreset[]>([]);
  const [builtinQuickLaunch, setBuiltinQuickLaunch] = useState<BuiltinQuickLaunchCookbook[]>([]); // Built-in quick launch items
  const [availableVariants, setAvailableVariants] = useState<VariantResponse[]>([]);
  const [nudgenikTarget, setNudgenikTarget] = useState('');

  // Terminal state
  const [terminalWidth, setTerminalWidth] = useState('120');
  const [terminalHeight, setTerminalHeight] = useState('40');
  const [terminalSeedLines, setTerminalSeedLines] = useState('100');
  const [terminalBootstrapLines, setTerminalBootstrapLines] = useState('20000');

  // Advanced settings state
  const [mtimePollInterval, setMtimePollInterval] = useState(5000);
  const [dashboardPollInterval, setDashboardPollInterval] = useState(5000);
  const [viewedBuffer, setViewedBuffer] = useState(5000);
  const [nudgenikSeenInterval, setNudgenikSeenInterval] = useState(2000);
  const [gitStatusPollInterval, setGitStatusPollInterval] = useState(10000);
  const [gitCloneTimeout, setGitCloneTimeout] = useState(300000);
  const [gitStatusTimeout, setGitStatusTimeout] = useState(30000);
  const [xtermQueryTimeout, setXtermQueryTimeout] = useState(5000);
  const [xtermOperationTimeout, setXtermOperationTimeout] = useState(10000);
  const [maxLogSizeMB, setMaxLogSizeMB] = useState(50);
  const [rotatedLogSizeMB, setRotatedLogSizeMB] = useState(1);
  const [networkAccess, setNetworkAccess] = useState(false);
  const [authEnabled, setAuthEnabled] = useState(false);
  const [authProvider, setAuthProvider] = useState('github');
  const [authPublicBaseURL, setAuthPublicBaseURL] = useState('');
  const [authSessionTTLMinutes, setAuthSessionTTLMinutes] = useState(1440);
  const [authTlsCertPath, setAuthTlsCertPath] = useState('');
  const [authTlsKeyPath, setAuthTlsKeyPath] = useState('');
  const [authClientIdSet, setAuthClientIdSet] = useState(false);
  const [authClientSecretSet, setAuthClientSecretSet] = useState(false);
  const [authWarnings, setAuthWarnings] = useState<string[]>([]);
  const [apiNeedsRestart, setApiNeedsRestart] = useState(false);

  // Overlays state
  const [overlays, setOverlays] = useState<OverlayInfo[]>([]);
  const [loadingOverlays, setLoadingOverlays] = useState(true);

  // Original config for change detection (in non-wizard mode)
  const [originalConfig, setOriginalConfig] = useState<ConfigSnapshot | null>(null);

  // Check if current config differs from original
  const hasChanges = () => {
    if (isFirstRun || !originalConfig) return false;

    // Compare all relevant fields
    const current = {
      workspacePath,
      repos,
      promptableTargets,
      commandTargets,
      quickLaunch,
      nudgenikTarget,
      terminalWidth,
      terminalHeight,
      terminalSeedLines,
      terminalBootstrapLines,
      mtimePollInterval,
      dashboardPollInterval,
      viewedBuffer,
      nudgenikSeenInterval,
      gitStatusPollInterval,
      gitCloneTimeout,
      gitStatusTimeout,
      xtermQueryTimeout,
      xtermOperationTimeout,
      maxLogSizeMB,
      rotatedLogSizeMB,
      networkAccess,
      authEnabled,
      authProvider,
      authPublicBaseURL,
      authSessionTTLMinutes,
      authTlsCertPath,
      authTlsKeyPath,
    };

    // Deep comparison for arrays
    const arraysMatch = (a: unknown[], b: unknown[]) => {
      if (a.length !== b.length) return false;
      return a.every((item, i) => JSON.stringify(item) === JSON.stringify(b[i]));
    };

    return (
      current.workspacePath !== originalConfig.workspacePath ||
      !arraysMatch(current.repos, originalConfig.repos) ||
      !arraysMatch(current.promptableTargets, originalConfig.promptableTargets) ||
      !arraysMatch(current.commandTargets, originalConfig.commandTargets) ||
      !arraysMatch(current.quickLaunch, originalConfig.quickLaunch) ||
      current.nudgenikTarget !== originalConfig.nudgenikTarget ||
      current.terminalWidth !== originalConfig.terminalWidth ||
      current.terminalHeight !== originalConfig.terminalHeight ||
      current.terminalSeedLines !== originalConfig.terminalSeedLines ||
      current.terminalBootstrapLines !== originalConfig.terminalBootstrapLines ||
      current.mtimePollInterval !== originalConfig.mtimePollInterval ||
      current.dashboardPollInterval !== originalConfig.dashboardPollInterval ||
      current.viewedBuffer !== originalConfig.viewedBuffer ||
      current.nudgenikSeenInterval !== originalConfig.nudgenikSeenInterval ||
      current.gitStatusPollInterval !== originalConfig.gitStatusPollInterval ||
      current.gitCloneTimeout !== originalConfig.gitCloneTimeout ||
      current.gitStatusTimeout !== originalConfig.gitStatusTimeout ||
      current.xtermQueryTimeout !== originalConfig.xtermQueryTimeout ||
      current.xtermOperationTimeout !== originalConfig.xtermOperationTimeout ||
      current.maxLogSizeMB !== originalConfig.maxLogSizeMB ||
      current.rotatedLogSizeMB !== originalConfig.rotatedLogSizeMB ||
      current.networkAccess !== originalConfig.networkAccess ||
      current.authEnabled !== originalConfig.authEnabled ||
      current.authProvider !== originalConfig.authProvider ||
      current.authPublicBaseURL !== originalConfig.authPublicBaseURL ||
      current.authSessionTTLMinutes !== originalConfig.authSessionTTLMinutes ||
      current.authTlsCertPath !== originalConfig.authTlsCertPath ||
      current.authTlsKeyPath !== originalConfig.authTlsKeyPath
    );
  };

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
  const [selectedCookbookTemplate, setSelectedCookbookTemplate] = useState<BuiltinQuickLaunchCookbook | null>(null); // Track which cookbook template is being added

  // Validation state per step
  const [stepErrors, setStepErrors] = useState<Record<number, string | null>>({ 1: null, 2: null, 3: null, 4: null, 5: null });

  const localAuthWarnings: string[] = [];
  if (authEnabled) {
    if (!authPublicBaseURL.trim()) {
      localAuthWarnings.push('Public base URL is required when auth is enabled.');
    } else if (!authPublicBaseURL.startsWith('https://') && !authPublicBaseURL.startsWith('http://localhost')) {
      localAuthWarnings.push('Public base URL must be https (http://localhost allowed).');
    }
    if (!authTlsCertPath.trim()) {
      localAuthWarnings.push('TLS cert path is required when auth is enabled.');
    }
    if (!authTlsKeyPath.trim()) {
      localAuthWarnings.push('TLS key path is required when auth is enabled.');
    }
    if (!authClientIdSet || !authClientSecretSet) {
      localAuthWarnings.push('GitHub client credentials are not configured.');
    }
  }
  const combinedAuthWarnings = Array.from(new Set([...localAuthWarnings, ...authWarnings]));

  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setError('');
      try {
        const data: ConfigResponse = await getConfig();
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

        setMtimePollInterval(data.xterm?.mtime_poll_interval_ms || 5000);
        setDashboardPollInterval(data.sessions?.dashboard_poll_interval_ms || 5000);
        setViewedBuffer(data.nudgenik?.viewed_buffer_ms || 5000);
        setNudgenikSeenInterval(data.nudgenik?.seen_interval_ms || 2000);
        setGitStatusPollInterval(data.sessions?.git_status_poll_interval_ms || 10000);
        setGitCloneTimeout(data.sessions?.git_clone_timeout_ms || 300000);
        setGitStatusTimeout(data.sessions?.git_status_timeout_ms || 30000);
        setXtermQueryTimeout(data.xterm?.query_timeout_ms || 5000);
        setXtermOperationTimeout(data.xterm?.operation_timeout_ms || 10000);
        setMaxLogSizeMB(data.xterm?.max_log_size_mb || 50);
        setRotatedLogSizeMB(data.xterm?.rotated_log_size_mb || 1);
        const netAccess = data.network?.bind_address === '0.0.0.0';
        setNetworkAccess(netAccess);
        setAuthEnabled(data.access_control?.enabled || false);
        setAuthProvider(data.access_control?.provider || 'github');
        setAuthPublicBaseURL(data.network?.public_base_url || '');
        setAuthSessionTTLMinutes(data.access_control?.session_ttl_minutes || 1440);
        setAuthTlsCertPath(data.network?.tls?.cert_path || '');
        setAuthTlsKeyPath(data.network?.tls?.key_path || '');
        setAuthWarnings([]);
        setApiNeedsRestart(data.needs_restart || false);

        // Set original config for change detection (non-wizard mode)
        if (!isFirstRun) {
          setOriginalConfig({
            workspacePath: data.workspace_path || '',
            repos: data.repos || [],
            promptableTargets: promptableItems,
            commandTargets: commandItems,
            quickLaunch: data.quick_launch || [],
            nudgenikTarget: data.nudgenik?.target || '',
            terminalWidth: String(data.terminal?.width || 120),
            terminalHeight: String(data.terminal?.height || 40),
            terminalSeedLines: String(data.terminal?.seed_lines || 100),
            terminalBootstrapLines: String(data.terminal?.bootstrap_lines || 20000),
            mtimePollInterval: data.xterm?.mtime_poll_interval_ms || 5000,
            dashboardPollInterval: data.sessions?.dashboard_poll_interval_ms || 5000,
            viewedBuffer: data.nudgenik?.viewed_buffer_ms || 5000,
            nudgenikSeenInterval: data.nudgenik?.seen_interval_ms || 2000,
            gitStatusPollInterval: data.sessions?.git_status_poll_interval_ms || 10000,
            gitCloneTimeout: data.sessions?.git_clone_timeout_ms || 300000,
            gitStatusTimeout: data.sessions?.git_status_timeout_ms || 30000,
            xtermQueryTimeout: data.xterm?.query_timeout_ms || 5000,
            xtermOperationTimeout: data.xterm?.operation_timeout_ms || 10000,
            maxLogSizeMB: data.xterm?.max_log_size_mb || 50,
            rotatedLogSizeMB: data.xterm?.rotated_log_size_mb || 1,
            networkAccess: netAccess,
            authEnabled: data.access_control?.enabled || false,
            authProvider: data.access_control?.provider || 'github',
            authPublicBaseURL: data.network?.public_base_url || '',
            authSessionTTLMinutes: data.access_control?.session_ttl_minutes || 1440,
            authTlsCertPath: data.network?.tls?.cert_path || '',
            authTlsKeyPath: data.network?.tls?.key_path || '',
          });
        }

        const variantData = await getVariants();
        if (active) {
          setAvailableVariants(variantData.variants || []);
        }

        const authStatus = await getAuthSecretsStatus();
        if (active) {
          setAuthClientIdSet(!!authStatus.client_id_set);
          setAuthClientSecretSet(!!authStatus.client_secret_set);
        }
      } catch (err) {
        if (!active) return;
        const message = err instanceof Error ? err.message : 'Failed to load config';
        setError(message);
      } finally {
        if (active) setLoading(false);
      }
    };

    load();
    return () => { active = false };
  }, []);

  // Load overlays data
  useEffect(() => {
    let active = true;

    const loadOverlays = async () => {
      setLoadingOverlays(true);
      try {
        const data = await getOverlays();
        if (!active) return;
        setOverlays(data.overlays || []);
      } catch (err) {
        if (!active) return;
        console.error('Failed to load overlays:', err);
        // Don't show error for overlays - it's a non-critical feature
      } finally {
        if (active) setLoadingOverlays(false);
      }
    };

    loadOverlays();
    return () => { active = false };
  }, []);

  // Load built-in quick launch templates
  useEffect(() => {
    let active = true;

    const loadBuiltinQuickLaunch = async () => {
      try {
        const data = await getBuiltinQuickLaunch();
        if (active) {
          setBuiltinQuickLaunch(data || []);
        }
      } catch (err) {
        if (!active) return;
        console.warn('Failed to load built-in quick launch templates:', err);
        // Continue without built-in templates
      }
    };

    loadBuiltinQuickLaunch();
    return () => { active = false };
  }, []);

  const reloadVariants = async () => {
    try {
      const variantData = await getVariants();
      setAvailableVariants(variantData.variants || []);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to load variants'));
    }
  };

  // Validation for each step - returns true if valid, also sets error state
  const validateStep = (step: number) => {
    let error = null;

    if (step === 1) {
      if (!workspacePath.trim()) {
        error = 'Workspace path is required';
      } else if (repos.length === 0) {
        error = 'Add at least one repository';
      }
    } else if (step === 2) {
      // Run targets are optional
    } else if (step === 3) {
      // Variants are optional
    } else if (step === 4) {
      // Quick launch is optional
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

      const runTargets = [
        ...promptableTargets.map(t => ({ ...t, type: 'promptable' })),
        ...commandTargets.map(t => ({ ...t, type: 'command' }))
      ];

      const updateRequest: ConfigUpdateRequest = {
        workspace_path: workspacePath,
        terminal: { width, height, seed_lines: seedLines, bootstrap_lines: parseInt(terminalBootstrapLines) },
        repos: repos,
        run_targets: runTargets,
        quick_launch: quickLaunch,
        nudgenik: {
          target: nudgenikTarget || '',
          viewed_buffer_ms: viewedBuffer,
          seen_interval_ms: nudgenikSeenInterval,
        },
        sessions: {
          dashboard_poll_interval_ms: dashboardPollInterval,
          git_status_poll_interval_ms: gitStatusPollInterval,
          git_clone_timeout_ms: gitCloneTimeout,
          git_status_timeout_ms: gitStatusTimeout,
        },
        xterm: {
          mtime_poll_interval_ms: mtimePollInterval,
          query_timeout_ms: xtermQueryTimeout,
          operation_timeout_ms: xtermOperationTimeout,
          max_log_size_mb: maxLogSizeMB,
          rotated_log_size_mb: rotatedLogSizeMB,
        },
        network: {
          bind_address: networkAccess ? '0.0.0.0' : '127.0.0.1',
          public_base_url: authPublicBaseURL,
          tls: {
            cert_path: authTlsCertPath,
            key_path: authTlsKeyPath,
          },
        },
        access_control: {
          enabled: authEnabled,
          provider: authProvider,
          session_ttl_minutes: authSessionTTLMinutes,
        },
      };

      const result = await updateConfig(updateRequest);
      reloadConfig();
      // Notify other tabs that config changed
      localStorage.setItem(CONFIG_UPDATED_KEY, Date.now().toString());
      setAuthWarnings(result.warnings || []);

      // Reload config to get updated needs_restart flag from server
      const reloaded = await getConfig();
      setApiNeedsRestart(reloaded.needs_restart || false);

      // Update original config after successful save (non-wizard mode)
      if (!isFirstRun) {
        setOriginalConfig({
          workspacePath,
          repos,
          promptableTargets,
          commandTargets,
          quickLaunch,
          nudgenikTarget,
          terminalWidth,
          terminalHeight,
          terminalSeedLines,
          terminalBootstrapLines,
          mtimePollInterval,
          dashboardPollInterval,
          viewedBuffer,
          nudgenikSeenInterval,
          gitStatusPollInterval,
          gitCloneTimeout,
          gitStatusTimeout,
          xtermQueryTimeout,
          xtermOperationTimeout,
          maxLogSizeMB,
          rotatedLogSizeMB,
          networkAccess,
          authEnabled,
          authProvider,
          authPublicBaseURL,
          authSessionTTLMinutes,
          authTlsCertPath,
          authTlsKeyPath,
        });
      }

      if (result.warning && !isFirstRun) {
        setWarning(result.warning);
      } else if (!isFirstRun) {
        success('Configuration saved');
      }
      return true;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to save config';
      toastError(message);
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
    setPromptableTargets([...promptableTargets, { name: newPromptableName, command: newPromptableCommand, type: 'promptable', source: 'user' }]);
    setNewPromptableName('');
    setNewPromptableCommand('');
  };

  const checkTargetUsage = (targetName) => {
    const inQuickLaunch = quickLaunch.some((item) => item.target === targetName);
    const inNudgenik = nudgenikTarget && nudgenikTarget === targetName;
    return { inQuickLaunch, inNudgenik };
  };

  const removePromptableTarget = async (name) => {
    const usage = checkTargetUsage(name);
    if (usage.inQuickLaunch || usage.inNudgenik) {
      const reasons = [
        usage.inQuickLaunch ? 'quick launch item' : null,
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
    setCommandTargets([...commandTargets, { name: newCommandName, command: newCommandCommand, type: 'command', source: 'user' }]);
    setNewCommandName('');
    setNewCommandCommand('');
  };

  const removeCommand = async (name) => {
    const usage = checkTargetUsage(name);
    if (usage.inQuickLaunch || usage.inNudgenik) {
      const reasons = [
        usage.inQuickLaunch ? 'quick launch item' : null,
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

  const [variantModal, setVariantModal] = useState<VariantModalState>(null);
  const [showWorkspaceModal, setShowWorkspaceModal] = useState(false);
  const [workspaceModalPath, setWorkspaceModalPath] = useState('');
  const [runTargetEditModal, setRunTargetEditModal] = useState<RunTargetEditModalState>(null);
  const [quickLaunchEditModal, setQuickLaunchEditModal] = useState<QuickLaunchEditModalState>(null);

  const openWorkspaceModal = () => {
    setWorkspaceModalPath(workspacePath);
    setShowWorkspaceModal(true);
  };

  const closeWorkspaceModal = () => {
    setShowWorkspaceModal(false);
    setWorkspaceModalPath('');
  };

  const saveWorkspaceModal = () => {
    setWorkspacePath(workspaceModalPath);
    setShowWorkspaceModal(false);
    if (workspaceModalPath.trim()) {
      setStepErrors(prev => ({ ...prev, 1: null }));
    }
  };

  const openVariantModal = (variant: VariantResponse, mode: 'add' | 'remove' | 'update') => {
    if (mode === 'remove') {
      const usage = checkTargetUsage(variant.name);
      if (usage.inQuickLaunch || usage.inNudgenik) {
        const reasons = [
          usage.inQuickLaunch ? 'quick launch item' : null,
          usage.inNudgenik ? 'nudgenik target' : null
        ].filter(Boolean).join(' and ');
        toastError(`Cannot remove variant "${variant.display_name}" while used by ${reasons}.`);
        return;
      }
    }
    const values: Record<string, string> = {};
    for (const key of variant.required_secrets || []) {
      values[key] = '';
    }
    setVariantModal({ variant, mode, values, error: '' });
  };

  const closeVariantModal = () => {
    setVariantModal(null);
  };

  const updateVariantValue = (key: string, value: string) => {
    setVariantModal((current) => {
      if (!current) return current;
      return {
        ...current,
        values: { ...current.values, [key]: value }
      };
    });
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
        setVariantModal((current) => {
          if (!current) return current;
          return {
            ...current,
            error: getErrorMessage(err, 'Failed to remove variant secrets')
          };
        });
      }
      return;
    }

    const missingKey = (variant.required_secrets || []).find((key) => !values[key]?.trim());
    if (missingKey) {
      setVariantModal((current) => {
        if (!current) return current;
        return {
          ...current,
          error: `Missing required secret ${missingKey}`
        };
      });
      return;
    }

    try {
      await configureVariantSecrets(variant.name, values);
      await reloadVariants();
      success(`Saved secrets for ${variant.display_name}`);
      closeVariantModal();
    } catch (err) {
      setVariantModal((current) => {
        if (!current) return current;
        return {
          ...current,
          error: getErrorMessage(err, 'Failed to save variant secrets')
        };
      });
    }
  };

  // Run target edit modal
  const openRunTargetEditModal = (target: RunTargetResponse) => {
    setRunTargetEditModal({ target, command: target.command, error: '' });
  };

  const closeRunTargetEditModal = () => {
    setRunTargetEditModal(null);
  };

  const saveRunTargetEditModal = () => {
    if (!runTargetEditModal) return;
    const { target, command } = runTargetEditModal;

    if (!command.trim()) {
      setRunTargetEditModal(current => current ? { ...current, error: 'Command is required' } : null);
      return;
    }

    if (target.type === 'promptable') {
      setPromptableTargets(promptableTargets.map(t =>
        t.name === target.name ? { ...t, command } : t
      ));
    } else {
      setCommandTargets(commandTargets.map(t =>
        t.name === target.name ? { ...t, command } : t
      ));
    }
    closeRunTargetEditModal();
  };

  // Quick launch edit modal
  const openQuickLaunchEditModal = (item: QuickLaunchPreset) => {
    const isCommandTarget = commandTargetNames.has(item.target);
    // For command targets, prefill with the underlying target's command
    let initialPrompt = item.prompt || '';
    if (isCommandTarget) {
      const commandTarget = commandTargets.find(t => t.name === item.target);
      if (commandTarget) {
        initialPrompt = commandTarget.command;
      }
    }
    setQuickLaunchEditModal({
      item,
      prompt: initialPrompt,
      isCommandTarget,
      error: ''
    });
  };

  const closeQuickLaunchEditModal = () => {
    setQuickLaunchEditModal(null);
  };

  const saveQuickLaunchEditModal = () => {
    if (!quickLaunchEditModal) return;
    const { item, prompt, isCommandTarget } = quickLaunchEditModal;
    const target = item.target;

    const isPromptable = promptableTargetNames.has(target);
    if (isPromptable && !prompt.trim()) {
      setQuickLaunchEditModal(current => current ? { ...current, error: 'Prompt is required for promptable targets' } : null);
      return;
    }

    // For command target items, require non-empty command and update the underlying run target
    if (isCommandTarget) {
      if (!prompt.trim()) {
        setQuickLaunchEditModal(current => current ? { ...current, error: 'Command is required for command targets' } : null);
        return;
      }
      setCommandTargets(commandTargets.map(t =>
        t.name === target ? { ...t, command: prompt } : t
      ));
    }

    // Update the quick launch item
    setQuickLaunch(quickLaunch.map(p =>
      p.name === item.name
        ? { name: item.name, target, prompt: isPromptable ? prompt : null }
        : p
    ));
    closeQuickLaunchEditModal();
  };

  // Auth secrets modal
  const [authSecretsModal, setAuthSecretsModal] = useState<{
    clientId: string;
    clientSecret: string;
    error: string;
  } | null>(null);

  const openAuthSecretsModal = () => {
    setAuthSecretsModal({ clientId: '', clientSecret: '', error: '' });
  };

  const closeAuthSecretsModal = () => {
    setAuthSecretsModal(null);
  };

  const saveAuthSecretsModal = async () => {
    if (!authSecretsModal) return;
    const { clientId, clientSecret } = authSecretsModal;

    if (!clientId.trim() || !clientSecret.trim()) {
      setAuthSecretsModal(current => current ? { ...current, error: 'Both client ID and client secret are required' } : null);
      return;
    }

    try {
      await saveAuthSecrets({ client_id: clientId.trim(), client_secret: clientSecret.trim() });
      const authStatus = await getAuthSecretsStatus();
      setAuthClientIdSet(!!authStatus.client_id_set);
      setAuthClientSecretSet(!!authStatus.client_secret_set);
      closeAuthSecretsModal();
      success('GitHub credentials saved');
    } catch (err) {
      setAuthSecretsModal(current => current ? { ...current, error: getErrorMessage(err, 'Failed to save credentials') } : null);
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
    1: workspacePath.trim().length > 0 && repos.length > 0,
    2: true, // Run targets are optional
    3: true, // Variants are optional
    4: true, // Quick launch is optional
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

      {/* Steps navigation - always use wizard__steps style now */}
      <div className="wizard__steps">
        {Array.from({ length: TOTAL_STEPS }, (_, i) => i + 1).map((stepNum) => {
          const isCompleted = isFirstRun && stepNum < currentStep;
          const isCurrent = stepNum === currentStep;
          const stepLabel = TABS[stepNum - 1];

          return (
            <div
              key={stepNum}
              className={`wizard__step ${isCurrent ? 'wizard__step--active' : ''} ${isCompleted ? 'wizard__step--completed' : ''}`}
              data-step={stepNum}
              onClick={() => setCurrentStep(stepNum)}
              style={{ cursor: 'pointer' }}
            >
              {stepLabel}
            </div>
          );
        })}
      </div>

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
                <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'stretch' }}>
                  <input
                    type="text"
                    className="input"
                    value={workspacePath}
                    readOnly
                    style={{ background: 'var(--color-surface-alt)', flex: 1 }}
                  />
                  <button
                    type="button"
                    className="btn"
                    onClick={openWorkspaceModal}
                  >
                    Edit
                  </button>
                </div>
                <p className="form-group__hint">
                  Directory where cloned repositories will be stored. Can use ~ for home directory.
                </p>
              </div>

              <h3 style={{ marginTop: 'var(--spacing-lg)' }}>Repositories</h3>
              <p className="wizard-step-content__description">
                Add the Git repositories that run targets will work on.
              </p>

              {repos.length === 0 ? (
                <div className="empty-state-hint">
                  No repositories configured. Add at least one to continue.
                </div>
              ) : (
                <div className="item-list">
                  {repos.map((repo) => {
                    // Find overlay info for this repo
                    const overlay = overlays.find(o => o.repo_name === repo.name);
                    const overlayPath = overlay?.path || `~/.schmux/overlays/${repo.name}`;
                    const fileCount = overlay?.exists ? overlay.file_count : 0;

                    return (
                      <div className="item-list__item" key={repo.name}>
                        <div className="item-list__item-primary">
                          <span className="item-list__item-name">{repo.name}</span>
                          <span className="item-list__item-detail">{repo.url}</span>
                          <span className="item-list__item-detail" style={{ fontSize: '0.85em', opacity: 0.8 }}>
                            Overlay: {overlayPath} {overlay?.exists ? (
                              <span style={{ color: 'var(--color-success)' }}>({fileCount} files)</span>
                            ) : (
                              <span style={{ color: 'var(--color-text-muted)' }}>(empty)</span>
                            )}
                          </span>
                        </div>
                        <button
                          className="btn btn--sm btn--danger"
                          onClick={() => removeRepo(repo.name)}
                        >
                          Remove
                        </button>
                      </div>
                    );
                  })}
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
                <button type="button" className="btn btn--sm btn--primary" onClick={addRepo}>Add</button>
              </div>
              {stepErrors[1] && (
                <p className="form-group__error" style={{ marginTop: 'var(--spacing-md)' }}>{stepErrors[1]}</p>
              )}
            </div>
          )}

          {currentTab === 2 && (
            <div className="wizard-step-content" data-step="2">
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
                      {target.source === 'user' ? (
                        <div className="btn-group">
                          <button
                            className="btn btn--sm btn--primary"
                            onClick={() => openRunTargetEditModal(target)}
                          >
                            Edit
                          </button>
                          <button
                            className="btn btn--sm btn--danger"
                            onClick={() => removePromptableTarget(target.name)}
                          >
                            Remove
                          </button>
                        </div>
                      ) : (
                        <button
                          className="btn btn--sm btn--danger"
                          onClick={() => removePromptableTarget(target.name)}
                        >
                          Remove
                        </button>
                      )}
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
                <button type="button" className="btn btn--sm btn--primary" onClick={addPromptableTarget}>Add</button>
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
                      {cmd.source === 'user' ? (
                        <div className="btn-group">
                          <button
                            className="btn btn--sm btn--primary"
                            onClick={() => openRunTargetEditModal(cmd)}
                          >
                            Edit
                          </button>
                          <button
                            className="btn btn--sm btn--danger"
                            onClick={() => removeCommand(cmd.name)}
                          >
                            Remove
                          </button>
                        </div>
                      ) : (
                        <button
                          className="btn btn--sm btn--danger"
                          onClick={() => removeCommand(cmd.name)}
                        >
                          Remove
                        </button>
                      )}
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
                <button type="button" className="btn btn--sm btn--primary" onClick={addCommand}>Add</button>
              </div>
            </div>
          )}

          {currentTab === 3 && (
            <div className="wizard-step-content" data-step="3">
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

          {currentTab === 4 && (
            <div className="wizard-step-content" data-step="4">
              <h2 className="wizard-step-content__title">Quick Launch</h2>
              <p className="wizard-step-content__description">
                Quick launch runs a target with a prompt. Promptable targets require a prompt.
              </p>

              <div className="quick-launch-editor">
                {quickLaunch.length === 0 ? (
                  <div className="quick-launch-editor__empty">
                    No quick launch items yet.
                  </div>
                ) : (
                  <div className="quick-launch-editor__list">
                    {quickLaunch.map((item) => (
                      <div className="quick-launch-editor__item" key={item.name}>
                        <div className="quick-launch-editor__item-main">
                          <span className="quick-launch-editor__item-name">{item.name}</span>
                          <span className="quick-launch-editor__item-detail">
                            {commandTargetNames.has(item.target) ? (() => {
                              const cmd = commandTargets.find(t => t.name === item.target);
                              return cmd ? cmd.command : item.target;
                            })() : `${item.target}${item.prompt ? ` — ${item.prompt}` : ''}`}
                          </span>
                        </div>
                        <div className="btn-group">
                          <button
                            className="btn btn--sm btn--primary"
                            onClick={() => openQuickLaunchEditModal(item)}
                          >
                            Edit
                          </button>
                          <button
                            className="btn btn--sm btn--danger"
                            onClick={() => removeQuickLaunch(item.name)}
                          >
                            Remove
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                <div className="quick-launch-editor__form">
                  {selectedCookbookTemplate && (
                    <div className="quick-launch-editor__cookbook-selected">
                      <span className="quick-launch-editor__cookbook-label">
                        Adding from Cookbook: <strong>{selectedCookbookTemplate.name}</strong>
                      </span>
                      <button
                        type="button"
                        className="quick-launch-editor__cookbook-clear"
                        onClick={() => {
                          setSelectedCookbookTemplate(null);
                          setNewQuickLaunchName('');
                          setNewQuickLaunchPrompt('');
                        }}
                      >
                        Clear
                      </button>
                    </div>
                  )}
                  <div className="quick-launch-editor__row">
                    <input
                      type="text"
                      className="input quick-launch-editor__name"
                      placeholder="Name"
                      value={newQuickLaunchName}
                      onChange={(e) => setNewQuickLaunchName(e.target.value)}
                    />
                    <select
                      className="input quick-launch-editor__select"
                      value={newQuickLaunchTarget}
                      onChange={(e) => {
                        const value = e.target.value;
                        setNewQuickLaunchTarget(value);
                        if (!newQuickLaunchName.trim()) {
                          setNewQuickLaunchName(value);
                        }
                        if (commandTargetNames.has(value)) {
                          setNewQuickLaunchPrompt('');
                        }
                      }}
                    >
                      <option value="">Select target...</option>
                      {selectedCookbookTemplate ? (
                        // When adding from Cookbook, only show promptable targets
                        <>
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
                        </>
                      ) : (
                        // Normal mode: show all targets
                        <>
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
                        </>
                      )}
                    </select>
                    <button type="button" className="btn btn--primary" onClick={addQuickLaunch}>Add</button>
                  </div>

                  {/* Show prompt for Cookbook OR when promptable target is selected */}
                  {(selectedCookbookTemplate || promptableTargetNames.has(newQuickLaunchTarget)) && (
                    <div className="quick-launch-editor__prompt">
                      <label className="form-group__label">
                        {selectedCookbookTemplate ? 'Prompt (from Cookbook)' : 'Prompt'}
                      </label>
                      <textarea
                        className="input quick-launch-editor__prompt-input"
                        placeholder={selectedCookbookTemplate ? '' : 'Prompt'}
                        value={newQuickLaunchPrompt}
                        onChange={(e) => setNewQuickLaunchPrompt(e.target.value)}
                        rows={6}
                      />
                    </div>
                  )}
                </div>

                {/* Cookbook Section */}
                {builtinQuickLaunch.length > 0 && (
                  <div className="quick-launch-editor__cookbook">
                    <h3 className="quick-launch-editor__section-title">Cookbook</h3>
                    <p className="quick-launch-editor__section-description">
                      Pre-configured quick launch recipes. Click to add to your quick launch with your chosen target.
                    </p>
                    <div className="quick-launch-editor__list">
                      {builtinQuickLaunch.map((template) => {
                        const isAdded = quickLaunch.some(p => p.name === template.name);
                        const isSelected = selectedCookbookTemplate?.name === template.name;
                        return (
                          <div
                            className={`quick-launch-editor__item quick-launch-editor__item--cookbook${isSelected ? ' quick-launch-editor__item--selected' : ''}`}
                            key={`cookbook-${template.name}`}
                          >
                            <div className="quick-launch-editor__item-main">
                              <span className="quick-launch-editor__item-name">{template.name}</span>
                              <span className="quick-launch-editor__item-detail quick-launch-editor__item-detail--prompt">
                                {template.prompt.slice(0, 80)}
                                {template.prompt.length > 80 ? '...' : ''}
                              </span>
                            </div>
                            {isAdded ? (
                              <span className="quick-launch-editor__item-status">Added</span>
                            ) : (
                              <button
                                className="btn"
                                onClick={() => {
                                  // Pre-fill the form with this template
                                  setSelectedCookbookTemplate(template);
                                  setNewQuickLaunchName(template.name);
                                  setNewQuickLaunchPrompt(template.prompt);
                                  // Focus on target select
                                  (document.querySelector('.quick-launch-editor__select') as HTMLSelectElement | null)?.focus();
                                }}
                              >
                                Add
                              </button>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}

          {currentTab === 5 && (
            <div className="wizard-step-content" data-step="5">
              <h2 className="wizard-step-content__title">Advanced Settings</h2>
              <p className="wizard-step-content__description">
                Terminal dimensions and advanced timing controls. You can leave these as defaults unless you have specific needs.
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
                      <option value="">Disabled</option>
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
                      Select a promptable target for NudgeNik session feedback, or leave disabled.
                    </p>
                    {nudgenikTargetMissing && (
                      <p className="form-group__error">Selected target is not available or not promptable.</p>
                    )}
                  </div>

                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Viewed Buffer (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={viewedBuffer === 0 ? '' : viewedBuffer}
                        onChange={(e) => setViewedBuffer(e.target.value === '' ? 0 : parseInt(e.target.value) || 5000)}
                      />
                      <p className="form-group__hint">Time to keep session marked as "viewed" after last check</p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Seen Interval (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={nudgenikSeenInterval === 0 ? '' : nudgenikSeenInterval}
                        onChange={(e) => setNudgenikSeenInterval(e.target.value === '' ? 0 : parseInt(e.target.value) || 2000)}
                      />
                      <p className="form-group__hint">How often to check for session activity</p>
                    </div>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Network</h3>
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
                  <h3 className="settings-section__title">Authentication</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)', cursor: 'pointer' }}>
                      <input
                        type="checkbox"
                        checked={authEnabled}
                        onChange={(e) => setAuthEnabled(e.target.checked)}
                      />
                      <span>Enable GitHub authentication</span>
                    </label>
                    <p className="form-group__hint">
                      Require GitHub login to access the dashboard. Requires HTTPS.
                    </p>
                  </div>

                  {authEnabled && (
                    <>
                      <div className="form-group">
                        <label className="form-group__label">Dashboard URL</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="https://schmux.local:7337"
                          value={authPublicBaseURL}
                          onChange={(e) => setAuthPublicBaseURL(e.target.value)}
                        />
                        <p className="form-group__hint">The URL users will type to access schmux. Must be https.</p>
                      </div>

                      <div className="form-row">
                        <div className="form-group">
                          <label className="form-group__label">TLS Cert Path</label>
                          <input
                            type="text"
                            className="input"
                            placeholder="~/.schmux/tls/schmux.local.pem"
                            value={authTlsCertPath}
                            onChange={(e) => setAuthTlsCertPath(e.target.value)}
                          />
                        </div>
                        <div className="form-group">
                          <label className="form-group__label">TLS Key Path</label>
                          <input
                            type="text"
                            className="input"
                            placeholder="~/.schmux/tls/schmux.local-key.pem"
                            value={authTlsKeyPath}
                            onChange={(e) => setAuthTlsKeyPath(e.target.value)}
                          />
                        </div>
                      </div>
                      <p className="form-group__hint" style={{ marginTop: 'calc(-1 * var(--spacing-sm))' }}>
                        Use <code>mkcert</code> to generate local certificates, or run <code>schmux auth github</code> for guided setup.
                      </p>

                      <div className="form-group">
                        <label className="form-group__label">Session TTL (minutes)</label>
                        <input
                          type="number"
                          className="input input--compact"
                          style={{ maxWidth: '120px' }}
                          min="1"
                          value={authSessionTTLMinutes}
                          onChange={(e) => setAuthSessionTTLMinutes(parseInt(e.target.value) || 1440)}
                        />
                        <p className="form-group__hint">How long before requiring re-authentication.</p>
                      </div>

                      <div className="form-group">
                        <label className="form-group__label">GitHub OAuth Credentials</label>
                        <div className="item-list" style={{ marginTop: 'var(--spacing-xs)' }}>
                          <div className="item-list__item">
                            <div className="item-list__item-primary">
                              <span className="item-list__item-name">
                                {authClientIdSet && authClientSecretSet ? (
                                  <span style={{ color: 'var(--color-success)' }}>Configured</span>
                                ) : (
                                  <span style={{ color: 'var(--color-warning)' }}>Not configured</span>
                                )}
                              </span>
                              <span className="item-list__item-detail">
                                Create at github.com/settings/developers
                              </span>
                            </div>
                            {authClientIdSet && authClientSecretSet ? (
                              <button
                                type="button"
                                className="btn btn--primary"
                                onClick={openAuthSecretsModal}
                              >
                                Update
                              </button>
                            ) : (
                              <button
                                type="button"
                                className="btn btn--primary"
                                onClick={openAuthSecretsModal}
                              >
                                Add
                              </button>
                            )}
                          </div>
                        </div>
                        <div className="form-group__hint" style={{ marginTop: 'var(--spacing-sm)' }}>
                          <p className="form-group__hint" style={{ marginTop: 'calc(-1 * var(--spacing-sm))' }}>
                            To create or check on your GitHub OAuth credentials, follow these steps:
                          </p>
                          <ol style={{ margin: 0, paddingLeft: 'var(--spacing-lg)' }}>
                            <li>Go to <a href="https://github.com/settings/developers" target="_blank" rel="noreferrer">github.com/settings/developers</a></li>
                            <li>Click "New OAuth App" (or edit existing)</li>
                            <li>Use these values:
                              <ul style={{ marginTop: 'var(--spacing-xs)' }}>
                                <li>Application name: <code>schmux</code></li>
                                <li>Homepage URL: <code>{authPublicBaseURL || 'https://schmux.local:7337'}</code></li>
                                <li>Callback URL: <code>{authPublicBaseURL ? `${authPublicBaseURL.replace(/\/+$/, '')}/auth/callback` : 'https://schmux.local:7337/auth/callback'}</code></li>
                              </ul>
                            </li>
                            <li>Copy the Client ID and Client Secret</li>
                          </ol>
                        </div>
                      </div>

                      {combinedAuthWarnings.length > 0 && (
                        <div className="form-group">
                          <p className="form-group__error">Configuration issues:</p>
                          <ul className="form-group__hint" style={{ color: 'var(--color-error)' }}>
                            {combinedAuthWarnings.map((item) => (
                              <li key={item}>{item}</li>
                            ))}
                          </ul>
                        </div>
                      )}
                    </>
                  )}
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Terminal</h3>
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
                  <h3 className="settings-section__title">Sessions</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Dashboard Poll Interval (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={dashboardPollInterval === 0 ? '' : dashboardPollInterval}
                        onChange={(e) => setDashboardPollInterval(e.target.value === '' ? 0 : parseInt(e.target.value) || 5000)}
                      />
                      <p className="form-group__hint">How often to refresh sessions list</p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Git Status Poll Interval (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={gitStatusPollInterval === 0 ? '' : gitStatusPollInterval}
                        onChange={(e) => setGitStatusPollInterval(e.target.value === '' ? 0 : parseInt(e.target.value) || 10000)}
                      />
                      <p className="form-group__hint">How often to refresh git status (dirty, ahead, behind)</p>
                    </div>
                  </div>

                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Git Clone Timeout (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={gitCloneTimeout === 0 ? '' : gitCloneTimeout}
                        onChange={(e) => setGitCloneTimeout(e.target.value === '' ? 0 : parseInt(e.target.value) || 300000)}
                      />
                      <p className="form-group__hint">Maximum time to wait for git clone when spawning sessions (default: 300000ms = 5 min)</p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Git Status Timeout (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={gitStatusTimeout === 0 ? '' : gitStatusTimeout}
                        onChange={(e) => setGitStatusTimeout(e.target.value === '' ? 0 : parseInt(e.target.value) || 30000)}
                      />
                      <p className="form-group__hint">Maximum time to wait for git status/diff operations (default: 30000ms)</p>
                    </div>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Xterm</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Mtime Poll Interval (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={mtimePollInterval === 0 ? '' : mtimePollInterval}
                        onChange={(e) => setMtimePollInterval(e.target.value === '' ? 0 : parseInt(e.target.value) || 5000)}
                      />
                      <p className="form-group__hint">How often to check log file mtimes for new output</p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Query Timeout (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={xtermQueryTimeout === 0 ? '' : xtermQueryTimeout}
                        onChange={(e) => setXtermQueryTimeout(e.target.value === '' ? 0 : parseInt(e.target.value) || 5000)}
                      />
                      <p className="form-group__hint">Maximum time to wait for xterm query operations (default: 5000ms)</p>
                    </div>
                  </div>

                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Operation Timeout (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={xtermOperationTimeout === 0 ? '' : xtermOperationTimeout}
                        onChange={(e) => setXtermOperationTimeout(e.target.value === '' ? 0 : parseInt(e.target.value) || 10000)}
                      />
                      <p className="form-group__hint">Maximum time to wait for xterm operations (default: 10000ms)</p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Max Log Size (MB)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="1"
                        value={maxLogSizeMB === 0 ? '' : maxLogSizeMB}
                        onChange={(e) => setMaxLogSizeMB(e.target.value === '' ? 0 : parseInt(e.target.value) || 50)}
                      />
                      <p className="form-group__hint">Maximum log file size before rotation (default: 50MB)</p>
                    </div>
                  </div>

                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Rotated Log Size (MB)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="1"
                        max={maxLogSizeMB}
                        value={rotatedLogSizeMB === 0 ? '' : rotatedLogSizeMB}
                        onChange={(e) => setRotatedLogSizeMB(e.target.value === '' ? 0 : Math.min(parseInt(e.target.value) || 1, maxLogSizeMB))}
                      />
                      <p className="form-group__hint">Target log size after rotation - keeps the tail (default: 1MB)</p>
                    </div>
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
            {isFirstRun && currentStep > 1 && (
              <button
                className="btn"
                onClick={prevStep}
                disabled={saving}
              >
                ← Back
              </button>
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
              disabled={saving || (!isFirstRun && !hasChanges())}
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

      {showWorkspaceModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="workspace-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeWorkspaceModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="workspace-modal-title">
                Edit Workspace Directory
              </h2>
            </div>
            <div className="modal__body">
              <div className="form-group">
                <label className="form-group__label">Workspace Path</label>
                <input
                  type="text"
                  className="input"
                  value={workspaceModalPath}
                  onChange={(e) => setWorkspaceModalPath(e.target.value)}
                  placeholder="~/schmux-workspaces"
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') saveWorkspaceModal();
                  }}
                />
                <p className="form-group__hint">
                  Directory where cloned repositories will be stored. Can use ~ for home directory.
                </p>
              </div>
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeWorkspaceModal}>Cancel</button>
              <button className="btn btn--primary" onClick={saveWorkspaceModal}>Save</button>
            </div>
          </div>
        </div>
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

      {authSecretsModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="auth-secrets-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeAuthSecretsModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="auth-secrets-modal-title">
                GitHub OAuth Credentials
              </h2>
            </div>
            <div className="modal__body">
              <div className="form-group">
                <label className="form-group__label">Client ID</label>
                <input
                  type="text"
                  className="input"
                  autoFocus
                  placeholder="Ov23li..."
                  value={authSecretsModal.clientId}
                  onChange={(e) => setAuthSecretsModal(current => current ? { ...current, clientId: e.target.value } : null)}
                />
              </div>
              <div className="form-group">
                <label className="form-group__label">Client Secret</label>
                <input
                  type="password"
                  className="input"
                  placeholder="Enter client secret"
                  value={authSecretsModal.clientSecret}
                  onChange={(e) => setAuthSecretsModal(current => current ? { ...current, clientSecret: e.target.value } : null)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') saveAuthSecretsModal();
                  }}
                />
              </div>
              {authSecretsModal.error && (
                <p className="form-group__error" style={{ marginTop: 'var(--spacing-sm)' }}>
                  {authSecretsModal.error}
                </p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeAuthSecretsModal}>Cancel</button>
              <button className="btn btn--primary" onClick={saveAuthSecretsModal}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}

      {runTargetEditModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="runtarget-edit-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeRunTargetEditModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="runtarget-edit-modal-title">
                Edit {runTargetEditModal.target.name}
              </h2>
            </div>
            <div className="modal__body">
              <div className="form-group">
                <label className="form-group__label">Command</label>
                <textarea
                  className="input"
                  value={runTargetEditModal.command}
                  onChange={(e) => setRunTargetEditModal(current => current ? { ...current, command: e.target.value, error: '' } : null)}
                  rows={6}
                  autoFocus
                />
                <p className="form-group__hint">
                  {runTargetEditModal.target.type === 'promptable'
                    ? 'Prompt is appended as last arg'
                    : 'Shell command to run'}
                </p>
              </div>
              {runTargetEditModal.error && (
                <p className="form-group__error">{runTargetEditModal.error}</p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeRunTargetEditModal}>Cancel</button>
              <button className="btn btn--primary" onClick={saveRunTargetEditModal}>Save</button>
            </div>
          </div>
        </div>
      )}

      {quickLaunchEditModal && (
        <div
          className="modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="quicklaunch-edit-modal-title"
          onKeyDown={(e) => {
            if (e.key === 'Escape') closeQuickLaunchEditModal();
          }}
        >
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="quicklaunch-edit-modal-title">
                Edit {quickLaunchEditModal.item.name}
              </h2>
            </div>
            <div className="modal__body">
              {quickLaunchEditModal.isCommandTarget ? (
                <div className="form-group">
                  <label className="form-group__label">Command</label>
                  <textarea
                    className="input"
                    value={quickLaunchEditModal.prompt}
                    onChange={(e) => setQuickLaunchEditModal(current => current ? { ...current, prompt: e.target.value, error: '' } : null)}
                    placeholder="Shell command to run"
                    rows={6}
                    autoFocus
                  />
                  <p className="form-group__hint" style={{ color: 'var(--color-warning-text)' }}>
                    This will update the underlying command target used by this quick launch item.
                  </p>
                </div>
              ) : (
                <div className="form-group">
                  <label className="form-group__label">Prompt</label>
                  <textarea
                    className="input quick-launch-editor__prompt-input"
                    value={quickLaunchEditModal.prompt}
                    onChange={(e) => setQuickLaunchEditModal(current => current ? { ...current, prompt: e.target.value, error: '' } : null)}
                    placeholder="Prompt to send to the agent"
                    rows={10}
                    autoFocus
                  />
                </div>
              )}
              {quickLaunchEditModal.error && (
                <p className="form-group__error">{quickLaunchEditModal.error}</p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn" onClick={closeQuickLaunchEditModal}>Cancel</button>
              <button className="btn btn--primary" onClick={saveQuickLaunchEditModal}>Save</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
