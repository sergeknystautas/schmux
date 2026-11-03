import { useEffect, useRef, useState } from 'react';
import useFocusTrap from '../../hooks/useFocusTrap';
import ModalFrame from '../../components/ModalFrame';
import layout from './FlightLayout.module.css';

type FlightDisposeModalProps = {
  flightLabel: string;
  /** The session IDs dispose will close. */
  sessionIds: string[];
  /** The workspace dispose will destroy, or '' when none carries the flight's id. */
  workspaceId: string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
};

/**
 * Confirms a destructive dispose. It names exactly what dispose will do: the
 * sessions it will close, the workspace it will destroy (if any), and the
 * flight record it will remove. Dispose bypasses the usual uncommitted-change
 * guards and cannot be undone.
 */
export default function FlightDisposeModal({
  flightLabel,
  sessionIds,
  workspaceId,
  onConfirm,
  onClose,
}: FlightDisposeModalProps) {
  const modalRef = useRef<HTMLDivElement>(null);
  useFocusTrap(modalRef, true);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !submitting) {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose, submitting]);

  const handleConfirm = async () => {
    setSubmitting(true);
    try {
      await onConfirm();
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ModalFrame
      modalRef={modalRef}
      title={`Dispose ${flightLabel}?`}
      titleId="flight-dispose-title"
      testId="flight-dispose-modal"
      footer={
        <>
          <button className="btn btn--secondary" onClick={onClose} disabled={submitting}>
            Keep flight
          </button>
          <button
            className="btn btn--danger"
            onClick={handleConfirm}
            disabled={submitting}
            data-testid="flight-dispose-confirm"
          >
            {submitting ? (
              <>
                <span className="spinner spinner--small" /> Disposing
              </>
            ) : (
              'Dispose'
            )}
          </button>
        </>
      }
    >
      <p>Disposing {flightLabel} will:</p>
      <ul className={layout.resourceList} data-testid="flight-dispose-consequences">
        {sessionIds.length === 0 ? (
          <li>close no sessions (none were started)</li>
        ) : (
          sessionIds.map((id) => (
            <li key={id}>
              close session <span className="mono">{id}</span>
            </li>
          ))
        )}
        {workspaceId ? (
          <li>
            destroy workspace <span className="mono">{workspaceId}</span>: every session in it,
            every uncommitted file change, the planning artifact, and the workspace directory
          </li>
        ) : (
          <li>touch no workspace (none carries this flight's id)</li>
        )}
        <li>remove the flight record</li>
      </ul>
      <p className="text-muted">No branch is touched, local or remote. This cannot be undone.</p>
    </ModalFrame>
  );
}
