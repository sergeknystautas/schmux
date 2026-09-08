import React from 'react';
import type { WorkspaceResponse } from '../lib/types';
import { WorkingSpinner } from '../lib/utils';
import { ArrowDownIcon, ArrowUpIcon } from './Icons';
import CIStatusChip from './CIStatusChip';
import Tooltip from './Tooltip';

type WorkspaceStatusBadgeProps = {
  workspace: WorkspaceResponse;
  locked: boolean;
};

/**
 * The slot between a sidebar workspace's name and its dev button.
 * First match wins: spinner while locked, then the diff size, then — for a
 * clean git workspace — how far the branch diverges from its remote plus the
 * GitHub build status.
 */
export default function WorkspaceStatusBadge({
  workspace,
  locked,
}: WorkspaceStatusBadgeProps): React.ReactElement | null {
  if (locked) {
    return (
      <span className="nav-workspace__changes">
        <WorkingSpinner />
      </span>
    );
  }

  const isGit = !workspace.vcs || workspace.vcs === 'git';
  if (!isGit) return null;

  const linesAdded = workspace.lines_added ?? 0;
  const linesRemoved = workspace.lines_removed ?? 0;
  if (linesAdded > 0 || linesRemoved > 0) {
    return (
      <span className="nav-workspace__changes">
        {linesAdded > 0 && <span className="text-success">+{linesAdded}</span>}
        {linesRemoved > 0 && (
          <span className="text-error" style={{ marginLeft: linesAdded > 0 ? '2px' : '0' }}>
            -{linesRemoved}
          </span>
        )}
      </span>
    );
  }

  const behind = workspace.remote_unique_commits ?? 0;
  const ahead = workspace.local_unique_commits ?? 0;
  const showPair = !!workspace.remote_branch_exists && behind + ahead > 0;
  const showCI = !!workspace.ci_status;
  if (!showPair && !showCI) return null;

  // `ci_status` is absent when there is no remote branch, so the chip needs no
  // separate remote_branch_exists gate — see docs/api.md.
  const remoteLabel = workspace.remote_branch_is_fork ? 'fork' : 'remote';

  return (
    <span className="nav-workspace__changes nav-workspace__sync">
      {showPair && (
        <Tooltip content={`${behind} behind ${remoteLabel}, ${ahead} ahead of ${remoteLabel}`}>
          <span className="nav-workspace__sync-pairs">
            <span className="app-header__git-pair">
              {behind}
              {ArrowDownIcon}
            </span>
            <span className="app-header__git-pair">
              {ahead}
              {ArrowUpIcon}
            </span>
          </span>
        </Tooltip>
      )}
      {showCI && <CIStatusChip status={workspace.ci_status} url={workspace.ci_url} />}
    </span>
  );
}
