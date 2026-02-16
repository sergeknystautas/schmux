import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useSessions } from '../contexts/SessionsContext';
import { useConfig } from '../contexts/ConfigContext';
import { remoteAccessOn, remoteAccessOff, getErrorMessage } from '../lib/api';

export default function RemoteAccessPanel() {
  const { remoteAccessStatus } = useSessions();
  const { config } = useConfig();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isDisabled = config?.remote_access?.disabled;
  const pinHashSet = config?.remote_access?.pin_hash_set;
  const isActive =
    remoteAccessStatus.state === 'connected' || remoteAccessStatus.state === 'starting';

  const handleToggle = async () => {
    setError(null);
    setLoading(true);
    try {
      if (isActive) {
        await remoteAccessOff();
      } else {
        await remoteAccessOn();
      }
    } catch (err) {
      setError(getErrorMessage(err, 'Remote access error'));
    } finally {
      setLoading(false);
    }
  };

  if (isDisabled) {
    return null;
  }

  return (
    <div className="remote-access-panel" data-testid="remote-access-panel">
      <div className="remote-access-panel__body">
        <span className="remote-access-panel__title">Remote Access</span>

        {!pinHashSet && remoteAccessStatus.state === 'off' && (
          <div className="remote-access-panel__warning">
            <Link to="/config?tab=access">Set a PIN</Link> to enable remote access.
          </div>
        )}

        {remoteAccessStatus.state === 'starting' && (
          <div className="remote-access-panel__status remote-access-panel__status--starting">
            Starting tunnel...
          </div>
        )}

        {remoteAccessStatus.state === 'connected' && remoteAccessStatus.url && (
          <div className="remote-access-panel__status remote-access-panel__status--connected">
            <a
              href={remoteAccessStatus.url}
              target="_blank"
              rel="noopener noreferrer"
              className="remote-access-panel__url"
            >
              {remoteAccessStatus.url.replace('https://', '')}
            </a>
          </div>
        )}

        {remoteAccessStatus.state === 'error' && (
          <div className="remote-access-panel__status remote-access-panel__status--error">
            {remoteAccessStatus.error || 'Tunnel error'}
          </div>
        )}

        {error && (
          <div className="remote-access-panel__status remote-access-panel__status--error">
            {error}
          </div>
        )}
      </div>

      <button
        className={`remote-access-panel__toggle ${isActive ? 'remote-access-panel__toggle--active' : ''}`}
        onClick={handleToggle}
        disabled={loading || remoteAccessStatus.state === 'starting'}
        data-testid="remote-access-toggle"
      >
        {loading || remoteAccessStatus.state === 'starting' ? (
          '...'
        ) : isActive ? (
          <>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" stroke="none">
              <rect x="4" y="4" width="16" height="16" rx="2" />
            </svg>
            Stop
          </>
        ) : (
          <>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" stroke="none">
              <polygon points="6,3 20,12 6,21" />
            </svg>
            Start
          </>
        )}
      </button>
    </div>
  );
}
