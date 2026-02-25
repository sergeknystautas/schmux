# Collapsed Persona Dropdown Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Collapse persona dropdown into the same row as Agent (and Repo), removing labels for a cleaner spawn form.

**Architecture:** Replace the 2-column grid layout with flexbox rows. Agent, Persona, and Repo dropdowns appear in a single row with equal widths. Persona is conditionally shown based on availability. Branch input spans full width below when needed.

**Tech Stack:** React, TypeScript, CSS flexbox

---

## Task 1: Refactor Fresh + Single Agent Mode Layout

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:935-1023`

**Step 1: Replace grid with flex row for fresh + single agent mode**

Find the section starting at line 935 (the "Single agent + fresh mode" branch). Replace the entire block:

```tsx
                  {mode === 'fresh' && !isRemoteWithoutProvisioning ? (
                    /* Single agent + fresh mode: agent and repo side-by-side */
                    <>
                      <label
                        className="form-group__label"
                        style={{ marginBottom: 0, whiteSpace: 'nowrap' }}
                      >
                        Agent
                      </label>
                      <div
                        data-testid="agent-repo-row"
                        style={{
                          display: 'flex',
                          gap: 'var(--spacing-md)',
                          alignItems: 'flex-start',
                        }}
                      >
                        <select
                          className="select"
                          data-testid="agent-select"
                          value={
                            promptableList.find((item) => (targetCounts[item.name] || 0) > 0)
                              ?.name || ''
                          }
                          onChange={(e) => {
                            const val = e.target.value;
                            if (val === '__multiple__') {
                              setModelSelectionMode('multiple');
                            } else if (val === '__advanced__') {
                              setModelSelectionMode('advanced');
                            } else if (val) {
                              toggleAgent(val);
                            } else {
                              const selected = promptableList.find(
                                (item) => (targetCounts[item.name] || 0) > 0
                              );
                              if (selected) toggleAgent(selected.name);
                            }
                          }}
                          style={{ width: '50%', flexShrink: 0 }}
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
                        <div style={{ flex: 1 }}>
                          <select
                            id="repo"
                            className="select"
                            required
                            value={repo}
                            data-testid="spawn-repo-select"
                            onChange={(event) => {
                              setRepo(event.target.value);
                              if (event.target.value !== '__new__') {
                                setNewRepoName('');
                              }
                            }}
                          >
                            <option value="">Select repository...</option>
                            {repos.map((item) => (
                              <option key={item.url} value={item.url}>
                                {item.name}
                              </option>
                            ))}
                            <option value="__new__">+ Create New Repository</option>
                          </select>
                        </div>
                      </div>
                      {repo === '__new__' && (
                        <div style={{ gridColumn: '1 / -1' }}>
                          <input
                            type="text"
                            id="newRepoName"
                            className="input"
                            value={newRepoName}
                            onChange={(event) => setNewRepoName(event.target.value)}
                            placeholder="Repository name"
                            required
                          />
                        </div>
                      )}
                    </>
```

Replace with:

```tsx
                  {mode === 'fresh' && !isRemoteWithoutProvisioning ? (
                    /* Single agent + fresh mode: agent, persona (if available), repo in one row */
                    <div style={{ gridColumn: '1 / -1' }}>
                      <div
                        data-testid="agent-repo-row"
                        style={{
                          display: 'flex',
                          gap: 'var(--spacing-md)',
                          alignItems: 'flex-start',
                        }}
                      >
                        <select
                          className="select"
                          data-testid="agent-select"
                          value={
                            promptableList.find((item) => (targetCounts[item.name] || 0) > 0)
                              ?.name || ''
                          }
                          onChange={(e) => {
                            const val = e.target.value;
                            if (val === '__multiple__') {
                              setModelSelectionMode('multiple');
                            } else if (val === '__advanced__') {
                              setModelSelectionMode('advanced');
                            } else if (val) {
                              toggleAgent(val);
                            } else {
                              const selected = promptableList.find(
                                (item) => (targetCounts[item.name] || 0) > 0
                              );
                              if (selected) toggleAgent(selected.name);
                            }
                          }}
                          style={{ flex: 1 }}
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
                        {personas.length > 0 && (
                          <select
                            className="select"
                            data-testid="persona-select"
                            value={selectedPersonaId}
                            onChange={(e) => setSelectedPersonaId(e.target.value)}
                            style={{ flex: 1 }}
                          >
                            <option value="">No persona</option>
                            {personas.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.icon} {p.name}
                              </option>
                            ))}
                          </select>
                        )}
                        <select
                          id="repo"
                          className="select"
                          required
                          value={repo}
                          data-testid="spawn-repo-select"
                          onChange={(event) => {
                            setRepo(event.target.value);
                            if (event.target.value !== '__new__') {
                              setNewRepoName('');
                            }
                          }}
                          style={{ flex: 1 }}
                        >
                          <option value="">Select repository...</option>
                          {repos.map((item) => (
                            <option key={item.url} value={item.url}>
                              {item.name}
                            </option>
                          ))}
                          <option value="__new__">+ Create New Repository</option>
                        </select>
                      </div>
                      {repo === '__new__' && (
                        <input
                          type="text"
                          id="newRepoName"
                          className="input"
                          value={newRepoName}
                          onChange={(event) => setNewRepoName(event.target.value)}
                          placeholder="Repository name"
                          required
                          style={{ marginTop: 'var(--spacing-sm)' }}
                        />
                      )}
                    </div>
```

**Step 2: Run existing tests to verify no regressions**

Run: `./test.sh --quick`
Expected: All tests pass

---

## Task 2: Refactor Workspace + Single Agent Mode Layout

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:1024-1067`

**Step 1: Replace grid with flex row for workspace + single agent mode**

Find the section at lines 1024-1067 (the "Single agent + workspace mode" branch). Replace:

```tsx
                  ) : (
                    /* Single agent + workspace mode: agent alone */
                    <>
                      <label
                        className="form-group__label"
                        style={{ marginBottom: 0, whiteSpace: 'nowrap' }}
                      >
                        Agent
                      </label>
                      <select
                        className="select"
                        data-testid="agent-select"
                        value={
                          promptableList.find((item) => (targetCounts[item.name] || 0) > 0)?.name ||
                          ''
                        }
                        onChange={(e) => {
                          const val = e.target.value;
                          if (val === '__multiple__') {
                            setModelSelectionMode('multiple');
                          } else if (val === '__advanced__') {
                            setModelSelectionMode('advanced');
                          } else if (val) {
                            toggleAgent(val);
                          } else {
                            const selected = promptableList.find(
                              (item) => (targetCounts[item.name] || 0) > 0
                            );
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
                    </>
                  )}
```

Replace with:

```tsx
                  ) : (
                    /* Single agent + workspace mode: agent and persona (if available) in one row */
                    <div style={{ gridColumn: '1 / -1' }}>
                      <div
                        style={{
                          display: 'flex',
                          gap: 'var(--spacing-md)',
                          alignItems: 'flex-start',
                        }}
                      >
                        <select
                          className="select"
                          data-testid="agent-select"
                          value={
                            promptableList.find((item) => (targetCounts[item.name] || 0) > 0)?.name ||
                            ''
                          }
                          onChange={(e) => {
                            const val = e.target.value;
                            if (val === '__multiple__') {
                              setModelSelectionMode('multiple');
                            } else if (val === '__advanced__') {
                              setModelSelectionMode('advanced');
                            } else if (val) {
                              toggleAgent(val);
                            } else {
                              const selected = promptableList.find(
                                (item) => (targetCounts[item.name] || 0) > 0
                              );
                              if (selected) toggleAgent(selected.name);
                            }
                          }}
                          style={{ flex: 1 }}
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
                        {personas.length > 0 && (
                          <select
                            className="select"
                            data-testid="persona-select"
                            value={selectedPersonaId}
                            onChange={(e) => setSelectedPersonaId(e.target.value)}
                            style={{ flex: 1 }}
                          >
                            <option value="">No persona</option>
                            {personas.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.icon} {p.name}
                              </option>
                            ))}
                          </select>
                        )}
                      </div>
                    </div>
                  )}
```

**Step 2: Run tests to verify**

Run: `./test.sh --quick`
Expected: All tests pass

---

## Task 3: Move Persona for Multiple/Advanced Modes

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:1256-1287`

**Step 1: Move persona selector below agent grid in multiple/advanced modes**

The persona selector at lines 1262-1287 needs to only appear in multiple/advanced modes (after the agent grid). Currently it appears in all modes.

Find the persona selector block:

```tsx
{
  /* Persona selector */
}
{
  personas.length > 0 && (
    <>
      <label
        htmlFor="persona-select"
        className="form-group__label"
        style={{ marginBottom: 0, whiteSpace: 'nowrap' }}
      >
        Persona
      </label>
      <select
        id="persona-select"
        className="select"
        data-testid="persona-select"
        value={selectedPersonaId}
        onChange={(e) => setSelectedPersonaId(e.target.value)}
      >
        <option value="">No persona</option>
        {personas.map((p) => (
          <option key={p.id} value={p.id}>
            {p.icon} {p.name}
          </option>
        ))}
      </select>
    </>
  );
}
```

Remove this entire block (it's now handled inline in single-agent mode).

**Step 2: Add persona selector inside the multiple/advanced mode block**

Find the closing of the advanced mode block (around line 1255-1257). After the agent grid but before the closing `</div>`, add:

```tsx
                    {modelSelectionMode === 'advanced' && (
                      <div ...>
                        ...existing advanced grid...
                      </div>
                    )}

                    {/* Persona selector for multiple/advanced modes */}
                    {personas.length > 0 && (
                      <select
                        className="select"
                        data-testid="persona-select"
                        value={selectedPersonaId}
                        onChange={(e) => setSelectedPersonaId(e.target.value)}
                        style={{ marginTop: 'var(--spacing-md)', width: '100%', maxWidth: '300px' }}
                      >
                        <option value="">No persona</option>
                        {personas.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.icon} {p.name}
                          </option>
                        ))}
                      </select>
                    )}
                  </div>
```

**Step 3: Run tests to verify**

Run: `./test.sh --quick`
Expected: All tests pass

---

## Task 4: Update Branch Input Layout

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx:1289-1308`

**Step 1: Move branch input outside the grid**

The branch input currently uses grid positioning. Since we're collapsing dropdowns into flex rows, the branch should appear as a full-width row below.

Find the branch input block:

```tsx
          {/* Branch (shown on suggestion failure or when suggestion is disabled) */}
          {mode === 'fresh' && !isRemoteWithoutProvisioning && showBranchInput && (
            <>
              <label
                htmlFor="branch"
                className="form-group__label"
                style={{ marginBottom: 0, whiteSpace: 'nowrap' }}
              >
                Branch
              </label>
              <input
                type="text"
                id="branch"
                className="input"
                value={branch}
                onChange={(event) => setBranch(event.target.value)}
                placeholder="e.g. feature/my-branch"
              />
            </>
          )}
        </div>
```

Replace with:

```tsx
          {/* Branch (shown on suggestion failure or when suggestion is disabled) */}
          {mode === 'fresh' && !isRemoteWithoutProvisioning && showBranchInput && (
            <div style={{ gridColumn: '1 / -1' }}>
              <input
                type="text"
                id="branch"
                className="input"
                value={branch}
                onChange={(event) => setBranch(event.target.value)}
                placeholder="Branch (e.g. feature/my-branch)"
                style={{ width: '100%' }}
              />
            </div>
          )}
        </div>
```

**Step 2: Run tests to verify**

Run: `./test.sh --quick`
Expected: All tests pass

---

## Task 5: Update Existing Tests for New Layout

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.agent-select.test.tsx`

**Step 1: Add test for persona in fresh mode dropdown row**

Add a new test to verify persona appears in the same row as agent and repo:

```tsx
it('shows persona dropdown in same row as agent and repo when personas exist', async () => {
  // Mock getPersonas to return personas
  vi.mock('../lib/api', () => ({
    ...vi.importActual('../lib/api'),
    getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
    getPersonas: vi.fn().mockResolvedValue({
      personas: [{ id: 'builder', name: 'Builder', icon: '🏗️' }],
    }),
    spawnSessions: vi.fn(),
    getErrorMessage: (_err: unknown, fallback: string) => fallback,
    suggestBranch: vi.fn(),
  }));

  renderSpawnPage();

  await waitFor(() => {
    expect(screen.getByTestId('agent-select')).toBeInTheDocument();
  });

  // Persona should be in the same row
  expect(screen.getByTestId('persona-select')).toBeInTheDocument();

  // All three should be in the agent-repo-row container
  const row = screen.getByTestId('agent-repo-row');
  expect(within(row).getByTestId('agent-select')).toBeInTheDocument();
  expect(within(row).getByTestId('persona-select')).toBeInTheDocument();
  expect(within(row).getByTestId('spawn-repo-select')).toBeInTheDocument();
});
```

**Step 2: Run tests**

Run: `./test.sh --quick`
Expected: All tests pass (new test may need adjustment based on actual mock setup)

---

## Task 6: Manual Verification

**Step 1: Start dev server**

Run: `./dev.sh`

**Step 2: Verify layouts in browser**

1. Navigate to `/spawn` (fresh mode)
   - [ ] Agent, Persona, Repo appear in one row (if personas configured)
   - [ ] Without personas: Agent, Repo appear in one row

2. Navigate to `/spawn?workspace_id=xxx` (workspace mode)
   - [ ] Agent, Persona appear in one row (if personas configured)
   - [ ] Without personas: Agent fills full width

3. Switch to "Multiple agents" mode
   - [ ] Repo row appears first (fresh mode)
   - [ ] Agent grid appears
   - [ ] Persona appears below the grid

4. Switch to "Advanced" mode
   - [ ] Same layout as multiple mode

5. Disable branch suggestion (or let it fail)
   - [ ] Branch input appears full-width below dropdowns

**Step 3: Commit changes**

Run: `/commit`
