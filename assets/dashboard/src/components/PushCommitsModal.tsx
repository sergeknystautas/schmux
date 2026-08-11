import { useEffect, useRef, useState } from 'react';
import { useSync } from '../hooks/useSync';
import useFocusTrap from '../hooks/useFocusTrap';
import Tooltip from './Tooltip';

export interface PushCommitsModalProps {
  workspaceId: string;
  commitHash: string;
  commitShortHash: string;
  commitMessage: string;
  defaultBranch: string;
  branchName: string;
  /** workspace branch === default branch: only one possible target */
  onDefaultBranch: boolean;
  /** ws.behind > 0 — disables the main target */
  behind: boolean;
  /** origin/<default> has no common ancestor with HEAD — disables the main target */
  defaultBranchOrphaned: boolean;
  /** ws.files_changed > 0 — disables both targets */
  dirty: boolean;
  /** false when the remote branch lives on a fork — hides the branch target */
  branchTargetAvailable: boolean;
  /** commit already on origin/<branch> — disables the branch target */
  branchAlreadyPushed: boolean;
  /** exact commit count for the main target */
  countToMain: number;
  /** exact count for the branch target, or null when unknown (estimate) */
  countToBranch: number | null;
  /** the selected commit is the branch head — a full push to main offers workspace cleanup */
  headCommit: boolean;
  /** used to skip the cleanup suggestion for the live dev workspace */
  workspacePath?: string;
  onClose: () => void;
  /** refetch the graph after a push landed */
  onPushed: () => Promise<void>;
}

export default function PushCommitsModal(props: PushCommitsModalProps) {
  const {
    workspaceId,
    commitHash,
    commitShortHash,
    commitMessage,
    defaultBranch,
    branchName,
    onDefaultBranch,
    behind,
    defaultBranchOrphaned,
    dirty,
    branchTargetAvailable,
    branchAlreadyPushed,
    countToMain,
    countToBranch,
    headCommit,
    workspacePath,
    onClose,
    onPushed,
  } = props;
  const { handlePushCommits } = useSync();
  const modalRef = useRef<HTMLDivElement>(null);
  useFocusTrap(modalRef, true);

  const mainDisabled = dirty || behind || defaultBranchOrphaned;
  const branchDisabled = dirty || branchAlreadyPushed;
  const showBranchTarget = !onDefaultBranch && branchTargetAvailable;

  const [target, setTarget] = useState<'default' | 'branch'>(
    mainDisabled && showBranchTarget && !branchDisabled ? 'branch' : 'default'
  );
  const [perCommit, setPerCommit] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // The modal is inert while a push loop is running server-side: closing
      // it mid-flight would hide the result surface and read as a cancel.
      if (e.key === 'Escape' && !submitting) {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose, submitting]);

  const isEstimate = target === 'branch' && countToBranch === null;
  const n = target === 'default' ? countToMain : (countToBranch ?? countToMain);
  const targetBranchName = target === 'default' ? defaultBranch : branchName;
  // With a single commit both modes are the same action — offering them is a
  // false choice, so the Mode section is hidden and bulk is used.
  const showModeChoice = n > 1;
  const effectivePerCommit = perCommit && showModeChoice;
  const pushes = effectivePerCommit ? n : 1;

  let mainTooltip = `One CI build per push — choose the mode below.`;
  if (dirty) {
    mainTooltip = `You cannot push with local changes. Please commit or discard them before pushing.`;
  } else if (behind) {
    mainTooltip = `You cannot push to ${defaultBranch} until you have pulled the latest from ${defaultBranch}.`;
  } else if (defaultBranchOrphaned) {
    mainTooltip = `origin/${defaultBranch} has no common history with this branch — push is not possible.`;
  }
  let branchTooltip = `Push commits to origin/${branchName}`;
  if (dirty) {
    branchTooltip = `You cannot push with local changes. Please commit or discard them before pushing.`;
  } else if (branchAlreadyPushed) {
    branchTooltip = `This commit is already on origin/${branchName}.`;
  }

  const handlePush = async () => {
    setSubmitting(true);
    try {
      const pushed = await handlePushCommits(workspaceId, {
        hash: commitHash,
        target,
        perCommit: effectivePerCommit,
        targetBranchName,
        headCommit,
        workspacePath,
      });
      if (pushed) {
        await onPushed();
        onClose();
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="push-modal-title"
    >
      <div
        ref={modalRef}
        className="modal"
        data-testid="push-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal__header">
          <h2 className="modal__title" id="push-modal-title">
            Push up to {commitShortHash}
          </h2>
        </div>
        <div className="modal__body">
          <p className="text-muted">{commitMessage}</p>

          {showBranchTarget && (
            <div className="form-group">
              <span className="form-group__label">Push to</span>
              <div className="checkbox-list">
                <Tooltip content={mainTooltip}>
                  <label className="checkbox-list__item">
                    <input
                      type="radio"
                      name="push-target"
                      data-testid="push-modal-target-default"
                      checked={target === 'default'}
                      disabled={mainDisabled || submitting}
                      onChange={() => setTarget('default')}
                    />
                    <span>origin/{defaultBranch}</span>
                  </label>
                </Tooltip>
                <Tooltip content={branchTooltip}>
                  <label className="checkbox-list__item">
                    <input
                      type="radio"
                      name="push-target"
                      data-testid="push-modal-target-branch"
                      checked={target === 'branch'}
                      disabled={branchDisabled || submitting}
                      onChange={() => setTarget('branch')}
                    />
                    <span>origin/{branchName}</span>
                  </label>
                </Tooltip>
              </div>
            </div>
          )}
          {!showBranchTarget && (
            <Tooltip content={mainTooltip}>
              <p>
                Target: <strong>origin/{defaultBranch}</strong>
              </p>
            </Tooltip>
          )}

          {showModeChoice && (
            <div className="form-group">
              <span className="form-group__label">Mode</span>
              <div className="checkbox-list">
                <label className="checkbox-list__item">
                  <input
                    type="radio"
                    name="push-mode"
                    data-testid="push-modal-mode-bulk"
                    checked={!perCommit}
                    disabled={submitting}
                    onChange={() => setPerCommit(false)}
                  />
                  <span>All at once — 1 push, 1 build</span>
                </label>
                <label className="checkbox-list__item">
                  <input
                    type="radio"
                    name="push-mode"
                    data-testid="push-modal-mode-percommit"
                    checked={perCommit}
                    disabled={submitting}
                    onChange={() => setPerCommit(true)}
                  />
                  <span data-testid="push-modal-mode-percommit-label">
                    One push per commit — {isEstimate ? 'up to ' : ''}
                    {n} push{n === 1 ? '' : 'es'}, {n} build{n === 1 ? '' : 's'}
                  </span>
                </label>
              </div>
            </div>
          )}
        </div>
        <div className="modal__footer">
          <button
            className="btn"
            onClick={onClose}
            disabled={submitting}
            data-testid="push-modal-cancel"
          >
            Cancel
          </button>
          <button
            className="btn btn--primary"
            onClick={handlePush}
            disabled={
              submitting ||
              (target === 'default' && mainDisabled) ||
              (target === 'branch' && branchDisabled)
            }
            data-testid="push-modal-submit"
          >
            {submitting ? (
              <>
                <span className="spinner" /> Pushing
              </>
            ) : (
              <>
                Push {isEstimate ? 'up to ' : ''}
                {n} commit{n === 1 ? '' : 's'} ({pushes} push{pushes === 1 ? '' : 'es'})
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}
