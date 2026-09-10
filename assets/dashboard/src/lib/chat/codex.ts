// Codex app-server dialect → the same Conversation model the Claude reducer
// produces. One rule per line shape; see the spec's section 7 table.
import {
  upsertOperation,
  updateOperation,
  upsertChecklistEntry,
  emptyActivity,
  clearRetries,
  type ActivityState,
  type Operation,
  type OperationLifecycle,
} from './activity';
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
  commandActions?: unknown;
  cwd?: string;
  aggregatedOutput?: string | null;
  exitCode?: number | null;
  status?: string;
  kind?: string;
  agentThreadId?: string;
  agentPath?: string;
  changes?: FileChange[];
  server?: string;
  tool?: string;
  arguments?: unknown;
  result?: { content?: { type: string; text?: string }[] } | string | null;
}

interface CommandAction {
  type: 'read' | 'search' | 'listFiles' | 'unknown' | string;
  command: string;
  path?: string | null;
  name?: string | null;
  query?: string | null;
}

interface FileChange {
  path: string;
  kind?: string | { type?: string };
  diff?: string;
}

function commandRow(source: { command?: string; commandActions?: unknown }): {
  name: string;
  input: Record<string, unknown>;
} {
  const actions = (
    Array.isArray(source.commandActions) ? source.commandActions : []
  ) as CommandAction[];
  const wrapper = source.command ?? '';
  if (actions.length === 0) return { name: 'Bash', input: { command: wrapper } };
  if (actions.length === 1) {
    const a = actions[0];
    switch (a.type) {
      case 'read':
        return { name: 'Read', input: { file_path: a.path ?? a.name ?? '' } };
      case 'search':
        return { name: 'Search', input: { pattern: a.query ?? '', path: a.path ?? '' } };
      case 'listFiles':
        return { name: 'List', input: { file_path: a.path ?? '.' } };
      default:
        return { name: 'Bash', input: { command: a.command || wrapper } };
    }
  }
  if (actions.some((a) => a.type === 'unknown'))
    return { name: 'Bash', input: { command: wrapper } };
  const targets = actions
    .map((a) => (a.type === 'search' ? a.query : (a.path ?? a.name)))
    .filter((target): target is string => !!target)
    .join(', ');
  return { name: 'Explore', input: { command: targets } };
}

function changeKind(ch: FileChange): string {
  return typeof ch.kind === 'string' ? ch.kind : (ch.kind?.type ?? 'update');
}

function fileChangeRow(changes: FileChange[]): {
  name: string;
  input: Record<string, unknown>;
  result: string;
} {
  const names: Record<string, string> = { add: 'Write', update: 'Edit', delete: 'Delete' };
  const name = changes.length === 1 ? (names[changeKind(changes[0])] ?? 'Edit') : 'Edit';
  const input = { file_path: changes.map((change) => change.path).join(', ') };
  const result = changes.map((change) => `--- ${change.path}\n${change.diff ?? ''}`).join('\n');
  return { name, input, result };
}

const INVISIBLE_ITEMS = new Set([
  'userMessage',
  'agentMessage',
  'reasoning',
  'commandExecution',
  'fileChange',
  'mcpToolCall',
  'plan',
  'contextCompaction',
  'enteredReviewMode',
  'exitedReviewMode',
]);

function genericInput(item: Item): Record<string, unknown> {
  const {
    id: _id,
    type: _type,
    status: _status,
    ...rest
  } = item as unknown as Record<string, unknown>;
  return rest;
}

function genericResult(item: Item): string {
  const rec = item as unknown as Record<string, unknown>;
  for (const key of ['output', 'result', 'text']) {
    const value = rec[key];
    if (typeof value === 'string') return value;
    if (value !== undefined && value !== null) return JSON.stringify(value);
  }
  return JSON.stringify(genericInput(item));
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
      return applyHarness(c, r);
    default:
      return c;
  }
}

function applyUserMessage(
  c: Conversation,
  r: Extract<ConversationRecord, { type: 'user_message' }>
): Conversation {
  // New user message after the session ended (or after Restart re-uses the
  // old record) starts a fresh activity lifetime. The previous session's
  // background tasks and checklist belong to a different process and must
  // not be displayed in the new session.
  // A new user message also clears any lingering retry signal: the
  // foreground turn is moving on, "Retrying request" must not persist
  // into the new turn.
  const baseActivity = c.activity.live ? c.activity : emptyActivity();
  const activity = { ...clearRetries(baseActivity), acknowledgedAt: r.ts };
  const open = openTurn({ ...c, activity });
  if (open) {
    const next = cloneTurn(open);
    next.segments.push({ kind: 'user', id: r.id, text: r.text, images: r.images ?? [] });
    return { ...replaceOpenTurn(c, next), activity };
  }
  const msg: UserMessage = {
    kind: 'user',
    id: r.id,
    text: r.text,
    images: r.images ?? [],
    queued: false,
  };
  return { items: [...c.items, msg, newTurn()], phase: 'running', activity };
}

function applyControl(c: Conversation, line: HarnessLine): Conversation {
  const open = openTurn(c);
  if (!open) return c;
  if (line.method === 'turn/interrupt')
    return replaceOpenTurn(c, { ...cloneTurn(open), interrupted: true });
  if (line.method === undefined && line.id !== undefined && ('result' in line || 'error' in line))
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

function applyHarness(
  c: Conversation,
  r: Extract<ConversationRecord, { type: 'harness' }>
): Conversation {
  const line = r.line;
  const params = (line.params ?? {}) as Record<string, unknown>;
  const emittedMs = (line as { emittedAtMs?: number }).emittedAtMs;
  const ts = typeof emittedMs === 'number' ? new Date(emittedMs).toISOString() : r.ts;
  // The handshake identifies the parent. A turn/started fallback supports
  // fixture cuts and older histories without the handshake notification.
  if (!c.activity.codexThreadId) {
    const result = line.result as { thread?: { id?: string } } | undefined;
    const thread = (params.thread ?? (line.id === 3 ? result?.thread : undefined)) as
      { id?: string } | undefined;
    const threadId = thread?.id ?? (line.method === 'turn/started' ? params.threadId : undefined);
    if (typeof threadId === 'string')
      c = { ...c, activity: { ...c.activity, codexThreadId: threadId } };
  }
  if (
    c.activity.codexThreadId &&
    typeof params.threadId === 'string' &&
    params.threadId !== c.activity.codexThreadId &&
    line.id === undefined &&
    line.method !== 'serverRequest/resolved'
  ) {
    return { ...c, activity: applyChildThreadEvent(c.activity, line, params, ts) };
  }
  if (line.method === 'item/started' || String(line.method).endsWith('/delta')) {
    c = { ...c, activity: clearRetries(c.activity) };
  }
  // Session-level notifications live outside the open turn. They may arrive
  // with no open turn; handle them before the open-turn early return so
  // background operations and pending snapshots survive the turn boundary.
  if (line.method !== undefined) {
    const updated = applyCodexSessionEvent(c, line, r.ts);
    if (updated !== c) return updated;
  }
  // Collaboration tool-call items (item/started, item/completed) are
  // independently tracked. They may arrive after the parent turn is closed
  // (the target agent can finish its work after the parent result), so
  // handle them outside the open-turn gate.
  if (line.method === 'item/started' || line.method === 'item/completed') {
    const item = (line.params as { item?: Item } | undefined)?.item;
    if (item?.type === 'subAgentActivity') {
      const open = openTurn(c);
      const next = open ? replaceOpenTurn(c, itemStarted(open, item)) : c;
      return { ...next, activity: applySubAgentActivity(c.activity, item, ts) };
    }
    if (item?.type === 'collabAgentToolCall') {
      const emittedMs = (line as { emittedAtMs?: number }).emittedAtMs;
      const ts = typeof emittedMs === 'number' ? new Date(emittedMs).toISOString() : r.ts;
      return {
        ...c,
        activity: applyCollabAgentToolCall(
          c.activity,
          item as unknown as Record<string, unknown>,
          ts
        ),
      };
    }
  }
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
    return { items: [...c.items, failed], phase: 'idle', activity: c.activity };
  }
  if (!open) return c;
  const method = line.method as string | undefined;
  if (method === undefined) return c;

  switch (method) {
    case 'item/started': {
      const item = params.item as Item | undefined;
      if (item?.type === 'contextCompaction') {
        return {
          ...replaceOpenTurn(c, open),
          activity: applyCompactionStarted(c.activity, String(item.id ?? 'compaction'), ts),
        };
      }
      if (item?.type === 'collabAgentToolCall') {
        return {
          ...replaceOpenTurn(c, open),
          activity: applyCollabAgentToolCall(
            c.activity,
            item as unknown as Record<string, unknown>,
            ts
          ),
        };
      }
      // Ordinary tool items (commandExecution, fileChange, mcpToolCall) are
      // recorded as activity operations so the panel can show "Bash: ls" and
      // similar while they run. invisible items (reasoning, prose) stay out.
      const next = itemStarted(open, item);
      const toolOp = toolItemOp(item, ts, 'running');
      return {
        ...replaceOpenTurn(c, next),
        activity: toolOp ? upsertOperation(c.activity, toolOp) : c.activity,
      };
    }
    case 'item/completed': {
      const item = params.item as Item | undefined;
      if (item?.type === 'contextCompaction') {
        return {
          ...replaceOpenTurn(c, open),
          activity: applyCompactionCompleted(c.activity, String(item.id ?? 'compaction'), ts),
        };
      }
      if (item?.type === 'collabAgentToolCall') {
        return {
          ...replaceOpenTurn(c, open),
          activity: applyCollabAgentToolCall(
            c.activity,
            item as unknown as Record<string, unknown>,
            ts
          ),
        };
      }
      // Update the activity op for ordinary tool items when they complete:
      // status moves to finished/failed/stopped and lastUpdateAt advances.
      const next = itemCompleted(open, item);
      const completedOp = toolItemOp(item, ts, itemLifecycleForStatus(item?.status));
      return {
        ...replaceOpenTurn(c, next),
        activity: completedOp
          ? updateOperation(c.activity, 'codex-tool', String(item?.id ?? ''), completedOp)
          : c.activity,
      };
    }
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
    case 'item/reasoning/summaryPartAdded':
      return replaceOpenTurn(c, appendThinkingBreak(open, String(params.itemId)));
    case 'item/commandExecution/outputDelta':
    case 'item/fileChange/outputDelta':
      return replaceOpenTurn(
        c,
        appendToolOutput(open, String(params.itemId), String(params.delta ?? ''))
      );
    case 'item/commandExecution/requestApproval': {
      const { name, input } = commandRow(params as { command?: string; commandActions?: unknown });
      return replaceOpenTurn(
        c,
        insertPending(open, String(params.itemId), {
          kind: 'pending',
          requestId: String(line.id),
          toolUseId: String(params.itemId),
          toolName: name,
          input: { ...input, cwd: params.cwd, reason: params.reason },
          questions: null,
        })
      );
    }
    case 'item/fileChange/requestApproval': {
      const row = toolAt(open, String(params.itemId));
      return replaceOpenTurn(
        c,
        insertPending(open, String(params.itemId), {
          kind: 'pending',
          requestId: String(line.id),
          toolUseId: String(params.itemId),
          toolName: row?.name ?? 'Edit',
          input: {
            file_path: (row?.input as { file_path?: string })?.file_path ?? '',
            reason: params.reason,
            grantRoot: params.grantRoot,
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
      // A successful or terminal turn clears any leftover retry signal: the
      // foreground is back, "Retrying request" must not stick.
      const cleared = { ...c, activity: clearRetries(c.activity) };
      if (status === 'interrupted')
        return replaceOpenTurn(cleared, closeTurn(open, { state: 'stopped' }));
      if (status === 'failed')
        return replaceOpenTurn(
          cleared,
          closeTurn(open, { state: 'error', text: turn?.error?.message ?? 'failed' })
        );
      return replaceOpenTurn(cleared, closeTurn(open, { state: 'done' }));
    }
    case 'error': {
      if (params.willRetry === true) {
        // Retrying does not end the turn; surface it through a dedicated
        // operation so the headline can show "Retrying…".
        const emittedMs = (line as { emittedAtMs?: number }).emittedAtMs;
        const ts = typeof emittedMs === 'number' ? new Date(emittedMs).toISOString() : r.ts;
        return {
          ...c,
          activity: upsertOperation(c.activity, {
            namespace: 'codex-retry',
            id: 'retry-pending',
            kind: 'codex-retry',
            title: 'Retrying',
            ownerTurnId: null,
            parentId: null,
            lifecycle: 'running',
            rawStatus: null,
            firstObservedAt: ts,
            startTime: null,
            endTime: null,
            lastUpdateAt: ts,
            durationMs: null,
            latestActivity: null,
            usage: null,
            toolId: null,
            assignmentId: 0,
            outputFile: null,
            terminalAt: null,
          }),
        };
      }
      const err = params.error as { message?: string } | undefined;
      return replaceOpenTurn(c, closeTurn(open, { state: 'error', text: err?.message ?? 'error' }));
    }
    default:
      if (line.id !== undefined) {
        return replaceOpenTurn(
          c,
          insertPending(open, String(params.itemId ?? ''), {
            kind: 'pending',
            requestId: String(line.id),
            toolUseId: String(params.itemId ?? ''),
            toolName: method,
            input: params,
            questions: null,
            abortOnly: true,
          })
        );
      }
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
      const next = pushSeg(t, item.id, {
        kind: 'thinking',
        text: (item.summary ?? []).join('\n\n'),
      });
      next.thinking = true;
      return next;
    }
    case 'commandExecution': {
      const { name, input } = commandRow(item);
      return pushSeg(t, item.id, {
        kind: 'tool',
        id: item.id,
        name,
        input,
        inputJson: JSON.stringify({
          command: item.command,
          cwd: item.cwd,
          commandActions: item.commandActions ?? [],
        }),
        result: '',
        state: 'running',
        subtools: [],
      });
    }
    case 'fileChange': {
      const { name, input, result } = fileChangeRow(item.changes ?? []);
      return pushSeg(t, item.id, {
        kind: 'tool',
        id: item.id,
        name,
        input,
        inputJson: JSON.stringify({ changes: item.changes ?? [] }),
        result,
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
    case 'contextCompaction': {
      // Compaction is invisible to the transcript but must enter the
      // activity model so the user sees "Preparing conversation context…"
      // while it runs.
      return t;
    }
    case 'subAgentActivity': {
      if (item.kind !== 'started' || segIndex(t, item.id) !== undefined) return t;
      const input = { description: item.agentPath ?? item.agentThreadId ?? 'Agent' };
      return pushSeg(t, item.id, {
        kind: 'tool',
        id: item.id,
        name: 'Agent',
        input,
        inputJson: JSON.stringify(input),
        result: '',
        state: 'running',
        subtools: [],
      });
    }
    case 'collabAgentToolCall': {
      // The control-call item is rendered as part of the transcript by
      // itemCompleted below; activity rows are tracked by the session
      // notification handler (turn/collabAgentToolCall or item/completed
      // with type collabAgentToolCall arrives via applyCodexSessionEvent).
      return t;
    }
    default:
      if (INVISIBLE_ITEMS.has(item.type)) return t;
      {
        const input = genericInput(item);
        return pushSeg(t, item.id, {
          kind: 'tool',
          id: item.id,
          name: item.type,
          input,
          inputJson: JSON.stringify(input),
          result: '',
          state: 'running',
          subtools: [],
        });
      }
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
      const text = [...(item.summary ?? []), ...(item.content ?? [])].join('\n\n');
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
            : fileChangeRow(item.changes ?? []).result;
      next.segments[idx] = { ...existing, result, state: toolState(item.status) };
      return next;
    }
    case 'contextCompaction': {
      return t;
    }
    case 'collabAgentToolCall': {
      // Treat the control-call item completion as a session event: route
      // through the same handler as the equivalent notification so the
      // control-call/agent-state relationship is preserved.
      return t;
    }
    default:
      if (INVISIBLE_ITEMS.has(item.type)) return t;
      {
        const base = si === undefined ? itemStarted(t, { ...item, status: 'inProgress' }) : t;
        const idx = segIndex(base, item.id);
        if (idx === undefined) return base;
        const next = cloneTurn(base);
        const existing = next.segments[idx] as ToolSegment;
        next.segments[idx] = {
          ...existing,
          result: genericResult(item),
          state: item.status === undefined ? 'done' : toolState(item.status),
        };
        return next;
      }
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

function appendThinkingBreak(t: OpenTurn, itemId: string): OpenTurn {
  const si = segIndex(t, itemId);
  const seg = si === undefined ? undefined : t.segments[si];
  if (!seg || seg.kind !== 'thinking' || seg.text === '') return t;
  const next = cloneTurn(t);
  next.segments[si!] = { ...seg, text: seg.text + '\n\n' };
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

// codexResolvedRequestId mirrors the records applyControl/applyHarness use to
// remove a pending segment: a JSON-RPC response frame (result or error) and
// the serverRequest/resolved notification. useChatSocket clears the saved
// answers draft when a request resolves.
export function codexResolvedRequestId(r: ConversationRecord): string | null {
  if (r.type === 'control') {
    const line = r.line;
    if (line.method === undefined && line.id !== undefined && ('result' in line || 'error' in line))
      return String(line.id);
  }
  if (r.type === 'harness' && r.line.method === 'serverRequest/resolved') {
    const params = r.line.params as { requestId?: unknown } | undefined;
    if (params?.requestId !== undefined) return String(params.requestId);
  }
  return null;
}

// applyCodexSessionEvent maps Codex session-level notifications (plan
// updates, hook lifecycle, mcp server status, error retry, collab control
// calls) into the activity model. It returns a new conversation when the
// event produced a change, or the input when the event was unhandled or
// routed to the per-turn handler below.
function applyCodexSessionEvent(
  c: Conversation,
  line: HarnessLine,
  recordTs: string
): Conversation {
  const method = line.method as string | undefined;
  if (!method) return c;
  const params = (line.params ?? {}) as Record<string, unknown>;
  // The record ts is the durable source. emittedAtMs is a notification-local
  // ms-precision value that can be present in the harness line; prefer it
  // when available, fall back to recordTs. Both are converted to ISO so the
  // selectors can compare with Date.now().
  const emittedMs = (line as { emittedAtMs?: number }).emittedAtMs;
  const ts = typeof emittedMs === 'number' ? new Date(emittedMs).toISOString() : recordTs;
  switch (method) {
    case 'turn/plan/updated': {
      return { ...c, activity: applyPlanUpdated(c.activity, params, ts) };
    }
    case 'hook/started': {
      return { ...c, activity: applyCodexHookStarted(c.activity, params, ts) };
    }
    case 'hook/completed': {
      return { ...c, activity: applyCodexHookCompleted(c.activity, params, ts) };
    }
    case 'mcpServer/startupStatus/updated': {
      return { ...c, activity: applyMcpServerStatus(c.activity, params, ts) };
    }
    case 'collabAgentToolCall': {
      // The collaboration control call is a JSON-RPC response/request, not a
      // notification. We accept it as a session event because the call
      // identity is distinct from the receiver agent's identity.
      return { ...c, activity: applyCollabAgentToolCall(c.activity, params, ts) };
    }
    default:
      return c;
  }
}

function applyPlanUpdated(
  activity: ActivityState,
  params: Record<string, unknown>,
  ts: string
): ActivityState {
  const plan = (params.plan as { step: string; status: string }[] | undefined) ?? [];
  const explanation = (params.explanation as string | null | undefined) ?? null;
  // Replace the checklist atomically with the latest snapshot. The view
  // shows the explanation in the Plan disclosure header.
  const next: ActivityState = {
    ...activity,
    checklist: {},
    checklistOrder: [],
  };
  let i = 0;
  for (const step of plan) {
    const status =
      step.status === 'completed'
        ? 'completed'
        : step.status === 'inProgress'
          ? 'in-progress'
          : step.status === 'pending'
            ? 'pending'
            : 'unknown';
    const entry: import('./activity').ChecklistEntry = {
      id: `plan-${i++}`,
      subject: String(step.step ?? ''),
      status,
      lastChangeAt: ts,
      activeForm: explanation ?? undefined,
    };
    next.checklist[entry.id] = entry;
    next.checklistOrder.push(entry.id);
  }
  return next;
}

function applyCodexHookStarted(
  activity: ActivityState,
  params: Record<string, unknown>,
  ts: string
): ActivityState {
  // The schema-shaped notification nests the run summary under params.run;
  // legacy codex builds wrote top-level runId/name. Read the run summary
  // first and fall back to the legacy fields so older records still work.
  const run = (params.run as Record<string, unknown> | undefined) ?? null;
  const runId = String(
    (run?.id as string | undefined) ?? (params.runId as string | undefined) ?? ''
  );
  if (!runId) return activity;
  const eventName =
    (run?.eventName as string | undefined) ?? (params.name as string | undefined) ?? 'hook';
  const op: Operation = {
    namespace: 'codex-hook',
    id: runId,
    kind: 'codex-hook',
    title: `Running ${eventName}`,
    ownerTurnId: null,
    parentId: null,
    lifecycle: 'running',
    rawStatus: (run?.status as string | undefined) ?? null,
    firstObservedAt: ts,
    startTime: typeof run?.startedAt === 'number' ? run.startedAt * 1000 : null,
    endTime: null,
    lastUpdateAt: ts,
    durationMs: null,
    latestActivity: null,
    usage: null,
    toolId: null,
    assignmentId: 0,
    outputFile: null,
    terminalAt: null,
  };
  return upsertOperation(activity, op);
}

function applyCodexHookCompleted(
  activity: ActivityState,
  params: Record<string, unknown>,
  ts: string
): ActivityState {
  const run = (params.run as Record<string, unknown> | undefined) ?? null;
  const runId = String(
    (run?.id as string | undefined) ?? (params.runId as string | undefined) ?? ''
  );
  if (!runId) return activity;
  const status = (run?.status as string | undefined) ?? (params.status as string | undefined);
  const durationMs = (run?.durationMs as number | undefined) ?? null;
  const completedAt = typeof run?.completedAt === 'number' ? run.completedAt * 1000 : null;
  let lifecycle: OperationLifecycle = 'finished';
  if (status === 'failed' || status === 'blocked') {
    lifecycle = 'failed';
  } else if (status === 'stopped') {
    lifecycle = 'stopped';
  } else if (status && status !== 'completed') {
    lifecycle = 'status-unavailable';
  }
  return updateOperation(activity, 'codex-hook', runId, {
    lifecycle,
    rawStatus: status ?? null,
    lastUpdateAt: ts,
    terminalAt: ts,
    durationMs,
    endTime: completedAt,
  });
}

function applyMcpServerStatus(
  activity: ActivityState,
  params: Record<string, unknown>,
  ts: string
): ActivityState {
  const name = String((params.name as string | undefined) ?? 'mcp');
  const status = String((params.status as string | undefined) ?? '');
  const failureReason = (params.failureReason as string | null | undefined) ?? null;
  let lifecycle: OperationLifecycle = 'running';
  if (status === 'ready' || status === 'started') lifecycle = 'finished';
  else if (status === 'failed') lifecycle = 'failed';
  else if (status === 'starting') lifecycle = 'preparing';
  const op: Operation = {
    namespace: 'codex-hook',
    id: `mcp-${name}`,
    kind: 'codex-hook',
    title: `Connecting ${name}`,
    ownerTurnId: null,
    parentId: null,
    lifecycle,
    rawStatus: status,
    firstObservedAt: ts,
    startTime: null,
    endTime: null,
    lastUpdateAt: ts,
    durationMs: null,
    latestActivity: failureReason,
    usage: null,
    toolId: null,
    assignmentId: 0,
    outputFile: null,
    terminalAt: status === 'ready' || status === 'started' || status === 'failed' ? ts : null,
  };
  return upsertOperation(activity, op);
}

function applySubAgentActivity(activity: ActivityState, item: Item, ts: string): ActivityState {
  if (!item.agentThreadId) return activity;
  const id = item.agentThreadId;
  const existing = activity.operations[`codex-agent:${id}`];
  // item/completed with kind "started" completes the launch, not the child.
  // Both item notifications describe the same transition; keep its clock.
  const status = item.kind === 'started' ? 'running' : item.kind;
  const next =
    existing?.rawStatus === status
      ? activity
      : applyAgentSnapshot(
          activity,
          id,
          { status: status ?? '', message: existing?.latestActivity },
          ts,
          { isObservation: true }
        );
  return updateOperation(next, 'codex-agent', id, {
    title: item.agentPath ?? existing?.title ?? `Agent ${id.slice(0, 6)}`,
    toolId: existing?.toolId ?? (item.kind === 'started' ? item.id : null),
  });
}

function applyChildThreadEvent(
  activity: ActivityState,
  line: HarnessLine,
  params: Record<string, unknown>,
  ts: string
): ActivityState {
  const id = params.threadId as string;
  const existing = activity.operations[`codex-agent:${id}`];
  if (line.method === 'turn/started') {
    // A new child turn is an explicit assignment, unlike a wait snapshot.
    return applyAgentSnapshot(activity, id, { status: 'running' }, ts);
  }
  if (line.method === 'turn/completed') {
    const turn = params.turn as { status: string; items?: Item[]; error?: { message?: string } };
    const message = turn.items
      ?.filter((item) => item.type === 'agentMessage')
      .map((item) => item.text ?? '')
      .join('\n');
    return applyAgentSnapshot(
      activity,
      id,
      {
        status: turn.status === 'failed' ? 'errored' : turn.status,
        message: turn.error?.message ?? (message || existing?.latestActivity),
      },
      ts
    );
  }
  if (line.method === 'item/completed') {
    const item = params.item as Item | undefined;
    if (item?.type === 'agentMessage')
      return updateOperation(activity, 'codex-agent', id, {
        latestActivity: item.text ?? null,
        lastUpdateAt: ts,
      });
  }
  // Child prose, reasoning, startup hooks, and plans belong to that child.
  // They must not mutate the parent's transcript or checklist.
  return activity;
}

function applyCollabAgentToolCall(
  activity: ActivityState,
  params: Record<string, unknown>,
  ts: string
): ActivityState {
  const id = String((params.id as string | undefined) ?? '');
  if (!id) return activity;
  const tool = String((params.tool as string | undefined) ?? 'collab');
  const status = String((params.status as string | undefined) ?? '');
  const sender = String((params.senderThreadId as string | undefined) ?? '');
  const receivers = (params.receiverThreadIds as string[] | undefined) ?? [];
  const agentsStates =
    (params.agentsStates as
      Record<string, { status: string; message?: string | null }> | undefined) ?? {};
  // Track the control call as a codex-control operation so wait/send/list
  // calls do not inflate the worker count, but their receiver-agent targets
  // do.
  const controlLifecycle: OperationLifecycle =
    status === 'completed'
      ? 'finished'
      : status === 'failed'
        ? 'failed'
        : status === 'inProgress'
          ? 'running'
          : 'status-unavailable';
  const controlOp: Operation = {
    namespace: 'codex-control',
    id,
    kind: 'codex-control',
    title: tool,
    ownerTurnId: sender || null,
    parentId: null,
    lifecycle: controlLifecycle,
    rawStatus: status,
    firstObservedAt: ts,
    startTime: null,
    endTime: null,
    lastUpdateAt: ts,
    durationMs: null,
    latestActivity: null,
    usage: null,
    toolId: null,
    assignmentId: 0,
    outputFile: null,
    terminalAt: status === 'completed' || status === 'failed' ? ts : null,
  };
  let next = upsertOperation(activity, controlOp);
  // Apply only supplied target-agent snapshots. Missing entries or empty
  // wait results do not clear known agents.
  for (const receiverId of receivers) {
    const state = agentsStates[receiverId];
    if (!state) continue;
    // Observation calls (wait, listAgents) report target snapshots but do
    // not start new work; they must not reset the assignment clock or
    // reopen a terminal assignment. Dispatch/resume calls do.
    const isObservation = tool === 'wait' || tool === 'listAgents';
    next = applyAgentSnapshot(next, receiverId, state, ts, { isObservation });
  }
  return next;
}

function agentLifecycle(raw: string): OperationLifecycle {
  switch (raw) {
    case 'pendingInit':
      return 'preparing';
    case 'running':
      return 'running';
    case 'completed':
      return 'finished';
    case 'interrupted':
      return 'stopped';
    case 'errored':
      return 'failed';
    case 'shutdown':
      return 'stopped';
    case 'notFound':
      return 'status-unavailable';
    default:
      return 'status-unavailable';
  }
}

function applyAgentSnapshot(
  activity: ActivityState,
  agentId: string,
  state: { status: string; message?: string | null },
  ts: string,
  opts: { isObservation?: boolean } = {}
): ActivityState {
  const lifecycle = agentLifecycle(state.status);
  const existing = activity.operations[`codex-agent:${agentId}`];
  // Observation calls (wait, listAgents) report target snapshots but do
  // not start new work; they must not reset the assignment clock or
  // reopen a terminal assignment. The terminal lifecycle is preserved.
  if (opts.isObservation && existing?.lifecycle === 'finished') {
    return activity;
  }
  // Assignment clock: a new explicit dispatch or resume restarts the
  // activity timing. A completed state never reopens on stale snapshots:
  // only an explicit dispatch or resume that follows a finished
  // assignment brings the agent back with a fresh clock. The agent
  // identity is preserved.
  const isResumption =
    !!existing &&
    existing.lifecycle === 'finished' &&
    lifecycle !== 'finished' &&
    lifecycle !== 'status-unavailable';
  const newAssignment = !existing || isResumption;
  const op: Operation = {
    namespace: 'codex-agent',
    id: agentId,
    kind: 'codex-agent',
    title: existing?.title ?? `Agent ${agentId.slice(0, 6)}`,
    ownerTurnId: null,
    parentId: null,
    lifecycle,
    rawStatus: state.status,
    firstObservedAt: existing?.firstObservedAt ?? ts,
    startTime: newAssignment ? Date.parse(ts) || null : (existing?.startTime ?? null),
    endTime:
      lifecycle === 'finished' || lifecycle === 'failed' || lifecycle === 'stopped'
        ? Date.parse(ts) || null
        : null,
    lastUpdateAt: ts,
    durationMs: null,
    latestActivity: state.message ?? null,
    usage: null,
    toolId: null,
    assignmentId: newAssignment ? (existing?.assignmentId ?? 0) + 1 : (existing?.assignmentId ?? 0),
    outputFile: null,
    terminalAt:
      lifecycle === 'finished' || lifecycle === 'failed' || lifecycle === 'stopped' ? ts : null,
  };
  return upsertOperation(activity, op);
}

function applyCompactionStarted(activity: ActivityState, id: string, ts: string): ActivityState {
  const op: Operation = {
    namespace: 'codex-compaction',
    id,
    kind: 'codex-compaction',
    title: 'Preparing conversation context…',
    ownerTurnId: null,
    parentId: null,
    lifecycle: 'running',
    rawStatus: null,
    firstObservedAt: ts,
    startTime: null,
    endTime: null,
    lastUpdateAt: ts,
    durationMs: null,
    latestActivity: null,
    usage: null,
    toolId: null,
    assignmentId: 0,
    outputFile: null,
    terminalAt: null,
  };
  return upsertOperation(activity, op);
}

function applyCompactionCompleted(activity: ActivityState, id: string, ts: string): ActivityState {
  return updateOperation(activity, 'codex-compaction', id, {
    lifecycle: 'finished',
    lastUpdateAt: ts,
    terminalAt: ts,
  });
}

// toolItemOp produces an Operation for ordinary tool items. The
// commandRow/fileChangeRow helpers in this file already compute a
// user-facing title; we mirror that here for the activity row. Returns
// null for invisible items (prose, reasoning, etc.) so the caller can
// leave the activity table unchanged.
function toolItemOp(
  item: Item | undefined,
  ts: string,
  lifecycle: OperationLifecycle
): Operation | null {
  if (!item || !item.id) return null;
  switch (item.type) {
    case 'commandExecution': {
      const { name, input } = commandRow(item);
      return {
        namespace: 'codex-tool',
        id: String(item.id),
        kind: 'codex-tool',
        title: `${name}: ${summarizeToolInput(name, input)}`,
        ownerTurnId: null,
        parentId: null,
        lifecycle,
        rawStatus: item.status ?? null,
        firstObservedAt: ts,
        startTime: null,
        endTime: null,
        lastUpdateAt: ts,
        durationMs: null,
        latestActivity: null,
        usage: null,
        toolId: String(item.id),
        assignmentId: 0,
        outputFile: null,
        terminalAt:
          lifecycle === 'finished' || lifecycle === 'failed' || lifecycle === 'stopped' ? ts : null,
      };
    }
    case 'fileChange': {
      const { name, input } = fileChangeRow(item.changes ?? []);
      return {
        namespace: 'codex-tool',
        id: String(item.id),
        kind: 'codex-tool',
        title: `${name}: ${summarizeToolInput(name, input)}`,
        ownerTurnId: null,
        parentId: null,
        lifecycle,
        rawStatus: item.status ?? null,
        firstObservedAt: ts,
        startTime: null,
        endTime: null,
        lastUpdateAt: ts,
        durationMs: null,
        latestActivity: null,
        usage: null,
        toolId: String(item.id),
        assignmentId: 0,
        outputFile: null,
        terminalAt:
          lifecycle === 'finished' || lifecycle === 'failed' || lifecycle === 'stopped' ? ts : null,
      };
    }
    case 'mcpToolCall': {
      const input = item.arguments ?? {};
      return {
        namespace: 'codex-tool',
        id: String(item.id),
        kind: 'codex-tool',
        title: `${item.server ?? ''}/${item.tool ?? ''}`,
        ownerTurnId: null,
        parentId: null,
        lifecycle,
        rawStatus: item.status ?? null,
        firstObservedAt: ts,
        startTime: null,
        endTime: null,
        lastUpdateAt: ts,
        durationMs: null,
        latestActivity: null,
        usage: null,
        toolId: String(item.id),
        assignmentId: 0,
        outputFile: null,
        terminalAt:
          lifecycle === 'finished' || lifecycle === 'failed' || lifecycle === 'stopped' ? ts : null,
      };
    }
    default:
      return null;
  }
}

function itemLifecycleForStatus(status: string | undefined): OperationLifecycle {
  if (status === undefined) return 'status-unavailable';
  if (status === 'completed') return 'finished';
  if (status === 'failed') return 'failed';
  if (status === 'inProgress' || status === 'in_progress') return 'running';
  if (status === 'interrupted') return 'stopped';
  return 'status-unavailable';
}

function summarizeToolInput(name: string, input: Record<string, unknown>): string {
  switch (name) {
    case 'Bash':
      return String(input.command ?? '');
    case 'Edit':
    case 'Write':
    case 'Read':
    case 'List':
      return String(input.file_path ?? '');
    case 'Search':
    case 'Explore':
      return String(input.pattern ?? input.command ?? '');
    default:
      return '';
  }
}
