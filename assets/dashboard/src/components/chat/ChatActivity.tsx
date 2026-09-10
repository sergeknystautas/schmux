import { useEffect, useMemo, useRef, useState } from 'react';
import styles from './chat.module.css';
import Tooltip from '../Tooltip';
import type { Conversation } from '../../lib/chat/types';
import {
  selectActivity,
  type ActivityView,
  type ConnectionState,
  type RowStatus,
} from '../../lib/chat/activity-selector';
import type { ChatSocketStatus } from '../../lib/chat/socket';

interface ChatActivityProps {
  conversation: Conversation;
  status: ChatSocketStatus;
  /** True when history has been applied; activity is only authoritative after this. */
  historyLoaded: boolean;
  /** True when the session's process is gone. */
  ended: boolean;
  onInterrupt?: () => void;
  /** Called when the user clicks a Jump link on an activity row. */
  onJumpToTool?: (toolId: string) => void;
}

const TICK_MS = 1000;
const SLOW_HOOK_THRESHOLD_MS = 1000;

function toConnectionState(
  status: ChatSocketStatus,
  ended: boolean,
  priorConnectedRef: { current: boolean }
): ConnectionState {
  if (ended) return { kind: 'gone' };
  if (status === 'connected') {
    priorConnectedRef.current = true;
    return { kind: 'connected' };
  }
  if (status === 'connecting') {
    return priorConnectedRef.current
      ? { kind: 'reconnecting', priorConnected: true }
      : { kind: 'connecting' };
  }
  if (status === 'disconnected' || status === 'gone') {
    return { kind: 'reconnecting', priorConnected: priorConnectedRef.current };
  }
  return { kind: 'connecting' };
}

function statusClassName(status: RowStatus): string {
  switch (status) {
    case 'running':
    case 'running-background':
    case 'preparing':
    case 'pending-input':
      return styles.activityRowStatusRunning;
    case 'finished':
      return styles.activityRowStatusFinished;
    case 'failed':
      return styles.activityRowStatusFailed;
    case 'stopped':
      return styles.activityRowStatusStopped;
    case 'status-unavailable':
      return styles.activityRowStatusUnavailable;
    default:
      return '';
  }
}

function formatDurationMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ${sec % 60}s`;
  const hours = Math.floor(min / 60);
  return `${hours}h ${min % 60}m`;
}

function statusLabel(status: RowStatus): string {
  switch (status) {
    case 'preparing':
      return 'Preparing';
    case 'running':
      return 'Running';
    case 'running-background':
      return 'Running in background';
    case 'pending-input':
      return 'Needs input';
    case 'finished':
      return 'Finished';
    case 'failed':
      return 'Failed';
    case 'stopped':
      return 'Stopped';
    case 'status-unavailable':
      return 'Status unavailable';
    default:
      return status;
  }
}

function planStatusLabel(
  status: 'pending' | 'in-progress' | 'completed' | 'deleted' | 'unknown'
): string {
  switch (status) {
    case 'in-progress':
      return 'In progress';
    case 'completed':
      return 'Complete';
    case 'pending':
      return 'Pending';
    case 'deleted':
      return 'Deleted';
    case 'unknown':
      return 'Unknown';
  }
}

export default function ChatActivity({
  conversation,
  status,
  historyLoaded,
  ended,
  onInterrupt,
  onJumpToTool,
}: ChatActivityProps) {
  const priorConnectedRef = useRef(false);
  const [now, setNow] = useState<number>(() => Date.now());
  const [expanded, setExpanded] = useState(false);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
  const toggleRow = (key: string) => {
    setExpandedRows((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  // Track prior connected state. When status moves to disconnected, the
  // socket hook will reset historyLoaded; we keep priorConnected true so
  // the headline can show "Reconnecting · activity may be out of date".
  useEffect(() => {
    if (status === 'connected') priorConnectedRef.current = true;
  }, [status]);

  // Single shared timer; at most one render per second for any visible clock.
  // The selector itself is pure; the timer only re-runs the view.
  useEffect(() => {
    if (status !== 'connected' || !historyLoaded || ended) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), TICK_MS);
    return () => clearInterval(id);
  }, [status, historyLoaded, ended]);

  const conn = useMemo(() => toConnectionState(status, ended, priorConnectedRef), [status, ended]);

  const view: ActivityView = useMemo(
    () =>
      selectActivity(conversation, conn, {
        now,
        slowHookThresholdMs: SLOW_HOOK_THRESHOLD_MS,
      }),
    [conversation, conn, now]
  );

  if (view.hidden) return null;
  // Initial load (no prior connection, no history): nothing authoritative yet.
  // Reconnects keep the previously observed rows with a "stale" explanation
  // so the user sees what was happening before the socket dropped.
  const isInitialLoad = !historyLoaded && !ended && !priorConnectedRef.current;
  if (isInitialLoad) return null;

  // Default: the first 3 rows. Expanded: every filtered row, in the order
  // the selector returned them. The selector returns the full set so this
  // slice is what the view should render.
  const visibleRows = expanded ? view.rows : view.rows.slice(0, 3);

  return (
    <section className={styles.activity} aria-label="Activity" data-testid="chat-activity">
      <div className={styles.activityHeader}>
        <div
          className={styles.activityHeadline}
          aria-live="polite"
          data-testid="chat-activity-headline"
        >
          {view.headline ?? ''}
          {view.headlineDetail ? (
            <span className={styles.activityHeadlineDetail}>{view.headlineDetail}</span>
          ) : null}
        </div>
        {conversation.phase === 'running' && status === 'connected' && onInterrupt ? (
          <Tooltip content="Interrupt the current turn (Escape)">
            <button
              type="button"
              className="btn btn--sm btn--danger"
              onClick={onInterrupt}
              data-testid="chat-stop"
            >
              Stop
            </button>
          </Tooltip>
        ) : null}
      </div>
      <div className={styles.activityBody} data-testid="chat-activity-body">
        {visibleRows.length > 0 ? (
          <ul className={styles.activityRows} data-testid="chat-activity-rows">
            {visibleRows.map((row) => {
              const isRowExpanded = expandedRows.has(row.key);
              return (
                <li
                  key={row.key}
                  className={styles.activityRow}
                  data-status={row.status}
                  data-testid="chat-activity-row"
                  data-activity-key={row.key}
                >
                  <span className={styles.activityRowTitle}>{row.title}</span>
                  <span className={`${styles.activityRowStatus} ${statusClassName(row.status)}`}>
                    {statusLabel(row.status)}
                  </span>
                  {row.ageLabel ? (
                    <span className={styles.activityRowAge}>Updated {row.ageLabel}</span>
                  ) : null}
                  {row.durationMs !== null ? (
                    <span className={styles.activityRowAge}>
                      Last reported runtime: {formatDurationMs(row.durationMs)}
                    </span>
                  ) : null}
                  {row.expandable ? (
                    <button
                      type="button"
                      className="btn btn--ghost btn--sm"
                      onClick={() => toggleRow(row.key)}
                      data-testid="chat-activity-row-expand"
                      aria-expanded={isRowExpanded}
                      aria-controls={`chat-activity-row-details-${row.key}`}
                    >
                      {isRowExpanded ? 'Hide details' : 'Details'}
                    </button>
                  ) : null}
                  {onJumpToTool && row.toolId ? (
                    <button
                      type="button"
                      className="btn btn--ghost btn--sm"
                      data-testid="chat-activity-row-jump"
                      onClick={() => onJumpToTool(row.toolId!)}
                    >
                      Jump to transcript
                    </button>
                  ) : null}
                  {row.command ? (
                    <code className={styles.activityRowActivity}>{row.command}</code>
                  ) : null}
                  {row.latestActivity ? (
                    <span className={styles.activityRowActivity}>
                      Last activity: {row.latestActivity}
                    </span>
                  ) : null}
                  {isRowExpanded && row.expandable ? (
                    <div
                      className={styles.activityRowDetails}
                      id={`chat-activity-row-details-${row.key}`}
                      data-testid="chat-activity-row-details"
                    >
                      {row.latestActivity ? <div>Latest: {row.latestActivity}</div> : null}
                      {row.usage?.toolUses !== undefined ? (
                        <div>Tool calls: {row.usage.toolUses}</div>
                      ) : null}
                      {row.usage?.totalTokens !== undefined ? (
                        <div>Tokens: {row.usage.totalTokens}</div>
                      ) : null}
                      {row.usage?.durationMs !== undefined ? (
                        <div>Reported duration: {formatDurationMs(row.usage.durationMs)}</div>
                      ) : null}
                      {row.outputFile ? <div>Output file: {row.outputFile}</div> : null}
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ul>
        ) : null}
        {view.showAll ? (
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={() => setExpanded((e) => !e)}
            data-testid="chat-activity-show-all"
            aria-expanded={expanded}
          >
            {expanded ? 'Show less' : `Show all (${view.rows.length})`}
          </button>
        ) : null}
        {view.plan && view.planSummary ? (
          <details className={styles.activityPlan} data-testid="chat-activity-plan">
            <summary className={styles.activityPlanTitle}>{view.planSummary}</summary>
            <ul className={styles.activityPlanList}>
              {view.plan.map((step) => (
                <li key={step.id} className={styles.activityPlanItem}>
                  <span className={styles.activityPlanItemSubject}>{step.subject}</span>
                  <span className={styles.activityPlanItemStatus}>
                    {planStatusLabel(step.status)}
                  </span>
                </li>
              ))}
            </ul>
          </details>
        ) : null}
      </div>
    </section>
  );
}
