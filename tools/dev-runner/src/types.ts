export type ProcessStatus = 'idle' | 'starting' | 'running' | 'stopped' | 'crashed' | 'building';

export interface RestartManifest {
  type: 'backend' | 'frontend' | 'both';
  workspace_path: string;
}

export interface BuildStatus {
  success: boolean;
  workspace_path: string;
  error: string;
  at: string;
}

export interface DevState {
  source_workspace: string;
}
