import { useEffect, useRef, useState } from 'react';
import useFocusTrap from '../../hooks/useFocusTrap';
import ModalFrame from '../../components/ModalFrame';
import type { WorkspaceResponse } from '../../lib/types';
import layout from './FlightLayout.module.css';

type FlightDisposeModalProps = {
  flightLabel: string;
  /** The session IDs dispose will close. */
  sessionIds: string[];
  /** The workspace dispose will destroy, or '' when none carries the flight's id. */
  workspaceId: string;
  /** The workspace with its git state, when the websocket list carries it. */
  workspace?: WorkspaceResponse;
  onConfirm: () => Promise<void>;
  onClose: () => void;
};

/**
 * Confirms a destructive dispose. It names exactly what dispose will do: the
 * sessions it will close, the workspace it will destroy (if any), and the
 * flight record it will remove. Dispose bypasses the usual uncommitted-change
 * guards and cannot be undone.
 *
 * When the workspace's git state is known, the modal also shows the size of
 * the uncommitted diff and the branch's position against its remote, and it
 * disables Dispose while the daemon would refuse: unpushed commits.
 */
export default function FlightDisposeModal({
  flightLabel,
  sessionIds,
  workspaceId,
  workspace,
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

  // The git numbers only exist for a git workspace the websocket list carries.
  const git = workspace && (!workspace.vcs || workspace.vcs === 'git') ? gitState(workspace) : null;

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
            disabled={submitting || !!git?.blocked}
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
            {git && (
              <ul className={layout.resourceList}>
                <li data-testid="flight-dispose-diff">{git.diff}</li>
                <li data-testid="flight-dispose-position">{git.position}</li>
                {git.blocked && (
                  <li data-testid="flight-dispose-blocked" className="text-error">
                    {git.blocked}
                  </li>
                )}
              </ul>
            )}
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

/**
 * The workspace's git state for the dispose modal: the size of the
 * uncommitted diff, the branch's position against its remote branch, and the
 * block the daemon's own rule applies — commits ahead of the default branch
 * whose local branch does not match its remote.
 */
function gitState(workspace: WorkspaceResponse) {
  const branch = workspace.default_branch || 'main';
  const hasDiff =
    workspace.files_changed > 0 || workspace.lines_added > 0 || workspace.lines_removed > 0;
  const diff = hasDiff ? (
    <>
      discard uncommitted changes in {workspace.files_changed} file(s):{' '}
      <span className="text-success">+{workspace.lines_added}</span>{' '}
      <span className="text-error">−{workspace.lines_removed}</span> lines
    </>
  ) : (
    <>no uncommitted file changes</>
  );
  const position = workspace.remote_branch_exists
    ? `${workspace.local_unique_commits ?? 0} commit(s) ahead of and ${
        workspace.remote_unique_commits ?? 0
      } behind the remote branch`
    : `${workspace.ahead} commit(s) ahead of ${branch}; the branch is on no remote`;
  const blocked =
    workspace.ahead > 0 && !workspace.commits_synced_with_remote
      ? workspace.remote_branch_exists
        ? `Dispose is blocked: ${workspace.ahead} commit(s) not in ${branch}, and the local branch does not match its remote. Push or merge first.`
        : `Dispose is blocked: ${workspace.ahead} commit(s) not in ${branch}, and the branch is on no remote. Push or merge first.`
      : null;
  return { diff, position, blocked };
}
