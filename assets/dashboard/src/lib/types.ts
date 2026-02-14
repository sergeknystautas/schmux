export interface SessionResponse {
  id: string;
  target: string;
  branch: string;
  branch_url?: string;
  nickname?: string;
  created_at: string;
  last_output_at?: string;
  running: boolean;
  attach_cmd: string;
  nudge_state?: string;
  nudge_summary?: string;
  nudge_seq?: number;
  // Remote session fields
  remote_host_id?: string;
  remote_pane_id?: string;
  remote_hostname?: string;
  remote_flavor_name?: string;
}

export interface WorkspaceResponse {
  id: string;
  repo: string;
  repo_name?: string;
  default_branch?: string;
  branch: string;
  branch_url?: string;
  path: string;
  session_count: number;
  sessions: SessionResponse[];
  quick_launch?: string[];
  git_ahead: number;
  git_behind: number;
  git_lines_added: number;
  git_lines_removed: number;
  git_files_changed: number;
  remote_host_id?: string;
  remote_host_status?: string;
  remote_flavor_name?: string;
  remote_flavor?: string;
  vcs?: string; // "git", "sapling", etc. Omitted defaults to "git".
  conflict_on_branch?: string; // Branch where sync conflict was detected
  commits_synced_with_remote?: boolean; // true if local HEAD matches origin/{branch}
  previews?: WorkspacePreview[];
}

export interface WorkspacePreview {
  id: string;
  workspace_id: string;
  target_host: string;
  target_port: number;
  proxy_port: number;
  url: string;
  status: 'ready' | 'degraded';
  last_error?: string;
}

export interface SessionWithWorkspace extends SessionResponse {
  workspace_id: string;
  workspace_path: string;
  repo: string;
  branch: string;
}

export interface RepoResponse {
  name: string;
  url: string;
  default_branch?: string; // Detected default branch (main, master, etc.), omitted if not yet detected
}

export interface RunTargetResponse {
  name: string;
  type: string;
  command: string;
  source?: string;
}

export interface QuickLaunchPreset {
  name: string;
  command?: string; // shell command to run directly
  target?: string; // run target (claude, codex, model, etc.)
  prompt?: string | null; // prompt for the target
}

export interface BuiltinQuickLaunchCookbook {
  name: string;
  target: string;
  prompt: string;
}

import type { PullRequest } from './types.generated';

export type {
  ConfigResponse,
  ConfigUpdateRequest,
  GitGraphResponse,
  GitGraphNode,
  GitGraphBranch,
  Model,
  PRsResponse,
  PullRequest,
  PrReview,
  PrReviewUpdate,
  Notifications,
  NotificationsUpdate,
} from './types.generated';

export interface SpawnRequest {
  repo: string;
  branch: string;
  prompt: string;
  nickname: string;
  targets?: Record<string, number>; // target-based spawn
  command?: string; // command-based spawn (alternative to targets)
  workspace_id?: string;
  quick_launch_name?: string;
  resume?: boolean; // resume mode: use agent's resume command
  remote_flavor_id?: string; // optional: spawn on remote host
  new_branch?: string; // create new workspace with this branch from source workspace
}

export interface SpawnResult {
  session_id?: string;
  workspace_id?: string;
  target?: string; // for target-based spawns
  command?: string; // for command-based spawns
  prompt?: string;
  nickname?: string;
  error?: string;
}

export interface SuggestBranchRequest {
  prompt: string;
}

export interface SuggestBranchResponse {
  branch: string;
  nickname: string;
}

export interface DetectTool {
  name: string;
  command: string;
  source: string;
}

export interface DetectToolsResponse {
  tools: DetectTool[];
}

export interface OverlayPathInfo {
  path: string;
  source: 'builtin' | 'global' | 'repo';
  status: 'synced' | 'pending';
}

export interface OverlayScanCandidate {
  path: string;
  size: number;
  detected: boolean;
}

export interface OverlayScanResponse {
  candidates: OverlayScanCandidate[];
}

export interface OverlayAddRequest {
  workspace_id: string;
  repo_name: string;
  paths: string[];
  custom_paths: string[];
}

export interface OverlayAddResponse {
  success: boolean;
  copied: string[];
  registered: string[];
}

export interface OverlayInfo {
  repo_name: string;
  path: string;
  exists: boolean;
  file_count: number;
  declared_paths: OverlayPathInfo[];
  nudge_dismissed: boolean;
}

export interface OverlaysResponse {
  overlays: OverlayInfo[];
}

export interface FileDiff {
  old_path?: string;
  new_path?: string;
  old_content?: string;
  new_content?: string;
  status?: string;
  lines_added: number;
  lines_removed: number;
  is_binary: boolean;
}

export interface DiffResponse {
  workspace_id: string;
  repo: string;
  branch: string;
  files: FileDiff[];
}

export interface OpenVSCodeResponse {
  success: boolean;
  message: string;
}

export interface DiffExternalResponse {
  success: boolean;
  message: string;
}

export interface ScanWorkspace {
  id: string;
  repo: string;
  branch: string;
  path: string;
}

export interface WorkspaceChange {
  old: ScanWorkspace;
  new: ScanWorkspace;
}

export interface ScanResult {
  added: ScanWorkspace[];
  updated: WorkspaceChange[];
  removed: ScanWorkspace[];
}

export interface TerminalSize {
  width: number;
  height: number;
}

export type ApiError = Error & { isConflict?: boolean };

export type PendingNavigation = { type: 'session'; id: string } | { type: 'workspace'; id: string };

export interface LinearSyncResponse {
  success: boolean;
  success_count?: number;
  conflicting_hash?: string;
  branch?: string;
  is_pre_commit_hook_error?: boolean;
  pre_commit_error_detail?: string;
}

export interface ConflictResolution {
  local_commit: string;
  local_commit_message: string;
  all_resolved: boolean;
  confidence: string;
  summary: string;
  files: string[];
}

export interface LinearSyncResolveConflictResponse {
  started: boolean;
  workspace_id?: string;
  message?: string;
}

export interface LinearSyncResolveConflictStep {
  action: string;
  status: string;
  message: string;
  at: string;
  local_commit?: string;
  local_commit_message?: string;
  files?: string[];
  confidence?: string;
  summary?: string;
  created?: boolean;
}

export interface LinearSyncResolveConflictStatePayload {
  type: 'linear_sync_resolve_conflict';
  workspace_id: string;
  status: 'in_progress' | 'done' | 'failed';
  hash?: string;
  started_at: string;
  finished_at?: string;
  message?: string;
  steps: LinearSyncResolveConflictStep[];
  resolutions?: ConflictResolution[];
}

export interface RecentBranch {
  repo_name: string;
  repo_url: string;
  branch: string;
  commit_date: string;
  subject: string;
}

export interface PRRefreshResponse {
  prs: PullRequest[];
  fetched_count: number;
  error?: string;
  retry_after_sec?: number;
}

export interface PRCheckoutResponse {
  workspace_id: string;
  session_id: string;
}

// Remote workspace types
export interface RemoteFlavor {
  id: string;
  flavor: string;
  display_name: string;
  vcs: string;
  workspace_path: string;
  connect_command?: string;
  reconnect_command?: string;
  provision_command?: string;
  hostname_regex?: string;
  vscode_command_template?: string;
}

export interface RemoteFlavorStatus {
  flavor: RemoteFlavor;
  connected: boolean;
  status: 'provisioning' | 'connecting' | 'connected' | 'disconnected' | 'expired' | 'reconnecting';
  hostname: string;
  host_id: string;
}

export interface RemoteHost {
  id: string;
  flavor_id: string;
  hostname: string;
  uuid: string;
  connected_at: string;
  expires_at: string;
  status: 'provisioning' | 'connecting' | 'connected' | 'disconnected' | 'expired' | 'reconnecting';
  provisioned: boolean;
  provisioning_session_id?: string; // Local tmux session ID for interactive provisioning terminal
}

export interface RemoteFlavorCreateRequest {
  display_name: string;
  flavor: string;
  workspace_path: string;
  vcs: string;
  connect_command?: string;
  reconnect_command?: string;
  provision_command?: string;
  hostname_regex?: string;
  vscode_command_template?: string;
}

export interface RemoteHostConnectRequest {
  flavor_id: string;
}

export interface RemoteSpawnRequest {
  flavor_id: string;
  target: string;
  prompt: string;
  nickname: string;
}

export interface LoreEntry {
  ts: string;
  ws?: string;
  agent?: string;
  type?: string;
  text?: string;
  state_change?: string;
  entry_ts?: string;
  proposal_id?: string;
}

export interface LoreProposal {
  id: string;
  repo: string;
  created_at: string;
  status: 'pending' | 'stale' | 'applied' | 'dismissed';
  source_count: number;
  sources: string[];
  file_hashes: Record<string, string>;
  proposed_files: Record<string, string>;
  diff_summary: string;
  entries_used: string[];
  entries_discarded?: Record<string, string>;
}

export interface LoreProposalsResponse {
  proposals: LoreProposal[];
}

export interface LoreEntriesResponse {
  entries: LoreEntry[];
}

export interface LoreApplyResponse {
  status: string;
  branch: string;
}
