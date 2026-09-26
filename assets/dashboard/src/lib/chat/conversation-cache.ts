// Normalized durable conversations kept for the SPA lifetime, keyed by
// session id, so a reconnect requests and reduces only records after lastSeq.
// Entries hold durable state only: queue overlays and live records are
// applied to the rendered conversation, never to the cached one.
import type { ChatProtocol, Conversation } from './types';

export interface CachedChatConversation {
  protocol: ChatProtocol;
  lastSeq: number;
  conversation: Conversation;
}

const cache = new Map<string, CachedChatConversation>();

export function getCachedConversation(sessionId: string): CachedChatConversation | undefined {
  return cache.get(sessionId);
}

export function setCachedConversation(sessionId: string, entry: CachedChatConversation): void {
  cache.set(sessionId, entry);
}

export function clearCachedConversation(sessionId: string): void {
  cache.delete(sessionId);
}

export function resetConversationCacheForTests(): void {
  cache.clear();
}
