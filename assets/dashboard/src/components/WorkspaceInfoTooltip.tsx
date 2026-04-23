import type { WorkspaceResponse } from '../lib/types';
import { buildWorkspaceInfoRows } from '../lib/workspace-info';
import { ArrowDownIcon, ArrowUpIcon } from './Icons';

interface WorkspaceInfoTooltipProps {
  workspace: WorkspaceResponse;
}

const branchIcon = (
  <svg
    width="14"
    height="14"
    viewBox="-1 -1 26 26"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    style={{ marginRight: 4, verticalAlign: 'text-bottom' }}
    aria-hidden="true"
  >
    <circle cx="3" cy="5" r="3" />
    <line x1="3" y1="8" x2="3" y2="17" />
    <circle cx="3" cy="20" r="3" />
    <circle cx="21" cy="20" r="3" />
    <line x1="21" y1="17" x2="21" y2="12" />
    <path d="M21 12c0-5-7-7-10-7" />
    <polyline points="13,3 11,5 13,7" />
  </svg>
);

export default function WorkspaceInfoTooltip({ workspace }: WorkspaceInfoTooltipProps) {
  const rows = buildWorkspaceInfoRows(workspace);
  return (
    <span className="workspace-info-tooltip">
      {rows.map((row, i) => {
        if (row.kind === 'commits') {
          return (
            <span key={i} className="workspace-info-tooltip__row">
              {branchIcon}
              <span className="app-header__git-pair">
                {row.behind}
                {ArrowDownIcon}
              </span>{' '}
              <span className="app-header__git-pair">
                {row.ahead}
                {ArrowUpIcon}
              </span>
            </span>
          );
        }
        return (
          <span
            key={i}
            className="workspace-info-tooltip__row"
            style={row.small ? { fontSize: '0.85em' } : undefined}
          >
            {row.value}
          </span>
        );
      })}
    </span>
  );
}
