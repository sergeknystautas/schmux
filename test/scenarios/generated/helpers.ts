import { type Page, expect } from '@playwright/test';
import WS from 'ws';

// Read at call time (not module load) so fixture-set env vars are picked up.
function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}
const SCHMUX_BIN = process.env.SCHMUX_BIN || 'schmux';

// --- Config helpers ---

interface RepoConfig {
  name: string;
  url: string;
  vcs?: string;
}

interface SetupOptions {
  repos?: string[];
  repoConfigs?: RepoConfig[];
  agents?: Array<{ name: string; command: string; promptable?: boolean }>;
  quickLaunch?: Array<{
    name: string;
    target?: string;
    command?: string;
    prompt?: string;
    fence?: boolean;
    kind?: string;
  }>;
  workspacePath?: string;
  scm?: 'git' | 'git-worktree';
  saplingCommands?: {
    create_workspace?: string[];
    remove_workspace?: string[];
    create_repo_base?: string[];
    check_repo_base?: string[];
  };
  xterm?: Record<string, unknown>;
}

/**
 * Seeds ~/.schmux/config.json with test repos and agents.
 * Mirrors internal/e2e/e2e.go CreateConfig (line 218).
 */
export async function seedConfig(opts: SetupOptions = {}): Promise<void> {
  // Dispose stale sessions from previous specs — they reference the old config
  await disposeAllSessions();

  const config: Record<string, unknown> = {
    ...(opts.workspacePath ? { workspace_path: opts.workspacePath } : {}),
    source_code_management: opts.scm || 'git',
    repos: [
      ...(opts.repos || []).map((r) => ({
        name: r.split('/').pop() || r,
        url: r,
      })),
      ...(opts.repoConfigs || []),
    ],
    ...(opts.saplingCommands ? { sapling_commands: opts.saplingCommands } : {}),
    ...(opts.xterm ? { xterm: opts.xterm } : {}),
    run_targets: (opts.agents || []).map((a) => ({
      name: a.name,
      command: a.command,
    })),
    quick_launch: (opts.quickLaunch || []).map((ql) => ({
      name: ql.name,
      ...(ql.target ? { target: ql.target } : {}),
      ...(ql.command ? { command: ql.command } : {}),
      ...(ql.prompt ? { prompt: ql.prompt } : {}),
      ...(ql.fence ? { fence: true } : {}),
      ...(ql.kind ? { kind: ql.kind } : {}),
    })),
  };

  const res = await fetch(`${getBaseURL()}/api/config`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });

  if (!res.ok) {
    throw new Error(`Failed to seed config: ${res.status} ${await res.text()}`);
  }
}

// --- API client helpers ---

export async function apiGet<T = unknown>(path: string): Promise<T> {
  const res = await fetch(`${getBaseURL()}${path}`);
  if (!res.ok) {
    throw new Error(`GET ${path} failed: ${res.status} ${await res.text()}`);
  }
  return res.json() as Promise<T>;
}

export async function apiPost<T = unknown>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${getBaseURL()}${path}`, {
    method: 'POST',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw new Error(`POST ${path} failed: ${res.status} ${await res.text()}`);
  }
  return res.json() as Promise<T>;
}

export async function apiPatch<T = unknown>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${getBaseURL()}${path}`, {
    method: 'PATCH',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw new Error(`PATCH ${path} failed: ${res.status} ${await res.text()}`);
  }
  return res.json() as Promise<T>;
}

// --- Health check ---

/**
 * The one centralized daemon-startup probe (rubric rule 6): daemon
 * readiness is an opaque external-process boundary. On timeout it
 * reports the last HTTP status/error and the daemon log the worker
 * fixture streams (see fixtures.ts).
 */
export async function waitForHealthy(timeoutMs: number = 15_000, url?: string): Promise<void> {
  const base = url ?? getBaseURL();
  const start = Date.now();
  let attempts = 0;
  let lastStatus: number | null = null;
  let lastError: string | null = null;
  while (Date.now() - start < timeoutMs) {
    attempts++;
    try {
      const res = await fetch(`${base}/api/healthz`);
      lastStatus = res.status;
      if (res.ok) return;
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    }
    await sleep(200);
  }
  const logPath = `${process.env.HOME ?? ''}/.schmux/daemon.log`;
  const details = [
    `url: ${base}/api/healthz`,
    `attempts: ${attempts}`,
    `last HTTP status: ${lastStatus ?? 'none'}`,
    `last error: ${lastError ?? 'none'}`,
    `daemon log: ${logPath}`,
  ];
  try {
    const { readFileSync } = await import('fs');
    const lines = readFileSync(logPath, 'utf-8').split('\n');
    const tail = lines.slice(-31).join('\n').trim();
    if (tail) details.push(`daemon log tail:\n${tail}`);
  } catch {
    // Log absent or unreadable — the path above still points at it.
  }
  throw new Error(`Daemon not healthy after ${timeoutMs}ms — ${details.join('; ')}`);
}

// --- Session helpers ---

interface SpawnRequest {
  repo: string;
  branch?: string;
  prompt?: string;
  nickname?: string;
  targets: Record<string, number>;
  workspace_id?: string;
}

interface SpawnResult {
  session_id: string;
  workspace_id: string;
  error?: string;
}

export async function spawnSession(req: SpawnRequest): Promise<SpawnResult[]> {
  return apiPost<SpawnResult[]>('/api/spawn', req);
}

interface WorkspaceItem {
  id: string;
  repo: string;
  branch: string;
  sessions: Array<{
    id: string;
    nickname: string;
    target: string;
    running: boolean;
    status?: string;
  }>;
}

export async function getSessions(): Promise<WorkspaceItem[]> {
  return apiGet<WorkspaceItem[]>('/api/sessions');
}

export async function disposeSession(sessionId: string): Promise<void> {
  await apiPost(`/api/sessions/${sessionId}/dispose`);
}

/**
 * Dispose ALL running sessions across all workspaces.
 * Useful for cleanup between repeated test runs to prevent session accumulation.
 */
export async function disposeAllSessions(): Promise<void> {
  try {
    const workspaces = await apiGet<WorkspaceItem[]>('/api/sessions');
    for (const ws of workspaces) {
      for (const sess of ws.sessions) {
        if (sess.running) {
          try {
            await disposeSession(sess.id);
          } catch {
            // Ignore individual dispose failures
          }
        }
      }
    }
  } catch {
    // Ignore if API is not ready
  }
}

// --- WebSocket helpers ---

export async function waitForTerminalOutput(
  sessionId: string,
  substring: string,
  timeoutMs: number = 10_000
): Promise<string> {
  return new Promise((resolve, reject) => {
    const ws = new WS(`${getBaseURL().replace(/^http/, 'ws')}/ws/terminal/${sessionId}`);
    let buffer = '';
    const timer = setTimeout(() => {
      ws.close();
      reject(
        new Error(
          `Terminal output did not contain "${substring}" after ${timeoutMs}ms. Buffer: ${buffer.slice(0, 500)}`
        )
      );
    }, timeoutMs);

    ws.on('message', (data: WS.Data, isBinary: boolean) => {
      if (isBinary) {
        // Binary frame: 8-byte sequence header + terminal bytes
        const buf = Buffer.isBuffer(data) ? data : Buffer.from(data as ArrayBuffer);
        buffer += buf.subarray(8).toString('utf-8');
      } else {
        // Text frame: JSON control message (legacy/fallback)
        try {
          const msg = JSON.parse(data.toString());
          if (msg.content) buffer += msg.content;
        } catch {
          // Non-JSON text, append as-is
          buffer += data.toString();
        }
      }
      if (buffer.includes(substring)) {
        clearTimeout(timer);
        ws.close();
        resolve(buffer);
      }
    });

    ws.on('error', (err: Error) => {
      clearTimeout(timer);
      reject(new Error(`WebSocket error: ${err.message}`));
    });
  });
}

// --- Git repo helpers ---

/**
 * Creates a local bare git repo for testing.
 * Returns the repo path.
 */
export async function createTestRepo(name: string): Promise<string> {
  if (!/^[a-zA-Z0-9_-]+$/.test(name)) {
    throw new Error(`Invalid repo name: ${name}`);
  }
  const { execSync } = await import('child_process');
  const repoDir = `${process.env.SCHMUX_REPO_DIR || '/tmp/schmux-test-repos'}/${name}`;
  execSync(`rm -rf ${repoDir} && mkdir -p ${repoDir}`);
  execSync(`git init -b main ${repoDir}`);
  execSync(`git -C ${repoDir} config user.email "test@schmux.dev"`);
  execSync(`git -C ${repoDir} config user.name "Schmux Test"`);
  execSync(`touch ${repoDir}/README.md`);
  execSync(`git -C ${repoDir} add .`);
  execSync(`git -C ${repoDir} commit -m "initial"`);
  return repoDir;
}

/**
 * Creates a local sapling repo for scenario tests.
 * Requires `sl` (sapling) to be installed in the test environment.
 * Returns the repo directory path.
 */
export async function createSaplingTestRepo(name: string): Promise<string> {
  if (!/^[a-zA-Z0-9_-]+$/.test(name)) {
    throw new Error(`Invalid repo name: ${name}`);
  }
  const { execSync } = await import('child_process');

  // Check if sapling is available
  try {
    execSync('sl version', { stdio: 'pipe' });
  } catch {
    throw new Error('Sapling (sl) is not installed — cannot create sapling test repo');
  }

  const repoDir = `${process.env.SCHMUX_REPO_DIR || '/tmp/schmux-test-repos'}/${name}`;
  execSync(`rm -rf ${repoDir} && mkdir -p ${repoDir}`);
  execSync(`sl init ${repoDir}`);
  execSync(`sl --cwd ${repoDir} config --local ui.username "Schmux Test <test@schmux.dev>"`);
  execSync(`touch ${repoDir}/README.md`);
  execSync(`sl --cwd ${repoDir} add README.md`);
  execSync(`sl --cwd ${repoDir} commit -m "initial"`);
  return repoDir;
}

// --- Tunnel simulation helpers ---

interface SimulateTunnelResult {
  url: string;
  token: string;
}

/**
 * Activates a simulated cloudflared tunnel via the dev-mode API.
 * Returns the tunnel URL and one-time auth token.
 * Requires the daemon to be running with --dev-mode (scenario entrypoint does this).
 */
export async function simulateTunnel(): Promise<SimulateTunnelResult> {
  return apiPost<SimulateTunnelResult>('/api/dev/simulate-tunnel');
}

/**
 * Stops the simulated tunnel and clears all remote auth state.
 */
export async function stopSimulatedTunnel(): Promise<void> {
  await apiPost('/api/dev/simulate-tunnel-stop');
}

// --- Session wait helpers ---

interface DashboardSessionsMessage {
  type: string;
  workspaces?: WorkspaceItem[];
}

function sessionsConditionMet(workspaces: WorkspaceItem[], sessionId?: string): boolean {
  const allSessions = workspaces.flatMap((ws) => ws.sessions);
  if (sessionId) {
    return allSessions.some((s) => s.id === sessionId && s.running);
  }
  return allSessions.length > 0 && allSessions.every((s) => s.running);
}

function describeSnapshot(workspaces: WorkspaceItem[]): string {
  return JSON.stringify(
    workspaces.map((ws) => ({
      workspace: ws.id,
      sessions: ws.sessions.map((s) => ({ id: s.id, running: s.running })),
    }))
  );
}

/**
 * Resolves when the given session shows running: true (or, with no id,
 * once at least one session exists and all run) — learned from
 * /ws/dashboard, not by polling REST. The server registers the
 * connection before sending the initial snapshot, and every later state
 * change triggers a debounced broadcast, so no transition is missed.
 * On timeout, reports the last observed snapshot (rubric rule 12).
 */
export async function waitForSessionRunning(
  sessionId?: string,
  timeoutMs: number = 15_000
): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const ws = new WS(`${getBaseURL().replace(/^http/, 'ws')}/ws/dashboard`);
    let lastSnapshot: WorkspaceItem[] | null = null;
    let settled = false;

    const finish = (err?: Error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      ws.close();
      if (err) reject(err);
      else resolve();
    };

    const timer = setTimeout(() => {
      finish(
        new Error(
          `Session${sessionId ? ` ${sessionId}` : 's'} not running after ${timeoutMs}ms. ` +
            `Last observed snapshot: ${lastSnapshot ? describeSnapshot(lastSnapshot) : 'none received'}`
        )
      );
    }, timeoutMs);

    ws.on('message', (data: WS.Data) => {
      let msg: DashboardSessionsMessage;
      try {
        msg = JSON.parse(data.toString());
      } catch {
        return;
      }
      if (msg.type !== 'sessions' || !Array.isArray(msg.workspaces)) return;
      lastSnapshot = msg.workspaces;
      if (sessionsConditionMet(msg.workspaces, sessionId)) finish();
    });

    ws.on('error', (err: Error) => {
      finish(
        new Error(
          `/ws/dashboard error: ${err.message}. Last observed snapshot: ` +
            `${lastSnapshot ? describeSnapshot(lastSnapshot) : 'none received'}`
        )
      );
    });

    ws.on('close', () => {
      finish(
        new Error(
          `/ws/dashboard closed before the condition was met. Last observed snapshot: ` +
            `${lastSnapshot ? describeSnapshot(lastSnapshot) : 'none received'}`
        )
      );
    });
  });
}

// --- Utilities ---

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// --- Config save/restore helpers ---

/**
 * Captures the current config for later restoration.
 * Used in beforeAll/afterAll pairs to prevent config contamination between specs.
 */
export async function getConfig(): Promise<Record<string, unknown>> {
  return apiGet<Record<string, unknown>>('/api/config');
}

/**
 * Restores a previously saved config.
 * Posts the full saved config object back to the API.
 */
export async function resetConfig(saved: Record<string, unknown>): Promise<void> {
  await apiPost('/api/config', saved);
}

// --- Page helpers ---

/**
 * Wait for the dashboard WebSocket to connect (green "Live" indicator).
 */
export async function waitForDashboardLive(page: Page): Promise<void> {
  // Wait for the connection indicator dot to reflect a live WebSocket.
  // 15s matches waitForHealthy and waitForSelector timeouts used elsewhere.
  await page.waitForSelector('[data-testid="connection-status"][data-connected="true"]', {
    timeout: 15_000,
  });
}

/**
 * Waits until the session page's terminal reports an attached tmux control
 * mode — the state in which the daemon's paste-buffer and pane listeners
 * are armed. Rubric rule 5: an eventual UI state via locator assertion.
 * The backend sends an initial controlMode snapshot on terminal WebSocket
 * connect, so this resolves without waiting for a transition.
 */
export async function waitForControlModeAttached(
  page: Page,
  timeoutMs: number = 10_000
): Promise<void> {
  const pill = page.getByTestId('session-connection-pill');
  try {
    await expect(pill).toHaveAttribute('data-control-mode', 'attached', {
      timeout: timeoutMs,
    });
  } catch (err) {
    const last = await pill.getAttribute('data-control-mode').catch(() => null);
    throw new Error(
      `Terminal control mode not attached after ${timeoutMs}ms ` +
        `(last observed control mode: ${last ?? 'session pill not found'}; page: ${page.url()})\n${err}`
    );
  }
}
