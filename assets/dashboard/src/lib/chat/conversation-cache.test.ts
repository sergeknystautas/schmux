import { beforeEach, describe, expect, it } from 'vitest';
import { emptyConversation } from './reducer';
import {
  clearCachedConversation,
  getCachedConversation,
  resetConversationCacheForTests,
  setCachedConversation,
} from './conversation-cache';

describe('conversation-cache', () => {
  beforeEach(() => {
    resetConversationCacheForTests();
  });

  it('stores entries by session', () => {
    const conversation = emptyConversation();
    setCachedConversation('s1', { protocol: 'claude-stream-json', lastSeq: 4, conversation });
    expect(getCachedConversation('s1')?.conversation).toBe(conversation);
    expect(getCachedConversation('s2')).toBeUndefined();
  });

  it('replaces an entry and clears it explicitly', () => {
    setCachedConversation('s1', {
      protocol: 'claude-stream-json',
      lastSeq: 1,
      conversation: emptyConversation(),
    });
    setCachedConversation('s1', {
      protocol: 'codex-app-server',
      lastSeq: 2,
      conversation: emptyConversation(),
    });
    expect(getCachedConversation('s1')?.lastSeq).toBe(2);
    clearCachedConversation('s1');
    expect(getCachedConversation('s1')).toBeUndefined();
  });
});
