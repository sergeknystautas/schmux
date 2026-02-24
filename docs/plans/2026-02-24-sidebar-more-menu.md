# Sidebar More Menu Button Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Consolidate 5 navigation items (Overlays, Lore, Remote Hosts, Tips, Config) into a "More ↑" dropdown button at the bottom of the sidebar to reclaim vertical space.

**Architecture:** Create a new `MoreMenu` component that renders a button with an up-arrow icon. On click, it opens a dropdown portal (similar to SpawnDropdown pattern) positioned above the button. The dropdown contains the 5 nav items as clickable links with icons and badges. AppShell is refactored to remove the `.nav-links` section and place MoreMenu at the bottom after RemoteAccessPanel.

**Tech Stack:** React, React Router (NavLink), ReactDOM createPortal for dropdown, CSS modules/global CSS

---

### Task 1: Create MoreMenu Component

**Files:**

- Create: `assets/dashboard/src/components/MoreMenu.tsx`

**Step 1: Write the component scaffold**

```tsx
import { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { NavLink, useLocation } from 'react-router-dom';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';

export default function MoreMenu() {
  const [isOpen, setIsOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const location = useLocation();

  const { config, isNotConfigured } = useConfig();
  const { overlayUnreadCount, markOverlaysRead, totalLorePending } = useSessions();

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
        <span className="more-menu__toggle-text">More</span>
        <svg
          className={`more-menu__arrow${isOpen ? ' more-menu__arrow--open' : ''}`}
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        >
          <polyline points="18 15 12 9 6 15"></polyline>
        </svg>
      </button>
      {renderMenu()}
    </>
  );
}
```

**Step 2: Verify component compiles**

Run: `cd assets/dashboard && npm run build 2>&1 | head -20`
Expected: No TypeScript errors (may have unused variable warnings)

---

### Task 2: Add CSS Styles for MoreMenu

**Files:**

- Modify: `assets/dashboard/src/styles/global.css`

**Step 1: Add MoreMenu styles**

Add after the `.nav-link` styles (around line 800):

```css
/* MoreMenu component */
.more-menu__toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: var(--spacing-sm) var(--spacing-md);
  background: none;
  border: none;
  color: var(--color-text-muted);
  font-size: var(--font-size-sm);
  cursor: pointer;
  border-radius: var(--radius-sm);
  transition:
    background-color 0.15s ease,
    color 0.15s ease;
}

.more-menu__toggle:hover {
  background-color: var(--color-surface-alt);
  color: var(--color-text);
}

.more-menu__toggle-text {
  font-weight: 500;
}

.more-menu__arrow {
  transition: transform 0.15s ease;
}

.more-menu__arrow--open {
  transform: rotate(180deg);
}

.more-menu__dropdown {
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  overflow: hidden;
  z-index: 1000;
  margin-bottom: 4px;
}

.more-menu__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-md);
  padding: var(--spacing-sm) var(--spacing-md);
  color: var(--color-text-muted);
  text-decoration: none;
  transition:
    background-color 0.15s ease,
    color 0.15s ease;
  border-left: 3px solid transparent;
}

.more-menu__item:hover {
  background-color: var(--color-surface-alt);
  color: var(--color-text);
}

.more-menu__item--active {
  background-color: var(--color-accent-subtle);
  color: var(--color-accent);
  border-left-color: var(--color-accent);
}

.more-menu__item--disabled {
  opacity: 0.5;
  pointer-events: none;
}

.more-menu__item-icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

.more-menu__item-icon svg {
  width: 100%;
  height: 100%;
}

.more-menu__item-label {
  flex: 1;
  font-size: var(--font-size-sm);
}

.more-menu__badge {
  font-size: 0.65rem;
  padding: 1px 6px;
  border-radius: var(--radius-sm);
  background-color: var(--color-accent-subtle);
  color: var(--color-accent);
  font-weight: 600;
}

.more-menu__badge--danger {
  background-color: var(--color-error-subtle, rgba(239, 68, 68, 0.15));
  color: var(--color-error);
}
```

**Step 2: Verify styles compile**

Run: `cd assets/dashboard && npm run build 2>&1 | head -20`
Expected: Build succeeds

---

### Task 3: Write Tests for MoreMenu

**Files:**

- Create: `assets/dashboard/src/components/MoreMenu.test.tsx`

**Step 1: Write the test file**

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import MoreMenu from './MoreMenu';
import { ConfigProvider } from '../contexts/ConfigContext';
import { SessionsProvider } from '../contexts/SessionsContext';

// Mock WebSocket
const mockWebSocket = vi.fn(() => ({
  close: vi.fn(),
  send: vi.fn(),
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  readyState: 1,
}));
vi.stubGlobal('WebSocket', mockWebSocket);

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <MemoryRouter>
    <ConfigProvider>
      <SessionsProvider>{children}</SessionsProvider>
    </ConfigProvider>
  </MemoryRouter>
);

describe('MoreMenu', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the More button with up arrow', () => {
    render(<MoreMenu />, { wrapper });
    expect(screen.getByRole('button', { name: /more/i })).toBeInTheDocument();
  });

  it('opens dropdown on click', async () => {
    render(<MoreMenu />, { wrapper });
    const button = screen.getByRole('button', { name: /more/i });

    fireEvent.click(button);

    await waitFor(() => {
      expect(screen.getByRole('menu')).toBeInTheDocument();
    });
  });

  it('shows menu items when open', async () => {
    render(<MoreMenu />, { wrapper });
    const button = screen.getByRole('button', { name: /more/i });

    fireEvent.click(button);

    await waitFor(() => {
      expect(screen.getByRole('menuitem', { name: /remote hosts/i })).toBeInTheDocument();
      expect(screen.getByRole('menuitem', { name: /tips/i })).toBeInTheDocument();
      expect(screen.getByRole('menuitem', { name: /config/i })).toBeInTheDocument();
    });
  });

  it('closes dropdown when clicking outside', async () => {
    render(
      <>
        <div data-testid="outside">Outside</div>
        <MoreMenu />
      </>,
      { wrapper }
    );

    const button = screen.getByRole('button', { name: /more/i });
    fireEvent.click(button);

    await waitFor(() => {
      expect(screen.getByRole('menu')).toBeInTheDocument();
    });

    fireEvent.mouseDown(screen.getByTestId('outside'));

    await waitFor(() => {
      expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    });
  });

  it('closes dropdown on Escape key', async () => {
    render(<MoreMenu />, { wrapper });
    const button = screen.getByRole('button', { name: /more/i });

    fireEvent.click(button);

    await waitFor(() => {
      expect(screen.getByRole('menu')).toBeInTheDocument();
    });

    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => {
      expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    });
  });

  it('has correct aria attributes', () => {
    render(<MoreMenu />, { wrapper });
    const button = screen.getByRole('button', { name: /more/i });

    expect(button).toHaveAttribute('aria-haspopup', 'menu');
    expect(button).toHaveAttribute('aria-expanded', 'false');

    fireEvent.click(button);

    expect(button).toHaveAttribute('aria-expanded', 'true');
  });
});
```

**Step 2: Run tests to verify they pass**

Run: `cd assets/dashboard && npm test -- --run MoreMenu.test.tsx`
Expected: All tests pass

---

### Task 4: Integrate MoreMenu into AppShell

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx`

**Step 1: Add import for MoreMenu**

Add to imports section (around line 35):

```tsx
import MoreMenu from './MoreMenu';
```

**Step 2: Remove the .nav-links section and add MoreMenu**

Replace the entire `.nav-links` div (lines 856-958) with:

```tsx
          {isDevMode && <TypingPerformance />}
          <RemoteAccessPanel />
          <MoreMenu />
```

The order should now be:

1. TypingPerformance (dev mode only)
2. RemoteAccessPanel (if enabled)
3. MoreMenu (always visible at bottom)

**Step 3: Verify the app builds and runs**

Run: `./test.sh`
Expected: All tests pass

---

### Task 5: Handle Collapsed Sidebar State

**Files:**

- Modify: `assets/dashboard/src/components/MoreMenu.tsx`
- Modify: `assets/dashboard/src/styles/global.css`

**Step 1: Add navCollapsed prop to MoreMenu**

Update MoreMenu component to accept and handle collapsed state:

```tsx
type MoreMenuProps = {
  collapsed?: boolean;
};

export default function MoreMenu({ collapsed = false }: MoreMenuProps) {
  // ... existing code ...

  // Don't show menu when collapsed
  if (collapsed) {
    return null;
  }

  // ... rest of component
}
```

**Step 2: Pass collapsed prop from AppShell**

In AppShell.tsx, update the MoreMenu usage:

```tsx
<MoreMenu collapsed={navCollapsed} />
```

**Step 3: Add collapsed state CSS**

Add to global.css:

```css
.app-shell--collapsed .more-menu__toggle {
  display: none;
}
```

**Step 4: Verify tests still pass**

Run: `./test.sh`
Expected: All tests pass

---

### Task 6: Update Tests for Props and Edge Cases

**Files:**

- Modify: `assets/dashboard/src/components/MoreMenu.test.tsx`

**Step 1: Add tests for collapsed state**

```tsx
it('returns null when collapsed prop is true', () => {
  render(<MoreMenu collapsed />, { wrapper });
  expect(screen.queryByRole('button', { name: /more/i })).not.toBeInTheDocument();
});
```

**Step 2: Run tests**

Run: `cd assets/dashboard && npm test -- --run MoreMenu.test.tsx`
Expected: All tests pass

---

### Task 7: Final Verification and Commit

**Step 1: Run full test suite**

Run: `./test.sh`
Expected: All tests pass

**Step 2: Manual testing in dev mode**

Run: `./dev.sh`
Manual checks:

1. Sidebar shows More button at bottom
2. Click opens dropdown with all 5 items
3. Clicking item navigates and closes dropdown
4. Clicking outside closes dropdown
5. Escape key closes dropdown
6. Collapsed sidebar hides More button
7. RemoteAccessPanel shows above More button (if enabled)

**Step 3: Commit the changes**

Run: `/commit` with message describing the feature

---

## Summary

This plan creates a `MoreMenu` component that:

1. Replaces the bottom nav-links section (Overlays, Lore, Remote Hosts, Tips, Config)
2. Shows a "More" button with up arrow at the bottom of the sidebar
3. Opens a dropdown portal above the button on click
4. Closes on outside click, Escape key, or navigation
5. Hides when sidebar is collapsed
6. Preserves badges (Overlays unread, Lore pending)
7. Maintains active state highlighting
