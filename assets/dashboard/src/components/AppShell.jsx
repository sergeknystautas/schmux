import React from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import useConnectionMonitor from '../hooks/useConnectionMonitor.js';
import useTheme from '../hooks/useTheme.js';
import Tooltip from './Tooltip.jsx';

const navItems = [
  { to: '/sessions', label: 'Sessions', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
      <line x1="9" y1="3" x2="9" y2="21"></line>
    </svg>
  ) },
  { to: '/workspaces', label: 'Workspaces', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
    </svg>
  ) },
  { to: '/spawn', label: 'Spawn', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10"></circle>
      <line x1="12" y1="8" x2="12" y2="16"></line>
      <line x1="8" y1="12" x2="16" y2="12"></line>
    </svg>
  ) },
  { to: '/tips', label: 'Tips', icon: (
    <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10"></circle>
      <line x1="12" y1="16" x2="12" y2="12"></line>
      <line x1="12" y1="8" x2="12.01" y2="8"></line>
    </svg>
  ) }
];

export default function AppShell() {
  const connected = useConnectionMonitor();
  const { toggleTheme } = useTheme();

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
          {navItems.map((item) => (
            <li className="nav-item" key={item.to}>
              <NavLink
                to={item.to}
                className={({ isActive }) => `nav-link${isActive ? ' nav-link--active' : ''}`}
              >
                {item.icon}
                {item.label}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>

      <main className="app-shell__content">
        <Outlet />
      </main>
    </div>
  );
}
