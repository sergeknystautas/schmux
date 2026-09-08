// In-progress answers to pending question cards, stored in sessionStorage.
// Keyed per session, like chat-draft.ts, so switching tabs and coming back
// restores what was selected and typed. An entry is cleared when the client
// observes the request resolve (delivery-authoritative), not at submit click.

export interface QuestionAnswer {
  selected: string[];
  other: string;
}

export type ChatAnswers = Record<string, Record<string, QuestionAnswer>>;

function getChatAnswersKey(sessionId: string): string {
  return `chat-answers-${sessionId}`;
}

export function loadChatAnswers(sessionId: string): ChatAnswers {
  try {
    const stored = sessionStorage.getItem(getChatAnswersKey(sessionId));
    if (stored) return JSON.parse(stored) as ChatAnswers;
  } catch (err) {
    console.warn('Failed to load chat answers:', err);
  }
  return {};
}

export function saveChatAnswer(
  sessionId: string,
  requestId: string,
  questionId: string,
  answer: QuestionAnswer
): void {
  try {
    const all = loadChatAnswers(sessionId);
    const forRequest = all[requestId] ?? {};
    if (answer.selected.length === 0 && answer.other === '') {
      delete forRequest[questionId];
    } else {
      forRequest[questionId] = answer;
    }
    if (Object.keys(forRequest).length === 0) delete all[requestId];
    else all[requestId] = forRequest;
    writeChatAnswers(sessionId, all);
  } catch (err) {
    console.warn('Failed to save chat answer:', err);
  }
}

export function clearChatAnswers(sessionId: string, requestId: string): void {
  try {
    const all = loadChatAnswers(sessionId);
    delete all[requestId];
    writeChatAnswers(sessionId, all);
  } catch (err) {
    console.warn('Failed to clear chat answers:', err);
  }
}

function writeChatAnswers(sessionId: string, all: ChatAnswers): void {
  const key = getChatAnswersKey(sessionId);
  if (Object.keys(all).length === 0) sessionStorage.removeItem(key);
  else sessionStorage.setItem(key, JSON.stringify(all));
}
