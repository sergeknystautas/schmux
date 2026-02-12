import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  getConfig,
  updateConfig,
  configureModelSecrets,
  removeModelSecrets,
  getOverlays,
  getBuiltinQuickLaunch,
  getAuthSecretsStatus,
  saveAuthSecrets,
  getErrorMessage,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { CONFIG_UPDATED_KEY } from '../lib/constants';
import type {
  BuiltinQuickLaunchCookbook,
  ConfigResponse,
  ConfigUpdateRequest,
  Model,
  OverlayInfo,
  QuickLaunchPreset,
  RepoResponse,
  RunTargetResponse,
} from '../lib/types';

const TOTAL_STEPS = 5;
const TABS = ['Workspaces', 'Sessions', 'Quick Launch', 'Code Review', 'Advanced'];

// Map step number to URL slug
const TAB_SLUGS = ['workspaces', 'sessions', 'quicklaunch', 'codereview', 'advanced'];

// Helper: step number -> slug
const stepToSlug = (step: number) => TAB_SLUGS[step - 1];

// Helper: slug -> step number
const slugToStep = (slug: string | null) => {
  const index = TAB_SLUGS.indexOf(slug);
  return index >= 0 ? index + 1 : 1;
};

// Model aliases for canonical ID normalization
const modelAliases: Record<string, string> = {
  opus: 'claude-opus',
  sonnet: 'claude-sonnet',
  haiku: 'claude-haiku',
  'minimax-m2.1': 'minimax',
};

type ConfigSnapshot = {
  workspacePath: string;
  sourceCodeManagement: string;
  repos: RepoResponse[];
  promptableTargets: RunTargetResponse[];
  commandTargets: RunTargetResponse[];
  quickLaunch: QuickLaunchPreset[];
  externalDiffCommands: { name: string; command: string }[];
  externalDiffCleanupMinutes: number;
  nudgenikTarget: string;
  branchSuggestTarget: string;
  conflictResolveTarget: string;
  prReviewTarget: string;
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
  soundDisabled: boolean;
  confirmBeforeClose: boolean;
  modelVersions: Record<string, string>;
};

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
  const { show, confirm, prompt } = useModal();
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
  const [sourceCodeManagement, setSourceCodeManager] = useState('git-worktree');
  const [repos, setRepos] = useState<RepoResponse[]>([]);
  const [promptableTargets, setPromptableTargets] = useState<RunTargetResponse[]>([]);
  const [commandTargets, setCommandTargets] = useState<RunTargetResponse[]>([]);
  const [detectedTargets, setDetectedTargets] = useState<RunTargetResponse[]>([]);
  const [quickLaunch, setQuickLaunch] = useState<QuickLaunchPreset[]>([]);
  const [builtinQuickLaunch, setBuiltinQuickLaunch] = useState<BuiltinQuickLaunchCookbook[]>([]); // Built-in quick launch presets
  const [externalDiffCommands, setExternalDiffCommands] = useState<
    { name: string; command: string }[]
  >([]);
  const [externalDiffCleanupMinutes, setExternalDiffCleanupMinutes] = useState(60);
  const [models, setModels] = useState<Model[]>([]);
  const [nudgenikTarget, setNudgenikTarget] = useState('');
  const [branchSuggestTarget, setBranchSuggestTarget] = useState('');
  const [conflictResolveTarget, setConflictResolveTarget] = useState('');
  const [prReviewTarget, setPrReviewTarget] = useState('');

  // External diff new item state
  const [newDiffName, setNewDiffName] = useState('');
  const [newDiffCommand, setNewDiffCommand] = useState('');

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
  const [soundDisabled, setSoundDisabled] = useState(false);
  const [confirmBeforeClose, setConfirmBeforeClose] = useState(false);
  const [modelVersions, setModelVersions] = useState<Record<string, string>>({});

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
      sourceCodeManagement,
      repos,
      promptableTargets,
      commandTargets,
      quickLaunch,
      externalDiffCommands,
      externalDiffCleanupMinutes,
      nudgenikTarget,
      branchSuggestTarget,
      conflictResolveTarget,
      prReviewTarget,
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
      soundDisabled,
      confirmBeforeClose,
      modelVersions,
    };

    // Deep comparison for arrays
    const arraysMatch = (a: unknown[], b: unknown[]) => {
      if (a.length !== b.length) return false;
      return a.every((item, i) => JSON.stringify(item) === JSON.stringify(b[i]));
    };

    return (
      current.workspacePath !== originalConfig.workspacePath ||
      current.sourceCodeManagement !== originalConfig.sourceCodeManagement ||
      !arraysMatch(current.repos, originalConfig.repos) ||
      !arraysMatch(current.promptableTargets, originalConfig.promptableTargets) ||
      !arraysMatch(current.commandTargets, originalConfig.commandTargets) ||
      !arraysMatch(current.quickLaunch, originalConfig.quickLaunch) ||
      !arraysMatch(current.externalDiffCommands, originalConfig.externalDiffCommands) ||
      current.nudgenikTarget !== originalConfig.nudgenikTarget ||
      current.branchSuggestTarget !== originalConfig.branchSuggestTarget ||
      current.conflictResolveTarget !== originalConfig.conflictResolveTarget ||
      current.prReviewTarget !== originalConfig.prReviewTarget ||
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
      current.authTlsKeyPath !== originalConfig.authTlsKeyPath ||
      current.soundDisabled !== originalConfig.soundDisabled ||
      current.confirmBeforeClose !== originalConfig.confirmBeforeClose ||
      JSON.stringify(current.modelVersions) !== JSON.stringify(originalConfig.modelVersions)
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
  const [selectedCookbookTemplate, setSelectedCookbookTemplate] =
    useState<BuiltinQuickLaunchCookbook | null>(null); // Track which cookbook template is being added

  // Validation state per step
  const [stepErrors, setStepErrors] = useState<Record<number, string | null>>({
    1: null,
    2: null,
    3: null,
    4: null,
    5: null,
  });

  const localAuthWarnings: string[] = [];
  if (authEnabled) {
    if (!authPublicBaseURL.trim()) {
      localAuthWarnings.push('Public base URL is required when auth is enabled.');
    } else if (
      !authPublicBaseURL.startsWith('https://') &&
      !authPublicBaseURL.startsWith('http://localhost')
    ) {
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
        setSourceCodeManager(data.source_code_management || 'git-worktree');
        setTerminalWidth(String(data.terminal?.width || 120));
        setTerminalHeight(String(data.terminal?.height || 40));
        setTerminalSeedLines(String(data.terminal?.seed_lines || 100));
        setTerminalBootstrapLines(String(data.terminal?.bootstrap_lines || 20000));
        setRepos(data.repos || []);

        const detectedItems = (data.run_targets || []).filter((t) => t.source === 'detected');
        const modelBaseTools = new Set((data.models || []).map((model) => model.base_tool));
        const filteredDetectedItems = detectedItems.filter(
          (target) => !modelBaseTools.has(target.name)
        );
        const promptableItems = (data.run_targets || []).filter(
          (t) => t.type === 'promptable' && t.source !== 'detected' && t.source !== 'model'
        );
        const commandItems = (data.run_targets || []).filter(
          (t) => t.type === 'command' && t.source !== 'detected' && t.source !== 'model'
        );
        setPromptableTargets(promptableItems);
        setCommandTargets(commandItems);
        setDetectedTargets(filteredDetectedItems);
        setQuickLaunch(data.quick_launch || []);
        // External diff commands - add VS Code as a built-in default
        // Built-in commands are not editable/deletable in the UI
        const userDiffCommands = data.external_diff_commands || [];
        setExternalDiffCommands(userDiffCommands);
        const cleanupMs = data.external_diff_cleanup_after_ms || 3600000;
        setExternalDiffCleanupMinutes(Math.max(1, cleanupMs / 60000));
        setNudgenikTarget(data.nudgenik?.target || '');
        setBranchSuggestTarget(data.branch_suggest?.target || '');
        setConflictResolveTarget(data.conflict_resolve?.target || '');
        setPrReviewTarget(data.pr_review?.target || '');

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
        setSoundDisabled(data.notifications?.sound_disabled || false);
        setConfirmBeforeClose(data.notifications?.confirm_before_close || false);
        setModelVersions(data.model_versions || {});

        // Set original config for change detection (non-wizard mode)
        if (!isFirstRun) {
          setOriginalConfig({
            workspacePath: data.workspace_path || '',
            sourceCodeManagement: data.source_code_management || 'git-worktree',
            repos: data.repos || [],
            promptableTargets: promptableItems,
            commandTargets: commandItems,
            quickLaunch: data.quick_launch || [],
            externalDiffCommands: data.external_diff_commands || [],
            externalDiffCleanupMinutes: Math.max(
              1,
              (data.external_diff_cleanup_after_ms || 3600000) / 60000
            ),
            nudgenikTarget: data.nudgenik?.target || '',
            branchSuggestTarget: data.branch_suggest?.target || '',
            conflictResolveTarget: data.conflict_resolve?.target || '',
            prReviewTarget: data.pr_review?.target || '',
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
            soundDisabled: data.notifications?.sound_disabled || false,
            confirmBeforeClose: data.notifications?.confirm_before_close || false,
            modelVersions: data.model_versions || {},
          });
        }

        // Models are now included in config response
        if (active) {
          setModels(data.models || []);
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
    return () => {
      active = false;
    };
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
    return () => {
      active = false;
    };
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
    return () => {
      active = false;
    };
  }, []);

  const reloadModels = async () => {
    try {
      const data = await getConfig();
      setModels(data.models || []);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to load models'));
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
      // Models are optional
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

    setStepErrors((prev) => ({ ...prev, [step]: error }));
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
        ...promptableTargets.map((t) => ({ ...t, type: 'promptable' })),
        ...commandTargets.map((t) => ({ ...t, type: 'command' })),
      ];

      const updateRequest: ConfigUpdateRequest = {
        workspace_path: workspacePath,
        source_code_management: sourceCodeManagement,
        terminal: {
          width,
          height,
          seed_lines: seedLines,
          bootstrap_lines: parseInt(terminalBootstrapLines),
        },
        repos: repos,
        run_targets: runTargets,
        quick_launch: quickLaunch,
        external_diff_commands: externalDiffCommands,
        external_diff_cleanup_after_ms: Math.max(
          60000,
          Math.round(externalDiffCleanupMinutes * 60000)
        ),
        nudgenik: {
          target: nudgenikTarget || '',
          viewed_buffer_ms: viewedBuffer,
          seen_interval_ms: nudgenikSeenInterval,
        },
        branch_suggest: {
          target: branchSuggestTarget || '',
        },
        conflict_resolve: {
          target: conflictResolveTarget || '',
        },
        pr_review: {
          target: prReviewTarget || '',
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
        notifications: {
          sound_disabled: soundDisabled,
          confirm_before_close: confirmBeforeClose,
        },
        model_versions: modelVersions,
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
          sourceCodeManagement,
          repos,
          promptableTargets,
          commandTargets,
          quickLaunch,
          externalDiffCommands,
          externalDiffCleanupMinutes,
          nudgenikTarget,
          branchSuggestTarget,
          conflictResolveTarget,
          prReviewTarget,
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
          soundDisabled,
          confirmBeforeClose,
          modelVersions,
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
    if (repos.some((r) => r.name === newRepoName)) {
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
      setRepos(repos.filter((r) => r.name !== name));
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
    const nameExists = [...promptableTargets, ...commandTargets, ...detectedTargets].some(
      (t) => t.name === newPromptableName
    );
    if (nameExists) {
      toastError('Run target name already exists');
      return;
    }
    setPromptableTargets([
      ...promptableTargets,
      {
        name: newPromptableName,
        command: newPromptableCommand,
        type: 'promptable',
        source: 'user',
      },
    ]);
    setNewPromptableName('');
    setNewPromptableCommand('');
  };

  const checkTargetUsage = (targetName) => {
    // Normalize to canonical model ID if targetName is a model ID or alias
    let canonicalName = targetName;
    const model = models.find((m) => m.id === targetName || modelAliases[m.id] === targetName);
    if (model) {
      canonicalName = model.id;
    }

    const inQuickLaunch = quickLaunch.some(
      (item) => item.target === canonicalName || modelAliases[item.target] === canonicalName
    );
    const inNudgenik =
      nudgenikTarget &&
      (nudgenikTarget === canonicalName || modelAliases[nudgenikTarget] === canonicalName);
    const inBranchSuggest =
      branchSuggestTarget &&
      (branchSuggestTarget === canonicalName ||
        modelAliases[branchSuggestTarget] === canonicalName);
    const inConflictResolve =
      conflictResolveTarget &&
      (conflictResolveTarget === canonicalName ||
        modelAliases[conflictResolveTarget] === canonicalName);
    const inPrReview =
      prReviewTarget &&
      (prReviewTarget === canonicalName || modelAliases[prReviewTarget] === canonicalName);
    return { inQuickLaunch, inNudgenik, inBranchSuggest, inConflictResolve, inPrReview };
  };

  const removePromptableTarget = async (name) => {
    const usage = checkTargetUsage(name);
    if (
      usage.inQuickLaunch ||
      usage.inNudgenik ||
      usage.inBranchSuggest ||
      usage.inConflictResolve ||
      usage.inPrReview
    ) {
      const reasons = [
        usage.inQuickLaunch ? 'quick launch item' : null,
        usage.inNudgenik ? 'nudgenik target' : null,
        usage.inBranchSuggest ? 'branch suggest target' : null,
        usage.inConflictResolve ? 'conflict resolve target' : null,
        usage.inPrReview ? 'PR review target' : null,
      ]
        .filter(Boolean)
        .join(' and ');
      toastError(`Cannot remove "${name}" while used by ${reasons}.`);
      return;
    }
    const confirmed = await confirm('Remove run target?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setPromptableTargets(promptableTargets.filter((t) => t.name !== name));
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
    const nameExists = [...promptableTargets, ...commandTargets, ...detectedTargets].some(
      (t) => t.name === newCommandName
    );
    if (nameExists) {
      toastError('Run target name already exists');
      return;
    }
    setCommandTargets([
      ...commandTargets,
      { name: newCommandName, command: newCommandCommand, type: 'command', source: 'user' },
    ]);
    setNewCommandName('');
    setNewCommandCommand('');
  };

  const removeCommand = async (name) => {
    const usage = checkTargetUsage(name);
    if (
      usage.inQuickLaunch ||
      usage.inNudgenik ||
      usage.inBranchSuggest ||
      usage.inConflictResolve ||
      usage.inPrReview
    ) {
      const reasons = [
        usage.inQuickLaunch ? 'quick launch item' : null,
        usage.inNudgenik ? 'nudgenik target' : null,
        usage.inBranchSuggest ? 'branch suggest target' : null,
        usage.inConflictResolve ? 'conflict resolve target' : null,
        usage.inPrReview ? 'PR review target' : null,
      ]
        .filter(Boolean)
        .join(' and ');
      toastError(`Cannot remove "${name}" while used by ${reasons}.`);
      return;
    }
    const confirmed = await confirm('Remove command?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setCommandTargets(commandTargets.filter((c) => c.name !== name));
    }
  };

  const addQuickLaunch = () => {
    const targetName = newQuickLaunchTarget.trim();
    if (!targetName) {
      toastError('Quick launch target is required');
      return;
    }
    const name = newQuickLaunchName.trim() || targetName;
    if (quickLaunch.some((q) => q.name === name)) {
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
      setQuickLaunch(quickLaunch.filter((q) => q.name !== name));
    }
  };

  const [runTargetEditModal, setRunTargetEditModal] = useState<RunTargetEditModalState>(null);
  const [quickLaunchEditModal, setQuickLaunchEditModal] = useState<QuickLaunchEditModalState>(null);

  const handleEditWorkspacePath = async () => {
    const newPath = await prompt('Edit Workspace Directory', {
      defaultValue: workspacePath,
      placeholder: '~/schmux-workspaces',
      confirmText: 'Save',
    });
    if (newPath !== null) {
      setWorkspacePath(newPath);
      if (newPath.trim()) {
        setStepErrors((prev) => ({ ...prev, 1: null }));
      }
    }
  };

  const handleModelAction = async (model: Model, mode: 'add' | 'remove' | 'update') => {
    if (mode === 'remove') {
      const usage = checkTargetUsage(model.id);
      if (usage.inQuickLaunch || usage.inNudgenik) {
        const reasons = [
          usage.inQuickLaunch ? 'quick launch item' : null,
          usage.inNudgenik ? 'nudgenik target' : null,
        ]
          .filter(Boolean)
          .join(' and ');
        toastError(`Cannot remove model "${model.display_name}" while used by ${reasons}.`);
        return;
      }
      const confirmed = await confirm(`Remove ${model.display_name}?`, {
        confirmText: 'Remove',
        danger: true,
        detailedMessage: 'Remove stored secrets for this model?',
      });
      if (!confirmed) return;
      try {
        await removeModelSecrets(model.id);
        await reloadModels();
        success(`Removed secrets for ${model.display_name}`);
      } catch (err) {
        toastError(getErrorMessage(err, 'Failed to remove model secrets'));
      }
      return;
    }

    // add or update mode
    const secretKey = (model.required_secrets || [])[0];
    if (!secretKey) return;

    const title = mode === 'add' ? `Add ${model.display_name}` : `Update ${model.display_name}`;
    const value = await prompt(title, {
      placeholder: secretKey,
      confirmText: mode === 'add' ? 'Add' : 'Update',
      password: true,
    });
    if (value === null) return;
    if (!value.trim()) {
      toastError(`Missing required secret ${secretKey}`);
      return;
    }

    try {
      await configureModelSecrets(model.id, { [secretKey]: value });
      await reloadModels();
      success(`Saved secrets for ${model.display_name}`);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to save model secrets'));
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
      setRunTargetEditModal((current) =>
        current ? { ...current, error: 'Command is required' } : null
      );
      return;
    }

    if (target.type === 'promptable') {
      setPromptableTargets(
        promptableTargets.map((t) => (t.name === target.name ? { ...t, command } : t))
      );
    } else {
      setCommandTargets(
        commandTargets.map((t) => (t.name === target.name ? { ...t, command } : t))
      );
    }
    closeRunTargetEditModal();
  };

  // Quick launch edit modal
  const openQuickLaunchEditModal = (item: QuickLaunchPreset) => {
    const isCommandTarget = commandTargetNames.has(item.target);
    // For command targets, prefill with the underlying target's command
    let initialPrompt = item.prompt || '';
    if (isCommandTarget) {
      const commandTarget = commandTargets.find((t) => t.name === item.target);
      if (commandTarget) {
        initialPrompt = commandTarget.command;
      }
    }
    setQuickLaunchEditModal({
      item,
      prompt: initialPrompt,
      isCommandTarget,
      error: '',
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
      setQuickLaunchEditModal((current) =>
        current ? { ...current, error: 'Prompt is required for promptable targets' } : null
      );
      return;
    }

    // For command target items, require non-empty command and update the underlying run target
    if (isCommandTarget) {
      if (!prompt.trim()) {
        setQuickLaunchEditModal((current) =>
          current ? { ...current, error: 'Command is required for command targets' } : null
        );
        return;
      }
      setCommandTargets(
        commandTargets.map((t) => (t.name === target ? { ...t, command: prompt } : t))
      );
    }

    // Update the quick launch item
    setQuickLaunch(
      quickLaunch.map((p) =>
        p.name === item.name ? { name: item.name, target, prompt: isPromptable ? prompt : null } : p
      )
    );
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
      setAuthSecretsModal((current) =>
        current ? { ...current, error: 'Both client ID and client secret are required' } : null
      );
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
      setAuthSecretsModal((current) =>
        current ? { ...current, error: getErrorMessage(err, 'Failed to save credentials') } : null
      );
    }
  };

  const promptableTargetNames = new Set([
    ...detectedTargets.map((target) => target.name),
    ...promptableTargets.map((target) => target.name),
    ...models.filter((model) => model.configured).map((model) => model.id),
  ]);

  const commandTargetNames = new Set(commandTargets.map((target) => target.name));
  const nudgenikTargetMissing =
    nudgenikTarget.trim() !== '' && !promptableTargetNames.has(nudgenikTarget.trim());
  const branchSuggestTargetMissing =
    branchSuggestTarget.trim() !== '' && !promptableTargetNames.has(branchSuggestTarget.trim());
  const conflictResolveTargetMissing =
    conflictResolveTarget.trim() !== '' && !promptableTargetNames.has(conflictResolveTarget.trim());
  const prReviewTargetMissing =
    prReviewTarget.trim() !== '' && !promptableTargetNames.has(prReviewTarget.trim());

  // Map wizard step to tab number - now 1:1 mapping
  const getTabForStep = (step) => step;

  const getCurrentTab = () => currentStep;

  // Check if each step is valid
  const stepValid = {
    1: workspacePath.trim().length > 0 && repos.length > 0,
    2: true, // Run targets (now includes models) are optional
    3: true, // Quick launch is optional
    4: true, // External diff is optional
    5: true, // Advanced step is always valid (has defaults)
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
      {/* Sticky header for edit mode (non-first-run) */}
      {!isFirstRun && (
        <div className="config-sticky-header">
          <div className="config-sticky-header__title-row">
            <h1 className="config-sticky-header__title">Configuration</h1>
            <div className="config-sticky-header__actions">
              <button
                className="btn btn--primary btn--sm"
                onClick={async () => {
                  await saveCurrentStep();
                }}
                disabled={saving || !hasChanges()}
                data-testid="config-save"
              >
                {saving ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </div>
          <div className="wizard__steps wizard__steps--compact">
            {Array.from({ length: TOTAL_STEPS }, (_, i) => i + 1).map((stepNum) => {
              const isCurrent = stepNum === currentStep;
              const stepLabel = TABS[stepNum - 1];

              return (
                <div
                  key={stepNum}
                  className={`wizard__step ${isCurrent ? 'wizard__step--active' : ''}`}
                  data-step={stepNum}
                  data-testid={`config-tab-${TAB_SLUGS[stepNum - 1]}`}
                  onClick={() => setCurrentStep(stepNum)}
                  style={{ cursor: 'pointer' }}
                >
                  {stepLabel}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Non-sticky header for first-run wizard */}
      {isFirstRun && (
        <>
          <div className="page-header">
            <h1 className="page-header__title">Setup schmux</h1>
          </div>

          <div className="banner banner--info" style={{ marginBottom: 'var(--spacing-lg)' }}>
            <p style={{ margin: 0 }}>
              <strong>Welcome to schmux!</strong> Complete these steps to start spawning sessions.
            </p>
          </div>
        </>
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
            <strong>Restart required:</strong> Network access setting has changed. Restart the
            daemon for this setting to take effect: <code>./schmux stop && ./schmux start</code>
          </p>
        </div>
      )}

      {/* Steps navigation for first-run wizard only */}
      {isFirstRun && (
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
      )}

      {/* Wizard content */}
      <div className="wizard">
        <div className="wizard__content">
          {currentTab === 1 && (
            <div className="wizard-step-content" data-step="1">
              <h2 className="wizard-step-content__title">Workspace Directory</h2>
              <p className="wizard-step-content__description">
                This is where schmux will store cloned repositories. Each session gets its own
                workspace directory here. Only affects new sessions - existing workspaces keep their
                current location.
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
                  <button type="button" className="btn" onClick={handleEditWorkspacePath}>
                    Edit
                  </button>
                </div>
                <p className="form-group__hint">
                  Directory where cloned repositories will be stored. Can use ~ for home directory.
                </p>
              </div>

              <h3>Repositories</h3>
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
                    const overlay = overlays.find((o) => o.repo_name === repo.name);
                    const overlayPath = overlay?.path || `~/.schmux/overlays/${repo.name}`;
                    const fileCount = overlay?.exists ? overlay.file_count : 0;

                    return (
                      <div className="item-list__item" key={repo.name}>
                        <div className="item-list__item-primary">
                          <span className="item-list__item-name">{repo.name}</span>
                          <span className="item-list__item-detail">{repo.url}</span>
                          <span
                            className="item-list__item-detail"
                            style={{ fontSize: '0.85em', opacity: 0.8 }}
                          >
                            Overlay: {overlayPath}{' '}
                            {overlay?.exists ? (
                              <span style={{ color: 'var(--color-success)' }}>
                                ({fileCount} files)
                              </span>
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
                <button
                  type="button"
                  className="btn btn--sm btn--primary"
                  onClick={addRepo}
                  data-testid="add-repo"
                >
                  Add
                </button>
              </div>

              <h3>Source Code Management</h3>
              <p className="wizard-step-content__description">
                How schmux creates workspace directories for each session.
              </p>
              <div className="form-group">
                <select
                  className="select"
                  value={sourceCodeManagement}
                  onChange={(e) => setSourceCodeManager(e.target.value)}
                >
                  <option value="git-worktree">git worktree (default)</option>
                  <option value="git">git</option>
                </select>
                <p className="form-group__hint">
                  {sourceCodeManagement === 'git-worktree' ? (
                    <>
                      <strong>git worktree:</strong> Efficient disk usage, shares repo history
                      across workspaces. Each branch can only be used by one workspace at a time.
                    </>
                  ) : (
                    <>
                      <strong>git:</strong> Independent clones for each workspace. Multiple
                      workspaces can use the same branch.
                    </>
                  )}
                </p>
              </div>

              {stepErrors[1] && (
                <p className="form-group__error" style={{ marginTop: 'var(--spacing-md)' }}>
                  {stepErrors[1]}
                </p>
              )}
            </div>
          )}

          {currentTab === 2 && (
            <div className="wizard-step-content" data-step="2">
              <h2 className="wizard-step-content__title">Run Targets</h2>
              <p className="wizard-step-content__description">
                Configure user-supplied run targets. Detected tools appear automatically in the
                spawn wizard.
              </p>

              <h3>Detected Run Targets (Read-only)</h3>
              <p className="section-hint">
                Official tools we detected on this machine and confirmed working. These are
                read-only.
              </p>
              {detectedTargets.length === 0 ? (
                <div className="empty-state-hint">
                  No detected run targets. Use the detect endpoint or restart the daemon to refresh
                  detection.
                </div>
              ) : (
                <div className="item-list item-list--two-col">
                  {detectedTargets.map((target) => (
                    <div className="item-list__item" key={target.name}>
                      <div className="item-list__item-primary item-list__item-row">
                        <span className="item-list__item-name">{target.name}</span>
                        <span className="item-list__item-detail item-list__item-detail--wide">
                          {target.command}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <h3>Models</h3>
              <p className="section-hint">
                Add secrets to enable third-party models for quick launch and spawning.
              </p>
              {models.length === 0 ? (
                <div className="empty-state-hint">
                  No models available. Install the base tool to enable models.
                </div>
              ) : (
                <div className="item-list">
                  {models.map((model) => (
                    <div className="item-list__item" key={model.id}>
                      <div className="item-list__item-primary">
                        <span className="item-list__item-name">{model.display_name}</span>
                        <span className="item-list__item-detail">
                          {model.id} · base: {model.base_tool}
                        </span>
                        {model.usage_url && (
                          <a
                            className="item-list__item-detail link"
                            href={model.usage_url}
                            target="_blank"
                            rel="noreferrer"
                          >
                            {model.usage_url}
                          </a>
                        )}
                      </div>
                      {/* Native models have no required secrets */}
                      {model.required_secrets && model.required_secrets.length > 0 ? (
                        model.configured ? (
                          <div style={{ display: 'flex', gap: 'var(--spacing-sm)' }}>
                            <button
                              className="btn btn--primary"
                              onClick={() => handleModelAction(model, 'update')}
                            >
                              Update
                            </button>
                            <button
                              className="btn btn--danger"
                              onClick={() => handleModelAction(model, 'remove')}
                            >
                              Remove
                            </button>
                          </div>
                        ) : (
                          <button
                            className="btn btn--primary"
                            onClick={() => handleModelAction(model, 'add')}
                          >
                            Add Secrets
                          </button>
                        )
                      ) : (
                        <span className="status-badge status-badge--success">
                          No secrets required
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              )}

              <h3>Promptable Targets</h3>
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
                        <span className="item-list__item-detail item-list__item-detail--wide">
                          {target.command}
                        </span>
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
                <button
                  type="button"
                  className="btn btn--sm btn--primary"
                  onClick={addPromptableTarget}
                  data-testid="add-target"
                >
                  Add
                </button>
              </div>

              <h3>Command Targets</h3>
              <p className="section-hint">
                Shell commands you want to run quickly, like launching a terminal or starting the
                app.
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
                        <span className="item-list__item-detail item-list__item-detail--wide">
                          {cmd.command}
                        </span>
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
                <button type="button" className="btn btn--sm btn--primary" onClick={addCommand}>
                  Add
                </button>
              </div>
            </div>
          )}

          {currentTab === 3 && (
            <div className="wizard-step-content" data-step="3">
              <h2 className="wizard-step-content__title">Quick Launch</h2>
              <p className="wizard-step-content__description">
                Quick launch runs a target with a prompt. Promptable targets require a prompt.
              </p>

              <div className="quick-launch-editor">
                {quickLaunch.length === 0 ? (
                  <div className="quick-launch-editor__empty">No quick launch items yet.</div>
                ) : (
                  <div className="quick-launch-editor__list">
                    {quickLaunch.map((item) => (
                      <div className="quick-launch-editor__item" key={item.name}>
                        <div className="quick-launch-editor__item-main">
                          <span className="quick-launch-editor__item-name">{item.name}</span>
                          <span className="quick-launch-editor__item-detail">
                            {commandTargetNames.has(item.target)
                              ? (() => {
                                  const cmd = commandTargets.find((t) => t.name === item.target);
                                  return cmd ? cmd.command : item.target;
                                })()
                              : `${item.target}${item.prompt ? ` — ${item.prompt}` : ''}`}
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
                              ...detectedTargets.map((target) => ({
                                value: target.name,
                                label: target.name,
                              })),
                              ...models
                                .filter((model) => model.configured)
                                .map((model) => ({
                                  value: model.id,
                                  label: model.display_name,
                                })),
                              ...promptableTargets.map((target) => ({
                                value: target.name,
                                label: target.name,
                              })),
                            ].map((option) => (
                              <option key={option.value} value={option.value}>
                                {option.label}
                              </option>
                            ))}
                          </optgroup>
                        </>
                      ) : (
                        // Normal mode: show all targets
                        <>
                          <optgroup label="Promptable Targets">
                            {[
                              ...detectedTargets.map((target) => ({
                                value: target.name,
                                label: target.name,
                              })),
                              ...models
                                .filter((model) => model.configured)
                                .map((model) => ({
                                  value: model.id,
                                  label: model.display_name,
                                })),
                              ...promptableTargets.map((target) => ({
                                value: target.name,
                                label: target.name,
                              })),
                            ].map((option) => (
                              <option key={option.value} value={option.value}>
                                {option.label}
                              </option>
                            ))}
                          </optgroup>
                          <optgroup label="Command Targets">
                            {commandTargets.map((target) => (
                              <option key={target.name} value={target.name}>
                                {target.name}
                              </option>
                            ))}
                          </optgroup>
                        </>
                      )}
                    </select>
                    <button type="button" className="btn btn--primary" onClick={addQuickLaunch}>
                      Add
                    </button>
                  </div>

                  {/* Show prompt for Cookbook OR when promptable target is selected */}
                  {(selectedCookbookTemplate ||
                    promptableTargetNames.has(newQuickLaunchTarget)) && (
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
                      Pre-configured quick launch recipes. Click to add to your quick launch with
                      your chosen target.
                    </p>
                    <div className="quick-launch-editor__list">
                      {builtinQuickLaunch.map((template) => {
                        const isAdded = quickLaunch.some((p) => p.name === template.name);
                        const isSelected = selectedCookbookTemplate?.name === template.name;
                        return (
                          <div
                            className={`quick-launch-editor__item quick-launch-editor__item--cookbook${isSelected ? ' quick-launch-editor__item--selected' : ''}`}
                            key={`cookbook-${template.name}`}
                          >
                            <div className="quick-launch-editor__item-main">
                              <span className="quick-launch-editor__item-name">
                                {template.name}
                              </span>
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
                                  (
                                    document.querySelector(
                                      '.quick-launch-editor__select'
                                    ) as HTMLSelectElement | null
                                  )?.focus();
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

          {currentTab === 4 && (
            <div className="wizard-step-content" data-step="4">
              <h2 className="wizard-step-content__title">Code Review</h2>
              <p className="wizard-step-content__description">
                Configure targets and tools used during code review workflows.
              </p>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">PR Review</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label className="form-group__label">Target</label>
                    <select
                      className="input"
                      value={prReviewTarget}
                      onChange={(e) => setPrReviewTarget(e.target.value)}
                    >
                      <option value="">Disabled</option>
                      <optgroup label="Detected Tools">
                        {detectedTargets.map((target) => (
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                      <optgroup label="Models">
                        {models
                          .filter((model) => model.configured)
                          .map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                      </optgroup>
                      <optgroup label="User Promptable">
                        {promptableTargets.map((target) => (
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                    </select>
                    <p className="form-group__hint">
                      Select a promptable target for PR review sessions, or leave disabled.
                    </p>
                    {prReviewTargetMissing && (
                      <p className="form-group__error">
                        Selected target is not available or not promptable.
                      </p>
                    )}
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Built-in Options</h3>
                </div>
                <div className="settings-section__body">
                  <div className="item-list">
                    <div className="item-list__item">
                      <span className="item-list__item-name">VS Code</span>
                      <span className="item-list__item-detail">
                        Always available in the diff dropdown
                      </span>
                    </div>
                    <div className="item-list__item">
                      <span className="item-list__item-name">Web view</span>
                      <span className="item-list__item-detail">
                        Always available in the diff dropdown
                      </span>
                    </div>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Custom Diff Tools</h3>
                </div>
                <div className="settings-section__body">
                  {externalDiffCommands.length === 0 ? (
                    <div className="empty-state-hint">No custom diff tools configured.</div>
                  ) : (
                    <div className="item-list item-list--two-col">
                      {externalDiffCommands.map((cmd) => (
                        <div className="item-list__item" key={cmd.name}>
                          <div className="item-list__item-primary item-list__item-row">
                            <span className="item-list__item-name">{cmd.name}</span>
                            <span className="item-list__item-detail item-list__item-detail--wide mono">
                              {cmd.command}
                            </span>
                          </div>
                          <button
                            className="btn btn--sm btn--danger"
                            onClick={() =>
                              setExternalDiffCommands(
                                externalDiffCommands.filter((c) => c.name !== cmd.name)
                              )
                            }
                          >
                            Remove
                          </button>
                        </div>
                      ))}
                    </div>
                  )}

                  <h3>Add Custom Diff Tool</h3>
                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Name</label>
                      <input
                        type="text"
                        className="input"
                        placeholder="e.g., Kaleidoscope"
                        value={newDiffName}
                        onChange={(e) => setNewDiffName(e.target.value)}
                      />
                    </div>
                    <div className="form-group">
                      <label className="form-group__label">Command</label>
                      <input
                        type="text"
                        className="input"
                        placeholder="e.g., ksdiff"
                        value={newDiffCommand}
                        onChange={(e) => setNewDiffCommand(e.target.value)}
                      />
                    </div>
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'flex-end',
                        paddingBottom: 'var(--spacing-sm)',
                      }}
                    >
                      <button
                        type="button"
                        className="btn btn--primary"
                        disabled={!newDiffName.trim() || !newDiffCommand.trim()}
                        onClick={() => {
                          const name = newDiffName.trim();
                          const command = newDiffCommand.trim();
                          if (externalDiffCommands.some((c) => c.name === name)) {
                            toastError('Diff tool name already exists');
                            return;
                          }
                          setExternalDiffCommands([...externalDiffCommands, { name, command }]);
                          setNewDiffName('');
                          setNewDiffCommand('');
                        }}
                      >
                        Add Diff Tool
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Temp Cleanup</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-row">
                    <div className="form-group">
                      <label className="form-group__label">Cleanup after (minutes)</label>
                      <input
                        type="number"
                        min="1"
                        className="input"
                        value={externalDiffCleanupMinutes}
                        onChange={(e) =>
                          setExternalDiffCleanupMinutes(Math.max(1, Number(e.target.value) || 1))
                        }
                      />
                      <p className="form-group__hint">
                        Temp diff files will be removed after this delay (default: 60 minutes).
                      </p>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {currentTab === 5 && (
            <div className="wizard-step-content" data-step="5">
              <h2 className="wizard-step-content__title">Advanced Settings</h2>
              <p className="wizard-step-content__description">
                Terminal dimensions and advanced timing controls. You can leave these as defaults
                unless you have specific needs.
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
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                      <optgroup label="Models">
                        {models
                          .filter((model) => model.configured)
                          .map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                      </optgroup>
                      <optgroup label="User Promptable">
                        {promptableTargets.map((target) => (
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                    </select>
                    <p className="form-group__hint">
                      Select a promptable target for NudgeNik session feedback, or leave disabled.
                    </p>
                    {nudgenikTargetMissing && (
                      <p className="form-group__error">
                        Selected target is not available or not promptable.
                      </p>
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
                        onChange={(e) =>
                          setViewedBuffer(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                          )
                        }
                      />
                      <p className="form-group__hint">
                        Time to keep session marked as "viewed" after last check
                      </p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Seen Interval (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={nudgenikSeenInterval === 0 ? '' : nudgenikSeenInterval}
                        onChange={(e) =>
                          setNudgenikSeenInterval(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 2000
                          )
                        }
                      />
                      <p className="form-group__hint">How often to check for session activity</p>
                    </div>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Branch Suggestion</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label className="form-group__label">Target</label>
                    <select
                      className="input"
                      value={branchSuggestTarget}
                      onChange={(e) => setBranchSuggestTarget(e.target.value)}
                    >
                      <option value="">Disabled</option>
                      <optgroup label="Detected Tools">
                        {detectedTargets.map((target) => (
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                      <optgroup label="Models">
                        {models
                          .filter((model) => model.configured)
                          .map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                      </optgroup>
                      <optgroup label="User Promptable">
                        {promptableTargets.map((target) => (
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                    </select>
                    <p className="form-group__hint">
                      Select a promptable target for branch name suggestion, or leave disabled.
                    </p>
                    {branchSuggestTargetMissing && (
                      <p className="form-group__error">
                        Selected target is not available or not promptable.
                      </p>
                    )}
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Conflict Resolution</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label className="form-group__label">Target</label>
                    <select
                      className="input"
                      value={conflictResolveTarget}
                      onChange={(e) => setConflictResolveTarget(e.target.value)}
                    >
                      <option value="">Disabled</option>
                      <optgroup label="Detected Tools">
                        {detectedTargets.map((target) => (
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                      <optgroup label="Models">
                        {models
                          .filter((model) => model.configured)
                          .map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                      </optgroup>
                      <optgroup label="User Promptable">
                        {promptableTargets.map((target) => (
                          <option key={target.name} value={target.name}>
                            {target.name}
                          </option>
                        ))}
                      </optgroup>
                    </select>
                    <p className="form-group__hint">
                      Select a promptable target for merge conflict resolution. When &quot;sync from
                      main conflict&quot; encounters a conflict, this target will be spawned to
                      resolve it.
                    </p>
                    {conflictResolveTargetMissing && (
                      <p className="form-group__error">
                        Selected target is not available or not promptable.
                      </p>
                    )}
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
                    <div
                      style={{
                        display: 'flex',
                        gap: 'var(--spacing-md)',
                        alignItems: 'center',
                        fontSize: '0.9rem',
                      }}
                    >
                      <label
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 'var(--spacing-xs)',
                          cursor: 'pointer',
                          fontSize: 'inherit',
                        }}
                      >
                        <input
                          type="radio"
                          name="networkAccess"
                          checked={!networkAccess}
                          onChange={() => setNetworkAccess(false)}
                        />
                        <span>Local access only</span>
                      </label>
                      <label
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 'var(--spacing-xs)',
                          cursor: 'pointer',
                          fontSize: 'inherit',
                        }}
                      >
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
                    <label
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 'var(--spacing-xs)',
                        cursor: 'pointer',
                      }}
                    >
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
                        <p className="form-group__hint">
                          The URL users will type to access schmux. Must be https.
                        </p>
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
                      <p
                        className="form-group__hint"
                        style={{ marginTop: 'calc(-1 * var(--spacing-sm))' }}
                      >
                        Use <code>mkcert</code> to generate local certificates, or run{' '}
                        <code>schmux auth github</code> for guided setup.
                      </p>

                      <div className="form-group">
                        <label className="form-group__label">Session TTL (minutes)</label>
                        <input
                          type="number"
                          className="input input--compact"
                          style={{ maxWidth: '120px' }}
                          min="1"
                          value={authSessionTTLMinutes}
                          onChange={(e) =>
                            setAuthSessionTTLMinutes(parseInt(e.target.value) || 1440)
                          }
                        />
                        <p className="form-group__hint">
                          How long before requiring re-authentication.
                        </p>
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
                                  <span style={{ color: 'var(--color-warning)' }}>
                                    Not configured
                                  </span>
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
                        <div
                          className="form-group__hint"
                          style={{ marginTop: 'var(--spacing-sm)' }}
                        >
                          <p
                            className="form-group__hint"
                            style={{ marginTop: 'calc(-1 * var(--spacing-sm))' }}
                          >
                            To create or check on your GitHub OAuth credentials, follow these steps:
                          </p>
                          <ol style={{ margin: 0, paddingLeft: 'var(--spacing-lg)' }}>
                            <li>
                              Go to{' '}
                              <a
                                href="https://github.com/settings/developers"
                                target="_blank"
                                rel="noreferrer"
                              >
                                github.com/settings/developers
                              </a>
                            </li>
                            <li>Click "New OAuth App" (or edit existing)</li>
                            <li>
                              Use these values:
                              <ul style={{ marginTop: 'var(--spacing-xs)' }}>
                                <li>
                                  Application name: <code>schmux</code>
                                </li>
                                <li>
                                  Homepage URL:{' '}
                                  <code>{authPublicBaseURL || 'https://schmux.local:7337'}</code>
                                </li>
                                <li>
                                  Callback URL:{' '}
                                  <code>
                                    {authPublicBaseURL
                                      ? `${authPublicBaseURL.replace(/\/+$/, '')}/auth/callback`
                                      : 'https://schmux.local:7337/auth/callback'}
                                  </code>
                                </li>
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
                  <h3 className="settings-section__title">Notifications</h3>
                </div>
                <div className="settings-section__body">
                  <div className="form-group">
                    <label
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 'var(--spacing-xs)',
                        cursor: 'pointer',
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={!soundDisabled}
                        onChange={(e) => setSoundDisabled(!e.target.checked)}
                      />
                      <span>Play sound when agents need attention</span>
                    </label>
                    <p className="form-group__hint">
                      Plays an audio notification when an agent signals it needs input or encounters
                      an error.
                    </p>
                  </div>
                  <div className="form-group">
                    <label
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 'var(--spacing-xs)',
                        cursor: 'pointer',
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={confirmBeforeClose}
                        onChange={(e) => setConfirmBeforeClose(e.target.checked)}
                      />
                      <span>Confirm before closing tab</span>
                    </label>
                    <p className="form-group__hint">
                      Shows a browser &ldquo;Leave site?&rdquo; dialog when closing or reloading the
                      dashboard tab.
                    </p>
                  </div>
                </div>
              </div>

              <div className="settings-section">
                <div className="settings-section__header">
                  <h3 className="settings-section__title">Model Versions</h3>
                </div>
                <div className="settings-section__body">
                  <p className="form-group__hint" style={{ marginBottom: 'var(--spacing-md)' }}>
                    Pin specific model versions or leave empty to use the latest.
                  </p>
                  {models
                    ?.filter((m) => m.provider === 'anthropic' && m.category === 'native')
                    .map((model) => (
                      <div key={model.id} className="form-group">
                        <label className="form-group__label">{model.display_name}</label>
                        <input
                          type="text"
                          className="input"
                          value={modelVersions[model.id] || ''}
                          onChange={(e) => {
                            const newVersions = { ...modelVersions };
                            if (e.target.value) {
                              newVersions[model.id] = e.target.value;
                            } else {
                              delete newVersions[model.id];
                            }
                            setModelVersions(newVersions);
                          }}
                          placeholder={`(latest - ${model.default_value})`}
                        />
                      </div>
                    ))}
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
                      <p className="form-group__hint">
                        Lines to send on initial WebSocket connection (default: 20000)
                      </p>
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
                        onChange={(e) =>
                          setDashboardPollInterval(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                          )
                        }
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
                        onChange={(e) =>
                          setGitStatusPollInterval(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 10000
                          )
                        }
                      />
                      <p className="form-group__hint">
                        How often to refresh git status (dirty, ahead, behind)
                      </p>
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
                        onChange={(e) =>
                          setGitCloneTimeout(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 300000
                          )
                        }
                      />
                      <p className="form-group__hint">
                        Maximum time to wait for git clone when spawning sessions (default: 300000ms
                        = 5 min)
                      </p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Git Status Timeout (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={gitStatusTimeout === 0 ? '' : gitStatusTimeout}
                        onChange={(e) =>
                          setGitStatusTimeout(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 30000
                          )
                        }
                      />
                      <p className="form-group__hint">
                        Maximum time to wait for git status/diff operations (default: 30000ms)
                      </p>
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
                        onChange={(e) =>
                          setMtimePollInterval(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                          )
                        }
                      />
                      <p className="form-group__hint">
                        How often to check log file mtimes for new output
                      </p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Query Timeout (ms)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="100"
                        value={xtermQueryTimeout === 0 ? '' : xtermQueryTimeout}
                        onChange={(e) =>
                          setXtermQueryTimeout(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                          )
                        }
                      />
                      <p className="form-group__hint">
                        Maximum time to wait for xterm query operations (default: 5000ms)
                      </p>
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
                        onChange={(e) =>
                          setXtermOperationTimeout(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 10000
                          )
                        }
                      />
                      <p className="form-group__hint">
                        Maximum time to wait for xterm operations (default: 10000ms)
                      </p>
                    </div>

                    <div className="form-group">
                      <label className="form-group__label">Max Log Size (MB)</label>
                      <input
                        type="number"
                        className="input input--compact"
                        min="1"
                        value={maxLogSizeMB === 0 ? '' : maxLogSizeMB}
                        onChange={(e) =>
                          setMaxLogSizeMB(
                            e.target.value === '' ? 0 : parseInt(e.target.value) || 50
                          )
                        }
                      />
                      <p className="form-group__hint">
                        Maximum log file size before rotation (default: 50MB)
                      </p>
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
                        onChange={(e) =>
                          setRotatedLogSizeMB(
                            e.target.value === ''
                              ? 0
                              : Math.min(parseInt(e.target.value) || 1, maxLogSizeMB)
                          )
                        }
                      />
                      <p className="form-group__hint">
                        Target log size after rotation - keeps the tail (default: 1MB)
                      </p>
                    </div>
                  </div>
                </div>
              </div>

              {stepErrors[5] && <p className="form-group__error">{stepErrors[5]}</p>}
            </div>
          )}
        </div>

        {/* Wizard footer navigation - only shown in first-run mode */}
        {isFirstRun && (
          <div className="wizard__actions">
            <div className="wizard__actions-left">
              {currentStep > 1 && (
                <button className="btn" onClick={prevStep} disabled={saving}>
                  ← Back
                </button>
              )}
            </div>
            <div className="wizard__actions-right">
              <button
                className="btn btn--primary"
                onClick={async () => {
                  if (currentStep < TOTAL_STEPS) {
                    nextStep();
                  } else {
                    const saved = await saveCurrentStep();
                    if (saved) {
                      completeFirstRun();
                      await show(
                        'Setup Complete! 🎉',
                        'schmux is ready to go. Spawn your first session to start working with run targets.',
                        { confirmText: 'Go to Spawn', cancelText: null }
                      );
                      navigate('/spawn');
                    }
                  }
                }}
                disabled={saving}
              >
                {saving ? 'Saving...' : currentStep === TOTAL_STEPS ? 'Finish Setup' : 'Next →'}
              </button>
            </div>
          </div>
        )}
      </div>

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
                  onChange={(e) =>
                    setAuthSecretsModal((current) =>
                      current ? { ...current, clientId: e.target.value } : null
                    )
                  }
                />
              </div>
              <div className="form-group">
                <label className="form-group__label">Client Secret</label>
                <input
                  type="password"
                  className="input"
                  placeholder="Enter client secret"
                  value={authSecretsModal.clientSecret}
                  onChange={(e) =>
                    setAuthSecretsModal((current) =>
                      current ? { ...current, clientSecret: e.target.value } : null
                    )
                  }
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
              <button className="btn" onClick={closeAuthSecretsModal}>
                Cancel
              </button>
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
                  onChange={(e) =>
                    setRunTargetEditModal((current) =>
                      current ? { ...current, command: e.target.value, error: '' } : null
                    )
                  }
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
              <button className="btn" onClick={closeRunTargetEditModal}>
                Cancel
              </button>
              <button className="btn btn--primary" onClick={saveRunTargetEditModal}>
                Save
              </button>
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
                    onChange={(e) =>
                      setQuickLaunchEditModal((current) =>
                        current ? { ...current, prompt: e.target.value, error: '' } : null
                      )
                    }
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
                    onChange={(e) =>
                      setQuickLaunchEditModal((current) =>
                        current ? { ...current, prompt: e.target.value, error: '' } : null
                      )
                    }
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
              <button className="btn" onClick={closeQuickLaunchEditModal}>
                Cancel
              </button>
              <button className="btn btn--primary" onClick={saveQuickLaunchEditModal}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
