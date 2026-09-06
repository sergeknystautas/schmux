// Per-tab in-progress chat message, stored in sessionStorage.
// Keyed per session, so switching tabs and coming back restores what was
// being typed and attached. Same mechanism as the spawn draft (spawn-draft.ts).
// Cleared when the message is sent.

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
    const stored = sessionStorage.getItem(getChatDraftKey(sessionId));
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
      sessionStorage.removeItem(getChatDraftKey(sessionId));
      return;
    }
    sessionStorage.setItem(getChatDraftKey(sessionId), JSON.stringify(draft));
  } catch (err) {
    console.warn('Failed to save chat draft:', err);
  }
}

export function clearChatDraft(sessionId: string): void {
  try {
    sessionStorage.removeItem(getChatDraftKey(sessionId));
  } catch (err) {
    console.warn('Failed to clear chat draft:', err);
  }
}
