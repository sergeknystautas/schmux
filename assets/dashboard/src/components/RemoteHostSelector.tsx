import { useState, useEffect, useCallback, useRef } from 'react';
import HostStatusIndicator, { getHostStatus, type HostStatus } from './HostStatusIndicator';
import ConnectionProgressModal from './ConnectionProgressModal';
import { getRemoteFlavorStatuses, getErrorMessage, getRemoteHosts, connectRemoteHost } from '../lib/api';
import { useToast } from './ToastProvider';
import type { RemoteFlavor, RemoteFlavorStatus, RemoteHost } from '../lib/types';

export type EnvironmentSelection =
  | { type: 'local' }
  | { type: 'remote'; flavorId: string; flavor: RemoteFlavor; host?: RemoteHost };

interface RemoteHostSelectorProps {
  value: EnvironmentSelection;
  onChange: (selection: EnvironmentSelection) => void;
  onConnectionComplete?: (host: RemoteHost) => void;
  disabled?: boolean;
}

export default function RemoteHostSelector({
  value,
  onChange,
  onConnectionComplete,
  disabled = false,
}: RemoteHostSelectorProps) {
  const [flavors, setFlavors] = useState<RemoteFlavorStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [connecting, setConnecting] = useState<string | null>(null);
  const [connectingFlavor, setConnectingFlavor] = useState<RemoteFlavor | null>(null);
  const [provisioningSessionId, setProvisioningSessionId] = useState<string | null>(null);
  const { error: toastError, success: toastSuccess } = useToast();
  const activeRef = useRef(true);

  useEffect(() => {
    activeRef.current = true;
    const load = async () => {
      try {
        const statuses = await getRemoteFlavorStatuses();
        if (activeRef.current) setFlavors(statuses);
      } catch (err) {
        console.error('Failed to load remote flavor statuses:', err);
      } finally {
        if (activeRef.current) setLoading(false);
      }
    };
    load();
    return () => { activeRef.current = false; };
  }, []);

  const handleSelectLocal = () => {
    onChange({ type: 'local' });
  };

  const handleSelectRemote = useCallback(async (flavorStatus: RemoteFlavorStatus) => {
    if (flavorStatus.connected && flavorStatus.host_id) {
      // Already connected - fetch full host data from API
      try {
        const hosts = await getRemoteHosts();
        const fullHost = hosts.find(h => h.id === flavorStatus.host_id);
        onChange({
          type: 'remote',
          flavorId: flavorStatus.flavor.id,
          flavor: flavorStatus.flavor,
          host: fullHost,
        });
      } catch (err) {
        console.error('Failed to fetch host data:', err);
        // Fall back to selection without full host data
        onChange({
          type: 'remote',
          flavorId: flavorStatus.flavor.id,
          flavor: flavorStatus.flavor,
        });
      }
    } else {
      // Need to connect - start connection and show modal
      setConnecting(flavorStatus.flavor.id);
      setConnectingFlavor(flavorStatus.flavor);

      try {
        // Start the connection (returns 202 with provisioning session ID)
        const response = await connectRemoteHost({ flavor_id: flavorStatus.flavor.id });
        setProvisioningSessionId(response.provisioning_session_id || null);
      } catch (err) {
        toastError(getErrorMessage(err, 'Failed to start connection'));
        setConnecting(null);
        setConnectingFlavor(null);
      }
    }
  }, [onChange, onConnectionComplete, toastError, toastSuccess]);

  const isSelected = (type: 'local' | string) => {
    if (type === 'local') return value.type === 'local';
    return value.type === 'remote' && value.flavorId === type;
  };

  const cardStyle = (selected: boolean) => ({
    display: 'flex',
    flexDirection: 'column' as const,
    gap: 'var(--spacing-xs)',
    padding: 'var(--spacing-md)',
    border: `2px solid ${selected ? 'var(--color-accent)' : 'var(--color-border)'}`,
    borderRadius: 'var(--radius-md)',
    backgroundColor: selected ? 'var(--color-accent-bg)' : 'var(--color-surface)',
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.6 : 1,
    transition: 'border-color 0.15s, background-color 0.15s',
    minWidth: '160px',
  });

  return (
    <div style={{ marginBottom: 'var(--spacing-lg)' }}>
      <label className="form-group__label" style={{ marginBottom: 'var(--spacing-sm)' }}>
        Where do you want to run?
      </label>
      <div style={{
        display: 'flex',
        flexWrap: 'wrap',
        gap: 'var(--spacing-md)',
      }}>
        {/* Local option */}
        <div
          style={cardStyle(isSelected('local'))}
          onClick={() => !disabled && handleSelectLocal()}
          role="button"
          tabIndex={disabled ? -1 : 0}
          onKeyDown={(e) => {
            if (!disabled && (e.key === 'Enter' || e.key === ' ')) {
              e.preventDefault();
              handleSelectLocal();
            }
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
              <line x1="8" y1="21" x2="16" y2="21" />
              <line x1="12" y1="17" x2="12" y2="21" />
            </svg>
            <strong>Local</strong>
          </div>
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
            Your machine
          </div>
          <HostStatusIndicator status="ready" />
        </div>

        {/* Remote flavor options */}
        {loading ? (
          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--spacing-sm)',
            padding: 'var(--spacing-md)',
            color: 'var(--color-text-muted)',
          }}>
            <span className="spinner spinner--small" />
            <span>Loading remote hosts...</span>
          </div>
        ) : (
          flavors.map((flavorStatus) => {
            const isConnecting = connecting === flavorStatus.flavor.id;
            const selected = isSelected(flavorStatus.flavor.id);

            return (
              <div
                key={flavorStatus.flavor.id}
                style={cardStyle(selected)}
                onClick={() => !disabled && !isConnecting && handleSelectRemote(flavorStatus)}
                role="button"
                tabIndex={disabled || isConnecting ? -1 : 0}
                onKeyDown={(e) => {
                  if (!disabled && !isConnecting && (e.key === 'Enter' || e.key === ' ')) {
                    e.preventDefault();
                    handleSelectRemote(flavorStatus);
                  }
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}>
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <rect x="1" y="4" width="22" height="16" rx="2" ry="2" />
                    <line x1="1" y1="10" x2="23" y2="10" />
                  </svg>
                  <strong style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {flavorStatus.flavor.display_name}
                  </strong>
                </div>
                <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {flavorStatus.flavor.flavor}
                </div>
                {isConnecting ? (
                  <HostStatusIndicator status="provisioning" />
                ) : flavorStatus.connected ? (
                  <HostStatusIndicator status={flavorStatus.status || 'connected'} hostname={flavorStatus.hostname} />
                ) : flavorStatus.status === 'provisioning' || flavorStatus.status === 'authenticating' ? (
                  <HostStatusIndicator status={flavorStatus.status} hostname={flavorStatus.hostname} />
                ) : (
                  <span style={{
                    fontSize: '0.75rem',
                    color: 'var(--color-text-muted)',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 'var(--spacing-xs)',
                  }}>
                    <span style={{
                      width: '6px',
                      height: '6px',
                      borderRadius: '50%',
                      backgroundColor: 'var(--color-text-muted)',
                    }} />
                    Click to connect
                  </span>
                )}
              </div>
            );
          })
        )}
      </div>

      {/* Connection Progress Modal */}
      {connecting && connectingFlavor && (
        <ConnectionProgressModal
          flavorId={connecting}
          flavorName={connectingFlavor.display_name}
          provisioningSessionId={provisioningSessionId}
          onClose={() => {
            setConnecting(null);
            setConnectingFlavor(null);
            setProvisioningSessionId(null);
          }}
          onConnected={(host) => {
            setConnecting(null);
            setConnectingFlavor(null);
            setProvisioningSessionId(null);
            onChange({
              type: 'remote',
              flavorId: connectingFlavor.id,
              flavor: connectingFlavor,
              host,
            });
            onConnectionComplete?.(host);
            toastSuccess(`Connected to ${connectingFlavor.display_name}`);
          }}
        />
      )}
    </div>
  );
}
