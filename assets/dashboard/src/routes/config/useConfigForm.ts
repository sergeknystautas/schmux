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

export type ConfigSnapshot = {
  workspacePath: string;
  sourceCodeManagement: string;
  repos: RepoResponse[];
  commandTargets: RunTargetResponse[];
  quickLaunch: QuickLaunchPreset[];
  externalDiffCommands: { name: string; command: string }[];
  externalDiffCleanupMinutes: number;
  nudgenikTarget: string;
  branchSuggestTarget: string;
  conflictResolveTarget: string;
  prReviewTarget: string;
  commitMessageTarget: string;
  dashboardPollInterval: number;
  viewedBuffer: number;
  nudgenikSeenInterval: number;
  gitStatusPollInterval: number;
  gitCloneTimeout: number;
  gitStatusTimeout: number;
  xtermQueryTimeout: number;
  xtermOperationTimeout: number;
  xtermStripClearScreen: boolean;
  xtermUseWebGL: boolean;
  networkAccess: boolean;
  authEnabled: boolean;
  authProvider: string;
  authPublicBaseURL: string;
  authSessionTTLMinutes: number;
  authTlsCertPath: string;
  authTlsKeyPath: string;
  soundDisabled: boolean;
  confirmBeforeClose: boolean;
  suggestDisposeAfterPush: boolean;
  enabledModels: Record<string, string>;
  loreEnabled: boolean;
  loreLLMTarget: string;
  loreCurateOnDispose: string;
  loreAutoPR: boolean;
  subredditTarget: string;
  subredditInterval: number;
  subredditCheckingRange: number;
  subredditMaxPosts: number;
  subredditMaxAge: number;
  subredditRepos: Record<string, boolean>;
  repofeedEnabled: boolean;
  repofeedPublishInterval: number;
  repofeedFetchInterval: number;
  repofeedCompletedRetention: number;
  repofeedRepos: Record<string, boolean>;
  remoteAccessEnabled: boolean;
  remoteAccessTimeoutMinutes: number;
  remoteAccessNtfyTopic: string;
  remoteAccessNotifyCommand: string;
  desyncEnabled: boolean;
  desyncTarget: string;
  fmEnabled: boolean;
  fmTarget: string;
  fmRotationThreshold: number;
  fmDebounceMs: number;
  ioWorkspaceTelemetryEnabled: boolean;
  ioWorkspaceTelemetryTarget: string;
  saplingCmdCreateWorkspace: string;
  saplingCmdRemoveWorkspace: string;
  saplingCmdCheckRepoBase: string;
  saplingCmdCreateRepoBase: string;
  tmuxBinary: string;
};

export type RunTargetEditModalState = {
  target: RunTargetResponse;
  command: string;
  error: string;
} | null;

export type QuickLaunchEditModalState = {
  item: QuickLaunchPreset;
  prompt: string;
  isCommandTarget: boolean;
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
  newQuickLaunchName: string;
  newQuickLaunchMode: 'agent' | 'command';
  newQuickLaunchTarget: string;
  newQuickLaunchPrompt: string;
  newQuickLaunchCommand: string;
  newQuickLaunchPersonaId: string;
  selectedCookbookTemplate: BuiltinQuickLaunchCookbook | null;
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
  xtermStripClearScreen: boolean;
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
  soundDisabled: boolean;
  confirmBeforeClose: boolean;
  suggestDisposeAfterPush: boolean;
  enabledModels: Record<string, string>;

  // Lore
  loreEnabled: boolean;
  loreLLMTarget: string;
  loreCurateOnDispose: string;
  loreAutoPR: boolean;

  // Subreddit
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

  ioWorkspaceTelemetryEnabled: boolean;
  ioWorkspaceTelemetryTarget: string;

  saplingCmdCreateWorkspace: string;
  saplingCmdRemoveWorkspace: string;
  saplingCmdCheckRepoBase: string;
  saplingCmdCreateRepoBase: string;

  tmuxBinary: string;

  // Overlays
  overlays: OverlayInfo[];
  loadingOverlays: boolean;

  // Original config for change detection
  originalConfig: ConfigSnapshot | null;

  // Validation
  stepErrors: Record<number, string | null>;

  // Modal state
  runTargetEditModal: RunTargetEditModalState;
  quickLaunchEditModal: QuickLaunchEditModalState;
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
  | { type: 'SET_ORIGINAL'; config: ConfigSnapshot | null }
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
  | { type: 'SET_MODELS'; models: Model[] }
  | { type: 'SET_STEP_ERROR'; step: number; error: string | null }
  | { type: 'RESET_NEW_REPO' }
  | { type: 'RESET_NEW_COMMAND' }
  | { type: 'RESET_NEW_QUICK_LAUNCH' }
  | { type: 'RESET_NEW_DIFF' }
  | { type: 'SET_RUN_TARGET_EDIT_MODAL'; modal: RunTargetEditModalState }
  | { type: 'SET_QUICK_LAUNCH_EDIT_MODAL'; modal: QuickLaunchEditModalState }
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
  newQuickLaunchName: '',
  newQuickLaunchMode: 'agent',
  newQuickLaunchTarget: '',
  newQuickLaunchPrompt: '',
  newQuickLaunchCommand: '',
  newQuickLaunchPersonaId: '',
  selectedCookbookTemplate: null,
  newDiffName: '',
  newDiffCommand: '',

  dashboardPollInterval: 5000,
  viewedBuffer: 5000,
  nudgenikSeenInterval: 2000,
  gitStatusPollInterval: 10000,
  gitCloneTimeout: 300000,
  gitStatusTimeout: 30000,
  xtermQueryTimeout: 5000,
  xtermOperationTimeout: 10000,
  xtermStripClearScreen: true,
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
  soundDisabled: false,
  confirmBeforeClose: false,
  suggestDisposeAfterPush: true,
  enabledModels: {},

  loreEnabled: true,
  loreLLMTarget: '',
  loreCurateOnDispose: 'session',
  loreAutoPR: false,

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
  ioWorkspaceTelemetryEnabled: false,
  ioWorkspaceTelemetryTarget: '',

  saplingCmdCreateWorkspace: '',
  saplingCmdRemoveWorkspace: '',
  saplingCmdCheckRepoBase: '',
  saplingCmdCreateRepoBase: '',

  tmuxBinary: '',

  overlays: [],
  loadingOverlays: true,

  originalConfig: null,

  stepErrors: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null, 7: null },

  runTargetEditModal: null,
  quickLaunchEditModal: null,
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

    case 'SET_ORIGINAL':
      return { ...state, originalConfig: action.config };

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

    case 'SET_MODELS':
      return { ...state, modelCatalog: action.models };

    case 'SET_STEP_ERROR':
      return { ...state, stepErrors: { ...state.stepErrors, [action.step]: action.error } };

    case 'RESET_NEW_REPO':
      return { ...state, newRepoName: '', newRepoUrl: '', newRepoVcs: '' };

    case 'RESET_NEW_COMMAND':
      return { ...state, newCommandName: '', newCommandCommand: '' };

    case 'RESET_NEW_QUICK_LAUNCH':
      return {
        ...state,
        newQuickLaunchName: '',
        newQuickLaunchMode: 'agent',
        newQuickLaunchTarget: '',
        newQuickLaunchPrompt: '',
        newQuickLaunchCommand: '',
        newQuickLaunchPersonaId: '',
        selectedCookbookTemplate: null,
      };

    case 'RESET_NEW_DIFF':
      return { ...state, newDiffName: '', newDiffCommand: '' };

    case 'SET_RUN_TARGET_EDIT_MODAL':
      return { ...state, runTargetEditModal: action.modal };

    case 'SET_QUICK_LAUNCH_EDIT_MODAL':
      return { ...state, quickLaunchEditModal: action.modal };

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

  const hasChanges = useCallback(
    (isFirstRun: boolean) => {
      if (isFirstRun || !state.originalConfig) return false;

      const oc = state.originalConfig;

      const arraysMatch = (a: unknown[], b: unknown[]) => {
        if (a.length !== b.length) return false;
        return a.every((item, i) => JSON.stringify(item) === JSON.stringify(b[i]));
      };

      return (
        state.workspacePath !== oc.workspacePath ||
        state.sourceCodeManagement !== oc.sourceCodeManagement ||
        !arraysMatch(state.repos, oc.repos) ||
        !arraysMatch(state.commandTargets, oc.commandTargets) ||
        !arraysMatch(state.quickLaunch, oc.quickLaunch) ||
        !arraysMatch(state.externalDiffCommands, oc.externalDiffCommands) ||
        state.externalDiffCleanupMinutes !== oc.externalDiffCleanupMinutes ||
        state.nudgenikTarget !== oc.nudgenikTarget ||
        state.branchSuggestTarget !== oc.branchSuggestTarget ||
        state.conflictResolveTarget !== oc.conflictResolveTarget ||
        state.prReviewTarget !== oc.prReviewTarget ||
        state.commitMessageTarget !== oc.commitMessageTarget ||
        state.dashboardPollInterval !== oc.dashboardPollInterval ||
        state.viewedBuffer !== oc.viewedBuffer ||
        state.nudgenikSeenInterval !== oc.nudgenikSeenInterval ||
        state.gitStatusPollInterval !== oc.gitStatusPollInterval ||
        state.gitCloneTimeout !== oc.gitCloneTimeout ||
        state.gitStatusTimeout !== oc.gitStatusTimeout ||
        state.xtermQueryTimeout !== oc.xtermQueryTimeout ||
        state.xtermOperationTimeout !== oc.xtermOperationTimeout ||
        state.xtermStripClearScreen !== oc.xtermStripClearScreen ||
        state.xtermUseWebGL !== oc.xtermUseWebGL ||
        state.networkAccess !== oc.networkAccess ||
        state.authEnabled !== oc.authEnabled ||
        state.authProvider !== oc.authProvider ||
        state.authPublicBaseURL !== oc.authPublicBaseURL ||
        state.authSessionTTLMinutes !== oc.authSessionTTLMinutes ||
        state.authTlsCertPath !== oc.authTlsCertPath ||
        state.authTlsKeyPath !== oc.authTlsKeyPath ||
        state.soundDisabled !== oc.soundDisabled ||
        state.confirmBeforeClose !== oc.confirmBeforeClose ||
        state.suggestDisposeAfterPush !== oc.suggestDisposeAfterPush ||
        JSON.stringify(state.enabledModels) !== JSON.stringify(oc.enabledModels) ||
        state.loreEnabled !== oc.loreEnabled ||
        state.loreLLMTarget !== oc.loreLLMTarget ||
        state.loreCurateOnDispose !== oc.loreCurateOnDispose ||
        state.loreAutoPR !== oc.loreAutoPR ||
        state.subredditTarget !== oc.subredditTarget ||
        state.subredditInterval !== oc.subredditInterval ||
        state.subredditCheckingRange !== oc.subredditCheckingRange ||
        state.subredditMaxPosts !== oc.subredditMaxPosts ||
        state.subredditMaxAge !== oc.subredditMaxAge ||
        JSON.stringify(state.subredditRepos) !== JSON.stringify(oc.subredditRepos) ||
        state.repofeedEnabled !== oc.repofeedEnabled ||
        state.repofeedPublishInterval !== oc.repofeedPublishInterval ||
        state.repofeedFetchInterval !== oc.repofeedFetchInterval ||
        state.repofeedCompletedRetention !== oc.repofeedCompletedRetention ||
        JSON.stringify(state.repofeedRepos) !== JSON.stringify(oc.repofeedRepos) ||
        state.remoteAccessEnabled !== oc.remoteAccessEnabled ||
        state.remoteAccessTimeoutMinutes !== oc.remoteAccessTimeoutMinutes ||
        state.remoteAccessNtfyTopic !== oc.remoteAccessNtfyTopic ||
        state.remoteAccessNotifyCommand !== oc.remoteAccessNotifyCommand ||
        state.desyncEnabled !== oc.desyncEnabled ||
        state.desyncTarget !== oc.desyncTarget ||
        state.fmEnabled !== oc.fmEnabled ||
        state.fmTarget !== oc.fmTarget ||
        state.fmRotationThreshold !== oc.fmRotationThreshold ||
        state.fmDebounceMs !== oc.fmDebounceMs ||
        state.ioWorkspaceTelemetryEnabled !== oc.ioWorkspaceTelemetryEnabled ||
        state.ioWorkspaceTelemetryTarget !== oc.ioWorkspaceTelemetryTarget ||
        state.saplingCmdCreateWorkspace !== oc.saplingCmdCreateWorkspace ||
        state.saplingCmdRemoveWorkspace !== oc.saplingCmdRemoveWorkspace ||
        state.saplingCmdCheckRepoBase !== oc.saplingCmdCheckRepoBase ||
        state.saplingCmdCreateRepoBase !== oc.saplingCmdCreateRepoBase ||
        state.tmuxBinary !== oc.tmuxBinary ||
        state.authSecretsChanged
      );
    },
    [state]
  );

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

  const snapshotConfig = useCallback((): ConfigSnapshot => {
    return {
      workspacePath: state.workspacePath,
      sourceCodeManagement: state.sourceCodeManagement,
      repos: state.repos,
      commandTargets: state.commandTargets,
      quickLaunch: state.quickLaunch,
      externalDiffCommands: state.externalDiffCommands,
      externalDiffCleanupMinutes: state.externalDiffCleanupMinutes,
      nudgenikTarget: state.nudgenikTarget,
      branchSuggestTarget: state.branchSuggestTarget,
      conflictResolveTarget: state.conflictResolveTarget,
      prReviewTarget: state.prReviewTarget,
      commitMessageTarget: state.commitMessageTarget,
      dashboardPollInterval: state.dashboardPollInterval,
      viewedBuffer: state.viewedBuffer,
      nudgenikSeenInterval: state.nudgenikSeenInterval,
      gitStatusPollInterval: state.gitStatusPollInterval,
      gitCloneTimeout: state.gitCloneTimeout,
      gitStatusTimeout: state.gitStatusTimeout,
      xtermQueryTimeout: state.xtermQueryTimeout,
      xtermOperationTimeout: state.xtermOperationTimeout,
      xtermStripClearScreen: state.xtermStripClearScreen,
      xtermUseWebGL: state.xtermUseWebGL,
      networkAccess: state.networkAccess,
      authEnabled: state.authEnabled,
      authProvider: state.authProvider,
      authPublicBaseURL: state.authPublicBaseURL,
      authSessionTTLMinutes: state.authSessionTTLMinutes,
      authTlsCertPath: state.authTlsCertPath,
      authTlsKeyPath: state.authTlsKeyPath,
      soundDisabled: state.soundDisabled,
      confirmBeforeClose: state.confirmBeforeClose,
      suggestDisposeAfterPush: state.suggestDisposeAfterPush,
      enabledModels: state.enabledModels,
      loreEnabled: state.loreEnabled,
      loreLLMTarget: state.loreLLMTarget,
      loreCurateOnDispose: state.loreCurateOnDispose,
      loreAutoPR: state.loreAutoPR,
      subredditTarget: state.subredditTarget,
      subredditInterval: state.subredditInterval,
      subredditCheckingRange: state.subredditCheckingRange,
      subredditMaxPosts: state.subredditMaxPosts,
      subredditMaxAge: state.subredditMaxAge,
      subredditRepos: state.subredditRepos,
      repofeedEnabled: state.repofeedEnabled,
      repofeedPublishInterval: state.repofeedPublishInterval,
      repofeedFetchInterval: state.repofeedFetchInterval,
      repofeedCompletedRetention: state.repofeedCompletedRetention,
      repofeedRepos: state.repofeedRepos,
      remoteAccessEnabled: state.remoteAccessEnabled,
      remoteAccessTimeoutMinutes: state.remoteAccessTimeoutMinutes,
      remoteAccessNtfyTopic: state.remoteAccessNtfyTopic,
      remoteAccessNotifyCommand: state.remoteAccessNotifyCommand,
      desyncEnabled: state.desyncEnabled,
      desyncTarget: state.desyncTarget,
      fmEnabled: state.fmEnabled,
      fmTarget: state.fmTarget,
      fmRotationThreshold: state.fmRotationThreshold,
      fmDebounceMs: state.fmDebounceMs,
      ioWorkspaceTelemetryEnabled: state.ioWorkspaceTelemetryEnabled,
      ioWorkspaceTelemetryTarget: state.ioWorkspaceTelemetryTarget,
      saplingCmdCreateWorkspace: state.saplingCmdCreateWorkspace,
      saplingCmdRemoveWorkspace: state.saplingCmdRemoveWorkspace,
      saplingCmdCheckRepoBase: state.saplingCmdCheckRepoBase,
      saplingCmdCreateRepoBase: state.saplingCmdCreateRepoBase,
      tmuxBinary: state.tmuxBinary,
    };
  }, [state]);

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
    hasChanges,
    checkTargetUsage,
    snapshotConfig,
  };
}
