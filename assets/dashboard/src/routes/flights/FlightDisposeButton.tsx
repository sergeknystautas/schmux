import { useState } from 'react';
import { disposeFlight, getErrorMessage } from '../../lib/api';
import { useModal } from '../../components/ModalProvider';
import { useToast } from '../../components/ToastProvider';
import { flightActionLabel, flightLabel } from '../../lib/flights';
import type { FlightSummaryResponse, FlightTransitionResponse } from '../../lib/types.generated';
import FlightDisposeModal from './FlightDisposeModal';

type FlightDisposeButtonProps = {
  flight: Pick<
    FlightSummaryResponse,
    'id' | 'short_name' | 'workflow_state' | 'cleanup_state' | 'sessions' | 'owned_workspace_id'
  >;
  /** Small buttons for list rows; regular buttons for page headers. */
  size: 'sm' | 'md';
  /** Receives every successful dispose response. */
  onDisposed?: (res: FlightTransitionResponse) => void;
};

/**
 * The Dispose control for one flight. It owns the consequence modal, the API
 * call, the failure alert, and the toast. The modal always opens: what
 * dispose will do depends on what the flight owns, and the modal says so.
 */
export default function FlightDisposeButton({
  flight,
  size,
  onDisposed,
}: FlightDisposeButtonProps) {
  const { alert } = useModal();
  const { success: toastSuccess } = useToast();
  const [dialog, setDialog] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const label = flightLabel(flight);
  const workspaceId = flight.owned_workspace_id ?? '';
  const sessionIds = flightSessionIds(flight.sessions);

  const submit = async (done: string) => {
    setSubmitting(true);
    try {
      const res = await disposeFlight(flight.id, { confirm: true });
      toastSuccess(done);
      onDisposed?.(res);
    } catch (err) {
      alert('Dispose failed', getErrorMessage(err, 'Request failed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleConfirm = async () => {
    setDialog(false);
    await submit(`${label} disposed`);
  };

  const sizeClass = size === 'sm' ? ' btn--sm' : '';
  return (
    <>
      <button
        type="button"
        className={`btn btn--ghost btn--danger${sizeClass}`}
        onClick={() => setDialog(true)}
        disabled={submitting}
        data-testid={`flight-action-dispose-${flight.id}`}
      >
        {submitting && <span className="spinner spinner--small" />}
        {flightActionLabel('dispose', flight)}
      </button>
      {dialog && (
        <FlightDisposeModal
          flightLabel={label}
          sessionIds={sessionIds}
          workspaceId={workspaceId}
          onConfirm={handleConfirm}
          onClose={() => setDialog(false)}
        />
      )}
    </>
  );
}

/**
 * The session IDs dispose will close: every current session ID followed by
 * every prior session ID, deduplicated and without empty entries.
 */
function flightSessionIds(sessions: FlightSummaryResponse['sessions']): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const s of sessions ?? []) {
    if (s.session_id) {
      if (!seen.has(s.session_id)) {
        seen.add(s.session_id);
        out.push(s.session_id);
      }
    }
    for (const prior of s.prior_session_ids ?? []) {
      if (prior && !seen.has(prior)) {
        seen.add(prior);
        out.push(prior);
      }
    }
  }
  return out;
}
