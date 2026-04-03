import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import SessionTabs from './SessionTabs';
import type { SessionResponse, WorkspaceResponse } from '../lib/types';

// ---- Context mocks ----

vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

vi.mock('./ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn().mockResolvedValue(null) }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    waitForSession: vi.fn().mockResolvedValue(undefined),
    ackSession: vi.fn(),
  }),
}));

vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: {} }),
}));

// Controlled per-test via mockSyncState variable
let mockWorkspaceLockStates: Record<string, { locked: boolean }> = {};
vi.mock('../contexts/SyncContext', () => ({
  useSyncState: () => ({
    linearSyncResolveConflictStates: {},
    clearLinearSyncResolveConflictState: vi.fn(),
    workspaceLockStates: mockWorkspaceLockStates,
    syncResultEvents: [],
    clearSyncResultEvents: vi.fn(),
  }),
}));

vi.mock('../contexts/KeyboardContext', () => ({
  useKeyboardMode: () => ({
    setContext: vi.fn(),
    clearContext: vi.fn(),
    registerAction: vi.fn(),
    unregisterAction: vi.fn(),
  }),
}));

// ---- Factories ----

function makeSession(id: string, overrides: Partial<SessionResponse> = {}): SessionResponse {
  return {
    id,
    target: `target-${id}`,
    branch: 'main',
    created_at: '2026-01-01T00:00:00Z',
    running: true,
    attach_cmd: `tmux attach -t ${id}`,
    ...overrides,
  };
}

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
    repo: 'git@github.com:test/repo.git',
    repo_name: 'test-repo',
    branch: 'main',
    path: '/tmp/ws',
    session_count: 0,
    sessions: [],
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    ...overrides,
  };
}

// ---- matchMedia helpers ----

function mockMatchMediaDesktop() {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

function mockMatchMediaMobile() {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

// Restore the setupTests stub after each test that overrides it
const originalMatchMedia = window.matchMedia;
afterEach(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: originalMatchMedia,
  });
  localStorage.clear();
});

// ---- Render helper ----

async function renderTabs(
  sessions: SessionResponse[],
  workspace?: WorkspaceResponse,
  extraProps: Partial<Parameters<typeof SessionTabs>[0]> = {}
) {
  await act(async () => {
    render(
      <MemoryRouter>
        <SessionTabs sessions={sessions} workspace={workspace} {...extraProps} />
      </MemoryRouter>
    );
  });
}

// ---- Tests ----

describe('SessionTabs', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    mockWorkspaceLockStates = {};
  });

  describe('DndContext — desktop + unlocked + workspace present', () => {
    it('renders sortable items with aria-roledescription when desktop, unlocked, workspace present', async () => {
      mockMatchMediaDesktop();
      const sessions = [makeSession('s1'), makeSession('s2')];
      const workspace = makeWorkspace({ sessions });

      await renderTabs(sessions, workspace);

      // dnd-kit adds aria-roledescription="sortable" to SortableContext items
      const sortableItems = screen.getAllByRole('button', {
        // The SortableSessionTab sets role="button" and dnd-kit adds aria-roledescription
      });

      // At least the session tabs should be present
      expect(sortableItems.length).toBeGreaterThan(0);

      // dnd-kit adds aria-roledescription="sortable" to useSortable items
      const sortableElements = document.querySelectorAll('[aria-roledescription="sortable"]');
      expect(sortableElements.length).toBe(2);
    });
  });

  describe('DndContext — NOT rendered when isLocked', () => {
    it('does not render sortable attributes when workspace is locked', async () => {
      mockMatchMediaDesktop();

      // Set the lock state for workspace ws-1 via the mutable variable read by the mock
      mockWorkspaceLockStates = { 'ws-1': { locked: true } };

      const sessions = [makeSession('s1'), makeSession('s2')];
      const workspace = makeWorkspace({ sessions });

      await renderTabs(sessions, workspace);

      const sortableElements = document.querySelectorAll('[aria-roledescription="sortable"]');
      expect(sortableElements.length).toBe(0);
    });
  });

  describe('DndContext — NOT rendered on mobile viewport', () => {
    it('does not render sortable attributes when matchMedia returns mobile (matches: false)', async () => {
      // setupTests.ts provides a matchMedia mock where matches: false by default
      // so no override needed here — just use the default mobile stub
      mockMatchMediaMobile();

      const sessions = [makeSession('s1'), makeSession('s2')];
      const workspace = makeWorkspace({ sessions });

      await renderTabs(sessions, workspace);

      const sortableElements = document.querySelectorAll('[aria-roledescription="sortable"]');
      expect(sortableElements.length).toBe(0);
    });
  });

  describe('DndContext — NOT rendered without workspace', () => {
    it('does not render sortable attributes when no workspace prop is provided', async () => {
      mockMatchMediaDesktop();

      const sessions = [makeSession('s1'), makeSession('s2')];

      // No workspace passed
      await renderTabs(sessions, undefined);

      const sortableElements = document.querySelectorAll('[aria-roledescription="sortable"]');
      expect(sortableElements.length).toBe(0);
    });
  });

  describe('Non-session elements are outside SortableContext', () => {
    it('Spawning tab (activeSpawnTab) does not have sortable attributes', async () => {
      mockMatchMediaDesktop();
      const sessions = [makeSession('s1')];
      const workspace = makeWorkspace({ sessions });

      await renderTabs(sessions, workspace, { activeSpawnTab: true });

      // The Spawning... tab should be rendered
      const spawningTab = screen.getByText('Spawning...');
      expect(spawningTab).toBeInTheDocument();

      // Its parent element should NOT have aria-roledescription="sortable"
      const spawningContainer = spawningTab.closest('[aria-roledescription="sortable"]');
      expect(spawningContainer).toBeNull();
    });

    it('Add button does not have sortable attributes', async () => {
      mockMatchMediaDesktop();
      const sessions = [makeSession('s1')];
      const workspace = makeWorkspace({ sessions });

      await renderTabs(sessions, workspace);

      const addButton = screen.getByLabelText('Spawn new session');
      expect(addButton).toBeInTheDocument();

      // The add button itself should NOT have aria-roledescription="sortable"
      expect(addButton.getAttribute('aria-roledescription')).not.toBe('sortable');

      // Nor any ancestor
      const sortableAncestor = addButton.closest('[aria-roledescription="sortable"]');
      expect(sortableAncestor).toBeNull();
    });
  });

  describe('xterm title display', () => {
    it('shows xterm_title when no nickname is set', async () => {
      mockMatchMediaMobile();
      const sessions = [makeSession('s1', { xterm_title: 'claude > Working' })];
      await renderTabs(sessions);

      expect(screen.getByText('claude > Working')).toBeInTheDocument();
    });

    it('prefers nickname over xterm_title', async () => {
      mockMatchMediaMobile();
      const sessions = [
        makeSession('s1', { nickname: 'my-agent', xterm_title: 'claude > Working' }),
      ];
      await renderTabs(sessions);

      expect(screen.getByText('my-agent')).toBeInTheDocument();
      expect(screen.queryByText('claude > Working')).not.toBeInTheDocument();
    });

    it('falls back to target when neither nickname nor xterm_title is set', async () => {
      mockMatchMediaMobile();
      const sessions = [makeSession('s1')];
      await renderTabs(sessions);

      expect(screen.getByText('target-s1')).toBeInTheDocument();
    });

    it('shows xterm_title in sortable tabs (desktop)', async () => {
      mockMatchMediaDesktop();
      const sessions = [makeSession('s1', { xterm_title: 'agent status' })];
      const workspace = makeWorkspace({ sessions });
      await renderTabs(sessions, workspace);

      expect(screen.getByText('agent status')).toBeInTheDocument();
    });
  });

  describe('resolve-conflict tab badges', () => {
    it('shows in-progress spinner from workspace resolve_conflicts state', async () => {
      mockMatchMediaMobile();
      const workspace = makeWorkspace({
        tabs: [
          {
            id: 'sys-resolve-conflict-abcdef1',
            kind: 'resolve-conflict',
            label: 'Conflict abcdef1',
            route: '/resolve-conflict/ws-1/sys-resolve-conflict-abcdef1',
            closable: false,
            meta: { hash: 'abcdef1' },
            created_at: new Date().toISOString(),
          },
        ],
        resolve_conflicts: [
          {
            type: 'linear_sync_resolve_conflict',
            workspace_id: 'ws-1',
            status: 'in_progress',
            hash: 'abcdef1',
            started_at: new Date().toISOString(),
            steps: [],
          },
        ],
      });

      await renderTabs([], workspace);

      expect(document.querySelector('.spinner.spinner--small')).not.toBeNull();
    });
  });

  describe('Tab order from localStorage', () => {
    it('renders session tabs in custom order from localStorage', async () => {
      mockMatchMediaDesktop();
      // Store order: c, b, a
      localStorage.setItem('schmux:tab-order:ws-1', JSON.stringify(['c', 'b', 'a']));

      const sessionA = makeSession('a', { nickname: 'Alpha' });
      const sessionB = makeSession('b', { nickname: 'Bravo' });
      const sessionC = makeSession('c', { nickname: 'Charlie' });
      const sessions = [sessionA, sessionB, sessionC];
      const workspace = makeWorkspace({ id: 'ws-1', sessions });

      await renderTabs(sessions, workspace);

      const tabs = document.querySelectorAll('[aria-roledescription="sortable"]');
      expect(tabs).toHaveLength(3);

      // Verify order: Charlie (c), Bravo (b), Alpha (a)
      const tabNames = Array.from(tabs).map((tab) => {
        const nameEl = tab.querySelector('.session-tab__name');
        return nameEl?.textContent ?? '';
      });
      expect(tabNames).toEqual(['Charlie', 'Bravo', 'Alpha']);
    });
  });
});
