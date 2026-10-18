import { useState } from 'react';
import {
  cancelFlight,
  enterFlightCompletion,
  enterFlightExecution,
  getErrorMessage,
  markFlightDone,
  takeoffFlight,
  terminateFlight,
} from '../../lib/api';
import { useModal } from '../../components/ModalProvider';
import { useToast } from '../../components/ToastProvider';
import {
  flightActionLabel,
  flightActions,
  flightLabel,
  type FlightAction,
} from '../../lib/flights';
import type { FlightSummaryResponse, FlightTransitionResponse } from '../../lib/types.generated';
import FlightTerminateModal from './FlightTerminateModal';

type FlightActionsProps = {
  flight: Pick<
    FlightSummaryResponse,
    'id' | 'short_name' | 'workflow_state' | 'cleanup_state' | 'sessions' | 'workspace'
  >;
  /** Small buttons for list rows; regular buttons for page headers. */
  compact?: boolean;
  /** Receives every successful transition response. */
  onTransition?: (res: FlightTransitionResponse) => void;
};

/**
 * The workflow controls for one flight. Each control calls the flight API;
 * the backend decides whether the transition is allowed, and the updated
 * flight arrives through the dashboard snapshot. A failed attempt is shown
 * and stays retryable through the same control.
 */
export default function FlightActions({ flight, compact, onTransition }: FlightActionsProps) {
  const { alert, prompt } = useModal();
  const { success: toastSuccess } = useToast();
  const [running, setRunning] = useState<FlightAction | null>(null);
  const [dialog, setDialog] = useState<'terminate' | null>(null);
  const label = flightLabel(flight);
  const workspaceId = flight.workspace?.workspace_id ?? '';

  const run = async (
    action: FlightAction,
    call: () => Promise<FlightTransitionResponse>,
    done: string
  ): Promise<boolean> => {
    setRunning(action);
    try {
      const res = await call();
      onTransition?.(res);
      toastSuccess(done);
      return true;
    } catch (err) {
      alert(`${flightActionLabel(action, flight)} failed`, getErrorMessage(err, 'Request failed'));
      return false;
    } finally {
      setRunning(null);
    }
  };

  const handle = async (action: FlightAction) => {
    switch (action) {
      case 'takeoff':
        await run(action, () => takeoffFlight(flight.id), `${label} took off`);
        return;
      case 'execution':
        await run(action, () => enterFlightExecution(flight.id), `${label} is executing`);
        return;
      case 'completion':
        await run(action, () => enterFlightCompletion(flight.id), `${label} commit agent started`);
        return;
      case 'terminate':
        setDialog(action);
        return;
      case 'done':
        await run(action, () => markFlightDone(flight.id), `${label} marked done`);
        return;
      case 'cancel': {
        const reason = await prompt(`Cancel ${label}`, {
          placeholder: 'Why is this flight being cancelled?',
          confirmText: 'Cancel flight',
        });
        if (!reason || !reason.trim()) return;
        await run(action, () => cancelFlight(flight.id, reason.trim()), `${label} cancelled`);
        return;
      }
    }
  };

  const confirmTerminate = async () => {
    // Close first: a failure leaves the flight terminating with a retry
    // control, and the alert explains what is left.
    setDialog(null);
    await run(
      'terminate',
      () => terminateFlight(flight.id, { workspace_id: workspaceId, confirm: true }),
      `${label} terminated`
    );
  };

  const size = compact ? ' btn--sm' : '';
  const variant = (action: FlightAction) => {
    if (action === 'terminate') return 'btn--ghost btn--danger';
    if (action === 'cancel') return 'btn--ghost';
    return 'btn--primary';
  };

  return (
    <>
      {flightActions(flight).map((action) => (
        <button
          key={action}
          type="button"
          className={`btn ${variant(action)}${size}`}
          disabled={running !== null}
          onClick={() => handle(action)}
          data-testid={`flight-action-${action}-${flight.id}`}
        >
          {running === action && <span className="spinner spinner--small" />}
          {flightActionLabel(action, flight)}
        </button>
      ))}
      {dialog === 'terminate' && (
        <FlightTerminateModal
          flightLabel={label}
          workspaceId={workspaceId}
          onConfirm={confirmTerminate}
          onClose={() => setDialog(null)}
        />
      )}
    </>
  );
}
