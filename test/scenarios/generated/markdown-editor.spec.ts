import { test, expect } from './coverage-fixture';
import fs from 'fs/promises';
import path from 'path';
import {
  createTestRepo,
  getSessions,
  seedConfig,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';

test.describe('Edit a Markdown file while an agent changes it', () => {
  let repoPath: string;
  let workspacePath: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('markdown-editor-repo');

    // Write docs/notes.md with the three lines the scenario specifies,
    // and commit so the clone carries it.
    const notesDir = path.join(repoPath, 'docs');
    await fs.mkdir(notesDir, { recursive: true });
    await fs.writeFile(path.join(notesDir, 'notes.md'), 'alpha\nbeta\ngamma\n', 'utf8');
    const { execSync } = await import('child_process');
    execSync(`git -C ${repoPath} add docs/notes.md`);
    execSync(`git -C ${repoPath} commit -m "add notes"`);

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'noop-agent',
          command: "sh -c 'sleep 600'",
        },
      ],
    });

    // Materialize the workspace by spawning a session, then read the id back
    // from the sessions API.
    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'noop-agent': 1 },
    });
    await waitForSessionRunning(results[0].session_id);
    const sessions = await getSessions();
    const ws = sessions.find((w) => w.repo === repoPath);
    expect(ws).toBeDefined();
    workspaceId = ws!.id;
    // The editor writes to the workspace clone, not back to the test repo.
    // The API reports the clone's absolute path, which is per worker.
    workspacePath = ws!.path;
    expect(workspacePath).toBeTruthy();
  });

  // The on-disk notes.md under test (and only that file) is the workspace
  // clone's copy — the daemon's writes go there, not back to the test repo.
  const notesPath = () => path.join(workspacePath, 'docs', 'notes.md');

  test('autosave lands a typed edit on disk', async ({ page }) => {
    await page.goto(`/diff/${workspaceId}/md/docs%2Fnotes.md`);
    await waitForDashboardLive(page);

    // The editor mounts and the status reaches Saved once the initial
    // document is pushed by the websocket.
    const editor = page.locator('[data-testid="markdown-editor"]');
    await expect(editor).toBeVisible();
    await expect(page.locator('[data-testid="markdown-status"]')).toHaveText('Saved');

    // Append " one" at the end of the first line. CodeMirror 5 exposes its
    // instance on the host element; we drive it directly so we can pick the
    // exact cursor position and skip the ime / composition path.
    await page.evaluate(() => {
      const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          setCursor(p: { line: number; ch: number }): void;
          replaceRange(text: string, p: { line: number; ch: number }): void;
        };
      };
      const cm = cmEl?.CodeMirror;
      if (!cm) throw new Error('CodeMirror not mounted');
      cm.setCursor({ line: 0, ch: 5 }); // end of "alpha"
      cm.replaceRange(' one', { line: 0, ch: 5 });
    });

    // The disk write is the observable outcome of autosave; poll for it
    // first. "Saving…" itself lasts only as long as one round trip, so it is
    // not asserted (docs/testing.md rule 4: a negative/transient claim needs
    // an observation window, and none is worth it here). Once disk has the
    // edit, the header must read Saved, not Saving… or an error.
    await expect
      .poll(async () => fs.readFile(notesPath(), 'utf8'))
      .toBe('alpha one\nbeta\ngamma\n');
    await expect(page.locator('[data-testid="markdown-status"]')).toHaveText('Saved');
  });

  test('agent rewrite shows up in the editor without reload', async ({ page }) => {
    await page.goto(`/diff/${workspaceId}/md/docs%2Fnotes.md`);
    await waitForDashboardLive(page);

    const editor = page.locator('[data-testid="markdown-editor"]');
    await expect(editor).toBeVisible();
    await expect(page.locator('[data-testid="markdown-status"]')).toHaveText('Saved');

    // Atomic rename, the way sed -i does it. The fsnotify watcher on the
    // parent directory must pick this up and push a document message.
    const tmp = path.join(workspacePath, 'docs', 'notes.md.tmp');
    await fs.writeFile(tmp, 'alpha one\nbeta\ngamma two\n', 'utf8');
    await fs.rename(tmp, path.join(workspacePath, 'docs', 'notes.md'));

    // The editor's CodeMirror surface is updated in place. Read the live
    // value out of the CodeMirror instance; the bytemd preview pane also
    // re-renders, but the textarea is the single source of truth.
    await expect
      .poll(async () => {
        return await page.evaluate(() => {
          const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
            CodeMirror?: { getValue(): string };
          };
          return cmEl?.CodeMirror?.getValue() ?? null;
        });
      })
      .toBe('alpha one\nbeta\ngamma two\n');
    await expect(page.locator('[data-testid="markdown-status"]')).toHaveText('Saved');
  });

  test('browser and agent edits to the same line land in one file', async ({ page }) => {
    await page.goto(`/diff/${workspaceId}/md/docs%2Fnotes.md`);
    await waitForDashboardLive(page);

    const editor = page.locator('[data-testid="markdown-editor"]');
    await expect(editor).toBeVisible();
    await expect(page.locator('[data-testid="markdown-status"]')).toHaveText('Saved');

    // From the previous test the on-disk file is "alpha one\nbeta\ngamma two\n".
    // The browser types " three" at the end of line 1 and the agent (test)
    // appends " four" to the same line on disk. The agent's write is derived
    // from the file as it is at that moment, the way a real agent's `sed -i`
    // would be, so the outcome is the same whichever side lands first: if the
    // browser's autosave already wrote "beta three", the agent appends to it;
    // if not, the server merges the pending " three" onto "beta four". Either
    // way one file holds both words, which is what the scenario requires.
    await page.evaluate(() => {
      const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          setCursor(p: { line: number; ch: number }): void;
          replaceRange(text: string, p: { line: number; ch: number }): void;
        };
      };
      const cm = cmEl?.CodeMirror;
      if (!cm) throw new Error('CodeMirror not mounted');
      cm.setCursor({ line: 1, ch: 4 }); // end of "beta"
      cm.replaceRange(' three', { line: 1, ch: 4 });
    });
    const notesPath = path.join(workspacePath, 'docs', 'notes.md');
    const current = await fs.readFile(notesPath, 'utf8');
    const agentVersion = current.replace(/^beta.*$/m, (line) => `${line} four`);
    expect(agentVersion).not.toBe(current);
    const tmp = path.join(workspacePath, 'docs', 'notes.md.tmp');
    await fs.writeFile(tmp, agentVersion, 'utf8');
    await fs.rename(tmp, notesPath);

    // Poll the editor's CodeMirror value: after the merge line 1 must carry
    // both the browser's "three" and the agent's "four". The server's atomic write
    // is what triggers the next document push, so polling the editor is
    // the right condition (no fixed sleep).
    await expect
      .poll(
        async () =>
          page.evaluate(() => {
            const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
              CodeMirror?: { getValue(): string };
            };
            return cmEl?.CodeMirror?.getValue() ?? null;
          }),
        { timeout: 5_000 }
      )
      .toMatch(/^alpha one\nbeta(?=.*three)(?=.*four).*\n/);
    await expect(page.locator('[data-testid="markdown-status"]')).toHaveText('Saved');
    // And the file on disk agrees with the editor.
    await expect
      .poll(async () => fs.readFile(notesPath, 'utf8'), { timeout: 5_000 })
      .toMatch(/^alpha one\nbeta(?=.*three)(?=.*four).*\n/);
  });

  test('Download link points at the workspace file API', async ({ page }) => {
    await page.goto(`/diff/${workspaceId}/md/docs%2Fnotes.md`);
    await waitForDashboardLive(page);

    const link = page.locator('[data-testid="download-markdown"]');
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', `/api/file/${workspaceId}/docs%2Fnotes.md`);
  });
});
