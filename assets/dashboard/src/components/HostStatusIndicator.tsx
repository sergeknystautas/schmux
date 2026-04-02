import type { RemoteHost } from '../lib/types';

export type HostStatus =
  | 'ready'
  | 'connected'
  | 'provisioning'
  | 'connecting'
  | 'disconnected'
  | 'expired'
  | 'reconnecting'
  | 'failed';

const validStatuses: HostStatus[] = [
  'ready',
  'connected',
  'provisioning',
  'connecting',
  'disconnected',
  'expired',
  'reconnecting',
  'failed',
];

function isValidStatus(s: string): s is HostStatus {
  return validStatuses.includes(s as HostStatus);
}

interface HostStatusIndicatorProps {
  status: HostStatus;
  hostname?: string;
  size?: 'sm' | 'md';
}

export default function HostStatusIndicator({
  status,
  hostname,
  size = 'sm',
}: HostStatusIndicatorProps) {
  const getStatusConfig = () => {
    switch (status) {
      case 'ready':
        return { color: 'var(--color-success)', label: 'Ready', icon: null };
      case 'connected':
        return { color: 'var(--color-success)', label: hostname || 'Connected', icon: null };
      case 'provisioning':
        return { color: 'var(--color-warning)', label: 'Provisioning...', icon: 'spinner' };
      case 'connecting':
        return { color: 'var(--color-warning)', label: 'Connecting...', icon: 'spinner' };
      case 'reconnecting':
        return { color: 'var(--color-warning)', label: 'Reconnecting...', icon: 'spinner' };
      case 'disconnected':
        return { color: 'var(--color-error)', label: 'Disconnected', icon: null };
      case 'expired':
        return { color: 'var(--color-text-muted)', label: 'Expired', icon: null };
      case 'failed':
        return { color: 'var(--color-error)', label: 'Failed', icon: null };
      default:
        return { color: 'var(--color-text-muted)', label: 'Unknown', icon: null };
    }
  };

  const config = getStatusConfig();
  const dotSize = size === 'sm' ? '6px' : '8px';
  const fontSize = size === 'sm' ? '0.75rem' : '0.875rem';

  return (
    <span
      className="inline-flex gap-xs"
      style={{
        fontSize,
        color: config.color,
      }}
    >
      {config.icon === 'spinner' ? (
        <span
          className="spinner spinner--small"
          style={{ width: dotSize, height: dotSize }}
          data-testid="host-status-indicator"
          data-variant="spinner"
        />
      ) : (
        <span
          className="flex-shrink-0"
          style={{
            width: dotSize,
            height: dotSize,
            borderRadius: '50%',
            backgroundColor: config.color,
          }}
          data-testid="host-status-indicator"
          data-variant="dot"
        />
      )}
      <span className="truncate">{config.label}</span>
    </span>
  );
}

export function getHostStatus(host: RemoteHost | null): HostStatus {
  if (!host) return 'disconnected';
  return isValidStatus(host.status) ? host.status : 'disconnected';
}
