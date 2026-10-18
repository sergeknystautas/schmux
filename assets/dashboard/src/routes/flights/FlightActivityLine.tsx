import { Link } from 'react-router';
import { roleLabel, type FlightActivity } from '../../lib/flights';
import { formatRelativeTime, formatNudgeSummary, nudgeStateEmoji } from '../../lib/utils';
import type { FlightSummaryResponse } from '../../lib/types.generated';
import layout from './FlightLayout.module.css';

type FlightActivityLineProps = {
  flight: Pick<FlightSummaryResponse, 'id' | 'workflow_state' | 'cleanup_state'>;
  activity: FlightActivity | null;
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
export default function FlightActivityLine({ flight, activity }: FlightActivityLineProps) {
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
  const text = flight.cleanup_state === 'disposing' ? 'Disposing' : `${stage} · ${description}`;

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
