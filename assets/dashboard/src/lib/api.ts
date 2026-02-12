import type {
  ApiError,
  BuiltinQuickLaunchCookbook,
  ConfigResponse,
  ConfigUpdateRequest,
  DetectToolsResponse,
  DiffExternalResponse,
  DiffResponse,
  GitGraphResponse,
  LinearSyncResponse,
  LinearSyncResolveConflictResponse,
  OpenVSCodeResponse,
  OverlaysResponse,
  PRCheckoutResponse,
  PRRefreshResponse,
  PRsResponse,
  RecentBranch,
  RemoteFlavor,
  RemoteFlavorCreateRequest,
  RemoteFlavorStatus,
  RemoteHost,
  RemoteHostConnectRequest,
  ScanResult,
  SpawnRequest,
  SpawnResult,
  SuggestBranchRequest,
  SuggestBranchResponse,
  WorkspaceResponse,
} from './types';

// Custom error types that preserve API response fields
export class LinearSyncError extends Error {
  isPreCommitHookError: boolean;
  preCommitErrorDetail?: string;

  constructor(message: string, isPreCommitHookError: boolean, preCommitErrorDetail?: string) {
    super(message);
    this.name = 'LinearSyncError';
    this.isPreCommitHookError = isPreCommitHookError;
    this.preCommitErrorDetail = preCommitErrorDetail;
  }
}

// Extract error message from unknown catch value
export function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error) return err.message;
  if (typeof err === 'string') return err;
  return fallback;
}

// Parse error from a non-ok Response, trying JSON then falling back to text
async function parseErrorResponse(response: Response, fallback: string): Promise<never> {
  let message = fallback;
  try {
    const err = await response.json();
    message = err.error || fallback;
  } catch {
    message = (await response.text()) || fallback;
  }
  throw new Error(message);
}

export async function getSessions(): Promise<WorkspaceResponse[]> {
  const response = await fetch('/api/sessions');
  if (!response.ok) throw new Error('Failed to fetch sessions');
  return response.json();
}

export async function getConfig(): Promise<ConfigResponse> {
  const response = await fetch('/api/config');
  if (!response.ok) throw new Error('Failed to fetch config');
  return response.json();
}

export async function spawnSessions(request: SpawnRequest): Promise<SpawnResult[]> {
  const response = await fetch('/api/spawn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    // Get error message from response body
    const text = await response.text();
    throw new Error(text || 'Failed to spawn sessions');
  }
  return response.json();
}

/**
 * Checks if a branch is already in use by an existing workspace (worktree conflict).
 * Only relevant when source_code_manager is "git-worktree".
 */
export async function checkBranchConflict(
  repo: string,
  branch: string
): Promise<{ conflict: boolean; workspace_id?: string }> {
  const response = await fetch('/api/check-branch-conflict', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo, branch }),
  });
  if (!response.ok) {
    throw new Error('Failed to check branch conflict');
  }
  return response.json();
}

/**
 * Suggests a branch name and nickname based on a task prompt.
 * Returns an object with branch (kebab-case) and nickname (short description).
 */
export async function suggestBranch(request: SuggestBranchRequest): Promise<SuggestBranchResponse> {
  const response = await fetch('/api/suggest-branch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const text = await response.text();
    let message = 'Failed to suggest branch name';
    try {
      const parsed = JSON.parse(text);
      if (parsed.error) message = parsed.error;
    } catch {
      if (text) message = text;
    }
    throw new Error(message);
  }
  return response.json();
}

/**
 * Prepares spawn data for an existing branch.
 * Gets commit log, generates nickname, and returns everything for the spawn form.
 */
export interface PrepareBranchSpawnResponse {
  repo: string;
  branch: string;
  prompt: string;
  nickname: string;
}

export async function prepareBranchSpawn(
  repo: string,
  branch: string
): Promise<PrepareBranchSpawnResponse> {
  const response = await fetch('/api/prepare-branch-spawn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo, branch }),
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to prepare branch spawn');
  }
  return response.json();
}

export async function disposeSession(sessionId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/sessions/${sessionId}/dispose`, { method: 'POST' });
  if (!response.ok) throw new Error('Failed to dispose session');
  return response.json();
}

export async function updateNickname(
  sessionId: string,
  nickname: string
): Promise<{ status: string }> {
  const response = await fetch(`/api/sessions-nickname/${sessionId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ nickname }),
  });
  if (!response.ok) {
    if (response.status === 409) {
      const err = await response.json();
      const error = new Error(err.error || 'Nickname already in use') as ApiError;
      error.isConflict = true;
      throw error;
    }
    throw new Error('Failed to update nickname');
  }
  return response.json();
}

export async function disposeWorkspace(workspaceId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/workspaces/${workspaceId}/dispose`, { method: 'POST' });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.error || 'Failed to dispose workspace');
  }
  return response.json();
}

export async function disposeWorkspaceAll(
  workspaceId: string
): Promise<{ status: string; sessions_disposed: number }> {
  const response = await fetch(`/api/workspaces/${workspaceId}/dispose-all`, { method: 'POST' });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.error || 'Failed to dispose workspace and sessions');
  }
  return response.json();
}

export async function getDiff(workspaceId: string): Promise<DiffResponse> {
  const response = await fetch(`/api/diff/${workspaceId}`);
  if (!response.ok) throw new Error('Failed to fetch diff');
  return response.json();
}

export async function getAuthMe(): Promise<{ login: string; avatar_url?: string; name?: string }> {
  const response = await fetch('/auth/me');
  if (!response.ok) {
    throw new Error('Failed to fetch auth user');
  }
  return response.json();
}

export async function scanWorkspaces(): Promise<ScanResult> {
  const response = await fetch('/api/workspaces/scan', { method: 'POST' });
  if (!response.ok) throw new Error('Failed to scan workspaces');
  return response.json();
}

export async function updateConfig(
  request: ConfigUpdateRequest
): Promise<{ status: string; message?: string; warning?: string; warnings?: string[] }> {
  const response = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    let message = response.statusText || 'Failed to update config';
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) {
      const err = await response.json();
      message = err.detail || err.error || message;
    } else {
      const text = await response.text();
      if (text) {
        message = text;
      }
    }
    throw new Error(message);
  }
  return response.json();
}

export async function getAuthSecretsStatus(): Promise<{
  client_id_set: boolean;
  client_secret_set: boolean;
}> {
  const response = await fetch('/api/auth/secrets');
  if (!response.ok) throw new Error('Failed to fetch auth secrets');
  return response.json();
}

export async function saveAuthSecrets(payload: {
  client_id: string;
  client_secret: string;
}): Promise<{ status: string }> {
  const response = await fetch('/api/auth/secrets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to save auth secrets');
  }
  return response.json();
}

export async function openVSCode(workspaceId: string): Promise<OpenVSCodeResponse> {
  const response = await fetch(`/api/open-vscode/${workspaceId}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.message || response.statusText || 'Failed to open VS Code');
  }
  return response.json();
}

export async function diffExternal(
  workspaceId: string,
  command?: string
): Promise<DiffExternalResponse> {
  const response = await fetch(`/api/diff-external/${workspaceId}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(command ? { command } : {}),
  });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.message || response.statusText || 'Failed to open external diff');
  }
  return response.json();
}

/**
 * Detects available tools on the system.
 * Returns a list of detected tools with their names, commands, and sources.
 */
export async function detectTools(): Promise<DetectToolsResponse> {
  const response = await fetch('/api/detect-tools');
  if (!response.ok) {
    throw new Error('Failed to detect tools');
  }
  return response.json();
}

/**
 * Configures secrets for a third-party model.
 */
export async function configureModelSecrets(
  modelId: string,
  secrets: Record<string, string>
): Promise<{ status: string }> {
  const response = await fetch(`/api/models/${modelId}/secrets`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ secrets }),
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to save model secrets');
  }
  return response.json();
}

/**
 * Removes secrets for a third-party model.
 */
export async function removeModelSecrets(modelId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/models/${modelId}/secrets`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to remove model secrets');
  }
  return response.json();
}

export async function getOverlays(): Promise<OverlaysResponse> {
  const response = await fetch('/api/overlays');
  if (!response.ok) throw new Error('Failed to fetch overlays');
  return response.json();
}

export async function refreshOverlay(workspaceId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/workspaces/${workspaceId}/refresh-overlay`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.error || 'Failed to refresh overlay');
  }
  return response.json();
}

/**
 * Fetches the list of built-in quick launch presets.
 * Returns a list of preset templates with names, targets, and prompts.
 */
export async function getBuiltinQuickLaunch(): Promise<BuiltinQuickLaunchCookbook[]> {
  const response = await fetch('/api/builtin-quick-launch');
  if (!response.ok) {
    throw new Error('Failed to fetch built-in quick launch presets');
  }
  return response.json();
}

export async function linearSyncFromMain(workspaceId: string): Promise<LinearSyncResponse> {
  const response = await fetch(`/api/workspaces/${workspaceId}/linear-sync-from-main`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const err = (await response.json()) as LinearSyncResponse & { message: string };
    throw new LinearSyncError(
      err.message || 'Failed to sync from main',
      err.is_pre_commit_hook_error || false,
      err.pre_commit_error_detail
    );
  }
  return response.json();
}

export async function linearSyncToMain(workspaceId: string): Promise<LinearSyncResponse> {
  const response = await fetch(`/api/workspaces/${workspaceId}/linear-sync-to-main`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.message || err.error || 'Failed to sync to main');
  }
  return response.json();
}

export async function linearSyncResolveConflict(
  workspaceId: string
): Promise<LinearSyncResolveConflictResponse> {
  const response = await fetch(`/api/workspaces/${workspaceId}/linear-sync-resolve-conflict`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok && response.status !== 202) {
    const err = await response.json();
    throw new Error(err.message || err.error || 'Failed to start conflict resolution');
  }
  return response.json();
}

export async function dismissLinearSyncResolveConflictState(workspaceId: string): Promise<void> {
  const response = await fetch(
    `/api/workspaces/${workspaceId}/linear-sync-resolve-conflict-state`,
    {
      method: 'DELETE',
    }
  );
  if (!response.ok) {
    const err = await response.json().catch(() => ({}));
    throw new Error((err as Record<string, string>).message || 'Failed to dismiss');
  }
}

export async function getRecentBranches(limit: number = 10): Promise<RecentBranch[]> {
  const response = await fetch(`/api/recent-branches?limit=${limit}`);
  if (!response.ok) {
    throw new Error('Failed to fetch recent branches');
  }
  return response.json();
}

export async function getGitGraph(
  workspaceId: string,
  opts?: { maxCommits?: number; context?: number }
): Promise<GitGraphResponse> {
  const params = new URLSearchParams();
  if (opts?.maxCommits) params.set('max_commits', String(opts.maxCommits));
  if (opts?.context) params.set('context', String(opts.context));
  const qs = params.toString();
  const url = `/api/workspaces/${encodeURIComponent(workspaceId)}/git-graph${qs ? `?${qs}` : ''}`;
  const response = await fetch(url);
  if (!response.ok) throw new Error('Failed to fetch git graph');
  return response.json();
}

export async function getPRs(): Promise<PRsResponse> {
  const response = await fetch('/api/prs');
  if (!response.ok) throw new Error('Failed to fetch PRs');
  return response.json();
}

export async function refreshPRs(): Promise<PRRefreshResponse> {
  const response = await fetch('/api/prs/refresh', { method: 'POST' });
  if (!response.ok) throw new Error('Failed to refresh PRs');
  return response.json();
}

export async function checkoutPR(repoUrl: string, prNumber: number): Promise<PRCheckoutResponse> {
  const response = await fetch('/api/prs/checkout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_url: repoUrl, pr_number: prNumber }),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to checkout PR');
  }
  return response.json();
}

// ============================================================================
// Remote Flavor API
// ============================================================================

export async function getRemoteFlavors(): Promise<RemoteFlavor[]> {
  const response = await fetch('/api/config/remote-flavors');
  if (!response.ok) throw new Error('Failed to fetch remote flavors');
  return response.json();
}

export async function createRemoteFlavor(
  request: RemoteFlavorCreateRequest
): Promise<RemoteFlavor> {
  const response = await fetch('/api/config/remote-flavors', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to create remote flavor');
  }
  return response.json();
}

export async function updateRemoteFlavor(
  id: string,
  request: RemoteFlavorCreateRequest
): Promise<RemoteFlavor> {
  const response = await fetch(`/api/config/remote-flavors/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to update remote flavor');
  }
  return response.json();
}

export async function deleteRemoteFlavor(id: string): Promise<void> {
  const response = await fetch(`/api/config/remote-flavors/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to delete remote flavor');
  }
}

// ============================================================================
// Remote Host API
// ============================================================================

export async function getRemoteHosts(): Promise<RemoteHost[]> {
  const response = await fetch('/api/remote/hosts');
  if (!response.ok) throw new Error('Failed to fetch remote hosts');
  return response.json();
}

export async function getRemoteFlavorStatuses(): Promise<RemoteFlavorStatus[]> {
  const response = await fetch('/api/remote/flavor-statuses');
  if (!response.ok) throw new Error('Failed to fetch remote flavor statuses');
  return response.json();
}

export async function connectRemoteHost(request: RemoteHostConnectRequest): Promise<RemoteHost> {
  const response = await fetch('/api/remote/hosts/connect', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to connect to remote host');
  }
  return response.json();
}

export async function reconnectRemoteHost(hostId: string): Promise<RemoteHost> {
  const response = await fetch(`/api/remote/hosts/${encodeURIComponent(hostId)}/reconnect`, {
    method: 'POST',
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to reconnect to remote host');
  }
  return response.json();
}

export async function disconnectRemoteHost(hostId: string): Promise<void> {
  const response = await fetch(`/api/remote/hosts/${encodeURIComponent(hostId)}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to disconnect remote host');
  }
}

// ============================================================================
// Git Commit Workflow API
// ============================================================================

export async function gitCommitStage(
  workspaceId: string,
  files: string[]
): Promise<{ success: boolean; message: string }> {
  const response = await fetch(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/git-commit-stage`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files }),
    }
  );
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to stage files');
  }
  return response.json();
}

export async function gitAmend(
  workspaceId: string,
  files: string[]
): Promise<{ success: boolean; message: string }> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/git-amend`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ files }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to amend commit');
  }
  return response.json();
}

export async function gitDiscard(
  workspaceId: string,
  files?: string[]
): Promise<{ success: boolean; message: string }> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/git-discard`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(files ? { files } : {}),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to discard changes');
  }
  return response.json();
}

const COMMIT_PROMPT_INSTRUCTIONS = [
  'Please create a thorough git commit for these staged files:',
  '',
  '{{FILE_LIST}}',
  '',
  'Do the necessary precommit steps first.',
  'Do not include the generated and co-authored lines.',
  'Keep the message focused on features, not just code changes.',
].join('\n');

function buildCommitPrompt(fileList: string): string {
  return COMMIT_PROMPT_INSTRUCTIONS.replace('{{FILE_LIST}}', fileList);
}

export async function spawnCommitSession(
  workspaceId: string,
  repo: string,
  branch: string,
  selectedFiles: string[]
): Promise<SpawnResult[]> {
  const fileList = selectedFiles.join('\n');
  const prompt = buildCommitPrompt(fileList);

  const spawnRequest: SpawnRequest = {
    repo,
    branch,
    nickname: 'git commit',
    prompt,
    targets: { claude: 1 },
    workspace_id: workspaceId,
  };

  return spawnSessions(spawnRequest);
}

// ============================================================================
// Dev Mode API
// ============================================================================

export interface DevStatus {
  active: boolean;
  source_workspace?: string;
  last_build?: {
    success: boolean;
    workspace_path: string;
    error: string;
    at: string;
  };
}

export async function getDevStatus(): Promise<DevStatus> {
  const response = await fetch('/api/dev/status');
  if (!response.ok) throw new Error('Failed to fetch dev status');
  return response.json();
}

export async function devRebuild(
  workspaceId: string,
  type: 'frontend' | 'backend' | 'both'
): Promise<{ status: string }> {
  const response = await fetch('/api/dev/rebuild', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace_id: workspaceId, type }),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to trigger rebuild');
  }
  return response.json();
}
