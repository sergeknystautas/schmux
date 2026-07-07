import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getRestartOptions, restartSession, getErrorMessage } from '../lib/api';
import type { RestartOptionsResponse } from '../lib/types.generated';
import { useSessions } from '../contexts/SessionsContext';
import useFocusTrap from '../hooks/useFocusTrap';

interface RestartSessionModalProps {
  sessionId: string;
  onClose: () => void;
}

export default function RestartSessionModal({ sessionId, onClose }: RestartSessionModalProps) {
  const navigate = useNavigate();
  const { waitForSession } = useSessions();
  const modalRef = useRef<HTMLDivElement>(null);
  const [options, setOptions] = useState<RestartOptionsResponse | null>(null);
  const [target, setTarget] = useState('');
  const [fence, setFence] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useFocusTrap(modalRef, true);

  useEffect(() => {
    let active = true;
    getRestartOptions(sessionId)
      .then((opts) => {
        if (!active) return;
        setOptions(opts);
        setTarget(opts.current_target);
        setFence(opts.fence);
      })
      .catch((err) => {
        if (active) setError(getErrorMessage(err, 'Failed to load restart options'));
      });
    return () => {
      active = false;
    };
  }, [sessionId]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleRestart = async () => {
    setSubmitting(true);
    setError('');
    try {
      const result = await restartSession(sessionId, { target, fence });
      if (result.session_id) {
        await waitForSession(result.session_id);
        navigate(`/sessions/${result.session_id}`);
      }
      onClose();
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to restart session'));
      setSubmitting(false);
    }
  };

  return (
    <div
      className="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="restart-modal-title"
    >
      <div
        ref={modalRef}
        className="modal"
        data-testid="restart-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal__header">
          <h2 className="modal__title" id="restart-modal-title">
            Restart session
          </h2>
        </div>
        <div className="modal__body">
          <div className="form-group">
            <label className="form-group__label" htmlFor="restart-target">
              Target
            </label>
            <select
              id="restart-target"
              className="select"
              data-testid="restart-modal-target"
              value={target}
              disabled={!options || submitting}
              onChange={(e) => setTarget(e.target.value)}
            >
              {(options?.targets ?? []).map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
          {options?.fence_available && (
            <div className="checkbox-list">
              <label className="checkbox-list__item">
                <input
                  type="checkbox"
                  data-testid="restart-modal-fence"
                  checked={fence}
                  disabled={submitting}
                  onChange={(e) => setFence(e.target.checked)}
                />
                <span>Fence (sandbox + skip approvals)</span>
              </label>
            </div>
          )}
          {error && <p className="text-error mt-sm">{error}</p>}
        </div>
        <div className="modal__footer">
          <button
            className="btn"
            onClick={onClose}
            disabled={submitting}
            data-testid="restart-modal-cancel"
          >
            Cancel
          </button>
          <button
            className="btn btn--primary"
            onClick={handleRestart}
            disabled={!options || submitting}
            data-testid="restart-modal-submit"
          >
            Restart
          </button>
        </div>
      </div>
    </div>
  );
}
