// Session-level activity and checklist state, independent of any specific
// assistant turn. Activity operations are keyed by a (namespace, id) pair:
// tool IDs, task IDs, hook IDs, agent IDs, control-call IDs, and checklist
// IDs are all separate namespaces. The reducer upserts them on each relevant
// event; the ChatActivity component renders a filtered view.

// Raw status as observed on the wire or in the model. Kept for inspection.
type OperationRawStatus = string;

// Explicit lifecycle state derived from the protocol's explicit transitions.
// Distinct from rawStatus so a model can be "completed" while the rawStatus is
// some unfamiliar string. The view shows "Status unavailable" when the
// transition is unknown.
export type OperationLifecycle =
  | 'preparing'
  | 'running'
  | 'running-background'
  | 'pending-input'
  | 'finished'
  | 'failed'
  | 'stopped'
  | 'status-unavailable';

export type OperationKind =
  | 'claude-task' // Claude system:task_* events
  | 'claude-agent' // Claude async Agent result merged with task events
  | 'claude-tool' // Claude tool_use with extended duration (heartbeats)
  | 'claude-hook' // Claude system:hook_*
  | 'claude-retry' // Claude system:api_retry
  | 'codex-tool' // Codex commandExecution / fileChange / mcpToolCall
  | 'codex-compaction' // Codex contextCompaction
  | 'codex-hook' // Codex hook/started / hook/completed
  | 'codex-agent' // Codex collabAgentToolCall target
  | 'codex-control' // Codex collaboration control call
  | 'codex-retry'; // Codex error willRetry

export interface Operation {
  // Stable identity: (namespace, id) makes the type unique within the session.
  namespace: string;
  id: string;
  kind: OperationKind;
  title: string;
  // Owner (turn id) and optional parent (for child tools/agent children).
  ownerTurnId: string | null;
  parentId: string | null;
  lifecycle: OperationLifecycle;
  rawStatus: OperationRawStatus | null;
  // First observed timestamp (record ts from the daemon).
  firstObservedAt: string;
  // Optional reported start/end times.
  startTime: number | null;
  endTime: number | null;
  // Last update time, used for the "last reported Ns ago" label.
  lastUpdateAt: string;
  // Optional reported duration.
  durationMs: number | null;
  // Latest reported activity, for the secondary line.
  latestActivity: string | null;
  // Optional usage summary.
  usage: { toolUses?: number; durationMs?: number; totalTokens?: number } | null;
  // Observed launch-tool ID; distinct from the harness task/agent identity.
  toolId: string | null;
  // Identifies a follow-up assignment on the same identity (Codex agents).
  assignmentId: number;
  // Optional output file path, not loaded here.
  outputFile: string | null;
  // Timestamp of the observed terminal outcome, retained for transcript history.
  terminalAt: string | null;
}

export interface ChecklistEntry {
  id: string;
  subject: string;
  activeForm?: string;
  status: 'pending' | 'in-progress' | 'completed' | 'deleted' | 'unknown';
  // Last lifecycle change source: which record index produced the change.
  lastChangeAt: string;
}

interface PendingInput {
  requestId: string;
  toolUseId: string;
  toolName: string;
  // True when schmux has no card and the request can only be declined.
  abortOnly: boolean;
  // First observed time, used to render age labels.
  firstObservedAt: string;
  // True when the user has supplied a saved answer awaiting submit.
  hasSavedAnswer: boolean;
}

interface AttentionOutcome {
  id: string;
  // The transcript segment or operation this is attached to.
  source: 'transcript' | 'operation';
  sourceId: string;
  status: 'failed' | 'unavailable' | 'stopped';
  // Short text shown in the row.
  text: string;
  // When the outcome was observed; dismissal gates on next user message.
  observedAt: string;
}

export interface ActivityState {
  // The app-server connection also carries child-thread notifications.
  codexThreadId?: string;
  // Operations keyed by composite identity "namespace:id".
  operations: Record<string, Operation>;
  // Reverse index of toolId → operations key, maintained by the upserters so
  // operationForTool is O(1). A stale entry is tolerated: the lookup
  // re-checks the operation's own toolId before returning.
  toolIndex: Record<string, string>;
  // Insertion order for first-observed display, plus assignment ordering.
  order: string[];
  // Checklist entries keyed by their task id.
  checklist: Record<string, ChecklistEntry>;
  checklistOrder: string[];
  // Pending input requests, in first-observed order.
  pendingInput: PendingInput[];
  // Attention outcomes pending dismissal on next accepted user message.
  attentionOutcomes: AttentionOutcome[];
  // True while the session is still alive. Reset by session: ended.
  live: boolean;
  // Transient foreground signals and acknowledgement boundary, from records.
  phase?: 'waiting' | 'thinking' | null;
  acknowledgedAt?: string;
}

export function emptyActivity(): ActivityState {
  return {
    operations: {},
    toolIndex: {},
    order: [],
    checklist: {},
    checklistOrder: [],
    pendingInput: [],
    attentionOutcomes: [],
    live: true,
  };
}

function opKey(namespace: string, id: string): string {
  return `${namespace}:${id}`;
}

// retoolIndex repoints the toolId index after an operation's tool link
// moved. The previous entry is only dropped when it still names this
// operation, so a toolId shared with another operation is left alone.
function retoolIndex(
  toolIndex: Record<string, string>,
  key: string,
  prevToolId: string | null,
  nextToolId: string | null
): Record<string, string> {
  if (prevToolId === nextToolId) return toolIndex;
  const next = { ...toolIndex };
  if (prevToolId !== null && next[prevToolId] === key) delete next[prevToolId];
  if (nextToolId !== null) next[nextToolId] = key;
  return next;
}

// upsertOperation inserts a new operation or merges an existing one. The
// caller picks a stable namespace; reducer code uses 'claude-task',
// 'claude-tool', 'codex-tool', 'codex-agent', etc. When an op already exists
// for the (namespace, id) pair, the caller's op supplies the new field
// values and the existing firstObservedAt/assignmentId/title are preserved.
export function upsertOperation(
  state: ActivityState,
  op: Operation,
  fields?: Partial<Operation>
): ActivityState {
  const key = opKey(op.namespace, op.id);
  const existing = state.operations[key];
  if (!existing) {
    const next: Operation = { ...op, ...(fields ?? {}) };
    return {
      ...state,
      operations: { ...state.operations, [key]: next },
      ...(next.toolId !== null ? { toolIndex: { ...state.toolIndex, [next.toolId]: key } } : {}),
      order: [...state.order, key],
    };
  }
  const merged: Operation = {
    ...existing,
    ...op,
    ...(fields ?? {}),
    // Identity and observation timestamps are sticky on update.
    firstObservedAt: existing.firstObservedAt,
    namespace: existing.namespace,
    id: existing.id,
    assignmentId: op.assignmentId || existing.assignmentId,
  };
  return {
    ...state,
    operations: { ...state.operations, [key]: merged },
    toolIndex: retoolIndex(state.toolIndex, key, existing.toolId, merged.toolId),
  };
}

// updateOperation is the variant when only a partial patch is available.
export function updateOperation(
  state: ActivityState,
  namespace: string,
  id: string,
  patch: Partial<Operation>
): ActivityState {
  const key = opKey(namespace, id);
  const existing = state.operations[key];
  if (!existing) return state;
  const merged: Operation = { ...existing, ...patch };
  return {
    ...state,
    operations: { ...state.operations, [key]: merged },
    toolIndex: retoolIndex(state.toolIndex, key, existing.toolId, merged.toolId),
  };
}

export function upsertChecklistEntry(state: ActivityState, entry: ChecklistEntry): ActivityState {
  const existing = state.checklist[entry.id];
  if (!existing) {
    return {
      ...state,
      checklist: { ...state.checklist, [entry.id]: entry },
      checklistOrder: [...state.checklistOrder, entry.id],
    };
  }
  // Merge: keep the existing subject if the new entry does not supply one.
  // An update may not echo the original subject.
  const merged: ChecklistEntry = {
    ...existing,
    ...entry,
    subject: entry.subject || existing.subject,
  };
  return { ...state, checklist: { ...state.checklist, [entry.id]: merged } };
}

// clearRetries drops every retry-flavored operation. Called on user
// message, turn result, and other "the model is back to work" events so
// the panel does not keep showing "Retrying request" after recovery.
export function clearRetries(state: ActivityState): ActivityState {
  let operations = state.operations;
  let order = state.order;
  let toolIndex = state.toolIndex;
  let dirty = false;
  for (const key of state.order) {
    const op = state.operations[key];
    if (op.kind === 'claude-retry' || op.kind === 'codex-retry') {
      if (!dirty) {
        operations = { ...state.operations };
        order = [...state.order];
        toolIndex = { ...state.toolIndex };
        dirty = true;
      }
      delete operations[key];
      if (op.toolId !== null && toolIndex[op.toolId] === key) delete toolIndex[op.toolId];
      const i = order.indexOf(key);
      if (i >= 0) order.splice(i, 1);
    }
  }
  if (!dirty) return state;
  return { ...state, operations, toolIndex, order };
}

/** Resolve the same explicit link for transcript rendering and memoization. */
export function operationForTool(
  activity: Pick<ActivityState, 'operations' | 'toolIndex'> | undefined,
  toolId: string
): Operation | undefined {
  if (!activity) return undefined;
  const key = activity.toolIndex?.[toolId];
  if (key === undefined) return undefined;
  const op = activity.operations[key];
  return op?.toolId === toolId ? op : undefined;
}
