import { csrfHeaders } from './csrf';
import { parseErrorResponse } from './api';
import type {
  SpawnEntry,
  SpawnEntriesResponse,
  CreateSpawnEntryRequest,
  UpdateSpawnEntryRequest,
  PromptHistoryEntry,
  PromptHistoryResponse,
} from './types.generated';

export async function getSpawnEntries(repo: string): Promise<SpawnEntry[]> {
  const response = await fetch(`/api/spawn/${encodeURIComponent(repo)}/entries`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch spawn entries');
  const data: SpawnEntriesResponse = await response.json();
  return data.entries;
}

export async function getAllSpawnEntries(repo: string): Promise<SpawnEntry[]> {
  const response = await fetch(`/api/spawn/${encodeURIComponent(repo)}/entries/all`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch all spawn entries');
  const data: SpawnEntriesResponse = await response.json();
  return data.entries;
}

export async function createSpawnEntry(
  repo: string,
  req: CreateSpawnEntryRequest
): Promise<SpawnEntry> {
  const response = await fetch(`/api/spawn/${encodeURIComponent(repo)}/entries`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to create spawn entry');
  return response.json();
}

async function updateSpawnEntry(
  repo: string,
  id: string,
  req: UpdateSpawnEntryRequest
): Promise<void> {
  const response = await fetch(
    `/api/spawn/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify(req),
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to update spawn entry');
}

async function deleteSpawnEntry(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/spawn/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to delete spawn entry');
}

export async function pinSpawnEntry(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/spawn/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}/pin`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to pin spawn entry');
}

export async function dismissSpawnEntry(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/spawn/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}/dismiss`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to dismiss spawn entry');
}

export async function recordSpawnEntryUse(repo: string, id: string): Promise<void> {
  const response = await fetch(
    `/api/spawn/${encodeURIComponent(repo)}/entries/${encodeURIComponent(id)}/use`,
    {
      method: 'POST',
      headers: { ...csrfHeaders() },
    }
  );
  if (!response.ok) await parseErrorResponse(response, 'Failed to record spawn entry use');
}

async function triggerSpawnCuration(repo: string): Promise<void> {
  const response = await fetch(`/api/spawn/${encodeURIComponent(repo)}/curate`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to trigger spawn curation');
}

export async function getPromptHistory(repo: string): Promise<PromptHistoryEntry[]> {
  const response = await fetch(`/api/spawn/${encodeURIComponent(repo)}/prompt-history`);
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch prompt history');
  const data: PromptHistoryResponse = await response.json();
  return data.prompts;
}
