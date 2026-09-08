// Where keyboard focus was in a chat session, stored in sessionStorage.
// One record per session: the composer caret, or which question field was
// focused and where. Restore falls back to the composer when the recorded
// target no longer exists (spec: docs/specs/chat-session-focus-persistence.md).

export type ChatFocus =
  | { target: 'composer'; position: number }
  | { target: 'other-input'; requestId: string; questionId: string; position: number }
  | { target: 'option'; requestId: string; questionId: string; label: string };

function getChatFocusKey(sessionId: string): string {
  return `chat-focus-${sessionId}`;
}

export function loadChatFocus(sessionId: string): ChatFocus | null {
  try {
    const stored = sessionStorage.getItem(getChatFocusKey(sessionId));
    if (stored) return JSON.parse(stored) as ChatFocus;
  } catch (err) {
    console.warn('Failed to load chat focus:', err);
  }
  return null;
}

export function saveChatFocus(sessionId: string, focus: ChatFocus): void {
  try {
    sessionStorage.setItem(getChatFocusKey(sessionId), JSON.stringify(focus));
  } catch (err) {
    console.warn('Failed to save chat focus:', err);
  }
}
