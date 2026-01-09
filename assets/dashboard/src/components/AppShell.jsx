import React from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import useConnectionMonitor from '../hooks/useConnectionMonitor.js';
import useTheme from '../hooks/useTheme.js';
import Tooltip from './Tooltip.jsx';
import { useConfig } from '../contexts/ConfigContext.jsx';

const navItems = [
  { to: '/sessions', label: 'Sessions', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
      <line x1="9" y1="3" x2="9" y2="21"></line>
    </svg>
  ), protected: true },
  { to: '/workspaces', label: 'Workspaces', icon: (
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
  const { isNotConfigured } = useConfig();
  const location = useLocation();

  return (
    <div className="app-shell">
      <header className="app-shell__header">
        <NavLink to="/sessions" className="logo">schmux</NavLink>
        <div className="header-actions">
          <div className={`connection-pill ${connected ? 'connection-pill--connected' : 'connection-pill--offline'}`}>
            <span className="connection-pill__dot"></span>
            <span>{connected ? 'Connected' : 'Disconnected'}</span>
          </div>
          <Tooltip content="Toggle theme">
            <button id="themeToggle" className="icon-btn" aria-label="Toggle theme" onClick={toggleTheme}>
              <span className="icon-theme"></span>
            </button>
          </Tooltip>
        </div>
      </header>

      <nav className="app-shell__nav">
        <ul className="nav-list">
          {navItems.map((item) => {
            const isDisabled = item.protected && isNotConfigured;
            return (
              <li className="nav-item" key={item.to}>
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
