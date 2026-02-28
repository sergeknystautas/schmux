import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createDemoTransport } from '../../website/src/demo/transport/mockTransport';
import {
  createDemoWorkspaces,
  createDemoConfig,
  createDemoDiff,
  createDemoGitGraph,
} from '../../website/src/demo/transport/mockData';

describe('createDemoTransport', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  describe('fetch', () => {
    it('returns demo config for /api/config', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/config');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data.workspace_path).toBe('/home/dev/workspaces');
      expect(data.repos.length).toBeGreaterThan(0);
    });

    it('returns config with model/tool run targets for spawn page', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/config');
      const data = await response.json();
      // SpawnPage filters run_targets for type === 'promptable' (models and detected tools)
      const promptable = (data.run_targets || []).filter((t: any) => t.type === 'promptable');
      expect(promptable.length).toBeGreaterThan(0);
    });

    it('returns demo config with remote_access disabled', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/config');
      const data = await response.json();
      expect(data.remote_access?.enabled).toBe(false);
    });

    it('returns an array for /api/remote/flavor-statuses', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/remote/flavor-statuses');
      const data = await response.json();
      expect(Array.isArray(data)).toBe(true);
    });

    it('returns workspaces for /api/sessions', async () => {
      const workspaces = createDemoWorkspaces();
      const dt = createDemoTransport({ workspaces, recordings: {} });

      const response = await dt.fetch('/api/sessions');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toHaveLength(workspaces.length);
      expect(data[0].sessions.length).toBeGreaterThan(0);
    });

    it('returns spawn result for /api/spawn', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/spawn');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data.sessions).toBeDefined();
    });

    it('returns empty array for /api/recent-branches', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/recent-branches?limit=10');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(Array.isArray(data)).toBe(true);
    });

    it('returns detect-tools with correct shape', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/detect-tools');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data.tools).toBeDefined();
      expect(Array.isArray(data.tools)).toBe(true);
    });

    it('returns personas with correct shape', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/personas');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data.personas).toBeDefined();
      expect(Array.isArray(data.personas)).toBe(true);
    });

    it('returns suggest-branch with correct shape', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/suggest-branch', {
        method: 'POST',
        body: JSON.stringify({ prompt: 'test' }),
      });
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data.branch).toBeDefined();
      expect(data.nickname).toBeDefined();
    });

    it('returns overlays object for /api/overlays', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/overlays');
      const data = await response.json();
      expect(data.overlays).toEqual([]);
    });

    it('returns prs object for /api/prs', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/prs');
      const data = await response.json();
      expect(data.prs).toEqual([]);
    });

    it('returns subreddit object for /api/subreddit', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/subreddit');
      const data = await response.json();
      expect(data.enabled).toBe(false);
    });

    it('returns lore proposals for /api/lore/', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/lore/acme%2Fwebapp/proposals');
      const data = await response.json();
      expect(data.proposals).toEqual([]);
    });

    it('returns diff with files for /api/diff/{workspaceId}', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/diff/demo-ws-1');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data.workspace_id).toBe('demo-ws-1');
      expect(data.files.length).toBeGreaterThan(0);
      // Should have both added and modified files
      const statuses = data.files.map((f: any) => f.status);
      expect(statuses).toContain('modified');
      expect(statuses).toContain('added');
      // Files should have content for the diff viewer
      const modified = data.files.find((f: any) => f.status === 'modified');
      expect(modified.old_content).toBeTruthy();
      expect(modified.new_content).toBeTruthy();
    });

    it('returns git graph for /api/workspaces/{id}/git-graph', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/workspaces/demo-ws-1/git-graph?max_total=50');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data.nodes.length).toBeGreaterThan(0);
      expect(data.branches).toBeDefined();
      expect(Object.keys(data.branches).length).toBeGreaterThan(0);
      // Should have commit nodes with hashes and messages
      expect(data.nodes[0].hash).toBeTruthy();
      expect(data.nodes[0].message).toBeTruthy();
    });

    it('returns 200 with empty object for unknown endpoints', async () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const response = await dt.fetch('/api/unknown');
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({});
    });
  });

  describe('createWebSocket (dashboard)', () => {
    it('creates a dashboard WebSocket that opens', () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const ws = dt.createWebSocket('ws://localhost/ws/dashboard');
      expect(ws).toBeDefined();

      const onopen = vi.fn();
      ws.onopen = onopen;

      // WebSocket opens after 50ms
      vi.advanceTimersByTime(50);
      expect(onopen).toHaveBeenCalled();
    });

    it('sends workspace data after connection opens', () => {
      const workspaces = createDemoWorkspaces();
      const dt = createDemoTransport({ workspaces, recordings: {} });

      const ws = dt.createWebSocket('ws://localhost/ws/dashboard');
      const onmessage = vi.fn();
      ws.onmessage = onmessage;

      // Open the WebSocket (50ms) then wait for data push (100ms more)
      vi.advanceTimersByTime(150);

      expect(onmessage).toHaveBeenCalled();
      const message = JSON.parse(onmessage.mock.calls[0][0].data);
      expect(message.type).toBe('sessions');
      expect(message.workspaces).toHaveLength(workspaces.length);
    });
  });

  describe('updateWorkspaces', () => {
    it('pushes updated workspaces to the dashboard WebSocket', () => {
      const dt = createDemoTransport({
        workspaces: createDemoWorkspaces(),
        recordings: {},
      });

      const ws = dt.createWebSocket('ws://localhost/ws/dashboard');
      ws.onopen = vi.fn();
      ws.onmessage = vi.fn();

      // Open the WebSocket
      vi.advanceTimersByTime(50);

      // Update workspaces
      const newWorkspaces = [createDemoWorkspaces()[0]];
      dt.updateWorkspaces(newWorkspaces);

      // First call is the initial state push at 100ms, but updateWorkspaces is immediate
      // The ws.onmessage was called synchronously by updateWorkspaces
      expect(ws.onmessage).toHaveBeenCalled();
      const lastCall = (ws.onmessage as any).mock.calls[
        (ws.onmessage as any).mock.calls.length - 1
      ];
      const message = JSON.parse(lastCall[0].data);
      expect(message.type).toBe('sessions');
      expect(message.workspaces).toHaveLength(1);
    });
  });
});

describe('createDemoWorkspaces', () => {
  it('has a session with Needs Input nudge state for tour highlight', () => {
    const workspaces = createDemoWorkspaces();
    const allSessions = workspaces.flatMap((ws) => ws.sessions);
    const needsInput = allSessions.find((s) => s.nudge_state === 'Needs Input');
    expect(needsInput).toBeDefined();
    expect(needsInput!.running).toBe(true);
  });

  it('has at least one session with Working nudge state for spinner display', () => {
    const workspaces = createDemoWorkspaces();
    const allSessions = workspaces.flatMap((ws) => ws.sessions);
    const working = allSessions.filter((s) => s.nudge_state === 'Working');
    expect(working.length).toBeGreaterThan(0);
  });

  it('stopped sessions do not have nudge_state Working', () => {
    const workspaces = createDemoWorkspaces();
    for (const ws of workspaces) {
      for (const sess of ws.sessions) {
        if (!sess.running) {
          expect(sess.nudge_state).not.toBe('Working');
        }
      }
    }
  });

  it('first workspace alphabetically has running sessions', () => {
    const workspaces = createDemoWorkspaces();
    const sorted = [...workspaces].sort((a, b) => {
      const repoA = a.repo_name || '';
      const repoB = b.repo_name || '';
      if (repoA !== repoB) return repoA.localeCompare(repoB);
      return a.branch.localeCompare(b.branch);
    });
    // First sorted workspace should have at least one running session
    // so that data-tour="sidebar-session" highlights a running session
    const firstWs = sorted[0];
    const hasRunning = firstWs.sessions.some((s) => s.running);
    expect(hasRunning).toBe(true);
  });
});

describe('createDemoDiff', () => {
  it('returns diff with matching workspace_id', () => {
    const diff = createDemoDiff('demo-ws-1');
    expect(diff.workspace_id).toBe('demo-ws-1');
  });

  it('includes files with old and new content', () => {
    const diff = createDemoDiff('demo-ws-1');
    expect(diff.files.length).toBeGreaterThan(0);
    const modified = diff.files.filter((f) => f.status === 'modified');
    expect(modified.length).toBeGreaterThan(0);
    for (const file of modified) {
      expect(file.old_content).toBeTruthy();
      expect(file.new_content).toBeTruthy();
    }
  });

  it('includes newly added files', () => {
    const diff = createDemoDiff('demo-ws-1');
    const added = diff.files.filter((f) => f.status === 'added');
    expect(added.length).toBeGreaterThan(0);
    for (const file of added) {
      expect(file.new_content).toBeTruthy();
    }
  });

  it('returns different files for different workspaces', () => {
    const diff1 = createDemoDiff('demo-ws-1');
    const diff2 = createDemoDiff('demo-ws-2');
    const paths1 = diff1.files.map((f) => f.new_path).sort();
    const paths2 = diff2.files.map((f) => f.new_path).sort();
    expect(paths1).not.toEqual(paths2);
  });

  it('file count matches workspace git_files_changed', () => {
    const workspaces = createDemoWorkspaces();
    for (const ws of workspaces) {
      const diff = createDemoDiff(ws.id);
      expect(diff.files.length).toBe(ws.git_files_changed);
    }
  });
});

describe('workspacesScenario tour steps', () => {
  it('diff-tab step navigates to the diff page', async () => {
    const { workspacesScenario } = await import('../../website/src/demo/scenarios/workspaces');
    const diffStep = workspacesScenario.steps.find((s) => s.target === '[data-tour="diff-tab"]');
    expect(diffStep).toBeDefined();
    expect(diffStep!.route).toMatch(/\/diff\//);
  });

  it('git-tab step navigates to the git graph page', async () => {
    const { workspacesScenario } = await import('../../website/src/demo/scenarios/workspaces');
    const gitStep = workspacesScenario.steps.find((s) => s.target === '[data-tour="git-tab"]');
    expect(gitStep).toBeDefined();
    expect(gitStep!.route).toMatch(/\/git\//);
  });

  it('session-detail-sidebar step expands collapsed sidebar via beforeStep', async () => {
    const { workspacesScenario } = await import('../../website/src/demo/scenarios/workspaces');
    const detailStep = workspacesScenario.steps.find(
      (s) => s.target === '[data-tour="session-detail-sidebar"]'
    );
    expect(detailStep).toBeDefined();
    expect(detailStep!.beforeStep).toBeDefined();

    // Simulate collapsed sidebar in localStorage
    localStorage.setItem('schmux:sessionSidebarCollapsed', JSON.stringify(true));
    detailStep!.beforeStep!();
    expect(JSON.parse(localStorage.getItem('schmux:sessionSidebarCollapsed')!)).toBe(false);
  });

  it('session-tabs step navigates to the needs-input session', async () => {
    const { workspacesScenario } = await import('../../website/src/demo/scenarios/workspaces');
    const tabsStep = workspacesScenario.steps.find(
      (s) => s.target === '[data-tour="session-tabs"]'
    );
    expect(tabsStep).toBeDefined();
    // Should switch to demo-sess-2 (the "Needs Input" session) to demonstrate tab switching
    expect(tabsStep!.route).toBe('/sessions/demo-sess-2');
  });
});

describe('computePosition', () => {
  it('clamps bottom-placement tooltip when target is near right edge', async () => {
    const { computePosition } = await import('../../website/src/demo/tour/Tooltip');
    // Simulate a target element near the right edge of a 1024px viewport
    Object.defineProperty(window, 'innerWidth', { value: 1024, writable: true });
    const rect = { left: 900, right: 960, top: 100, bottom: 120 } as DOMRect;
    const style = computePosition(rect, 'bottom');
    // Tooltip (320px wide) + 16px edge padding must fit within 1024px
    expect(style.left as number).toBeLessThanOrEqual(1024 - 320 - 16);
  });

  it('clamps top-placement tooltip when target is near right edge', async () => {
    const { computePosition } = await import('../../website/src/demo/tour/Tooltip');
    Object.defineProperty(window, 'innerWidth', { value: 1024, writable: true });
    Object.defineProperty(window, 'innerHeight', { value: 768, writable: true });
    const rect = { left: 900, right: 960, top: 200, bottom: 220 } as DOMRect;
    const style = computePosition(rect, 'top');
    expect(style.left as number).toBeLessThanOrEqual(1024 - 320 - 16);
  });

  it('does not clamp when target has room', async () => {
    const { computePosition } = await import('../../website/src/demo/tour/Tooltip');
    Object.defineProperty(window, 'innerWidth', { value: 1024, writable: true });
    const rect = { left: 100, right: 200, top: 100, bottom: 120 } as DOMRect;
    const style = computePosition(rect, 'bottom');
    expect(style.left).toBe(100);
  });
});

describe('createDemoGitGraph', () => {
  it('returns graph with nodes and branches', () => {
    const graph = createDemoGitGraph('demo-ws-1');
    expect(graph.nodes.length).toBeGreaterThan(0);
    expect(Object.keys(graph.branches).length).toBeGreaterThan(0);
  });

  it('has a main branch and a feature branch', () => {
    const graph = createDemoGitGraph('demo-ws-1');
    const branchNames = Object.keys(graph.branches);
    const hasMain = branchNames.some((b) => graph.branches[b].is_main);
    const hasFeature = branchNames.some((b) => !graph.branches[b].is_main);
    expect(hasMain).toBe(true);
    expect(hasFeature).toBe(true);
  });

  it('nodes have required fields', () => {
    const graph = createDemoGitGraph('demo-ws-1');
    for (const node of graph.nodes) {
      expect(node.hash).toBeTruthy();
      expect(node.short_hash).toBeTruthy();
      expect(node.message).toBeTruthy();
      expect(node.author).toBeTruthy();
      expect(node.timestamp).toBeTruthy();
      expect(Array.isArray(node.parents)).toBe(true);
    }
  });

  it('includes dirty state matching workspace changes', () => {
    const graph = createDemoGitGraph('demo-ws-1');
    expect(graph.dirty_state).toBeDefined();
    expect(graph.dirty_state!.files_changed).toBeGreaterThan(0);
    expect(graph.dirty_state!.lines_added).toBeGreaterThan(0);
  });

  it('tracks ahead count from main', () => {
    const graph = createDemoGitGraph('demo-ws-1');
    expect(graph.main_ahead_count).toBeGreaterThan(0);
  });

  it('returns different commit messages for different workspaces', () => {
    const graph1 = createDemoGitGraph('demo-ws-1');
    const graph2 = createDemoGitGraph('demo-ws-2');
    const msgs1 = graph1.nodes.filter((n) => !n.branches?.includes('main')).map((n) => n.message);
    const msgs2 = graph2.nodes.filter((n) => !n.branches?.includes('main')).map((n) => n.message);
    expect(msgs1).not.toEqual(msgs2);
  });
});
