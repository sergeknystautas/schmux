import { describe, it, expect, beforeEach } from 'vitest';
import { loadChatAnswers, saveChatAnswer, clearChatAnswers } from './chat-answers';

describe('chat-answers', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it('round-trips answers per session and request', () => {
    saveChatAnswer('s1', 'r1', 'q1', { selected: ['a'], other: '' });
    saveChatAnswer('s1', 'r1', 'q2', { selected: [], other: 'x' });
    expect(loadChatAnswers('s1')).toEqual({
      r1: {
        q1: { selected: ['a'], other: '' },
        q2: { selected: [], other: 'x' },
      },
    });
    expect(loadChatAnswers('s2')).toEqual({});
  });

  it('an empty answer removes the question entry', () => {
    saveChatAnswer('s1', 'r1', 'q1', { selected: ['a'], other: '' });
    saveChatAnswer('s1', 'r1', 'q1', { selected: [], other: '' });
    expect(loadChatAnswers('s1')).toEqual({});
  });

  it('clearChatAnswers removes only the given request', () => {
    saveChatAnswer('s1', 'r1', 'q1', { selected: ['a'], other: '' });
    saveChatAnswer('s1', 'r2', 'q1', { selected: ['b'], other: '' });
    clearChatAnswers('s1', 'r1');
    expect(loadChatAnswers('s1')).toEqual({ r2: { q1: { selected: ['b'], other: '' } } });
  });

  it('clearing the last request removes the storage key', () => {
    saveChatAnswer('s1', 'r1', 'q1', { selected: ['a'], other: '' });
    clearChatAnswers('s1', 'r1');
    expect(sessionStorage.getItem('chat-answers-s1')).toBeNull();
  });

  it('returns {} on corrupt stored JSON instead of throwing', () => {
    sessionStorage.setItem('chat-answers-s1', '{not json');
    expect(loadChatAnswers('s1')).toEqual({});
  });
});
