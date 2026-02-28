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
    repos: [{ name: 'repo1', url: 'git@github.com:user/repo1.git' }],
    promptableTargets: [],
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
    modelVersions: {},
    loreEnabled: true,
    loreLLMTarget: '',
    loreCurateOnDispose: 'session',
    loreAutoPR: false,
    subredditTarget: '',
    subredditHours: 24,
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

  describe('promptable targets', () => {
    it('ADD_PROMPTABLE_TARGET appends', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_PROMPTABLE_TARGET',
          target: { name: 'pt1', command: 'cmd', type: 'promptable', source: 'user' },
        });
      });
      expect(result.current.state.promptableTargets).toHaveLength(1);
    });

    it('REMOVE_PROMPTABLE_TARGET removes by name', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_PROMPTABLE_TARGET',
          target: { name: 'pt1', command: 'cmd', type: 'promptable' },
        });
        result.current.dispatch({
          type: 'ADD_PROMPTABLE_TARGET',
          target: { name: 'pt2', command: 'cmd2', type: 'promptable' },
        });
      });
      act(() => {
        result.current.dispatch({ type: 'REMOVE_PROMPTABLE_TARGET', name: 'pt1' });
      });
      expect(result.current.state.promptableTargets).toHaveLength(1);
      expect(result.current.state.promptableTargets[0].name).toBe('pt2');
    });

    it('UPDATE_PROMPTABLE_TARGET updates command', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_PROMPTABLE_TARGET',
          target: { name: 'pt1', command: 'old', type: 'promptable' },
        });
      });
      act(() => {
        result.current.dispatch({ type: 'UPDATE_PROMPTABLE_TARGET', name: 'pt1', command: 'new' });
      });
      expect(result.current.state.promptableTargets[0].command).toBe('new');
    });
  });

  describe('command targets', () => {
    it('ADD_COMMAND_TARGET appends', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_COMMAND_TARGET',
          target: { name: 'ct1', command: 'make build', type: 'command' },
        });
      });
      expect(result.current.state.commandTargets).toHaveLength(1);
    });

    it('REMOVE_COMMAND_TARGET removes by name', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_COMMAND_TARGET',
          target: { name: 'ct1', command: 'a', type: 'command' },
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
          target: { name: 'ct1', command: 'old', type: 'command' },
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

  describe('modals', () => {
    it('SET_RUN_TARGET_EDIT_MODAL opens and closes', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'SET_RUN_TARGET_EDIT_MODAL',
          modal: {
            target: { name: 'claude', command: 'claude', type: 'promptable' },
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
    it('promptableTargetNames includes detected, promptable, and configured models', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            detectedTargets: [
              { name: 'claude', command: 'claude', type: 'promptable', source: 'detected' },
            ],
            promptableTargets: [
              { name: 'custom', command: 'custom', type: 'promptable', source: 'user' },
            ],
            models: [
              {
                id: 'gpt-4',
                display_name: 'GPT-4',
                configured: true,
                base_tool: 'openai',
                provider: 'openai',
                category: 'external',
                default_value: 'gpt-4',
              },
              {
                id: 'unconfigured',
                display_name: 'X',
                configured: false,
                base_tool: 'x',
                provider: 'x',
                category: 'external',
                default_value: 'x',
              },
            ],
          },
        });
      });
      const names = result.current.promptableTargetNames;
      expect(names.has('claude')).toBe(true);
      expect(names.has('custom')).toBe(true);
      expect(names.has('gpt-4')).toBe(true);
      expect(names.has('unconfigured')).toBe(false);
    });

    it('commandTargetNames tracks command targets', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'ADD_COMMAND_TARGET',
          target: { name: 'build', command: 'make', type: 'command' },
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
            detectedTargets: [],
            promptableTargets: [],
            models: [],
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

    it('target missing flags are false when target exists', () => {
      const { result } = renderHook(() => useConfigForm());
      act(() => {
        result.current.dispatch({
          type: 'LOAD_CONFIG',
          state: {
            nudgenikTarget: 'claude',
            detectedTargets: [
              { name: 'claude', command: 'claude', type: 'promptable', source: 'detected' },
            ],
            promptableTargets: [],
            models: [],
          },
        });
      });
      expect(result.current.nudgenikTargetMissing).toBe(false);
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
            promptableTargets: snapshot.promptableTargets,
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
            modelVersions: snapshot.modelVersions,
            loreEnabled: snapshot.loreEnabled,
            loreLLMTarget: snapshot.loreLLMTarget,
            loreCurateOnDispose: snapshot.loreCurateOnDispose,
            loreAutoPR: snapshot.loreAutoPR,
            subredditTarget: snapshot.subredditTarget,
            subredditHours: snapshot.subredditHours,
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
            promptableTargets: [],
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
            modelVersions: snapshot.modelVersions,
            loreEnabled: snapshot.loreEnabled,
            loreLLMTarget: snapshot.loreLLMTarget,
            loreCurateOnDispose: snapshot.loreCurateOnDispose,
            loreAutoPR: snapshot.loreAutoPR,
            subredditTarget: snapshot.subredditTarget,
            subredditHours: snapshot.subredditHours,
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
