import React from 'react';
import Tooltip from './Tooltip';

const labels: Record<string, string> = {
  success: 'CI: passing',
  failure: 'CI: failing',
  in_progress: 'CI: running',
  queued: 'CI: queued',
};

const checkIcon = (
  <svg width="10" height="10" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path
      d="M3 8.5L6.5 12L13 4.5"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

const crossIcon = (
  <svg width="10" height="10" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path d="M4 4L12 12M12 4L4 12" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
  </svg>
);

function chipIcon(status: string): React.ReactNode {
  switch (status) {
    case 'success':
      return checkIcon;
    case 'failure':
      return crossIcon;
    case 'in_progress':
      return <span className="app-header__ci-dot" />;
    case 'queued':
      return <span className="app-header__ci-circle" />;
    default:
      return null;
  }
}

type CIStatusChipProps = {
  status?: string;
  url?: string;
};

export default function CIStatusChip({
  status,
  url,
}: CIStatusChipProps): React.ReactElement | null {
  if (!status) return null;
  const label = labels[status] ?? 'CI status';
  const icon = chipIcon(status);
  if (!icon) return null;
  const className = `app-header__ci app-header__ci--${status}`;
  const inner = url ? (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className={className}
      aria-label={label}
    >
      {icon}
    </a>
  ) : (
    <span className={className} aria-label={label}>
      {icon}
    </span>
  );
  return <Tooltip content={label}>{inner}</Tooltip>;
}
