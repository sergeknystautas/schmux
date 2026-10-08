/**
 * Worker-scoped Playwright fixture for per-worker daemon isolation.
 *
 * Each Playwright worker gets its own schmux daemon running on an ephemeral
 * port with an isolated HOME directory. This enables parallel test execution
 * (workers > 1) without shared state conflicts.
 *
 * The isolation pattern mirrors internal/e2e/e2e.go (Go E2E tests):
 * - SCHMUX_PORT=0, so the daemon binds a port the OS picks and reports it
 *   in daemon.url
 * - Isolated HOME so each daemon gets its own ~/.schmux/
 * - Isolated TMUX_TMPDIR so each daemon gets its own tmux socket directory
 * - Unique tmux_socket_name in config to prevent socket collisions
 */
import { test as base } from '@playwright/test';
import { execSync, spawn, type ChildProcess } from 'child_process';
import { mkdirSync, writeFileSync, rmSync, readFileSync, existsSync, createWriteStream } from 'fs';
import { join } from 'path';
import { sleep, waitForHealthy } from './helpers';

export { expect } from '@playwright/test';

/**
 * Wait for the daemon to write daemon.url, then for /api/healthz to answer
 * there, and return the URL. The daemon runs with SCHMUX_PORT=0, so the OS
 * picks its port: picking a free port here and handing it over raced other
 * workers for the same port. daemon.url is in this worker's own HOME, so it
 * can only name this worker's daemon.
 */
async function waitForDaemonURL(schmuxDir: string, timeoutMs: number): Promise<string> {
  const urlFile = join(schmuxDir, 'daemon.url');
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (existsSync(urlFile)) {
      const url = readFileSync(urlFile, 'utf8').trim();
      await waitForHealthy(timeoutMs - (Date.now() - start), url);
      return url;
    }
    await sleep(100);
  }
  throw new Error(
    `daemon did not write ${urlFile} within ${timeoutMs}ms (daemon log: ${join(schmuxDir, 'daemon.log')})`
  );
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

      // Write config with isolated workspace path and tmux socket. The port
      // comes from the OS at bind time (SCHMUX_PORT=0 below).
      const config = {
        workspace_path: workspacePath,
        source_code_management: 'git',
        tmux_socket_name: tmuxSocket,
        repos: [],
        run_targets: [],
        terminal: { width: 120, height: 40, seed_lines: 100 },
        ui: { panels: { eventMonitor: false, tmuxDiagnostic: false } },
      };
      writeFileSync(join(schmuxDir, 'config.json'), JSON.stringify(config, null, 2));

      // Set env vars so helpers.ts and helpers-terminal.ts pick them up.
      // Playwright workers are separate processes, so this is safe.
      // SCHMUX_BASE_URL is set once the daemon reports its URL, below.
      process.env.SCHMUX_TMUX_SOCKET = tmuxSocket;
      process.env.SCHMUX_REPO_DIR = repoDir;
      process.env.HOME = homeDir;
      process.env.TMUX_TMPDIR = homeDir;

      // Start daemon
      const daemon = spawn('schmux', ['daemon-run', '--dev-mode'], {
        env: {
          ...process.env,
          HOME: homeDir,
          TMUX_TMPDIR: homeDir,
          SCHMUX_PORT: '0',
        },
        stdio: ['ignore', 'pipe', 'pipe'],
        detached: false,
      });

      // Capture daemon logs for debugging — streamed to disk so the file is
      // useful for post-mortem even if the daemon is killed (e.g. on a test
      // failure that the entrypoint copies the log out before the worker
      // teardown gets a chance to flush).
      const logPath = join(schmuxDir, 'daemon.log');
      const logStream = createWriteStream(logPath, { flags: 'a' });
      daemon.stdout?.pipe(logStream);
      daemon.stderr?.pipe(logStream);

      daemon.on('exit', (code) => {
        if (code !== null && code !== 0) {
          console.error(`[worker ${idx}] Daemon exited with code ${code}`);
        }
      });

      // Wait for daemon to be ready — the same centralized probe the
      // helpers use, so timeouts carry last-status + daemon.log telemetry.
      const baseURL = await waitForDaemonURL(schmuxDir, 30_000);
      process.env.SCHMUX_BASE_URL = baseURL;

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

      // Clean up temp directory unless SCHMUX_KEEP_WORKER_DIRS is set
      // (used by debugging flows that need to inspect daemon.log post-mortem).
      if (!process.env.SCHMUX_KEEP_WORKER_DIRS) {
        rmSync(homeDir, { recursive: true, force: true });
      }
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
