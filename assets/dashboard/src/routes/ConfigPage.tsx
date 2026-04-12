import React, { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  getConfig,
  configureModelSecrets,
  removeModelSecrets,
  getOverlays,
  getBuiltinQuickLaunch,
  getPersonas,
  getAuthSecretsStatus,
  saveAuthSecrets,
  setRemoteAccessPassword,
  getErrorMessage,
  validateTLS,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useConfigForm } from './config/useConfigForm';
import { useAutoSave, type SaveStatus } from './config/useAutoSave';
import WorkspacesTab from './config/WorkspacesTab';
import SessionsTab from './config/SessionsTab';
import AccessTab from './config/AccessTab';
import AdvancedTab from './config/AdvancedTab';
import AgentsTab from './config/AgentsTab';
import ExperimentalTab from './config/ExperimentalTab';
import RemoteSettingsPage from './RemoteSettingsPage';
import ConfigModals from './config/ConfigModals';
import type { ConfigResponse, Model, RunTargetResponse } from '../lib/types';
import type { Persona } from '../lib/types.generated';

const TABS = [
  'Workspaces',
  'Sessions',
  'Agents',
  'Access',
  'Remote Hosts',
  'Experimental',
  'Advanced',
];
const TAB_SLUGS = [
  'workspaces',
  'sessions',
  'agents',
  'access',
  'remote',
  'experimental',
  'advanced',
];

const stepToSlug = (step: number) => TAB_SLUGS[step - 1];
const DISSOLVED_SLUGS = new Set([
  'pastebin',
  'codereview',
  'floormanager',
  'subreddit',
  'repofeed',
]);
const DISSOLVED_SLUG_REDIRECTS: Record<string, number> = {
  quicklaunch: 2, // Sessions tab
};
const slugToStep = (slug: string | null) => {
  if (slug && slug in DISSOLVED_SLUG_REDIRECTS) return DISSOLVED_SLUG_REDIRECTS[slug];
  if (slug && DISSOLVED_SLUGS.has(slug)) return 1;
  const index = slug ? TAB_SLUGS.indexOf(slug) : -1;
  return index >= 0 ? index + 1 : 1;
};

export default function ConfigPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { reloadConfig } = useConfig();
  const { confirm, prompt, alert } = useModal();
  const { success, error: toastError } = useToast();
  const isTabHidden = (_slug: string) => {
    return false;
  };

  const initialStep = searchParams.get('tab') ? slugToStep(searchParams.get('tab')) : 1;
  const {
    state,
    dispatch: rawDispatch,
    models,
    oneshotModels,
    branchSuggestTargetMissing,
    conflictResolveTargetMissing,
    prReviewTargetMissing,
    commitMessageTargetMissing,
    checkTargetUsage,
  } = useConfigForm(initialStep);

  const [saveStatus, setSaveStatus] = useState<SaveStatus>('idle');
  const saveStatusRef = useRef<SaveStatus>('idle');
  // Keep ref in sync for async callbacks
  saveStatusRef.current = saveStatus;

  const { dispatch, flushSave, setLastSavedConfig } = useAutoSave(
    state,
    rawDispatch,
    saveStatusRef,
    setSaveStatus
  );

  // Sync currentStep with URL
  useEffect(() => {
    const slug = stepToSlug(state.currentStep);
    setSearchParams({ tab: slug });
  }, [state.currentStep, setSearchParams]);

  // Load config
  useEffect(() => {
    let active = true;

    const load = async () => {
      rawDispatch({ type: 'SET_FIELD', field: 'loading', value: true });
      rawDispatch({ type: 'SET_FIELD', field: 'error', value: '' });
      try {
        const data: ConfigResponse = await getConfig();
        if (!active) return;

        const commandItems = data.run_targets || [];

        const netAccess = data.network?.bind_address === '0.0.0.0';

        const loadedState: Partial<import('./config/useConfigForm').ConfigFormState> = {
          workspacePath: data.workspace_path || '',
          sourceCodeManagement: data.source_code_management || 'git-worktree',
          recycleWorkspaces: data.recycle_workspaces ?? false,
          repos: (data.repos || []).sort((a, b) => a.name.localeCompare(b.name)),
          commandTargets: commandItems,
          quickLaunch: (data.quick_launch || []).sort((a, b) => a.name.localeCompare(b.name)),
          externalDiffCommands: data.external_diff_commands || [],
          externalDiffCleanupMinutes: Math.max(
            1,
            (data.external_diff_cleanup_after_ms || 3600000) / 60000
          ),
          pastebin: (data.pastebin || [])
            .slice()
            .sort((a: string, b: string) => a.localeCompare(b)),
          nudgenikTarget: data.nudgenik?.target || '',
          branchSuggestTarget: data.branch_suggest?.target || '',
          conflictResolveTarget: data.conflict_resolve?.target || '',
          prReviewTarget: data.pr_review?.target || '',
          commitMessageTarget: data.commit_message?.target || '',
          dashboardPollInterval: data.sessions?.dashboard_poll_interval_ms || 5000,
          viewedBuffer: data.nudgenik?.viewed_buffer_ms || 5000,
          nudgenikSeenInterval: data.nudgenik?.seen_interval_ms || 2000,
          gitStatusPollInterval: data.sessions?.git_status_poll_interval_ms || 10000,
          gitCloneTimeout: data.sessions?.git_clone_timeout_ms || 300000,
          gitStatusTimeout: data.sessions?.git_status_timeout_ms || 30000,
          xtermQueryTimeout: data.xterm?.query_timeout_ms || 5000,
          xtermOperationTimeout: data.xterm?.operation_timeout_ms || 10000,
          xtermUseWebGL: data.xterm?.use_webgl !== false,
          networkAccess: netAccess,
          authEnabled: data.access_control?.enabled || false,
          authProvider: data.access_control?.provider || 'github',
          authPublicBaseURL: data.network?.public_base_url || '',
          authSessionTTLMinutes: data.access_control?.session_ttl_minutes || 1440,
          authTlsCertPath: data.network?.tls?.cert_path || '',
          authTlsKeyPath: data.network?.tls?.key_path || '',
          authWarnings: [],
          apiNeedsRestart: data.needs_restart || false,
          soundDisabled: data.notifications?.sound_disabled || false,
          confirmBeforeClose: data.notifications?.confirm_before_close || false,
          suggestDisposeAfterPush: data.notifications?.suggest_dispose_after_push ?? true,
          enabledModels: data.enabled_models || {},
          commStyles: data.comm_styles || {},
          loreEnabled: data.lore?.enabled ?? true,
          loreLLMTarget: data.lore?.llm_target || '',
          loreCurateOnDispose: data.lore?.curate_on_dispose || 'session',
          loreAutoPR: data.lore?.auto_pr || false,
          lorePublicRuleMode: data.lore?.public_rule_mode || 'direct_push',
          subredditEnabled: data.subreddit?.enabled ?? false,
          subredditTarget: data.subreddit?.target || '',
          subredditInterval: data.subreddit?.interval || 30,
          subredditCheckingRange: data.subreddit?.checking_range || 48,
          subredditMaxPosts: data.subreddit?.max_posts || 30,
          subredditMaxAge: data.subreddit?.max_age || 14,
          subredditRepos: data.subreddit?.repos || {},
          repofeedEnabled: data.repofeed?.enabled || false,
          repofeedPublishInterval: data.repofeed?.publish_interval_seconds || 30,
          repofeedFetchInterval: data.repofeed?.fetch_interval_seconds || 60,
          repofeedCompletedRetention: data.repofeed?.completed_retention_hours || 48,
          repofeedRepos: data.repofeed?.repos || {},
          remoteAccessEnabled: data.remote_access?.enabled || false,
          remoteAccessTimeoutMinutes: data.remote_access?.timeout_minutes || 0,
          remoteAccessNtfyTopic: data.remote_access?.notify?.ntfy_topic || '',
          remoteAccessNotifyCommand: data.remote_access?.notify?.command || '',
          remoteAccessPasswordHashSet: data.remote_access?.password_hash_set || false,
          desyncEnabled: data.desync?.enabled || false,
          desyncTarget: data.desync?.target || '',
          fmEnabled: data.floor_manager?.enabled || false,
          fmTarget: data.floor_manager?.target || '',
          fmRotationThreshold: data.floor_manager?.rotation_threshold || 150,
          fmDebounceMs: data.floor_manager?.debounce_ms || 2000,
          timelapseEnabled: data.timelapse?.enabled ?? true,
          timelapseRetentionDays: data.timelapse?.retention_days || 7,
          timelapseMaxFileSizeMB: data.timelapse?.max_file_size_mb || 50,
          timelapseMaxTotalStorageMB: data.timelapse?.max_total_storage_mb || 500,
          ioWorkspaceTelemetryEnabled: data.io_workspace_telemetry?.enabled || false,
          ioWorkspaceTelemetryTarget: data.io_workspace_telemetry?.target || '',
          saplingCmdCreateWorkspace: data.sapling_commands?.create_workspace || '',
          saplingCmdRemoveWorkspace: data.sapling_commands?.remove_workspace || '',
          saplingCmdCheckRepoBase: data.sapling_commands?.check_repo_base || '',
          saplingCmdCreateRepoBase: data.sapling_commands?.create_repo_base || '',
          personasEnabled: data.personas_enabled ?? false,
          commStylesEnabled: data.comm_styles_enabled ?? false,
          localEchoRemote: data.local_echo_remote || false,
          debugUI: data.debug_ui ?? false,
          tmuxBinary: data.tmux_binary || '',
          tmuxSocketName: data.tmux_socket_name || '',
          modelCatalog: data.models || [],
          runners: data.runners || {},
        };

        rawDispatch({ type: 'LOAD_CONFIG', state: loadedState });

        // Initialize auto-save baseline from loaded config
        setLastSavedConfig({
          ...state,
          ...loadedState,
        } as import('./config/useConfigForm').ConfigFormState);

        const authStatus = await getAuthSecretsStatus();
        if (active) {
          rawDispatch({
            type: 'SET_FIELD',
            field: 'authClientIdSet',
            value: !!authStatus.client_id,
          });
          rawDispatch({
            type: 'SET_FIELD',
            field: 'authClientSecretSet',
            value: !!authStatus.client_secret_set,
          });
          rawDispatch({
            type: 'SET_FIELD',
            field: 'authClientId',
            value: authStatus.client_id || '',
          });
          rawDispatch({
            type: 'SET_FIELD',
            field: 'authClientSecretWasSet',
            value: !!authStatus.client_secret_set,
          });
        }
      } catch (err) {
        if (!active) return;
        const message = err instanceof Error ? err.message : 'Failed to load config';
        rawDispatch({ type: 'SET_FIELD', field: 'error', value: message });
      } finally {
        if (active) rawDispatch({ type: 'SET_FIELD', field: 'loading', value: false });
      }
    };

    load();
    return () => {
      active = false;
    };
  }, []);

  // Load overlays
  useEffect(() => {
    let active = true;
    const loadOverlays = async () => {
      rawDispatch({ type: 'SET_FIELD', field: 'loadingOverlays', value: true });
      try {
        const data = await getOverlays();
        if (!active) return;
        rawDispatch({ type: 'SET_FIELD', field: 'overlays', value: data.overlays || [] });
      } catch (err) {
        if (!active) return;
        console.error('Failed to load overlays:', err);
      } finally {
        if (active) rawDispatch({ type: 'SET_FIELD', field: 'loadingOverlays', value: false });
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
          rawDispatch({ type: 'SET_FIELD', field: 'builtinQuickLaunch', value: data || [] });
        }
      } catch (err) {
        if (!active) return;
        console.warn('Failed to load built-in quick launch templates:', err);
      }
    };
    loadBuiltinQuickLaunch();
    return () => {
      active = false;
    };
  }, []);

  // Load personas for quick launch persona selector
  const [personas, setPersonas] = useState<Persona[]>([]);
  useEffect(() => {
    let active = true;
    const loadPersonas = async () => {
      try {
        const data = await getPersonas();
        if (active) setPersonas(data.personas || []);
      } catch {
        // Non-fatal: personas are optional
      }
    };
    loadPersonas();
    return () => {
      active = false;
    };
  }, []);

  // Auth warnings computation
  const localAuthWarnings: string[] = [];
  if (state.authEnabled) {
    if (!state.authPublicBaseURL.trim()) {
      localAuthWarnings.push('Public base URL is required when auth is enabled.');
    } else if (
      !state.authPublicBaseURL.startsWith('https://') &&
      !state.authPublicBaseURL.startsWith('http://localhost')
    ) {
      localAuthWarnings.push('Public base URL must be https (http://localhost allowed).');
    }
    if (!state.authTlsCertPath.trim()) {
      localAuthWarnings.push('TLS cert path is required when auth is enabled.');
    }
    if (!state.authTlsKeyPath.trim()) {
      localAuthWarnings.push('TLS key path is required when auth is enabled.');
    }
    if (!state.authClientIdSet || !state.authClientSecretSet) {
      localAuthWarnings.push('GitHub client credentials are not configured.');
    }
  }
  const combinedAuthWarnings = Array.from(new Set([...localAuthWarnings, ...state.authWarnings]));

  // HTTPS derived state
  const httpsEnabled = !!(state.authTlsCertPath.trim() && state.authTlsKeyPath.trim());
  const tlsHostname = state.authPublicBaseURL
    ? (() => {
        try {
          const url = new URL(state.authPublicBaseURL);
          return url.hostname;
        } catch {
          return '';
        }
      })()
    : '';
  const tlsExpires = ''; // We don't store this, would need to validate again

  const openTlsModal = () => {
    dispatch({
      type: 'SET_TLS_MODAL',
      modal: {
        certPath: state.authTlsCertPath,
        keyPath: state.authTlsKeyPath,
        hostname: '',
        expires: '',
        validating: false,
        error: '',
      },
    });
  };

  const handleTlsValidate = async () => {
    if (!state.tlsModal) return;
    const { certPath, keyPath } = state.tlsModal;

    if (!certPath.trim() || !keyPath.trim()) {
      dispatch({
        type: 'SET_TLS_MODAL',
        modal: { ...state.tlsModal, error: 'Both cert and key paths are required' },
      });
      return;
    }

    dispatch({
      type: 'SET_TLS_MODAL',
      modal: { ...state.tlsModal, validating: true, error: '' },
    });

    try {
      const result = await validateTLS(certPath, keyPath);
      if (result.valid) {
        dispatch({
          type: 'SET_TLS_MODAL',
          modal: {
            ...state.tlsModal,
            hostname: result.hostname || '',
            expires: result.expires || '',
            validating: false,
          },
        });
      } else {
        dispatch({
          type: 'SET_TLS_MODAL',
          modal: {
            ...state.tlsModal,
            validating: false,
            error: result.error || 'Validation failed',
          },
        });
      }
    } catch (err) {
      dispatch({
        type: 'SET_TLS_MODAL',
        modal: {
          ...state.tlsModal,
          validating: false,
          error: getErrorMessage(err, 'Failed to validate TLS certificates'),
        },
      });
    }
  };

  const handleTlsModalSave = () => {
    if (!state.tlsModal) return;
    const { certPath, keyPath, hostname, expires } = state.tlsModal;

    // Update the form state with the validated paths
    dispatch({ type: 'SET_FIELD', field: 'authTlsCertPath', value: certPath });
    dispatch({ type: 'SET_FIELD', field: 'authTlsKeyPath', value: keyPath });

    // Derive public_base_url from hostname if we have one
    if (hostname) {
      const port = state.authPublicBaseURL
        ? (() => {
            try {
              const url = new URL(state.authPublicBaseURL);
              return url.port || '7337';
            } catch {
              return '7337';
            }
          })()
        : '7337';
      dispatch({
        type: 'SET_FIELD',
        field: 'authPublicBaseURL',
        value: `https://${hostname}:${port}`,
      });
    }

    dispatch({ type: 'SET_TLS_MODAL', modal: null });
    success('TLS certificate configured');
  };

  const reloadModels = async () => {
    try {
      const data = await getConfig();
      dispatch({ type: 'SET_MODELS', models: data.models || [] });
      dispatch({ type: 'SET_FIELD', field: 'runners', value: data.runners || {} });
    } catch (err) {
      alert('Load Models Failed', getErrorMessage(err, 'Failed to load models'));
    }
  };

  // Handler functions
  const handleEditWorkspacePath = async () => {
    const newPath = await prompt('Edit Workspace Directory', {
      defaultValue: state.workspacePath,
      placeholder: '~/schmux-workspaces',
      confirmText: 'Save',
    });
    if (newPath !== null) {
      dispatch({ type: 'SET_FIELD', field: 'workspacePath', value: newPath });
    }
  };

  const addRepo = () => {
    if (!state.newRepoName.trim()) {
      toastError('Repo name is required');
      return;
    }
    if (!state.newRepoUrl.trim()) {
      toastError('Repo URL is required');
      return;
    }
    if (state.repos.some((r) => r.name === state.newRepoName)) {
      toastError('Repo name already exists');
      return;
    }
    dispatch({
      type: 'ADD_REPO',
      repo: { name: state.newRepoName, url: state.newRepoUrl, vcs: state.newRepoVcs || undefined },
    });
    dispatch({ type: 'RESET_NEW_REPO' });
  };

  const removeRepo = async (name: string) => {
    const confirmed = await confirm('Remove repo?', `Remove "${name}" from the config?`);
    if (confirmed) {
      dispatch({ type: 'REMOVE_REPO', name });
    }
  };

  const addCommand = () => {
    if (!state.newCommandName.trim()) {
      toastError('Command name is required');
      return;
    }
    if (!state.newCommandCommand.trim()) {
      toastError('Command is required');
      return;
    }
    const nameExists = state.commandTargets.some((t) => t.name === state.newCommandName);
    if (nameExists) {
      toastError('Run target name already exists');
      return;
    }
    dispatch({
      type: 'ADD_COMMAND_TARGET',
      target: {
        name: state.newCommandName,
        command: state.newCommandCommand,
      },
    });
    dispatch({ type: 'RESET_NEW_COMMAND' });
  };

  const removeCommand = async (name: string) => {
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
      dispatch({ type: 'REMOVE_COMMAND_TARGET', name });
    }
  };

  const openAddAgentDialog = () => {
    dispatch({
      type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
      modal: { mode: 'add', kind: 'agent', name: '', target: '', prompt: '', error: '' },
    });
  };

  const openAddCommandDialog = () => {
    dispatch({
      type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
      modal: { mode: 'add', kind: 'command', name: '', command: '', error: '' },
    });
  };

  const openEditQuickLaunchDialog = (item: import('../lib/types').QuickLaunchPreset) => {
    const commandTarget = state.commandTargets.find((t) => t.name === item.target);
    if (item.command || commandTarget) {
      dispatch({
        type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
        modal: {
          mode: 'edit',
          kind: 'command',
          name: item.name,
          originalName: item.name,
          command: item.command || commandTarget?.command || '',
          error: '',
        },
      });
    } else {
      dispatch({
        type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
        modal: {
          mode: 'edit',
          kind: 'agent',
          name: item.name,
          originalName: item.name,
          target: item.target || '',
          prompt: item.prompt || '',
          personaId: item.persona_id || '',
          error: '',
        },
      });
    }
  };

  const openCookbookDialog = (template: import('../lib/types').BuiltinQuickLaunchCookbook) => {
    dispatch({
      type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
      modal: {
        mode: 'add',
        kind: 'agent',
        name: template.name,
        target: '',
        prompt: template.prompt,
        error: '',
      },
    });
  };

  const saveQuickLaunchDialog = () => {
    if (!state.quickLaunchDialogModal) return;
    const modal = state.quickLaunchDialogModal;

    if (modal.kind === 'command') {
      const command = (modal.command || '').trim();
      if (!command) {
        dispatch({
          type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
          modal: { ...modal, error: 'Command is required' },
        });
        return;
      }
      const name = modal.name.trim() || command;
      if (modal.mode === 'add' && state.quickLaunch.some((q) => q.name === name)) {
        dispatch({
          type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
          modal: { ...modal, error: 'Quick launch name already exists' },
        });
        return;
      }
      if (modal.mode === 'add') {
        dispatch({ type: 'ADD_QUICK_LAUNCH', item: { name, command } });
      } else {
        dispatch({
          type: 'UPDATE_QUICK_LAUNCH',
          name: modal.originalName!,
          updates: { name, command },
        });
      }
      dispatch({ type: 'SET_QUICK_LAUNCH_DIALOG_MODAL', modal: null });
      return;
    }

    // Agent kind
    const target = (modal.target || '').trim();
    if (!target) {
      dispatch({
        type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
        modal: { ...modal, error: 'Model is required' },
      });
      return;
    }
    const name = modal.name.trim() || target;
    if (modal.mode === 'add' && state.quickLaunch.some((q) => q.name === name)) {
      dispatch({
        type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
        modal: { ...modal, error: 'Quick launch name already exists' },
      });
      return;
    }
    const prompt = (modal.prompt || '').trim();
    if (!prompt) {
      dispatch({
        type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
        modal: { ...modal, error: 'Prompt is required' },
      });
      return;
    }

    if (modal.mode === 'add') {
      dispatch({
        type: 'ADD_QUICK_LAUNCH',
        item: { name, target, prompt, persona_id: modal.personaId || undefined },
      });
    } else {
      dispatch({
        type: 'UPDATE_QUICK_LAUNCH',
        name: modal.originalName!,
        updates: { name, target, prompt, persona_id: modal.personaId || undefined },
      });
    }
    dispatch({ type: 'SET_QUICK_LAUNCH_DIALOG_MODAL', modal: null });
  };

  const removeQuickLaunch = async (name: string) => {
    const confirmed = await confirm('Remove quick launch?', `Remove "${name}" from the config?`);
    if (confirmed) {
      dispatch({ type: 'REMOVE_QUICK_LAUNCH', name });
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
        alert('Remove Secrets Failed', getErrorMessage(err, 'Failed to remove model secrets'));
      }
      return;
    }

    const secretKey = model.required_secrets?.[0];
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
      alert('Save Secrets Failed', getErrorMessage(err, 'Failed to save model secrets'));
    }
  };

  const openRunTargetEditModal = (target: RunTargetResponse) => {
    dispatch({
      type: 'SET_RUN_TARGET_EDIT_MODAL',
      modal: { target, command: target.command, error: '' },
    });
  };

  const saveRunTargetEditModal = () => {
    if (!state.runTargetEditModal) return;
    const { target, command } = state.runTargetEditModal;
    if (!command.trim()) {
      dispatch({
        type: 'SET_RUN_TARGET_EDIT_MODAL',
        modal: { ...state.runTargetEditModal, error: 'Command is required' },
      });
      return;
    }
    dispatch({ type: 'UPDATE_COMMAND_TARGET', name: target.name, command });
    dispatch({ type: 'SET_RUN_TARGET_EDIT_MODAL', modal: null });
  };

  const savePastebinEditModal = () => {
    if (!state.pastebinEditModal) return;
    const { index, content } = state.pastebinEditModal;

    if (!content.trim()) {
      dispatch({
        type: 'SET_PASTEBIN_EDIT_MODAL',
        modal: { ...state.pastebinEditModal, error: 'Content cannot be empty' },
      });
      return;
    }

    if (index !== undefined) {
      dispatch({ type: 'UPDATE_PASTEBIN', index, content: content.trim() });
    } else {
      dispatch({ type: 'ADD_PASTEBIN', content: content.trim() });
    }
  };

  const openAuthSecretsModal = async () => {
    const status = await getAuthSecretsStatus();
    dispatch({
      type: 'SET_AUTH_SECRETS_MODAL',
      modal: {
        clientId: status.client_id || '',
        clientSecret: status.client_secret_set ? '••••••••' : '',
        clientSecretWasSet: status.client_secret_set,
        error: '',
      },
    });
  };

  const saveAuthSecretsModal = async () => {
    if (!state.authSecretsModal) return;
    const { clientId, clientSecret, clientSecretWasSet } = state.authSecretsModal;

    // Validate client ID is required
    if (!clientId.trim()) {
      rawDispatch({
        type: 'SET_AUTH_SECRETS_MODAL',
        modal: {
          ...state.authSecretsModal,
          error: 'Client ID is required',
        },
      });
      return;
    }

    // Validate client secret is required for first-time setup
    if (!clientSecretWasSet && !clientSecret.trim()) {
      rawDispatch({
        type: 'SET_AUTH_SECRETS_MODAL',
        modal: {
          ...state.authSecretsModal,
          error: 'Client secret is required for initial setup',
        },
      });
      return;
    }

    // Save auth secrets directly (separate from config auto-save)
    const newSecret =
      clientSecret.trim() && clientSecret !== '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022'
        ? clientSecret.trim()
        : '';
    try {
      const secretPayload: { client_id: string; client_secret?: string } = {
        client_id: clientId.trim(),
      };
      if (newSecret) {
        secretPayload.client_secret = newSecret;
      }
      await saveAuthSecrets(secretPayload);

      // Reload auth status
      const authStatus = await getAuthSecretsStatus();
      rawDispatch({ type: 'SET_FIELD', field: 'authClientId', value: authStatus.client_id || '' });
      rawDispatch({ type: 'SET_FIELD', field: 'authClientIdSet', value: !!authStatus.client_id });
      rawDispatch({
        type: 'SET_FIELD',
        field: 'authClientSecretSet',
        value: !!authStatus.client_secret_set,
      });
      rawDispatch({
        type: 'SET_FIELD',
        field: 'authClientSecretWasSet',
        value: !!authStatus.client_secret_set,
      });
      rawDispatch({ type: 'SET_AUTH_SECRETS_MODAL', modal: null });
      success('Auth credentials saved');
    } catch (err) {
      rawDispatch({
        type: 'SET_AUTH_SECRETS_MODAL',
        modal: {
          ...state.authSecretsModal,
          error: getErrorMessage(err, 'Failed to save auth credentials'),
        },
      });
    }
  };

  const handleSetPassword = async () => {
    dispatch({ type: 'SET_FIELD', field: 'passwordError', value: '' });
    dispatch({ type: 'SET_FIELD', field: 'passwordSuccess', value: '' });
    if (!state.passwordInput.trim()) {
      dispatch({ type: 'SET_FIELD', field: 'passwordError', value: 'Password cannot be empty' });
      return;
    }
    if (state.passwordInput.length < 8) {
      dispatch({
        type: 'SET_FIELD',
        field: 'passwordError',
        value: 'Password must be at least 8 characters',
      });
      return;
    }
    if (state.passwordInput !== state.passwordConfirm) {
      dispatch({
        type: 'SET_FIELD',
        field: 'passwordError',
        value: 'Passwords do not match',
      });
      return;
    }
    dispatch({ type: 'SET_FIELD', field: 'passwordSaving', value: true });
    try {
      await setRemoteAccessPassword(state.passwordInput);
      dispatch({ type: 'SET_FIELD', field: 'remoteAccessPasswordHashSet', value: true });
      dispatch({ type: 'SET_FIELD', field: 'passwordInput', value: '' });
      dispatch({ type: 'SET_FIELD', field: 'passwordConfirm', value: '' });
      dispatch({ type: 'SET_FIELD', field: 'passwordSuccess', value: 'Password updated' });
      reloadConfig();
    } catch (err) {
      dispatch({
        type: 'SET_FIELD',
        field: 'passwordError',
        value: getErrorMessage(err, 'Failed to set password'),
      });
    } finally {
      dispatch({ type: 'SET_FIELD', field: 'passwordSaving', value: false });
    }
  };

  const addDiffCommand = () => {
    const name = state.newDiffName.trim();
    const command = state.newDiffCommand.trim();
    if (state.externalDiffCommands.some((c) => c.name === name)) {
      toastError('Diff tool name already exists');
      return;
    }
    dispatch({ type: 'ADD_DIFF_COMMAND', command: { name, command } });
    dispatch({ type: 'RESET_NEW_DIFF' });
  };

  const setCurrentStep = (step: number) => {
    rawDispatch({ type: 'SET_FIELD', field: 'currentStep', value: step });
  };

  // Early returns
  if (state.loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading configuration...</span>
      </div>
    );
  }

  if (state.error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Failed to load config</h3>
        <p className="empty-state__description">{state.error}</p>
      </div>
    );
  }

  const currentTab = state.currentStep;

  return (
    <>
      <div className="config-sticky-header">
        <div className="config-sticky-header__title-row">
          <h1 className="config-sticky-header__title">Settings</h1>
          <div className="config-sticky-header__actions">
            {saveStatus === 'error' && (
              <span
                className="config-save-status config-save-status--error"
                data-testid="config-save-status"
                data-status="error"
              >
                Error saving
              </span>
            )}
          </div>
        </div>
        <div className="wizard__steps wizard__steps--compact">
          {Array.from({ length: TABS.length }, (_, i) => i + 1).map((stepNum) => {
            const isCurrent = stepNum === state.currentStep;
            const stepLabel = TABS[stepNum - 1];
            if (isTabHidden(TAB_SLUGS[stepNum - 1])) return null;

            return (
              <div
                key={stepNum}
                className={`wizard__step cursor-pointer ${isCurrent ? 'wizard__step--active' : ''}`}
                data-step={stepNum}
                data-testid={`config-tab-${TAB_SLUGS[stepNum - 1]}`}
                aria-selected={isCurrent}
                onClick={() => setCurrentStep(stepNum)}
              >
                {stepLabel}
              </div>
            );
          })}
        </div>
      </div>

      {state.warning && (
        <div className="banner banner--warning mb-lg">
          <p className="m-0">
            <strong>Warning:</strong> {state.warning}
          </p>
        </div>
      )}

      {state.apiNeedsRestart && (
        <div className="banner banner--warning mb-lg">
          <p className="m-0">
            <strong>Restart required:</strong> Network access setting has changed. Restart the
            daemon for this setting to take effect: <code>./schmux stop && ./schmux start</code>
          </p>
        </div>
      )}

      {/* Config content */}
      <div className="wizard">
        <div className="wizard__content">
          {currentTab === 1 && (
            <WorkspacesTab
              workspacePath={state.workspacePath}
              recycleWorkspaces={state.recycleWorkspaces}
              repos={state.repos}
              overlays={state.overlays}
              newRepoName={state.newRepoName}
              newRepoUrl={state.newRepoUrl}
              newRepoVcs={state.newRepoVcs}
              dispatch={dispatch}
              onEditWorkspacePath={handleEditWorkspacePath}
              onRemoveRepo={removeRepo}
              onAddRepo={addRepo}
            />
          )}

          {currentTab === 2 && (
            <SessionsTab
              state={state}
              dispatch={dispatch}
              models={models}
              personas={personas}
              builtinQuickLaunch={state.builtinQuickLaunch}
              onEditQuickLaunch={openEditQuickLaunchDialog}
              onRemoveQuickLaunch={removeQuickLaunch}
              onAddAgent={openAddAgentDialog}
              onAddQuickLaunchCommand={openAddCommandDialog}
              onAddFromCookbook={openCookbookDialog}
              onOpenPastebinEditModal={(index, content) => {
                dispatch({
                  type: 'SET_PASTEBIN_EDIT_MODAL',
                  modal: { index, content, error: '' },
                });
              }}
              onOpenAddPastebinModal={() => {
                dispatch({
                  type: 'SET_PASTEBIN_EDIT_MODAL',
                  modal: { content: '', error: '' },
                });
              }}
              onAddCommand={addCommand}
              onRemoveCommand={removeCommand}
              onOpenRunTargetEditModal={openRunTargetEditModal}
            />
          )}

          {currentTab === 3 && (
            <AgentsTab
              state={state}
              dispatch={dispatch}
              models={state.modelCatalog}
              runners={state.runners}
              onModelAction={handleModelAction}
              onOpenRunTargetEditModal={openRunTargetEditModal}
              commitMessageTargetMissing={commitMessageTargetMissing}
              prReviewTargetMissing={prReviewTargetMissing}
              branchSuggestTargetMissing={branchSuggestTargetMissing}
              conflictResolveTargetMissing={conflictResolveTargetMissing}
            />
          )}

          {currentTab === 4 && (
            <AccessTab
              networkAccess={state.networkAccess}
              remoteAccessEnabled={state.remoteAccessEnabled}
              remoteAccessPasswordHashSet={state.remoteAccessPasswordHashSet}
              passwordInput={state.passwordInput}
              passwordConfirm={state.passwordConfirm}
              passwordSaving={state.passwordSaving}
              passwordError={state.passwordError}
              passwordSuccess={state.passwordSuccess}
              remoteAccessTimeoutMinutes={state.remoteAccessTimeoutMinutes}
              remoteAccessNtfyTopic={state.remoteAccessNtfyTopic}
              remoteAccessNotifyCommand={state.remoteAccessNotifyCommand}
              authEnabled={state.authEnabled}
              authPublicBaseURL={state.authPublicBaseURL}
              authTlsCertPath={state.authTlsCertPath}
              authTlsKeyPath={state.authTlsKeyPath}
              authSessionTTLMinutes={state.authSessionTTLMinutes}
              authClientIdSet={state.authClientIdSet}
              authClientSecretSet={state.authClientSecretSet}
              combinedAuthWarnings={combinedAuthWarnings}
              httpsEnabled={httpsEnabled}
              tlsHostname={tlsHostname}
              tlsExpires={tlsExpires}
              tlsModalCertPath={state.tlsModal?.certPath || ''}
              tlsModalKeyPath={state.tlsModal?.keyPath || ''}
              tlsModalValidating={state.tlsModal?.validating || false}
              tlsModalHostname={state.tlsModal?.hostname || ''}
              tlsModalExpires={state.tlsModal?.expires || ''}
              tlsModalError={state.tlsModal?.error || ''}
              dispatch={dispatch}
              onSetPassword={handleSetPassword}
              onOpenAuthSecretsModal={openAuthSecretsModal}
              onOpenTlsModal={openTlsModal}
              success={success}
              toastError={toastError}
            />
          )}

          {currentTab === 5 && <RemoteSettingsPage />}

          {currentTab === 6 && (
            <ExperimentalTab state={state} dispatch={dispatch} models={oneshotModels} />
          )}

          {currentTab === 7 && (
            <AdvancedTab
              desyncEnabled={state.desyncEnabled}
              desyncTarget={state.desyncTarget}
              ioWorkspaceTelemetryEnabled={state.ioWorkspaceTelemetryEnabled}
              ioWorkspaceTelemetryTarget={state.ioWorkspaceTelemetryTarget}
              dashboardPollInterval={state.dashboardPollInterval}
              gitStatusPollInterval={state.gitStatusPollInterval}
              gitCloneTimeout={state.gitCloneTimeout}
              gitStatusTimeout={state.gitStatusTimeout}
              xtermQueryTimeout={state.xtermQueryTimeout}
              xtermOperationTimeout={state.xtermOperationTimeout}
              xtermUseWebGL={state.xtermUseWebGL}
              localEchoRemote={state.localEchoRemote}
              debugUI={state.debugUI}
              hasSaplingRepos={state.repos.some((r) => r.vcs === 'sapling')}
              saplingCmdCreateWorkspace={state.saplingCmdCreateWorkspace}
              saplingCmdRemoveWorkspace={state.saplingCmdRemoveWorkspace}
              saplingCmdCheckRepoBase={state.saplingCmdCheckRepoBase}
              saplingCmdCreateRepoBase={state.saplingCmdCreateRepoBase}
              tmuxBinary={state.tmuxBinary}
              tmuxSocketName={state.tmuxSocketName}
              externalDiffCommands={state.externalDiffCommands}
              externalDiffCleanupMinutes={state.externalDiffCleanupMinutes}
              newDiffName={state.newDiffName}
              newDiffCommand={state.newDiffCommand}
              onAddDiffCommand={addDiffCommand}
              models={oneshotModels}
              dispatch={dispatch}
            />
          )}
        </div>
      </div>

      <ConfigModals
        authSecretsModal={state.authSecretsModal}
        runTargetEditModal={state.runTargetEditModal}
        quickLaunchDialogModal={state.quickLaunchDialogModal}
        pastebinEditModal={state.pastebinEditModal}
        tlsModal={state.tlsModal}
        dispatch={dispatch}
        onSaveAuthSecrets={saveAuthSecretsModal}
        onSaveRunTargetEdit={saveRunTargetEditModal}
        onSaveQuickLaunchDialog={saveQuickLaunchDialog}
        onSavePastebinEdit={savePastebinEditModal}
        onSaveTls={handleTlsModalSave}
        onValidateTls={handleTlsValidate}
        authPublicBaseURL={state.authPublicBaseURL}
        models={models}
        personas={personas}
      />
    </>
  );
}
