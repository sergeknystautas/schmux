import { Link } from 'react-router';
import { reviewReading, roleLabel, type FlightActivity } from '../../lib/flights';
import { formatRelativeTime, formatNudgeSummary, nudgeStateEmoji } from '../../lib/utils';
import type { FlightSummaryResponse } from '../../lib/types.generated';
import layout from './FlightLayout.module.css';

type FlightActivityLineProps = {
  flight: Pick<
    FlightSummaryResponse,
    'id' | 'workflow_state' | 'cleanup_state' | 'pilot_stop' | 'review_status'
  >;
  activity: FlightActivity | null;
  /**
   * The list shows the reviewer's latest reading in the status line. The
   * detail page shows it in its own row, so it turns this one off there.
   */
  showReviewReading?: boolean;
};

/**
 * The state words the session tabs use (`SessionTabs.tsx`), with "busy" taken
 * from the activity's in-progress flag so it matches the list order.
 */
function sessionStateText(
  activity: FlightActivity,
  s: NonNullable<FlightActivity['session']>
): string {
  if (!s.running) return 'Stopped';
  if (s.signed_out) return '🪪 Signed out';
  if (activity.inProgress) return 'Working';
  const state = s.nudge_state || '';
  if (!state || state === 'Idle') return 'Idle';
  const emoji = nudgeStateEmoji[state];
  const summary = formatNudgeSummary(s.nudge_summary);
  if (emoji) return `${emoji} ${summary ?? state}`;
  return summary ?? state;
}

/** Workflow and observed activity share one line, with cleanup taking precedence. */
export default function FlightActivityLine({
  flight,
  activity,
  showReviewReading = true,
}: FlightActivityLineProps) {
  const stage = flight.workflow_state[0].toUpperCase() + flight.workflow_state.slice(1);
  const session = activity?.session;
  const role = activity ? roleLabel(activity.role) : '';
  const state = session && activity ? sessionStateText(activity, session) : '';
  const lastOutput = session?.last_output_at;
  const description = session
    ? [role, state, lastOutput && formatRelativeTime(lastOutput)].filter(Boolean).join(' · ')
    : activity
      ? `${role} session gone`
      : 'No session';
  const reading = showReviewReading ? reviewReading(flight.review_status) : null;
  // A pilot stop leads the line until the phase changes: the daemon exposes
  // the record only while the flight is still in the phase it was made in.
  const text =
    flight.cleanup_state === 'disposing'
      ? 'Disposing'
      : flight.pilot_stop
        ? `Pilot stopped: ${flight.pilot_stop.reason}`
        : reading
          ? reading
          : `${stage} · ${description}`;

  if (!session || flight.cleanup_state === 'disposing') {
    return (
      <span
        data-testid={`flight-activity-${flight.id}`}
        className={`${layout.activityLine} text-muted`}
        title={text}
      >
        {text}
      </span>
    );
  }
  return (
    <span data-testid={`flight-activity-${flight.id}`} className={layout.activityLine} title={text}>
      <Link to={`/sessions/${encodeURIComponent(session.id)}`} className="text-accent">
        {state === 'Working' && <span className="spinner spinner--small" aria-hidden="true" />}
        {text}
      </Link>
    </span>
  );
}
