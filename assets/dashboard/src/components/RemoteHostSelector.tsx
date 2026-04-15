import React, { useState, useEffect, useCallback, useRef } from 'react';
import HostStatusIndicator from './HostStatusIndicator';
import ConnectionProgressModal from './ConnectionProgressModal';
import {
  getRemoteProfileStatuses,
  getErrorMessage,
  getRemoteHosts,
  connectRemoteHost,
  reconnectRemoteHost,
} from '../lib/api';
import { useToast } from './ToastProvider';
import { useModal } from './ModalProvider';
import { useSessions } from '../contexts/SessionsContext';
import type {
  RemoteProfile,
  RemoteProfileStatus,
  RemoteHostStatus,
  RemoteHost,
} from '../lib/types';

export type EnvironmentSelection =
  | { type: 'local' }
  | {
      type: 'remote';
      profileId: string;
      profile: RemoteProfile;
      flavor: string;
      host?: RemoteHost;
      hostId?: string;
    };

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
  const [profileStatuses, setProfileStatuses] = useState<RemoteProfileStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [connecting, setConnecting] = useState<string | null>(null);
  const [connectingProfileId, setConnectingProfileId] = useState<string | null>(null);
  const [connectingFlavor, setConnectingFlavor] = useState<string | null>(null);
  const [connectingDisplayName, setConnectingDisplayName] = useState<string>('');
  const [provisioningSessionId, setProvisioningSessionId] = useState<string | null>(null);
  // Track selected flavor per profile (for profiles with multiple flavors)
  const [selectedFlavors, setSelectedFlavors] = useState<Record<string, string>>({});
  const { success: toastSuccess } = useToast();
  const { alert } = useModal();
  const { workspaces } = useSessions();
  const activeRef = useRef(true);

  // Re-fetch profile statuses on mount and whenever WebSocket broadcasts
  // (BroadcastSessions fires on remote host status changes)
  useEffect(() => {
    activeRef.current = true;
    const load = async () => {
      try {
        const statuses = await getRemoteProfileStatuses();
        if (activeRef.current) setProfileStatuses(statuses);
      } catch (err) {
        console.error('Failed to load remote profile statuses:', err);
      } finally {
        if (activeRef.current) setLoading(false);
      }
    };
    load();
    return () => {
      activeRef.current = false;
    };
  }, [workspaces]);

  // Initialize selected flavors for profiles when statuses load
  useEffect(() => {
    const newSelected: Record<string, string> = {};
    for (const ps of profileStatuses) {
      if (!selectedFlavors[ps.profile.id] && ps.profile.flavors.length > 0) {
        newSelected[ps.profile.id] = ps.profile.flavors[0].flavor;
      }
    }
    if (Object.keys(newSelected).length > 0) {
      setSelectedFlavors((prev) => ({ ...prev, ...newSelected }));
    }
  }, [profileStatuses]); // eslint-disable-line react-hooks/exhaustive-deps

  const getSelectedFlavor = (profileId: string, profile?: RemoteProfile): string => {
    // Persistent hosts have no flavors — return empty string.
    if (profile?.host_type === 'persistent') return '';
    return selectedFlavors[profileId] || '';
  };

  const handleSelectLocal = () => {
    onChange({ type: 'local' });
  };

  const handleSelectExistingHost = useCallback(
    async (profileStatus: RemoteProfileStatus, flavorStr: string, hostStatus: RemoteHostStatus) => {
      if (hostStatus.connected) {
        // Already connected - fetch full host data from API
        try {
          const hosts = await getRemoteHosts();
          const fullHost = hosts.find((h) => h.id === hostStatus.host_id);
          onChange({
            type: 'remote',
            profileId: profileStatus.profile.id,
            profile: profileStatus.profile,
            flavor: flavorStr,
            host: fullHost,
            hostId: hostStatus.host_id,
          });
        } catch (err) {
          console.error('Failed to fetch host data:', err);
          // Fall back to selection without full host data
          onChange({
            type: 'remote',
            profileId: profileStatus.profile.id,
            profile: profileStatus.profile,
            flavor: flavorStr,
            hostId: hostStatus.host_id,
          });
        }
      } else if (hostStatus.status === 'disconnected' || hostStatus.status === 'expired') {
        // Disconnected/expired host - trigger reconnect
        setConnecting(profileStatus.profile.id);
        setConnectingProfileId(profileStatus.profile.id);
        setConnectingFlavor(flavorStr);
        setConnectingDisplayName(profileStatus.profile.display_name);
        try {
          const response = await reconnectRemoteHost(hostStatus.host_id);
          setProvisioningSessionId(response.provisioning_session_id || null);
        } catch (err) {
          alert('Reconnect Failed', getErrorMessage(err, 'Failed to reconnect'));
          setConnecting(null);
          setConnectingProfileId(null);
          setConnectingFlavor(null);
          setConnectingDisplayName('');
        }
      } else {
        // Provisioning/connecting - select it
        onChange({
          type: 'remote',
          profileId: profileStatus.profile.id,
          profile: profileStatus.profile,
          flavor: flavorStr,
          hostId: hostStatus.host_id,
        });
      }
    },
    [onChange, alert]
  );

  const handleSelectNewHost = useCallback(
    async (profileStatus: RemoteProfileStatus, flavorStr: string) => {
      const isPersistentProfile = profileStatus.profile.host_type === 'persistent';

      // For persistent hosts already connected, reuse the existing connection
      // instead of opening a second SSH session.
      if (isPersistentProfile) {
        const allHosts = profileStatus.flavor_hosts.flatMap((fg) => fg.hosts);
        const connectedHost = allHosts.find((h) => h.connected);
        if (connectedHost) {
          handleSelectExistingHost(profileStatus, flavorStr, connectedHost);
          return;
        }
      }

      // Start a new connection for this profile+flavor
      setConnecting(profileStatus.profile.id);
      setConnectingProfileId(profileStatus.profile.id);
      setConnectingFlavor(flavorStr);
      setConnectingDisplayName(profileStatus.profile.display_name);

      try {
        const response = await connectRemoteHost({
          profile_id: profileStatus.profile.id,
          flavor: flavorStr,
        });
        setProvisioningSessionId(response.provisioning_session_id || null);
      } catch (err) {
        alert('Connection Failed', getErrorMessage(err, 'Failed to start connection'));
        setConnecting(null);
        setConnectingProfileId(null);
        setConnectingFlavor(null);
        setConnectingDisplayName('');
      }
    },
    [alert, handleSelectExistingHost]
  );

  const isSelected = (type: 'local' | string, hostId?: string) => {
    if (type === 'local') return value.type === 'local';
    if (value.type !== 'remote') return false;
    if (value.profileId !== type) return false;
    // If hostId is specified, match on it; otherwise just match profile
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

  // Don't show the selector if no remote profiles are configured
  if (!loading && profileStatuses.length === 0) {
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

        {/* Remote profile options — connected hosts first, then provision cards */}
        {loading ? (
          <div className="flex-row gap-sm p-md text-muted">
            <span className="spinner spinner--small" />
            <span>Loading remote hosts...</span>
          </div>
        ) : (
          (() => {
            // Collect all cards into connected vs provision groups so connected
            // cards render left (immediately usable) and provision cards render right.
            const connectedCards: React.ReactNode[] = [];
            const provisionCards: React.ReactNode[] = [];

            for (const profileStatus of profileStatuses) {
              const isConnecting = connecting === profileStatus.profile.id;
              const isPersistent = profileStatus.profile.host_type === 'persistent';
              const currentFlavor = getSelectedFlavor(
                profileStatus.profile.id,
                profileStatus.profile
              );
              const currentFlavorGroup = profileStatus.flavor_hosts.find(
                (fg) => fg.flavor === currentFlavor
              );
              const hosts = currentFlavorGroup?.hosts || [];

              // Existing host cards → connectedCards
              for (const hostStatus of hosts) {
                const selected = isSelected(profileStatus.profile.id, hostStatus.host_id);
                connectedCards.push(
                  <div
                    key={hostStatus.host_id}
                    style={cardStyle(selected)}
                    onClick={() =>
                      !disabled &&
                      !isConnecting &&
                      handleSelectExistingHost(profileStatus, currentFlavor, hostStatus)
                    }
                    role="button"
                    tabIndex={disabled || isConnecting ? -1 : 0}
                    onKeyDown={(e) => {
                      if (!disabled && !isConnecting && (e.key === 'Enter' || e.key === ' ')) {
                        e.preventDefault();
                        handleSelectExistingHost(profileStatus, currentFlavor, hostStatus);
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
                        {hostStatus.hostname || profileStatus.profile.display_name}
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
                      {profileStatus.profile.display_name}
                    </div>
                    <HostStatusIndicator
                      status={hostStatus.status || 'disconnected'}
                      hostname={hostStatus.hostname}
                    />
                  </div>
                );
              }

              // "New host" / "Spawn on" card → provisionCards (hidden for connected persistent hosts)
              if (!(isPersistent && hosts.some((h) => h.connected))) {
                provisionCards.push(
                  <div
                    key={`new-${profileStatus.profile.id}`}
                    style={{ ...cardStyle(false), borderStyle: 'dashed' }}
                    onClick={() =>
                      !disabled &&
                      !isConnecting &&
                      handleSelectNewHost(profileStatus, currentFlavor)
                    }
                    role="button"
                    tabIndex={disabled || isConnecting ? -1 : 0}
                    onKeyDown={(e) => {
                      if (!disabled && !isConnecting && (e.key === 'Enter' || e.key === ' ')) {
                        e.preventDefault();
                        handleSelectNewHost(profileStatus, currentFlavor);
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
                        {isPersistent
                          ? `Spawn on ${profileStatus.profile.display_name}`
                          : `New ${profileStatus.profile.display_name} host`}
                      </strong>
                    </div>
                    <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                      {isPersistent ? (
                        'Connect and create workspace'
                      ) : profileStatus.profile.flavors.length > 1 ? (
                        <select
                          className="select"
                          style={{ fontSize: '0.75rem', padding: '2px 4px' }}
                          value={currentFlavor}
                          onChange={(e) => {
                            e.stopPropagation();
                            setSelectedFlavors((prev) => ({
                              ...prev,
                              [profileStatus.profile.id]: e.target.value,
                            }));
                          }}
                          onClick={(e) => e.stopPropagation()}
                        >
                          {profileStatus.profile.flavors.map((pf) => (
                            <option key={pf.flavor} value={pf.flavor}>
                              {pf.display_name || pf.flavor}
                            </option>
                          ))}
                        </select>
                      ) : (
                        'Provision a new instance'
                      )}
                    </div>
                  </div>
                );
              }
            } // end for

            return (
              <>
                {connectedCards}
                {provisionCards}
              </>
            );
          })()
        )}
      </div>

      {/* Connection Progress Modal */}
      {connecting && connectingProfileId && (
        <ConnectionProgressModal
          profileId={connectingProfileId}
          flavor={connectingFlavor || undefined}
          flavorName={connectingDisplayName}
          provisioningSessionId={provisioningSessionId}
          onClose={() => {
            setConnecting(null);
            setConnectingProfileId(null);
            setConnectingFlavor(null);
            setConnectingDisplayName('');
            setProvisioningSessionId(null);
          }}
          onConnected={async (host) => {
            const profile = profileStatuses.find(
              (ps) => ps.profile.id === connectingProfileId
            )?.profile;
            setConnecting(null);
            setConnectingProfileId(null);
            const flavorStr = connectingFlavor || '';
            setConnectingFlavor(null);
            setConnectingDisplayName('');
            setProvisioningSessionId(null);
            if (profile) {
              onChange({
                type: 'remote',
                profileId: profile.id,
                profile,
                flavor: flavorStr,
                host,
                hostId: host.id,
              });
            }
            onConnectionComplete?.(host);
            toastSuccess(`Connected to ${profile?.display_name || 'remote host'}`);
            // Re-fetch profile statuses so host cards update immediately
            try {
              const statuses = await getRemoteProfileStatuses();
              setProfileStatuses(statuses);
            } catch {
              // WebSocket will update eventually
            }
          }}
        />
      )}
    </div>
  );
}
