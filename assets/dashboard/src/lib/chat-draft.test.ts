import { describe, it, expect, beforeEach } from 'vitest';
import { loadChatDraft, saveChatDraft, clearChatDraft } from './chat-draft';

describe('chat-draft', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it('round-trips text and images per session key', () => {
    saveChatDraft('s1', { text: 'hello', images: [{ media_type: 'image/png', data: 'AA==' }] });
    expect(loadChatDraft('s1')).toEqual({
      text: 'hello',
      images: [{ media_type: 'image/png', data: 'AA==' }],
    });
    expect(loadChatDraft('s2')).toBeNull();
  });

  it('an empty draft removes the stored entry', () => {
    saveChatDraft('s1', { text: 'x', images: [] });
    saveChatDraft('s1', { text: '', images: [] });
    expect(loadChatDraft('s1')).toBeNull();
  });

  it('clearChatDraft removes only the given key', () => {
    saveChatDraft('s1', { text: 'a', images: [] });
    saveChatDraft('s2', { text: 'b', images: [] });
    clearChatDraft('s1');
    expect(loadChatDraft('s1')).toBeNull();
    expect(loadChatDraft('s2')?.text).toBe('b');
  });

  it('returns null on corrupt stored JSON instead of throwing', () => {
    sessionStorage.setItem('chat-draft-s1', '{not json');
    expect(loadChatDraft('s1')).toBeNull();
  });
});
