import { type Page, expect } from '@playwright/test';

const BASE_URL = 'http://localhost:7337';
const SCHMUX_BIN = process.env.SCHMUX_BIN || 'schmux';
const SCHMUX_HOME = process.env.SCHMUX_HOME || `${process.env.HOME}/.schmux`;

// --- Config helpers ---

interface SetupOptions {
  repos?: string[];
  agents?: Array<{ name: string; command: string; promptable?: boolean }>;
  workspacePath?: string;
  scm?: 'git' | 'git-worktree';
}

/**
 * Seeds ~/.schmux/config.json with test repos and agents.
 * Mirrors internal/e2e/e2e.go CreateConfig (line 218).
 */
export async function seedConfig(opts: SetupOptions = {}): Promise<void> {
  const config: Record<string, unknown> = {
    workspace_path: opts.workspacePath || '/tmp/schmux-test-workspaces',
    source_code_management: opts.scm || 'git',
    repos: (opts.repos || []).map((r) => ({
      name: r,
      url: r,
      default_branch: 'main',
    })),
    run_targets: {
      promptable: (opts.agents || [])
        .filter((a) => a.promptable !== false)
        .map((a) => ({ name: a.name, command: a.command })),
      command: (opts.agents || [])
        .filter((a) => a.promptable === false)
        .map((a) => ({ name: a.name, command: a.command })),
    },
  };

  const res = await fetch(`${BASE_URL}/api/config`, {
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
  const res = await fetch(`${BASE_URL}${path}`);
  if (!res.ok) {
    throw new Error(`GET ${path} failed: ${res.status} ${await res.text()}`);
  }
  return res.json() as Promise<T>;
}

export async function apiPost<T = unknown>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    method: 'POST',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw new Error(`POST ${path} failed: ${res.status} ${await res.text()}`);
  }
  return res.json() as Promise<T>;
}

// --- Health check ---

export async function waitForHealthy(timeoutMs: number = 15_000): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const res = await fetch(`${BASE_URL}/api/healthz`);
      if (res.ok) return;
    } catch {
      // not ready yet
    }
    await sleep(500);
  }
  throw new Error(`Daemon not healthy after ${timeoutMs}ms`);
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

interface SessionsResponse {
  workspaces: Array<{
    workspace_id: string;
    repo: string;
    branch: string;
    sessions: Array<{
      session_id: string;
      nickname: string;
      target: string;
      status: string;
    }>;
  }>;
}

export async function getSessions(): Promise<SessionsResponse> {
  return apiGet<SessionsResponse>('/api/sessions');
}

export async function disposeSession(sessionId: string): Promise<void> {
  await apiPost(`/api/sessions/${sessionId}/dispose`);
}

// --- WebSocket helpers ---

export async function waitForTerminalOutput(
  sessionId: string,
  substring: string,
  timeoutMs: number = 10_000
): Promise<string> {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:7337/ws/terminal/${sessionId}`);
    let buffer = '';
    const timer = setTimeout(() => {
      ws.close();
      reject(
        new Error(
          `Terminal output did not contain "${substring}" after ${timeoutMs}ms. Buffer: ${buffer.slice(0, 500)}`
        )
      );
    }, timeoutMs);

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      if (msg.content) buffer += msg.content;
      if (buffer.includes(substring)) {
        clearTimeout(timer);
        ws.close();
        resolve(buffer);
      }
    };

    ws.onerror = (err) => {
      clearTimeout(timer);
      reject(new Error(`WebSocket error: ${err}`));
    };
  });
}

// --- Git repo helpers ---

/**
 * Creates a local bare git repo for testing.
 * Returns the repo path.
 */
export async function createTestRepo(name: string): Promise<string> {
  const { execSync } = await import('child_process');
  const repoDir = `/tmp/schmux-test-repos/${name}`;
  execSync(`rm -rf ${repoDir} && mkdir -p ${repoDir}`);
  execSync(`git init ${repoDir}`);
  execSync(`git -C ${repoDir} commit --allow-empty -m "initial"`);
  return repoDir;
}

// --- Utilities ---

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// --- Page helpers ---

/**
 * Wait for the dashboard WebSocket to connect (green "Live" indicator).
 */
export async function waitForDashboardLive(page: Page): Promise<void> {
  // The dashboard shows a connection indicator; wait for the page to load
  // and the WebSocket to establish.
  await page.waitForLoadState('networkidle');
  await sleep(1000); // allow WebSocket handshake
}
