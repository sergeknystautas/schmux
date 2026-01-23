import type {
  ApiError,
  BuiltinQuickLaunchCookbook,
  ConfigResponse,
  ConfigUpdateRequest,
  DetectToolsResponse,
  DiffExternalResponse,
  DiffResponse,
  OpenVSCodeResponse,
  OverlaysResponse,
  ScanResult,
  SpawnRequest,
  SpawnResult,
  SuggestBranchRequest,
  SuggestBranchResponse,
  VariantsResponse,
  WorkspaceResponse,
} from './types';

// Extract error message from unknown catch value
export function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error) return err.message;
  if (typeof err === 'string') return err;
  return fallback;
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
    body: JSON.stringify(request)
  });
  if (!response.ok) throw new Error('Failed to spawn sessions');
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
    body: JSON.stringify(request)
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to suggest branch name');
  }
  return response.json();
}

export async function disposeSession(sessionId: string): Promise<{ status: string }> {
  const response = await fetch(`/api/dispose/${sessionId}`, { method: 'POST' });
  if (!response.ok) throw new Error('Failed to dispose session');
  return response.json();
}

export async function updateNickname(sessionId: string, nickname: string): Promise<{ status: string }> {
  const response = await fetch(`/api/sessions-nickname/${sessionId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ nickname })
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
  const response = await fetch(`/api/dispose-workspace/${workspaceId}`, { method: 'POST' });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.error || 'Failed to dispose workspace');
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

export async function updateConfig(request: ConfigUpdateRequest): Promise<{ status: string; message?: string; warning?: string; warnings?: string[] }> {
  const response = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request)
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

export async function getAuthSecretsStatus(): Promise<{ client_id_set: boolean; client_secret_set: boolean }> {
  const response = await fetch('/api/auth/secrets');
  if (!response.ok) throw new Error('Failed to fetch auth secrets');
  return response.json();
}

export async function saveAuthSecrets(payload: { client_id: string; client_secret: string }): Promise<{ status: string }> {
  const response = await fetch('/api/auth/secrets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
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
    headers: { 'Content-Type': 'application/json' }
  });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.message || response.statusText || 'Failed to open VS Code');
  }
  return response.json();
}

export async function diffExternal(workspaceId: string, command?: string): Promise<DiffExternalResponse> {
  const response = await fetch(`/api/diff-external/${workspaceId}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(command ? { command } : {})
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

export async function getVariants(): Promise<VariantsResponse> {
  const response = await fetch('/api/variants');
  if (!response.ok) throw new Error('Failed to fetch variants');
  return response.json();
}

export async function configureVariantSecrets(variantName: string, secrets: Record<string, string>): Promise<{ status: string }> {
  const response = await fetch(`/api/variants/${variantName}/secrets`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ secrets })
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to save variant secrets');
  }
  return response.json();
}

export async function removeVariantSecrets(variantName: string): Promise<{ status: string }> {
  const response = await fetch(`/api/variants/${variantName}/secrets`, {
    method: 'DELETE'
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to remove variant secrets');
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
    headers: { 'Content-Type': 'application/json' }
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
