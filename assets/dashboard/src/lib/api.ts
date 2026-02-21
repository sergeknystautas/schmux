import type {
  ApiError,
  BuiltinQuickLaunchCookbook,
  ConfigResponse,
  ConfigUpdateRequest,
  DetectToolsResponse,
  DiffExternalResponse,
  DiffResponse,
  GitCommitDetailResponse,
  GitGraphResponse,
  LinearSyncResponse,
  LinearSyncResolveConflictResponse,
  LoreApplyResponse,
  LoreEntriesResponse,
  LoreProposal,
  LoreProposalsResponse,
  LoreStatusResponse,
  OpenVSCodeResponse,
  OverlayAddRequest,
  OverlayAddResponse,
  OverlayScanResponse,
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
  WorkspacePreview,
} from './types';
import { csrfHeaders } from './csrf';

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
    const text = await response.text();
    try {
      const err = JSON.parse(text);
      message = err.error || fallback;
    } catch {
      message = text || fallback;
    }
  } catch {
    // body unreadable, use fallback
  }
  throw new Error(message);
}

export async function getSessions(): Promise<WorkspaceResponse[]> {
  const response = await fetch('/api/sessions');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch sessions');
  return response.json();
}

export async function getConfig(): Promise<ConfigResponse> {
  const response = await fetch('/api/config');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch config');
  return response.json();
}

export async function spawnSessions(request: SpawnRequest): Promise<SpawnResult[]> {
  const response = await fetch('/api/spawn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to spawn sessions');
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
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ repo, branch }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to check branch conflict');
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
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to suggest branch name');
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
  repoName: string,
  branch: string
): Promise<PrepareBranchSpawnResponse> {
  const response = await fetch('/api/prepare-branch-spawn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ repo_name: repoName, branch }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to prepare branch spawn');
  }
  return response.json();
}

export async function disposeSession(sessionId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/sessions/${encodeURIComponent(sessionId)}/dispose`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to dispose session');
  return response.json();
}

export async function updateNickname(
  sessionId: string,
  nickname: string
): Promise<{ status: string }> {
  const response = await fetch(`/api/sessions-nickname/${encodeURIComponent(sessionId)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ nickname }),
  });
  if (!response.ok) {
    if (response.status === 409) {
      let message = 'Nickname already in use';
      try {
        const err = await response.json();
        message = err.error || message;
      } catch {
        // Response wasn't JSON, use default message
      }
      const error = new Error(message) as ApiError;
      error.isConflict = true;
      throw error;
    }
    throw new Error('Failed to update nickname');
  }
  return response.json();
}

export async function disposeWorkspace(workspaceId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/dispose`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to dispose workspace');
  }
  return response.json();
}

export async function disposeWorkspaceAll(
  workspaceId: string
): Promise<{ status: string; sessions_disposed: number }> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/dispose-all`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to dispose workspace and sessions');
  }
  return response.json();
}

export async function getDiff(workspaceId: string): Promise<DiffResponse> {
  const response = await fetch(`/api/diff/${encodeURIComponent(workspaceId)}`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch diff');
  return response.json();
}

// Get a file from the workspace for image preview
export function getWorkspaceFileUrl(workspaceId: string, filePath: string): string {
  const encoded = encodeURIComponent(filePath);
  return `/api/file/${encodeURIComponent(workspaceId)}/${encoded}`;
}

export async function getAuthMe(): Promise<{ login: string; avatar_url?: string; name?: string }> {
  const response = await fetch('/auth/me');
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch auth user');
  }
  return response.json();
}

export async function scanWorkspaces(): Promise<ScanResult> {
  const response = await fetch('/api/workspaces/scan', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to scan workspaces');
  return response.json();
}

export async function updateConfig(
  request: ConfigUpdateRequest
): Promise<{ status: string; message?: string; warning?: string; warnings?: string[] }> {
  const response = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to update config');
  }
  return response.json();
}

export async function getAuthSecretsStatus(): Promise<{
  client_id_set: boolean;
  client_secret_set: boolean;
}> {
  const response = await fetch('/api/auth/secrets');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch auth secrets');
  return response.json();
}

export async function saveAuthSecrets(payload: {
  client_id: string;
  client_secret: string;
}): Promise<{ status: string }> {
  const response = await fetch('/api/auth/secrets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to save auth secrets');
  }
  return response.json();
}

export async function openVSCode(workspaceId: string): Promise<OpenVSCodeResponse> {
  const response = await fetch(`/api/open-vscode/${encodeURIComponent(workspaceId)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to open VS Code');
  }
  return response.json();
}

export async function diffExternal(
  workspaceId: string,
  command?: string
): Promise<DiffExternalResponse> {
  const response = await fetch(`/api/diff-external/${encodeURIComponent(workspaceId)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(command ? { command } : {}),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to open external diff');
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
    await parseErrorResponse(response, 'Failed to detect tools');
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
  const response = await fetch(`/api/models/${encodeURIComponent(modelId)}/secrets`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ secrets }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to save model secrets');
  }
  return response.json();
}

/**
 * Removes secrets for a third-party model.
 */
export async function removeModelSecrets(modelId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/models/${encodeURIComponent(modelId)}/secrets`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to remove model secrets');
  }
  return response.json();
}

export async function getOverlays(): Promise<OverlaysResponse> {
  const response = await fetch('/api/overlays');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch overlays');
  return response.json();
}

export async function refreshOverlay(workspaceId: string): Promise<{ status: string }> {
  const response = await fetch(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/refresh-overlay`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    }
  );
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to refresh overlay');
  }
  return response.json();
}

export async function scanOverlayFiles(
  workspaceId: string,
  repoName: string
): Promise<OverlayScanResponse> {
  const response = await fetch('/api/overlays/scan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ workspace_id: workspaceId, repo_name: repoName }),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to scan overlay files');
  return response.json();
}

export async function addOverlayFiles(req: OverlayAddRequest): Promise<OverlayAddResponse> {
  const response = await fetch('/api/overlays/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to add overlay files');
  return response.json();
}

export async function dismissOverlayNudge(repoName: string): Promise<{ status: string }> {
  const response = await fetch('/api/overlays/dismiss-nudge', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ repo_name: repoName }),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to dismiss overlay nudge');
  return response.json();
}

/**
 * Fetches the list of built-in quick launch presets.
 * Returns a list of preset templates with names, targets, and prompts.
 */
export async function getBuiltinQuickLaunch(): Promise<BuiltinQuickLaunchCookbook[]> {
  const response = await fetch('/api/builtin-quick-launch');
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch built-in quick launch presets');
  }
  return response.json();
}

export async function linearSyncFromMain(
  workspaceId: string,
  hash: string
): Promise<LinearSyncResponse> {
  const response = await fetch(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/linear-sync-from-main`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify({ hash }),
    }
  );
  if (!response.ok) {
    let err: (LinearSyncResponse & { message?: string }) | null = null;
    let fallbackMessage = 'Failed to sync from main';
    try {
      err = (await response.json()) as LinearSyncResponse & { message?: string };
    } catch {
      const text = await response.text().catch(() => '');
      if (text.trim()) {
        fallbackMessage = text.trim();
      }
    }
    throw new LinearSyncError(
      err?.message || fallbackMessage,
      err?.is_pre_commit_hook_error || false,
      err?.pre_commit_error_detail
    );
  }
  return response.json();
}

export async function linearSyncToMain(workspaceId: string): Promise<LinearSyncResponse> {
  const response = await fetch(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/linear-sync-to-main`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    }
  );
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to sync to main');
  }
  return response.json();
}

export async function pushToBranch(workspaceId: string): Promise<LinearSyncResponse> {
  const response = await fetch(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/push-to-branch`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    }
  );
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to push to branch');
  }
  return response.json();
}

export async function linearSyncResolveConflict(
  workspaceId: string
): Promise<LinearSyncResolveConflictResponse> {
  const response = await fetch(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/linear-sync-resolve-conflict`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    }
  );
  if (!response.ok && response.status !== 202) {
    await parseErrorResponse(response, 'Failed to start conflict resolution');
  }
  return response.json();
}

export async function dismissLinearSyncResolveConflictState(workspaceId: string): Promise<void> {
  const response = await fetch(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/linear-sync-resolve-conflict-state`,
    {
      method: 'DELETE',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to dismiss');
  }
}

export async function getRecentBranches(limit: number = 10): Promise<RecentBranch[]> {
  const response = await fetch(`/api/recent-branches?limit=${limit}`);
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch recent branches');
  }
  return response.json();
}

export interface RecentBranchesRefreshResponse {
  branches: RecentBranch[];
  fetched_count: number;
}

export async function refreshRecentBranches(): Promise<RecentBranchesRefreshResponse> {
  const response = await fetch('/api/recent-branches/refresh', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to refresh recent branches');
  return response.json();
}

export async function getGitGraph(
  workspaceId: string,
  opts?: {
    maxTotal?: number;
    mainContext?: number;
    /** @deprecated Use maxTotal instead */
    maxCommits?: number;
    /** @deprecated Use mainContext instead */
    context?: number;
  }
): Promise<GitGraphResponse> {
  const params = new URLSearchParams();
  if (opts?.maxTotal) params.set('max_total', String(opts.maxTotal));
  if (opts?.mainContext) params.set('main_context', String(opts.mainContext));
  // For backward compatibility, also accept old parameter names
  if (opts?.maxCommits !== undefined) params.set('max_commits', String(opts.maxCommits));
  if (opts?.context !== undefined) params.set('context', String(opts.context));
  const qs = params.toString();
  const url = `/api/workspaces/${encodeURIComponent(workspaceId)}/git-graph${qs ? `?${qs}` : ''}`;
  const response = await fetch(url);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch git graph');
  return response.json();
}

export async function getCommitDetail(
  workspaceId: string,
  commitHash: string
): Promise<GitCommitDetailResponse> {
  const url = `/api/workspaces/${encodeURIComponent(workspaceId)}/git-commit/${encodeURIComponent(commitHash)}`;
  const response = await fetch(url);
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch commit detail');
  }
  return response.json();
}

export async function getPRs(): Promise<PRsResponse> {
  const response = await fetch('/api/prs');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch PRs');
  return response.json();
}

export async function refreshPRs(): Promise<PRRefreshResponse> {
  const response = await fetch('/api/prs/refresh', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to refresh PRs');
  return response.json();
}

export async function checkoutPR(repoUrl: string, prNumber: number): Promise<PRCheckoutResponse> {
  const response = await fetch('/api/prs/checkout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ repo_url: repoUrl, pr_number: prNumber }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to checkout PR');
  }
  return response.json();
}

// ============================================================================
// Remote Flavor API
// ============================================================================

export async function getRemoteFlavors(): Promise<RemoteFlavor[]> {
  const response = await fetch('/api/config/remote-flavors');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch remote flavors');
  return response.json();
}

export async function createRemoteFlavor(
  request: RemoteFlavorCreateRequest
): Promise<RemoteFlavor> {
  const response = await fetch('/api/config/remote-flavors', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to create remote flavor');
  }
  return response.json();
}

export async function updateRemoteFlavor(
  id: string,
  request: RemoteFlavorCreateRequest
): Promise<RemoteFlavor> {
  const response = await fetch(`/api/config/remote-flavors/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to update remote flavor');
  }
  return response.json();
}

export async function deleteRemoteFlavor(id: string): Promise<void> {
  const response = await fetch(`/api/config/remote-flavors/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to delete remote flavor');
  }
}

export async function dismissEscalation(): Promise<void> {
  const response = await fetch('/api/escalate', {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to dismiss escalation');
  }
}

// ============================================================================
// Remote Host API
// ============================================================================

export async function getRemoteHosts(): Promise<RemoteHost[]> {
  const response = await fetch('/api/remote/hosts');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch remote hosts');
  return response.json();
}

export async function getRemoteFlavorStatuses(): Promise<RemoteFlavorStatus[]> {
  const response = await fetch('/api/remote/flavor-statuses');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch remote flavor statuses');
  return response.json();
}

export async function connectRemoteHost(request: RemoteHostConnectRequest): Promise<RemoteHost> {
  const response = await fetch('/api/remote/hosts/connect', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to connect to remote host');
  }
  return response.json();
}

export async function reconnectRemoteHost(hostId: string): Promise<RemoteHost> {
  const response = await fetch(`/api/remote/hosts/${encodeURIComponent(hostId)}/reconnect`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to reconnect to remote host');
  }
  return response.json();
}

export async function disconnectRemoteHost(hostId: string): Promise<void> {
  const response = await fetch(`/api/remote/hosts/${encodeURIComponent(hostId)}`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to disconnect remote host');
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
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
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
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
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
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(files ? { files } : {}),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to discard changes');
  }
  return response.json();
}

export async function gitUncommit(
  workspaceId: string,
  hash: string
): Promise<{ success: boolean; message: string }> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/git-uncommit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ hash }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to uncommit');
  }
  return response.json();
}

export interface CommitPromptResponse {
  prompt: string;
}

export interface CommitFile {
  path: string;
  added: number;
  deleted: number;
}

export interface CommitMessageResponse {
  message: string;
  files: CommitFile[];
}

// Fetch the commit prompt template from the backend.
export async function getCommitPrompt(): Promise<string> {
  const response = await fetch('/api/commit/prompt');
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch commit prompt');
  }
  const data: CommitPromptResponse = await response.json();
  return data.prompt;
}

// Generate a commit message using oneshot.
export async function generateCommitMessage(workspaceId: string): Promise<CommitMessageResponse> {
  const response = await fetch('/api/commit/generate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ workspace_id: workspaceId }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to generate commit message');
  }
  return response.json();
}

const PRE_COMMIT_INSTRUCTIONS =
  'Do the necessary precommit steps first (e.g., run linters, formatters, tests).';

export async function spawnCommitSession(
  workspaceId: string,
  repo: string,
  branch: string,
  selectedFiles: string[]
): Promise<SpawnResult[]> {
  // Fetch config to get the configured commit message target
  const config = await getConfig();
  const target = config.commit_message?.target;
  if (!target) {
    throw new Error('No commit message target configured');
  }

  // Fetch the base prompt template from the backend
  const promptTemplate = await getCommitPrompt();
  const fileList = selectedFiles.join('\n');

  // Build prompt: base template + pre-commit instructions + file list
  const prompt = promptTemplate + '\n\n' + PRE_COMMIT_INSTRUCTIONS + '\n\nFiles:\n' + fileList;

  const spawnRequest: SpawnRequest = {
    repo,
    branch,
    nickname: 'git commit',
    prompt,
    targets: { [target]: 1 },
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
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch dev status');
  return response.json();
}

export async function devRebuild(
  workspaceId: string,
  type: 'frontend' | 'backend' | 'both'
): Promise<{ status: string }> {
  const response = await fetch('/api/dev/rebuild', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ workspace_id: workspaceId, type }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to trigger rebuild');
  }
  return response.json();
}

// ============================================================================
// Lore API
// ============================================================================

export async function getLoreProposals(repoName: string): Promise<LoreProposalsResponse> {
  const res = await fetch(`/api/lore/${encodeURIComponent(repoName)}/proposals`);
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore proposals');
  return res.json();
}

export async function getLoreProposal(repoName: string, id: string): Promise<LoreProposal> {
  const res = await fetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(id)}`
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore proposal');
  return res.json();
}

export async function applyLoreProposal(
  repoName: string,
  id: string,
  overrides?: Record<string, string>
): Promise<LoreApplyResponse> {
  const res = await fetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(id)}/apply`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: overrides ? JSON.stringify({ overrides }) : undefined,
    }
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to apply lore proposal');
  return res.json();
}

export async function dismissLoreProposal(repoName: string, id: string): Promise<void> {
  const res = await fetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(id)}/dismiss`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to dismiss lore proposal');
}

export async function getLoreEntries(
  repoName: string,
  filters?: { state?: string; agent?: string; type?: string; limit?: number }
): Promise<LoreEntriesResponse> {
  const params = new URLSearchParams();
  if (filters?.state) params.set('state', filters.state);
  if (filters?.agent) params.set('agent', filters.agent);
  if (filters?.type) params.set('type', filters.type);
  if (filters?.limit) params.set('limit', String(filters.limit));
  const qs = params.toString();
  const res = await fetch(`/api/lore/${encodeURIComponent(repoName)}/entries${qs ? '?' + qs : ''}`);
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore entries');
  return res.json();
}

export async function triggerLoreCuration(
  repoName: string
): Promise<{ status: string; proposal_id?: string }> {
  const res = await fetch(`/api/lore/${encodeURIComponent(repoName)}/curate`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!res.ok) await parseErrorResponse(res, 'Failed to trigger lore curation');
  return res.json();
}

export async function getLoreStatus(): Promise<LoreStatusResponse> {
  const res = await fetch('/api/lore/status');
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore status');
  return res.json();
}

// ============================================================================
// Remote Access API
// ============================================================================

export async function remoteAccessOn(): Promise<void> {
  const response = await fetch('/api/remote-access/on', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to start remote access');
  }
}

export async function remoteAccessOff(): Promise<void> {
  const response = await fetch('/api/remote-access/off', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to stop remote access');
  }
}

export async function setRemoteAccessPassword(password: string): Promise<void> {
  const response = await fetch('/api/remote-access/set-password', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ password }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to set password');
  }
}

export async function testRemoteAccessNotification(): Promise<void> {
  const response = await fetch('/api/remote-access/test-notification', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to send test notification');
  }
}
