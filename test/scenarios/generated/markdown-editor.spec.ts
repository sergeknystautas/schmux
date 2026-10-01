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

    // The editor mounts in Preview mode; the status reaches Saved once the
    // websocket pushes the initial document.
    const editor = page.locator('[data-testid="markdown-editor"]');
    await expect(editor).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute(
      'aria-selected',
      'true'
    );
    await expect(page.locator('[data-testid="markdown-status"]')).toHaveText('Saved');
    await page.getByRole('tab', { name: 'Write' }).click();

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

    await page.getByRole('tab', { name: 'Write' }).click();
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

    await page.getByRole('tab', { name: 'Write' }).click();
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

  test('editing surface scrolls and tables use panel layout', async ({ page }) => {
    const tablePath = path.join(workspacePath, 'docs', 'table.md');
    const rows = Array.from({ length: 60 }, (_, i) => `| row ${i} | value ${i} |`);
    await fs.writeFile(
      tablePath,
      '# Long table\n\n| Name | Value |\n| --- | --- |\n' + rows.join('\n') + '\n',
      'utf8'
    );
    await page.goto(`/diff/${workspaceId}/md/docs%2Ftable.md`);
    await waitForDashboardLive(page);

    await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute(
      'aria-selected',
      'true'
    );
    const headerGeometry = await page.evaluate(() => {
      const header = document.querySelector('.diff-content__header') as HTMLElement | null;
      const toolbar = document.querySelector('.bytemd-toolbar') as HTMLElement | null;
      if (!header || !toolbar) throw new Error('header toolbar not mounted');
      const headerBox = header.getBoundingClientRect();
      const toolbarBox = toolbar.getBoundingClientRect();
      return {
        headerTop: headerBox.top,
        toolbarTop: toolbarBox.top,
        headerBottom: headerBox.bottom,
        toolbarBottom: toolbarBox.bottom,
      };
    });
    expect(headerGeometry.toolbarTop).toBeGreaterThanOrEqual(headerGeometry.headerTop - 1);
    expect(headerGeometry.toolbarTop).toBeLessThan(headerGeometry.headerBottom);
    expect(headerGeometry.toolbarBottom).toBeLessThanOrEqual(headerGeometry.headerBottom + 1);
    await expect(page.getByRole('heading', { name: 'Long table' })).toBeVisible();
    const previewMetrics = await page.evaluate(() => {
      const body = document.querySelector('.bytemd-body') as HTMLElement | null;
      const table = document.querySelector('.bytemd-preview table') as HTMLElement | null;
      const preview = document.querySelector('.bytemd-preview') as HTMLElement | null;
      if (!body || !table) throw new Error('preview surface not mounted');
      return {
        contentHeight: document.querySelector('.diff-content')?.clientHeight ?? null,
        editorHeight:
          document.querySelector('[data-testid="markdown-editor"]')?.clientHeight ?? null,
        bytemdHeight: document.querySelector('.bytemd')?.clientHeight ?? null,
        scrollHeight: preview?.scrollHeight ?? body.scrollHeight,
        clientHeight: preview?.clientHeight ?? body.clientHeight,
        tableDisplay: getComputedStyle(table).display,
        tableWidth: table.getBoundingClientRect().width,
        bodyWidth: body.clientWidth,
      };
    });
    expect(previewMetrics.scrollHeight).toBeGreaterThan(previewMetrics.clientHeight);
    expect(previewMetrics.tableDisplay).toBe('table');
    expect(previewMetrics.tableWidth).toBeGreaterThan(previewMetrics.bodyWidth - 80);

    await page.getByRole('tab', { name: 'Write' }).click();
    const writeMetrics = await page.evaluate(() => {
      const scroll = document.querySelector('.CodeMirror-scroll') as HTMLElement | null;
      const surface = document.querySelector('.CodeMirror') as HTMLElement | null;
      if (!scroll || !surface) throw new Error('writing surface not mounted');
      scroll.scrollTop = 100;
      return {
        scrollTop: scroll.scrollTop,
        scrollHeight: scroll.scrollHeight,
        clientHeight: scroll.clientHeight,
        fontSize: getComputedStyle(surface).fontSize,
        lineHeight: getComputedStyle(surface).lineHeight,
      };
    });
    expect(writeMetrics.scrollTop).toBe(100);
    expect(writeMetrics.scrollHeight).toBeGreaterThan(writeMetrics.clientHeight);
    expect(writeMetrics.fontSize).toBe('14px');
    expect(writeMetrics.lineHeight).toBe('21px');
  });

  test('editor tab, scroll, and cursor state survive reload', async ({ page }) => {
    const statePath = path.join(workspacePath, 'docs', 'state.md');
    const headings = Array.from({ length: 50 }, (_, i) => `## Heading ${i + 1}`).join('\n\n');
    await fs.writeFile(statePath, `${headings}\n`, 'utf8');
    await page.goto(`/diff/${workspaceId}/md/docs%2Fstate.md`);
    await waitForDashboardLive(page);

    await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute(
      'aria-selected',
      'true'
    );
    await expect(page.getByRole('heading', { name: 'Heading 1', exact: true })).toBeVisible();
    await page.evaluate(() => {
      const preview = document.querySelector('.bytemd-preview') as HTMLElement | null;
      if (!preview) throw new Error('preview not mounted');
      preview.scrollTop = 180;
      preview.dispatchEvent(new Event('scroll'));
    });
    await page.keyboard.press('Meta+\\');
    await expect(page.getByRole('tab', { name: 'Write' })).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('.bytemd-editor')).toBeVisible();
    await page.evaluate(() => {
      const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: { focus(): void };
      };
      if (!cmEl?.CodeMirror) throw new Error('CodeMirror not mounted');
      cmEl.CodeMirror.focus();
    });
    for (let i = 0; i < 20; i++) {
      await page.keyboard.press('ArrowDown');
    }
    await expect
      .poll(() =>
        page.evaluate(() => {
          const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
            CodeMirror?: { getCursor(): { line: number; ch: number } };
          };
          return cmEl?.CodeMirror?.getCursor().line ?? null;
        })
      )
      .toBe(20);
    const beforeReload = await page.evaluate((storageKey: string) => {
      const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          getCursor(): { line: number; ch: number };
          indexFromPos(position: { line: number; ch: number }): number;
        };
      };
      return {
        line: cmEl?.CodeMirror?.getCursor().line ?? null,
        index: cmEl?.CodeMirror ? cmEl.CodeMirror.indexFromPos(cmEl.CodeMirror.getCursor()) : null,
        stored: localStorage.getItem(storageKey),
      };
    }, `schmux-markdown-cursor-position-${workspaceId}-docs/state.md`);
    expect(beforeReload).toMatchObject({ line: 20, index: Number(beforeReload.stored) });

    await page.reload();
    await waitForDashboardLive(page);
    await expect(page.getByRole('tab', { name: 'Write' })).toHaveAttribute('aria-selected', 'true');
    await expect
      .poll(() =>
        page.evaluate(() => {
          const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
            CodeMirror?: { getCursor(): { line: number; ch: number } };
          };
          return cmEl?.CodeMirror?.getCursor().line ?? null;
        })
      )
      .toBe(20);
    const afterReload = await page.evaluate((storageKey: string) => {
      const cmEl = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          getCursor(): { line: number; ch: number };
          indexFromPos(position: { line: number; ch: number }): number;
        };
      };
      return {
        line: cmEl?.CodeMirror?.getCursor().line ?? null,
        index: cmEl?.CodeMirror ? cmEl.CodeMirror.indexFromPos(cmEl.CodeMirror.getCursor()) : null,
        stored: localStorage.getItem(storageKey),
      };
    }, `schmux-markdown-cursor-position-${workspaceId}-docs/state.md`);
    expect(afterReload).toMatchObject({ line: 20, index: Number(afterReload.stored) });

    await page.keyboard.press('Meta+\\');
    await expect(page.getByRole('tab', { name: 'Preview' })).toHaveAttribute(
      'aria-selected',
      'true'
    );
    await expect
      .poll(() => page.evaluate(() => document.querySelector('.bytemd-preview')?.scrollTop ?? null))
      .toBe(180);
  });
});
