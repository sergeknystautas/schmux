import { useEffect, useRef, useState } from 'react';
import useFocusTrap from '../../hooks/useFocusTrap';
import ModalFrame from '../../components/ModalFrame';
import layout from './FlightLayout.module.css';

type FlightTerminateModalProps = {
  flightLabel: string;
  workspaceId: string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
};

/**
 * Confirms a destructive terminate. It names the workspace that will be
 * destroyed and every consequence, because terminate bypasses the usual
 * uncommitted-change guards and cannot be undone.
 */
export default function FlightTerminateModal({
  flightLabel,
  workspaceId,
  onConfirm,
  onClose,
}: FlightTerminateModalProps) {
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
      title={`Terminate ${flightLabel}?`}
      titleId="flight-terminate-title"
      testId="flight-terminate-modal"
      footer={
        <>
          <button className="btn btn--secondary" onClick={onClose} disabled={submitting}>
            Keep flight
          </button>
          <button
            className="btn btn--danger"
            onClick={handleConfirm}
            disabled={submitting}
            data-testid="flight-terminate-confirm"
          >
            {submitting ? (
              <>
                <span className="spinner spinner--small" /> Terminating
              </>
            ) : (
              'Terminate'
            )}
          </button>
        </>
      }
    >
      <p>
        This permanently destroys workspace <span className="mono">{workspaceId}</span>:
      </p>
      <ul className={layout.resourceList} data-testid="flight-terminate-consequences">
        <li>every session in the workspace, not only this flight's</li>
        <li>every uncommitted file change</li>
        <li>the planning artifact</li>
        <li>all retained chat history for the workspace</li>
        <li>the workspace directory and its local flight branch</li>
      </ul>
      <p className="text-muted">Remote branches are not touched. This cannot be undone.</p>
    </ModalFrame>
  );
}
