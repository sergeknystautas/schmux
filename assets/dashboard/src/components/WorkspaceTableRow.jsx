import React from 'react';
import { extractRepoName } from '../lib/utils.js';
import Tooltip from './Tooltip.jsx';

export default function WorkspaceTableRow({ workspace, onToggle, expanded, sessionCount, actions, sessions }) {
  const repoName = extractRepoName(workspace.repo);

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
            {workspace.branch} ·
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
