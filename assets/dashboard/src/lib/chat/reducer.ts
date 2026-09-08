// Shared dispatcher and turn helpers for chat reducers. The per-protocol
// logic lives in claude.ts and codex.ts; this file owns the rule both
// protocols follow and the per-turn state machine they drive.
import type {
  AssistantTurn,
  ChatProtocol,
  Conversation,
  ConversationRecord,
  PendingSegment,
} from './types';
import { applyClaudeRecord, claudeResolvedRequestId } from './claude';
import { applyCodexRecord, codexResolvedRequestId } from './codex';

export interface OpenTurn extends AssistantTurn {
  _blocks: Record<number, number>;
  _thinkingIndex: number | null;
  _items: Record<string, number>;
}

export function emptyConversation(): Conversation {
  return { items: [], phase: 'idle' };
}

export function newTurn(): OpenTurn {
  return {
    kind: 'assistant',
    segments: [],
    end: null,
    interrupted: false,
    thinking: false,
    _blocks: {},
    _thinkingIndex: null,
    _items: {},
  };
}

export function openTurn(c: Conversation): OpenTurn | null {
  for (let i = c.items.length - 1; i >= 0; i--) {
    const it = c.items[i];
    if (it.kind === 'assistant') return it.end === null ? (it as OpenTurn) : null;
  }
  return null;
}

export function replaceOpenTurn(c: Conversation, turn: OpenTurn): Conversation {
  const items = c.items.slice();
  for (let i = items.length - 1; i >= 0; i--) {
    if (items[i].kind === 'assistant') {
      items[i] = turn;
      break;
    }
  }
  return { items, phase: turn.end === null ? 'running' : 'idle' };
}

export function cloneTurn(t: OpenTurn): OpenTurn {
  return {
    ...t,
    segments: t.segments.slice(),
    _blocks: { ...t._blocks },
    _items: { ...t._items },
  };
}

export function closeTurn(open: OpenTurn, end: NonNullable<AssistantTurn['end']>): OpenTurn {
  const next = cloneTurn(open);
  next.thinking = false;
  next._thinkingIndex = null;
  next._items = {};
  next.segments = next.segments
    .filter((s) => s.kind !== 'pending')
    .map((s) =>
      s.kind === 'tool' && (s.state === 'preparing' || s.state === 'running')
        ? { ...s, result: 'interrupted', state: 'error' as const }
        : s
    );
  next.end = end;
  return next;
}

export function removePending(t: OpenTurn, requestId: string): OpenTurn {
  const next = cloneTurn(t);
  const si = next.segments.findIndex(
    (s) => s.kind === 'pending' && (s as PendingSegment).requestId === requestId
  );
  if (si >= 0) dropSegment(next, si);
  return next;
}

export function findLast(
  segs: AssistantTurn['segments'],
  pred: (s: AssistantTurn['segments'][number]) => boolean
): number {
  for (let i = segs.length - 1; i >= 0; i--) if (pred(segs[i])) return i;
  return -1;
}

export function dropSegment(t: OpenTurn, si: number) {
  t.segments.splice(si, 1);
  for (const k of Object.keys(t._blocks)) {
    const v = t._blocks[Number(k)];
    if (v === si) delete t._blocks[Number(k)];
    else if (v > si) t._blocks[Number(k)] = v - 1;
  }
  for (const k of Object.keys(t._items)) {
    const v = t._items[k];
    if (v === si) delete t._items[k];
    else if (v > si) t._items[k] = v - 1;
  }
}

export function contentText(content: unknown): string {
  if (typeof content === 'string') return content;
  if (Array.isArray(content))
    return content
      .filter((b) => b && (b as { type?: string }).type === 'text')
      .map((b) => String((b as { text?: string }).text ?? ''))
      .join('');
  return '';
}

const reducers: Record<ChatProtocol, (c: Conversation, r: ConversationRecord) => Conversation> = {
  'claude-stream-json': applyClaudeRecord,
  'codex-app-server': applyCodexRecord,
};

export function reduceRecords(protocol: ChatProtocol, records: ConversationRecord[]): Conversation {
  return records.reduce((c, r) => applyRecord(protocol, c, r), emptyConversation());
}

export function applyRecord(
  protocol: ChatProtocol,
  c: Conversation,
  r: ConversationRecord
): Conversation {
  if (r.type === 'session') return applySessionEnded(c, r);
  return reducers[protocol](c, r);
}

const resolvers: Record<ChatProtocol, (r: ConversationRecord) => string | null> = {
  'claude-stream-json': claudeResolvedRequestId,
  'codex-app-server': codexResolvedRequestId,
};

// resolvesRequest reports the request id a record resolves, or null. The
// dispatcher parallels reducers[] above; useChatSocket uses it to clear
// per-session answer drafts at the same boundary that removes the card.
export function resolvesRequest(protocol: ChatProtocol, r: ConversationRecord): string | null {
  return resolvers[protocol](r);
}

function applySessionEnded(
  c: Conversation,
  r: Extract<ConversationRecord, { type: 'session' }>
): Conversation {
  if (r.event !== 'ended') return c;
  const cleared = {
    ...c,
    items: c.items.map((i) => (i.kind === 'user' && i.queued ? { ...i, queued: false } : i)),
  };
  const open = openTurn(cleared);
  if (!open) return { ...cleared, phase: 'idle' };
  const next = closeTurn(open, { state: 'stopped' });
  const items = cleared.items.slice();
  for (let i = items.length - 1; i >= 0; i--) {
    if (items[i].kind === 'assistant') {
      items[i] = next;
      break;
    }
  }
  return { items, phase: 'idle' };
}
