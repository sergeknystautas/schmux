import type {
  ApiError,
  BuiltinQuickLaunchCookbook,
  ConfigResponse,
  ConfigUpdateRequest,
  DetectToolsResponse,
  DiffExternalResponse,
  DiffResponse,
  CommitDetailResponse,
  CommitGraphResponse,
  LinearSyncResponse,
  LinearSyncResolveConflictResponse,
  LoreEntriesResponse,
  LoreMergeApplyResult,
  LoreProposal,
  LoreProposalsResponse,
  LoreRuleStatus,
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
  RemoteProfile,
  RemoteProfileCreateRequest,
  RemoteProfileStatus,
  RemoteHost,
  RemoteHostConnectRequest,
  RepofeedListResponse,
  RepofeedRepoResponse,
  ScanResult,
  SpawnRequest,
  SpawnResult,
  SubredditResponse,
  SuggestBranchRequest,
  SuggestBranchResponse,
  TLSValidateResponse,
  WorkspaceResponse,
  WorkspacePreview,
} from './types';
import type {
  CreateSpawnEntryRequest,
  Persona,
  PersonaListResponse,
  PersonaCreateRequest,
  PersonaUpdateRequest,
  PromptHistoryResponse,
  SpawnEntriesResponse,
  SpawnEntry,
  UpdateSpawnEntryRequest,
  Features,
  EnvironmentResponse,
} from './types.generated';
import { csrfHeaders } from './csrf';
import { transport } from './transport';

// All fetch calls in this module route through the active transport.
// This enables the demo shell to intercept API calls with mock responses.
function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return transport.fetch(input, init);
}

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

// Parse error from a non-ok Response, trying JSON then falling back to text.
// Reads the body once as text to avoid "body stream already read" errors.
export async function parseErrorResponse(response: Response, fallback: string): Promise<never> {
  let message = fallback;
  try {
    const text = await response.text();
    try {
      const err = JSON.parse(text);
      message = err.error || text || fallback;
    } catch {
      message = text || fallback;
    }
  } catch {
    // Body unreadable — use fallback
  }
  throw new Error(message);
}

export async function getSessions(): Promise<WorkspaceResponse[]> {
  const response = await apiFetch('/api/sessions');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch sessions');
  return response.json();
}

export async function getConfig(): Promise<ConfigResponse> {
  const response = await apiFetch('/api/config');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch config');
  return response.json();
}

export async function getFeatures(): Promise<Features> {
  const response = await apiFetch('/api/features');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch features');
  return response.json();
}

export async function spawnSessions(request: SpawnRequest): Promise<SpawnResult[]> {
  const response = await apiFetch('/api/spawn', {
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
  const response = await apiFetch('/api/check-branch-conflict', {
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
  const response = await apiFetch('/api/suggest-branch', {
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
  const response = await apiFetch('/api/prepare-branch-spawn', {
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
  const response = await apiFetch(`/api/sessions/${sessionId}/dispose`, {
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
  const response = await apiFetch(`/api/sessions-nickname/${sessionId}`, {
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
  const response = await apiFetch(`/api/workspaces/${workspaceId}/dispose`, {
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
  const response = await apiFetch(`/api/workspaces/${workspaceId}/dispose-all`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to dispose workspace and sessions');
  }
  return response.json();
}

export async function createTab(
  workspaceId: string,
  tab: {
    kind: string;
    label: string;
    route: string;
    closable: boolean;
    meta?: Record<string, string>;
  }
): Promise<{ id: string; status: string }> {
  const response = await apiFetch(`/api/workspaces/${workspaceId}/tabs`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(tab),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to create tab');
  }
  return response.json();
}

export async function closeTab(workspaceId: string, tabId: string): Promise<{ status: string }> {
  const response = await apiFetch(`/api/workspaces/${workspaceId}/tabs/${tabId}`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to close tab');
  }
  return response.json();
}

export async function getDiff(workspaceId: string): Promise<DiffResponse> {
  const response = await apiFetch(`/api/diff/${workspaceId}`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch diff');
  return response.json();
}

// Get a file from the workspace for image preview
export function getWorkspaceFileUrl(workspaceId: string, filePath: string): string {
  const encoded = encodeURIComponent(filePath);
  return `/api/file/${workspaceId}/${encoded}`;
}

// Fetch file content as text (for markdown preview)
export async function getFileContent(workspaceId: string, filePath: string): Promise<string> {
  const url = getWorkspaceFileUrl(workspaceId, filePath);
  const response = await apiFetch(url);
  if (!response.ok) {
    if (response.status === 404) throw new Error('File not found');
    await parseErrorResponse(response, 'Failed to fetch file');
  }
  return response.text();
}

export async function getAuthMe(): Promise<{ login: string; avatar_url?: string; name?: string }> {
  const response = await apiFetch('/auth/me');
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch auth user');
  }
  return response.json();
}

export async function scanWorkspaces(): Promise<ScanResult> {
  const response = await apiFetch('/api/workspaces/scan', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to scan workspaces');
  return response.json();
}

export async function updateConfig(
  request: ConfigUpdateRequest
): Promise<{ status: string; message?: string; warning?: string; warnings?: string[] }> {
  const response = await apiFetch('/api/config', {
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
  client_id: string;
  client_secret_set: boolean;
}> {
  const response = await apiFetch('/api/auth/secrets');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch auth secrets');
  return response.json();
}

export async function saveAuthSecrets(payload: {
  client_id: string;
  client_secret?: string;
}): Promise<{ status: string }> {
  const response = await apiFetch('/api/auth/secrets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to save auth secrets');
  }
  return response.json();
}

export async function validateTLS(certPath: string, keyPath: string): Promise<TLSValidateResponse> {
  const response = await apiFetch('/api/tls/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ cert_path: certPath, key_path: keyPath }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to validate TLS certificates');
  }
  return response.json();
}

export async function openVSCode(
  workspaceId: string,
  options?: { mode?: 'uri' }
): Promise<OpenVSCodeResponse> {
  const params = options?.mode ? `?mode=${options.mode}` : '';
  const response = await apiFetch(`/api/open-vscode/${workspaceId}${params}`, {
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
  const response = await apiFetch(`/api/diff-external/${workspaceId}`, {
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
  const response = await apiFetch('/api/detect-tools');
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
  const response = await apiFetch(`/api/models/${modelId}/secrets`, {
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
  const response = await apiFetch(`/api/models/${modelId}/secrets`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to remove model secrets');
  }
  return response.json();
}

export async function getOverlays(): Promise<OverlaysResponse> {
  const response = await apiFetch('/api/overlays');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch overlays');
  return response.json();
}

export async function refreshOverlay(workspaceId: string): Promise<{ status: string }> {
  const response = await apiFetch(`/api/workspaces/${workspaceId}/refresh-overlay`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to refresh overlay');
  }
  return response.json();
}

export async function scanOverlayFiles(
  workspaceId: string,
  repoName: string
): Promise<OverlayScanResponse> {
  const response = await apiFetch('/api/overlays/scan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ workspace_id: workspaceId, repo_name: repoName }),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to scan overlay files');
  return response.json();
}

export async function addOverlayFiles(req: OverlayAddRequest): Promise<OverlayAddResponse> {
  const response = await apiFetch('/api/overlays/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to add overlay files');
  return response.json();
}

export async function dismissOverlayNudge(repoName: string): Promise<{ status: string }> {
  const response = await apiFetch('/api/overlays/dismiss-nudge', {
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
  const response = await apiFetch('/api/builtin-quick-launch');
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch built-in quick launch presets');
  }
  return response.json();
}

export async function linearSyncFromMain(
  workspaceId: string,
  hash: string
): Promise<LinearSyncResponse> {
  const response = await apiFetch(`/api/workspaces/${workspaceId}/linear-sync-from-main`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ hash }),
  });
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
  const response = await apiFetch(`/api/workspaces/${workspaceId}/linear-sync-to-main`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to sync to main');
  }
  return response.json();
}

export async function pushToBranch(workspaceId: string): Promise<LinearSyncResponse> {
  const response = await apiFetch(`/api/workspaces/${workspaceId}/push-to-branch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to push to branch');
  }
  return response.json();
}

export async function linearSyncResolveConflict(
  workspaceId: string
): Promise<LinearSyncResolveConflictResponse> {
  const response = await apiFetch(`/api/workspaces/${workspaceId}/linear-sync-resolve-conflict`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
  });
  if (!response.ok && response.status !== 202) {
    await parseErrorResponse(response, 'Failed to start conflict resolution');
  }
  return response.json();
}

export async function getRecentBranches(limit: number = 10): Promise<RecentBranch[]> {
  const response = await apiFetch(`/api/recent-branches?limit=${limit}`);
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
  const response = await apiFetch('/api/recent-branches/refresh', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to refresh recent branches');
  return response.json();
}

export async function getCommitGraph(
  workspaceId: string,
  opts?: {
    maxTotal?: number;
    mainContext?: number;
    /** @deprecated Use maxTotal instead */
    maxCommits?: number;
    /** @deprecated Use mainContext instead */
    context?: number;
  }
): Promise<CommitGraphResponse> {
  const params = new URLSearchParams();
  if (opts?.maxTotal) params.set('max_total', String(opts.maxTotal));
  if (opts?.mainContext) params.set('main_context', String(opts.mainContext));
  // For backward compatibility, also accept old parameter names
  if (opts?.maxCommits !== undefined) params.set('max_commits', String(opts.maxCommits));
  if (opts?.context !== undefined) params.set('context', String(opts.context));
  const qs = params.toString();
  const url = `/api/workspaces/${encodeURIComponent(workspaceId)}/commit-graph${qs ? `?${qs}` : ''}`;
  const response = await apiFetch(url);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch commit graph');
  return response.json();
}

export async function getCommitDetail(
  workspaceId: string,
  commitHash: string
): Promise<CommitDetailResponse> {
  const url = `/api/workspaces/${encodeURIComponent(workspaceId)}/commit-detail/${encodeURIComponent(commitHash)}`;
  const response = await apiFetch(url);
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch commit detail');
  }
  return response.json();
}

export async function getPRs(): Promise<PRsResponse> {
  const response = await apiFetch('/api/prs');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch PRs');
  return response.json();
}

export async function refreshPRs(): Promise<PRRefreshResponse> {
  const response = await apiFetch('/api/prs/refresh', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to refresh PRs');
  return response.json();
}

export async function checkoutPR(repoUrl: string, prNumber: number): Promise<PRCheckoutResponse> {
  const response = await apiFetch('/api/prs/checkout', {
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
// Remote Profile API
// ============================================================================

export async function getRemoteProfiles(): Promise<RemoteProfile[]> {
  const response = await apiFetch('/api/config/remote-profiles');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch remote profiles');
  return response.json();
}

export async function createRemoteProfile(
  request: RemoteProfileCreateRequest
): Promise<RemoteProfile> {
  const response = await apiFetch('/api/config/remote-profiles', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to create remote profile');
  }
  return response.json();
}

export async function updateRemoteProfile(
  id: string,
  request: RemoteProfileCreateRequest
): Promise<RemoteProfile> {
  const response = await apiFetch(`/api/config/remote-profiles/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to update remote profile');
  }
  return response.json();
}

export async function deleteRemoteProfile(id: string): Promise<void> {
  const response = await apiFetch(`/api/config/remote-profiles/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to delete remote profile');
  }
}

// ============================================================================
// Remote Host API
// ============================================================================

export async function getRemoteHosts(): Promise<RemoteHost[]> {
  const response = await apiFetch('/api/remote/hosts');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch remote hosts');
  return response.json();
}

export async function getRemoteProfileStatuses(): Promise<RemoteProfileStatus[]> {
  const response = await apiFetch('/api/remote/profile-statuses');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch remote profile statuses');
  return response.json();
}

export async function connectRemoteHost(request: RemoteHostConnectRequest): Promise<RemoteHost> {
  const response = await apiFetch('/api/remote/hosts/connect', {
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
  const response = await apiFetch(`/api/remote/hosts/${encodeURIComponent(hostId)}/reconnect`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to reconnect to remote host');
  }
  return response.json();
}

export async function disconnectRemoteHost(hostId: string): Promise<void> {
  const response = await apiFetch(`/api/remote/hosts/${encodeURIComponent(hostId)}`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to disconnect remote host');
  }
}

export async function dismissRemoteHost(hostId: string): Promise<void> {
  const response = await apiFetch(`/api/remote/hosts/${encodeURIComponent(hostId)}?dismiss=true`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to dismiss remote host');
  }
}

// ============================================================================
// Git Commit Workflow API
// ============================================================================

export async function commitStage(
  workspaceId: string,
  files: string[]
): Promise<{ success: boolean; message: string }> {
  const response = await apiFetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/stage`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ files }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to stage files');
  }
  return response.json();
}

export async function commitAmend(
  workspaceId: string,
  files: string[]
): Promise<{ success: boolean; message: string }> {
  const response = await apiFetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/amend`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ files }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to amend commit');
  }
  return response.json();
}

export async function commitDiscard(
  workspaceId: string,
  files?: string[]
): Promise<{ success: boolean; message: string }> {
  const response = await apiFetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/discard`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(files ? { files } : {}),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to discard changes');
  }
  return response.json();
}

export async function commitUncommit(
  workspaceId: string,
  hash: string
): Promise<{ success: boolean; message: string }> {
  const response = await apiFetch(`/api/workspaces/${encodeURIComponent(workspaceId)}/uncommit`, {
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
  const response = await apiFetch('/api/commit/prompt');
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to fetch commit prompt');
  }
  const data: CommitPromptResponse = await response.json();
  return data.prompt;
}

// Generate a commit message using oneshot.
export async function generateCommitMessage(workspaceId: string): Promise<CommitMessageResponse> {
  const response = await apiFetch('/api/commit/generate', {
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
  const response = await apiFetch('/api/dev/status');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch dev status');
  return response.json();
}

export async function devRebuild(
  workspaceId: string,
  type: 'frontend' | 'backend' | 'both'
): Promise<{ status: string }> {
  const response = await apiFetch('/api/dev/rebuild', {
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
  const res = await apiFetch(`/api/lore/${encodeURIComponent(repoName)}/proposals`);
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore proposals');
  return res.json();
}

export async function getLoreProposal(repoName: string, id: string): Promise<LoreProposal> {
  const res = await apiFetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(id)}`
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore proposal');
  return res.json();
}

export async function dismissLoreProposal(repoName: string, id: string): Promise<void> {
  const res = await apiFetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(id)}/dismiss`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to dismiss lore proposal');
}

export async function updateLoreRule(
  repoName: string,
  proposalID: string,
  ruleID: string,
  update: { status?: LoreRuleStatus; text?: string; chosen_layer?: string }
): Promise<LoreProposal> {
  const res = await apiFetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(proposalID)}/rules/${encodeURIComponent(ruleID)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify(update),
    }
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to update lore rule');
  return res.json();
}

export async function startLoreMerge(repoName: string, proposalID: string): Promise<void> {
  const res = await apiFetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(proposalID)}/merge`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to start merge');
}

export async function applyLoreMerge(
  repoName: string,
  proposalID: string,
  merges: { layer: string; content: string }[],
  autoCommit = false
): Promise<{ results: LoreMergeApplyResult[] }> {
  const res = await apiFetch(
    `/api/lore/${encodeURIComponent(repoName)}/proposals/${encodeURIComponent(proposalID)}/apply-merge`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify({ merges, auto_commit: autoCommit }),
    }
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to apply lore merge');
  return res.json();
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
  const res = await apiFetch(
    `/api/lore/${encodeURIComponent(repoName)}/entries${qs ? '?' + qs : ''}`
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore entries');
  return res.json();
}

export async function clearLoreEntries(
  repoName: string
): Promise<{ status: string; cleared: number }> {
  const res = await apiFetch(`/api/lore/${encodeURIComponent(repoName)}/entries`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!res.ok) await parseErrorResponse(res, 'Failed to clear lore entries');
  return res.json();
}

export async function startLoreCuration(repoName: string): Promise<{ id: string; status: string }> {
  const res = await apiFetch(`/api/lore/${encodeURIComponent(repoName)}/curate`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!res.ok) {
    await parseErrorResponse(res, 'Failed to trigger lore curation');
  }
  return res.json();
}

export async function getLoreStatus(): Promise<LoreStatusResponse> {
  const res = await apiFetch('/api/lore/status');
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch lore status');
  return res.json();
}

export interface CurationRunInfo {
  id: string;
  size_bytes: number;
  created_at: string;
}

export async function getLoreCurations(repoName: string): Promise<{ runs: CurationRunInfo[] }> {
  const res = await apiFetch(`/api/lore/${encodeURIComponent(repoName)}/curations`);
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch curation runs');
  return res.json();
}

export async function getLoreCurationLog(
  repoName: string,
  id: string
): Promise<{ events: Record<string, unknown>[] }> {
  const res = await apiFetch(
    `/api/lore/${encodeURIComponent(repoName)}/curations/${encodeURIComponent(id)}/log`
  );
  if (!res.ok) await parseErrorResponse(res, 'Failed to fetch curation log');
  return res.json();
}

// ============================================================================
// Remote Access API
// ============================================================================

export async function remoteAccessOn(): Promise<void> {
  const response = await apiFetch('/api/remote-access/on', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to start remote access');
  }
}

export async function remoteAccessOff(): Promise<void> {
  const response = await apiFetch('/api/remote-access/off', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to stop remote access');
  }
}

export async function setRemoteAccessPassword(password: string): Promise<void> {
  const response = await apiFetch('/api/remote-access/set-password', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ password }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to set password');
  }
}

export async function testRemoteAccessNotification(): Promise<void> {
  const response = await apiFetch('/api/remote-access/test-notification', {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to send test notification');
  }
}

// ============================================================================
// Persona API
// ============================================================================

export async function getPersonas(): Promise<PersonaListResponse> {
  const response = await apiFetch('/api/personas');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch personas');
  return response.json();
}

export async function createPersona(req: PersonaCreateRequest): Promise<Persona> {
  const response = await apiFetch('/api/personas', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to create persona');
  return response.json();
}

export async function updatePersona(id: string, req: PersonaUpdateRequest): Promise<Persona> {
  const response = await apiFetch(`/api/personas/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to update persona');
  return response.json();
}

export async function deletePersona(id: string): Promise<void> {
  const response = await apiFetch(`/api/personas/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to delete persona');
}

// ============================================================================
// Subreddit Digest API
// ============================================================================

export async function getSubreddit(): Promise<SubredditResponse> {
  const response = await apiFetch('/api/subreddit');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch subreddit digest');
  return response.json();
}

// ============================================================================
// Repofeed API
// ============================================================================

export async function getRepofeedList(): Promise<RepofeedListResponse> {
  const response = await apiFetch('/api/repofeed');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch repofeed');
  return response.json();
}

export async function getRepofeedRepo(slug: string): Promise<RepofeedRepoResponse> {
  const response = await apiFetch(`/api/repofeed/${encodeURIComponent(slug)}`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch repofeed for repo');
  return response.json();
}

// ============================================================================
// Spawn Entries API (Emergence)
// ============================================================================

export async function getSpawnEntries(repo: string): Promise<SpawnEntry[]> {
  const response = await fetch(`/api/emergence/${encodeURIComponent(repo)}/entries`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch spawn entries');
  const data: SpawnEntriesResponse = await response.json();
  return data.entries;
}

export async function getAllSpawnEntries(repo: string): Promise<SpawnEntry[]> {
  const response = await fetch(`/api/emergence/${encodeURIComponent(repo)}/entries/all`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch all spawn entries');
  const data: SpawnEntriesResponse = await response.json();
  return data.entries;
}

export async function createSpawnEntry(
  repo: string,
  req: CreateSpawnEntryRequest
): Promise<SpawnEntry> {
  const response = await fetch(`/api/emergence/${encodeURIComponent(repo)}/entries`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to create spawn entry');
  return response.json();
}

export async function updateSpawnEntry(
  repo: string,
  id: string,
  req: UpdateSpawnEntryRequest
): Promise<SpawnEntry> {
  const response = await fetch(
    `/api/emergence/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify(req),
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to update spawn entry');
  return response.json();
}

export async function deleteSpawnEntry(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/emergence/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to delete spawn entry');
}

export async function pinSpawnEntry(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/emergence/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}/pin`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to pin spawn entry');
}

export async function dismissSpawnEntry(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/emergence/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}/dismiss`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to dismiss spawn entry');
}

export async function recordSpawnEntryUse(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/emergence/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}/use`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to record spawn entry use');
}

export async function getPromptHistory(repo: string): Promise<PromptHistoryResponse> {
  const response = await fetch(`/api/emergence/${encodeURIComponent(repo)}/prompt-history`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch prompt history');
  return response.json();
}

// ============================================================================
// Environment API
// ============================================================================

export async function getEnvironment(): Promise<EnvironmentResponse> {
  const response = await apiFetch('/api/environment');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch environment');
  return response.json();
}

export async function syncEnvironmentVar(key: string): Promise<void> {
  const response = await apiFetch('/api/environment/sync', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ key }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to sync environment variable');
  }
}

// ============================================================================
// Recyclable Workspaces API
// ============================================================================

export interface RecyclableWorkspacesResponse {
  total: number;
  by_repo: Record<string, number>;
}

export interface PurgeWorkspacesResponse {
  status: string;
  purged: number;
}

export async function getRecyclableWorkspaces(): Promise<RecyclableWorkspacesResponse> {
  const response = await apiFetch('/api/workspaces/recyclable');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch recyclable workspaces');
  return response.json();
}

export async function purgeWorkspaces(): Promise<PurgeWorkspacesResponse> {
  const response = await apiFetch('/api/workspaces/purge', {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to purge workspaces');
  return response.json();
}

// Timelapse Recording API

export interface TimelapseRecording {
  RecordingID: string;
  SessionID: string;
  StartTime: string;
  ModTime: string;
  Duration: number;
  FileSize: number;
  Width: number;
  Height: number;
  InProgress: boolean;
  HasCompressed: boolean;
  Path: string;
}

export async function getTimelapseRecordings(): Promise<TimelapseRecording[]> {
  const response = await apiFetch('/api/timelapse');
  if (!response.ok) return [];
  return response.json();
}

export async function exportTimelapseRecording(recordingId: string): Promise<void> {
  await apiFetch(`/api/timelapse/${recordingId}/export`, {
    method: 'POST',
    headers: csrfHeaders(),
  });
}

export async function deleteTimelapseRecording(recordingId: string): Promise<void> {
  await apiFetch(`/api/timelapse/${recordingId}`, {
    method: 'DELETE',
    headers: csrfHeaders(),
  });
}
