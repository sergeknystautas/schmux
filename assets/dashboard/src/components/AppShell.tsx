import React from 'react';
import { NavLink, Outlet, useNavigate, useParams, useLocation } from 'react-router-dom';
import useTheme from '../hooks/useTheme'
import useVersionInfo from '../hooks/useVersionInfo'
import useLocalStorage from '../hooks/useLocalStorage'
import Tooltip from './Tooltip'
import { useConfig } from '../contexts/ConfigContext'
import { useSessions } from '../contexts/SessionsContext'
import { formatRelativeTime } from '../lib/utils'
import { navigateToWorkspace } from '../lib/navigation'

const NAV_COLLAPSED_KEY = 'schmux-nav-collapsed';

const nudgeStateEmoji: Record<string, string> = {
  'Needs Authorization': '\u26D4\uFE0F',
  'Needs Feature Clarification': '\uD83D\uDD0D',
  'Needs User Testing': '\uD83D\uDC40',
  'Completed': '\u2705',
};

function WorkingSpinner() {
  return <span className="working-spinner"></span>;
}

function formatNudgeSummary(summary?: string) {
  if (!summary) return null;
  let text = summary.trim();
  if (text.length > 40) {
    text = text.substring(0, 37) + '...';
  }
  return text;
}

export default function AppShell() {
  const { toggleTheme } = useTheme();
  const { isNotConfigured, config, getRepoName } = useConfig();
  const { versionInfo } = useVersionInfo();
  const { workspaces, connected } = useSessions();
  const navigate = useNavigate();
  const location = useLocation();
  const { sessionId } = useParams();
  const [navCollapsed, setNavCollapsed] = useLocalStorage(NAV_COLLAPSED_KEY, false);

  // Check if we're on a diff page for a specific workspace
  const diffMatch = location.pathname.match(/^\/diff\/(.+)$/);
  const activeWorkspaceId = diffMatch ? diffMatch[1] : null;

  const showUpdateBadge = versionInfo?.update_available;
  const nudgenikEnabled = Boolean(config?.nudgenik?.target);

  const handleWorkspaceClick = (workspaceId: string) => {
    navigateToWorkspace(navigate, workspaces || [], workspaceId);
  };

  const handleSessionClick = (sessId: string) => {
    navigate(`/sessions/${sessId}`);
  };

  return (
    <div className={`app-shell${navCollapsed ? ' app-shell--collapsed' : ''}`}>
      <nav className="app-shell__nav">
        <div className="nav-top">
          <div className="nav-header">
            <NavLink to="/" className="logo">
              schmux
              {showUpdateBadge && (
                <span className="update-badge" title={`Update available: ${versionInfo.latest_version}`}></span>
              )}
            </NavLink>
            <button
              className="nav-collapse-btn"
              onClick={() => setNavCollapsed(!navCollapsed)}
              aria-label={navCollapsed ? 'Expand navigation' : 'Collapse navigation'}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                {navCollapsed ? (
                  <polyline points="9 18 15 12 9 6"></polyline>
                ) : (
                  <polyline points="15 18 9 12 15 6"></polyline>
                )}
              </svg>
            </button>
          </div>

          <div className="nav-workspaces">
            <div className="nav-section-header">
              <span className="nav-section-title">Workspaces</span>
              <Tooltip content="New workspace">
                <button
                  className="nav-section-add"
                  onClick={() => navigate('/spawn')}
                  aria-label="New workspace"
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                    <line x1="12" y1="5" x2="12" y2="19"></line>
                    <line x1="5" y1="12" x2="19" y2="12"></line>
                  </svg>
                </button>
              </Tooltip>
            </div>
            {(!workspaces || workspaces.length === 0) && (
              <div className="nav-empty-state">
                <p>No workspaces yet</p>
                <button className="btn btn--sm btn--primary" onClick={() => navigate('/spawn')}>
                  Create workspace
                </button>
              </div>
            )}
            {workspaces?.map((workspace) => {
              const linesAdded = workspace.git_lines_added ?? 0;
              const linesRemoved = workspace.git_lines_removed ?? 0;
              const hasChanges = linesAdded > 0 || linesRemoved > 0;
              const isWorkspaceActive = workspace.id === activeWorkspaceId;

              return (
                <div key={workspace.id} className={`nav-workspace${isWorkspaceActive ? ' nav-workspace--active' : ''}`}>
                  <div
                    className="nav-workspace__header"
                    onClick={() => handleWorkspaceClick(workspace.id)}
                  >
                    <span className="nav-workspace__name">
                      {workspace.branch}
                    </span>
                    {hasChanges && (
                      <span className="nav-workspace__changes">
                        {linesAdded > 0 && <span className="text-success">+{linesAdded}</span>}
                        {linesRemoved > 0 && <span className="text-error" style={{ marginLeft: linesAdded > 0 ? '2px' : '0' }}>-{linesRemoved}</span>}
                      </span>
                    )}
                  </div>
                  <div className="nav-workspace__repo">{getRepoName(workspace.repo)}</div>
                  <div className="nav-workspace__sessions">
                    {workspace.sessions?.map((sess) => {
                      const isActive = sess.id === sessionId;
                      const activityDisplay = !sess.running
                        ? 'Stopped'
                        : sess.last_output_at
                          ? formatRelativeTime(sess.last_output_at)
                          : '-';

                      // Check if this session's target is promptable
                      const runTarget = (config?.run_targets || []).find(t => t.name === sess.target);
                      const isPromptable = runTarget ? runTarget.type === 'promptable' : true;

                      const nudgeEmoji = sess.nudge_state ? (nudgeStateEmoji[sess.nudge_state] || '\uD83D\uDCDD') : null;
                      const nudgeSummary = formatNudgeSummary(sess.nudge_summary);

                      // Determine what to show in row2
                      let nudgePreviewElement: React.ReactNode = null;
                      if (nudgenikEnabled && nudgeEmoji) {
                        // Show state with summary if available, or just state emoji and name if not
                        nudgePreviewElement = nudgeSummary
                          ? `${nudgeEmoji} ${nudgeSummary}`
                          : `${nudgeEmoji} ${sess.nudge_state}`;
                      } else if (nudgenikEnabled && isPromptable && sess.running) {
                        nudgePreviewElement = (
                          <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                            <WorkingSpinner />
                            <span>Working...</span>
                          </span>
                        );
                      }

                      return (
                        <div
                          key={sess.id}
                          className={`nav-session${isActive ? ' nav-session--active' : ''}`}
                          onClick={() => handleSessionClick(sess.id)}
                        >
                          <div className="nav-session__row1">
                            <span className="nav-session__name">{sess.nickname || sess.target}</span>
                            <span className="nav-session__activity">{activityDisplay}</span>
                          </div>
                          <div className="nav-session__row2">
                            {nudgePreviewElement || '\u00A0'}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </div>

          <div className="nav-links">
            <NavLink
              to="/tips"
              className={({ isActive }) => `nav-link${isActive ? ' nav-link--active' : ''}${isNotConfigured ? ' nav-link--disabled' : ''}`}
            >
              <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10"></circle>
                <line x1="12" y1="16" x2="12" y2="12"></line>
                <line x1="12" y1="8" x2="12.01" y2="8"></line>
              </svg>
              <span>Tips</span>
            </NavLink>
            <NavLink
              to="/config"
              className={({ isActive }) => `nav-link${isActive ? ' nav-link--active' : ''}`}
            >
              <svg className="nav-link__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"/>
                <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1Z"/>
              </svg>
              <span>Config</span>
            </NavLink>
          </div>
        </div>

        <div className="nav-bottom">
          <div className="nav-bottom__version">
            {versionInfo?.version ? (versionInfo.version === 'dev' ? 'Version dev' : `Version ${versionInfo.version}`) : 'Loading...'}
          </div>
          <div className="nav-bottom__actions">
            <div className={`connection-pill connection-pill--sm ${connected ? 'connection-pill--connected' : 'connection-pill--offline'}`}>
              <span className="connection-pill__dot"></span>
              <span>{connected ? 'Connected' : 'Offline'}</span>
            </div>
            <Tooltip content="Toggle theme">
              <button id="themeToggle" className="icon-btn icon-btn--sm" aria-label="Toggle theme" onClick={toggleTheme}>
                <span className="icon-theme"></span>
              </button>
            </Tooltip>
            <Tooltip content="View on GitHub">
              <a href="https://github.com/sergeknystautas/schmux" target="_blank" rel="noopener noreferrer" className="icon-btn icon-btn--sm" aria-label="View on GitHub">
                <svg className="icon-github" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                  <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
                </svg>
              </a>
            </Tooltip>
          </div>
        </div>
      </nav>

      <main className="app-shell__content">
        <Outlet />
      </main>
    </div>
  );
}
