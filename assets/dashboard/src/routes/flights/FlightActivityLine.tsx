import { Link } from 'react-router';
import { reviewSummary, roleLabel, type FlightActivity } from '../../lib/flights';
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
   * Where the line goes when clicked. The list links to the flight, which is
   * what the user wants to see next; the detail page, already on the flight,
   * links to the session the line names.
   */
  linkTo?: 'flight' | 'session';
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

/**
 * One line that says how the flight is going. Cleanup and a pilot stop take
 * precedence; a session that is working or waiting on the user is named with
 * its state; otherwise the reviewer's latest reading, in a few words, stands
 * in for an idle reviewer; otherwise the stage, the role, its state, and how
 * long ago it last did anything.
 */
export default function FlightActivityLine({
  flight,
  activity,
  linkTo = 'flight',
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
  const live = state === 'Working' || session?.nudge_state === 'Needs Input';
  const reading = live ? null : reviewSummary(flight.review_status);
  // A pilot stop leads the line until the phase changes: the daemon exposes
  // the record only while the flight is still in the phase it was made in.
  const text =
    flight.cleanup_state === 'disposing'
      ? 'Disposing'
      : flight.pilot_stop
        ? `Pilot stopped: ${flight.pilot_stop.reason}`
        : reading
          ? `${stage} · ${reading}`
          : `${stage} · ${description}`;

  const href =
    flight.cleanup_state === 'disposing'
      ? null
      : linkTo === 'session'
        ? session
          ? `/sessions/${encodeURIComponent(session.id)}`
          : null
        : `/flights/${encodeURIComponent(flight.id)}`;
  if (!href) {
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
      <Link to={href} className="text-accent">
        {state === 'Working' && <span className="spinner spinner--small" aria-hidden="true" />}
        {text}
      </Link>
    </span>
  );
}
