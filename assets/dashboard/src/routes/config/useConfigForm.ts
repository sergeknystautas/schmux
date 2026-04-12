import { useReducer, useCallback, useMemo } from 'react';
import type {
  BuiltinQuickLaunchCookbook,
  Model,
  OverlayInfo,
  QuickLaunchPreset,
  RepoResponse,
  RunnerInfo,
  RunTargetResponse,
} from '../../lib/types';

export type RunTargetEditModalState = {
  target: RunTargetResponse;
  command: string;
  error: string;
} | null;

export type PastebinEditModalState = {
  index?: number;
  content: string;
  error: string;
} | null;

export type QuickLaunchDialogModalState = {
  mode: 'add' | 'edit';
  kind: 'agent' | 'command';
  name: string;
  originalName?: string;
  target?: string;
  personaId?: string;
  prompt?: string;
  command?: string;
  error: string;
} | null;

export type AuthSecretsModalState = {
  clientId: string;
  clientSecret: string;
  clientSecretWasSet: boolean;
  error: string;
} | null;

export type TlsModalState = {
  certPath: string;
  keyPath: string;
  hostname: string;
  expires: string;
  validating: boolean;
  error: string;
} | null;

export type ConfigFormState = {
  // Core config fields
  workspacePath: string;
  sourceCodeManagement: string;
  repos: RepoResponse[];
  commandTargets: RunTargetResponse[];
  quickLaunch: QuickLaunchPreset[];
  builtinQuickLaunch: BuiltinQuickLaunchCookbook[];
  externalDiffCommands: { name: string; command: string }[];
  externalDiffCleanupMinutes: number;
  pastebin: string[];
  modelCatalog: Model[];
  runners: Record<string, RunnerInfo>;
  nudgenikTarget: string;
  branchSuggestTarget: string;
  conflictResolveTarget: string;
  prReviewTarget: string;
  commitMessageTarget: string;

  // New item inputs
  newRepoName: string;
  newRepoUrl: string;
  newRepoVcs: string;
  newCommandName: string;
  newCommandCommand: string;
  newDiffName: string;
  newDiffCommand: string;

  // Advanced settings
  dashboardPollInterval: number;
  viewedBuffer: number;
  nudgenikSeenInterval: number;
  gitStatusPollInterval: number;
  gitCloneTimeout: number;
  gitStatusTimeout: number;
  xtermQueryTimeout: number;
  xtermOperationTimeout: number;
  xtermUseWebGL: boolean;

  networkAccess: boolean;
  authEnabled: boolean;
  authProvider: string;
  authPublicBaseURL: string;
  authSessionTTLMinutes: number;
  authTlsCertPath: string;
  authTlsKeyPath: string;
  authClientIdSet: boolean;
  authClientSecretSet: boolean;
  authClientId: string;
  authClientSecret: string;
  authClientSecretWasSet: boolean;
  authSecretsChanged: boolean;
  authWarnings: string[];
  apiNeedsRestart: boolean;
  recycleWorkspaces: boolean;
  soundDisabled: boolean;
  confirmBeforeClose: boolean;
  suggestDisposeAfterPush: boolean;
  enabledModels: Record<string, string>;
  commStyles: Record<string, string>;

  // Lore
  loreEnabled: boolean;
  loreLLMTarget: string;
  loreCurateOnDispose: string;
  loreAutoPR: boolean;
  lorePublicRuleMode: string;

  // Subreddit
  subredditEnabled: boolean;
  subredditTarget: string;
  subredditInterval: number;
  subredditCheckingRange: number;
  subredditMaxPosts: number;
  subredditMaxAge: number;
  subredditRepos: Record<string, boolean>;

  // Repofeed
  repofeedEnabled: boolean;
  repofeedPublishInterval: number;
  repofeedFetchInterval: number;
  repofeedCompletedRetention: number;
  repofeedRepos: Record<string, boolean>;

  // Remote access
  remoteAccessEnabled: boolean;
  remoteAccessTimeoutMinutes: number;
  remoteAccessNtfyTopic: string;
  remoteAccessNotifyCommand: string;
  remoteAccessPasswordHashSet: boolean;
  passwordInput: string;
  passwordConfirm: string;
  passwordSaving: boolean;
  passwordError: string;
  passwordSuccess: string;

  // Desync
  desyncEnabled: boolean;
  desyncTarget: string;

  // Floor Manager
  fmEnabled: boolean;
  fmTarget: string;
  fmRotationThreshold: number;
  fmDebounceMs: number;

  // Timelapse
  timelapseEnabled: boolean;
  timelapseRetentionDays: number;
  timelapseMaxFileSizeMB: number;
  timelapseMaxTotalStorageMB: number;

  ioWorkspaceTelemetryEnabled: boolean;
  ioWorkspaceTelemetryTarget: string;

  saplingCmdCreateWorkspace: string;
  saplingCmdRemoveWorkspace: string;
  saplingCmdCheckRepoBase: string;
  saplingCmdCreateRepoBase: string;

  personasEnabled: boolean;
  commStylesEnabled: boolean;
  localEchoRemote: boolean;
  debugUI: boolean;
  tmuxBinary: string;
  tmuxSocketName: string;

  // Overlays
  overlays: OverlayInfo[];
  loadingOverlays: boolean;

  // Modal state
  runTargetEditModal: RunTargetEditModalState;
  quickLaunchDialogModal: QuickLaunchDialogModalState;
  pastebinEditModal: PastebinEditModalState;
  authSecretsModal: AuthSecretsModalState;
  tlsModal: TlsModalState;

  // Loading state
  loading: boolean;
  saving: boolean;
  error: string;
  warning: string;

  // Wizard
  currentStep: number;
};

export type ConfigFormAction =
  | { type: 'SET_FIELD'; field: keyof ConfigFormState; value: unknown }
  | { type: 'LOAD_CONFIG'; state: Partial<ConfigFormState> }
  | { type: 'ADD_REPO'; repo: RepoResponse }
  | { type: 'REMOVE_REPO'; name: string }
  | { type: 'ADD_COMMAND_TARGET'; target: RunTargetResponse }
  | { type: 'REMOVE_COMMAND_TARGET'; name: string }
  | { type: 'UPDATE_COMMAND_TARGET'; name: string; command: string }
  | { type: 'ADD_QUICK_LAUNCH'; item: QuickLaunchPreset }
  | { type: 'REMOVE_QUICK_LAUNCH'; name: string }
  | { type: 'UPDATE_QUICK_LAUNCH'; name: string; updates: Partial<QuickLaunchPreset> }
  | { type: 'ADD_DIFF_COMMAND'; command: { name: string; command: string } }
  | { type: 'REMOVE_DIFF_COMMAND'; name: string }
  | { type: 'ADD_PASTEBIN'; content: string }
  | { type: 'REMOVE_PASTEBIN'; index: number }
  | { type: 'UPDATE_PASTEBIN'; index: number; content: string }
  | { type: 'SET_MODELS'; models: Model[] }
  | { type: 'RESET_NEW_REPO' }
  | { type: 'RESET_NEW_COMMAND' }
  | { type: 'RESET_NEW_DIFF' }
  | { type: 'SET_RUN_TARGET_EDIT_MODAL'; modal: RunTargetEditModalState }
  | { type: 'SET_QUICK_LAUNCH_DIALOG_MODAL'; modal: QuickLaunchDialogModalState }
  | { type: 'SET_PASTEBIN_EDIT_MODAL'; modal: PastebinEditModalState }
  | { type: 'UPDATE_PASTEBIN'; index: number; content: string }
  | { type: 'SET_AUTH_SECRETS_MODAL'; modal: AuthSecretsModalState }
  | { type: 'SET_TLS_MODAL'; modal: TlsModalState }
  | { type: 'TOGGLE_MODEL'; modelId: string; enabled: boolean; defaultRunner: string }
  | { type: 'CHANGE_RUNNER'; modelId: string; runner: string };

export const initialState: ConfigFormState = {
  workspacePath: '',
  sourceCodeManagement: 'git-worktree',
  repos: [],
  commandTargets: [],
  quickLaunch: [],
  builtinQuickLaunch: [],
  externalDiffCommands: [],
  externalDiffCleanupMinutes: 60,
  modelCatalog: [],
  runners: {},
  nudgenikTarget: '',
  branchSuggestTarget: '',
  conflictResolveTarget: '',
  prReviewTarget: '',
  commitMessageTarget: '',

  newRepoName: '',
  newRepoUrl: '',
  newRepoVcs: '',
  newCommandName: '',
  newCommandCommand: '',
  newDiffName: '',
  newDiffCommand: '',
  pastebin: [],

  dashboardPollInterval: 5000,
  viewedBuffer: 5000,
  nudgenikSeenInterval: 2000,
  gitStatusPollInterval: 10000,
  gitCloneTimeout: 300000,
  gitStatusTimeout: 30000,
  xtermQueryTimeout: 5000,
  xtermOperationTimeout: 10000,
  xtermUseWebGL: true,
  networkAccess: false,
  authEnabled: false,
  authProvider: 'github',
  authPublicBaseURL: '',
  authSessionTTLMinutes: 1440,
  authTlsCertPath: '',
  authTlsKeyPath: '',
  authClientIdSet: false,
  authClientSecretSet: false,
  authClientId: '',
  authClientSecret: '',
  authClientSecretWasSet: false,
  authSecretsChanged: false,
  authWarnings: [],
  apiNeedsRestart: false,
  recycleWorkspaces: false,
  soundDisabled: false,
  confirmBeforeClose: false,
  suggestDisposeAfterPush: true,
  enabledModels: {},
  commStyles: {},

  loreEnabled: true,
  loreLLMTarget: '',
  loreCurateOnDispose: 'session',
  loreAutoPR: false,
  lorePublicRuleMode: 'direct_push',

  subredditEnabled: false,
  subredditTarget: '',
  subredditInterval: 30,
  subredditCheckingRange: 48,
  subredditMaxPosts: 30,
  subredditMaxAge: 14,
  subredditRepos: {},

  repofeedEnabled: false,
  repofeedPublishInterval: 30,
  repofeedFetchInterval: 60,
  repofeedCompletedRetention: 48,
  repofeedRepos: {},

  remoteAccessEnabled: false,
  remoteAccessTimeoutMinutes: 0,
  remoteAccessNtfyTopic: '',
  remoteAccessNotifyCommand: '',
  remoteAccessPasswordHashSet: false,
  passwordInput: '',
  passwordConfirm: '',
  passwordSaving: false,
  passwordError: '',
  passwordSuccess: '',

  desyncEnabled: false,
  desyncTarget: '',

  fmEnabled: false,
  fmTarget: '',
  fmRotationThreshold: 150,
  fmDebounceMs: 2000,
  timelapseEnabled: true,
  timelapseRetentionDays: 7,
  timelapseMaxFileSizeMB: 50,
  timelapseMaxTotalStorageMB: 500,
  ioWorkspaceTelemetryEnabled: false,
  ioWorkspaceTelemetryTarget: '',

  saplingCmdCreateWorkspace: '',
  saplingCmdRemoveWorkspace: '',
  saplingCmdCheckRepoBase: '',
  saplingCmdCreateRepoBase: '',

  personasEnabled: false,
  commStylesEnabled: false,
  localEchoRemote: false,
  debugUI: false,
  tmuxBinary: '',
  tmuxSocketName: '',

  overlays: [],
  loadingOverlays: true,

  runTargetEditModal: null,
  quickLaunchDialogModal: null,
  pastebinEditModal: null,
  authSecretsModal: null,
  tlsModal: null,

  loading: true,
  saving: false,
  error: '',
  warning: '',

  currentStep: 1,
};

function configFormReducer(state: ConfigFormState, action: ConfigFormAction): ConfigFormState {
  switch (action.type) {
    case 'SET_FIELD':
      return { ...state, [action.field]: action.value };

    case 'LOAD_CONFIG':
      return { ...state, ...action.state };

    case 'ADD_REPO':
      return { ...state, repos: [...state.repos, action.repo] };

    case 'REMOVE_REPO':
      return { ...state, repos: state.repos.filter((r) => r.name !== action.name) };

    case 'ADD_COMMAND_TARGET':
      return { ...state, commandTargets: [...state.commandTargets, action.target] };

    case 'REMOVE_COMMAND_TARGET':
      return {
        ...state,
        commandTargets: state.commandTargets.filter((t) => t.name !== action.name),
      };

    case 'UPDATE_COMMAND_TARGET':
      return {
        ...state,
        commandTargets: state.commandTargets.map((t) =>
          t.name === action.name ? { ...t, command: action.command } : t
        ),
      };

    case 'ADD_QUICK_LAUNCH':
      return {
        ...state,
        quickLaunch: [...state.quickLaunch, action.item].sort((a, b) =>
          a.name.localeCompare(b.name)
        ),
      };

    case 'REMOVE_QUICK_LAUNCH':
      return {
        ...state,
        quickLaunch: state.quickLaunch.filter((q) => q.name !== action.name),
      };

    case 'UPDATE_QUICK_LAUNCH':
      return {
        ...state,
        quickLaunch: state.quickLaunch
          .map((q) => (q.name === action.name ? { ...q, ...action.updates } : q))
          .sort((a, b) => a.name.localeCompare(b.name)),
      };

    case 'ADD_DIFF_COMMAND':
      return {
        ...state,
        externalDiffCommands: [...state.externalDiffCommands, action.command],
      };

    case 'REMOVE_DIFF_COMMAND':
      return {
        ...state,
        externalDiffCommands: state.externalDiffCommands.filter((c) => c.name !== action.name),
      };

    case 'ADD_PASTEBIN': {
      const updated = [...state.pastebin, action.content].sort((a, b) => a.localeCompare(b));
      return { ...state, pastebin: updated, pastebinEditModal: null };
    }

    case 'REMOVE_PASTEBIN':
      return {
        ...state,
        pastebin: state.pastebin.filter((_, i) => i !== action.index),
      };

    case 'SET_PASTEBIN_EDIT_MODAL':
      return { ...state, pastebinEditModal: action.modal };

    case 'UPDATE_PASTEBIN': {
      const updated = [...state.pastebin];
      updated[action.index] = action.content;
      return {
        ...state,
        pastebin: updated.sort((a, b) => a.localeCompare(b)),
        pastebinEditModal: null,
      };
    }

    case 'SET_MODELS':
      return { ...state, modelCatalog: action.models };

    case 'RESET_NEW_REPO':
      return { ...state, newRepoName: '', newRepoUrl: '', newRepoVcs: '' };

    case 'RESET_NEW_COMMAND':
      return { ...state, newCommandName: '', newCommandCommand: '' };

    case 'RESET_NEW_DIFF':
      return { ...state, newDiffName: '', newDiffCommand: '' };

    case 'SET_RUN_TARGET_EDIT_MODAL':
      return { ...state, runTargetEditModal: action.modal };

    case 'SET_QUICK_LAUNCH_DIALOG_MODAL':
      return { ...state, quickLaunchDialogModal: action.modal };

    case 'SET_AUTH_SECRETS_MODAL':
      return { ...state, authSecretsModal: action.modal };

    case 'SET_TLS_MODAL':
      return { ...state, tlsModal: action.modal };

    case 'TOGGLE_MODEL': {
      const next = { ...state.enabledModels };
      if (action.enabled) {
        next[action.modelId] = action.defaultRunner;
      } else {
        delete next[action.modelId];
      }
      return { ...state, enabledModels: next };
    }

    case 'CHANGE_RUNNER': {
      if (!(action.modelId in state.enabledModels)) return state;
      return {
        ...state,
        enabledModels: { ...state.enabledModels, [action.modelId]: action.runner },
      };
    }

    default:
      return state;
  }
}

export function useConfigForm(initialStep: number = 1) {
  const [state, dispatch] = useReducer(configFormReducer, {
    ...initialState,
    currentStep: initialStep,
  });

  const modelTargetNames = new Set(
    state.modelCatalog.filter((model) => model.configured).map((model) => model.id)
  );

  const models = useMemo(() => {
    const enabled = state.enabledModels;
    const hasExplicit = Object.keys(enabled).length > 0;
    return state.modelCatalog.filter((m) => (hasExplicit ? m.id in enabled : m.configured));
  }, [state.modelCatalog, state.enabledModels]);

  const oneshotModels = useMemo(() => {
    return models.filter((m) => {
      const preferredRunner = state.enabledModels[m.id];
      if (preferredRunner) {
        const runner = state.runners[preferredRunner];
        return runner?.capabilities?.includes('oneshot') ?? false;
      }
      // No explicit preferred runner — check if any of the model's runners support oneshot
      return m.runners.some((name) => {
        const runner = state.runners[name];
        return runner?.capabilities?.includes('oneshot') ?? false;
      });
    });
  }, [models, state.enabledModels, state.runners]);

  const commandTargetNames = new Set(state.commandTargets.map((target) => target.name));

  const nudgenikTargetMissing =
    state.nudgenikTarget.trim() !== '' && !modelTargetNames.has(state.nudgenikTarget.trim());
  const branchSuggestTargetMissing =
    state.branchSuggestTarget.trim() !== '' &&
    !modelTargetNames.has(state.branchSuggestTarget.trim());
  const conflictResolveTargetMissing =
    state.conflictResolveTarget.trim() !== '' &&
    !modelTargetNames.has(state.conflictResolveTarget.trim());
  const prReviewTargetMissing =
    state.prReviewTarget.trim() !== '' && !modelTargetNames.has(state.prReviewTarget.trim());
  const commitMessageTargetMissing =
    state.commitMessageTarget.trim() !== '' &&
    !modelTargetNames.has(state.commitMessageTarget.trim());

  const checkTargetUsage = useCallback(
    (targetName: string) => {
      const inQuickLaunch = state.quickLaunch.some((item) => item.target === targetName);
      const inNudgenik = state.nudgenikTarget === targetName;
      const inBranchSuggest = state.branchSuggestTarget === targetName;
      const inConflictResolve = state.conflictResolveTarget === targetName;
      const inPrReview = state.prReviewTarget === targetName;
      const inCommitMessage = state.commitMessageTarget === targetName;
      return {
        inQuickLaunch,
        inNudgenik,
        inBranchSuggest,
        inConflictResolve,
        inPrReview,
        inCommitMessage,
      };
    },
    [state]
  );

  return {
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
    checkTargetUsage,
  };
}
