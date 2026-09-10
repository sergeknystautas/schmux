// Pure selector from Conversation + connection state to the activity area's
// view model. Implements the spec's precedence table (headline, rows, plan).
// The view is the only consumer; reducers never call this.
//
// `now` is an explicit parameter so the tests can drive clocks without
// timers; the component passes a real Date.now() in production.
import type { ActivityState, Operation, OperationKind, OperationLifecycle } from './activity';
import type { AssistantTurn, Conversation, ToolSegment } from './types';

export type ConnectionState =
  | { kind: 'connecting' }
  | { kind: 'connected' }
  | { kind: 'reconnecting'; priorConnected: boolean }
  | { kind: 'gone' };

export type RowStatus =
  | 'preparing'
  | 'running'
  | 'running-background'
  | 'pending-input'
  | 'finished'
  | 'failed'
  | 'stopped'
  | 'status-unavailable';

interface ActivityRow {
  key: string;
  title: string;
  command: string | null;
  status: RowStatus;
  rawStatus: string | null;
  kind: OperationKind;
  // "Last activity: …" or latest non-empty command output line.
  latestActivity: string | null;
  // Last update age label (e.g. "20s ago"). null when the row is fresh or
  // there is no timestamp to compare against.
  ageLabel: string | null;
  // Reported duration in ms when supplied; the view formats it.
  durationMs: number | null;
  usage: Operation['usage'];
  outputFile: string | null;
  // True when this row represents a still-executing operation.
  active: boolean;
  // Explicit launch-tool link, validated against rendered transcript segments.
  toolId: string | null;
  // True when an "expand" affordance is needed.
  expandable: boolean;
}

interface PlanRow {
  id: string;
  subject: string;
  status: 'pending' | 'in-progress' | 'completed' | 'deleted' | 'unknown';
}

export interface ActivityView {
  hidden: boolean;
  headline: string | null;
  // Subtext for the headline (e.g. attempt 2 of 5).
  headlineDetail: string | null;
  // All visible operations; the component initially shows three.
  rows: ActivityRow[];
  // Total count when the rows array is truncated.
  moreCount: number;
  plan: PlanRow[] | null;
  planSummary: string | null;
  // True when the user can expand "Show all".
  showAll: boolean;
  // Pending question / permission count, surfaced in the headline.
  pendingCount: number;
}

const ACTIVE_LIFECYCLES: OperationLifecycle[] = [
  'preparing',
  'running',
  'running-background',
  'pending-input',
];

// Routine foreground tools stay in the transcript unless they run for 10s.
const LONG_ACTION_THRESHOLD_MS = 10_000;

function operationLabel(
  op: Operation,
  tool?: ToolSegment
): { title: string; command: string | null } {
  const input = tool?.input as Record<string, unknown> | undefined;
  const description = typeof input?.description === 'string' ? input.description : null;
  const command = typeof input?.command === 'string' ? input.command : null;
  const task = ['claude-task', 'claude-agent', 'codex-agent'].includes(op.kind);
  const title = description || (task ? op.title : command || op.title);
  return { title, command: command && command !== title ? command : null };
}

function ageLabelFor(op: Operation, now: number): string | null {
  const lastMs = Date.parse(op.lastUpdateAt);
  if (!Number.isFinite(lastMs)) return null;
  const ageSec = Math.max(0, Math.floor((now - lastMs) / 1000));
  if (ageSec < 5) return null;
  if (ageSec < 60) return `${ageSec}s ago`;
  const minutes = Math.floor(ageSec / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}

function formatDuration(ms: number | null): string | null {
  if (ms == null) return null;
  if (ms < 1000) return `${ms}ms`;
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ${sec % 60}s`;
  const hours = Math.floor(min / 60);
  return `${hours}h ${min % 60}m`;
}

function planSummary(plan: PlanRow[]): string | null {
  if (plan.length === 0) return null;
  return `Plan · ${plan.length} step${plan.length === 1 ? '' : 's'} in progress`;
}

export interface SelectorOptions {
  now: number;
  slowHookThresholdMs?: number;
}

export function selectActivity(
  c: Conversation,
  conn: ConnectionState,
  opts: SelectorOptions
): ActivityView {
  const slowHookThresholdMs = opts.slowHookThresholdMs ?? 1000;
  const { activity } = c;
  const openTurn = lastOpenTurn(c.items);
  const phase = c.phase;
  const isGone = conn.kind === 'gone';
  const priorLifetime = !activity.live && !isGone;

  // 1. Filter operations by recency and visibility.
  const transcriptTools = new Map<string, ToolSegment>(
    c.items.flatMap((item) =>
      item.kind === 'assistant'
        ? item.segments.flatMap((s) => (s.kind === 'tool' ? [[s.id, s] as const] : []))
        : []
    )
  );
  const rows: ActivityRow[] = [];
  for (const key of activity.order) {
    const op = activity.operations[key];
    if (!op || priorLifetime || !ACTIVE_LIFECYCLES.includes(op.lifecycle)) continue;
    if (op.kind === 'codex-control') continue;
    const tool = op.toolId ? transcriptTools.get(op.toolId) : undefined;
    const label = operationLabel(op, tool);
    const elapsed = opts.now - (op.startTime ?? Date.parse(op.firstObservedAt));
    if (op.kind === 'claude-hook' || op.kind === 'codex-hook') {
      if (!(elapsed >= slowHookThresholdMs)) continue;
    }
    const foregroundAction =
      ['claude-tool', 'codex-tool', 'claude-task'].includes(op.kind) &&
      op.lifecycle !== 'running-background' &&
      op.lifecycle !== 'pending-input';
    if (foregroundAction) {
      // Unlinked progress cannot tell us what is executing or where it ends.
      // Preserve it in the model for a later launch, without advertising a worker.
      if (!tool || !(elapsed >= LONG_ACTION_THRESHOLD_MS)) continue;
      if (label.title === tool.name) continue;
    }
    rows.push({
      key,
      ...label,
      status: op.lifecycle,
      rawStatus: op.rawStatus,
      kind: op.kind,
      latestActivity: op.latestActivity,
      ageLabel: ageLabelFor(op, opts.now),
      durationMs: op.durationMs,
      usage: op.usage,
      outputFile: op.outputFile,
      active: true,
      toolId: op.toolId && transcriptTools.has(op.toolId) ? op.toolId : null,
      expandable: !!op.latestActivity || op.usage !== null || !!op.outputFile,
    });
  }

  // 2. Plan rows.
  const plan: PlanRow[] = (!activity.live ? [] : activity.checklistOrder)
    .map((id) => activity.checklist[id])
    .filter((e): e is NonNullable<typeof e> => e !== undefined && e.status === 'in-progress')
    .map((e) => ({ id: e.id, subject: e.subject, status: e.status }));
  const planSum = planSummary(plan);

  // 3. Headline.
  const headlineInfo = computeHeadline({
    rows,
    plan: plan.length > 0 ? plan : null,
    openTurn,
    phase,
    conn,
    activity,
    isGone,
  });

  // 4. The selector returns the full filtered row set; the view decides
  // whether to render only the first 3 (default) or all of them when the
  // user expands the "Show all" affordance. Exposing the full set is
  // required: the prior selector returned only the first three, so the
  // expand action could not surface rows 4+.
  const moreCount = Math.max(0, rows.length - 3);

  // 5. Hidden: no rows, no plan, no pending, no headline reason to render.
  const pendingCount = countPendingInput(activity, openTurn);
  const hidden =
    rows.length === 0 && plan.length === 0 && pendingCount === 0 && headlineInfo.headline === null;

  return {
    hidden,
    headline: headlineInfo.headline,
    headlineDetail: headlineInfo.detail,
    rows,
    moreCount,
    plan: plan.length > 0 ? plan : null,
    planSummary: planSum,
    showAll: moreCount > 0,
    pendingCount,
  };
}

interface HeadlineInputs {
  rows: ActivityRow[];
  plan: PlanRow[] | null;
  openTurn: AssistantTurn | null;
  phase: 'idle' | 'running';
  conn: ConnectionState;
  activity: ActivityState;
  isGone: boolean;
}

interface HeadlineResult {
  headline: string | null;
  detail: string | null;
}

function computeHeadline(i: HeadlineInputs): HeadlineResult {
  // Highest-priority rules first. When two rules match, the higher one wins.
  if (i.isGone) {
    return { headline: 'Session ended', detail: null };
  }
  if (i.conn.kind === 'connecting' || i.conn.kind === 'reconnecting') {
    const prior = i.conn.kind === 'reconnecting' ? i.conn.priorConnected : false;
    return {
      headline: prior ? 'Reconnecting · activity may be out of date' : 'Connecting…',
      detail: null,
    };
  }
  const pending = countPendingInput(i.activity, i.openTurn);
  if (pending > 0) {
    // Surface a single-word headline; the panel still renders the card.
    if (i.openTurn?.segments.some((s) => s.kind === 'pending' && s.questions !== null)) {
      return {
        headline: pending === 1 ? 'Needs your answer' : `Needs your answer (${pending})`,
        detail: null,
      };
    }
    return {
      headline: pending === 1 ? 'Needs approval' : `Needs approval (${pending})`,
      detail: null,
    };
  }
  if (i.openTurn?.interrupted) {
    return { headline: 'Stopping current turn…', detail: null };
  }
  // Retry/compaction specific headlines.
  const retryOp = i.rows.find((r) => r.status === 'running' && /retry/i.test(r.title));
  if (retryOp) {
    return { headline: 'Retrying request', detail: retryOp.title };
  }
  const compaction = i.rows.find((r) => r.active && r.kind === 'codex-compaction');
  if (compaction) {
    return { headline: 'Preparing conversation context…', detail: null };
  }
  // Background-only headline: when every active row is background and no
  // foreground turn is open, prefer the dedicated background count. The
  // active-executable path below covers foreground work.
  const background = i.rows.filter((r) => r.status === 'running-background' && r.active);
  if (background.length > 0 && !i.openTurn) {
    return {
      headline: `${background.length} background task${background.length === 1 ? '' : 's'} active`,
      detail: null,
    };
  }
  // Active executable work. Coordination calls (Codex control) and mcp
  // startup probes do not count as workers; the spec excludes them from
  // the headline tally and the worker list.
  const isWorker = (r: { kind: string; active: boolean }) =>
    r.active && !['codex-control', 'codex-hook', 'claude-hook'].includes(r.kind);
  const active = i.rows.filter(isWorker);
  if (active.length > 0) {
    if (active.length === 1) {
      return { headline: active[0].title, detail: null };
    }
    // Count agents and tools by operation kind, not by title prefix.
    const agents = active.filter(
      (r) => r.kind === 'claude-agent' || r.kind === 'codex-agent'
    ).length;
    const commands = active.length - agents;
    const parts: string[] = [];
    if (commands > 0) parts.push(`${commands} task${commands === 1 ? '' : 's'}`);
    if (agents > 0) parts.push(`${agents} agent${agents === 1 ? '' : 's'}`);
    return {
      headline: parts.length > 0 ? parts.join(' · ') : `${active.length} operations active`,
      detail: null,
    };
  }
  // Reasoning/responding/transcript phase hints.
  if (i.openTurn?.thinking || i.activity.phase === 'thinking') {
    return { headline: 'Thinking…', detail: null };
  }
  if (i.phase === 'running' && i.openTurn) {
    const hasProse = i.openTurn.segments.some(
      (s) => s.kind === 'prose' && (s as { streaming?: boolean }).streaming
    );
    if (hasProse) {
      return { headline: 'Responding…', detail: null };
    }
  }
  // Slow hooks.
  const slowHook = i.rows.find(
    (r) => r.status === 'running' && /Running .*hook|Connecting/i.test(r.title)
  );
  if (slowHook) {
    return { headline: slowHook.title, detail: null };
  }
  if (i.phase === 'running') {
    return {
      headline: i.activity.phase === 'waiting' ? 'Waiting for response…' : 'Working…',
      detail: null,
    };
  }
  // Background only (fallback when no foreground turn was open).
  const backgroundOnly = i.rows.filter((r) => r.status === 'running-background');
  if (backgroundOnly.length > 0) {
    return {
      headline: `${backgroundOnly.length} background task${backgroundOnly.length === 1 ? '' : 's'} active`,
      detail: null,
    };
  }
  if (i.plan && i.plan.length > 0) {
    return { headline: planSummary(i.plan), detail: null };
  }
  return { headline: null, detail: null };
}

function lastOpenTurn(items: Conversation['items']): AssistantTurn | null {
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.kind === 'assistant' && it.end === null) return it;
  }
  return null;
}

function countPendingInput(activity: ActivityState, openTurn: AssistantTurn | null): number {
  let count = 0;
  for (const seg of openTurn?.segments ?? []) {
    if (seg.kind === 'pending') count++;
  }
  count += activity.pendingInput.length;
  return count;
}

// Summary helper exposed for tests.
export function _formatDurationForTest(ms: number | null): string | null {
  return formatDuration(ms);
}
