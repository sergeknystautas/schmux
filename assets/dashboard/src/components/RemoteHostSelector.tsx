import React, { useState, useEffect, useCallback, useRef } from 'react';
import HostStatusIndicator from './HostStatusIndicator';
import ConnectionProgressModal from './ConnectionProgressModal';
import {
  getRemoteFlavorStatuses,
  getErrorMessage,
  getRemoteHosts,
  connectRemoteHost,
  reconnectRemoteHost,
} from '../lib/api';
import { useToast } from './ToastProvider';
import { useModal } from './ModalProvider';
import { useSessions } from '../contexts/SessionsContext';
import type { RemoteFlavor, RemoteFlavorStatus, RemoteHostStatus, RemoteHost } from '../lib/types';

export type EnvironmentSelection =
  | { type: 'local' }
  | { type: 'remote'; flavorId: string; flavor: RemoteFlavor; host?: RemoteHost; hostId?: string };

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
  const { success: toastSuccess } = useToast();
  const { alert } = useModal();
  const { workspaces } = useSessions();
  const activeRef = useRef(true);

  // Re-fetch flavor statuses on mount and whenever WebSocket broadcasts
  // (BroadcastSessions fires on remote host status changes)
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
    return () => {
      activeRef.current = false;
    };
  }, [workspaces]);

  const handleSelectLocal = () => {
    onChange({ type: 'local' });
  };

  const handleSelectExistingHost = useCallback(
    async (flavorStatus: RemoteFlavorStatus, hostStatus: RemoteHostStatus) => {
      if (hostStatus.connected) {
        // Already connected - fetch full host data from API
        try {
          const hosts = await getRemoteHosts();
          const fullHost = hosts.find((h) => h.id === hostStatus.host_id);
          onChange({
            type: 'remote',
            flavorId: flavorStatus.flavor.id,
            flavor: flavorStatus.flavor,
            host: fullHost,
            hostId: hostStatus.host_id,
          });
        } catch (err) {
          console.error('Failed to fetch host data:', err);
          // Fall back to selection without full host data
          onChange({
            type: 'remote',
            flavorId: flavorStatus.flavor.id,
            flavor: flavorStatus.flavor,
            hostId: hostStatus.host_id,
          });
        }
      } else if (hostStatus.status === 'disconnected' || hostStatus.status === 'expired') {
        // Disconnected/expired host - trigger reconnect
        setConnecting(flavorStatus.flavor.id);
        setConnectingFlavor(flavorStatus.flavor);
        try {
          const response = await reconnectRemoteHost(hostStatus.host_id);
          setProvisioningSessionId(response.provisioning_session_id || null);
        } catch (err) {
          alert('Reconnect Failed', getErrorMessage(err, 'Failed to reconnect'));
          setConnecting(null);
          setConnectingFlavor(null);
        }
      } else {
        // Provisioning/connecting - select it
        onChange({
          type: 'remote',
          flavorId: flavorStatus.flavor.id,
          flavor: flavorStatus.flavor,
          hostId: hostStatus.host_id,
        });
      }
    },
    [onChange, alert]
  );

  const handleSelectNewHost = useCallback(
    async (flavorStatus: RemoteFlavorStatus) => {
      // Start a new connection for this flavor
      setConnecting(flavorStatus.flavor.id);
      setConnectingFlavor(flavorStatus.flavor);

      try {
        const response = await connectRemoteHost({ flavor_id: flavorStatus.flavor.id });
        setProvisioningSessionId(response.provisioning_session_id || null);
      } catch (err) {
        alert('Connection Failed', getErrorMessage(err, 'Failed to start connection'));
        setConnecting(null);
        setConnectingFlavor(null);
      }
    },
    [alert]
  );

  const isSelected = (type: 'local' | string, hostId?: string) => {
    if (type === 'local') return value.type === 'local';
    if (value.type !== 'remote') return false;
    if (value.flavorId !== type) return false;
    // If hostId is specified, match on it; otherwise just match flavor
    if (hostId) return value.hostId === hostId;
    return !value.hostId;
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

  // Don't show the selector if no remote flavors are configured
  if (!loading && flavors.length === 0) {
    return null;
  }

  return (
    <div className="mb-lg">
      <label className="form-group__label mb-sm">Where do you want to run?</label>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 'var(--spacing-md)',
        }}
      >
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
          <div className="flex-row gap-sm">
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
              <line x1="8" y1="21" x2="16" y2="21" />
              <line x1="12" y1="17" x2="12" y2="21" />
            </svg>
            <strong>Local</strong>
          </div>
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>Your machine</div>
          <HostStatusIndicator status="ready" />
        </div>

        {/* Remote flavor options */}
        {loading ? (
          <div className="flex-row gap-sm p-md text-muted">
            <span className="spinner spinner--small" />
            <span>Loading remote hosts...</span>
          </div>
        ) : (
          flavors.map((flavorStatus) => {
            const isConnecting = connecting === flavorStatus.flavor.id;
            // Render existing host cards (if any) + a "New host" card
            return (
              <React.Fragment key={flavorStatus.flavor.id}>
                {(flavorStatus.hosts || []).map((hostStatus) => {
                  const selected = isSelected(flavorStatus.flavor.id, hostStatus.host_id);
                  return (
                    <div
                      key={hostStatus.host_id}
                      style={cardStyle(selected)}
                      onClick={() =>
                        !disabled &&
                        !isConnecting &&
                        handleSelectExistingHost(flavorStatus, hostStatus)
                      }
                      role="button"
                      tabIndex={disabled || isConnecting ? -1 : 0}
                      onKeyDown={(e) => {
                        if (!disabled && !isConnecting && (e.key === 'Enter' || e.key === ' ')) {
                          e.preventDefault();
                          handleSelectExistingHost(flavorStatus, hostStatus);
                        }
                      }}
                    >
                      <div className="flex-row gap-sm">
                        <svg
                          width="20"
                          height="20"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                        >
                          <rect x="1" y="4" width="22" height="16" rx="2" ry="2" />
                          <line x1="1" y1="10" x2="23" y2="10" />
                        </svg>
                        <strong className="truncate">
                          {hostStatus.hostname || flavorStatus.flavor.display_name}
                        </strong>
                      </div>
                      <div
                        style={{
                          fontSize: '0.75rem',
                          color: 'var(--color-text-muted)',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                        }}
                      >
                        {flavorStatus.flavor.display_name}
                      </div>
                      <HostStatusIndicator
                        status={hostStatus.status || 'disconnected'}
                        hostname={hostStatus.hostname}
                      />
                    </div>
                  );
                })}

                {/* "+ New host" card */}
                <div
                  style={{
                    ...cardStyle(false),
                    borderStyle: 'dashed',
                  }}
                  onClick={() => !disabled && !isConnecting && handleSelectNewHost(flavorStatus)}
                  role="button"
                  tabIndex={disabled || isConnecting ? -1 : 0}
                  onKeyDown={(e) => {
                    if (!disabled && !isConnecting && (e.key === 'Enter' || e.key === ' ')) {
                      e.preventDefault();
                      handleSelectNewHost(flavorStatus);
                    }
                  }}
                >
                  <div className="flex-row gap-sm">
                    <svg
                      width="20"
                      height="20"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                    >
                      <line x1="12" y1="5" x2="12" y2="19" />
                      <line x1="5" y1="12" x2="19" y2="12" />
                    </svg>
                    <strong className="truncate">
                      New {flavorStatus.flavor.display_name} host
                    </strong>
                  </div>
                  <div
                    style={{
                      fontSize: '0.75rem',
                      color: 'var(--color-text-muted)',
                    }}
                  >
                    Provision a new instance
                  </div>
                </div>
              </React.Fragment>
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
          onConnected={async (host) => {
            setConnecting(null);
            setConnectingFlavor(null);
            setProvisioningSessionId(null);
            onChange({
              type: 'remote',
              flavorId: connectingFlavor.id,
              flavor: connectingFlavor,
              host,
              hostId: host.id,
            });
            onConnectionComplete?.(host);
            toastSuccess(`Connected to ${connectingFlavor.display_name}`);
            // Re-fetch flavor statuses so host cards update immediately
            try {
              const statuses = await getRemoteFlavorStatuses();
              setFlavors(statuses);
            } catch {
              // WebSocket will update eventually
            }
          }}
        />
      )}
    </div>
  );
}
