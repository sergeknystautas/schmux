import { describe, it, expect, beforeEach } from 'vitest';
import { loadChatFocus, saveChatFocus, type ChatFocus } from './chat-focus';

describe('chat-focus', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it('round-trips a composer focus record per session', () => {
    const focus: ChatFocus = { target: 'composer', position: 7 };
    saveChatFocus('s1', focus);
    expect(loadChatFocus('s1')).toEqual(focus);
    expect(loadChatFocus('s2')).toBeNull();
  });

  it('round-trips question-target records', () => {
    const other: ChatFocus = {
      target: 'other-input',
      requestId: 'r1',
      questionId: 'q1',
      position: 3,
    };
    const option: ChatFocus = { target: 'option', requestId: 'r1', questionId: 'q2', label: 'Yes' };
    saveChatFocus('s1', other);
    expect(loadChatFocus('s1')).toEqual(other);
    saveChatFocus('s1', option);
    expect(loadChatFocus('s1')).toEqual(option);
  });

  it('returns null on corrupt stored JSON instead of throwing', () => {
    sessionStorage.setItem('chat-focus-s1', '{not json');
    expect(loadChatFocus('s1')).toBeNull();
  });
});
