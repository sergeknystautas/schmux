import { useRef, useState, useEffect } from 'react';
import { pushToBranch, getErrorMessage } from '../lib/api';
import type { BranchDivergenceResponse, DivergenceCommit } from '../lib/types.generated';
import { useToast } from './ToastProvider';
import { formatRelativeTime } from '../lib/utils';
import useFocusTrap from '../hooks/useFocusTrap';
import styles from './ForcePushModal.module.css';

interface ForcePushModalProps {
  workspaceId: string;
  divergence: BranchDivergenceResponse;
  onClose: () => void;
}

function CommitList({ commits, total }: { commits: DivergenceCommit[]; total: number }) {
  return (
    <ul className={styles.list}>
      {commits.map((c) => (
        <li key={c.hash} className={styles.commit}>
          <span className={styles.hash}>{c.short_hash}</span>
          <span className={styles.author}>{c.author}</span>
          <span className={styles.time}>{formatRelativeTime(c.timestamp)}</span>
          <span className={styles.subject}>{c.subject}</span>
        </li>
      ))}
      {total > commits.length && <li className={styles.more}>and {total - commits.length} more</li>}
    </ul>
  );
}

export default function ForcePushModal({ workspaceId, divergence, onClose }: ForcePushModalProps) {
  const modalRef = useRef<HTMLDivElement>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const { success: toastSuccess } = useToast();

  useFocusTrap(modalRef, true);

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

  const handleForcePush = async () => {
    setSubmitting(true);
    setError('');
    try {
      const result = await pushToBranch(workspaceId, {
        confirm: true,
        expected_local: divergence.local_head,
        expected_remote: divergence.remote_head,
      });
      if (result.success) {
        toastSuccess(`Pushed to origin/${divergence.branch}`);
        onClose();
      } else {
        setError(
          result.message ||
            'Force push failed. Close and reopen the push options to see the latest state.'
        );
        setSubmitting(false);
      }
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to force push'));
      setSubmitting(false);
    }
  };

  return (
    <div
      className="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="force-push-modal-title"
    >
      <div
        ref={modalRef}
        className="modal modal--wide"
        data-testid="force-push-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal__header">
          <h2 className="modal__title" id="force-push-modal-title">
            Force push to origin/{divergence.branch}?
          </h2>
        </div>
        <div className="modal__body">
          <div className={styles.section}>
            <p className={styles.sectionTitle}>Only on origin — will be overwritten</p>
            <CommitList commits={divergence.remote_commits} total={divergence.remote_total} />
          </div>
          <div className={styles.section}>
            <p className={styles.sectionTitle}>Only local — will be pushed</p>
            <CommitList commits={divergence.local_commits} total={divergence.local_total} />
          </div>
          {error && (
            <p className="text-error mt-sm" data-testid="force-push-modal-error">
              {error}
            </p>
          )}
          <p className={styles.leaseNote}>
            Uses git push --force-with-lease — fails safely if origin moved since this list was
            fetched.
          </p>
        </div>
        <div className="modal__footer">
          <button
            className="btn"
            onClick={onClose}
            disabled={submitting}
            data-testid="force-push-modal-cancel"
          >
            Cancel
          </button>
          <button
            className="btn btn--danger"
            onClick={handleForcePush}
            disabled={submitting}
            data-testid="force-push-modal-submit"
          >
            {submitting ? (
              <>
                <span className="spinner" /> Force pushing
              </>
            ) : (
              'Force Push'
            )}
          </button>
        </div>
      </div>
    </div>
  );
}
