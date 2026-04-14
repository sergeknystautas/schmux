import { useState, useEffect, useMemo } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { useConfig } from '../contexts/ConfigContext';
import { useCuration } from '../contexts/CurationContext';
import { useOverlay } from '../contexts/OverlayContext';
import { useFeatures } from '../contexts/FeaturesContext';
import { getAutolearnBatches } from '../lib/api';
import { getAllSpawnEntries } from '../lib/spawn-api';
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
  const { config } = useConfig();
  const { proposalVersion } = useCuration();
  const { overlayUnreadCount, markOverlaysRead } = useOverlay();
  const { features } = useFeatures();

  // Persist collapsed state
  useEffect(() => {
    try {
      localStorage.setItem(TOOLS_COLLAPSED_KEY, String(isCollapsed));
    } catch {
      // ignore
    }
  }, [isCollapsed]);

  // Autolearn pending proposal + proposed action counts
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
        Promise.allSettled(repoNames.map((name) => getAutolearnBatches(name))),
        Promise.allSettled(repoNames.map((name) => getAllSpawnEntries(name))),
      ]);
      const counts: Record<string, number> = {};
      repoNames.forEach((name, i) => {
        let count = 0;
        const pr = proposalResults[i];
        if (pr.status === 'fulfilled') {
          for (const p of pr.value.batches || []) {
            if (p.status === 'pending' || p.status === 'merging') {
              count += (p.learnings || []).filter((r: any) => r.status === 'pending').length;
            }
          }
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
      to: '/autolearn',
      label: 'Autolearn',
      badge: totalLorePending > 0 ? totalLorePending : null,
      badgeVariant: 'default' as const,
      hidden: !config?.lore?.enabled,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z"></path>
          <path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z"></path>
          <path d="M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4"></path>
          <path d="M17.599 6.5a3 3 0 0 0 .399-1.375"></path>
          <path d="M6.003 5.125A3 3 0 0 0 6.401 6.5"></path>
          <path d="M3.477 10.896a4 4 0 0 1 .585-.396"></path>
          <path d="M19.938 10.5a4 4 0 0 1 .585.396"></path>
          <path d="M6 18a4 4 0 0 1-1.967-.516"></path>
          <path d="M19.967 17.484A4 4 0 0 1 18 18"></path>
        </svg>
      ),
    },
    {
      to: '/personas',
      label: 'Personas',
      hidden: !config?.personas_enabled,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
          <circle cx="12" cy="7" r="4"></circle>
        </svg>
      ),
    },
    {
      to: '/styles',
      label: 'Comm Styles',
      hidden: !config?.comm_styles_enabled,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="m3 11 18-5v12L3 13v-2z"></path>
          <path d="M11.6 16.8a3 3 0 1 1-5.8-1.6"></path>
        </svg>
      ),
    },
    {
      to: '/repofeed',
      label: 'Repofeed',
      hidden: !features.repofeed || !config?.repofeed?.enabled,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M4.9 16.1C1 12.2 1 5.8 4.9 1.9"></path>
          <path d="M7.8 4.7a6.14 6.14 0 0 0-.8 7.5"></path>
          <circle cx="12" cy="9" r="2"></circle>
          <path d="M16.2 4.7a6.14 6.14 0 0 1 .8 7.5"></path>
          <path d="M19.1 1.9a10.14 10.14 0 0 1 0 14.2"></path>
          <path d="M9.5 18h5"></path>
          <path d="m8 22 4-11 4 11"></path>
        </svg>
      ),
    },
    {
      to: '/timelapse',
      label: 'Timelapse',
      hidden: !config?.timelapse?.enabled,
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <rect x="2" y="3" width="20" height="14" rx="2"></rect>
          <path d="m10.5 7.5 4 2.5-4 2.5v-5z"></path>
          <line x1="8" y1="21" x2="16" y2="21"></line>
          <line x1="12" y1="17" x2="12" y2="21"></line>
        </svg>
      ),
    },
    {
      to: '/environment',
      label: 'Environment',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="m16 3 4 4-4 4"></path>
          <path d="M20 7H4"></path>
          <path d="m8 21-4-4 4-4"></path>
          <path d="M4 17h16"></path>
        </svg>
      ),
    },
    {
      to: '/tips',
      label: 'Tips',
      icon: (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"></path>
          <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"></path>
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
                  className={`tools-section__icon${isItemActive(item.to) ? ' tools-section__icon--active' : ''}`}
                  onClick={() => {
                    item.onClick?.();
                  }}
                  aria-label={item.label}
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
                  `tools-section__item${isActive ? ' tools-section__item--active' : ''}`
                }
                onClick={() => {
                  item.onClick?.();
                }}
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
