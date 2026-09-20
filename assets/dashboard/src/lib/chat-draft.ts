// In-progress chat message, stored in localStorage.
// Keyed per session, so reloading the page or coming back later restores what
// was being typed and attached. Cleared when the message is sent.

import type { ChatImage } from './chat/types';

export interface ChatDraft {
  text: string;
  images: ChatImage[];
}

function getChatDraftKey(sessionId: string): string {
  return `chat-draft-${sessionId}`;
}

export function loadChatDraft(sessionId: string): ChatDraft | null {
  try {
    const stored = localStorage.getItem(getChatDraftKey(sessionId));
    if (stored) {
      const draft = JSON.parse(stored) as Partial<ChatDraft>;
      return { text: draft.text ?? '', images: draft.images ?? [] };
    }
  } catch (err) {
    console.warn('Failed to load chat draft:', err);
  }
  return null;
}

export function saveChatDraft(sessionId: string, draft: ChatDraft): void {
  try {
    if (draft.text === '' && draft.images.length === 0) {
      localStorage.removeItem(getChatDraftKey(sessionId));
      return;
    }
    localStorage.setItem(getChatDraftKey(sessionId), JSON.stringify(draft));
  } catch (err) {
    console.warn('Failed to save chat draft:', err);
  }
}

export function clearChatDraft(sessionId: string): void {
  try {
    localStorage.removeItem(getChatDraftKey(sessionId));
  } catch (err) {
    console.warn('Failed to clear chat draft:', err);
  }
}
