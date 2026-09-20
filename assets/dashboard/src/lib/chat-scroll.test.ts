import { describe, it, expect, beforeEach } from 'vitest';
import { loadChatScroll, saveChatScroll, type ChatScroll } from './chat-scroll';

describe('chat-scroll', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('round-trips a position record per session', () => {
    const record: ChatScroll = { mode: 'position', scrollTop: 340 };
    saveChatScroll('s1', record);
    expect(loadChatScroll('s1')).toEqual(record);
    expect(loadChatScroll('s2')).toBeNull();
  });

  it('round-trips a bottom record and overwrites the previous one', () => {
    saveChatScroll('s1', { mode: 'position', scrollTop: 12 });
    saveChatScroll('s1', { mode: 'bottom' });
    expect(loadChatScroll('s1')).toEqual({ mode: 'bottom' });
  });

  it('stores under the chat-scroll-<sessionId> key in localStorage', () => {
    saveChatScroll('s1', { mode: 'bottom' });
    expect(localStorage.getItem('chat-scroll-s1')).toBe('{"mode":"bottom"}');
  });

  it('returns null on corrupt stored JSON instead of throwing', () => {
    localStorage.setItem('chat-scroll-s1', '{not json');
    expect(loadChatScroll('s1')).toBeNull();
  });
});
