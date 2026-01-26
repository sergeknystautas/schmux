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
}

export interface WorkspaceResponse {
  id: string;
  repo: string;
  branch: string;
  branch_url?: string;
  path: string;
  session_count: number;
  sessions: SessionResponse[];
  git_ahead: number;
  git_behind: number;
  git_lines_added: number;
  git_lines_removed: number;
  git_files_changed: number;
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
}

export interface RunTargetResponse {
  name: string;
  type: string;
  command: string;
  source?: string;
}

export interface QuickLaunchPreset {
  name: string;
  target: string;
  prompt?: string | null;
}

export interface BuiltinQuickLaunchCookbook {
  name: string;
  target: string;
  prompt: string;
}

export type { ConfigResponse, ConfigUpdateRequest } from './types.generated';

export interface SpawnRequest {
  repo: string;
  branch: string;
  prompt: string;
  nickname: string;
  targets: Record<string, number>;
  workspace_id?: string;
}

export interface SpawnResult {
  session_id?: string;
  workspace_id?: string;
  target: string;
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

export interface VariantResponse {
  name: string;
  display_name: string;
  base_tool: string;
  required_secrets: string[];
  usage_url: string;
  configured: boolean;
}

export interface VariantsResponse {
  variants: VariantResponse[];
}

export interface OverlayInfo {
  repo_name: string;
  path: string;
  exists: boolean;
  file_count: number;
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

export interface LinearSyncResponse {
  success: boolean;
  message: string;
}
