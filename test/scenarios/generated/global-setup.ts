/**
 * Playwright global setup: disposes all running sessions and resets config
 * before the test suite starts. This prevents session accumulation when using
 * --repeat-each, where persistent processes (e.g. flood-agent) from previous
 * cycles would overwhelm the daemon with output and cause tmux control mode
 * timeouts. The config reset is a safety net for spec files that don't restore
 * their own config in afterAll.
 */

const BASE_URL = 'http://localhost:7337';

async function globalSetup(): Promise<void> {
  try {
    // Capture the entrypoint config baseline before disposing sessions
    let baselineConfig: Record<string, unknown> | undefined;
    try {
      const configRes = await fetch(`${BASE_URL}/api/config`);
      if (configRes.ok) {
        baselineConfig = (await configRes.json()) as Record<string, unknown>;
      }
    } catch {
      // Config fetch failed — skip reset
    }

    // Dispose all running sessions
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

    // Reset config to entrypoint baseline
    if (baselineConfig) {
      try {
        await fetch(`${BASE_URL}/api/config`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(baselineConfig),
        });
      } catch {
        // Ignore config reset failures
      }
    }
  } catch {
    // Daemon may not be ready yet during first run — that's fine
  }
}

export default globalSetup;
