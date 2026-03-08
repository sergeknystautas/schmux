import { useState, useEffect, useMemo } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { useConfig } from '../contexts/ConfigContext';
import { useCuration } from '../contexts/CurationContext';
import { useOverlay } from '../contexts/OverlayContext';
import { getLoreProposals } from '../lib/api';
import { getAllSpawnEntries } from '../lib/emergence-api';
import Tooltip from './Tooltip';

const TOOLS_COLLAPSED_KEY = 'schmux-tools-collapsed';

type ToolsSectionProps = {
  navCollapsed?: boolean;
  disableCollapse?: boolean;
};

export default function ToolsSection({
  navCollapsed = false,
  disableCollapse = false,
}: ToolsSectionProps) {
  const [isCollapsed, setIsCollapsed] = useState(() => {
    try {
      const stored = localStorage.getItem(TOOLS_COLLAPSED_KEY);
      return stored === 'true';
    } catch {
      return false;
    }
  });

  const location = useLocation();
  const { config, isNotConfigured } = useConfig();
  const { proposalVersion } = useCuration();
  const { overlayUnreadCount, markOverlaysRead } = useOverlay();

  // Persist collapsed state
  useEffect(() => {
    try {
      localStorage.setItem(TOOLS_COLLAPSED_KEY, String(isCollapsed));
    } catch {
      // ignore
    }
  }, [isCollapsed]);

  // Lore pending proposal + proposed action counts
  const [loreCounts, setLoreCounts] = useState<Record<string, number>>({});
  const repoNamesKey = useMemo(
    () => (config?.repos || []).map((r) => r.name).join(','),
    [config?.repos]
  );

  useEffect(() => {
    if (!repoNamesKey) return;
    const repoNames = repoNamesKey.split(',');
    const fetchCounts = async () => {
      const [proposalResults, actionResults] = await Promise.all([
        Promise.allSettled(repoNames.map((name) => getLoreProposals(name))),
        Promise.allSettled(repoNames.map((name) => getAllSpawnEntries(name))),
      ]);
      const counts: Record<string, number> = {};
      repoNames.forEach((name, i) => {
        let count = 0;
        const pr = proposalResults[i];
        if (pr.status === 'fulfilled') {
          count += (pr.value.proposals || []).filter((p) => p.status === 'pending').length;
        }
        const ar = actionResults[i];
        if (ar.status === 'fulfilled') {
          count += (ar.value || []).filter((e) => e.state === 'proposed').length;
        }
        counts[name] = count;
      });
      setLoreCounts(counts);
    };
    fetchCounts();
  }, [repoNamesKey, proposalVersion]);

  const totalLorePending = useMemo(
    () => Object.values(loreCounts).reduce((sum, n) => sum + n, 0),
    [loreCounts]
  );

  const menuItems = [
    {
      to: '/overlays',
      label: 'Overlays',
      badge: overlayUnreadCount > 0 ? overlayUnreadCount : null,
      badgeVariant: 'danger' as const,
      onClick: markOverlaysRead,
      hidden: !config?.repos?.length,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <polygon points="12 2 2 7 12 12 22 7 12 2"></polygon>
          <polyline points="2 17 12 22 22 17"></polyline>
          <polyline points="2 12 12 17 22 12"></polyline>
        </svg>
      ),
    },
    {
      to: '/lore',
      label: 'Lore',
      badge: totalLorePending > 0 ? totalLorePending : null,
      badgeVariant: 'default' as const,
      hidden: !config?.repos?.length,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"></path>
          <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"></path>
        </svg>
      ),
    },
    {
      to: '/personas',
      label: 'Personas',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
          <circle cx="12" cy="7" r="4"></circle>
        </svg>
      ),
    },
    {
      to: '/repofeed',
      label: 'Repofeed',
      hidden: !config?.repos?.length,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M4 11a9 9 0 0 1 9 9"></path>
          <path d="M4 4a16 16 0 0 1 16 16"></path>
          <circle cx="5" cy="19" r="1"></circle>
        </svg>
      ),
    },
    {
      to: '/settings/remote',
      label: 'Remote Hosts',
      hidden: !config?.remote_access?.enabled,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect>
          <line x1="8" y1="21" x2="16" y2="21"></line>
          <line x1="12" y1="17" x2="12" y2="21"></line>
        </svg>
      ),
    },
    {
      to: '/tips',
      label: 'Tips',
      disabled: isNotConfigured,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="10"></circle>
          <line x1="12" y1="16" x2="12" y2="12"></line>
          <line x1="12" y1="8" x2="12.01" y2="8"></line>
        </svg>
      ),
    },
    {
      to: '/config',
      label: 'Config',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" />
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1Z" />
        </svg>
      ),
    },
  ];

  const visibleItems = menuItems.filter((item) => !item.hidden);

  // Hide entirely when whole sidebar is collapsed
  if (navCollapsed) {
    return null;
  }

  const isItemActive = (to: string) => {
    return location.pathname === to || location.pathname.startsWith(to + '/');
  };

  const getBadgeLabel = (item: (typeof menuItems)[0]) => {
    if (item.badge === null || item.badge === undefined) return '';
    return ` (${item.badge})`;
  };

  return (
    <div className="tools-section" data-testid="tools-section">
      {isCollapsed || disableCollapse ? (
        /* Collapsed: chevron + horizontal icon row */
        <div className="tools-section__collapsed">
          {!disableCollapse && (
            <button
              className="tools-section__chevron"
              onClick={() => setIsCollapsed(false)}
              aria-label="Expand tools"
              data-testid="tools-expand-btn"
            >
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <polyline points="9 18 15 12 9 6"></polyline>
              </svg>
            </button>
          )}
          <div className="tools-section__icons">
            {visibleItems.map((item) => (
              <Tooltip key={item.to} content={`${item.label}${getBadgeLabel(item)}`}>
                <NavLink
                  to={item.to}
                  className={`tools-section__icon${isItemActive(item.to) ? ' tools-section__icon--active' : ''}${item.disabled ? ' tools-section__icon--disabled' : ''}`}
                  onClick={(e) => {
                    if (item.disabled) {
                      e.preventDefault();
                      return;
                    }
                    item.onClick?.();
                  }}
                  aria-label={item.label}
                  tabIndex={item.disabled ? -1 : 0}
                >
                  <span className="tools-section__icon-svg">{item.icon}</span>
                  {item.badge !== null && item.badge !== undefined && (
                    <span
                      className={`tools-section__icon-badge${item.badgeVariant === 'danger' ? ' tools-section__icon-badge--danger' : ''}`}
                      data-testid="icon-badge"
                      data-severity={item.badgeVariant === 'danger' ? 'danger' : 'warning'}
                    />
                  )}
                </NavLink>
              </Tooltip>
            ))}
          </div>
        </div>
      ) : (
        /* Expanded: section header + vertical list */
        <>
          {!disableCollapse && (
            <button
              className="tools-section__header"
              onClick={() => setIsCollapsed(true)}
              aria-label="Collapse tools"
              data-testid="tools-collapse-btn"
            >
              <svg
                className="tools-section__header-chevron"
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <polyline points="6 9 12 15 18 9"></polyline>
              </svg>
              <span className="tools-section__header-label">Tools</span>
            </button>
          )}
          <div className="tools-section__list">
            {visibleItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  `tools-section__item${isActive ? ' tools-section__item--active' : ''}${item.disabled ? ' tools-section__item--disabled' : ''}`
                }
                onClick={(e) => {
                  if (item.disabled) {
                    e.preventDefault();
                    return;
                  }
                  item.onClick?.();
                }}
                tabIndex={item.disabled ? -1 : 0}
              >
                <span className="tools-section__item-icon">{item.icon}</span>
                <span className="tools-section__item-label">{item.label}</span>
                {item.badge !== null && item.badge !== undefined && (
                  <span
                    className={`tools-section__item-badge${item.badgeVariant === 'danger' ? ' tools-section__item-badge--danger' : ''}`}
                  >
                    {item.badge}
                  </span>
                )}
              </NavLink>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
