/**
 * Playwright global setup: disposes all running sessions before the test suite
 * starts. This prevents session accumulation when using --repeat-each, where
 * persistent processes (e.g. flood-agent) from previous cycles would overwhelm
 * the daemon with output and cause tmux control mode timeouts.
 */

const BASE_URL = 'http://localhost:7337';

async function globalSetup(): Promise<void> {
  try {
    const res = await fetch(`${BASE_URL}/api/sessions`);
    if (!res.ok) return;

    const workspaces = (await res.json()) as Array<{
      sessions: Array<{ id: string; running: boolean }>;
    }>;

    for (const ws of workspaces) {
      for (const sess of ws.sessions) {
        if (sess.running) {
          try {
            await fetch(`${BASE_URL}/api/sessions/${sess.id}/dispose`, {
              method: 'POST',
            });
          } catch {
            // Ignore individual dispose failures
          }
        }
      }
    }
  } catch {
    // Daemon may not be ready yet during first run — that's fine
  }
}

export default globalSetup;
