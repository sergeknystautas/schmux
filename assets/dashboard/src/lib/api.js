export async function getSessions() {
  const response = await fetch('/api/sessions');
  if (!response.ok) throw new Error('Failed to fetch sessions');
  return response.json();
}

export async function getConfig() {
  const response = await fetch('/api/config');
  if (!response.ok) throw new Error('Failed to fetch config');
  return response.json();
}

export async function spawnSessions(request) {
  const response = await fetch('/api/spawn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request)
  });
  if (!response.ok) throw new Error('Failed to spawn sessions');
  return response.json();
}

export async function disposeSession(sessionId) {
  const response = await fetch(`/api/dispose/${sessionId}`, { method: 'POST' });
  if (!response.ok) throw new Error('Failed to dispose session');
  return response.json();
}

export async function updateNickname(sessionId, nickname) {
  const response = await fetch(`/api/sessions-nickname/${sessionId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ nickname })
  });
  if (!response.ok) {
    if (response.status === 409) {
      const err = await response.json();
      const error = new Error(err.error || 'Nickname already in use');
      error.isConflict = true;
      throw error;
    }
    throw new Error('Failed to update nickname');
  }
  return response.json();
}

export async function disposeWorkspace(workspaceId) {
  const response = await fetch(`/api/dispose-workspace/${workspaceId}`, { method: 'POST' });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.error || 'Failed to dispose workspace');
  }
  return response.json();
}

export async function getDiff(workspaceId) {
  const response = await fetch(`/api/diff/${workspaceId}`);
  if (!response.ok) throw new Error('Failed to fetch diff');
  return response.json();
}

export async function scanWorkspaces() {
  const response = await fetch('/api/workspaces/scan', { method: 'POST' });
  if (!response.ok) throw new Error('Failed to scan workspaces');
  return response.json();
}

export async function updateConfig(request) {
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

export async function openVSCode(workspaceId) {
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

/**
 * Detects available tools on the system.
 * Returns a list of detected tools with their names, commands, and sources.
 * @returns {Promise<{tools: Array<{name: string, command: string, source: string}>>>}
 */
export async function detectTools() {
  const response = await fetch('/api/detect-tools');
  if (!response.ok) {
    throw new Error('Failed to detect tools');
  }
  return response.json();
}

export async function getVariants() {
  const response = await fetch('/api/variants');
  if (!response.ok) throw new Error('Failed to fetch variants');
  return response.json();
}

export async function configureVariantSecrets(variantName, secrets) {
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

export async function removeVariantSecrets(variantName) {
  const response = await fetch(`/api/variants/${variantName}/secrets`, {
    method: 'DELETE'
  });
  if (!response.ok) {
    const err = await response.text();
    throw new Error(err || 'Failed to remove variant secrets');
  }
  return response.json();
}

export async function getOverlays() {
  const response = await fetch('/api/overlays');
  if (!response.ok) throw new Error('Failed to fetch overlays');
  return response.json();
}

export async function refreshOverlay(workspaceId) {
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
 * @returns {Promise<Array<{name: string, target: string, prompt: string}>>}
 */
export async function getBuiltinQuickLaunch() {
  const response = await fetch('/api/builtin-quick-launch');
  if (!response.ok) {
    throw new Error('Failed to fetch built-in quick launch presets');
  }
  return response.json();
}
