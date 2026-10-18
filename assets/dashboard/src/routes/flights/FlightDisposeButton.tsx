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
    'id' | 'short_name' | 'workflow_state' | 'cleanup_state' | 'workspace'
  >;
  /** Small buttons for list rows; regular buttons for page headers. */
  size: 'sm' | 'md';
  /** Receives every successful dispose response. */
  onDisposed?: (res: FlightTransitionResponse) => void;
};

/**
 * The Dispose control for one flight. Owns both dialogs (the consequence
 * modal for a flight with a workspace, the standard `confirm()` for one
 * without), the API call, the failure alert, and the toast. The flight
 * decides which dialog to open by what it owns; this component keeps the
 * wording in one place.
 */
export default function FlightDisposeButton({
  flight,
  size,
  onDisposed,
}: FlightDisposeButtonProps) {
  const { alert, confirm } = useModal();
  const { success: toastSuccess } = useToast();
  const [dialog, setDialog] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const label = flightLabel(flight);
  const workspaceId = flight.workspace?.workspace_id ?? '';
  const hasWorkspace = !!flight.workspace;

  const submit = async (body: { workspace_id: string; confirm: true }, done: string) => {
    setSubmitting(true);
    try {
      const res = await disposeFlight(flight.id, body);
      toastSuccess(done);
      onDisposed?.(res);
    } catch (err) {
      alert('Dispose failed', getErrorMessage(err, 'Request failed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleClick = async () => {
    if (!hasWorkspace) {
      const ok = await confirm(`Remove ${label}? Nothing has been created for it yet.`, {
        danger: true,
        confirmText: 'Remove',
      });
      if (!ok) return;
      await submit({ workspace_id: '', confirm: true }, `${label} disposed`);
      return;
    }
    setDialog(true);
  };

  const handleConfirm = async () => {
    setDialog(false);
    await submit({ workspace_id: workspaceId, confirm: true }, `${label} disposed`);
  };

  const sizeClass = size === 'sm' ? ' btn--sm' : '';
  return (
    <>
      <button
        type="button"
        className={`btn btn--ghost btn--danger${sizeClass}`}
        onClick={handleClick}
        disabled={submitting}
        data-testid={`flight-action-dispose-${flight.id}`}
      >
        {submitting && <span className="spinner spinner--small" />}
        {flightActionLabel('dispose', flight)}
      </button>
      {dialog && (
        <FlightDisposeModal
          flightLabel={label}
          workspaceId={workspaceId}
          onConfirm={handleConfirm}
          onClose={() => setDialog(false)}
        />
      )}
    </>
  );
}
