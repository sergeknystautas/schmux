import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';
import WS from 'ws';
import { execSync } from 'child_process';
import { writeFileSync } from 'fs';
import path from 'path';
import os from 'os';

const BASE_URL = 'http://localhost:7337';

/**
 * Helper: listen to the /ws/dashboard WebSocket and collect
 * linear_sync_resolve_conflict messages until a condition is met.
 */
function waitForCRState(
  condition: (msg: Record<string, unknown>) => boolean,
  timeoutMs: number = 30_000
): Promise<Record<string, unknown>> {
  return new Promise((resolve, reject) => {
    const ws = new WS('ws://localhost:7337/ws/dashboard');
    const timer = setTimeout(() => {
      ws.close();
      reject(new Error(`CR state condition not met after ${timeoutMs}ms`));
    }, timeoutMs);

    ws.on('message', (data: WS.Data) => {
      try {
        const msg = JSON.parse(data.toString());
        if (msg.type === 'linear_sync_resolve_conflict' && condition(msg)) {
          clearTimeout(timer);
          ws.close();
          resolve(msg);
        }
      } catch {
        // ignore non-JSON messages
      }
    });

    ws.on('error', (err: Error) => {
      clearTimeout(timer);
      reject(new Error(`Dashboard WS error: ${err.message}`));
    });
  });
}

test.describe.serial('Conflict resolution progress with terminal panel', () => {
  let sessionId: string;
  let workspaceId: string;
  let repoDir: string;

  test.beforeAll(async () => {
    await waitForHealthy();

    // Create a repo with divergent branches that conflict on file.txt
    repoDir = '/tmp/schmux-test-repos/test-repo-cr-terminal';
    execSync(`rm -rf ${repoDir} && mkdir -p ${repoDir}`);
    execSync(`git init -b main ${repoDir}`);
    execSync(`git -C ${repoDir} config user.email "test@schmux.dev"`);
    execSync(`git -C ${repoDir} config user.name "Schmux Test"`);
    execSync(`printf 'original content\\n' > ${repoDir}/file.txt`);
    execSync(`git -C ${repoDir} add .`);
    execSync(`git -C ${repoDir} commit -m "initial"`);

    // Create branch with a conflicting change
    execSync(`git -C ${repoDir} checkout -b test-cr-terminal`);
    execSync(`printf 'branch content\\n' > ${repoDir}/file.txt`);
    execSync(`git -C ${repoDir} add .`);
    execSync(`git -C ${repoDir} commit -m "branch change"`);

    // Add a conflicting commit on main (same file, different content)
    execSync(`git -C ${repoDir} checkout main`);
    execSync(`printf 'main content\\n' > ${repoDir}/file.txt`);
    execSync(`git -C ${repoDir} add .`);
    execSync(`git -C ${repoDir} commit -m "main change"`);

    // Create a fake conflict resolver script.
    // The script reads the prompt from stdin (which contains the workspace path),
    // resolves the conflict by removing markers and keeping both sides, then outputs
    // valid JSON for conflictresolve.ParseResult.
    const resolverScript = path.join(os.tmpdir(), 'schmux-test-cr-resolver.sh');
    writeFileSync(
      resolverScript,
      `#!/bin/sh
# Read the prompt from stdin to find workspace path
PROMPT=$(cat)
# Extract workspace path from prompt (line: "Workspace path: /path/to/workspace")
WS_PATH=$(echo "$PROMPT" | grep "^Workspace path:" | sed 's/Workspace path: //')

# Find and resolve any conflicted files by keeping the incoming (HEAD) version
if [ -n "$WS_PATH" ]; then
  for f in $(cd "$WS_PATH" && git diff --name-only --diff-filter=U 2>/dev/null); do
    FULL="$WS_PATH/$f"
    if [ -f "$FULL" ]; then
      # Remove conflict markers, keep HEAD version (between <<<<<<< and =======)
      awk '
        /^<<<<<<< / { skip=0; in_ours=1; next }
        /^=======$/ { in_ours=0; in_theirs=1; next }
        /^>>>>>>> / { in_theirs=0; next }
        !in_theirs { print }
      ' "$FULL" > "$FULL.resolved"
      mv "$FULL.resolved" "$FULL"
    fi
  done
fi

# Output valid JSON response
cat <<'RESPONSE'
{"all_resolved": true, "confidence": "high", "summary": "Resolved conflict by keeping HEAD version", "files": {"file.txt": {"action": "modified", "description": "kept HEAD version"}}}
RESPONSE
`,
      { mode: 0o755 }
    );

    // Seed config with a conflict_resolve target pointing to our fake resolver
    const config: Record<string, unknown> = {
      workspace_path: '/tmp/schmux-test-workspaces',
      source_code_management: 'git',
      repos: [
        {
          name: repoDir.split('/').pop(),
          url: repoDir,
        },
      ],
      run_targets: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
      conflict_resolve: {
        target: `cr-resolver`,
        timeout_ms: 30000,
      },
    };

    // We need to also add the cr-resolver as a run target so resolveTarget can find it
    (config.run_targets as Array<Record<string, unknown>>).push({
      name: 'cr-resolver',
      command: resolverScript,
    });

    const res = await fetch(`${BASE_URL}/api/config`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    });
    if (!res.ok) {
      throw new Error(`Failed to seed config: ${res.status} ${await res.text()}`);
    }

    const results = await spawnSession({
      repo: repoDir,
      branch: 'test-cr-terminal',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;
    workspaceId = results[0].workspace_id;

    // Wait for session to be ready
    await waitForSessionRunning(sessionId);
  });

  test('01 trigger conflict resolution returns 202', async () => {
    const res = await fetch(
      `${BASE_URL}/api/workspaces/${workspaceId}/linear-sync-resolve-conflict`,
      { method: 'POST', headers: { 'Content-Type': 'application/json' } }
    );
    // Accept 202 (started) or 409 (auto-triggered during spawn)
    expect([202, 409]).toContain(res.status);
  });

  test('02 dashboard WS broadcasts in_progress state with steps', async () => {
    // Wait for the CR state to appear as in_progress or already finished
    const state = await waitForCRState(
      (msg) =>
        msg.workspace_id === workspaceId &&
        (msg.status === 'in_progress' || msg.status === 'done' || msg.status === 'failed'),
      15_000
    );
    expect(state.type).toBe('linear_sync_resolve_conflict');
    expect(state.workspace_id).toBe(workspaceId);
    expect(['in_progress', 'done', 'failed']).toContain(state.status);
  });

  test('03 dashboard WS broadcasts final done or failed state', async () => {
    // Wait for the CR state to reach terminal status
    const finalState = await waitForCRState(
      (msg) =>
        msg.workspace_id === workspaceId && (msg.status === 'done' || msg.status === 'failed'),
      30_000
    );
    expect(['done', 'failed']).toContain(finalState.status);
    // Steps array should have entries
    expect(Array.isArray(finalState.steps)).toBe(true);
    expect((finalState.steps as unknown[]).length).toBeGreaterThan(0);
    // tmux_session should be cleared after completion
    expect(finalState.tmux_session || '').toBe('');
  });

  test('04 final state includes commit hash', async () => {
    const finalState = await waitForCRState(
      (msg) =>
        msg.workspace_id === workspaceId && (msg.status === 'done' || msg.status === 'failed'),
      15_000
    );
    // The hash field should be set (the conflicting commit hash)
    expect(typeof finalState.hash).toBe('string');
    expect((finalState.hash as string).length).toBeGreaterThan(0);
  });

  test('05 resolve-conflict page shows status heading or auto-redirects', async ({ page }) => {
    // Find the resolve-conflict tab's route from workspace state.
    // The tab is a server-managed entity with kind="resolve-conflict".
    interface WorkspaceWithTabs {
      id: string;
      tabs?: Array<{ id: string; kind: string; route: string }>;
    }
    const workspaces = await fetch(`${BASE_URL}/api/sessions`).then(
      (r) => r.json() as Promise<WorkspaceWithTabs[]>
    );
    const ws = workspaces.find((w) => w.id === workspaceId);
    const crTab = ws?.tabs?.find((t) => t.kind === 'resolve-conflict');
    const conflictRoute = crTab?.route ?? `/resolve-conflict/${workspaceId}`;

    await page.goto(conflictRoute);
    await waitForDashboardLive(page);

    // After resolution completes, the page auto-redirects to session or home
    // So we either see the conflict heading OR we've been redirected
    const heading = page.locator('strong');
    const conflictHeading = heading.filter({
      hasText: /Resolving conflicts|Conflict resolution completed|Conflict resolution failed/,
    });

    // Check if we're still on resolve-conflict page or have been redirected
    const currentUrl = page.url();
    if (currentUrl.includes('/resolve-conflict/')) {
      // Still on conflict page - should show heading
      await expect(conflictHeading).toBeVisible({ timeout: 15000 });
    } else {
      // Auto-redirected away — verify we landed on a valid page (session detail or home),
      // not a 404 or error page.
      expect(currentUrl).toMatch(/\/(sessions\/|$)/);
      await expect(page.locator('body')).not.toContainText('404');
    }
  });

  test('06 resolve-conflict page shows final status or auto-redirects', async ({ page }) => {
    // Resolution already finished (verified by tests 02-04), state may be cleared.
    // Find the resolve-conflict tab's route from workspace state (if tab still exists).
    interface WorkspaceWithTabs {
      id: string;
      tabs?: Array<{ id: string; kind: string; route: string }>;
    }
    const workspaces = await fetch(`${BASE_URL}/api/sessions`).then(
      (r) => r.json() as Promise<WorkspaceWithTabs[]>
    );
    const ws = workspaces.find((w) => w.id === workspaceId);
    const crTab = ws?.tabs?.find((t) => t.kind === 'resolve-conflict');
    const conflictRoute = crTab?.route ?? `/resolve-conflict/${workspaceId}`;

    await page.goto(conflictRoute);
    await waitForDashboardLive(page);

    // After resolution completes, the page may auto-redirect to session or home
    const currentUrl = page.url();
    if (currentUrl.includes('/resolve-conflict/')) {
      // Still on conflict page - either show final status OR "Starting conflict resolution..."
      // (the latter happens when state is cleared but page hasn't redirected yet)
      const heading = page.locator('strong');
      const conflictHeading = heading.filter({
        hasText: /Conflict resolution completed|Conflict resolution failed/,
      });
      const startingText = page.locator('text=Starting conflict resolution');

      // One of these should be visible
      await expect(conflictHeading.or(startingText)).toBeVisible({ timeout: 15000 });
    } else {
      // Auto-redirected away — verify we landed on a valid page (session detail or home),
      // not a 404 or error page.
      expect(currentUrl).toMatch(/\/(sessions\/|$)/);
      await expect(page.locator('body')).not.toContainText('404');
    }
  });

  test('07 re-trigger endpoint responds', async () => {
    const res = await fetch(
      `${BASE_URL}/api/workspaces/${workspaceId}/linear-sync-resolve-conflict`,
      { method: 'POST', headers: { 'Content-Type': 'application/json' } }
    );
    expect([202, 409]).toContain(res.status);
  });
});
