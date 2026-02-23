# Spawn UI Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Simplify the spawn page by unifying the agent dropdown, putting repo on the same row as agent, and making slash commands auto-engage.

**Architecture:** Surgical edits to `SpawnPage.tsx` (rendering + handler logic) and `PromptTextarea.tsx` (slash command list + auto-engage callback). No new components, no state management changes, no API changes.

**Tech Stack:** React, TypeScript, Vitest

**Design doc:** `docs/specs/2026-02-23-spawn-ui-redesign-design.md`

---

### Task 1: Unified Agent Dropdown — Replace Mode Selector with Single Dropdown

Replace the two-control pattern (mode selector + agent dropdown/grid) with a single `<select>` that lists agents plus "Multiple agents" and "Advanced" at the bottom.

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:1103-1269`

**Step 1: Write a failing test**

Create `assets/dashboard/src/routes/SpawnPage.agent-select.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { SpawnPage } from './SpawnPage';
import { SessionsContext } from '../contexts/SessionsContext';
import { ConfigContext } from '../contexts/ConfigContext';

// Minimal context providers for spawn page rendering
function renderSpawnPage() {
  const config = {
    repos: [{ url: 'https://github.com/test/repo', name: 'test-repo' }],
    run_targets: [
      { name: 'claude', label: 'Claude Code', type: 'promptable' },
      { name: 'codex', label: 'Codex', type: 'promptable' },
      { name: 'deploy', label: 'Deploy', type: 'command' },
    ],
    quick_launch: [],
    branch_suggest_target: 'claude',
  };
  const sessions = {
    workspaces: [],
    waitForSession: vi.fn(),
  };
  return render(
    <MemoryRouter initialEntries={['/spawn']}>
      <ConfigContext.Provider value={{ config, loading: false, error: null, refetch: vi.fn() }}>
        <SessionsContext.Provider value={sessions as any}>
          <SpawnPage />
        </SessionsContext.Provider>
      </ConfigContext.Provider>
    </MemoryRouter>
  );
}

describe('Unified agent dropdown', () => {
  it('shows agents plus Multiple/Advanced in a single dropdown', () => {
    renderSpawnPage();
    const agentSelect = screen.getByTestId('agent-select');
    const options = within(agentSelect).getAllByRole('option');
    const labels = options.map((o) => o.textContent);
    expect(labels).toContain('Claude Code');
    expect(labels).toContain('Codex');
    expect(labels).toContain('Multiple agents');
    expect(labels).toContain('Advanced');
  });

  it('does NOT show the old mode selector dropdown', () => {
    renderSpawnPage();
    // The old dropdown had options "Single Agent", "Multiple Agents", "Advanced"
    expect(screen.queryByText('Single Agent')).toBeNull();
  });
});
```

**Step 2: Run the test to verify it fails**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage.agent-select.test.tsx`
Expected: FAIL — no `data-testid="agent-select"` element, old mode selector still present.

**Step 3: Replace the mode selector + agent dropdown with unified dropdown**

In `SpawnPage.tsx`, replace lines 1103-1269 (the entire agent selection block inside the `<>` fragment) with:

```tsx
{
  /* Agent selection */
}
{
  spawnMode === 'promptable' && promptableList.length > 0 && (
    <>
      {modelSelectionMode === 'single' ? (
        <select
          className="select"
          data-testid="agent-select"
          value={promptableList.find((item) => (targetCounts[item.name] || 0) > 0)?.name || ''}
          onChange={(e) => {
            const val = e.target.value;
            if (val === '__multiple__') {
              setModelSelectionMode('multiple');
            } else if (val === '__advanced__') {
              setModelSelectionMode('advanced');
            } else if (val) {
              toggleAgent(val);
            } else {
              const selected = promptableList.find((item) => (targetCounts[item.name] || 0) > 0);
              if (selected) toggleAgent(selected.name);
            }
          }}
        >
          <option value="">Select agent...</option>
          {promptableList.map((item) => (
            <option key={item.name} value={item.name}>
              {item.label}
            </option>
          ))}
          <option disabled>──────────</option>
          <option value="__multiple__">Multiple agents</option>
          <option value="__advanced__">Advanced</option>
        </select>
      ) : (
        <>
          <div style={{ gridColumn: '1 / -1' }}>
            <div style={{ marginBottom: 'var(--spacing-sm)' }}>
              <button type="button" className="btn" onClick={() => setModelSelectionMode('single')}>
                Single agent
              </button>
            </div>

            {modelSelectionMode === 'multiple' && (
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
                  gap: 'var(--spacing-sm)',
                }}
              >
                {promptableList.map((item) => {
                  const isSelected = (targetCounts[item.name] || 0) > 0;
                  return (
                    <button
                      key={item.name}
                      type="button"
                      className={`btn${isSelected ? ' btn--primary' : ''}`}
                      onClick={() => toggleAgent(item.name)}
                      data-testid={`agent-${item.name}`}
                      style={{
                        height: 'auto',
                        padding: 'var(--spacing-sm)',
                        textAlign: 'left',
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}
                    >
                      {item.label}
                    </button>
                  );
                })}
              </div>
            )}

            {modelSelectionMode === 'advanced' && (
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                  gap: 'var(--spacing-sm)',
                }}
              >
                {/* Same advanced counter grid as today — unchanged */}
                {promptableList.map((item) => {
                  const count = targetCounts[item.name] || 0;
                  const isSelected = count > 0;
                  return (
                    <div
                      key={item.name}
                      data-testid={`agent-${item.name}`}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 'var(--spacing-xs)',
                        border: '1px solid var(--color-border)',
                        borderRadius: 'var(--radius-sm)',
                        padding: 'var(--spacing-xs)',
                        backgroundColor: isSelected
                          ? 'var(--color-accent)'
                          : 'var(--color-surface-alt)',
                      }}
                    >
                      <span
                        style={{
                          fontSize: '0.875rem',
                          flex: 1,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                        }}
                      >
                        {item.label}
                      </span>
                      <button
                        type="button"
                        className="btn"
                        onClick={() => updateTargetCount(item.name, -1)}
                        disabled={count === 0}
                        style={{
                          padding: '2px 8px',
                          fontSize: '0.75rem',
                          minHeight: '24px',
                          minWidth: '28px',
                          lineHeight: '1',
                          backgroundColor: isSelected
                            ? 'rgba(255,255,255,0.2)'
                            : 'var(--color-surface)',
                          color: isSelected ? 'white' : 'var(--color-text)',
                          border: 'none',
                          borderRadius: 'var(--radius-sm)',
                        }}
                      >
                        −
                      </button>
                      <span style={{ fontSize: '0.875rem', minWidth: '16px', textAlign: 'center' }}>
                        {count}
                      </span>
                      <button
                        type="button"
                        className="btn"
                        onClick={() => updateTargetCount(item.name, 1)}
                        style={{
                          padding: '2px 8px',
                          fontSize: '0.75rem',
                          minHeight: '24px',
                          minWidth: '28px',
                          lineHeight: '1',
                          backgroundColor: isSelected
                            ? 'rgba(255,255,255,0.2)'
                            : 'var(--color-surface)',
                          color: isSelected ? 'white' : 'var(--color-text)',
                          border: 'none',
                          borderRadius: 'var(--radius-sm)',
                        }}
                      >
                        +
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </>
      )}
    </>
  );
}
```

**Step 4: Run the test to verify it passes**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage.agent-select.test.tsx`
Expected: PASS

**Step 5: Run full test suite**

Run: `./test.sh`
Expected: All tests pass. If existing tests reference the old mode selector dropdown, update them.

**Step 6: Commit**

```
/commit
```

---

### Task 2: Agent + Repo Same Row Layout

When in single agent mode (fresh spawn), put the agent dropdown and repo dropdown side-by-side. When in multi/advanced, repo gets its own full-width row.

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:1093-1319` (the grid container and repo section)

**Step 1: Write a failing test**

Add to `SpawnPage.agent-select.test.tsx`:

```tsx
describe('Agent + Repo same row layout', () => {
  it('shows agent and repo on the same row in single mode', () => {
    renderSpawnPage();
    const agentSelect = screen.getByTestId('agent-select');
    const repoSelect = screen.getByTestId('spawn-repo-select');
    // Both should share a parent flex row
    const row = agentSelect.closest('[data-testid="agent-repo-row"]');
    expect(row).not.toBeNull();
    expect(row).toContainElement(repoSelect);
  });

  it('moves repo to its own row in multiple mode', async () => {
    const user = userEvent.setup();
    renderSpawnPage();
    // Switch to multiple mode
    const agentSelect = screen.getByTestId('agent-select');
    await user.selectOptions(agentSelect, '__multiple__');
    // Repo should no longer be in the agent-repo-row
    const repoSelect = screen.getByTestId('spawn-repo-select');
    const row = repoSelect.closest('[data-testid="agent-repo-row"]');
    expect(row).toBeNull();
  });
});
```

**Step 2: Run the test to verify it fails**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage.agent-select.test.tsx`
Expected: FAIL

**Step 3: Restructure the layout**

Replace the grid container (lines 1093-1101 and its contents through line 1319) with a conditional layout:

- **Single mode**: Wrap agent select + repo select in a flex row with `data-testid="agent-repo-row"`
- **Multi/Advanced mode**: Repo gets its own full-width row above the grid; agent grid is below with "Single agent" button

The parent container changes from a 2-column grid to a flex column, with child rows for each logical group.

Key structural change:

```tsx
{spawnMode !== 'resume' && (
  <div style={{ marginBottom: 'var(--spacing-md)' }}>
    {/* Single agent mode: agent + repo side by side */}
    {spawnMode === 'promptable' && promptableList.length > 0 && modelSelectionMode === 'single' && (
      <div
        data-testid="agent-repo-row"
        style={{ display: 'flex', gap: 'var(--spacing-md)', alignItems: 'flex-start' }}
      >
        {/* Agent dropdown */}
        <select ...>...</select>

        {/* Repo dropdown (only in fresh mode) */}
        {mode === 'fresh' && !isRemoteWithoutProvisioning && (
          <div style={{ flex: 1 }}>
            <select ...>...</select>
          </div>
        )}
      </div>
    )}

    {/* Multi/Advanced mode: repo on own row, then grid */}
    {spawnMode === 'promptable' && promptableList.length > 0 && modelSelectionMode !== 'single' && (
      <>
        {mode === 'fresh' && !isRemoteWithoutProvisioning && (
          <div style={{ marginBottom: 'var(--spacing-md)' }}>
            <label ...>Repository</label>
            <select ...>...</select>
          </div>
        )}
        <div>
          <button ... onClick={() => setModelSelectionMode('single')}>Single agent</button>
        </div>
        {/* Grid (multiple or advanced) */}
        ...
      </>
    )}

    {/* Branch input (unchanged) */}
    ...
  </div>
)}
```

**Step 4: Run the test to verify it passes**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage.agent-select.test.tsx`
Expected: PASS

**Step 5: Run full test suite**

Run: `./test.sh`
Expected: All tests pass.

**Step 6: Commit**

```
/commit
```

---

### Task 3: Expand Quick Launch Items in Slash Autocomplete

Replace the single `/quick` entry with individual `/quick <name>` entries in the PromptTextarea commands list.

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:945-949` (commands prop)
- Modify: `assets/dashboard/src/lib/quicklaunch.ts` (optional: add helper)

**Step 1: Write a failing test**

Add to `SpawnPage.agent-select.test.tsx` (or a new file `SpawnPage.slash-commands.test.tsx`):

```tsx
describe('Quick launch in slash autocomplete', () => {
  it('passes individual quick launch names as /quick <name> commands', () => {
    // Render in workspace mode with quick launch items configured
    renderSpawnPageInWorkspace({
      quickLaunch: ['dev:web', 'test:unit'],
    });
    // The PromptTextarea should receive commands including '/quick dev:web' and '/quick test:unit'
    // but NOT '/quick' by itself
    const textarea = screen.getByTestId('spawn-prompt');
    // Type / to trigger autocomplete
    // ... verify the autocomplete shows /quick dev:web and /quick test:unit
  });
});
```

The exact test approach depends on whether we test the commands prop or the rendered autocomplete menu. Testing the commands prop via a spy/mock on PromptTextarea is simpler and more reliable.

**Step 2: Run the test to verify it fails**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage.slash-commands.test.tsx`
Expected: FAIL

**Step 3: Replace `/quick` with expanded items in commands array**

In `SpawnPage.tsx` around line 945, change:

```tsx
// Before:
commands={[
  ...commandTargets.map((t) => t.name),
  '/resume',
  ...(mode === 'workspace' ? ['/quick'] : []),
]}

// After:
commands={[
  ...commandTargets.map((t) => t.name),
  '/resume',
  ...(mode === 'workspace'
    ? getQuickLaunchItems(
        (config?.quick_launch || []).map((ql) => ql.name),
        currentWorkspace?.quick_launch || []
      ).map((item) => `/quick ${item.name}`)
    : []),
]}
```

Import `getQuickLaunchItems` at the top if not already imported.

**Step 4: Run the test to verify it passes**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage.slash-commands.test.tsx`
Expected: PASS

**Step 5: Run full test suite**

Run: `./test.sh`
Expected: All tests pass.

**Step 6: Commit**

```
/commit
```

---

### Task 4: Slash Commands Auto-Engage

Make selecting a slash command from autocomplete immediately submit the form instead of switching to a mode UI.

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:622-639` (`handleSlashCommandSelect`)
- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:647-689` (`handleEngage` — needs to handle direct command/resume/quick spawns)
- Modify: `assets/dashboard/src/components/PromptTextarea.tsx:4-12` (add `onAutoEngage` callback or modify `onSelectCommand`)

**Step 1: Write a failing test**

```tsx
describe('Slash commands auto-engage', () => {
  it('selecting /resume immediately triggers spawn', async () => {
    const user = userEvent.setup();
    renderSpawnPage();
    // Select an agent first
    const agentSelect = screen.getByTestId('agent-select');
    await user.selectOptions(agentSelect, 'claude');
    // Type /resume in textarea
    const textarea = screen.getByTestId('spawn-prompt');
    await user.type(textarea, '/resume');
    // Select from autocomplete (simulated)
    // The spawn API should be called with resume: true
    // ...
  });

  it('selecting a command immediately triggers spawn', async () => {
    // Similar — selecting /deploy should immediately call spawnSessions with command
  });

  it('selecting /quick dev:web immediately triggers spawn', async () => {
    // Similar — should call spawnSessions with quick_launch_name: 'dev:web'
  });
});
```

**Step 2: Run the test to verify it fails**

Expected: FAIL — slash commands still just switch mode.

**Step 3: Modify `handleSlashCommandSelect` to auto-engage**

Replace the current `handleSlashCommandSelect` (lines 622-639) with a function that directly triggers spawning:

```tsx
const handleSlashCommandSelect = useCallback(async (command: string) => {
  if (engagePhase !== 'idle') return;

  if (command === '/resume') {
    // Resume with currently selected agent
    const selectedTargets: Record<string, number> = {};
    Object.entries(targetCounts).forEach(([name, count]) => {
      if (count > 0) selectedTargets[name] = count;
    });

    if (Object.keys(selectedTargets).length === 0) {
      toastError('Select an agent first');
      return;
    }

    setEngagePhase('spawning');
    try {
      const actualRepo = mode === 'fresh' ? repo : '';
      const defaultBranch = repo ? (defaultBranchMap[repo] || 'main') : 'main';
      const response = await spawnSessions({
        repo: actualRepo,
        branch: defaultBranch,
        prompt: '',
        nickname: '',
        targets: selectedTargets,
        workspace_id: prefillWorkspaceId || '',
        resume: true,
      });
      // ... standard success/error/navigation handling (same as handleEngage)
    } catch (err) { ... }
    return;
  }

  if (command.startsWith('/quick ')) {
    const quickName = command.slice('/quick '.length);
    setEngagePhase('spawning');
    try {
      const response = await spawnSessions({
        repo: '',
        branch: '',
        prompt: '',
        nickname: '',
        targets: {},
        workspace_id: prefillWorkspaceId || '',
        quick_launch_name: quickName,
      });
      // ... standard success/error/navigation handling
    } catch (err) { ... }
    return;
  }

  // Command target (e.g., /deploy)
  setEngagePhase('spawning');
  try {
    const response = await spawnSessions({
      repo: '',
      branch: '',
      prompt: '',
      nickname: '',
      targets: { [command]: 1 },
      workspace_id: prefillWorkspaceId || '',
    });
    // ... standard success/error/navigation handling
  } catch (err) { ... }
}, [engagePhase, targetCounts, repo, mode, defaultBranchMap, prefillWorkspaceId, ...]);
```

Extract the spawn-result-handling logic (success check, navigation, error toasts) into a helper to avoid duplication with `handleEngage`. Something like:

```tsx
const handleSpawnResult = (response: SpawnResult[]) => {
  const hasSuccess = response.some((r) => !r.error);
  if (!hasSuccess) {
    const errors = response.filter((r) => r.error).map((r) => r.error);
    const unique = [...new Set(errors)];
    toastError(`Spawn failed: ${unique.join('; ')}`);
    setEngagePhase('idle');
    return;
  }
  clearSpawnDraft(urlWorkspaceId);
  const successfulResults = response.filter((r) => !r.error);
  if (successfulResults.length === 1 && successfulResults[0].session_id) {
    setPendingNavigation({ type: 'session', id: successfulResults[0].session_id });
  } else if (successfulResults.length > 0) {
    const workspaceId = successfulResults[0].workspace_id;
    if (workspaceId) {
      setPendingNavigation({ type: 'workspace', id: workspaceId });
    }
  }
  setEngagePhase('waiting');
};
```

**Step 4: Run the test to verify it passes**

Run: `cd assets/dashboard && npx vitest run src/routes/SpawnPage.slash-commands.test.tsx`
Expected: PASS

**Step 5: Remove dead code**

With slash commands auto-engaging, the following UI code is no longer reachable:

- The command mode card (lines 956-978) — commands never switch to `spawnMode === 'command'`
- The quick mode card (lines 979-1007) — quick never switches to `spawnMode === 'quick'`
- The "Prompt" button for returning from command/quick mode (in the bottom action bar)
- The `handlePromptMode` function (lines 641-645)
- The `selectedCommand` state (if only used for the command dropdown)
- The `selectedQuickLaunch` state (if only used for the quick dropdown)
- The `quickLaunchSelectRef` ref

Keep `spawnMode` state for now (it's used in draft persistence and resume mode still renders differently), but the command/quick branches can be cleaned up.

**Step 6: Run full test suite**

Run: `./test.sh`
Expected: All tests pass. Update any tests that reference the removed command/quick mode UI.

**Step 7: Commit**

```
/commit
```

---

### Task 5: Update Scenario Tests

The existing scenario tests reference UI patterns that changed (e.g., "switch to Multiple agent selection mode").

**Files:**

- Modify: `test/scenarios/spawn-single-session.md` (if needed)
- Modify: `test/scenarios/spawn-multiple-agents.md` (update to use new unified dropdown)

**Step 1: Update `spawn-multiple-agents.md`**

Change the verification about switching to "Multiple" mode. Instead of switching a mode selector, the user now selects "Multiple agents" from the unified agent dropdown.

**Step 2: Run scenario tests**

Run: `./test.sh --scenarios`
Expected: All scenarios pass.

**Step 3: Commit**

```
/commit
```

---

### Task 6: Manual Smoke Test

Not automatable — verify the three changes work together in the running app.

**Step 1: Start dev mode**

Run: `./dev.sh`

**Step 2: Test single agent flow**

- Navigate to /spawn
- Verify: single dropdown with agents + "Multiple agents" + "Advanced" at bottom
- Select an agent — verify it selects
- Verify: repo dropdown is on the same row (in fresh mode)

**Step 3: Test multi/advanced mode**

- Select "Multiple agents" from dropdown
- Verify: dropdown replaced by toggle grid + "Single agent" button
- Verify: repo moved to its own row
- Click "Single agent" button — verify dropdown returns
- Select "Advanced" — verify counter grid appears

**Step 4: Test slash command auto-engage**

- Type `/` in prompt — verify autocomplete shows commands and `/quick <name>` items (in workspace mode)
- Select `/resume` — verify form submits immediately (or shows toast if no agent selected)
- Select a command — verify form submits immediately

**Step 5: Verify nothing else broke**

- Full promptable spawn flow still works
- Branch suggestion still works
- Workspace mode spawning still works
- Remote host spawning still works
