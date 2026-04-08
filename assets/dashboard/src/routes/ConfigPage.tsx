import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useFeatures } from '../contexts/FeaturesContext';
import {
  getConfig,
  updateConfig,
  configureModelSecrets,
  removeModelSecrets,
  getOverlays,
  getBuiltinQuickLaunch,
  getPersonas,
  getStyles,
  getAuthSecretsStatus,
  saveAuthSecrets,
  setRemoteAccessPassword,
  getErrorMessage,
  validateTLS,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { CONFIG_UPDATED_KEY } from '../lib/constants';
import { useConfigForm, type ConfigSnapshot } from './config/useConfigForm';
import WorkspacesTab from './config/WorkspacesTab';
import SessionsTab from './config/SessionsTab';
import QuickLaunchTab from './config/QuickLaunchTab';
import CodeReviewTab from './config/CodeReviewTab';
import FloorManagerTab from './config/FloorManagerTab';
import AccessTab from './config/AccessTab';
import SubredditTab from './config/SubredditTab';
import RepofeedTab from './config/RepofeedTab';
import AdvancedTab from './config/AdvancedTab';
import PastebinTab from './config/PastebinTab';
import ConfigModals from './config/ConfigModals';
import type { ConfigResponse, ConfigUpdateRequest, Model, RunTargetResponse } from '../lib/types';
import type { Persona, Style } from '../lib/types.generated';

const TOTAL_STEPS = 10;
const TABS = [
  'Workspaces',
  'Sessions',
  'Quick Launch',
  'Pastebin',
  'Code Review',
  'Floor Manager',
  'Access',
  'Subreddit',
  'Repofeed',
  'Advanced',
];
const TAB_SLUGS = [
  'workspaces',
  'sessions',
  'quicklaunch',
  'pastebin',
  'codereview',
  'floormanager',
  'access',
  'subreddit',
  'repofeed',
  'advanced',
];

const stepToSlug = (step: number) => TAB_SLUGS[step - 1];
const slugToStep = (slug: string | null) => {
  const index = slug ? TAB_SLUGS.indexOf(slug) : -1;
  return index >= 0 ? index + 1 : 1;
};

export default function ConfigPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { isNotConfigured, isFirstRun, completeFirstRun, reloadConfig } = useConfig();
  const { show, confirm, prompt, alert } = useModal();
  const { success, error: toastError } = useToast();
  const { features } = useFeatures();

  const isTabHidden = (slug: string) => {
    if (slug === 'subreddit' && !features.subreddit) return true;
    if (slug === 'repofeed' && !features.repofeed) return true;
    return false;
  };

  const initialStep = searchParams.get('tab') ? slugToStep(searchParams.get('tab')) : 1;
  const {
    state,
    dispatch,
    models,
    oneshotModels,
    modelTargetNames,
    commandTargetNames,
    nudgenikTargetMissing,
    branchSuggestTargetMissing,
    conflictResolveTargetMissing,
    prReviewTargetMissing,
    commitMessageTargetMissing,
    hasChanges,
    checkTargetUsage,
    snapshotConfig,
  } = useConfigForm(initialStep);

  // Sync currentStep with URL (only in non-wizard mode)
  useEffect(() => {
    if (!isFirstRun) {
      const slug = stepToSlug(state.currentStep);
      setSearchParams({ tab: slug });
    }
  }, [state.currentStep, isFirstRun, setSearchParams]);

  // Browser close/refresh warning
  useEffect(() => {
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      if (!isFirstRun && hasChanges(isFirstRun)) {
        e.preventDefault();
        e.returnValue = '';
      }
    };

    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [isFirstRun]); // Dependency doesn't include hasChanges values - function reads current state

  // Load config
  useEffect(() => {
    let active = true;

    const load = async () => {
      dispatch({ type: 'SET_FIELD', field: 'loading', value: true });
      dispatch({ type: 'SET_FIELD', field: 'error', value: '' });
      try {
        const data: ConfigResponse = await getConfig();
        if (!active) return;

        const commandItems = data.run_targets || [];

        const netAccess = data.network?.bind_address === '0.0.0.0';

        dispatch({
          type: 'LOAD_CONFIG',
          state: {
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
            localEchoRemote: data.local_echo_remote || false,
            debugUI: data.debug_ui ?? false,
            tmuxBinary: data.tmux_binary || '',
            tmuxSocketName: data.tmux_socket_name || '',
            modelCatalog: data.models || [],
            runners: data.runners || {},
          },
        });

        // Set original config for change detection (non-wizard mode)
        if (!isFirstRun) {
          const originalConfig: ConfigSnapshot = {
            workspacePath: data.workspace_path || '',
            sourceCodeManagement: data.source_code_management || 'git-worktree',
            recycleWorkspaces: data.recycle_workspaces ?? false,
            repos: (data.repos || []).sort((a, b) => a.name.localeCompare(b.name)),
            commandTargets: commandItems,
            quickLaunch: data.quick_launch || [],
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
            localEchoRemote: data.local_echo_remote || false,
            debugUI: data.debug_ui ?? false,
            tmuxBinary: data.tmux_binary || '',
            tmuxSocketName: data.tmux_socket_name || '',
          };
          dispatch({ type: 'SET_ORIGINAL', config: originalConfig });
        }

        const authStatus = await getAuthSecretsStatus();
        if (active) {
          dispatch({
            type: 'SET_FIELD',
            field: 'authClientIdSet',
            value: !!authStatus.client_id,
          });
          dispatch({
            type: 'SET_FIELD',
            field: 'authClientSecretSet',
            value: !!authStatus.client_secret_set,
          });
          dispatch({
            type: 'SET_FIELD',
            field: 'authClientId',
            value: authStatus.client_id || '',
          });
          dispatch({
            type: 'SET_FIELD',
            field: 'authClientSecretWasSet',
            value: !!authStatus.client_secret_set,
          });
        }
      } catch (err) {
        if (!active) return;
        const message = err instanceof Error ? err.message : 'Failed to load config';
        dispatch({ type: 'SET_FIELD', field: 'error', value: message });
      } finally {
        if (active) dispatch({ type: 'SET_FIELD', field: 'loading', value: false });
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
      dispatch({ type: 'SET_FIELD', field: 'loadingOverlays', value: true });
      try {
        const data = await getOverlays();
        if (!active) return;
        dispatch({ type: 'SET_FIELD', field: 'overlays', value: data.overlays || [] });
      } catch (err) {
        if (!active) return;
        console.error('Failed to load overlays:', err);
      } finally {
        if (active) dispatch({ type: 'SET_FIELD', field: 'loadingOverlays', value: false });
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
          dispatch({ type: 'SET_FIELD', field: 'builtinQuickLaunch', value: data || [] });
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

  // Load styles for comm styles section
  const [configStyles, setConfigStyles] = useState<Style[]>([]);
  useEffect(() => {
    let active = true;
    const loadStyles = async () => {
      try {
        const data = await getStyles();
        if (active) setConfigStyles(data.styles || []);
      } catch {
        // Non-fatal: styles are optional
      }
    };
    loadStyles();
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

  // Validation
  const validateStep = (step: number) => {
    let error = null;
    if (step === 1) {
      if (!state.workspacePath.trim()) {
        error = 'Workspace path is required';
      } else if (state.repos.length === 0) {
        error = 'Add at least one repository';
      }
    } else if (step === 7) {
      if (
        !state.xtermQueryTimeout ||
        !state.xtermOperationTimeout ||
        state.xtermQueryTimeout <= 0 ||
        state.xtermOperationTimeout <= 0
      ) {
        error = 'xterm settings must be greater than 0';
      }
    }
    dispatch({ type: 'SET_STEP_ERROR', step, error });
    return !error;
  };

  // Save
  const saveCurrentStep = async () => {
    if (!validateStep(state.currentStep)) {
      if (state.stepErrors[state.currentStep]) {
        toastError(state.stepErrors[state.currentStep]!);
      }
      return false;
    }

    dispatch({ type: 'SET_FIELD', field: 'saving', value: true });
    dispatch({ type: 'SET_FIELD', field: 'warning', value: '' });

    try {
      const runTargets = state.commandTargets.map((t) => ({ name: t.name, command: t.command }));

      const updateRequest: ConfigUpdateRequest = {
        workspace_path: state.workspacePath,
        source_code_management: state.sourceCodeManagement,
        recycle_workspaces: state.recycleWorkspaces,
        repos: state.repos,
        run_targets: runTargets,
        quick_launch: state.quickLaunch.map((q) => ({
          ...q,
          prompt: q.prompt ?? undefined,
        })),
        external_diff_commands: state.externalDiffCommands,
        external_diff_cleanup_after_ms: Math.max(
          60000,
          Math.round(state.externalDiffCleanupMinutes * 60000)
        ),
        pastebin: state.pastebin,
        nudgenik: {
          target: state.nudgenikTarget || '',
          viewed_buffer_ms: state.viewedBuffer,
          seen_interval_ms: state.nudgenikSeenInterval,
        },
        branch_suggest: { target: state.branchSuggestTarget || '' },
        conflict_resolve: { target: state.conflictResolveTarget || '' },
        pr_review: { target: state.prReviewTarget || '' },
        commit_message: { target: state.commitMessageTarget || '' },
        sessions: {
          dashboard_poll_interval_ms: state.dashboardPollInterval,
          git_status_poll_interval_ms: state.gitStatusPollInterval,
          git_clone_timeout_ms: state.gitCloneTimeout,
          git_status_timeout_ms: state.gitStatusTimeout,
        },
        xterm: {
          query_timeout_ms: state.xtermQueryTimeout,
          operation_timeout_ms: state.xtermOperationTimeout,
          use_webgl: state.xtermUseWebGL,
        },
        network: {
          bind_address: state.networkAccess ? '0.0.0.0' : '127.0.0.1',
          public_base_url: state.authPublicBaseURL,
          tls: {
            cert_path: state.authTlsCertPath,
            key_path: state.authTlsKeyPath,
          },
        },
        access_control: {
          enabled: state.authEnabled,
          provider: state.authProvider,
          session_ttl_minutes: state.authSessionTTLMinutes,
        },
        notifications: {
          sound_disabled: state.soundDisabled,
          confirm_before_close: state.confirmBeforeClose,
          suggest_dispose_after_push: state.suggestDisposeAfterPush,
        },
        lore: {
          enabled: state.loreEnabled,
          llm_target: state.loreLLMTarget,
          curate_on_dispose: state.loreCurateOnDispose,
          auto_pr: state.loreAutoPR,
          public_rule_mode: state.lorePublicRuleMode,
        },
        subreddit: {
          target: state.subredditTarget,
          interval: state.subredditInterval,
          checking_range: state.subredditCheckingRange,
          max_posts: state.subredditMaxPosts,
          max_age: state.subredditMaxAge,
          repos: state.subredditRepos,
        },
        repofeed: {
          enabled: state.repofeedEnabled,
          publish_interval_seconds: state.repofeedPublishInterval,
          fetch_interval_seconds: state.repofeedFetchInterval,
          completed_retention_hours: state.repofeedCompletedRetention,
          repos: state.repofeedRepos,
        },
        enabled_models: state.enabledModels,
        comm_styles: Object.keys(state.commStyles).length > 0 ? state.commStyles : undefined,
        remote_access: {
          enabled: state.remoteAccessEnabled,
          timeout_minutes: state.remoteAccessTimeoutMinutes,
          notify: {
            ntfy_topic: state.remoteAccessNtfyTopic,
            command: state.remoteAccessNotifyCommand,
          },
        },
        desync: {
          enabled: state.desyncEnabled,
          target: state.desyncTarget || '',
        },
        floor_manager: {
          enabled: state.fmEnabled,
          target: state.fmTarget || '',
          rotation_threshold: state.fmRotationThreshold,
          debounce_ms: state.fmDebounceMs,
        },
        timelapse: {
          enabled: state.timelapseEnabled,
          retention_days: state.timelapseRetentionDays,
          max_file_size_mb: state.timelapseMaxFileSizeMB,
          max_total_storage_mb: state.timelapseMaxTotalStorageMB,
        },
        io_workspace_telemetry: {
          enabled: state.ioWorkspaceTelemetryEnabled,
          target: state.ioWorkspaceTelemetryTarget || '',
        },
        sapling_commands:
          state.saplingCmdCreateWorkspace ||
          state.saplingCmdRemoveWorkspace ||
          state.saplingCmdCheckRepoBase ||
          state.saplingCmdCreateRepoBase
            ? {
                create_workspace: state.saplingCmdCreateWorkspace || undefined,
                remove_workspace: state.saplingCmdRemoveWorkspace || undefined,
                check_repo_base: state.saplingCmdCheckRepoBase || undefined,
                create_repo_base: state.saplingCmdCreateRepoBase || undefined,
              }
            : undefined,
        tmux_binary:
          state.tmuxBinary !== state.originalConfig?.tmuxBinary ? state.tmuxBinary : undefined,
        tmux_socket_name:
          state.tmuxSocketName !== state.originalConfig?.tmuxSocketName
            ? state.tmuxSocketName
            : undefined,
        local_echo_remote: state.localEchoRemote,
        debug_ui: state.debugUI,
      };

      const result = await updateConfig(updateRequest);

      // Save staged GitHub OAuth credentials if changed
      if (state.authSecretsChanged && state.authClientId.trim()) {
        const secretPayload: { client_id: string; client_secret?: string } = {
          client_id: state.authClientId.trim(),
        };
        // Only include secret if user entered a new one
        if (state.authClientSecret.trim()) {
          secretPayload.client_secret = state.authClientSecret.trim();
        }
        await saveAuthSecrets(secretPayload);
        dispatch({ type: 'SET_FIELD', field: 'authSecretsChanged', value: false });
        dispatch({ type: 'SET_FIELD', field: 'authClientSecret', value: '' }); // Clear staged secret
        // Reload auth status after saving
        const authStatus = await getAuthSecretsStatus();
        dispatch({ type: 'SET_FIELD', field: 'authClientIdSet', value: !!authStatus.client_id });
        dispatch({
          type: 'SET_FIELD',
          field: 'authClientSecretSet',
          value: !!authStatus.client_secret_set,
        });
        dispatch({
          type: 'SET_FIELD',
          field: 'authClientSecretWasSet',
          value: !!authStatus.client_secret_set,
        });
      }

      reloadConfig();
      localStorage.setItem(CONFIG_UPDATED_KEY, Date.now().toString());
      dispatch({ type: 'SET_FIELD', field: 'authWarnings', value: result.warnings || [] });

      const reloaded = await getConfig();
      dispatch({
        type: 'SET_FIELD',
        field: 'apiNeedsRestart',
        value: reloaded.needs_restart || false,
      });

      if (!isFirstRun) {
        dispatch({ type: 'SET_ORIGINAL', config: snapshotConfig() });
      }

      if (result.warning && !isFirstRun) {
        dispatch({ type: 'SET_FIELD', field: 'warning', value: result.warning });
      } else if (!isFirstRun) {
        success('Configuration saved');
      }
      return true;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to save config';
      alert('Save Failed', message);
      return false;
    } finally {
      dispatch({ type: 'SET_FIELD', field: 'saving', value: false });
    }
  };

  const nextStep = async () => {
    if (!validateStep(state.currentStep)) {
      if (state.stepErrors[state.currentStep]) {
        toastError(state.stepErrors[state.currentStep]!);
      }
      return;
    }
    const saved = await saveCurrentStep();
    if (saved && state.currentStep < TOTAL_STEPS) {
      dispatch({ type: 'SET_FIELD', field: 'currentStep', value: state.currentStep + 1 });
    }
  };

  const prevStep = () => {
    dispatch({
      type: 'SET_FIELD',
      field: 'currentStep',
      value: Math.max(1, state.currentStep - 1),
    });
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
      if (newPath.trim()) {
        dispatch({ type: 'SET_STEP_ERROR', step: 1, error: null });
      }
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

  const addQuickLaunch = () => {
    if (state.newQuickLaunchMode === 'command') {
      const command = state.newQuickLaunchCommand.trim();
      if (!command) {
        toastError('Command is required');
        return;
      }
      const name = state.newQuickLaunchName.trim() || command;
      if (state.quickLaunch.some((q) => q.name === name)) {
        toastError('Quick launch name already exists');
        return;
      }
      dispatch({
        type: 'ADD_QUICK_LAUNCH',
        item: { name, command },
      });
      dispatch({ type: 'RESET_NEW_QUICK_LAUNCH' });
      return;
    }

    const targetName = state.newQuickLaunchTarget.trim();
    if (!targetName) {
      toastError('Quick launch target is required');
      return;
    }
    const name = state.newQuickLaunchName.trim() || targetName;
    if (state.quickLaunch.some((q) => q.name === name)) {
      toastError('Quick launch name already exists');
      return;
    }
    const promptValue = state.newQuickLaunchPrompt.trim();
    if (promptValue === '') {
      toastError('Prompt is required for agent targets');
      return;
    }
    dispatch({
      type: 'ADD_QUICK_LAUNCH',
      item: {
        name,
        target: targetName,
        prompt: promptValue,
        persona_id: state.newQuickLaunchPersonaId || undefined,
      },
    });
    dispatch({ type: 'RESET_NEW_QUICK_LAUNCH' });
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

  const openQuickLaunchEditModal = (item: import('../lib/types').QuickLaunchPreset) => {
    const isCommandTarget = commandTargetNames.has(item.target || '');
    let initialPrompt = item.prompt || '';
    if (isCommandTarget) {
      const commandTarget = state.commandTargets.find((t) => t.name === item.target);
      if (commandTarget) {
        initialPrompt = commandTarget.command;
      }
    }
    dispatch({
      type: 'SET_QUICK_LAUNCH_EDIT_MODAL',
      modal: { item, prompt: initialPrompt, isCommandTarget, error: '' },
    });
  };

  const saveQuickLaunchEditModal = () => {
    if (!state.quickLaunchEditModal) return;
    const { item, prompt: modalPrompt, isCommandTarget } = state.quickLaunchEditModal;
    const target = item.target || '';

    const isPromptable = modelTargetNames.has(target);
    if (isPromptable && !modalPrompt.trim()) {
      dispatch({
        type: 'SET_QUICK_LAUNCH_EDIT_MODAL',
        modal: {
          ...state.quickLaunchEditModal,
          error: 'Prompt is required for promptable targets',
        },
      });
      return;
    }

    if (isCommandTarget) {
      if (!modalPrompt.trim()) {
        dispatch({
          type: 'SET_QUICK_LAUNCH_EDIT_MODAL',
          modal: {
            ...state.quickLaunchEditModal,
            error: 'Command is required for command targets',
          },
        });
        return;
      }
      dispatch({ type: 'UPDATE_COMMAND_TARGET', name: target, command: modalPrompt });
    }

    dispatch({
      type: 'UPDATE_QUICK_LAUNCH',
      name: item.name,
      updates: { name: item.name, target, prompt: isPromptable ? modalPrompt : null },
    });
    dispatch({ type: 'SET_QUICK_LAUNCH_EDIT_MODAL', modal: null });
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

  const saveAuthSecretsModal = () => {
    if (!state.authSecretsModal) return;
    const { clientId, clientSecret, clientSecretWasSet } = state.authSecretsModal;

    // Validate client ID is required
    if (!clientId.trim()) {
      dispatch({
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
      dispatch({
        type: 'SET_AUTH_SECRETS_MODAL',
        modal: {
          ...state.authSecretsModal,
          error: 'Client secret is required for initial setup',
        },
      });
      return;
    }

    // Stage credentials in form state (save with "Save Changes")
    // Only store new secret if user entered one (not the mask)
    const newSecret = clientSecret.trim() && clientSecret !== '••••••••' ? clientSecret.trim() : '';
    dispatch({ type: 'SET_FIELD', field: 'authClientId', value: clientId.trim() });
    dispatch({ type: 'SET_FIELD', field: 'authClientSecret', value: newSecret });
    dispatch({ type: 'SET_FIELD', field: 'authClientSecretWasSet', value: clientSecretWasSet });
    dispatch({ type: 'SET_FIELD', field: 'authSecretsChanged', value: true });
    dispatch({ type: 'SET_FIELD', field: 'authClientIdSet', value: !!clientId.trim() });
    dispatch({
      type: 'SET_FIELD',
      field: 'authClientSecretSet',
      value: clientSecretWasSet || !!newSecret,
    });
    dispatch({ type: 'SET_AUTH_SECRETS_MODAL', modal: null });
    success('Credentials staged - click Save Changes to apply');
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
    dispatch({ type: 'SET_FIELD', field: 'currentStep', value: step });
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
                disabled={state.saving || !hasChanges(isFirstRun)}
                data-testid="config-save"
              >
                {state.saving ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </div>
          <div className="wizard__steps wizard__steps--compact">
            {Array.from({ length: TOTAL_STEPS }, (_, i) => i + 1).map((stepNum) => {
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
      )}

      {/* Non-sticky header for first-run wizard */}
      {isFirstRun && (
        <>
          <div className="page-header">
            <h1 className="page-header__title">Setup schmux</h1>
          </div>

          <div className="banner banner--info mb-lg">
            <p className="m-0">
              <strong>Welcome to schmux!</strong> Complete these steps to start spawning sessions.
            </p>
          </div>
        </>
      )}

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

      {/* Steps navigation for first-run wizard only */}
      {isFirstRun && (
        <div className="wizard__steps">
          {Array.from({ length: TOTAL_STEPS }, (_, i) => i + 1).map((stepNum) => {
            const isCompleted = isFirstRun && stepNum < state.currentStep;
            const isCurrent = stepNum === state.currentStep;
            const stepLabel = TABS[stepNum - 1];
            if (isTabHidden(TAB_SLUGS[stepNum - 1])) return null;

            return (
              <div
                key={stepNum}
                className={`wizard__step cursor-pointer ${isCurrent ? 'wizard__step--active' : ''} ${isCompleted ? 'wizard__step--completed' : ''}`}
                data-step={stepNum}
                aria-selected={isCurrent}
                onClick={() => setCurrentStep(stepNum)}
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
            <WorkspacesTab
              workspacePath={state.workspacePath}
              recycleWorkspaces={state.recycleWorkspaces}
              repos={state.repos}
              overlays={state.overlays}
              newRepoName={state.newRepoName}
              newRepoUrl={state.newRepoUrl}
              newRepoVcs={state.newRepoVcs}
              stepErrors={state.stepErrors}
              dispatch={dispatch}
              onEditWorkspacePath={handleEditWorkspacePath}
              onRemoveRepo={removeRepo}
              onAddRepo={addRepo}
            />
          )}

          {currentTab === 2 && (
            <SessionsTab
              models={state.modelCatalog}
              runners={state.runners}
              enabledModels={state.enabledModels}
              commStyles={state.commStyles}
              styles={configStyles}
              commandTargets={state.commandTargets}
              newCommandName={state.newCommandName}
              newCommandCommand={state.newCommandCommand}
              dispatch={dispatch}
              onAddCommand={addCommand}
              onRemoveCommand={removeCommand}
              onModelAction={handleModelAction}
              onOpenRunTargetEditModal={openRunTargetEditModal}
            />
          )}

          {currentTab === 3 && (
            <QuickLaunchTab
              quickLaunch={state.quickLaunch}
              builtinQuickLaunch={state.builtinQuickLaunch}
              models={models}
              personas={personas}
              newQuickLaunchName={state.newQuickLaunchName}
              newQuickLaunchMode={state.newQuickLaunchMode}
              newQuickLaunchTarget={state.newQuickLaunchTarget}
              newQuickLaunchPrompt={state.newQuickLaunchPrompt}
              newQuickLaunchCommand={state.newQuickLaunchCommand}
              newQuickLaunchPersonaId={state.newQuickLaunchPersonaId}
              selectedCookbookTemplate={state.selectedCookbookTemplate}
              dispatch={dispatch}
              onAddQuickLaunch={addQuickLaunch}
              onRemoveQuickLaunch={removeQuickLaunch}
              onOpenQuickLaunchEditModal={openQuickLaunchEditModal}
            />
          )}

          {currentTab === 4 && (
            <PastebinTab
              pastebin={state.pastebin}
              dispatch={dispatch}
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
            />
          )}

          {currentTab === 5 && (
            <CodeReviewTab
              commitMessageTarget={state.commitMessageTarget}
              prReviewTarget={state.prReviewTarget}
              externalDiffCommands={state.externalDiffCommands}
              externalDiffCleanupMinutes={state.externalDiffCleanupMinutes}
              newDiffName={state.newDiffName}
              newDiffCommand={state.newDiffCommand}
              commitMessageTargetMissing={commitMessageTargetMissing}
              prReviewTargetMissing={prReviewTargetMissing}
              models={oneshotModels}
              dispatch={dispatch}
              onAddDiffCommand={addDiffCommand}
            />
          )}

          {currentTab === 6 && (
            <FloorManagerTab
              fmEnabled={state.fmEnabled}
              fmTarget={state.fmTarget}
              fmRotationThreshold={state.fmRotationThreshold}
              fmDebounceMs={state.fmDebounceMs}
              models={oneshotModels}
              dispatch={dispatch}
            />
          )}

          {currentTab === 7 && (
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

          {features.subreddit && currentTab === 8 && (
            <SubredditTab
              subredditTarget={state.subredditTarget}
              subredditInterval={state.subredditInterval}
              subredditCheckingRange={state.subredditCheckingRange}
              subredditMaxPosts={state.subredditMaxPosts}
              subredditMaxAge={state.subredditMaxAge}
              subredditRepos={state.subredditRepos}
              repos={state.repos}
              models={oneshotModels}
              dispatch={dispatch}
            />
          )}

          {features.repofeed && currentTab === 9 && (
            <RepofeedTab
              repofeedEnabled={state.repofeedEnabled}
              repofeedPublishInterval={state.repofeedPublishInterval}
              repofeedFetchInterval={state.repofeedFetchInterval}
              repofeedCompletedRetention={state.repofeedCompletedRetention}
              repofeedRepos={state.repofeedRepos}
              repos={state.repos}
              dispatch={dispatch}
            />
          )}

          {currentTab === 10 && (
            <AdvancedTab
              loreEnabled={state.loreEnabled}
              loreLLMTarget={state.loreLLMTarget}
              loreCurateOnDispose={state.loreCurateOnDispose}
              loreAutoPR={state.loreAutoPR}
              lorePublicRuleMode={state.lorePublicRuleMode}
              nudgenikTarget={state.nudgenikTarget}
              viewedBuffer={state.viewedBuffer}
              nudgenikSeenInterval={state.nudgenikSeenInterval}
              desyncEnabled={state.desyncEnabled}
              desyncTarget={state.desyncTarget}
              ioWorkspaceTelemetryEnabled={state.ioWorkspaceTelemetryEnabled}
              ioWorkspaceTelemetryTarget={state.ioWorkspaceTelemetryTarget}
              branchSuggestTarget={state.branchSuggestTarget}
              conflictResolveTarget={state.conflictResolveTarget}
              soundDisabled={state.soundDisabled}
              confirmBeforeClose={state.confirmBeforeClose}
              suggestDisposeAfterPush={state.suggestDisposeAfterPush}
              dashboardPollInterval={state.dashboardPollInterval}
              gitStatusPollInterval={state.gitStatusPollInterval}
              gitCloneTimeout={state.gitCloneTimeout}
              gitStatusTimeout={state.gitStatusTimeout}
              xtermQueryTimeout={state.xtermQueryTimeout}
              xtermOperationTimeout={state.xtermOperationTimeout}
              xtermUseWebGL={state.xtermUseWebGL}
              localEchoRemote={state.localEchoRemote}
              debugUI={state.debugUI}
              nudgenikTargetMissing={nudgenikTargetMissing}
              branchSuggestTargetMissing={branchSuggestTargetMissing}
              conflictResolveTargetMissing={conflictResolveTargetMissing}
              hasSaplingRepos={state.repos.some((r) => r.vcs === 'sapling')}
              saplingCmdCreateWorkspace={state.saplingCmdCreateWorkspace}
              saplingCmdRemoveWorkspace={state.saplingCmdRemoveWorkspace}
              saplingCmdCheckRepoBase={state.saplingCmdCheckRepoBase}
              saplingCmdCreateRepoBase={state.saplingCmdCreateRepoBase}
              tmuxBinary={state.tmuxBinary}
              tmuxSocketName={state.tmuxSocketName}
              timelapseEnabled={state.timelapseEnabled}
              timelapseRetentionDays={state.timelapseRetentionDays}
              timelapseMaxFileSizeMB={state.timelapseMaxFileSizeMB}
              timelapseMaxTotalStorageMB={state.timelapseMaxTotalStorageMB}
              stepErrors={state.stepErrors}
              models={oneshotModels}
              dispatch={dispatch}
            />
          )}
        </div>

        {/* Wizard footer navigation - only shown in first-run mode */}
        {isFirstRun && (
          <div className="wizard__actions">
            <div className="wizard__actions-left">
              {state.currentStep > 1 && (
                <button className="btn" onClick={prevStep} disabled={state.saving}>
                  ← Back
                </button>
              )}
            </div>
            <div className="wizard__actions-right">
              <button
                className="btn btn--primary"
                onClick={async () => {
                  if (state.currentStep < TOTAL_STEPS) {
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
                disabled={state.saving}
              >
                {state.saving
                  ? 'Saving...'
                  : state.currentStep === TOTAL_STEPS
                    ? 'Finish Setup'
                    : 'Next →'}
              </button>
            </div>
          </div>
        )}
      </div>

      <ConfigModals
        authSecretsModal={state.authSecretsModal}
        runTargetEditModal={state.runTargetEditModal}
        quickLaunchEditModal={state.quickLaunchEditModal}
        pastebinEditModal={state.pastebinEditModal}
        tlsModal={state.tlsModal}
        dispatch={dispatch}
        onSaveAuthSecrets={saveAuthSecretsModal}
        onSaveRunTargetEdit={saveRunTargetEditModal}
        onSaveQuickLaunchEdit={saveQuickLaunchEditModal}
        onSavePastebinEdit={savePastebinEditModal}
        onSaveTls={handleTlsModalSave}
        onValidateTls={handleTlsValidate}
        authPublicBaseURL={state.authPublicBaseURL}
      />
    </>
  );
}
