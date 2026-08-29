import { describe, it, expect, beforeEach } from 'vitest';
import { loadSpawnDraft, saveSpawnDraft, clearSpawnDraft, type SpawnDraft } from './spawn-draft';

describe('spawn-draft', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  const draft: SpawnDraft = {
    prompt: 'do the thing',
    targetCounts: { claude: 1 },
    modelSelectionMode: 'single',
  };

  it('round-trips a draft per workspace key', () => {
    saveSpawnDraft('ws-1', draft);
    expect(loadSpawnDraft('ws-1')).toEqual(draft);
    expect(loadSpawnDraft(null)).toBeNull();
  });

  it('uses a distinct key for fresh (null workspace)', () => {
    saveSpawnDraft(null, draft);
    expect(loadSpawnDraft(null)).toEqual(draft);
    expect(loadSpawnDraft('ws-1')).toBeNull();
  });

  it('clearSpawnDraft removes only the given key', () => {
    saveSpawnDraft('ws-1', draft);
    saveSpawnDraft(null, draft);
    clearSpawnDraft('ws-1');
    expect(loadSpawnDraft('ws-1')).toBeNull();
    expect(loadSpawnDraft(null)).toEqual(draft);
  });

  it('returns null on corrupt stored JSON instead of throwing', () => {
    sessionStorage.setItem('spawn-draft-ws-1', '{not json');
    expect(loadSpawnDraft('ws-1')).toBeNull();
  });
});
