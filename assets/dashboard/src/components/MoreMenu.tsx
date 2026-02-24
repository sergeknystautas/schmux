import { useState, useRef, useEffect, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { NavLink, useLocation } from 'react-router-dom';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { getLoreProposals } from '../lib/api';

type MoreMenuProps = {
  collapsed?: boolean;
};

export default function MoreMenu({ collapsed = false }: MoreMenuProps) {
  const [isOpen, setIsOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const location = useLocation();

  const { config, isNotConfigured } = useConfig();
  const { overlayUnreadCount, markOverlaysRead } = useSessions();

  // Lore pending proposal counts (computed locally like AppShell does)
  const [loreCounts, setLoreCounts] = useState<Record<string, number>>({});
  const repoNamesKey = useMemo(
    () => (config?.repos || []).map((r) => r.name).join(','),
    [config?.repos]
  );

  useEffect(() => {
    if (!repoNamesKey) return;
    const repoNames = repoNamesKey.split(',');
    const fetchCounts = async () => {
      const results = await Promise.allSettled(repoNames.map((name) => getLoreProposals(name)));
      const counts: Record<string, number> = {};
      results.forEach((result, i) => {
        if (result.status === 'fulfilled') {
          counts[repoNames[i]] = (result.value.proposals || []).filter(
            (p) => p.status === 'pending'
          ).length;
        }
      });
      setLoreCounts(counts);
    };
    fetchCounts();
  }, [repoNamesKey]);

  const totalLorePending = useMemo(
    () => Object.values(loreCounts).reduce((sum, n) => sum + n, 0),
    [loreCounts]
  );

  // Close on outside click
  useEffect(() => {
    if (!isOpen) return;

    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (buttonRef.current?.contains(target)) return;
      if (menuRef.current?.contains(target)) return;
      setIsOpen(false);
    };

    document.addEventListener('click', handleClickOutside, true);
    return () => document.removeEventListener('click', handleClickOutside, true);
  }, [isOpen]);

  // Close on Escape key
  useEffect(() => {
    if (!isOpen) return;

    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsOpen(false);
    };

    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen]);

  // Close on route change
  useEffect(() => {
    setIsOpen(false);
  }, [location.pathname]);

  const handleToggle = () => setIsOpen(!isOpen);

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
      to: '/settings/remote',
      label: 'Remote Hosts',
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

  // Don't show menu when collapsed
  if (collapsed) {
    return null;
  }

  const renderMenu = () => {
    if (!isOpen || !buttonRef.current) return null;

    const rect = buttonRef.current.getBoundingClientRect();
    const gap = 4;

    return createPortal(
      <div
        ref={menuRef}
        className="more-menu__dropdown"
        role="menu"
        style={{
          position: 'fixed',
          bottom: `calc(100vh - ${rect.top - gap}px)`,
          left: rect.left,
          width: rect.width,
        }}
      >
        {visibleItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            className={({ isActive }) =>
              `more-menu__item${isActive ? ' more-menu__item--active' : ''}${item.disabled ? ' more-menu__item--disabled' : ''}`
            }
            onClick={(e) => {
              if (item.disabled) {
                e.preventDefault();
                return;
              }
              item.onClick?.();
              setIsOpen(false);
            }}
            role="menuitem"
            tabIndex={item.disabled ? -1 : 0}
          >
            <span className="more-menu__item-icon">{item.icon}</span>
            <span className="more-menu__item-label">{item.label}</span>
            {item.badge !== null && item.badge !== undefined && (
              <span
                className={`more-menu__badge${item.badgeVariant === 'danger' ? ' more-menu__badge--danger' : ''}`}
              >
                {item.badge}
              </span>
            )}
          </NavLink>
        ))}
      </div>,
      document.body
    );
  };

  return (
    <>
      <button
        ref={buttonRef}
        className="more-menu__toggle"
        onClick={handleToggle}
        aria-expanded={isOpen}
        aria-haspopup="menu"
      >
        <svg
          className={`more-menu__arrow${isOpen ? ' more-menu__arrow--open' : ''}`}
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        >
          <polyline points="18 15 12 9 6 15"></polyline>
        </svg>
        <span className="more-menu__toggle-text">More</span>
      </button>
      {renderMenu()}
    </>
  );
}
