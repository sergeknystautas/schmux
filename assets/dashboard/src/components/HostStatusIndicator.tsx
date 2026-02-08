import type { RemoteHost } from '../lib/types';

export type HostStatus = 'ready' | 'connected' | 'provisioning' | 'connecting' | 'disconnected' | 'expired' | 'reconnecting';

const validStatuses: HostStatus[] = ['ready', 'connected', 'provisioning', 'connecting', 'disconnected', 'expired', 'reconnecting'];

function isValidStatus(s: string): s is HostStatus {
  return validStatuses.includes(s as HostStatus);
}

interface HostStatusIndicatorProps {
  status: HostStatus;
  hostname?: string;
  size?: 'sm' | 'md';
}

export default function HostStatusIndicator({ status, hostname, size = 'sm' }: HostStatusIndicatorProps) {
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
      default:
        return { color: 'var(--color-text-muted)', label: 'Unknown', icon: null };
    }
  };

  const config = getStatusConfig();
  const dotSize = size === 'sm' ? '6px' : '8px';
  const fontSize = size === 'sm' ? '0.75rem' : '0.875rem';

  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--spacing-xs)',
        fontSize,
        color: config.color,
      }}
    >
      {config.icon === 'spinner' ? (
        <span className="spinner spinner--small" style={{ width: dotSize, height: dotSize }} />
      ) : (
        <span
          style={{
            width: dotSize,
            height: dotSize,
            borderRadius: '50%',
            backgroundColor: config.color,
            flexShrink: 0,
          }}
        />
      )}
      <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
        {config.label}
      </span>
    </span>
  );
}

export function getHostStatus(host: RemoteHost | null): HostStatus {
  if (!host) return 'disconnected';
  return isValidStatus(host.status) ? host.status : 'disconnected';
}
