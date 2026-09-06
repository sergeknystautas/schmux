// Codex app-server dialect → the same Conversation model the Claude reducer
// produces. One rule per line shape; see the spec's section 7 table.
import {
  cloneTurn,
  closeTurn,
  dropSegment,
  newTurn,
  openTurn,
  removePending,
  replaceOpenTurn,
  type OpenTurn,
} from './reducer';
import type {
  AssistantTurn,
  Conversation,
  ConversationRecord,
  HarnessLine,
  PendingSegment,
  Segment,
  ToolSegment,
  UserMessage,
} from './types';

interface Item {
  type: string;
  id: string;
  text?: string;
  summary?: string[];
  content?: string[];
  command?: string;
  cwd?: string;
  aggregatedOutput?: string | null;
  exitCode?: number | null;
  status?: string;
  changes?: unknown[];
  server?: string;
  tool?: string;
  arguments?: unknown;
  result?: { content?: { type: string; text?: string }[] } | string | null;
}

const ACCOUNT_ID = 2;
const LOGGED_OUT = 'Codex is not logged in. Run `codex login`, then Restart.';

export function applyCodexRecord(c: Conversation, r: ConversationRecord): Conversation {
  switch (r.type) {
    case 'user_message':
      return applyUserMessage(c, r);
    case 'control':
      return applyControl(c, r.line);
    case 'harness':
      return applyHarness(c, r.line);
    default:
      return c;
  }
}

function applyUserMessage(
  c: Conversation,
  r: Extract<ConversationRecord, { type: 'user_message' }>
): Conversation {
  const open = openTurn(c);
  if (open) {
    const next = cloneTurn(open);
    next.segments.push({ kind: 'user', id: r.id, text: r.text, images: r.images ?? [] });
    return replaceOpenTurn(c, next);
  }
  const msg: UserMessage = {
    kind: 'user',
    id: r.id,
    text: r.text,
    images: r.images ?? [],
    queued: false,
  };
  return { items: [...c.items, msg, newTurn()], phase: 'running' };
}

function applyControl(c: Conversation, line: HarnessLine): Conversation {
  const open = openTurn(c);
  if (!open) return c;
  if (line.method === 'turn/interrupt')
    return replaceOpenTurn(c, { ...cloneTurn(open), interrupted: true });
  if (line.method === undefined && line.id !== undefined && 'result' in line)
    return replaceOpenTurn(c, removePending(open, String(line.id)));
  return c;
}

// The account/read response is {"account":null,"requiresOpenaiAuth":true}
// when logged out and carries an account object otherwise. Only an explicit
// null (or an error response) counts: a response to id 2 with no `account`
// key at all is some other request's answer (probe captures number
// thread/start as 2) and must not be read as logged out.
function isLoggedOutResponse(line: HarnessLine): boolean {
  if (line.method !== undefined || line.id !== ACCOUNT_ID) return false;
  if ('error' in line) return true;
  const result = line.result as Record<string, unknown> | undefined;
  return !!result && 'account' in result && result.account === null;
}

function applyHarness(c: Conversation, line: HarnessLine): Conversation {
  const open = openTurn(c);
  // The logged-out response is the one line that must show whether or not a
  // turn is open: the runtime never sends a turn while logged out, so without
  // this the page would show nothing at all. With no open turn it becomes a
  // closed assistant turn carrying only the error.
  if (isLoggedOutResponse(line)) {
    if (open) return replaceOpenTurn(c, closeTurn(open, { state: 'error', text: LOGGED_OUT }));
    const failed: AssistantTurn = {
      kind: 'assistant',
      segments: [],
      end: { state: 'error', text: LOGGED_OUT },
      interrupted: false,
      thinking: false,
    };
    return { items: [...c.items, failed], phase: 'idle' };
  }
  if (!open) return c;
  const params = (line.params ?? {}) as Record<string, unknown>;
  const method = line.method as string | undefined;
  if (method === undefined) return c;

  switch (method) {
    case 'item/started':
      return replaceOpenTurn(c, itemStarted(open, params.item as Item));
    case 'item/completed':
      return replaceOpenTurn(c, itemCompleted(open, params.item as Item));
    case 'item/agentMessage/delta':
      return replaceOpenTurn(
        c,
        appendProse(open, String(params.itemId), String(params.delta ?? ''))
      );
    case 'item/reasoning/summaryTextDelta':
    case 'item/reasoning/textDelta':
      return replaceOpenTurn(
        c,
        appendThinking(open, String(params.itemId), String(params.delta ?? ''))
      );
    case 'item/commandExecution/outputDelta':
    case 'item/fileChange/outputDelta':
      return replaceOpenTurn(
        c,
        appendToolOutput(open, String(params.itemId), String(params.delta ?? ''))
      );
    case 'item/commandExecution/requestApproval':
      return replaceOpenTurn(
        c,
        insertPending(open, String(params.itemId), {
          kind: 'pending',
          requestId: String(line.id),
          toolUseId: String(params.itemId),
          toolName: 'command',
          input: { command: params.command, cwd: params.cwd, reason: params.reason },
          questions: null,
        })
      );
    case 'item/fileChange/requestApproval': {
      const row = toolAt(open, String(params.itemId));
      return replaceOpenTurn(
        c,
        insertPending(open, String(params.itemId), {
          kind: 'pending',
          requestId: String(line.id),
          toolUseId: String(params.itemId),
          toolName: 'edit',
          input: {
            reason: params.reason,
            grantRoot: params.grantRoot,
            changes: (row?.input as { changes?: unknown })?.changes,
          },
          questions: null,
        })
      );
    }
    case 'item/tool/requestUserInput': {
      const qs =
        (params.questions as {
          id: string;
          header?: string;
          question: string;
          options: { label: string; description?: string }[];
        }[]) ?? [];
      return replaceOpenTurn(
        c,
        insertPending(open, String(params.itemId), {
          kind: 'pending',
          requestId: String(line.id),
          toolUseId: String(params.itemId),
          toolName: 'question',
          input: { questions: qs },
          questions: qs.map((q) => ({
            id: q.id,
            header: q.header,
            question: q.question,
            options: q.options,
            multiSelect: false,
          })),
        })
      );
    }
    case 'serverRequest/resolved':
      return replaceOpenTurn(c, removePending(open, String(params.requestId)));
    case 'turn/completed': {
      const turn = params.turn as
        { status?: string; error?: { message?: string } | null } | undefined;
      const status = turn?.status;
      if (status === 'interrupted')
        return replaceOpenTurn(c, closeTurn(open, { state: 'stopped' }));
      if (status === 'failed')
        return replaceOpenTurn(
          c,
          closeTurn(open, { state: 'error', text: turn?.error?.message ?? 'failed' })
        );
      return replaceOpenTurn(c, closeTurn(open, { state: 'done' }));
    }
    case 'error': {
      if (params.willRetry === true) return c;
      const err = params.error as { message?: string } | undefined;
      return replaceOpenTurn(c, closeTurn(open, { state: 'error', text: err?.message ?? 'error' }));
    }
    default:
      return c;
  }
}

function segIndex(t: OpenTurn, itemId: string): number | undefined {
  return t._items[itemId];
}

function toolAt(t: OpenTurn, itemId: string): ToolSegment | undefined {
  const si = segIndex(t, itemId);
  const s = si === undefined ? undefined : t.segments[si];
  return s?.kind === 'tool' ? s : undefined;
}

function pushSeg(t: OpenTurn, itemId: string, seg: Segment): OpenTurn {
  const next = cloneTurn(t);
  next._items[itemId] = next.segments.push(seg) - 1;
  return next;
}

function itemStarted(t: OpenTurn, item: Item | undefined): OpenTurn {
  if (!item) return t;
  switch (item.type) {
    case 'agentMessage':
      return pushSeg(t, item.id, { kind: 'prose', text: item.text ?? '', streaming: true });
    case 'reasoning': {
      const next = pushSeg(t, item.id, { kind: 'thinking', text: (item.summary ?? []).join('') });
      next.thinking = true;
      return next;
    }
    case 'commandExecution': {
      const input = { command: item.command ?? '', cwd: item.cwd ?? '' };
      return pushSeg(t, item.id, {
        kind: 'tool',
        id: item.id,
        name: 'command',
        input,
        inputJson: JSON.stringify(input),
        result: '',
        state: 'running',
        subtools: [],
      });
    }
    case 'fileChange': {
      const input = { changes: item.changes ?? [] };
      return pushSeg(t, item.id, {
        kind: 'tool',
        id: item.id,
        name: 'edit',
        input,
        inputJson: JSON.stringify(input),
        result: '',
        state: 'running',
        subtools: [],
      });
    }
    case 'mcpToolCall': {
      const input = item.arguments ?? {};
      return pushSeg(t, item.id, {
        kind: 'tool',
        id: item.id,
        name: `${item.server ?? ''}/${item.tool ?? ''}`,
        input,
        inputJson: JSON.stringify(input),
        result: '',
        state: 'running',
        subtools: [],
      });
    }
    default:
      return t;
  }
}

function toolState(status: string | undefined): ToolSegment['state'] {
  return status === 'completed' ? 'done' : 'error';
}

function mcpResultText(result: Item['result']): string {
  if (!result) return '';
  if (typeof result === 'string') return result;
  return (result.content ?? [])
    .filter((b) => b.type === 'text')
    .map((b) => b.text ?? '')
    .join('');
}

function itemCompleted(t: OpenTurn, item: Item | undefined): OpenTurn {
  if (!item) return t;
  const si = segIndex(t, item.id);
  switch (item.type) {
    case 'agentMessage': {
      if (si === undefined)
        return pushSeg(t, item.id, { kind: 'prose', text: item.text ?? '', streaming: false });
      const next = cloneTurn(t);
      next.segments[si] = { kind: 'prose', text: item.text ?? '', streaming: false };
      return next;
    }
    case 'reasoning': {
      const text = [...(item.summary ?? []), ...(item.content ?? [])].join('');
      const next = cloneTurn(t);
      next.thinking = false;
      if (si === undefined) {
        if (text.trim() !== '')
          next._items[item.id] = next.segments.push({ kind: 'thinking', text }) - 1;
        return next;
      }
      if (text.trim() === '') {
        dropSegment(next, si);
        delete next._items[item.id];
      } else next.segments[si] = { kind: 'thinking', text };
      return next;
    }
    case 'commandExecution':
    case 'fileChange':
    case 'mcpToolCall': {
      const base = si === undefined ? itemStarted(t, { ...item, status: 'inProgress' }) : t;
      const idx = segIndex(base, item.id);
      if (idx === undefined) return base;
      const next = cloneTurn(base);
      const existing = next.segments[idx] as ToolSegment;
      const result =
        item.type === 'commandExecution'
          ? (item.aggregatedOutput ?? existing.result)
          : item.type === 'mcpToolCall'
            ? mcpResultText(item.result)
            : (item.changes ?? []).map((ch) => JSON.stringify(ch)).join('\n');
      next.segments[idx] = { ...existing, result, state: toolState(item.status) };
      return next;
    }
    default:
      return t;
  }
}

function appendProse(t: OpenTurn, itemId: string, delta: string): OpenTurn {
  const si = segIndex(t, itemId);
  if (si === undefined) return pushSeg(t, itemId, { kind: 'prose', text: delta, streaming: true });
  const seg = t.segments[si];
  if (seg.kind !== 'prose') return t;
  const next = cloneTurn(t);
  next.segments[si] = { ...seg, text: seg.text + delta };
  return next;
}

function appendThinking(t: OpenTurn, itemId: string, delta: string): OpenTurn {
  const si = segIndex(t, itemId);
  if (si === undefined) {
    const next = pushSeg(t, itemId, { kind: 'thinking', text: delta });
    next.thinking = true;
    return next;
  }
  const seg = t.segments[si];
  if (seg.kind !== 'thinking') return t;
  const next = cloneTurn(t);
  next.segments[si] = { ...seg, text: seg.text + delta };
  return next;
}

function appendToolOutput(t: OpenTurn, itemId: string, delta: string): OpenTurn {
  const si = segIndex(t, itemId);
  const seg = si === undefined ? undefined : t.segments[si];
  if (!seg || seg.kind !== 'tool') return t;
  const next = cloneTurn(t);
  next.segments[si!] = { ...seg, result: seg.result + delta };
  return next;
}

function insertPending(t: OpenTurn, itemId: string, pending: PendingSegment): OpenTurn {
  const next = cloneTurn(t);
  const si = segIndex(next, itemId);
  const at = si === undefined ? next.segments.length : si + 1;
  next.segments.splice(at, 0, pending);
  for (const k of Object.keys(next._items)) if (next._items[k] >= at) next._items[k]++;
  return next;
}
