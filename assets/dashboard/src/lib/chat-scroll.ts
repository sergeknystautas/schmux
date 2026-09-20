// Where the chat transcript was scrolled to, stored in localStorage.
// One record per session, keyed like chat-draft / chat-answers / chat-focus.
// `bottom` means the user was following the tail; on return they land at
// the new bottom. `position` is the transcript container's scrollTop in
// pixels, the same value the markdown and HTML viewers store.

export type ChatScroll = { mode: 'bottom' } | { mode: 'position'; scrollTop: number };

function getChatScrollKey(sessionId: string): string {
  return `chat-scroll-${sessionId}`;
}

export function loadChatScroll(sessionId: string): ChatScroll | null {
  try {
    const stored = localStorage.getItem(getChatScrollKey(sessionId));
    if (stored) return JSON.parse(stored) as ChatScroll;
  } catch (err) {
    console.warn('Failed to load chat scroll:', err);
  }
  return null;
}

export function saveChatScroll(sessionId: string, record: ChatScroll): void {
  try {
    localStorage.setItem(getChatScrollKey(sessionId), JSON.stringify(record));
  } catch (err) {
    console.warn('Failed to save chat scroll:', err);
  }
}
