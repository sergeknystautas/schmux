export async function getSessions() {
  const response = await fetch('/api/sessions');
  if (!response.ok) throw new Error('Failed to fetch sessions');
  return response.json();
}

export async function getWorkspaces() {
  const response = await fetch('/api/workspaces');
  if (!response.ok) throw new Error('Failed to fetch workspaces');
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
  if (!response.ok) throw new Error('Failed to update nickname');
  return response.json();
}

export async function disposeWorkspace(workspaceId) {
  const response = await fetch(`/api/dispose-workspace/${workspaceId}`, { method: 'POST' });
  if (!response.ok) throw new Error('Failed to dispose workspace');
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
