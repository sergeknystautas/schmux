import { useState } from 'react';
import {
  enterFlightCompletion,
  enterFlightExecution,
  enterFlightReview,
  getErrorMessage,
  takeoffFlight,
} from '../../lib/api';
import { useModal } from '../../components/ModalProvider';
import { useToast } from '../../components/ToastProvider';
import {
  flightActionHint,
  flightActionLabel,
  flightActions,
  flightLabel,
  type FlightAction,
} from '../../lib/flights';
import type { FlightSummaryResponse, FlightTransitionResponse } from '../../lib/types.generated';
import FlightDisposeButton from './FlightDisposeButton';
import layout from './FlightLayout.module.css';

type FlightActionsProps = {
  flight: Pick<
    FlightSummaryResponse,
    'id' | 'short_name' | 'workflow_state' | 'cleanup_state' | 'sessions' | 'workspace'
  >;
  /** Receives every successful transition and dispose response. */
  onTransition?: (res: FlightTransitionResponse) => void;
};

type TransitionAction = Exclude<FlightAction, 'dispose'>;

/**
 * The detail page header controls for one flight. Each transition calls the
 * flight API; the backend decides whether it is allowed, and the updated
 * flight arrives through the dashboard snapshot. A failed attempt is shown
 * and stays retryable through the same control.
 *
 * The pilot advances the ordinary phases itself, so those actions are
 * overrides; the completion action is the human review-and-commit checkpoint.
 * Each action carries that explanation.
 *
 * Dispose renders through `FlightDisposeButton`, which the list rows also use.
 */
export default function FlightActions({ flight, onTransition }: FlightActionsProps) {
  const { alert } = useModal();
  const { success: toastSuccess } = useToast();
  const [running, setRunning] = useState<TransitionAction | null>(null);
  const label = flightLabel(flight);
  const actions = flightActions(flight);
  const transitions = actions.filter((a): a is TransitionAction => a !== 'dispose');
  const hint = transitions.length > 0 ? flightActionHint(transitions[0]) : null;

  const run = async (
    action: TransitionAction,
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

  const handle = async (action: TransitionAction) => {
    switch (action) {
      case 'takeoff':
        await run(action, () => takeoffFlight(flight.id), `${label} took off`);
        return;
      case 'execution':
        await run(action, () => enterFlightExecution(flight.id), `${label} is executing`);
        return;
      case 'review':
        await run(action, () => enterFlightReview(flight.id), `${label} review agent started`);
        return;
      case 'completion':
        await run(action, () => enterFlightCompletion(flight.id), `${label} commit agent started`);
        return;
    }
  };

  return (
    <div className={layout.actionGroup}>
      <div className={layout.actionButtons}>
        {transitions.map((action) => (
          <button
            key={action}
            type="button"
            className="btn btn--primary"
            disabled={running !== null}
            onClick={() => handle(action)}
            data-testid={`flight-action-${action}-${flight.id}`}
          >
            {running === action && <span className="spinner spinner--small" />}
            {flightActionLabel(action, flight)}
          </button>
        ))}
        {actions.includes('dispose') && (
          <FlightDisposeButton flight={flight} size="md" onDisposed={onTransition} />
        )}
      </div>
      {hint && <p className={`${layout.actionHint} text-muted`}>{hint}</p>}
    </div>
  );
}
