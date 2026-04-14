import { useEffect, useMemo, useRef, useState, useCallback } from 'react';
import { useSearchParams, useNavigate, useLocation } from 'react-router-dom';
import {
  getConfig,
  spawnSessions,
  getErrorMessage,
  suggestBranch,
  getPersonas,
  getStyles,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { usePendingNavigation } from '../lib/navigation';
import { getQuickLaunchItems } from '../lib/quicklaunch';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import PromptTextarea from '../components/PromptTextarea';
import Tooltip from '../components/Tooltip';
import RemoteHostSelector, { type EnvironmentSelection } from '../components/RemoteHostSelector';
import { getSpawnEntries, getPromptHistory } from '../lib/spawn-api';
import type { AutocompleteItem } from '../components/PromptAutocomplete';
import type { Model, RepoResponse, SpawnResult, SuggestBranchResponse } from '../lib/types';
import type { Persona, SpawnEntry, PromptHistoryEntry, Style } from '../lib/types.generated';
import { WORKSPACE_EXPANDED_KEY } from '../lib/constants';

// ============================================================================
// Layer 2: Session Storage Draft (Active Draft)
// Per-tab, cleared on successful spawn
// ============================================================================

interface SpawnDraft {
  prompt: string;
  targetCounts: Record<string, number>;
  modelSelectionMode: 'single' | 'multiple' | 'advanced';
  // Only for fresh spawns (no workspace_id)
  repo?: string;
  newRepoName?: string;
  // Only for workspace mode
  createBranch?: boolean;
  imageAttachments?: string[]; // base64-encoded PNGs
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
  const [repos, setRepos] = useState<RepoResponse[]>([]);
  const [commandTargets, setCommandTargets] = useState<{ name: string; command: string }[]>([]);
  const [models, setModels] = useState<Model[]>([]);
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('');
  const [newRepoName, setNewRepoName] = useState('');
  const [prompt, setPrompt] = useState('');
  const [nickname, setNickname] = useState('');
  const [engagePhase, setEngagePhase] = useState<'idle' | 'naming' | 'spawning' | 'waiting'>(
    'idle'
  );
  const [showBranchInput, setShowBranchInput] = useState(false);
  const [createBranch, setCreateBranch] = useState(false);
  const [prefillWorkspaceId, setPrefillWorkspaceId] = useState('');
  const [resolvedWorkspaceId, setResolvedWorkspaceId] = useState('');
  const [environment, setEnvironment] = useState<EnvironmentSelection>({ type: 'local' });
  const skipNextPersist = useRef(false);
  const [loading, setLoading] = useState(true);
  const [configError, setConfigError] = useState('');
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [selectedPersonaId, setSelectedPersonaId] = useState('');
  const [styles, setStyles] = useState<Style[]>([]);
  const [selectedStyleId, setSelectedStyleId] = useState('');
  const [imageAttachments, setImageAttachments] = useState<string[]>([]);
  const [shareIntent, setShareIntent] = useState(false);
  const [tmuxError, setTmuxError] = useState('');

  const [searchParams] = useSearchParams();
  const { error: toastError } = useToast();
  const { alert } = useModal();
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

  // Derive repo name for autocomplete (lookup by URL)
  const repoName = useMemo(() => repos.find((r) => r.url === repo)?.name || '', [repos, repo]);

  // Autocomplete: load spawn entries and prompt history for the selected repo
  const [acEntries, setAcEntries] = useState<SpawnEntry[]>([]);
  const [acHistory, setAcHistory] = useState<PromptHistoryEntry[]>([]);
  useEffect(() => {
    if (!repoName) return;
    getSpawnEntries(repoName)
      .then(setAcEntries)
      .catch(() => {});
    getPromptHistory(repoName)
      .then(setAcHistory)
      .catch(() => {});
  }, [repoName]);

  // handleAutocompleteSelect is defined below after targetCounts/personas state

  // Helper to get the default branch for a repo URL from precomputed map
  const getDefaultBranch = (repoUrl: string): string => {
    return defaultBranchMap.get(repoUrl) || 'main';
  };

  // Spawn page mode: derived from current URL/state
  const urlWorkspaceId = searchParams.get('workspace_id');
  const mode: 'workspace' | 'fresh' = (() => {
    if (urlWorkspaceId) return 'workspace';
    return 'fresh';
  })();
  const initialized = useRef(false);

  const isMounted = useRef(true);
  const navigate = useNavigate();
  const inExistingWorkspace = mode === 'workspace';

  // Get current workspace for header display
  const currentWorkspace = workspaces?.find((ws) => ws.id === resolvedWorkspaceId);
  const workspaceExists =
    resolvedWorkspaceId && workspaces?.some((ws) => ws.id === resolvedWorkspaceId);

  // Navigate home if workspace was disposed while on this page (in workspace mode)
  useEffect(() => {
    if (inExistingWorkspace && resolvedWorkspaceId && !workspaceExists && !sessionsLoading) {
      navigate('/');
    }
  }, [inExistingWorkspace, resolvedWorkspaceId, workspaceExists, sessionsLoading, navigate]);

  // Get branch suggest target from config
  const branchSuggestTarget = config?.branch_suggest?.target || '';

  // Remote spawns don't need repo/branch selection — the workspace is determined
  // by the flavor's workspace_path on the remote host, not by a git clone.
  const isRemoteSpawn = environment.type === 'remote';

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
        const commandItems = (cfg.run_targets || []).sort((a, b) => a.name.localeCompare(b.name));
        setCommandTargets(commandItems);
        setModels(cfg.models || []);

        // Fetch personas (only when feature is enabled)
        if (cfg.personas_enabled) {
          try {
            const personaData = await getPersonas();
            if (active) setPersonas(personaData.personas || []);
          } catch {
            // Non-fatal: personas are optional
          }
        }

        // Fetch styles (only when feature is enabled)
        if (cfg.comm_styles_enabled) {
          getStyles()
            .then((data) => {
              if (active) setStyles(data.styles || []);
            })
            .catch(() => {});
        }
      } catch (err) {
        if (!active) return;
        setConfigError(getErrorMessage(err, 'Failed to load config'));
      } finally {
        if (active) setLoading(false);
      }
    };

    load();
    return () => {
      active = false;
    };
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

      const workspace = workspaces.find((ws) => ws.id === workspaceId);
      if (workspace) {
        setRepo(workspace.repo);
        setBranch(workspace.branch);
      }
    }

    // Layer 2: sessionStorage Draft (Active Draft)
    const draft = loadSpawnDraft(urlWorkspaceId);

    // Layer 3: localStorage (Long-term Memory)
    const lastRepo = loadLastRepo();
    const lastTargetCounts = loadLastTargetCounts();
    const lastModelSelectionMode = loadLastModelSelectionMode();

    // Apply three-layer waterfall for each field
    if (mode === 'workspace') {
      if (draft?.prompt) {
        setPrompt(draft.prompt);
      }
      // modelSelectionMode: draft → localStorage → default
      setModelSelectionMode(draft?.modelSelectionMode || lastModelSelectionMode || 'single');
      // targetCounts: draft → localStorage → default
      if (draft?.targetCounts) {
        setTargetCounts(draft.targetCounts);
      } else if (lastTargetCounts) {
        setTargetCounts(lastTargetCounts);
      }
      // createBranch: draft → default (only for workspace mode)
      if (mode === 'workspace') {
        setCreateBranch(draft?.createBranch || false);
      }
    } else if (mode === 'fresh') {
      // repo: draft → localStorage → default
      setRepo(draft?.repo || lastRepo || '');
      // newRepoName: draft → default
      if (draft?.newRepoName) setNewRepoName(draft.newRepoName);
      // prompt: draft → default
      if (draft?.prompt) setPrompt(draft.prompt);
      // modelSelectionMode: draft → localStorage → default
      setModelSelectionMode(draft?.modelSelectionMode || lastModelSelectionMode || 'single');
      // targetCounts: draft → localStorage → default
      if (draft?.targetCounts) {
        setTargetCounts(draft.targetCounts);
      } else if (lastTargetCounts) {
        setTargetCounts(lastTargetCounts);
      }

      // Override with location.state if navigating from Add Repository flow
      if (location.state?.repo) setRepo(location.state.repo);
      if (location.state?.branch) setBranch(location.state.branch);
      if (location.state?.prompt) setPrompt(location.state.prompt);
    }

    // imageAttachments: draft → default (applies to all modes)
    if (draft?.imageAttachments) {
      setImageAttachments(draft.imageAttachments);
    }

    initialized.current = true;
    skipNextPersist.current = true;
  }, [mode, sessionsLoading, workspaces, searchParams, urlWorkspaceId, location.state]);

  const availableModels = useMemo(() => {
    const enabled = config?.enabled_models || {};
    const hasExplicit = Object.keys(enabled).length > 0;
    return models
      .filter((m) => (hasExplicit ? m.id in enabled : m.configured))
      .map((m) => ({ name: m.id, label: m.display_name }));
  }, [models, config]);

  const [targetCounts, setTargetCounts] = useState<Record<string, number>>({});
  const [modelSelectionMode, setModelSelectionMode] = useState<'single' | 'multiple' | 'advanced'>(
    'single'
  );

  // Ensure all items are in targetCounts (skip when empty to avoid wiping draft values)
  useEffect(() => {
    if (availableModels.length === 0) return;
    setTargetCounts((current) => {
      const next = { ...current };
      let changed = false;
      availableModels.forEach((item) => {
        if (next[item.name] === undefined) {
          next[item.name] = 0;
          changed = true;
        }
      });
      Object.keys(next).forEach((name) => {
        if (!availableModels.find((item) => item.name === name)) {
          delete next[name];
          changed = true;
        }
      });
      return changed ? next : current;
    });
  }, [availableModels]);

  // Enforce single mode constraint: when switching to single, reduce to at most one selection
  useEffect(() => {
    if (modelSelectionMode !== 'single') return;
    if (availableModels.length === 0) return;
    setTargetCounts((current) => {
      // Find all selected agents
      const selected = availableModels.filter((item) => (current[item.name] || 0) > 0);
      if (selected.length <= 1) return current; // Already at most one

      // Keep only the first selected, clear the rest
      const firstSelected = selected[0].name;
      const next: Record<string, number> = {};
      availableModels.forEach((item) => {
        next[item.name] = item.name === firstSelected ? 1 : 0;
      });
      return next;
    });
  }, [modelSelectionMode, availableModels]);

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
      targetCounts,
      modelSelectionMode,
    };
    // Only save repo/newRepoName for fresh spawns
    if (!urlWorkspaceId) {
      draft.repo = repo;
      draft.newRepoName = newRepoName;
    }
    // Only save createBranch for workspace mode
    if (urlWorkspaceId) {
      draft.createBranch = createBranch;
    }
    if (imageAttachments.length > 0) {
      draft.imageAttachments = imageAttachments;
    }
    saveSpawnDraft(urlWorkspaceId, draft);
  }, [
    prompt,
    targetCounts,
    modelSelectionMode,
    repo,
    newRepoName,
    createBranch,
    imageAttachments,
    urlWorkspaceId,
    engagePhase,
  ]);

  // Handle autocomplete selection: apply learned defaults from spawn entries
  const handleAutocompleteSelect = useCallback(
    (item: AutocompleteItem) => {
      if (item.source === 'spawn-entry' && item.spawnEntry) {
        const e = item.spawnEntry;
        if (e.target && availableModels.some((m) => m.name === e.target)) {
          setTargetCounts((prev) => {
            const next = { ...prev };
            if (!next[e.target!]) {
              next[e.target!] = 1;
            }
            return next;
          });
        }
      }
    },
    [availableModels]
  );

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
          availableModels.forEach((item) => {
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

  const generateBranchName = useCallback(
    async (
      promptText: string
    ): Promise<{ result: SuggestBranchResponse | null; error: string | null }> => {
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
    },
    []
  );

  const validateForm = useCallback(() => {
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
      if (repo === '__new__' && /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())) {
        const matchingRepo = repos.find((r) => r.url === newRepoName.trim());
        if (matchingRepo) {
          setRepo(matchingRepo.url);
          setNewRepoName('');
          return false;
        }
      }
      if (mode === 'fresh' && !branchSuggestTarget && !branch.trim()) {
        toastError('Please enter a branch name');
        return false;
      }
    }
    if (totalPromptableCount === 0) {
      toastError('Please select at least one target');
      return false;
    }
    // Prompt is optional — agents can be spawned without one for interactive use
    return true;
  }, [
    totalPromptableCount,
    mode,
    repo,
    repos,
    newRepoName,
    branchSuggestTarget,
    branch,
    prompt,
    environment.type,
    toastError,
  ]);

  // Handle spawn result: check for errors, navigate on success, return true if successful
  const handleSpawnResult = useCallback(
    (response: SpawnResult[]): boolean => {
      const hasSuccess = response.some((r) => !r.error);
      if (!hasSuccess) {
        const errors = response.filter((r) => r.error).map((r) => r.error);
        const unique = [...new Set(errors)];
        const combinedError = unique.join('; ');
        if (combinedError.includes('tmux is required')) {
          setTmuxError(combinedError);
        } else {
          alert('Spawn Failed', `Spawn failed: ${combinedError}`);
        }
        setEngagePhase('idle');
        return false;
      }
      setTmuxError('');
      clearSpawnDraft(urlWorkspaceId);
      setImageAttachments([]);
      const successfulResults = response.filter((r) => !r.error);
      if (successfulResults.length === 1 && successfulResults[0].session_id) {
        setPendingNavigation({ type: 'session', id: successfulResults[0].session_id });
      } else if (successfulResults.length > 0) {
        const workspaceId = successfulResults[0].workspace_id;
        if (workspaceId) {
          setPendingNavigation({ type: 'workspace', id: workspaceId });
        }
      }
      setEngagePhase('waiting');

      // Auto-expand workspace(s) in sidebar
      const workspaceIds = [
        ...new Set(
          response
            .filter((r) => !r.error)
            .map((r) => r.workspace_id)
            .filter(Boolean)
        ),
      ] as string[];
      let expanded: Record<string, boolean> = {};
      try {
        expanded = JSON.parse(localStorage.getItem(WORKSPACE_EXPANDED_KEY) || '{}') as Record<
          string,
          boolean
        >;
      } catch (err) {
        console.warn('Failed to parse workspace expanded state:', err);
        expanded = {};
      }
      let changed = false;
      workspaceIds.forEach((id) => {
        if (expanded[id] !== true) {
          expanded[id] = true;
          changed = true;
        }
      });
      if (changed) {
        localStorage.setItem(WORKSPACE_EXPANDED_KEY, JSON.stringify(expanded));
      }
      return true;
    },
    [urlWorkspaceId, toastError, setPendingNavigation]
  );

  // Handle slash command selection - immediately spawns instead of switching mode
  const handleSlashCommandSelect = useCallback(
    async (command: string) => {
      if (engagePhase !== 'idle') return;
      setTmuxError('');

      if (command === '/resume') {
        // Resume: spawn immediately with currently selected agent
        const selectedTargets: Record<string, number> = {};
        Object.entries(targetCounts).forEach(([name, count]) => {
          if (count > 0) selectedTargets[name] = count;
        });
        if (Object.keys(selectedTargets).length === 0) {
          toastError('Select an agent first');
          return;
        }
        setEngagePhase('spawning');
        try {
          const actualRepo =
            repo === '__new__'
              ? /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())
                ? newRepoName.trim()
                : `local:${newRepoName.trim()}`
              : repo;
          const actualBranch =
            mode === 'fresh' ? branch.trim() || getDefaultBranch(actualRepo) : '';
          const response = await spawnSessions({
            repo: mode === 'fresh' ? actualRepo : '',
            branch: actualBranch,
            prompt: '',
            nickname: '',
            targets: selectedTargets,
            workspace_id: prefillWorkspaceId || '',
            resume: true,
            remote_profile_id: environment.type === 'remote' ? environment.profileId : undefined,
            remote_flavor: environment.type === 'remote' ? environment.flavor : undefined,
            remote_host_id: environment.type === 'remote' ? environment.hostId : undefined,
            persona_id: selectedPersonaId || undefined,
            style_id: selectedStyleId || undefined,
            intent_shared: shareIntent || undefined,
          });
          if (handleSpawnResult(response)) {
            saveLastRepo(actualRepo);
            saveLastTargetCounts(selectedTargets);
          }
        } catch (err) {
          const errorMsg = getErrorMessage(err, 'Unknown error');
          if (errorMsg.includes('tmux is required')) {
            setTmuxError(errorMsg);
          } else {
            alert('Spawn Failed', `Failed to spawn: ${errorMsg}`);
          }
          setEngagePhase('idle');
        }
        return;
      }

      if (command.startsWith('/quick ')) {
        // Quick launch: spawn immediately
        const quickName = command.slice('/quick '.length);
        setEngagePhase('spawning');
        try {
          const response = await spawnSessions({
            repo: '',
            branch: '',
            prompt: '',
            nickname: '',
            targets: {},
            workspace_id: prefillWorkspaceId || '',
            quick_launch_name: quickName,
          });
          handleSpawnResult(response);
        } catch (err) {
          const errorMsg = getErrorMessage(err, 'Unknown error');
          if (errorMsg.includes('tmux is required')) {
            setTmuxError(errorMsg);
          } else {
            alert('Spawn Failed', `Failed to spawn: ${errorMsg}`);
          }
          setEngagePhase('idle');
        }
        return;
      }

      // Command target: spawn immediately with the command
      setEngagePhase('spawning');
      try {
        const actualRepo =
          repo === '__new__'
            ? /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())
              ? newRepoName.trim()
              : `local:${newRepoName.trim()}`
            : repo;
        const actualBranch =
          mode === 'fresh' ? branch.trim() || getDefaultBranch(actualRepo) : branch;
        const response = await spawnSessions({
          repo: actualRepo,
          branch: actualBranch,
          prompt: '',
          nickname: '',
          targets: { [command]: 1 },
          workspace_id: prefillWorkspaceId || '',
          remote_profile_id: environment.type === 'remote' ? environment.profileId : undefined,
          remote_flavor: environment.type === 'remote' ? environment.flavor : undefined,
          remote_host_id: environment.type === 'remote' ? environment.hostId : undefined,
          persona_id: selectedPersonaId || undefined,
          style_id: selectedStyleId || undefined,
        });
        if (handleSpawnResult(response)) {
          saveLastRepo(actualRepo);
        }
      } catch (err) {
        const errorMsg = getErrorMessage(err, 'Unknown error');
        if (errorMsg.includes('tmux is required')) {
          setTmuxError(errorMsg);
        } else {
          alert('Spawn Failed', `Failed to spawn: ${errorMsg}`);
        }
        setEngagePhase('idle');
      }
    },
    [
      engagePhase,
      targetCounts,
      prefillWorkspaceId,
      repo,
      newRepoName,
      mode,
      branch,
      environment,
      getDefaultBranch,
      handleSpawnResult,
      toastError,
      selectedPersonaId,
      selectedStyleId,
    ]
  );

  const handleEngage = useCallback(async () => {
    if (!validateForm()) return;
    setTmuxError('');

    const selectedTargets: Record<string, number> = {};
    Object.entries(targetCounts).forEach(([name, count]) => {
      if (count > 0) selectedTargets[name] = count;
    });

    const actualRepo =
      repo === '__new__'
        ? /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())
          ? newRepoName.trim()
          : `local:${newRepoName.trim()}`
        : repo;
    let actualBranch = branch;
    let actualNickname = nickname;
    let newBranch: string | undefined;

    // Fresh mode: need to determine branch
    if (mode === 'fresh') {
      if (branch.trim()) {
        // User provided a branch name — use it directly, skip suggestion
        actualBranch = branch.trim();
        actualNickname = nickname;
      } else if (prompt.trim() && branchSuggestTarget) {
        // Call branch suggest API
        setEngagePhase('naming');
        const { result, error } = await generateBranchName(prompt);
        if (!isMounted.current) return;
        if (result && result.branch.trim()) {
          actualBranch = result.branch;
        } else {
          // Abort — reveal branch input so user can provide one
          setShowBranchInput(true);
          setEngagePhase('idle');
          alert(
            'Branch Suggestion Failed',
            `Branch suggestion failed: ${error}. Please enter a branch name.`
          );
          return;
        }
      } else {
        // No suggestion available and no branch provided — shouldn't reach here
        // due to validateForm, but guard anyway
        actualBranch = getDefaultBranch(actualRepo);
        actualNickname = '';
      }
    }

    // Workspace mode with "Create new branch" checked
    if (mode === 'workspace' && createBranch && prompt.trim() && branchSuggestTarget) {
      // Call branch suggest API to get new branch name
      setEngagePhase('naming');
      const { result, error } = await generateBranchName(prompt);
      if (!isMounted.current) return;
      if (result && result.branch.trim()) {
        newBranch = result.branch;
      } else {
        setEngagePhase('idle');
        alert('Branch Suggestion Failed', `Branch suggestion failed: ${error}. Please try again.`);
        return;
      }
    }

    // Spawn
    setEngagePhase('spawning');

    try {
      const response = await spawnSessions({
        repo: actualRepo,
        branch: actualBranch,
        prompt,
        nickname: actualNickname.trim(),
        targets: selectedTargets,
        workspace_id: prefillWorkspaceId || '',
        remote_profile_id: environment.type === 'remote' ? environment.profileId : undefined,
        remote_flavor: environment.type === 'remote' ? environment.flavor : undefined,
        remote_host_id: environment.type === 'remote' ? environment.hostId : undefined,
        new_branch: newBranch,
        persona_id: selectedPersonaId || undefined,
        style_id: selectedStyleId || undefined,
        image_attachments: imageAttachments.length > 0 ? imageAttachments : undefined,
      });
      if (handleSpawnResult(response)) {
        saveLastRepo(actualRepo);
        saveLastTargetCounts(selectedTargets);
        saveLastModelSelectionMode(modelSelectionMode);
      }
    } catch (err) {
      const errorMsg = getErrorMessage(err, 'Unknown error');
      if (errorMsg.includes('tmux is required')) {
        setTmuxError(errorMsg);
      } else {
        alert('Spawn Failed', `Failed to spawn: ${errorMsg}`);
      }
      setEngagePhase('idle');
    }
  }, [
    validateForm,
    targetCounts,
    repo,
    newRepoName,
    branch,
    nickname,
    mode,
    createBranch,
    prompt,
    branchSuggestTarget,
    environment,
    prefillWorkspaceId,
    modelSelectionMode,
    getDefaultBranch,
    generateBranchName,
    toastError,
    handleSpawnResult,
    selectedPersonaId,
    selectedStyleId,
    imageAttachments,
  ]);

  // Global Cmd+Enter handler to submit form from any input on the spawn page
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Cmd+Enter (Mac) or Ctrl+Enter (other platforms) submits the form
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
        e.preventDefault();
        void handleEngage();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleEngage]);

  // Handle paste events for image attachments
  useEffect(() => {
    const handlePaste = async (e: ClipboardEvent) => {
      if (!e.clipboardData?.items) return;

      // Find image item in clipboard
      const imageItem = Array.from(e.clipboardData.items).find((item) =>
        item.type.startsWith('image/')
      );
      if (!imageItem) return;

      const blob = imageItem.getAsFile();
      if (!blob) return;

      // Check limit
      if (imageAttachments.length >= 5) return;

      // Prevent default paste behavior (don't paste image as text in textarea)
      e.preventDefault();

      // Convert to base64
      const buf = await blob.arrayBuffer();
      const bytes = new Uint8Array(buf);
      let binary = '';
      for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
      }
      const base64 = btoa(binary);

      setImageAttachments((prev) => {
        if (prev.length >= 5) return prev;
        return [...prev, base64];
      });
    };

    document.addEventListener('paste', handlePaste);
    return () => document.removeEventListener('paste', handlePaste);
  }, [imageAttachments.length]);

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
          <SessionTabs
            sessions={currentWorkspace.sessions || []}
            workspace={currentWorkspace}
            activeSpawnTab
          />
        </>
      )}
      {!currentWorkspace && (
        <div className="app-header">
          <div className="app-header__info">
            <h1 className="app-header__meta">What do you want me to do?</h1>
          </div>
        </div>
      )}

      <div className="spawn-content" data-tour="spawn-form">
        {/* Environment selection for fresh spawns */}
        {mode === 'fresh' && (
          <RemoteHostSelector
            value={environment}
            onChange={setEnvironment}
            disabled={engagePhase !== 'idle'}
          />
        )}

        {/* Prompt area */}
        <div
          className="card card--prompt"
          style={{ marginBottom: 'var(--spacing-md)', padding: '0', overflow: 'visible' }}
        >
          <PromptTextarea
            value={prompt}
            onChange={setPrompt}
            placeholder="Describe the task you want the targets to work on... (Type / for commands, ⌘↩ to submit)"
            commands={[
              ...commandTargets.map((t) => t.name),
              '/resume',
              ...(mode === 'workspace'
                ? getQuickLaunchItems(
                    (config?.quick_launch || []).map((ql) => ql.name),
                    currentWorkspace?.quick_launch || []
                  ).map((item) => `/quick ${item.name}`)
                : []),
            ]}
            onSelectCommand={handleSlashCommandSelect}
            onSubmit={handleEngage}
            data-testid="spawn-prompt"
            autocompleteEntries={acEntries}
            autocompleteHistory={acHistory}
            onAutocompleteSelect={handleAutocompleteSelect}
          />
          {imageAttachments.length > 0 && (
            <div
              style={{
                padding: 'var(--spacing-sm) var(--spacing-md)',
                borderTop: '1px solid var(--color-border)',
                display: 'flex',
                flexWrap: 'wrap',
                gap: 'var(--spacing-sm)',
                fontSize: '0.8125rem',
                color: 'var(--color-text-secondary)',
              }}
            >
              {imageAttachments.map((_, index) => (
                <span
                  key={index}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 'var(--spacing-xs)',
                    padding: '2px var(--spacing-sm)',
                    background: 'var(--color-surface-raised)',
                    borderRadius: 'var(--radius-sm)',
                  }}
                >
                  Image {index + 1}
                  <button
                    type="button"
                    onClick={() =>
                      setImageAttachments((prev) => prev.filter((_, i) => i !== index))
                    }
                    style={{
                      background: 'none',
                      border: 'none',
                      cursor: 'pointer',
                      padding: '0 2px',
                      fontSize: '0.75rem',
                      color: 'var(--color-text-secondary)',
                      lineHeight: 1,
                    }}
                    aria-label={`Remove image ${index + 1}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </div>
          )}
        </div>

        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'auto 1fr',
            gap: 'var(--spacing-md)',
            marginBottom: 'var(--spacing-md)',
            alignItems: 'center',
          }}
        >
          {/* Agent selection */}
          {availableModels.length > 0 && (
            <>
              {modelSelectionMode === 'single' ? (
                <>
                  {mode === 'fresh' && !isRemoteSpawn ? (
                    /* Single agent + fresh mode: compact labeled selectors */
                    <div className="grid-full">
                      <div data-testid="agent-repo-row" className="spawn-selectors">
                        <div className="spawn-selector">
                          <span className="spawn-selector__label">Agent</span>
                          <select
                            className="select"
                            data-testid="agent-select"
                            value={
                              availableModels.find((item) => (targetCounts[item.name] || 0) > 0)
                                ?.name || ''
                            }
                            onChange={(e) => {
                              const val = e.target.value;
                              if (val === '__multiple__') {
                                setModelSelectionMode('multiple');
                              } else if (val === '__advanced__') {
                                setModelSelectionMode('advanced');
                              } else if (val) {
                                toggleAgent(val);
                              } else {
                                const selected = availableModels.find(
                                  (item) => (targetCounts[item.name] || 0) > 0
                                );
                                if (selected) toggleAgent(selected.name);
                              }
                            }}
                          >
                            <option value="">Select...</option>
                            {availableModels.map((item) => (
                              <option key={item.name} value={item.name}>
                                {item.label}
                              </option>
                            ))}
                            <option disabled>──────────</option>
                            <option value="__multiple__">Multiple agents</option>
                            <option value="__advanced__">Advanced</option>
                          </select>
                        </div>
                        {personas.length > 0 && (
                          <div className="spawn-selector">
                            <span className="spawn-selector__label">Persona</span>
                            <select
                              data-testid="persona-select"
                              className="select"
                              data-tour="spawn-persona-select"
                              value={selectedPersonaId}
                              onChange={(e) => setSelectedPersonaId(e.target.value)}
                            >
                              <option value="">None</option>
                              {personas.map((p) => (
                                <option key={p.id} value={p.id}>
                                  {p.icon} {p.name}
                                </option>
                              ))}
                            </select>
                          </div>
                        )}
                        {styles.length > 0 && (
                          <div className="spawn-selector">
                            <span className="spawn-selector__label">Style</span>
                            <select
                              data-testid="style-select"
                              className="select"
                              value={selectedStyleId}
                              onChange={(e) => setSelectedStyleId(e.target.value)}
                            >
                              <option value="">Default</option>
                              <option value="none">None</option>
                              {styles.map((s) => (
                                <option key={s.id} value={s.id}>
                                  {s.icon} {s.name}
                                </option>
                              ))}
                            </select>
                          </div>
                        )}
                        <div className="spawn-selector">
                          <span className="spawn-selector__label">Repo</span>
                          <select
                            id="repo"
                            className="select"
                            data-tour="spawn-repo-select"
                            required
                            value={repo}
                            data-testid="spawn-repo-select"
                            onChange={(event) => {
                              setRepo(event.target.value);
                              if (event.target.value !== '__new__') {
                                setNewRepoName('');
                              }
                            }}
                          >
                            <option value="">Select...</option>
                            {repos.map((item) => (
                              <option key={item.url} value={item.url}>
                                {item.name}
                              </option>
                            ))}
                            <option value="__new__">+ Add Repository</option>
                          </select>
                        </div>
                        {showBranchInput && (
                          <div className="spawn-selector">
                            <span className="spawn-selector__label">Branch</span>
                            <input
                              type="text"
                              id="branch"
                              className="input"
                              value={branch}
                              onChange={(event) => setBranch(event.target.value)}
                              placeholder="e.g. feature/my-branch"
                            />
                          </div>
                        )}
                      </div>
                      {repo === '__new__' && (
                        <input
                          type="text"
                          id="newRepoName"
                          className="input mt-sm"
                          value={newRepoName}
                          onChange={(event) => setNewRepoName(event.target.value)}
                          placeholder="Name or git URL"
                          required
                        />
                      )}
                      {repo === '__new__' && (
                        <span className="form-group__hint">
                          Enter a name to create locally, or paste a git URL to clone.
                        </span>
                      )}
                    </div>
                  ) : (
                    /* Single agent + workspace/remote mode: compact labeled selectors */
                    <div className="grid-full">
                      <div data-testid="agent-repo-row" className="spawn-selectors">
                        <div className="spawn-selector">
                          <span className="spawn-selector__label">Agent</span>
                          <select
                            className="select"
                            data-testid="agent-select"
                            value={
                              availableModels.find((item) => (targetCounts[item.name] || 0) > 0)
                                ?.name || ''
                            }
                            onChange={(e) => {
                              const val = e.target.value;
                              if (val === '__multiple__') {
                                setModelSelectionMode('multiple');
                              } else if (val === '__advanced__') {
                                setModelSelectionMode('advanced');
                              } else if (val) {
                                toggleAgent(val);
                              } else {
                                const selected = availableModels.find(
                                  (item) => (targetCounts[item.name] || 0) > 0
                                );
                                if (selected) toggleAgent(selected.name);
                              }
                            }}
                          >
                            <option value="">Select...</option>
                            {availableModels.map((item) => (
                              <option key={item.name} value={item.name}>
                                {item.label}
                              </option>
                            ))}
                            <option disabled>──────────</option>
                            <option value="__multiple__">Multiple agents</option>
                            <option value="__advanced__">Advanced</option>
                          </select>
                        </div>
                        {personas.length > 0 && (
                          <div className="spawn-selector">
                            <span className="spawn-selector__label">Persona</span>
                            <select
                              data-testid="persona-select"
                              className="select"
                              value={selectedPersonaId}
                              onChange={(e) => setSelectedPersonaId(e.target.value)}
                            >
                              <option value="">None</option>
                              {personas.map((p) => (
                                <option key={p.id} value={p.id}>
                                  {p.icon} {p.name}
                                </option>
                              ))}
                            </select>
                          </div>
                        )}
                        {styles.length > 0 && (
                          <div className="spawn-selector">
                            <span className="spawn-selector__label">Style</span>
                            <select
                              data-testid="style-select"
                              className="select"
                              value={selectedStyleId}
                              onChange={(e) => setSelectedStyleId(e.target.value)}
                            >
                              <option value="">Default</option>
                              <option value="none">None</option>
                              {styles.map((s) => (
                                <option key={s.id} value={s.id}>
                                  {s.icon} {s.name}
                                </option>
                              ))}
                            </select>
                          </div>
                        )}
                      </div>
                    </div>
                  )}
                </>
              ) : (
                <>
                  {/* Multi/Advanced mode: repo on its own row first (fresh only) */}
                  {mode === 'fresh' && !isRemoteSpawn && (
                    <>
                      <label
                        htmlFor="repo"
                        className="form-group__label"
                        style={{ marginBottom: 0, whiteSpace: 'nowrap' }}
                      >
                        Repository
                      </label>
                      <div>
                        <select
                          id="repo"
                          className="select"
                          required
                          value={repo}
                          data-testid="spawn-repo-select"
                          onChange={(event) => {
                            setRepo(event.target.value);
                            if (event.target.value !== '__new__') {
                              setNewRepoName('');
                            }
                          }}
                        >
                          <option value="">Select repository...</option>
                          {repos.map((item) => (
                            <option key={item.url} value={item.url}>
                              {item.name}
                            </option>
                          ))}
                          <option value="__new__">+ Add Repository</option>
                        </select>

                        {repo === '__new__' && (
                          <div className="mt-sm">
                            <input
                              type="text"
                              id="newRepoName"
                              className="input"
                              value={newRepoName}
                              onChange={(event) => setNewRepoName(event.target.value)}
                              placeholder="Name or git URL"
                              required
                            />
                            <p
                              style={{
                                color: 'var(--color-text-secondary)',
                                fontSize: '0.75rem',
                                marginTop: 'var(--spacing-xs)',
                              }}
                            >
                              Enter a name to create locally, or paste a git URL to clone.
                            </p>
                          </div>
                        )}
                      </div>
                    </>
                  )}

                  <div className="grid-full">
                    <button
                      type="button"
                      className="btn mb-sm"
                      onClick={() => setModelSelectionMode('single')}
                    >
                      Single agent
                    </button>

                    {modelSelectionMode === 'multiple' && (
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
                          gap: 'var(--spacing-sm)',
                        }}
                      >
                        {availableModels.map((item) => {
                          const isSelected = (targetCounts[item.name] || 0) > 0;
                          return (
                            <button
                              key={item.name}
                              type="button"
                              className={`btn${isSelected ? ' btn--primary' : ''}`}
                              onClick={() => toggleAgent(item.name)}
                              data-testid={`agent-${item.name}`}
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
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                          gap: 'var(--spacing-sm)',
                        }}
                      >
                        {availableModels.map((item) => {
                          const count = targetCounts[item.name] || 0;
                          const isSelected = count > 0;
                          return (
                            <div
                              key={item.name}
                              data-testid={`agent-${item.name}`}
                              style={{
                                display: 'flex',
                                alignItems: 'center',
                                gap: 'var(--spacing-xs)',
                                border: '1px solid var(--color-border)',
                                borderRadius: 'var(--radius-sm)',
                                padding: 'var(--spacing-xs)',
                                backgroundColor: isSelected
                                  ? 'var(--color-accent)'
                                  : 'var(--color-surface-alt)',
                              }}
                            >
                              <span
                                style={{
                                  fontSize: '0.875rem',
                                  flex: 1,
                                  overflow: 'hidden',
                                  textOverflow: 'ellipsis',
                                }}
                              >
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
                                  backgroundColor: isSelected
                                    ? 'rgba(255,255,255,0.2)'
                                    : 'var(--color-surface)',
                                  color: isSelected ? 'white' : 'var(--color-text)',
                                  border: 'none',
                                  borderRadius: 'var(--radius-sm)',
                                }}
                              >
                                −
                              </button>
                              <span
                                style={{
                                  fontSize: '0.875rem',
                                  minWidth: '16px',
                                  textAlign: 'center',
                                }}
                              >
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
                                  backgroundColor: isSelected
                                    ? 'rgba(255,255,255,0.2)'
                                    : 'var(--color-surface)',
                                  color: isSelected ? 'white' : 'var(--color-text)',
                                  border: 'none',
                                  borderRadius: 'var(--radius-sm)',
                                }}
                              >
                                +
                              </button>
                            </div>
                          );
                        })}
                      </div>
                    )}

                    {/* Persona + style selector for multiple/advanced modes */}
                    {(personas.length > 0 || styles.length > 0) && (
                      <div
                        className="form-row"
                        data-testid="persona-style-row"
                        style={{
                          display: 'flex',
                          gap: 'var(--spacing-md)',
                          marginTop: 'var(--spacing-md)',
                        }}
                      >
                        {personas.length > 0 && (
                          <select
                            data-testid="persona-select"
                            className="select flex-1"
                            value={selectedPersonaId}
                            onChange={(e) => setSelectedPersonaId(e.target.value)}
                          >
                            <option value="">No Persona</option>
                            {personas.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.icon} {p.name}
                              </option>
                            ))}
                          </select>
                        )}
                        {styles.length > 0 && (
                          <select
                            data-testid="style-select"
                            className="select flex-1"
                            value={selectedStyleId}
                            onChange={(e) => setSelectedStyleId(e.target.value)}
                          >
                            <option value="">Global Default</option>
                            <option value="none">None</option>
                            {styles.map((s) => (
                              <option key={s.id} value={s.id}>
                                {s.icon} {s.name}
                              </option>
                            ))}
                          </select>
                        )}
                      </div>
                    )}
                  </div>
                </>
              )}
            </>
          )}

          {/* Branch (shown on suggestion failure or when suggestion is disabled, multi/advanced only) */}
          {mode === 'fresh' &&
            !isRemoteSpawn &&
            showBranchInput &&
            modelSelectionMode !== 'single' && (
              <div className="grid-full">
                <input
                  type="text"
                  id="branch"
                  className="input w-full"
                  value={branch}
                  onChange={(event) => setBranch(event.target.value)}
                  placeholder="Branch (e.g. feature/my-branch)"
                />
              </div>
            )}
        </div>

        {tmuxError && (
          <div
            className="mt-md"
            data-testid="tmux-error"
            style={{
              padding: 'var(--spacing-md)',
              borderRadius: 'var(--radius-md)',
              backgroundColor: 'var(--color-error-bg, rgba(239, 68, 68, 0.1))',
              border: '1px solid var(--color-error, #ef4444)',
              color: 'var(--color-error, #ef4444)',
            }}
          >
            <strong>tmux not found</strong>
            <p style={{ margin: 'var(--spacing-xs) 0 0' }}>{tmuxError}</p>
            <p style={{ margin: 'var(--spacing-xs) 0 0', fontSize: 'var(--font-size-sm)' }}>
              Install with: <code>brew install tmux</code> (macOS) or <code>apt install tmux</code>{' '}
              (Linux), then retry.
            </p>
          </div>
        )}

        <div className="flex-row mt-lg gap-sm" style={{ justifyContent: 'flex-end' }}>
          {/* Create new branch checkbox (only in workspace mode) */}
          {mode === 'workspace' && currentWorkspace && (
            <div style={{ marginRight: 'auto' }}>
              {!currentWorkspace.commits_synced_with_remote ? (
                <Tooltip content="Branch must be pushed to origin first" variant="warning">
                  <span style={{ display: 'inline-block' }}>
                    <label
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 'var(--spacing-xs)',
                        cursor: 'not-allowed',
                        opacity: 0.5,
                      }}
                    >
                      <input type="checkbox" checked={createBranch} onChange={() => {}} disabled />
                      <span>Create new branch from here</span>
                    </label>
                  </span>
                </Tooltip>
              ) : (
                <label className="flex-row gap-xs cursor-pointer">
                  <input
                    type="checkbox"
                    checked={createBranch}
                    onChange={(e) => setCreateBranch(e.target.checked)}
                    disabled={engagePhase !== 'idle'}
                  />
                  <span>Create new branch from here</span>
                </label>
              )}
            </div>
          )}
          {config?.repofeed?.enabled && (
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                marginRight: 'auto',
                fontSize: '0.8125rem',
                color: 'var(--color-text-secondary)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={shareIntent}
                onChange={(e) => setShareIntent(e.target.checked)}
                data-testid="share-intent-toggle"
              />
              Share activity with team
            </label>
          )}
          <button
            className="btn btn--primary flex-row gap-sm"
            onClick={handleEngage}
            disabled={engagePhase !== 'idle'}
            data-tour="spawn-submit"
            data-testid="spawn-submit"
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
            ) : (
              'Engage'
            )}
          </button>
        </div>
      </div>
    </>
  );
}
