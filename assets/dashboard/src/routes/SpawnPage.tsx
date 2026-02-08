import { useEffect, useMemo, useRef, useState, useCallback } from 'react';
import { useSearchParams, useNavigate, useLocation } from 'react-router-dom';
import { getConfig, spawnSessions, getErrorMessage, suggestBranch } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useRequireConfig, useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { usePendingNavigation } from '../lib/navigation';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import PromptTextarea from '../components/PromptTextarea';
import RemoteHostSelector, { type EnvironmentSelection } from '../components/RemoteHostSelector';
import type { Model, RepoResponse, RunTargetResponse, SpawnResult, SuggestBranchResponse, RemoteFlavor } from '../lib/types';
import { WORKSPACE_EXPANDED_KEY } from '../lib/constants';


// ============================================================================
// Layer 2: Session Storage Draft (Active Draft)
// Per-tab, cleared on successful spawn
// ============================================================================

interface SpawnDraft {
  prompt: string;
  spawnMode: 'promptable' | 'command' | 'resume';
  selectedCommand: string;
  targetCounts: Record<string, number>;
  modelSelectionMode: 'single' | 'multiple' | 'advanced';
  // Only for fresh spawns (no workspace_id)
  repo?: string;
  newRepoName?: string;
}

function getSpawnDraftKey(workspaceId: string | null): string {
  return `spawn-draft-${workspaceId || 'fresh'}`;
}

function loadSpawnDraft(workspaceId: string | null): SpawnDraft | null {
  try {
    const key = getSpawnDraftKey(workspaceId);
    const stored = sessionStorage.getItem(key);
    if (stored) {
      return JSON.parse(stored) as SpawnDraft;
    }
  } catch (err) {
    console.warn('Failed to load spawn draft:', err);
  }
  return null;
}

function saveSpawnDraft(workspaceId: string | null, draft: SpawnDraft): void {
  try {
    const key = getSpawnDraftKey(workspaceId);
    sessionStorage.setItem(key, JSON.stringify(draft));
  } catch (err) {
    console.warn('Failed to save spawn draft:', err);
  }
}

function clearSpawnDraft(workspaceId: string | null): void {
  try {
    const key = getSpawnDraftKey(workspaceId);
    sessionStorage.removeItem(key);
  } catch (err) {
    console.warn('Failed to clear spawn draft:', err);
  }
}

// ============================================================================
// Layer 3: Local Storage (Long-term Memory)
// Cross-tab, never auto-cleared, updated on successful spawn
// Keys use 'schmux:' prefix for consistency with other localStorage usage
// Cross-tab sync happens automatically via storage event on next page load
// ============================================================================

const LAST_REPO_KEY = 'schmux:spawn-last-repo';
const LAST_TARGET_COUNTS_KEY = 'schmux:spawn-last-target-counts';
const LAST_MODEL_SELECTION_MODE_KEY = 'schmux:spawn-last-model-selection-mode';

function loadLastRepo(): string | null {
  try {
    return localStorage.getItem(LAST_REPO_KEY);
  } catch (err) {
    console.warn('Failed to load last repo:', err);
    return null;
  }
}

function saveLastRepo(repo: string): void {
  try {
    localStorage.setItem(LAST_REPO_KEY, repo);
  } catch (err) {
    console.warn('Failed to save last repo:', err);
  }
}

function loadLastTargetCounts(): Record<string, number> | null {
  try {
    const stored = localStorage.getItem(LAST_TARGET_COUNTS_KEY);
    if (stored) {
      return JSON.parse(stored) as Record<string, number>;
    }
  } catch (err) {
    console.warn('Failed to load last target counts:', err);
  }
  return null;
}

function saveLastTargetCounts(counts: Record<string, number>): void {
  try {
    // Only save non-zero counts
    const nonZero: Record<string, number> = {};
    Object.entries(counts).forEach(([name, count]) => {
      if (count > 0) {
        nonZero[name] = count;
      }
    });
    localStorage.setItem(LAST_TARGET_COUNTS_KEY, JSON.stringify(nonZero));
  } catch (err) {
    console.warn('Failed to save last target counts:', err);
  }
}

function loadLastModelSelectionMode(): 'single' | 'multiple' | 'advanced' | null {
  try {
    const stored = localStorage.getItem(LAST_MODEL_SELECTION_MODE_KEY);
    if (stored) {
      return stored as 'single' | 'multiple' | 'advanced';
    }
  } catch (err) {
    console.warn('Failed to load last model selection mode:', err);
  }
  return null;
}

function saveLastModelSelectionMode(mode: 'single' | 'multiple' | 'advanced'): void {
  try {
    localStorage.setItem(LAST_MODEL_SELECTION_MODE_KEY, mode);
  } catch (err) {
    console.warn('Failed to save last model selection mode:', err);
  }
}


export default function SpawnPage() {
  useRequireConfig();
  const [repos, setRepos] = useState<RepoResponse[]>([]);
  const [promptableTargets, setPromptableTargets] = useState<RunTargetResponse[]>([]);
  const [commandTargets, setCommandTargets] = useState<RunTargetResponse[]>([]);
  const [models, setModels] = useState<Model[]>([]);
  const [selectedCommand, setSelectedCommand] = useState('');
  const [spawnMode, setSpawnMode] = useState<'promptable' | 'command' | 'resume'>('promptable');
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('');
  const [newRepoName, setNewRepoName] = useState('');
  const [prompt, setPrompt] = useState('');
  const [nickname, setNickname] = useState('');
  const [engagePhase, setEngagePhase] = useState<'idle' | 'naming' | 'spawning' | 'waiting'>('idle');
  const [showBranchInput, setShowBranchInput] = useState(false);
  const [prefillWorkspaceId, setPrefillWorkspaceId] = useState('');
  const [resolvedWorkspaceId, setResolvedWorkspaceId] = useState('');
  const [environment, setEnvironment] = useState<EnvironmentSelection>({ type: 'local' });
  const skipNextPersist = useRef(false);
  const [loading, setLoading] = useState(true);
  const [configError, setConfigError] = useState('');

  const [searchParams] = useSearchParams();
  const { error: toastError } = useToast();
  const { workspaces, loading: sessionsLoading, waitForSession } = useSessions();
  const { setPendingNavigation } = usePendingNavigation();
  const { config } = useConfig();

  const location = useLocation();

  // Precompute URL -> default branch map for O(1) lookups
  const defaultBranchMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const repo of repos) {
      map.set(repo.url, repo.default_branch || 'main');
    }
    return map;
  }, [repos]);

  // Helper to get the default branch for a repo URL from precomputed map
  const getDefaultBranch = (repoUrl: string): string => {
    return defaultBranchMap.get(repoUrl) || 'main';
  };

  // Spawn page mode: derived from current URL/state
  const urlWorkspaceId = searchParams.get('workspace_id');
  const mode: 'workspace' | 'prefilled' | 'fresh' = (() => {
    if (urlWorkspaceId) return 'workspace';
    if (location.state?.repo && location.state?.branch) return 'prefilled';
    return 'fresh';
  })();
  const initialized = useRef(false);

  const isMounted = useRef(true);
  const navigate = useNavigate();
  const inExistingWorkspace = mode === 'workspace';

  // Get current workspace for header display
  const currentWorkspace = workspaces?.find(ws => ws.id === resolvedWorkspaceId);
  const workspaceExists = resolvedWorkspaceId && workspaces?.some(ws => ws.id === resolvedWorkspaceId);

  // Navigate home if workspace was disposed while on this page (in workspace mode)
  useEffect(() => {
    if (inExistingWorkspace && resolvedWorkspaceId && !workspaceExists && !sessionsLoading) {
      navigate('/');
    }
  }, [inExistingWorkspace, resolvedWorkspaceId, workspaceExists, sessionsLoading, navigate]);

  // Get branch suggest target from config
  const branchSuggestTarget = config?.branch_suggest?.target || '';

  // Remote flavors without a provisioning command don't need repo/branch selection
  const isRemoteWithoutProvisioning = environment.type === 'remote' && !environment.flavor.provision_command;

  // Show branch input immediately when suggestion is disabled
  useEffect(() => {
    if (mode === 'fresh' && !branchSuggestTarget && config) {
      setShowBranchInput(true);
    }
  }, [mode, branchSuggestTarget, config]);

  useEffect(() => {
    return () => {
      isMounted.current = false;
    };
  }, []);

  // Load config and data
  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setConfigError('');
      try {
        const cfg = await getConfig();
        if (!active) return;
        setRepos((cfg.repos || []).sort((a, b) => a.name.localeCompare(b.name)));
        const modelBaseTools = new Set((cfg.models || []).map((model) => model.base_tool));
        const promptableItems = (cfg.run_targets || []).filter(t => {
          if (t.type !== 'promptable') {
            return false;
          }
          if (t.source === 'detected' && modelBaseTools.has(t.name)) {
            return false;
          }
          return true;
        }).sort((a, b) => a.name.localeCompare(b.name));
        const commandItems = (cfg.run_targets || []).filter(t => t.type === 'command').sort((a, b) => a.name.localeCompare(b.name));
        setPromptableTargets(promptableItems);
        setCommandTargets(commandItems);
        setModels(cfg.models || []);

      } catch (err) {
        if (!active) return;
        setConfigError(getErrorMessage(err, 'Failed to load config'));
      } finally {
        if (active) setLoading(false);
      }
    };

    load();
    return () => { active = false; };
  }, []);

  // Re-run initialization when navigation context changes.
  useEffect(() => {
    initialized.current = false;
  }, [mode, urlWorkspaceId, location.key]);

  // Initialize form fields based on mode (re-runs on context change; see docs/sessions.md)
  // Three-layer waterfall: Mode Logic → sessionStorage → localStorage → Default
  useEffect(() => {
    if (initialized.current) return;

    // Layer 1: Mode Logic (Entry Point)
    if (mode === 'workspace') {
      // Wait for workspace data to load
      if (sessionsLoading) return;

      const workspaceId = searchParams.get('workspace_id')!;
      setPrefillWorkspaceId(workspaceId);
      setResolvedWorkspaceId(workspaceId);

      const workspace = workspaces.find(ws => ws.id === workspaceId);
      if (workspace) {
        setRepo(workspace.repo);
        setBranch(workspace.branch);
      }
    } else if (mode === 'prefilled') {
      const state = location.state as { repo: string; branch: string; prompt: string; nickname: string };
      setRepo(state.repo);
      setBranch(state.branch);
      setPrompt(state.prompt);
      if (state.nickname) setNickname(state.nickname);
    }

    // Layer 2: sessionStorage Draft (Active Draft)
    const draft = loadSpawnDraft(urlWorkspaceId);

    // Layer 3: localStorage (Long-term Memory)
    const lastRepo = loadLastRepo();
    const lastTargetCounts = loadLastTargetCounts();
    const lastModelSelectionMode = loadLastModelSelectionMode();

    // Apply three-layer waterfall for each field
    if (mode === 'workspace' || mode === 'prefilled') {
      // prompt: draft (workspace/prefilled already set prompt in Layer 1 for prefilled)
      if (mode === 'workspace' && draft?.prompt) {
        setPrompt(draft.prompt);
      }
      // spawnMode: draft → default
      setSpawnMode(draft?.spawnMode || 'promptable');
      // modelSelectionMode: draft → localStorage → default
      setModelSelectionMode(draft?.modelSelectionMode || lastModelSelectionMode || 'single');
      // selectedCommand: draft → default
      if (draft?.selectedCommand) setSelectedCommand(draft.selectedCommand);
      // targetCounts: draft → localStorage → default
      if (draft?.targetCounts) {
        setTargetCounts(draft.targetCounts);
      } else if (lastTargetCounts) {
        setTargetCounts(lastTargetCounts);
      }
    } else if (mode === 'fresh') {
      // repo: draft → localStorage → default
      setRepo(draft?.repo || lastRepo || '');
      // newRepoName: draft → default
      if (draft?.newRepoName) setNewRepoName(draft.newRepoName);
      // prompt: draft → default
      if (draft?.prompt) setPrompt(draft.prompt);
      // spawnMode: draft → default
      setSpawnMode(draft?.spawnMode || 'promptable');
      // modelSelectionMode: draft → localStorage → default
      setModelSelectionMode(draft?.modelSelectionMode || lastModelSelectionMode || 'single');
      // selectedCommand: draft → default
      if (draft?.selectedCommand) setSelectedCommand(draft.selectedCommand);
      // targetCounts: draft → localStorage → default
      if (draft?.targetCounts) {
        setTargetCounts(draft.targetCounts);
      } else if (lastTargetCounts) {
        setTargetCounts(lastTargetCounts);
      }
    }

    initialized.current = true;
    skipNextPersist.current = true;
  }, [mode, sessionsLoading, workspaces, searchParams, urlWorkspaceId, location.state]);

  type PromptableListItem = {
    name: string;
    label: string;
  };

  const promptableList = useMemo<PromptableListItem[]>(() => {
    const modelLabels = new Map(models.map((model) => [model.id, model.display_name]));
    return promptableTargets.map((target) => ({
      name: target.name,
      label: modelLabels.get(target.name) || target.name,
    }));
  }, [models, promptableTargets]);

  const [targetCounts, setTargetCounts] = useState<Record<string, number>>({});
  const [modelSelectionMode, setModelSelectionMode] = useState<'single' | 'multiple' | 'advanced'>('single');

  // Ensure all items are in targetCounts (skip when empty to avoid wiping draft values)
  useEffect(() => {
    if (promptableList.length === 0) return;
    setTargetCounts((current) => {
      const next = { ...current };
      let changed = false;
      promptableList.forEach((item) => {
        if (next[item.name] === undefined) {
          next[item.name] = 0;
          changed = true;
        }
      });
      Object.keys(next).forEach((name) => {
        if (!promptableList.find((item) => item.name === name)) {
          delete next[name];
          changed = true;
        }
      });
      return changed ? next : current;
    });
  }, [promptableList]);

  // Enforce single mode constraint: when switching to single, reduce to at most one selection
  useEffect(() => {
    if (modelSelectionMode !== 'single') return;
    if (promptableList.length === 0) return;
    setTargetCounts((current) => {
      // Find all selected agents
      const selected = promptableList.filter((item) => (current[item.name] || 0) > 0);
      if (selected.length <= 1) return current; // Already at most one

      // Keep only the first selected, clear the rest
      const firstSelected = selected[0].name;
      const next: Record<string, number> = {};
      promptableList.forEach((item) => {
        next[item.name] = item.name === firstSelected ? 1 : 0;
      });
      return next;
    });
  }, [modelSelectionMode, promptableList]);

  // Persist to sessionStorage on changes
  useEffect(() => {
    if (!initialized.current) return;
    if (skipNextPersist.current) {
      skipNextPersist.current = false;
      return;
    }
    // Don't save if spawn succeeded (navigating away)
    if (engagePhase === 'waiting') return;

    const draft: SpawnDraft = {
      prompt,
      spawnMode,
      selectedCommand,
      targetCounts,
      modelSelectionMode,
    };
    // Only save repo/newRepoName for fresh spawns
    if (!urlWorkspaceId) {
      draft.repo = repo;
      draft.newRepoName = newRepoName;
    }
    saveSpawnDraft(urlWorkspaceId, draft);
  }, [prompt, spawnMode, selectedCommand, targetCounts, modelSelectionMode, repo, newRepoName, urlWorkspaceId, engagePhase]);

  const totalPromptableCount = useMemo(() => {
    return Object.values(targetCounts).reduce((sum, count) => sum + count, 0);
  }, [targetCounts]);


  const updateTargetCount = (name: string, delta: number) => {
    setTargetCounts((current) => {
      const next = Math.max(0, Math.min(10, (current[name] || 0) + delta));
      return { ...current, [name]: next };
    });
  };

  const toggleAgent = (name: string) => {
    setTargetCounts((current) => {
      if (modelSelectionMode === 'single') {
        // Single mode: only one agent at a time, count is 0 or 1
        const isCurrentlySelected = current[name] === 1;
        if (isCurrentlySelected) {
          // Deselect
          return { ...current, [name]: 0 };
        } else {
          // Select this one, deselect all others
          const next: Record<string, number> = {};
          promptableList.forEach((item) => {
            next[item.name] = item.name === name ? 1 : 0;
          });
          return next;
        }
      } else {
        // Multiple mode: toggle on/off (0 or 1)
        const isCurrentlySelected = current[name] === 1;
        return { ...current, [name]: isCurrentlySelected ? 0 : 1 };
      }
    });
  };

  const generateBranchName = useCallback(async (promptText: string): Promise<{ result: SuggestBranchResponse | null; error: string | null }> => {
    if (!promptText.trim()) {
      return { result: null, error: 'Empty prompt' };
    }
    try {
      const result = await suggestBranch({ prompt: promptText });
      return { result, error: null };
    } catch (err) {
      console.error('Failed to suggest branch:', err);
      return { result: null, error: getErrorMessage(err, 'Unknown error') };
    }
  }, []);

  const validateForm = () => {
    if (spawnMode === 'resume') {
      if (totalPromptableCount === 0) {
        toastError('Please select at least one target');
        return false;
      }
      if (mode === 'fresh' && !isRemoteWithoutProvisioning && !repo) {
        toastError('Please select a repository');
        return false;
      }
      return true;
    }

    // Remote spawns don't require repo/branch - they use the remote host's workspace
    const isRemote = environment.type === 'remote';

    if (!isRemote) {
      if (!repo) {
        toastError('Please select a repository');
        return false;
      }
      if (repo === '__new__' && !newRepoName.trim()) {
        toastError('Please enter a repository name');
        return false;
      }
      if (mode === 'fresh' && !branchSuggestTarget && !branch.trim()) {
        toastError('Please enter a branch name');
        return false;
      }
    }
    if (spawnMode === 'promptable') {
      if (totalPromptableCount === 0) {
        toastError('Please select at least one target');
        return false;
      }
      if (!prompt.trim()) {
        toastError('Please enter a prompt');
        return false;
      }
    }
    if (spawnMode === 'command' && !selectedCommand) {
      toastError('Please select a command');
      return false;
    }
    return true;
  };

  // Handle slash command selection - switches to command mode
  const handleSlashCommandSelect = (command: string) => {
    if (command === '/resume') {
      setSpawnMode('resume');
      setPrompt('');
      setNickname('');
    } else {
      // It's a command target - switch to command mode and pre-select it
      setSelectedCommand(command);
      setSpawnMode('command');
      setNickname('');
    }
  };

  // Handle "Prompt" button - return to promptable mode
  const handlePromptMode = () => {
    setSpawnMode('promptable');
    setSelectedCommand('');
  };

  const handleEngage = async () => {
    if (!validateForm()) return;

    const selectedTargets: Record<string, number> = {};
    if (spawnMode === 'command') {
      selectedTargets[selectedCommand] = 1;
    } else {
      Object.entries(targetCounts).forEach(([name, count]) => {
        if (count > 0) selectedTargets[name] = count;
      });
    }

    const actualRepo = repo === '__new__' ? `local:${newRepoName.trim()}` : repo;
    let actualBranch = branch;
    let actualNickname = nickname;

    // Fresh mode: need to determine branch
    if (mode === 'fresh') {
      if (branch.trim()) {
        // User provided a branch name — use it directly, skip suggestion
        actualBranch = branch.trim();
        actualNickname = nickname;
      } else if (spawnMode === 'resume') {
        // Resume mode: use default branch (no prompt to suggest from)
        actualBranch = getDefaultBranch(actualRepo);
        actualNickname = '';
      } else if (spawnMode === 'promptable' && prompt.trim() && branchSuggestTarget) {
        // Call branch suggest API
        setEngagePhase('naming');
        const { result, error } = await generateBranchName(prompt);
        if (!isMounted.current) return;
        if (result && result.branch.trim()) {
          actualBranch = result.branch;
          actualNickname = result.nickname || '';
        } else {
          // Abort — reveal branch input so user can provide one
          setShowBranchInput(true);
          setEngagePhase('idle');
          toastError(`Branch suggestion failed: ${error}. Please enter a branch name.`);
          return;
        }
      } else {
        // No suggestion available and no branch provided — shouldn't reach here
        // due to validateForm, but guard anyway
        actualBranch = getDefaultBranch(actualRepo);
        actualNickname = '';
      }
    }

    if (spawnMode === 'command') {
      actualNickname = '';
    }

    // Spawn
    setEngagePhase('spawning');

    try {
      const response = await spawnSessions({
        repo: actualRepo,
        branch: actualBranch,
        prompt: spawnMode === 'promptable' ? prompt : '',
        nickname: actualNickname.trim(),
        targets: selectedTargets,
        workspace_id: prefillWorkspaceId || '',
        resume: spawnMode === 'resume',
        remote_flavor_id: environment.type === 'remote' ? environment.flavorId : undefined,
      });
      const hasSuccess = response.some(r => !r.error);
      if (!hasSuccess) {
        // All spawns failed — stay on form, show errors as toasts
        const errors = response.filter(r => r.error).map(r => r.error);
        const unique = [...new Set(errors)];
        toastError(`Spawn failed: ${unique.join('; ')}`);
        setEngagePhase('idle');
        return;
      }
      if (hasSuccess) {
        clearSpawnDraft(urlWorkspaceId);
        saveLastRepo(actualRepo);
        // Only save promptable target counts — command targets would overwrite agent selection
        if (spawnMode === 'promptable') {
          saveLastTargetCounts(selectedTargets);
          saveLastModelSelectionMode(modelSelectionMode);
        }
      }

      // Navigate directly to the spawned session(s) instead of showing results interstitial
      const successfulResults = response.filter((r) => !r.error);
      if (successfulResults.length === 1 && successfulResults[0].session_id) {
        // Single session: navigate directly to it
        setPendingNavigation({ type: 'session', id: successfulResults[0].session_id });
      } else if (successfulResults.length > 0) {
        // Multiple sessions: navigate to the workspace (shows first session)
        const workspaceId = successfulResults[0].workspace_id;
        if (workspaceId) {
          setPendingNavigation({ type: 'workspace', id: workspaceId });
        }
      }
      setEngagePhase('waiting');

      const workspaceIds = [...new Set(response.filter(r => !r.error).map(r => r.workspace_id).filter(Boolean))] as string[];
      let expanded: Record<string, boolean> = {};
      try {
        expanded = JSON.parse(localStorage.getItem(WORKSPACE_EXPANDED_KEY) || '{}') as Record<string, boolean>;
      } catch (err) {
        console.warn('Failed to parse workspace expanded state:', err);
        expanded = {};
      }
      let changed = false;
      workspaceIds.forEach(id => {
        if (expanded[id] !== true) {
          expanded[id] = true;
          changed = true;
        }
      });
      if (changed) {
        localStorage.setItem(WORKSPACE_EXPANDED_KEY, JSON.stringify(expanded));
      }
    } catch (err) {
      const errorMsg = getErrorMessage(err, 'Unknown error');
      toastError(`Failed to spawn: ${errorMsg}`);
      setEngagePhase('idle');
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

  if (configError) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Failed to load config</h3>
        <p className="empty-state__description">{configError}</p>
      </div>
    );
  }

  // Form screen
  return (
    <>
      {currentWorkspace && (
        <>
          <WorkspaceHeader workspace={currentWorkspace} />
          <SessionTabs sessions={currentWorkspace.sessions || []} workspace={currentWorkspace} activeSpawnTab />
        </>
      )}
      {!currentWorkspace && (
        <div className="app-header">
          <div className="app-header__info">
            <h1 className="app-header__meta">What do you want me to do?</h1>
          </div>
        </div>
      )}

      <div className="spawn-content">

      {/* Environment selection for fresh spawns */}
      {mode === 'fresh' && (
        <RemoteHostSelector
          value={environment}
          onChange={setEnvironment}
          disabled={engagePhase !== 'idle'}
        />
      )}

      {/* Prompt/Command area - first */}
      {spawnMode === 'promptable' ? (
        <>
          <div className="card" style={{ marginBottom: 'var(--spacing-md)', padding: '0', overflow: 'visible' }}>
            <PromptTextarea
              value={prompt}
              onChange={setPrompt}
              placeholder="Describe the task you want the targets to work on... (Type / for commands, ⌘↩ to submit)"
              commands={[...commandTargets.map(t => t.name), '/resume']}
              onSelectCommand={handleSlashCommandSelect}
              onSubmit={handleEngage}
            />
          </div>
        </>
      ) : spawnMode === 'command' ? (
        <div className="card" style={{ marginBottom: 'var(--spacing-md)', padding: 'var(--spacing-md)' }}>
          <label htmlFor="command" className="form-group__label">Command</label>
          <select
            id="command"
            className="select"
            required
            value={selectedCommand}
            onChange={(event) => setSelectedCommand(event.target.value)}
          >
            <option value="">Select command...</option>
            {commandTargets.map((cmd) => (
              <option key={cmd.name} value={cmd.name}>
                {cmd.name}
              </option>
            ))}
          </select>
        </div>
      ) : null}

      {spawnMode === 'resume' ? (
        <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: 'var(--spacing-md)', marginBottom: 'var(--spacing-md)', alignItems: 'center' }}>
          <label htmlFor="resume-target" className="form-group__label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>Target</label>
          <select
            id="resume-target"
            className="select"
            value={promptableList.find(item => (targetCounts[item.name] || 0) > 0)?.name || ''}
            onChange={(e) => {
              const name = e.target.value;
              const next: Record<string, number> = {};
              promptableList.forEach((item) => {
                next[item.name] = item.name === name ? 1 : 0;
              });
              setTargetCounts(next);
            }}
          >
            <option value="">Select agent...</option>
            {promptableList.map((item) => (
              <option key={item.name} value={item.name}>{item.label}</option>
            ))}
          </select>

          {mode === 'fresh' && !isRemoteWithoutProvisioning && (
            <>
              <label htmlFor="repo" className="form-group__label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>Repository</label>
              <select
                id="repo"
                className="select"
                required
                value={repo}
                onChange={(event) => {
                  setRepo(event.target.value);
                }}
              >
                <option value="">Select repository...</option>
                {repos.map((item) => (
                  <option key={item.url} value={item.url}>{item.name}</option>
                ))}
              </select>

              <label htmlFor="branch" className="form-group__label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>Branch</label>
              <input
                type="text"
                id="branch"
                className="input"
                value={branch}
                onChange={(event) => setBranch(event.target.value)}
                placeholder="Leave blank for default branch"
              />
            </>
          )}
        </div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: 'var(--spacing-md)', marginBottom: 'var(--spacing-md)', alignItems: 'center' }}>

        {/* Agent selection */}
        {spawnMode === 'promptable' && promptableList.length > 0 && (<>
              {/* Mode selector - dropdown */}
              <select
                className="select"
                value={modelSelectionMode}
                onChange={(e) => setModelSelectionMode(e.target.value as 'single' | 'multiple' | 'advanced')}
                style={{ width: 'auto' }}
              >
                <option value="single">Single Agent</option>
                <option value="multiple">Multiple Agents</option>
                <option value="advanced">Advanced</option>
              </select>

              {modelSelectionMode === 'single' && (
                <select
                  className="select"
                  value={promptableList.find(item => (targetCounts[item.name] || 0) > 0)?.name || ''}
                  onChange={(e) => {
                    const name = e.target.value;
                    if (name) {
                      toggleAgent(name);
                    } else {
                      // Deselect all when picking "Select agent..."
                      const selected = promptableList.find(item => (targetCounts[item.name] || 0) > 0);
                      if (selected) toggleAgent(selected.name);
                    }
                  }}
                >
                  <option value="">Select agent...</option>
                  {promptableList.map((item) => (
                    <option key={item.name} value={item.name}>{item.label}</option>
                  ))}
                </select>
              )}

              {modelSelectionMode === 'multiple' && (
                <div style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
                  gap: 'var(--spacing-sm)',
                }}>
                    {promptableList.map((item) => {
                      const isSelected = (targetCounts[item.name] || 0) > 0;
                      return (
                        <button
                          key={item.name}
                          type="button"
                          className={`btn${isSelected ? ' btn--primary' : ''}`}
                          onClick={() => toggleAgent(item.name)}
                          style={{
                            height: 'auto',
                            padding: 'var(--spacing-sm)',
                            textAlign: 'left',
                            whiteSpace: 'nowrap',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                          }}
                        >
                          {item.label}
                        </button>
                      );
                    })}
                  </div>
                )}

                {modelSelectionMode === 'advanced' && (
                  <div style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                    gap: 'var(--spacing-sm)',
                  }}>
                    {promptableList.map((item) => {
                      const count = targetCounts[item.name] || 0;
                      const isSelected = count > 0;
                      return (
                        <div
                          key={item.name}
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 'var(--spacing-xs)',
                            border: '1px solid var(--color-border)',
                            borderRadius: 'var(--radius-sm)',
                            padding: 'var(--spacing-xs)',
                            backgroundColor: isSelected ? 'var(--color-accent)' : 'var(--color-surface-alt)',
                          }}
                        >
                          <span style={{ fontSize: '0.875rem', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            {item.label}
                          </span>
                          <button
                            type="button"
                            className="btn"
                            onClick={() => updateTargetCount(item.name, -1)}
                            disabled={count === 0}
                            style={{
                              padding: '2px 8px',
                              fontSize: '0.75rem',
                              minHeight: '24px',
                              minWidth: '28px',
                              lineHeight: '1',
                              backgroundColor: isSelected ? 'rgba(255,255,255,0.2)' : 'var(--color-surface)',
                              color: isSelected ? 'white' : 'var(--color-text)',
                              border: 'none',
                              borderRadius: 'var(--radius-sm)'
                            }}
                          >
                            −
                          </button>
                          <span style={{ fontSize: '0.875rem', minWidth: '16px', textAlign: 'center' }}>
                            {count}
                          </span>
                          <button
                            type="button"
                            className="btn"
                            onClick={() => updateTargetCount(item.name, 1)}
                            style={{
                              padding: '2px 8px',
                              fontSize: '0.75rem',
                              minHeight: '24px',
                              minWidth: '28px',
                              lineHeight: '1',
                              backgroundColor: isSelected ? 'rgba(255,255,255,0.2)' : 'var(--color-surface)',
                              color: isSelected ? 'white' : 'var(--color-text)',
                              border: 'none',
                              borderRadius: 'var(--radius-sm)'
                            }}
                          >
                            +
                          </button>
                        </div>
                      );
                    })}
                  </div>
                )}
            </>
        )}

        {/* Repository (hidden when not editable or remote without provisioning) */}
        {mode === 'fresh' && !isRemoteWithoutProvisioning && (
          <>
            <label htmlFor="repo" className="form-group__label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>Repository</label>
            <div>
              <select
                id="repo"
                className="select"
                required
                value={repo}
                onChange={(event) => {
                  setRepo(event.target.value);
                  if (event.target.value !== '__new__') {
                    setNewRepoName('');
                  }
                }}
              >
                <option value="">Select repository...</option>
                {repos.map((item) => (
                  <option key={item.url} value={item.url}>{item.name}</option>
                ))}
                <option value="__new__">+ Create New Repository</option>
              </select>

              {repo === '__new__' && (
                <div style={{ marginTop: 'var(--spacing-sm)' }}>
                  <input
                    type="text"
                    id="newRepoName"
                    className="input"
                    value={newRepoName}
                    onChange={(event) => setNewRepoName(event.target.value)}
                    placeholder="Repository name"
                    required
                  />
                </div>
              )}
            </div>
          </>
        )}

        {/* Branch (shown on suggestion failure or when suggestion is disabled) */}
        {mode === 'fresh' && !isRemoteWithoutProvisioning && showBranchInput && (
          <>
            <label htmlFor="branch" className="form-group__label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>Branch</label>
            <input
              type="text"
              id="branch"
              className="input"
              value={branch}
              onChange={(event) => setBranch(event.target.value)}
              placeholder="e.g. feature/my-branch"
            />
          </>
        )}

        </div>
      )}

      <div style={{ marginTop: 'var(--spacing-lg)', display: 'flex', gap: 'var(--spacing-sm)', justifyContent: 'flex-end' }}>
        {(spawnMode === 'command' || spawnMode === 'resume') && (
          <button className="btn" onClick={handlePromptMode} disabled={engagePhase !== 'idle'}>
            Prompt
          </button>
        )}
        <button
          className="btn btn--primary"
          onClick={handleEngage}
          disabled={engagePhase !== 'idle'}
          style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}
        >
          {engagePhase === 'naming' ? (
            <>
              <span className="spinner spinner--small"></span>
              Naming branch...
            </>
          ) : engagePhase === 'spawning' ? (
            <>
              <span className="spinner spinner--small"></span>
              Spawning...
            </>
          ) : engagePhase === 'waiting' ? (
            <>
              <span className="spinner spinner--small"></span>
              Downloading session...
            </>
          ) : 'Engage'}
        </button>
      </div>
      </div>
    </>
  );
}
