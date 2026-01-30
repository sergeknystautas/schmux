import React, { useEffect, useMemo, useRef, useState, useCallback } from 'react';
import { Link, useSearchParams, useNavigate, useLocation } from 'react-router-dom';
import { getConfig, spawnSessions, getErrorMessage, suggestBranch, checkBranchConflict } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useRequireConfig, useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import type { Model, RepoResponse, RunTargetResponse, SpawnResult } from '../lib/types';
import { WORKSPACE_EXPANDED_KEY } from '../lib/constants';

const PROMPT_TEXTAREA_STYLE: React.CSSProperties = {
  width: '100%',
  height: '420px',
  resize: 'vertical',
  border: 'none',
  outline: 'none',
  boxShadow: 'none',
  padding: 'var(--spacing-md)',
  borderRadius: 'var(--radius-lg) var(--radius-lg) 0 0',
};


// ============================================================================
// Layer 2: Session Storage Draft (Active Draft)
// Per-tab, cleared on successful spawn
// ============================================================================

interface SpawnDraft {
  prompt: string;
  spawnMode: 'promptable' | 'command';
  selectedCommand: string;
  targetCounts: Record<string, number>;
  modelSelectionMode: 'single' | 'multiple' | 'advanced';
  // Only for fresh spawns (no workspace_id)
  repo?: string;
  newRepoName?: string;
  // Remembers which screen user was on: 'write' or 'review'
  stage?: 'write' | 'review';
  // Branch and nickname from review screen
  branch?: string;
  nickname?: string;
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
  const [screen, setScreen] = useState<'write' | 'review'>('write');
  const [repos, setRepos] = useState<RepoResponse[]>([]);
  const [promptableTargets, setPromptableTargets] = useState<RunTargetResponse[]>([]);
  const [commandTargets, setCommandTargets] = useState<RunTargetResponse[]>([]);
  const [models, setModels] = useState<Model[]>([]);
  const [selectedCommand, setSelectedCommand] = useState('');
  const [spawnMode, setSpawnMode] = useState<'promptable' | 'command'>('promptable');
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('');
  const [newRepoName, setNewRepoName] = useState('');
  const [prompt, setPrompt] = useState('');
  const [nickname, setNickname] = useState('');
  const [reviewing, setReviewing] = useState(false);
  const [prefillWorkspaceId, setPrefillWorkspaceId] = useState('');
  const [resolvedWorkspaceId, setResolvedWorkspaceId] = useState('');
  const [branchConflict, setBranchConflict] = useState<{ conflict: boolean; workspace_id?: string } | null>(null);
  const [checkingConflict, setCheckingConflict] = useState(false);
  const [conflictCheckError, setConflictCheckError] = useState(false);
  const [sourceCodeManagement, setSourceCodeManager] = useState('git-worktree');
  const skipNextPersist = useRef(false);
  const [loading, setLoading] = useState(true);
  const [configError, setConfigError] = useState('');
  const [results, setResults] = useState<SpawnResult[] | null>(null);
  const [spawning, setSpawning] = useState(false);
  const [searchParams] = useSearchParams();
  const { error: toastError } = useToast();
  const { workspaces, loading: sessionsLoading, waitForSession } = useSessions();
  const { config, getRepoName } = useConfig();

  const location = useLocation();

  // Spawn page mode: determined once on mount (see docs/sessions.md)
  const [mode] = useState<'workspace' | 'prefilled' | 'fresh'>(() => {
    const wsId = searchParams.get('workspace_id');
    if (wsId) return 'workspace';
    if (location.state?.repo && location.state?.branch) return 'prefilled';
    return 'fresh';
  });
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
        setRepos(cfg.repos || []);
        setSourceCodeManager(cfg.source_code_management || 'git-worktree');

        const modelBaseTools = new Set((cfg.models || []).map((model) => model.base_tool));
        const promptableItems = (cfg.run_targets || []).filter(t => {
          if (t.type !== 'promptable') {
            return false;
          }
          if (t.source === 'detected' && modelBaseTools.has(t.name)) {
            return false;
          }
          return true;
        });
        const commandItems = (cfg.run_targets || []).filter(t => t.type === 'command');
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

  // Initialize form fields based on mode (runs once; see docs/sessions.md)
  // Three-layer waterfall: Mode Logic → sessionStorage → localStorage → Default
  const urlWorkspaceId = searchParams.get('workspace_id');
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
      // branch, nickname: restore from review screen
      if (draft?.branch) setBranch(draft.branch);
      if (draft?.nickname) setNickname(draft.nickname);
      // stage: restore which screen user was on
      if (draft?.stage) setScreen(draft.stage);
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
    // Don't save if we're on results screen (spawn succeeded)
    if (results) return;

    const draft: SpawnDraft = {
      prompt,
      spawnMode,
      selectedCommand,
      targetCounts,
      modelSelectionMode,
      stage: screen,
      branch,
      nickname,
    };
    // Only save repo/newRepoName for fresh spawns
    if (!urlWorkspaceId) {
      draft.repo = repo;
      draft.newRepoName = newRepoName;
    }
    saveSpawnDraft(urlWorkspaceId, draft);
  }, [prompt, spawnMode, selectedCommand, targetCounts, modelSelectionMode, repo, newRepoName, urlWorkspaceId, results, screen, branch, nickname]);

  const totalPromptableCount = useMemo(() => {
    return Object.values(targetCounts).reduce((sum, count) => sum + count, 0);
  }, [targetCounts]);

  // Auto-navigate to first successful session when spawning into existing workspace
  useEffect(() => {
    if (!results) return;
    const successfulResults = results.filter((r) => !r.error);
    const errorCount = results.filter((r) => r.error).length;

    if (inExistingWorkspace && successfulResults.length > 0 && errorCount === 0) {
      const sessionId = successfulResults[0].session_id;
      if (sessionId) {
        // Wait for session to appear in the list before navigating
        const doNavigate = async () => {
          await waitForSession(sessionId);
          navigate(`/sessions/${sessionId}`);
        };
        doNavigate();
      }
    }
  }, [results, inExistingWorkspace, navigate, waitForSession]);

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

  // Check for branch conflicts when branch changes (worktree mode only, new workspace only)
  useEffect(() => {
    // Only check if:
    // 1. Not spawning into existing workspace
    // 2. Using worktrees
    // 3. Have both repo and branch set
    // 4. Not creating a new repo
    if (inExistingWorkspace || sourceCodeManagement !== 'git-worktree' || !repo || !branch || repo === '__new__') {
      setBranchConflict(null);
      setConflictCheckError(false);
      return;
    }

    let cancelled = false;
    const check = async () => {
      setCheckingConflict(true);
      setConflictCheckError(false);
      try {
        const result = await checkBranchConflict(repo, branch);
        if (!cancelled) {
          setBranchConflict(result);
        }
      } catch (err) {
        console.error('Failed to check branch conflict:', err);
        if (!cancelled) {
          setBranchConflict(null);
          setConflictCheckError(true);
        }
      } finally {
        if (!cancelled) {
          setCheckingConflict(false);
        }
      }
    };

    // Debounce the check
    const timeout = setTimeout(check, 300);
    return () => {
      cancelled = true;
      clearTimeout(timeout);
    };
  }, [repo, branch, inExistingWorkspace, sourceCodeManagement]);

  const generateBranchName = useCallback(async (promptText: string) => {
    if (!promptText.trim()) {
      return null;
    }
    try {
      const result = await suggestBranch({ prompt: promptText });
      return result;
    } catch (err) {
      console.error('Failed to suggest branch:', err);
      return null;
    }
  }, []);

  const validateForm = () => {
    if (!repo) {
      toastError('Please select a repository');
      return false;
    }
    if (repo === '__new__' && !newRepoName.trim()) {
      toastError('Please enter a repository name');
      return false;
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

  const handleNext = () => {
    if (!validateForm()) return;

    if (spawnMode === 'command') {
      setNickname('');
    }

    // Workspace/prefilled: branch already set, skip suggestion
    if (mode !== 'fresh') {
      setScreen('review');
      return;
    }

    // Fresh mode: suggest branch or default to 'main'
    if (spawnMode !== 'promptable' || !prompt.trim()) {
      setBranch('main');
      setScreen('review');
      return;
    }

    if (!branchSuggestTarget) {
      setBranch('main');
      setScreen('review');
      return;
    }

    setReviewing(true);
    generateBranchName(prompt).then((result) => {
      if (!isMounted.current) return;
      if (result) {
        setBranch(result.branch || 'main');
        setNickname(result.nickname || '');
      } else {
        toastError('Branch suggestion failed. Using "main".');
        setBranch('main');
        setNickname('');
      }
      setScreen('review');
      setReviewing(false);
    });
  };

  const handleBack = () => {
    setScreen('write');
  };

  const handleSpawn = async () => {
    const selectedTargets: Record<string, number> = {};

    if (spawnMode === 'command') {
      selectedTargets[selectedCommand] = 1;
    } else {
      Object.entries(targetCounts).forEach(([name, count]) => {
        if (count > 0) selectedTargets[name] = count;
      });
    }

    const actualRepo = repo === '__new__' ? `local:${newRepoName.trim()}` : repo;
    const actualBranch = inExistingWorkspace ? branch : (branch || 'main');

    setSpawning(true);

    try {
      const response = await spawnSessions({
        repo: actualRepo,
        branch: actualBranch,
        prompt: spawnMode === 'promptable' ? prompt : '',
        nickname: nickname.trim(),
        targets: selectedTargets,
        workspace_id: prefillWorkspaceId || ''
      });
      setResults(response);
      // Clear draft and write-back to localStorage if at least one spawn succeeded
      const hasSuccess = response.some(r => !r.error);
      if (hasSuccess) {
        clearSpawnDraft(urlWorkspaceId);
        // Write-back to localStorage (long-term memory)
        saveLastRepo(actualRepo);
        saveLastTargetCounts(selectedTargets);
        saveLastModelSelectionMode(modelSelectionMode);
      }

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
      // Check for server-side branch conflict error (race condition catch)
      if (errorMsg.startsWith('branch_conflict:')) {
        // Parse workspace ID from message: "branch_conflict: branch "x" is already in use by workspace "y""
        const match = errorMsg.match(/workspace "([^"]+)"/);
        setBranchConflict({
          conflict: true,
          workspace_id: match ? match[1] : undefined
        });
        toastError('Branch is already in use by another workspace');
      } else {
        toastError(`Failed to spawn: ${errorMsg}`);
      }
    } finally {
      setSpawning(false);
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

  if (results) {
    const successCount = results.filter((r) => !r.error).length;
    const errorCount = results.filter((r) => r.error).length;
    const successfulResults = results.filter((r) => !r.error);

    // If we're auto-navigating, show loading
    if (inExistingWorkspace && successfulResults.length > 0 && errorCount === 0) {
      return (
        <div className="loading-state">
          <div className="spinner"></div>
          <span>Opening session...</span>
        </div>
      );
    }

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
              <h1 className="app-header__meta">Spawn Sessions</h1>
            </div>
          </div>
        )}
        <div className="spawn-content">
          <h2 style={{ marginBottom: 'var(--spacing-lg)' }}>Results</h2>
          {successCount > 0 ? (
            <div className="results-panel" style={{ marginBottom: 'var(--spacing-lg)' }}>
              <div className="results-panel__title">Successfully spawned {successCount} session(s)</div>
              {successfulResults.map((r, index) => (
                <div className="results-panel__item results-panel__item--success" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }} key={r.session_id}>
                  <div>
                    <span className="badge badge--primary" style={{ marginRight: 'var(--spacing-sm)' }}>{index + 1}</span>
                    <span className="mono">{r.workspace_id}</span>
                    <span style={{ color: 'var(--color-text-muted)', margin: '0 var(--spacing-sm)' }}>·</span>
                    <span>{r.target}</span>
                    {r.nickname && <span style={{ color: 'var(--color-text-muted)', margin: '0 var(--spacing-sm)' }}>·</span>}
                    {r.nickname && <span style={{ fontStyle: 'italic', color: 'var(--color-text-muted)' }}>{r.nickname}</span>}
                  </div>
                  <Link to={`/sessions/${r.session_id}`} className="btn btn--sm">View</Link>
                </div>
              ))}
            </div>
          ) : null}
          {errorCount > 0 ? (
            <div className="results-panel">
              <div className="results-panel__title text-error">{errorCount} error(s)</div>
              {results.filter((r) => r.error).map((r) => (
                <div className="results-panel__item results-panel__item--error" key={`${r.target}-${r.error}`}>
                  <div><strong>{r.target}:</strong> {r.error}</div>
                  {r.prompt && (
                    <div style={{ marginTop: 'var(--spacing-sm)' }}>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', marginBottom: 'var(--spacing-xs)' }}>Prompt:</div>
                      <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontSize: '0.875rem' }}>{r.prompt}</div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          ) : null}
          <div style={{ marginTop: 'var(--spacing-lg)' }}>
            <Link to="/" className="btn btn--primary">Back to Home</Link>
          </div>
        </div>
      </>
    );
  }

  // Review screen
  if (screen === 'review') {
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
              <h1 className="app-header__meta">Spawn Sessions</h1>
            </div>
          </div>
        )}

        <div className="spawn-content">
        <div className="card">
          <div className="card__body">
            <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Repository</h3>
            <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
              <span className="metadata-field__value">
                {repo === '__new__' ? `New repository: ${newRepoName}` : getRepoName(repo)}
              </span>
            </div>

            {spawnMode === 'promptable' && (
              <>
                <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Prompt</h3>
                <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <span
                    className="metadata-field__value"
                    style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
                  >
                    {prompt}
                  </span>
                </div>

                <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Targets</h3>
                <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <span className="metadata-field__value">
                    {promptableList
                      .filter((item) => (targetCounts[item.name] || 0) > 0)
                      .map((item) => {
                        const count = targetCounts[item.name] || 0;
                        return `${item.label} ×${count}`;
                      })
                      .join(', ')}
                  </span>
                </div>
              </>
            )}

            {spawnMode === 'command' && (
              <>
                <h3 style={{ marginBottom: 'var(--spacing-sm)' }}>Command</h3>
                <div className="metadata-field" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <span className="metadata-field__value">{selectedCommand}</span>
                </div>
              </>
            )}

            <div className="form-group">
              <label className="form-group__label">Branch</label>
              {mode !== 'fresh' ? (
                <div className="metadata-field">
                  <span className="metadata-field__value">{branch}</span>
                </div>
              ) : (
                <>
                  <input
                    type="text"
                    className={`input${branchConflict?.conflict ? ' input--error' : ''}`}
                    value={branch}
                    onChange={(event) => setBranch(event.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter' && !branchConflict?.conflict) handleSpawn(); }}
                    required
                  />
                  {checkingConflict && (
                    <p className="form-group__hint" style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)' }}>
                      <span className="spinner spinner--small"></span>
                      Checking branch availability...
                    </p>
                  )}
                  {branchConflict?.conflict && (
                    <p className="form-group__error">
                      Branch "{branch}" is already in use by workspace "{branchConflict.workspace_id}".
                      Use a different branch name or spawn into the existing workspace.
                    </p>
                  )}
                  {conflictCheckError && (
                    <p className="form-group__error">
                      Failed to verify branch availability. Cannot spawn in worktree mode until check succeeds.
                    </p>
                  )}
                </>
              )}
            </div>

            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="form-group__label">Nickname</label>
              {inExistingWorkspace ? (
                <div className="metadata-field">
                  <span className="metadata-field__value">{nickname || '—'}</span>
                </div>
              ) : (
                <>
                  <input
                    type="text"
                    className="input"
                    placeholder="e.g., 'Fix login bug', 'Refactor auth flow'"
                    maxLength={100}
                    value={nickname}
                    onChange={(event) => setNickname(event.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter') handleSpawn(); }}
                  />
                  {!branchSuggestTarget && (
                    <div className="banner banner--info" style={{ margin: 'var(--spacing-sm) 0', fontSize: '0.875rem' }}>
                      Auto-suggested branch names are disabled. Enable in config to use suggestions.
                    </div>
                  )}
                </>
              )}
            </div>
          </div>
        </div>

        <div style={{ marginTop: 'var(--spacing-lg)', display: 'flex', gap: 'var(--spacing-sm)' }}>
          <button className="btn" onClick={handleBack} disabled={spawning}>
            Back
          </button>
          <button
            className="btn btn--primary"
            onClick={handleSpawn}
            disabled={spawning || checkingConflict || branchConflict?.conflict || conflictCheckError}
            style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}
          >
            {spawning ? (
              <>
                <span className="spinner spinner--small"></span>
                Spawning...
              </>
            ) : 'Spawn'}
          </button>
        </div>
        </div>
      </>
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
            <h1 className="app-header__meta">Spawn Sessions</h1>
          </div>
        </div>
      )}

      <div className="spawn-content">

      {/* Mode + Repository on same line */}
      <div className="card" style={{ marginBottom: 'var(--spacing-md)' }}>
        <div className="card__body" style={{ display: 'flex', gap: 'var(--spacing-md)', alignItems: 'flex-end' }}>
          <div style={{ flex: '0 0 auto' }}>
            <label className="form-group__label">Mode</label>
            <div className="button-group">
              <button
                type="button"
                className={`btn${spawnMode === 'promptable' ? ' btn--primary' : ''}`}
                onClick={() => setSpawnMode('promptable')}
              >
                Promptable
              </button>
              <button
                type="button"
                className={`btn${spawnMode === 'command' ? ' btn--primary' : ''}`}
                onClick={() => {
                  setSpawnMode('command');
                  setNickname('');
                }}
                disabled={commandTargets.length === 0}
              >
                Command
              </button>
            </div>
          </div>

          <div style={{ flex: 1 }}>
            <label htmlFor="repo" className="form-group__label">Repository</label>
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
              disabled={mode !== 'fresh'}
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
                  disabled={mode !== 'fresh'}
                />
              </div>
            )}
          </div>
        </div>

      </div>

      {/* Prompt area - big and centered */}
      <div className="card" style={{ marginBottom: 'var(--spacing-md)', padding: '0' }}>
        {spawnMode === 'promptable' ? (
          <textarea
            className="textarea"
            style={PROMPT_TEXTAREA_STYLE}
            placeholder="Describe the task you want the targets to work on..."
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
          />
        ) : (
          <div style={{ padding: 'var(--spacing-md)' }}>
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
        )}
      </div>

      {/* Agent selection - mode selector + grid layout */}
      {spawnMode === 'promptable' && promptableList.length > 0 && (
        <div className="card" style={{ marginBottom: 'var(--spacing-md)' }}>
          <div className="card__body">
            <div style={{ display: 'flex', gap: 'var(--spacing-md)' }}>
              {/* Mode selector - left column */}
              <div style={{ flex: '0 0 auto', display: 'flex', flexDirection: 'column', gap: 'var(--spacing-xs)' }}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-xs)' }}>
                  <button
                    type="button"
                    className={`btn${modelSelectionMode === 'single' ? ' btn--primary' : ''}`}
                    onClick={() => setModelSelectionMode('single')}
                    style={{ justifyContent: 'flex-start' }}
                  >
                    Single
                  </button>
                  <button
                    type="button"
                    className={`btn${modelSelectionMode === 'multiple' ? ' btn--primary' : ''}`}
                    onClick={() => setModelSelectionMode('multiple')}
                    style={{ justifyContent: 'flex-start' }}
                  >
                    Multiple
                  </button>
                  <button
                    type="button"
                    className={`btn${modelSelectionMode === 'advanced' ? ' btn--primary' : ''}`}
                    onClick={() => setModelSelectionMode('advanced')}
                    style={{ justifyContent: 'flex-start' }}
                  >
                    Advanced
                  </button>
                </div>
              </div>

              {/* Agent grid - right column */}
              <div style={{ flex: 1 }}>
                <div style={{
                  display: 'grid',
                  gridTemplateColumns: modelSelectionMode === 'advanced'
                    ? 'repeat(auto-fill, minmax(200px, 1fr))'
                    : 'repeat(auto-fill, minmax(140px, 1fr))',
                  gap: 'var(--spacing-sm)',
                }}>
                  {promptableList.map((item) => {
                    const count = targetCounts[item.name] || 0;
                    const isSelected = count > 0;

                    // Advanced mode: show counter buttons
                    if (modelSelectionMode === 'advanced') {
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
                    }

                    // Single/Multiple mode: toggle button
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
              </div>
            </div>
          </div>
        </div>
      )}

      <div style={{ marginTop: 'var(--spacing-lg)' }}>
        <button
          className="btn btn--primary"
          onClick={handleNext}
          disabled={reviewing}
          style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}
        >
          {reviewing ? (
            <>
              <span className="spinner spinner--small"></span>
              Reviewing...
            </>
          ) : 'Review'}
        </button>
      </div>
      </div>
    </>
  );
}
