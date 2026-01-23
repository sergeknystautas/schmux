import React, { useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import useConnectionMonitor from '../hooks/useConnectionMonitor'
import useTheme from '../hooks/useTheme'
import useVersionInfo from '../hooks/useVersionInfo'
import useAuthUser from '../hooks/useAuthUser'
import Tooltip from './Tooltip'
import { useConfig } from '../contexts/ConfigContext'

const navItems = [
  { to: '/sessions', label: 'Sessions', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
    </svg>
  ), protected: true },
  { to: '/spawn', label: 'Spawn', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10"></circle>
      <line x1="12" y1="8" x2="12" y2="16"></line>
      <line x1="8" y1="12" x2="16" y2="12"></line>
    </svg>
  ), protected: true },
  { to: '/beads', label: 'Beads', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="3"></circle>
      <path d="M12 1v6m0 6v6"></path>
      <path d="M1 12h6m6 0h6"></path>
      <circle cx="5" cy="5" r="1.5"></circle>
      <circle cx="19" cy="5" r="1.5"></circle>
      <circle cx="5" cy="19" r="1.5"></circle>
      <circle cx="19" cy="19" r="1.5"></circle>
    </svg>
  ), protected: true, indent: true },
  { to: '/tips', label: 'Tips', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10"></circle>
      <line x1="12" y1="16" x2="12" y2="12"></line>
      <line x1="12" y1="8" x2="12.01" y2="8"></line>
    </svg>
  ), protected: true },
  { to: '/config', label: 'Config', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"/>
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1Z"/>
    </svg>
  ), protected: false }
];

export default function AppShell() {
  const connected = useConnectionMonitor();
  const { toggleTheme } = useTheme();
  const { isNotConfigured, config } = useConfig();
  const { versionInfo } = useVersionInfo();
  const authUser = useAuthUser(config.access_control?.enabled ?? false);
  const [isUpdating, setIsUpdating] = useState(false);

  const handleUpdate = async () => {
    if (isUpdating) return;
    if (!confirm('Update schmux to the latest version?\n\nThe dashboard will close after the update completes. You will need to restart the daemon.')) {
      return;
    }
    setIsUpdating(true);
    try {
      const response = await fetch('/api/update', { method: 'POST' });
      const data = await response.json().catch(() => ({}));
      if (response.ok) {
        alert(data.message || 'Update successful. Restart schmux to use the new version.');
        // Note: button stays disabled since dashboard will close
      } else {
        alert(data.error || 'Update failed');
        setIsUpdating(false);
      }
    } catch (err) {
      alert('Update failed: ' + (err as Error).message);
      setIsUpdating(false);
    }
  };

  const showUpdateBadge = versionInfo?.update_available;

  return (
    <div className="app-shell">
      <nav className="app-shell__nav">
        <div className="nav-header">
          <NavLink to="/sessions" className="logo">
            schmux
            {showUpdateBadge && (
              <span className="update-badge" title={`Update available: ${versionInfo.latest_version}`}></span>
            )}
          </NavLink>
          <div className="nav-header__version">
            {versionInfo?.version ? (versionInfo.version === 'dev' ? 'dev' : `v${versionInfo.version}`) : 'loading...'}
          </div>
          <div className="nav-header__actions">
            <div className={`connection-pill ${connected ? 'connection-pill--connected' : 'connection-pill--offline'}`}>
              <span className="connection-pill__dot"></span>
              <span>{connected ? 'Connected' : 'Disconnected'}</span>
            </div>
            {showUpdateBadge && (
              <Tooltip content={`Update to v${versionInfo.latest_version}`}>
                <button
                  className="icon-btn update-btn"
                  aria-label="Update schmux"
                  onClick={handleUpdate}
                  disabled={isUpdating}
                >
                  {isUpdating ? (
                    <span className="update-btn__spinner"></span>
                  ) : (
                    <svg className="icon-update" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"></path>
                      <path d="M3 3v5h5"></path>
                      <path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"></path>
                      <path d="M16 21h5v-5"></path>
                    </svg>
                  )}
                </button>
              </Tooltip>
            )}
            <Tooltip content="Toggle theme">
              <button id="themeToggle" className="icon-btn" aria-label="Toggle theme" onClick={toggleTheme}>
                <span className="icon-theme"></span>
              </button>
            </Tooltip>
            <a href="https://github.com/sergeknystautas/schmux" target="_blank" rel="noopener noreferrer" className="icon-btn" aria-label="View on GitHub">
              <svg className="icon-github" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
              </svg>
            </a>
          </div>
          {authUser && (
            <div className="nav-header__user">
              {authUser.avatar_url ? (
                <img className="nav-header__avatar" src={authUser.avatar_url} alt={authUser.login} />
              ) : (
                <span className="nav-header__avatar nav-header__avatar--placeholder" />
              )}
              <span className="nav-header__login">{authUser.login}</span>
            </div>
          )}
        </div>
        <hr className="nav-header__separator" />
        <ul className="nav-list">
          {navItems.map((item) => {
            const isDisabled = item.protected && isNotConfigured;
            const indentClass = (item as any).indent ? 'nav-item--indent' : '';
            return (
              <li className={`nav-item ${indentClass}`} key={item.to}>
                {isDisabled ? (
                  <span className="nav-link nav-link--disabled">
                    {item.icon}
                    {item.label}
                  </span>
                ) : (
                  <NavLink
                    to={item.to}
                    className={({ isActive }) => `nav-link${isActive ? ' nav-link--active' : ''}`}
                  >
                    {item.icon}
                    {item.label}
                  </NavLink>
                )}
              </li>
            );
          })}
        </ul>
      </nav>

      <main className="app-shell__content">
        <Outlet />
      </main>
    </div>
  );
}
