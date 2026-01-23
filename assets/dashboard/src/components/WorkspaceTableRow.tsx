import React from 'react';
import { extractRepoName } from '../lib/utils';
import Tooltip from './Tooltip';
import type { WorkspaceResponse } from '../lib/types';

type WorkspaceTableRowProps = {
  workspace: WorkspaceResponse;
  onToggle: () => void;
  expanded?: boolean;
  sessionCount: number;
  actions?: React.ReactNode;
  sessions?: React.ReactNode;
};

export default function WorkspaceTableRow({ workspace, onToggle, expanded, sessionCount, actions, sessions }: WorkspaceTableRowProps) {
  const repoName = extractRepoName(workspace.repo);

  // Git branch icon SVG
  const branchIcon = (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <line x1="6" y1="3" x2="6" y2="15"></line>
      <circle cx="18" cy="6" r="3"></circle>
      <circle cx="6" cy="18" r="3"></circle>
      <path d="M18 9a9 9 0 0 1-9 9"></path>
    </svg>
  );

  // Build git status indicators - always show both behind and ahead
  const gitStatusParts = [];
  // Always show both behind and ahead numbers
  const behind = workspace.git_behind ?? 0;
  const ahead = workspace.git_ahead ?? 0;
  gitStatusParts.push(
    <Tooltip key="status" content={`${behind} behind, ${ahead} ahead`}>
      <span
        className="workspace-item__git-status"
        style={{
          marginLeft: '8px',
          color: 'var(--color-text-muted)',
          fontSize: '0.75rem',
          fontFamily: 'var(--font-mono)',
        }}
      >
        {behind} | {ahead}
      </span>
    </Tooltip>
  );

  return (
    <div className="workspace-item" key={workspace.id}>
      <div className="workspace-item__header" onClick={onToggle}>
        <div className="workspace-item__info">
          <span className={`workspace-item__toggle${expanded ? '' : ' workspace-item__toggle--collapsed'}`}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <polyline points="6 9 12 15 18 9"></polyline>
            </svg>
          </span>
          <span className="workspace-item__name">
            {workspace.id}
            {workspace.git_dirty && (
              <Tooltip content="Uncommitted changes">
                <span
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: '8px',
                    height: '8px',
                    borderRadius: '50%',
                    backgroundColor: 'var(--color-warning)',
                    marginLeft: '6px',
                  }}
                />
              </Tooltip>
            )}
          </span>
          <span className="workspace-item__meta">
            {workspace.branch_url ? (
              <Tooltip content="View branch in git">
                <a
                  href={workspace.branch_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="workspace-item__branch-link"
                  onClick={(e) => e.stopPropagation()}
                >
                  {branchIcon}
                  {workspace.branch}
                </a>
              </Tooltip>
            ) : (
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
                {branchIcon}
                {workspace.branch}
              </span>
            )} ·
            {gitStatusParts}
          </span>
          <span className="badge badge--neutral">{sessionCount} session{sessionCount !== 1 ? 's' : ''}</span>
        </div>
        {actions && (
          <div className="workspace-item__actions">
            {actions}
          </div>
        )}
      </div>

      <div className={`workspace-item__sessions${expanded ? ' workspace-item__sessions--expanded' : ''}`}>
        {sessions}
      </div>
    </div>
  );
}
