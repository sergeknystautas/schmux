// Claude stream-json reducer: today's rules, moved unchanged. Helpers used
// here are exported from reducer.ts.
import {
  upsertOperation,
  operationForTool,
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
    case 'harness': {
      const next = applyHarness(c, r);
      // Phase-only heartbeats cannot change tool segments or tool
      // operations, so the per-record segment sync is pure overhead — and
      // these heartbeats dominate long thinking sessions.
      if (isPhaseHeartbeat(r.line)) return next;
      return syncClaudeTools(next, r.ts);
    }
    default:
      return c;
  }
}

// thinking_tokens and status system events update only activity.phase: no
// tool segment or operation they could influence changes, so records of
// these subtypes skip the syncClaudeTools walk entirely. The next ordinary
// record resyncs anything that changed in the meantime.
function isPhaseHeartbeat(line: HarnessLine): boolean {
  return (
    line.type === 'system' && (line.subtype === 'thinking_tokens' || line.subtype === 'status')
  );
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
  // New user message after the session ended (or after Restart re-uses the
  // old record) starts a fresh activity lifetime. The previous session's
  // background tasks and checklist belong to a different process and must
  // not be displayed in the new session.
  // A new user message also clears any lingering retry signal: the
  // foreground turn is moving on, and "Retrying request" must not persist
  // into the new turn.
  const baseActivity = c.activity.live ? c.activity : emptyActivity();
  const activity = { ...clearRetries(baseActivity), phase: null, acknowledgedAt: r.ts };
  if (open) return { items, phase: 'running', activity };
  return { items: [...items, newTurn()], phase: 'running', activity };
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
    if (!rid) return c;
    const next = replaceOpenTurn(c, removePending(open, rid));
    // The answered request may belong to a subagent's question; clear
    // the owning Agent's pending-input lifecycle.
    const agentToolId = findAgentToolIdForRequest(open, rid);
    if (agentToolId) {
      return {
        ...next,
        activity: clearAgentPendingInput(c.activity, agentToolId, removePending(open, rid!)),
      };
    }
    return next;
  }
  return c;
}

function applyHarness(
  c: Conversation,
  r: Extract<ConversationRecord, { type: 'harness' }>
): Conversation {
  const line = r.line;
  if (
    !line.parent_tool_use_id &&
    (line.type === 'assistant' || line.type === 'stream_event' || line.type === 'result')
  ) {
    c = { ...c, activity: { ...clearRetries(c.activity), phase: null } };
  }
  if (line.type === 'tool_progress')
    return { ...c, activity: applyToolProgress(c.activity, line, r.ts) };
  // Session-level events live outside the open turn. They may arrive with no
  // open turn or carry parent_tool_use_id without being a child assistant
  // message; handle them before the open-turn and parent-tool early returns.
  if (line.type === 'system') {
    const updated = applySystemEvent(c, line, r.ts);
    if (updated !== c) return updated;
  }
  // Structured tool_use_result (TaskCreate/TaskUpdate/Agent async result)
  // updates the activity model AND must still flow through the normal
  // transcript path that completes the originating tool. Compose both
  // effects: a record with a tool_use_result still carries the same
  // tool_result content blocks the reducer uses to mark tools as done.
  if (line.type === 'user' && line.isReplay !== true) {
    const cWithEnvelope = applyToolResultEnvelope(c, line, r.ts);
    if (cWithEnvelope !== c) {
      const open = openTurn(cWithEnvelope);
      const next = applyHarnessUser(cWithEnvelope, open, line);
      if (next !== cWithEnvelope) return next;
      return cWithEnvelope;
    }
  }
  if (line.parent_tool_use_id && line.type !== 'control_request') {
    const open = openTurn(c);
    return open ? replaceOpenTurn(c, applySubagent(open, line)) : c;
  }
  const open = openTurn(c);
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
    case 'control_request': {
      const nextTurn = applyControlRequest(open, line);
      const updated = replaceOpenTurn(c, nextTurn);
      // Subagent control_request signals the owning Agent needs input from
      // the user. Find the parent tool by id and mark the matching
      // claude-agent op as pending-input. If the pending segment was
      // added at top level (no subagent), there is no agent op to mark.
      const agentToolId = findAgentToolIdForPending(nextTurn, line);
      if (agentToolId) {
        return { ...updated, activity: markAgentPendingInput(c.activity, agentToolId) };
      }
      return updated;
    }
    case 'control_cancel_request': {
      const rid = line.request_id as string | undefined;
      const next = rid ? replaceOpenTurn(c, removePending(open, rid)) : c;
      // Clear the pending-input lifecycle on the owning agent when its
      // subagent's request is cancelled.
      const agentToolId = rid ? findAgentToolIdForRequest(open, rid) : undefined;
      if (agentToolId) {
        return {
          ...next,
          activity: clearAgentPendingInput(c.activity, agentToolId, removePending(open, rid!)),
        };
      }
      return next;
    }
    case 'result':
      return { ...endTurn(c, open, line), activity: clearRetries(c.activity) };
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

// findAgentToolIdForPending locates the Agent tool whose child produced
// the pending can_use_tool control_request. The subagent is a child of
// the Agent tool; the pending is attached to the Agent tool's
// subtools. We identify the parent by finding the tool with name
// 'Agent' that owns the pending's tool_use_id.
function findAgentToolIdForPending(t: OpenTurn, line: HarnessLine): string | null {
  const req = (line.request as { tool_use_id?: string } | undefined) ?? undefined;
  const toolUseId = req?.tool_use_id;
  const parent = t.segments.find(
    (s) => s.kind === 'tool' && s.name === 'Agent' && s.id === line.parent_tool_use_id
  );
  if (parent && parent.kind === 'tool') return parent.id;
  if (!toolUseId) return null;
  for (const s of t.segments) {
    if (s.kind !== 'tool') continue;
    if (s.subtools.some((sc) => sc.id === toolUseId)) {
      if (s.name === 'Agent') return s.id;
    }
  }
  return null;
}

// findAgentToolIdForRequest walks the turn's segments to find the Agent
// tool that owns the pending segment with the given request id. Pending
// segments for a subagent's question sit at the top level of the turn
// (placed just after the Agent tool by applyControlRequest).
function findAgentToolIdForRequest(t: OpenTurn, requestId: string): string | null {
  const pending = t.segments.find((s) => s.kind === 'pending' && s.requestId === requestId);
  if (!pending || pending.kind !== 'pending') return null;
  return findAgentToolIdForPending(t, {
    type: 'control_request',
    request: { tool_use_id: pending.toolUseId },
  });
}

function markAgentPendingInput(activity: ActivityState, toolId: string): ActivityState {
  const existing = operationForTool(activity, toolId);
  if (!existing || existing.terminalAt) return activity;
  return updateOperation(activity, existing.namespace, existing.id, { lifecycle: 'pending-input' });
}

function clearAgentPendingInput(
  activity: ActivityState,
  toolId: string,
  turn: OpenTurn
): ActivityState {
  // A second outstanding question from the same agent still requires input.
  if (
    turn.segments.some(
      (s) => s.kind === 'pending' && findAgentToolIdForRequest(turn, s.requestId) === toolId
    )
  )
    return activity;
  const existing = operationForTool(activity, toolId);
  if (!existing || existing.lifecycle !== 'pending-input') return activity;
  return updateOperation(activity, existing.namespace, existing.id, {
    lifecycle: existing.id === toolId ? 'running' : 'running-background',
  });
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
  return { items, phase: 'running', activity: closed.activity };
}

// applySystemEvent maps Claude's session-level events (status, thinking
// estimates, tasks, agents, hooks, retries, background snapshots) into the
// activity model. It returns a new conversation when the event produced a
// change, or the input when the event was unhandled.
//
// recordTs is the durable ConversationRecord.ts from the daemon. The inner
// harness line frequently omits or falsifies a timestamp (Claude stream-json
// events often carry no `timestamp` field at all), so the record's ts is the
// authoritative source for first-observed, last-update, and terminal times.
function applySystemEvent(c: Conversation, line: HarnessLine, recordTs: string): Conversation {
  const sub = line.subtype as string | undefined;
  const ts = recordTs;
  switch (sub) {
    case 'status': {
      // system/status with status: requesting means the harness is waiting
      // for the model; we surface this through a transient attention outcome
      // so the headline can show "Waiting for response...".
      const status = (line as { status?: string }).status;
      if (status === 'requesting') {
        return { ...c, activity: { ...c.activity, phase: 'waiting' } };
      }
      return c;
    }
    case 'thinking_tokens': {
      return { ...c, activity: { ...c.activity, phase: 'thinking' } };
    }
    case 'api_retry': {
      // system/api_retry surfaces only on the foreground turn. We use a
      // dedicated operation keyed by attempt number.
      const attempt = (line as { attempt?: number }).attempt;
      const maxAttempts = (line as { maxAttempts?: number }).maxAttempts;
      const delayMs = (line as { retryDelayMs?: number }).retryDelayMs;
      const id = attempt !== undefined ? `attempt-${attempt}` : 'retry-pending';
      const op: Operation = {
        namespace: 'claude-retry',
        id,
        kind: 'claude-retry',
        title: `Retrying (attempt ${attempt ?? '?'}${maxAttempts ? ` of ${maxAttempts}` : ''})`,
        ownerTurnId: null,
        parentId: null,
        lifecycle: 'running',
        rawStatus: null,
        firstObservedAt: ts,
        startTime: null,
        endTime: null,
        lastUpdateAt: ts,
        durationMs: delayMs ?? null,
        latestActivity: null,
        usage: null,
        toolId: null,
        assignmentId: 0,
        outputFile: null,
        terminalAt: null,
      };
      return { ...c, activity: upsertOperation(c.activity, op) };
    }
    case 'task_started': {
      return { ...c, activity: applyTaskStarted(c.activity, line, ts) };
    }
    case 'task_progress': {
      return { ...c, activity: applyTaskProgress(c.activity, line, ts) };
    }
    case 'task_updated': {
      return { ...c, activity: applyTaskUpdated(c.activity, line, ts) };
    }
    case 'task_notification': {
      const updated = { ...c, activity: applyTaskNotification(c.activity, line, ts) };
      // A top-level task notification can wake an idle assistant. Preserve
      // the activity update and open a turn for its follow-up reply together;
      // this event is consumed before the ordinary transcript handlers run.
      if (!line.parent_tool_use_id && !openTurn(c)) {
        return { ...updated, items: [...c.items, newTurn()], phase: 'running' };
      }
      return updated;
    }
    case 'background_tasks_changed': {
      return { ...c, activity: applyBackgroundTasksChanged(c.activity, line, ts) };
    }
    case 'hook_started': {
      return { ...c, activity: applyHookStarted(c.activity, line, ts) };
    }
    case 'hook_response': {
      return { ...c, activity: applyHookResponse(c.activity, line, ts) };
    }
    case 'tool_progress': {
      // Heartbeat-style progress for a still-running tool. The wire shape
      // carries either parent_tool_use_id (subagent heartbeats) or tool_use_id
      // (top-level tool heartbeats). The reducer must not create a worker per
      // heartbeat id; it updates the existing tool op.
      return { ...c, activity: applyToolProgress(c.activity, line, ts) };
    }
    default:
      return c;
  }
}

function lifecycleForTaskStatus(status: string | undefined): OperationLifecycle {
  if (!status) return 'status-unavailable';
  switch (status) {
    case 'completed':
      return 'finished';
    case 'failed':
      return 'failed';
    case 'stopped':
    case 'interrupted':
      return 'stopped';
    case 'running':
      return 'running';
    case 'pending_init':
    case 'preparing':
      return 'preparing';
    default:
      return 'status-unavailable';
  }
}

function taskOpFromEvent(line: HarnessLine, ts: string, patch: Partial<Operation> = {}): Operation {
  const taskId = String((line as { task_id?: string }).task_id ?? 'unknown');
  const description = String((line as { description?: string }).description ?? 'Task');
  const isBackgrounded = (line as { is_backgrounded?: boolean }).is_backgrounded === true;
  // Async Agent launches and their follow-on task events share an id; pick
  // the agent namespace for them so the same id maps to one operation, not
  // a duplicate worker in the headline.
  const isAgent = (line as { task_type?: string }).task_type === 'local_agent';
  return {
    namespace: isAgent ? 'claude-agent' : 'claude-task',
    id: taskId,
    kind: isAgent ? 'claude-agent' : 'claude-task',
    title: description,
    ownerTurnId: null,
    parentId: null,
    lifecycle: isBackgrounded ? 'running-background' : 'preparing',
    rawStatus: null,
    firstObservedAt: ts,
    startTime: null,
    endTime: null,
    lastUpdateAt: ts,
    durationMs: null,
    latestActivity: null,
    usage: null,
    toolId: typeof line.tool_use_id === 'string' ? line.tool_use_id : null,
    assignmentId: 0,
    outputFile: null,
    terminalAt: null,
    ...patch,
  };
}

function applyTaskStarted(activity: ActivityState, line: HarnessLine, ts: string): ActivityState {
  const op = taskOpFromEvent(line, ts, {
    lifecycle: (line as { is_backgrounded?: boolean }).is_backgrounded
      ? 'running-background'
      : 'running',
    outputFile: (line as { output_file?: string }).output_file ?? null,
  });
  return upsertClaudeTask(activity, op);
}

// resolveTaskNamespace finds the single namespace the task_id lives in so
// the reducer updates one operation, not a phantom duplicate. Background
// tasks use claude-task; async Agent launches use claude-agent. The same
// task id may only exist in one of them.
function resolveTaskNamespace(
  activity: ActivityState,
  taskId: string
): 'claude-task' | 'claude-agent' | null {
  if (activity.operations[`claude-agent:${taskId}`]) return 'claude-agent';
  if (activity.operations[`claude-task:${taskId}`]) return 'claude-task';
  return null;
}

function applyTaskProgress(activity: ActivityState, line: HarnessLine, ts: string): ActivityState {
  const taskId = String((line as { task_id?: string }).task_id ?? '');
  if (!taskId) return activity;
  const latest = (line as { last_tool_name?: string }).last_tool_name;
  const usage = (
    line as { usage?: { tool_uses?: number; duration_ms?: number; total_tokens?: number } }
  ).usage;
  const update = {
    latestActivity: latest ?? null,
    lastUpdateAt: ts,
    usage: usage
      ? {
          toolUses: usage.tool_uses,
          durationMs: usage.duration_ms,
          totalTokens: usage.total_tokens,
        }
      : null,
  };
  const ns = resolveTaskNamespace(activity, taskId);
  if (!ns) {
    // No prior op: create the right namespace based on the wire task_type
    // (if present) and default to claude-task for ordinary background work.
    const isAgent = (line as { task_type?: string }).task_type === 'local_agent';
    return upsertOperation(activity, taskOpFromEvent(line, ts, update));
  }
  return updateOperation(activity, ns, taskId, update);
}

function applyTaskUpdated(activity: ActivityState, line: HarnessLine, ts: string): ActivityState {
  const taskId = String((line as { task_id?: string }).task_id ?? '');
  if (!taskId) return activity;
  const patch = (
    line as { patch?: { status?: string; end_time?: number; is_backgrounded?: boolean } }
  ).patch;
  if (!patch) return activity;
  const lifecycle = patch.status ? lifecycleForTaskStatus(patch.status) : undefined;
  const update = {
    rawStatus: patch.status ?? null,
    endTime: patch.end_time ?? null,
    lastUpdateAt: ts,
    ...(lifecycle
      ? { lifecycle, terminalAt: ['finished', 'failed', 'stopped'].includes(lifecycle) ? ts : null }
      : {}),
    ...(patch.is_backgrounded === true ? { lifecycle: 'running-background' as const } : {}),
  };
  const ns = resolveTaskNamespace(activity, taskId);
  if (!ns) return upsertOperation(activity, taskOpFromEvent(line, ts, update));
  return updateOperation(activity, ns, taskId, update);
}

function applyTaskNotification(
  activity: ActivityState,
  line: HarnessLine,
  ts: string
): ActivityState {
  const taskId = String((line as { task_id?: string }).task_id ?? '');
  if (!taskId) return activity;
  const status = (line as { status?: string }).status;
  const lifecycle = lifecycleForTaskStatus(status);
  const summary = (line as { summary?: string }).summary ?? null;
  const usage = (
    line as { usage?: { tool_uses?: number; duration_ms?: number; total_tokens?: number } }
  ).usage;
  const patch = {
    lifecycle,
    rawStatus: status ?? null,
    latestActivity: summary,
    lastUpdateAt: ts,
    terminalAt: ts,
    usage: usage
      ? {
          toolUses: usage.tool_uses,
          durationMs: usage.duration_ms,
          totalTokens: usage.total_tokens,
        }
      : null,
  };
  // Update the single namespace the task already lives in. If no prior op
  // exists, fall back to creating a claude-task (we cannot infer the
  // agent case from a notification alone).
  const ns = resolveTaskNamespace(activity, taskId);
  return upsertClaudeTask(
    activity,
    taskOpFromEvent(line, ts, {
      ...(ns ? activity.operations[`${ns}:${taskId}`] : {}),
      ...patch,
      toolId:
        typeof line.tool_use_id === 'string'
          ? line.tool_use_id
          : ns
            ? activity.operations[`${ns}:${taskId}`].toolId
            : null,
    })
  );
}

function applyBackgroundTasksChanged(
  activity: ActivityState,
  line: HarnessLine,
  ts: string
): ActivityState {
  const tasks =
    (line as { tasks?: { task_id: string; task_type?: string; description?: string }[] }).tasks ??
    [];
  const seen = new Set<string>();
  let next: ActivityState = activity;
  for (const t of tasks) {
    seen.add(t.task_id);
    const op: Operation = taskOpFromEvent(
      {
        type: 'system',
        task_id: t.task_id,
        task_type: t.task_type,
        description: t.description,
        is_backgrounded: true,
      } as unknown as HarnessLine,
      ts,
      {
        lifecycle: 'running-background',
        rawStatus: 'background',
        outputFile: null,
      }
    );
    if (!next.operations[`${op.namespace}:${op.id}`]) next = upsertClaudeTask(next, op);
  }
  // Membership: any task not in the snapshot loses background membership.
  // Lifecycle is preserved; only the background flag is cleared so the view
  // can decide whether to render or hide it. Snapshot removal alone does not
  // prove success.
  for (const key of Object.keys(next.operations)) {
    const op = next.operations[key];
    if (op.namespace !== 'claude-task') continue;
    if (seen.has(op.id)) continue;
    if (op.lifecycle === 'running-background') {
      next = updateOperation(next, 'claude-task', op.id, { lifecycle: 'status-unavailable' });
    }
  }
  return next;
}

function applyHookStarted(activity: ActivityState, line: HarnessLine, ts: string): ActivityState {
  const hookId = String((line as { hook_id?: string }).hook_id ?? '');
  if (!hookId) return activity;
  const name = String((line as { hook_name?: string }).hook_name ?? 'hook');
  const op: Operation = {
    namespace: 'claude-hook',
    id: hookId,
    kind: 'claude-hook',
    title: `Running ${name.replace(/:.+$/, '')}`,
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

function applyHookResponse(activity: ActivityState, line: HarnessLine, ts: string): ActivityState {
  const hookId = String((line as { hook_id?: string }).hook_id ?? '');
  if (!hookId) return activity;
  const outcome = (line as { outcome?: string }).outcome;
  const exitCode = (line as { exit_code?: number }).exit_code;
  const lifecycle: OperationLifecycle =
    outcome === 'success' || outcome === undefined
      ? 'finished'
      : outcome === 'failed' || (exitCode !== undefined && exitCode !== 0)
        ? 'failed'
        : 'status-unavailable';
  return updateOperation(activity, 'claude-hook', hookId, {
    lifecycle,
    rawStatus: outcome ?? null,
    lastUpdateAt: ts,
    terminalAt: ts,
  });
}

// applyToolResultEnvelope reads the structured `tool_use_result` field on a
// harness user record. The existing transcript rendering still uses the
// content text. The activity layer uses the envelope for TaskCreate /
// TaskUpdate / Agent and to attach a launch tool's async result to a task.
function applyToolResultEnvelope(
  c: Conversation,
  line: HarnessLine,
  recordTs: string
): Conversation {
  const result = (line as { tool_use_result?: unknown }).tool_use_result;
  if (!result) return c;
  // TaskCreate result: { task: { id, subject, ... } } or { success, taskId, ... } for TaskUpdate.
  const r = result as Record<string, unknown>;
  const ts = recordTs;
  // Look up the originating tool_use from the message content.
  const content =
    (
      line as {
        message?: {
          content?: {
            type?: string;
            tool_use_id?: string;
            name?: string;
            input?: Record<string, unknown>;
          }[];
        };
      }
    ).message?.content ?? [];
  let toolName = '';
  let toolInput: Record<string, unknown> = {};
  let toolUseId = '';
  for (const c of content) {
    if (c?.type === 'tool_result' && typeof c.tool_use_id === 'string') {
      toolUseId = c.tool_use_id;
      // tool name and input are not in tool_result, so we rely on the
      // matching assistant record. The harness does not duplicate them here,
      // so the reducer must keep track by tool_use_id.
    }
  }
  if (r.task && typeof r.task === 'object') {
    const taskInfo = r.task as { id?: string; subject?: string; status?: string };
    if (taskInfo.id) {
      const entry = {
        id: String(taskInfo.id),
        subject: String(taskInfo.subject ?? ''),
        status: 'pending' as const,
        lastChangeAt: ts,
      };
      return {
        ...c,
        activity: upsertChecklistEntry(c.activity, entry),
      };
    }
  }
  if (typeof r.success === 'boolean' && typeof r.taskId === 'string') {
    const statusChange = (r.statusChange as { to?: string } | undefined)?.to;
    let status: 'pending' | 'in-progress' | 'completed' | 'deleted' | 'unknown' = 'unknown';
    if (statusChange === 'completed') status = 'completed';
    else if (statusChange === 'in_progress') status = 'in-progress';
    else if (statusChange === 'pending') status = 'pending';
    else if (statusChange === 'deleted') status = 'deleted';
    if (r.success === false) status = 'unknown';
    return {
      ...c,
      activity: upsertChecklistEntry(c.activity, {
        id: String(r.taskId),
        subject: '',
        status,
        lastChangeAt: ts,
      }),
    };
  }
  // Async Agent launch result: { isAsync, status, agentId, description, ... }
  if (r.isAsync === true && typeof r.agentId === 'string') {
    const agentId = String(r.agentId);
    const op: Operation = {
      namespace: 'claude-agent',
      id: agentId,
      kind: 'claude-agent',
      title: String(r.description ?? 'Subagent'),
      ownerTurnId: null,
      parentId: toolUseId || null,
      lifecycle: r.status === 'async_launched' ? 'running-background' : 'running',
      rawStatus: typeof r.status === 'string' ? r.status : null,
      firstObservedAt: ts,
      startTime: null,
      endTime: null,
      lastUpdateAt: ts,
      durationMs: null,
      latestActivity: null,
      usage: null,
      toolId: toolUseId || null,
      assignmentId: 0,
      outputFile: typeof r.outputFile === 'string' ? r.outputFile : null,
      terminalAt: null,
    };
    return { ...c, activity: upsertClaudeTask(c.activity, op) };
  }
  return c;
}

// A heartbeat may precede its durable launch record. Retain the observed
// tool ID now; the selector only offers navigation once that tool is rendered.
function applyToolProgress(activity: ActivityState, line: HarnessLine, ts: string): ActivityState {
  // Heartbeats have their own event IDs (launch-heartbeat-N). Their parent
  // identifies the command; ordinary child-tool progress still uses its own ID.
  const id =
    line.heartbeat === true && line.parent_tool_use_id
      ? line.parent_tool_use_id
      : (line.tool_use_id ?? line.parent_tool_use_id);
  if (typeof id !== 'string' || !id) return activity;
  const elapsedMs =
    typeof line.elapsed_time_seconds === 'number'
      ? line.elapsed_time_seconds * 1000
      : ((line as { elapsed_time_ms?: number }).elapsed_time_ms ?? null);
  // Try to find an existing op for this id across both claude-tool and
  // claude-task namespaces. We do not need a deep search; the upstream
  // reducer that produced the transcript op uses the same id, and the
  // reducer code is what we trust.
  const existing = operationForTool(activity, id);
  if (existing) {
    return updateOperation(activity, existing.namespace, existing.id, {
      lastUpdateAt: ts,
      durationMs: elapsedMs ?? existing.durationMs,
    });
  }
  // Retain early progress by launch ID; the panel waits for a descriptive
  // launch before showing it as a long-running foreground action.
  const op: Operation = {
    namespace: 'claude-tool',
    id: String(id),
    kind: 'claude-tool',
    title: typeof line.tool_name === 'string' ? line.tool_name : 'Tool running…',
    ownerTurnId: null,
    parentId: null,
    lifecycle: 'running',
    rawStatus: null,
    firstObservedAt: ts,
    startTime: null,
    endTime: null,
    lastUpdateAt: ts,
    durationMs: elapsedMs,
    latestActivity: null,
    usage: null,
    toolId: String(id),
    assignmentId: 0,
    outputFile: null,
    terminalAt: null,
  };
  return upsertOperation(activity, op);
}

/**
 * A launch tool, task_started and an async result can describe the same worker
 * in either order. Keep one canonical task/agent key and its observed tool link.
 */
function upsertClaudeTask(activity: ActivityState, incoming: Operation): ActivityState {
  const key = `${incoming.namespace}:${incoming.id}`;
  const aliases = activity.order.filter((k) => {
    const op = activity.operations[k];
    return (
      k === key ||
      (incoming.toolId !== null &&
        op.toolId === incoming.toolId &&
        ['claude-tool', 'claude-task', 'claude-agent'].includes(op.kind))
    );
  });
  const prior = activity.operations[key] ?? activity.operations[aliases[0]];
  const first = activity.operations[aliases[0]];
  const terminal = prior?.terminalAt && !incoming.terminalAt ? prior : null;
  const merged: Operation = {
    ...incoming,
    firstObservedAt: first?.firstObservedAt ?? incoming.firstObservedAt,
    toolId: incoming.toolId ?? prior?.toolId ?? null,
    title: incoming.title === 'Task' ? (prior?.title ?? incoming.title) : incoming.title,
    usage: incoming.usage ?? prior?.usage ?? null,
    outputFile: incoming.outputFile ?? prior?.outputFile ?? null,
    ...(terminal
      ? {
          lifecycle: terminal.lifecycle,
          rawStatus: terminal.rawStatus,
          lastUpdateAt: terminal.lastUpdateAt,
          terminalAt: terminal.terminalAt,
          endTime: terminal.endTime,
          latestActivity: terminal.latestActivity,
        }
      : {}),
    ...(prior?.lifecycle === 'pending-input' && !incoming.terminalAt
      ? { lifecycle: 'pending-input' as const }
      : {}),
  };
  const operations = { ...activity.operations };
  const toolIndex = { ...activity.toolIndex };
  for (const alias of aliases) {
    delete operations[alias];
    const aliasOp = activity.operations[alias];
    if (aliasOp?.toolId && toolIndex[aliasOp.toolId] === alias) delete toolIndex[aliasOp.toolId];
  }
  operations[key] = merged;
  if (merged.toolId !== null) toolIndex[merged.toolId] = key;
  const order = [...new Set(activity.order.map((k) => (aliases.includes(k) ? key : k)))];
  if (!order.includes(key)) order.push(key);
  return { ...activity, operations, toolIndex, order };
}

/** Ordinary tool activity comes from the same observed segments as the transcript. */
function syncClaudeTools(c: Conversation, ts: string): Conversation {
  const turn = [...c.items].reverse().find((i) => i.kind === 'assistant');
  if (!turn || turn.kind !== 'assistant') return c;
  let activity = c.activity;
  for (const tool of turn.segments) {
    if (tool.kind !== 'tool') continue;
    const existing = operationForTool(activity, tool.id);
    // Task/agent IDs have their own lifecycle; completing a launch is not
    // completing the independent worker.
    if (existing && (existing.kind === 'claude-task' || existing.id !== tool.id)) continue;
    if (existing?.lifecycle === 'pending-input') continue;
    const lifecycle: OperationLifecycle =
      tool.state === 'done'
        ? 'finished'
        : tool.state === 'error'
          ? tool.result === 'Status unavailable'
            ? 'status-unavailable'
            : tool.result === 'interrupted'
              ? 'stopped'
              : 'failed'
          : tool.state;
    const input = tool.input as Record<string, unknown> | null;
    const detail = input?.description ?? input?.command ?? input?.file_path ?? input?.pattern;
    const title = typeof detail === 'string' ? `${tool.name}: ${detail}` : tool.name;
    if (existing?.lifecycle === lifecycle && existing.title === title) continue;
    const namespace = tool.name === 'Agent' ? 'claude-agent' : 'claude-tool';
    activity = upsertOperation(activity, {
      namespace,
      id: tool.id,
      kind: namespace,
      title,
      toolId: tool.id,
      ownerTurnId: null,
      parentId: null,
      lifecycle,
      rawStatus: null,
      firstObservedAt: existing?.firstObservedAt ?? ts,
      lastUpdateAt: ts,
      startTime: null,
      endTime: null,
      durationMs: existing?.durationMs ?? null,
      latestActivity: tool.state === 'done' || tool.state === 'error' ? tool.result : null,
      usage: null,
      outputFile: null,
      assignmentId: 0,
      terminalAt: ['finished', 'failed', 'stopped', 'status-unavailable'].includes(lifecycle)
        ? ts
        : null,
    });
  }
  return activity === c.activity ? c : { ...c, activity };
}
