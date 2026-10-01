// Pure state machine for the Markdown editor's document. The hook feeds it
// socket and timer events and executes the effects it returns; nothing here
// touches React, timers, or the network, so every rule is testable in
// isolation. Terminology follows the spec: base is the text the draft was
// edited from, draft is the editor text, inFlight is the one outstanding save.

export type DocStatus = 'connecting' | 'saved' | 'saving' | 'error';

interface InFlight {
  id: string;
  base: string;
  draft: string;
}

export interface DocState {
  base: string;
  draft: string;
  inFlight: InFlight | null;
  status: DocStatus;
  reason: string | null;
  // True between 'open' and the first document, which is the server's snapshot.
  awaitingFirst: boolean;
}

export type DocEvent =
  | { type: 'edit'; text: string }
  | { type: 'timer' }
  | { type: 'document'; content: string; reply?: string }
  | { type: 'open' }
  | { type: 'close'; reason: string | null };

export type Effect =
  | { type: 'send'; id: string; base: string; draft: string }
  | { type: 'armTimer' }
  | { type: 'cancelTimer' };

export const TERMINAL_REASONS: ReadonlySet<string> = new Set([
  'too_large',
  'not_utf8',
  'invalid_path',
  'bad_request',
]);

export const initialState: DocState = {
  base: '',
  draft: '',
  inFlight: null,
  status: 'connecting',
  reason: null,
  awaitingFirst: true,
};

const isDirty = (s: DocState) => s.draft !== s.base;

function send(s: DocState, id: string, base: string, draft: string): [DocState, Effect[]] {
  return [
    { ...s, inFlight: { id, base, draft }, status: 'saving' },
    [{ type: 'send', id, base, draft }],
  ];
}

export function reduce(
  state: DocState,
  event: DocEvent,
  newId: () => string
): [DocState, Effect[]] {
  switch (event.type) {
    case 'edit': {
      const s = { ...state, draft: event.text };
      if (isDirty(s) && !s.inFlight && !s.awaitingFirst && s.status !== 'error') {
        return [s, [{ type: 'armTimer' }]];
      }
      return [s, []];
    }

    case 'timer': {
      if (state.inFlight || !isDirty(state)) return [state, []];
      return send(state, newId(), state.base, state.draft);
    }

    case 'open':
      return [{ ...state, status: 'connecting', awaitingFirst: true }, []];

    case 'close': {
      const reason = event.reason ?? state.reason;
      return [
        { ...state, status: 'error', reason, awaitingFirst: false },
        [{ type: 'cancelTimer' }],
      ];
    }

    case 'document': {
      const { content } = event;

      if (state.awaitingFirst) {
        const s = { ...state, awaitingFirst: false, reason: null };
        if (s.inFlight) {
          // A save was outstanding when the socket dropped; resend it with the
          // same id so a committed-but-unacknowledged save is not applied twice.
          return send(s, s.inFlight.id, s.inFlight.base, s.inFlight.draft);
        }
        if (isDirty(s)) {
          return send(s, newId(), s.base, s.draft);
        }
        return [{ ...s, base: content, draft: content, status: 'saved' }, []];
      }

      const isReply =
        event.reply !== undefined && state.inFlight !== null && event.reply === state.inFlight.id;

      if (isReply) {
        const sent = state.inFlight!;
        if (state.draft === sent.draft) {
          return [{ ...state, base: content, draft: content, inFlight: null, status: 'saved' }, []];
        }
        // Typed since sending: the new keystrokes sit on top of the sent draft.
        return send({ ...state, base: sent.draft }, newId(), sent.draft, state.draft);
      }

      if (state.inFlight) {
        return [state, []];
      }
      if (!isDirty(state)) {
        return [{ ...state, base: content, draft: content, status: 'saved' }, []];
      }
      const [s, effects] = send(state, newId(), state.base, state.draft);
      return [s, [{ type: 'cancelTimer' }, ...effects]];
    }
  }
}
