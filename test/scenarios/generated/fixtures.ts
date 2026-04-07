/**
 * Worker-scoped Playwright fixture for per-worker daemon isolation.
 *
 * Each Playwright worker gets its own schmux daemon running on an ephemeral
 * port with an isolated HOME directory. This enables parallel test execution
 * (workers > 1) without shared state conflicts.
 *
 * The isolation pattern mirrors internal/e2e/e2e.go (Go E2E tests):
 * - Ephemeral port via net.createServer().listen(0)
 * - Isolated HOME so each daemon gets its own ~/.schmux/
 * - Isolated TMUX_TMPDIR so each daemon gets its own tmux socket directory
 * - Unique tmux_socket_name in config to prevent socket collisions
 */
import { test as base } from '@playwright/test';
import { createServer } from 'net';
import { execSync, spawn, type ChildProcess } from 'child_process';
import { mkdirSync, writeFileSync, rmSync, readFileSync, existsSync } from 'fs';
import { join, resolve } from 'path';

export { expect } from '@playwright/test';

/** Allocate an ephemeral port by binding to :0 and immediately closing. */
async function allocatePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.listen(0, '127.0.0.1', () => {
      const addr = server.address();
      if (!addr || typeof addr === 'string') {
        server.close(() => reject(new Error('Failed to get port')));
        return;
      }
      const port = addr.port;
      server.close(() => resolve(port));
    });
    server.on('error', reject);
  });
}

/** Poll a URL until it returns 200 or timeout. */
async function waitForHealthz(url: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${url}/api/healthz`);
      if (res.ok) return;
    } catch {
      // Not ready yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`Daemon at ${url} not healthy after ${timeoutMs}ms`);
}

export const test = base.extend<{}, { daemonURL: string }>({
  // Worker-scoped fixture: starts an isolated daemon per Playwright worker.
  // All tests in this worker share the same daemon.
  daemonURL: [
    async ({}, use, workerInfo) => {
      const idx = workerInfo.workerIndex;
      const homeDir = `/tmp/schmux-worker-${idx}`;
      const schmuxDir = join(homeDir, '.schmux');
      const workspacePath = join(homeDir, 'workspaces');
      const repoDir = join(homeDir, 'test-repos');
      const tmuxSocket = `schmux-w${idx}`;

      // Clean up any leftover state from a previous run
      rmSync(homeDir, { recursive: true, force: true });

      // Create directory structure
      mkdirSync(schmuxDir, { recursive: true });
      mkdirSync(workspacePath, { recursive: true });
      mkdirSync(repoDir, { recursive: true });

      // Write git config (the entrypoint sets global, but we need per-worker)
      writeFileSync(
        join(homeDir, '.gitconfig'),
        '[user]\n  email = test@schmux.dev\n  name = Schmux Test\n'
      );

      // Allocate ephemeral port
      const port = await allocatePort();
      const baseURL = `http://127.0.0.1:${port}`;

      // Write config with isolated port, workspace path, and tmux socket
      const config = {
        workspace_path: workspacePath,
        source_code_management: 'git',
        tmux_socket_name: tmuxSocket,
        network: { port },
        repos: [],
        run_targets: [],
        terminal: { width: 120, height: 40, seed_lines: 100 },
      };
      writeFileSync(join(schmuxDir, 'config.json'), JSON.stringify(config, null, 2));

      // Set env vars so helpers.ts and helpers-terminal.ts pick them up.
      // Playwright workers are separate processes, so this is safe.
      process.env.SCHMUX_BASE_URL = baseURL;
      process.env.SCHMUX_TMUX_SOCKET = tmuxSocket;
      process.env.SCHMUX_REPO_DIR = repoDir;
      process.env.HOME = homeDir;
      process.env.TMUX_TMPDIR = homeDir;

      // Start daemon from the project root so it can find ./assets/dashboard/dist
      // (the binary doesn't embed assets — they're copied alongside it in Docker).
      const projectRoot = resolve(__dirname, '../../..');
      const daemon = spawn('schmux', ['daemon-run', '--dev-mode'], {
        cwd: projectRoot,
        env: {
          ...process.env,
          HOME: homeDir,
          TMUX_TMPDIR: homeDir,
        },
        stdio: ['ignore', 'pipe', 'pipe'],
        detached: false,
      });

      // Capture daemon logs for debugging
      const logPath = join(schmuxDir, 'daemon.log');
      const logChunks: Buffer[] = [];
      daemon.stdout?.on('data', (chunk: Buffer) => logChunks.push(chunk));
      daemon.stderr?.on('data', (chunk: Buffer) => logChunks.push(chunk));

      daemon.on('exit', (code) => {
        // Write collected logs on exit
        try {
          writeFileSync(logPath, Buffer.concat(logChunks));
        } catch {
          // Best-effort
        }
        if (code !== null && code !== 0) {
          console.error(`[worker ${idx}] Daemon exited with code ${code}`);
        }
      });

      // Wait for daemon to be ready
      await waitForHealthz(baseURL, 30_000);

      // Provide the URL to all tests in this worker
      await use(baseURL);

      // Teardown: kill daemon and clean up
      daemon.kill('SIGTERM');
      await new Promise<void>((resolve) => {
        const timeout = setTimeout(() => {
          daemon.kill('SIGKILL');
          resolve();
        }, 10_000);
        daemon.on('exit', () => {
          clearTimeout(timeout);
          resolve();
        });
      });

      // Kill tmux server to clean up any zombie sessions
      try {
        execSync(`tmux -L ${tmuxSocket} kill-server`, { stdio: 'ignore' });
      } catch {
        // Best-effort: tmux server may already be gone
      }

      // Clean up temp directory
      rmSync(homeDir, { recursive: true, force: true });
    },
    { scope: 'worker' },
  ],

  // Override Playwright's built-in baseURL so page.goto('/path') works.
  // Must be test-scoped (Playwright's default for baseURL) but reads from
  // the worker-scoped daemonURL.
  baseURL: async ({ daemonURL }, use) => {
    await use(daemonURL);
  },
});
