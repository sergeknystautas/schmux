import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  useConfigForm,
  initialState,
  type ConfigFormState,
  type ConfigSnapshot,
} from './useConfigForm';

// Helper: produce a state with known values to test change detection
function makeSnapshot(overrides: Partial<ConfigSnapshot> = {}): ConfigSnapshot {
  return {
    workspacePath: '/home/test/workspaces',
    sourceCodeManagement: 'git-worktree',
    recycleWorkspaces: false,
    repos: [{ name: 'repo1', url: 'git@github.com:user/repo1.git' }],
    commandTargets: [],
    quickLaunch: [],
    externalDiffCommands: [],
    externalDiffCleanupMinutes: 60,
    nudgenikTarget: '',
    branchSuggestTarget: '',
    conflictResolveTarget: '',
    prReviewTarget: '',
    commitMessageTarget: '',
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
    tmuxSocketName: '',
    timelapseEnabled: true,
    timelapseRetentionDays: 7,
    timelapseMaxFileSizeMB: 50,
    timelapseMaxTotalStorageMB: 500,
    localEchoRemote: false,
    debugUI: false,
    pastebin: [],
    ...overrides,
  };
}

describe('useConfigForm', () => {
  describe('initial state', () => {
    it('returns initial state with loading=true', () => {
      const { result } = renderHook(() => useConfigForm());
      expect(result.current.state.loading).toBe(true);
      expect(result.current.state.currentStep).toBe(1);
    });

    it('accepts initial step', () => {
      const { result } = renderHook(() => useConfigForm(3));
      expect(result.current.state.currentStep).toBe(3);
    });
  });

  describe('SET_FIELD', () => {
    it('updates a single field', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'SET_FIELD', field: 'workspacePath', value: '/new/path' });
      });
      expect(result.current.state.workspacePath).toBe('/new/path');
    });
  });

  describe('LOAD_CONFIG', () => {
    it('bulk-sets multiple fields', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            workspacePath: '/loaded',
            sourceCodeManagement: 'git',
            loading: false,
          },
        });
      });
      expect(result.current.state.workspacePath).toBe('/loaded');
      expect(result.current.state.sourceCodeManagement).toBe('git');
      expect(result.current.state.loading).toBe(false);
    });
  });

  describe('repos', () => {
    it('ADD_REPO appends a repo', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_REPO',
          repo: { name: 'myrepo', url: 'git@github.com:u/r.git' },
        });
      });
      expect(result.current.state.repos).toHaveLength(1);
      expect(result.current.state.repos[0].name).toBe('myrepo');
    });

    it('REMOVE_REPO removes by name', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'ADD_REPO', repo: { name: 'a', url: 'u1' } });
        result.current.dispatch({ type: 'ADD_REPO', repo: { name: 'b', url: 'u2' } });
      });
      act(() => {
        result.current.dispatch({ type: 'REMOVE_REPO', name: 'a' });
      });
      expect(result.current.state.repos).toHaveLength(1);
      expect(result.current.state.repos[0].name).toBe('b');
    });

    it('RESET_NEW_REPO clears input fields', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'SET_FIELD', field: 'newRepoName', value: 'name' });
        result.current.dispatch({ type: 'SET_FIELD', field: 'newRepoUrl', value: 'url' });
      });
      act(() => {
        result.current.dispatch({ type: 'RESET_NEW_REPO' });
      });
      expect(result.current.state.newRepoName).toBe('');
      expect(result.current.state.newRepoUrl).toBe('');
    });
  });

  describe('command targets', () => {
    it('ADD_COMMAND_TARGET appends', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_COMMAND_TARGET',
          target: { name: 'ct1', command: 'make build' },
        });
      });
      expect(result.current.state.commandTargets).toHaveLength(1);
    });

    it('REMOVE_COMMAND_TARGET removes by name', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_COMMAND_TARGET',
          target: { name: 'ct1', command: 'a' },
        });
      });
      act(() => {
        result.current.dispatch({ type: 'REMOVE_COMMAND_TARGET', name: 'ct1' });
      });
      expect(result.current.state.commandTargets).toHaveLength(0);
    });

    it('UPDATE_COMMAND_TARGET updates command', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_COMMAND_TARGET',
          target: { name: 'ct1', command: 'old' },
        });
      });
      act(() => {
        result.current.dispatch({ type: 'UPDATE_COMMAND_TARGET', name: 'ct1', command: 'new' });
      });
      expect(result.current.state.commandTargets[0].command).toBe('new');
    });
  });

  describe('quick launch', () => {
    it('ADD_QUICK_LAUNCH appends and sorts', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_QUICK_LAUNCH',
          item: { name: 'zebra', target: 'claude', prompt: 'p' },
        });
        result.current.dispatch({
          type: 'ADD_QUICK_LAUNCH',
          item: { name: 'alpha', target: 'claude', prompt: 'q' },
        });
      });
      expect(result.current.state.quickLaunch[0].name).toBe('alpha');
      expect(result.current.state.quickLaunch[1].name).toBe('zebra');
    });

    it('REMOVE_QUICK_LAUNCH removes by name', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_QUICK_LAUNCH',
          item: { name: 'ql1', target: 't', prompt: 'p' },
        });
      });
      act(() => {
        result.current.dispatch({ type: 'REMOVE_QUICK_LAUNCH', name: 'ql1' });
      });
      expect(result.current.state.quickLaunch).toHaveLength(0);
    });

    it('UPDATE_QUICK_LAUNCH updates item and re-sorts', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_QUICK_LAUNCH',
          item: { name: 'item', target: 't', prompt: 'old' },
        });
      });
      act(() => {
        result.current.dispatch({
          type: 'UPDATE_QUICK_LAUNCH',
          name: 'item',
          updates: { prompt: 'new' },
        });
      });
      expect(result.current.state.quickLaunch[0].prompt).toBe('new');
    });

    it('ADD_QUICK_LAUNCH supports command-type items', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_QUICK_LAUNCH',
          item: { name: 'build', command: 'make build' },
        });
      });
      expect(result.current.state.quickLaunch).toHaveLength(1);
      expect(result.current.state.quickLaunch[0].command).toBe('make build');
      expect(result.current.state.quickLaunch[0].target).toBeUndefined();
    });

    it('RESET_NEW_QUICK_LAUNCH clears mode and command fields', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'SET_FIELD',
          field: 'newQuickLaunchMode',
          value: 'command',
        });
        result.current.dispatch({
          type: 'SET_FIELD',
          field: 'newQuickLaunchCommand',
          value: 'make test',
        });
      });
      expect(result.current.state.newQuickLaunchMode).toBe('command');
      expect(result.current.state.newQuickLaunchCommand).toBe('make test');
      act(() => {
        result.current.dispatch({ type: 'RESET_NEW_QUICK_LAUNCH' });
      });
      expect(result.current.state.newQuickLaunchMode).toBe('agent');
      expect(result.current.state.newQuickLaunchCommand).toBe('');
    });

    it('ADD_QUICK_LAUNCH preserves persona_id on agent items', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_QUICK_LAUNCH',
          item: { name: 'review', target: 'claude', prompt: 'review code', persona_id: 'reviewer' },
        });
      });
      expect(result.current.state.quickLaunch).toHaveLength(1);
      expect(result.current.state.quickLaunch[0].persona_id).toBe('reviewer');
    });

    it('RESET_NEW_QUICK_LAUNCH clears persona_id', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'SET_FIELD',
          field: 'newQuickLaunchPersonaId',
          value: 'reviewer',
        });
      });
      expect(result.current.state.newQuickLaunchPersonaId).toBe('reviewer');
      act(() => {
        result.current.dispatch({ type: 'RESET_NEW_QUICK_LAUNCH' });
      });
      expect(result.current.state.newQuickLaunchPersonaId).toBe('');
    });
  });

  describe('diff commands', () => {
    it('ADD_DIFF_COMMAND / REMOVE_DIFF_COMMAND', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_DIFF_COMMAND',
          command: { name: 'ksdiff', command: 'ksdiff' },
        });
      });
      expect(result.current.state.externalDiffCommands).toHaveLength(1);
      act(() => {
        result.current.dispatch({ type: 'REMOVE_DIFF_COMMAND', name: 'ksdiff' });
      });
      expect(result.current.state.externalDiffCommands).toHaveLength(0);
    });
  });

  describe('pastebin', () => {
    it('ADD_PASTEBIN appends and sorts', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'ADD_PASTEBIN', content: 'beta' });
        result.current.dispatch({ type: 'ADD_PASTEBIN', content: 'alpha' });
      });
      expect(result.current.state.pastebin).toEqual(['alpha', 'beta']);
    });

    it('REMOVE_PASTEBIN removes by index', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: { pastebin: ['alpha', 'beta', 'gamma'] },
        });
      });
      act(() => {
        result.current.dispatch({ type: 'REMOVE_PASTEBIN', index: 1 });
      });
      expect(result.current.state.pastebin).toEqual(['alpha', 'gamma']);
    });

    it('ADD_PASTEBIN closes modal', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'SET_PASTEBIN_EDIT_MODAL',
          modal: { content: 'new clip', error: '' },
        });
      });
      expect(result.current.state.pastebinEditModal).not.toBeNull();
      act(() => {
        result.current.dispatch({ type: 'ADD_PASTEBIN', content: 'new clip' });
      });
      expect(result.current.state.pastebinEditModal).toBeNull();
      expect(result.current.state.pastebin).toEqual(['new clip']);
    });
  });

  describe('modals', () => {
    it('SET_RUN_TARGET_EDIT_MODAL opens and closes', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'SET_RUN_TARGET_EDIT_MODAL',
          modal: {
            target: { name: 'claude', command: 'claude' },
            command: 'claude',
            error: '',
          },
        });
      });
      expect(result.current.state.runTargetEditModal).not.toBeNull();
      act(() => {
        result.current.dispatch({ type: 'SET_RUN_TARGET_EDIT_MODAL', modal: null });
      });
      expect(result.current.state.runTargetEditModal).toBeNull();
    });
  });

  describe('step errors', () => {
    it('SET_STEP_ERROR sets and clears errors', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'SET_STEP_ERROR', step: 1, error: 'bad' });
      });
      expect(result.current.state.stepErrors[1]).toBe('bad');
      act(() => {
        result.current.dispatch({ type: 'SET_STEP_ERROR', step: 1, error: null });
      });
      expect(result.current.state.stepErrors[1]).toBeNull();
    });
  });

  describe('derived values', () => {
    it('modelTargetNames includes configured models', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            modelCatalog: [
              {
                id: 'gpt-4',
                display_name: 'GPT-4',
                configured: true,
                provider: 'openai',
                runners: ['openai'],
              },
              {
                id: 'unconfigured',
                display_name: 'X',
                configured: false,
                provider: 'x',
                runners: ['x'],
              },
            ],
          },
        });
      });
      const names = result.current.modelTargetNames;
      expect(names.has('gpt-4')).toBe(true);
      expect(names.has('unconfigured')).toBe(false);
    });

    it('commandTargetNames tracks command targets', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_COMMAND_TARGET',
          target: { name: 'build', command: 'make' },
        });
      });
      expect(result.current.commandTargetNames.has('build')).toBe(true);
    });

    it('target missing flags detect orphaned targets', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            nudgenikTarget: 'nonexistent',
            branchSuggestTarget: 'nonexistent',
            conflictResolveTarget: 'nonexistent',
            prReviewTarget: 'nonexistent',
            commitMessageTarget: 'nonexistent',
            modelCatalog: [],
          },
        });
      });
      expect(result.current.nudgenikTargetMissing).toBe(true);
      expect(result.current.branchSuggestTargetMissing).toBe(true);
      expect(result.current.conflictResolveTargetMissing).toBe(true);
      expect(result.current.prReviewTargetMissing).toBe(true);
      expect(result.current.commitMessageTargetMissing).toBe(true);
    });

    it('target missing flags are false when target is empty', () => {
      const { result } = renderHook(() => useConfigForm());
      // All targets default to ''
      expect(result.current.nudgenikTargetMissing).toBe(false);
      expect(result.current.branchSuggestTargetMissing).toBe(false);
    });

    it('target missing flags are false when target exists as model', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            nudgenikTarget: 'claude-sonnet',
            modelCatalog: [
              {
                id: 'claude-sonnet',
                display_name: 'Claude Sonnet',
                configured: true,
                provider: 'anthropic',
                runners: ['claude'],
              },
            ],
          },
        });
      });
      expect(result.current.nudgenikTargetMissing).toBe(false);
    });
  });

  describe('oneshotModels', () => {
    it('includes models with oneshot-capable runners when enabledModels is empty', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            modelCatalog: [
              {
                id: 'claude-sonnet',
                display_name: 'Claude Sonnet',
                configured: true,
                provider: 'anthropic',
                runners: ['claude'],
              },
              {
                id: 'gemini-pro',
                display_name: 'Gemini Pro',
                configured: true,
                provider: 'google',
                runners: ['gemini'],
              },
            ],
            runners: {
              claude: { available: true, capabilities: ['interactive', 'oneshot', 'streaming'] },
              gemini: { available: true, capabilities: ['interactive'] },
            },
            enabledModels: {},
          },
        });
      });
      // Claude has oneshot capability, Gemini does not
      const ids = result.current.oneshotModels.map((m) => m.id);
      expect(ids).toContain('claude-sonnet');
      expect(ids).not.toContain('gemini-pro');
    });

    it('uses preferred runner from enabledModels when set', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            modelCatalog: [
              {
                id: 'claude-sonnet',
                display_name: 'Claude Sonnet',
                configured: true,
                provider: 'anthropic',
                runners: ['claude', 'opencode'],
              },
            ],
            runners: {
              claude: { available: true, capabilities: ['interactive', 'oneshot', 'streaming'] },
              opencode: { available: true, capabilities: ['interactive', 'oneshot'] },
            },
            enabledModels: { 'claude-sonnet': 'claude' },
          },
        });
      });
      const ids = result.current.oneshotModels.map((m) => m.id);
      expect(ids).toContain('claude-sonnet');
    });

    it('excludes models whose preferred runner lacks oneshot', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            modelCatalog: [
              {
                id: 'gemini-pro',
                display_name: 'Gemini Pro',
                configured: true,
                provider: 'google',
                runners: ['gemini'],
              },
            ],
            runners: {
              gemini: { available: true, capabilities: ['interactive'] },
            },
            enabledModels: { 'gemini-pro': 'gemini' },
          },
        });
      });
      const ids = result.current.oneshotModels.map((m) => m.id);
      expect(ids).not.toContain('gemini-pro');
    });
  });

  describe('models (for QuickLaunchTab)', () => {
    it('includes all configured models regardless of oneshot capability', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            modelCatalog: [
              {
                id: 'claude-sonnet',
                display_name: 'Claude Sonnet',
                configured: true,
                provider: 'anthropic',
                runners: ['claude'],
              },
              {
                id: 'gemini-pro',
                display_name: 'Gemini Pro',
                configured: true,
                provider: 'google',
                runners: ['gemini'],
              },
              {
                id: 'unconfigured-model',
                display_name: 'Unconfigured',
                configured: false,
                provider: 'x',
                runners: ['x'],
              },
            ],
            runners: {
              claude: { available: true, capabilities: ['interactive', 'oneshot'] },
              gemini: { available: true, capabilities: ['interactive'] },
            },
            enabledModels: {},
          },
        });
      });
      // models should include all configured models (both claude and gemini)
      const ids = result.current.models.map((m) => m.id);
      expect(ids).toContain('claude-sonnet');
      expect(ids).toContain('gemini-pro');
      expect(ids).not.toContain('unconfigured-model');
    });
  });

  describe('enabledModels', () => {
    it('TOGGLE_MODEL enables a model with default runner', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'TOGGLE_MODEL',
          modelId: 'gpt-4o',
          enabled: true,
          defaultRunner: 'openai',
        });
      });
      expect(result.current.state.enabledModels).toEqual({ 'gpt-4o': 'openai' });
    });

    it('TOGGLE_MODEL disables a model', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'TOGGLE_MODEL',
          modelId: 'gpt-4o',
          enabled: true,
          defaultRunner: 'openai',
        });
      });
      expect(result.current.state.enabledModels).toEqual({ 'gpt-4o': 'openai' });
      act(() => {
        result.current.dispatch({
          type: 'TOGGLE_MODEL',
          modelId: 'gpt-4o',
          enabled: false,
          defaultRunner: 'openai',
        });
      });
      expect(result.current.state.enabledModels).toEqual({});
    });

    it('CHANGE_RUNNER updates runner for enabled model', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'TOGGLE_MODEL',
          modelId: 'gpt-4o',
          enabled: true,
          defaultRunner: 'openai',
        });
      });
      act(() => {
        result.current.dispatch({
          type: 'CHANGE_RUNNER',
          modelId: 'gpt-4o',
          runner: 'azure',
        });
      });
      expect(result.current.state.enabledModels).toEqual({ 'gpt-4o': 'azure' });
    });

    it('CHANGE_RUNNER does nothing for non-enabled model', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'CHANGE_RUNNER',
          modelId: 'gpt-4o',
          runner: 'azure',
        });
      });
      expect(result.current.state.enabledModels).toEqual({});
    });
  });

  describe('hasChanges', () => {
    it('returns false when original is null', () => {
      const { result } = renderHook(() => useConfigForm());
      expect(result.current.hasChanges(false)).toBe(false);
    });

    it('returns false in first-run mode', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'SET_ORIGINAL', config: makeSnapshot() });
        result.current.dispatch({ type: 'SET_FIELD', field: 'workspacePath', value: '/changed' });
      });
      expect(result.current.hasChanges(true)).toBe(false);
    });

    it('returns false when state matches original', () => {
      const { result } = renderHook(() => useConfigForm());
      const snapshot = makeSnapshot();
      act(() => {
        result.current.dispatch({ type: 'SET_ORIGINAL', config: snapshot });
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            workspacePath: snapshot.workspacePath,
            sourceCodeManagement: snapshot.sourceCodeManagement,
            repos: snapshot.repos,
            commandTargets: snapshot.commandTargets,
            quickLaunch: snapshot.quickLaunch,
            externalDiffCommands: snapshot.externalDiffCommands,
            externalDiffCleanupMinutes: snapshot.externalDiffCleanupMinutes,
            nudgenikTarget: snapshot.nudgenikTarget,
            branchSuggestTarget: snapshot.branchSuggestTarget,
            conflictResolveTarget: snapshot.conflictResolveTarget,
            prReviewTarget: snapshot.prReviewTarget,
            commitMessageTarget: snapshot.commitMessageTarget,
            dashboardPollInterval: snapshot.dashboardPollInterval,
            viewedBuffer: snapshot.viewedBuffer,
            nudgenikSeenInterval: snapshot.nudgenikSeenInterval,
            gitStatusPollInterval: snapshot.gitStatusPollInterval,
            gitCloneTimeout: snapshot.gitCloneTimeout,
            gitStatusTimeout: snapshot.gitStatusTimeout,
            xtermQueryTimeout: snapshot.xtermQueryTimeout,
            xtermOperationTimeout: snapshot.xtermOperationTimeout,
            xtermUseWebGL: snapshot.xtermUseWebGL,
            networkAccess: snapshot.networkAccess,
            authEnabled: snapshot.authEnabled,
            authProvider: snapshot.authProvider,
            authPublicBaseURL: snapshot.authPublicBaseURL,
            authSessionTTLMinutes: snapshot.authSessionTTLMinutes,
            authTlsCertPath: snapshot.authTlsCertPath,
            authTlsKeyPath: snapshot.authTlsKeyPath,
            soundDisabled: snapshot.soundDisabled,
            confirmBeforeClose: snapshot.confirmBeforeClose,
            suggestDisposeAfterPush: snapshot.suggestDisposeAfterPush,
            enabledModels: snapshot.enabledModels,
            loreEnabled: snapshot.loreEnabled,
            loreLLMTarget: snapshot.loreLLMTarget,
            loreCurateOnDispose: snapshot.loreCurateOnDispose,
            loreAutoPR: snapshot.loreAutoPR,
            lorePublicRuleMode: snapshot.lorePublicRuleMode,
            subredditTarget: snapshot.subredditTarget,
            subredditInterval: snapshot.subredditInterval,
            subredditCheckingRange: snapshot.subredditCheckingRange,
            subredditMaxPosts: snapshot.subredditMaxPosts,
            subredditMaxAge: snapshot.subredditMaxAge,
            subredditRepos: snapshot.subredditRepos,
            repofeedEnabled: snapshot.repofeedEnabled,
            repofeedPublishInterval: snapshot.repofeedPublishInterval,
            repofeedFetchInterval: snapshot.repofeedFetchInterval,
            repofeedCompletedRetention: snapshot.repofeedCompletedRetention,
            repofeedRepos: snapshot.repofeedRepos,
            remoteAccessEnabled: snapshot.remoteAccessEnabled,
            remoteAccessTimeoutMinutes: snapshot.remoteAccessTimeoutMinutes,
            remoteAccessNtfyTopic: snapshot.remoteAccessNtfyTopic,
            remoteAccessNotifyCommand: snapshot.remoteAccessNotifyCommand,
            desyncEnabled: snapshot.desyncEnabled,
            desyncTarget: snapshot.desyncTarget,
            ioWorkspaceTelemetryEnabled: snapshot.ioWorkspaceTelemetryEnabled,
            ioWorkspaceTelemetryTarget: snapshot.ioWorkspaceTelemetryTarget,
            saplingCmdCreateWorkspace: snapshot.saplingCmdCreateWorkspace,
            saplingCmdRemoveWorkspace: snapshot.saplingCmdRemoveWorkspace,
            saplingCmdCheckRepoBase: snapshot.saplingCmdCheckRepoBase,
            saplingCmdCreateRepoBase: snapshot.saplingCmdCreateRepoBase,
            localEchoRemote: snapshot.localEchoRemote,
            tmuxBinary: snapshot.tmuxBinary,
            tmuxSocketName: snapshot.tmuxSocketName,
            pastebin: snapshot.pastebin,
          },
        });
      });
      expect(result.current.hasChanges(false)).toBe(false);
    });

    it('detects scalar field changes', () => {
      const { result } = renderHook(() => useConfigForm());
      const snapshot = makeSnapshot();
      act(() => {
        result.current.dispatch({ type: 'SET_ORIGINAL', config: snapshot });
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            workspacePath: snapshot.workspacePath,
            sourceCodeManagement: snapshot.sourceCodeManagement,
            repos: snapshot.repos,
            commandTargets: [],
            quickLaunch: [],
            externalDiffCommands: [],
            externalDiffCleanupMinutes: snapshot.externalDiffCleanupMinutes,
            nudgenikTarget: snapshot.nudgenikTarget,
            branchSuggestTarget: snapshot.branchSuggestTarget,
            conflictResolveTarget: snapshot.conflictResolveTarget,
            prReviewTarget: snapshot.prReviewTarget,
            commitMessageTarget: snapshot.commitMessageTarget,
            dashboardPollInterval: snapshot.dashboardPollInterval,
            viewedBuffer: snapshot.viewedBuffer,
            nudgenikSeenInterval: snapshot.nudgenikSeenInterval,
            gitStatusPollInterval: snapshot.gitStatusPollInterval,
            gitCloneTimeout: snapshot.gitCloneTimeout,
            gitStatusTimeout: snapshot.gitStatusTimeout,
            xtermQueryTimeout: snapshot.xtermQueryTimeout,
            xtermOperationTimeout: snapshot.xtermOperationTimeout,
            xtermUseWebGL: snapshot.xtermUseWebGL,
            networkAccess: snapshot.networkAccess,
            authEnabled: snapshot.authEnabled,
            authProvider: snapshot.authProvider,
            authPublicBaseURL: snapshot.authPublicBaseURL,
            authSessionTTLMinutes: snapshot.authSessionTTLMinutes,
            authTlsCertPath: snapshot.authTlsCertPath,
            authTlsKeyPath: snapshot.authTlsKeyPath,
            soundDisabled: snapshot.soundDisabled,
            confirmBeforeClose: snapshot.confirmBeforeClose,
            suggestDisposeAfterPush: snapshot.suggestDisposeAfterPush,
            enabledModels: snapshot.enabledModels,
            loreEnabled: snapshot.loreEnabled,
            loreLLMTarget: snapshot.loreLLMTarget,
            loreCurateOnDispose: snapshot.loreCurateOnDispose,
            loreAutoPR: snapshot.loreAutoPR,
            lorePublicRuleMode: snapshot.lorePublicRuleMode,
            subredditTarget: snapshot.subredditTarget,
            subredditInterval: snapshot.subredditInterval,
            subredditCheckingRange: snapshot.subredditCheckingRange,
            subredditMaxPosts: snapshot.subredditMaxPosts,
            subredditMaxAge: snapshot.subredditMaxAge,
            subredditRepos: snapshot.subredditRepos,
            repofeedEnabled: snapshot.repofeedEnabled,
            repofeedPublishInterval: snapshot.repofeedPublishInterval,
            repofeedFetchInterval: snapshot.repofeedFetchInterval,
            repofeedCompletedRetention: snapshot.repofeedCompletedRetention,
            repofeedRepos: snapshot.repofeedRepos,
            remoteAccessEnabled: snapshot.remoteAccessEnabled,
            remoteAccessTimeoutMinutes: snapshot.remoteAccessTimeoutMinutes,
            remoteAccessNtfyTopic: snapshot.remoteAccessNtfyTopic,
            remoteAccessNotifyCommand: snapshot.remoteAccessNotifyCommand,
            desyncEnabled: snapshot.desyncEnabled,
            desyncTarget: snapshot.desyncTarget,
            ioWorkspaceTelemetryEnabled: snapshot.ioWorkspaceTelemetryEnabled,
            ioWorkspaceTelemetryTarget: snapshot.ioWorkspaceTelemetryTarget,
          },
        });
      });
      // Change workspace path
      act(() => {
        result.current.dispatch({ type: 'SET_FIELD', field: 'workspacePath', value: '/different' });
      });
      expect(result.current.hasChanges(false)).toBe(true);
    });

    it('detects array changes (adding a repo)', () => {
      const { result } = renderHook(() => useConfigForm());
      const snapshot = makeSnapshot();
      act(() => {
        result.current.dispatch({ type: 'SET_ORIGINAL', config: snapshot });
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: { repos: [...snapshot.repos] },
        });
      });
      act(() => {
        result.current.dispatch({ type: 'ADD_REPO', repo: { name: 'new', url: 'url' } });
      });
      expect(result.current.hasChanges(false)).toBe(true);
    });
  });

  describe('checkTargetUsage', () => {
    it('detects usage in quick launch', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_QUICK_LAUNCH',
          item: { name: 'ql', target: 'claude', prompt: 'p' },
        });
      });
      const usage = result.current.checkTargetUsage('claude');
      expect(usage.inQuickLaunch).toBe(true);
    });

    it('detects usage in nudgenik', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'SET_FIELD', field: 'nudgenikTarget', value: 'claude' });
      });
      const usage = result.current.checkTargetUsage('claude');
      expect(usage.inNudgenik).toBeTruthy();
    });

    it('detects usage in pr review', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'SET_FIELD', field: 'prReviewTarget', value: 'claude' });
      });
      const usage = result.current.checkTargetUsage('claude');
      expect(usage.inPrReview).toBeTruthy();
    });

    it('returns all false when target is unused', () => {
      const { result } = renderHook(() => useConfigForm());
      const usage = result.current.checkTargetUsage('unused');
      expect(usage.inQuickLaunch).toBe(false);
      expect(usage.inNudgenik).toBeFalsy();
      expect(usage.inBranchSuggest).toBeFalsy();
      expect(usage.inConflictResolve).toBeFalsy();
      expect(usage.inPrReview).toBeFalsy();
      expect(usage.inCommitMessage).toBeFalsy();
    });
  });

  describe('snapshotConfig', () => {
    it('returns current state as ConfigSnapshot', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({ type: 'SET_FIELD', field: 'workspacePath', value: '/snap' });
        result.current.dispatch({ type: 'SET_FIELD', field: 'nudgenikTarget', value: 'claude' });
      });
      const snap = result.current.snapshotConfig();
      expect(snap.workspacePath).toBe('/snap');
      expect(snap.nudgenikTarget).toBe('claude');
    });
  });
});
