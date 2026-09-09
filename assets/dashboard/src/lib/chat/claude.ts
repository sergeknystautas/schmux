// Claude stream-json reducer: today's rules, moved unchanged. Helpers used
// here are exported from reducer.ts.
import {
  cloneTurn,
  closeTurn,
  contentText,
  dropSegment,
  findLast,
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
  Question,
  ToolSegment,
  UserMessage,
} from './types';

interface Block {
  type: 'text' | 'thinking' | 'tool_use' | string;
  text?: string;
  thinking?: string;
  id?: string;
  name?: string;
  input?: unknown;
  tool_use_id?: string;
  content?: unknown;
  is_error?: boolean;
}

export function applyClaudeRecord(c: Conversation, r: ConversationRecord): Conversation {
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
  const msg: UserMessage = {
    kind: 'user',
    id: r.id,
    text: r.text,
    images: r.images ?? [],
    queued: open !== null,
  };
  const items = [...c.items, msg];
  if (open) return { items, phase: 'running' };
  return { items: [...items, newTurn()], phase: 'running' };
}

function applyControl(c: Conversation, line: HarnessLine): Conversation {
  const open = openTurn(c);
  if (!open) return c;
  if (
    line.type === 'control_request' &&
    (line.request as { subtype?: string } | undefined)?.subtype === 'interrupt'
  ) {
    return replaceOpenTurn(c, { ...cloneTurn(open), interrupted: true });
  }
  if (line.type === 'control_response') {
    const rid = (line.response as { request_id?: string } | undefined)?.request_id;
    if (rid) return replaceOpenTurn(c, removePending(open, rid));
  }
  return c;
}

function applyHarness(c: Conversation, line: HarnessLine): Conversation {
  if (line.parent_tool_use_id && line.type !== 'control_request') {
    const open = openTurn(c);
    return open ? replaceOpenTurn(c, applySubagent(open, line)) : c;
  }
  const open = openTurn(c);
  if (!open && line.type === 'system' && line.subtype === 'task_notification') {
    return { items: [...c.items, newTurn()], phase: 'running' };
  }
  if (line.type === 'user') return applyHarnessUser(c, open, line);
  if (!open) return c;
  switch (line.type) {
    case 'stream_event':
      return replaceOpenTurn(
        c,
        applyStreamEvent(
          open,
          line.event as {
            type: string;
            index?: number;
            content_block?: Block;
            delta?: Record<string, unknown>;
          }
        )
      );
    case 'assistant':
      return replaceOpenTurn(c, applyAssistant(open, line));
    case 'control_request':
      return replaceOpenTurn(c, applyControlRequest(open, line));
    case 'control_cancel_request': {
      const rid = line.request_id as string | undefined;
      return rid ? replaceOpenTurn(c, removePending(open, rid)) : c;
    }
    case 'result':
      return endTurn(c, open, line);
    default:
      return c;
  }
}

function applySubagent(t: OpenTurn, line: HarnessLine): OpenTurn {
  const parentId = String(line.parent_tool_use_id);
  const pi = t.segments.findIndex((s) => s.kind === 'tool' && s.id === parentId);
  if (pi < 0) return t;
  const parent = t.segments[pi] as ToolSegment;
  const content = (line.message as { content?: Block[] } | undefined)?.content;
  if (!Array.isArray(content)) return t;
  let subtools = parent.subtools;
  if (line.type === 'assistant') {
    for (const b of content) {
      if (b.type !== 'tool_use') continue;
      subtools = [
        ...subtools,
        {
          id: b.id ?? '',
          name: b.name ?? '',
          inputJson: JSON.stringify(b.input ?? {}),
          result: '',
          state: 'running',
        },
      ];
    }
  } else if (line.type === 'user') {
    for (const b of content) {
      if (b.type !== 'tool_result' || !b.tool_use_id) continue;
      subtools = subtools.map((s) =>
        s.id === b.tool_use_id
          ? { ...s, result: contentText(b.content), state: b.is_error ? 'error' : 'done' }
          : s
      );
    }
  } else {
    return t;
  }
  if (subtools === parent.subtools) return t;
  const next = cloneTurn(t);
  next.segments[pi] = { ...parent, subtools };
  return next;
}

function applyHarnessUser(c: Conversation, open: OpenTurn | null, line: HarnessLine): Conversation {
  const message = line.message as { content?: unknown } | undefined;
  const content = message?.content;
  if (line.isReplay === true) {
    const text = contentText(content);
    const idx = c.items.findIndex((i) => i.kind === 'user' && i.queued && i.text === text);
    if (idx < 0) return c;
    const items = c.items.slice();
    items[idx] = { ...(items[idx] as UserMessage), queued: false };
    return { ...c, items };
  }
  if (!open || !Array.isArray(content)) return c;
  let next = open;
  for (const block of content as Block[]) {
    if (block.type !== 'tool_result' || !block.tool_use_id) continue;
    next = cloneTurn(next);
    next.segments = next.segments.map((s) =>
      s.kind === 'tool' && s.id === block.tool_use_id
        ? { ...s, result: contentText(block.content), state: block.is_error ? 'error' : 'done' }
        : s
    );
  }
  return next === open ? c : replaceOpenTurn(c, next);
}

function applyStreamEvent(
  t: OpenTurn,
  ev: { type: string; index?: number; content_block?: Block; delta?: Record<string, unknown> }
): OpenTurn {
  const next = cloneTurn(t);
  const idx = ev.index ?? -1;
  switch (ev.type) {
    case 'content_block_start': {
      const b = ev.content_block;
      if (!b) return t;
      if (b.type === 'text') {
        next._blocks[idx] =
          next.segments.push({ kind: 'prose', text: b.text ?? '', streaming: true }) - 1;
      } else if (b.type === 'thinking') {
        next._blocks[idx] = next.segments.push({ kind: 'thinking', text: b.thinking ?? '' }) - 1;
        next._thinkingIndex = idx;
        next.thinking = true;
      } else if (b.type === 'tool_use') {
        next._blocks[idx] =
          next.segments.push({
            kind: 'tool',
            id: b.id ?? '',
            name: b.name ?? '',
            input: b.input ?? {},
            inputJson: '',
            result: '',
            state: 'preparing',
            subtools: [],
          }) - 1;
      } else {
        return t;
      }
      return next;
    }
    case 'content_block_delta': {
      const si = next._blocks[idx];
      const seg = si === undefined ? undefined : next.segments[si];
      const d = ev.delta ?? {};
      if (!seg) return t;
      if (d.type === 'text_delta' && seg.kind === 'prose')
        next.segments[si] = { ...seg, text: seg.text + String(d.text ?? '') };
      else if (d.type === 'thinking_delta' && seg.kind === 'thinking')
        next.segments[si] = { ...seg, text: seg.text + String(d.thinking ?? '') };
      else if (d.type === 'input_json_delta' && seg.kind === 'tool')
        next.segments[si] = { ...seg, inputJson: seg.inputJson + String(d.partial_json ?? '') };
      else return t;
      return next;
    }
    case 'content_block_stop': {
      if (next._thinkingIndex === idx) {
        next._thinkingIndex = null;
        next.thinking = false;
        const si = next._blocks[idx];
        const seg = si === undefined ? undefined : next.segments[si];
        if (seg?.kind === 'thinking' && seg.text.trim() === '') dropSegment(next, si!);
      }
      return next;
    }
    default:
      return t;
  }
}

function applyAssistant(t: OpenTurn, line: HarnessLine): OpenTurn {
  const content = (line.message as { content?: Block[] } | undefined)?.content;
  if (!Array.isArray(content)) return t;
  const next = cloneTurn(t);
  for (const b of content) {
    if (b.type === 'text') {
      const si = findLast(next.segments, (s) => s.kind === 'prose' && s.streaming);
      if (si >= 0) next.segments[si] = { kind: 'prose', text: b.text ?? '', streaming: false };
      else if ((b.text ?? '') !== '')
        next.segments.push({ kind: 'prose', text: b.text ?? '', streaming: false });
    } else if (b.type === 'thinking') {
      const si = findLast(next.segments, (s) => s.kind === 'thinking');
      const text = b.thinking ?? '';
      if (si >= 0) {
        if (text.trim() === '') dropSegment(next, si);
        else next.segments[si] = { kind: 'thinking', text };
      } else if (text.trim() !== '') {
        next.segments.push({ kind: 'thinking', text });
      }
    } else if (b.type === 'tool_use') {
      const si = next.segments.findIndex((s) => s.kind === 'tool' && s.id === b.id);
      const seg: ToolSegment = {
        kind: 'tool',
        id: b.id ?? '',
        name: b.name ?? '',
        input: b.input ?? {},
        inputJson: JSON.stringify(b.input ?? {}),
        result: '',
        state: 'running',
        subtools: [],
      };
      if (si >= 0) {
        const existing = next.segments[si] as ToolSegment;
        next.segments[si] = {
          ...existing,
          ...seg,
          result: existing.result,
          subtools: existing.subtools,
        };
      } else next.segments.push(seg);
    }
  }
  return next;
}

function applyControlRequest(t: OpenTurn, line: HarnessLine): OpenTurn {
  const req = line.request as
    | {
        subtype?: string;
        tool_name?: string;
        input?: Record<string, unknown>;
        tool_use_id?: string;
        requires_user_interaction?: boolean;
      }
    | undefined;
  const rid = line.request_id as string | undefined;
  if (!req || req.subtype !== 'can_use_tool' || !rid) return t;
  const questions =
    req.requires_user_interaction && Array.isArray(req.input?.questions)
      ? (req.input!.questions as Omit<Question, 'id'>[]).map((q) => ({ ...q, id: q.question }))
      : null;
  const pending: PendingSegment = {
    kind: 'pending',
    requestId: rid,
    toolUseId: req.tool_use_id ?? '',
    toolName: req.tool_name ?? '',
    input: req.input ?? {},
    questions,
  };
  const next = cloneTurn(t);
  const ti = next.segments.findIndex((s) => s.kind === 'tool' && s.id === pending.toolUseId);
  if (ti >= 0) {
    next.segments.splice(ti + 1, 0, pending);
    return next;
  }
  const pi = next.segments.findIndex(
    (s) => s.kind === 'tool' && s.subtools.some((sc) => sc.id === pending.toolUseId)
  );
  if (pi >= 0) next.segments.splice(pi + 1, 0, pending);
  else next.segments.push(pending);
  return next;
}

// claudeResolvedRequestId mirrors the records applyControl/applyHarness use
// to remove a pending segment: a control_response echoes the resolved request
// id; control_cancel_request cancels one. useChatSocket clears the saved
// answers draft when a request resolves.
export function claudeResolvedRequestId(r: ConversationRecord): string | null {
  if (r.type === 'control' && r.line.type === 'control_response') {
    const rid = (r.line.response as { request_id?: string } | undefined)?.request_id;
    if (rid) return rid;
  }
  if (r.type === 'harness' && r.line.type === 'control_cancel_request') {
    const rid = r.line.request_id as string | undefined;
    if (rid) return rid;
  }
  return null;
}

function endTurn(c: Conversation, open: OpenTurn, line: HarnessLine): Conversation {
  let end: NonNullable<NonNullable<AssistantTurn['end']>>;
  if (open.interrupted) end = { state: 'stopped' };
  else if (line.is_error === true)
    end = {
      state: 'error',
      text:
        typeof line.result === 'string' && line.result
          ? line.result
          : String(line.subtype ?? 'error'),
    };
  else end = { state: 'done' };
  const closed = replaceOpenTurn(c, closeTurn(open, end));
  const qi = closed.items.findIndex((i) => i.kind === 'user' && i.queued);
  if (qi < 0) return closed;
  const items = closed.items.slice();
  items.splice(qi + 1, 0, newTurn());
  return { items, phase: 'running' };
}
