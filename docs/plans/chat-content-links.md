# Chat Content Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make agent-emitted workspace-file citations reliable across content types while preserving React navigation and authenticated browser fallbacks.

**Architecture:** Keep `/jump/{workspaceId}/{filepath}` and the `kind: "file"` tab API as the two navigation surfaces, backed by one server-side target validator that now includes VCS-ignore checks. Keep chat URL normalization in `fileNavigation.ts`, and put the agent-facing citation rule in the existing schmux-managed instruction template.

**Tech Stack:** Go 1.x with `net/http`, React/TypeScript, React Markdown, Vitest/React Testing Library, and the existing Go/VCS command builder.

**Spec:** `docs/specs/chat-content-links.md`

## Global Constraints

- The canonical browser fallback is exactly `/jump/{workspaceId}/{filepath}`.
- Contract-conformant citations are URI-encoded absolute filesystem paths or equivalent `file://` URLs beneath the current workspace.
- Relative links and every HTTP(S) URL remain unchanged, including guessed dashboard URLs and dashboard-origin `/jump/...` URLs.
- Ordinary unmodified clicks navigate through the tab API and React Router; modifier clicks, middle clicks, and copied links use the authenticated `/jump/...` fallback.
- `/jump` and `kind: "file"` reject traversal, remote workspaces, missing files, directories, non-regular files, symlinked path components, targets outside the workspace, and VCS-ignored targets before navigation.
- A VCS-ignore check failure returns `500`; a confirmed ignored target returns `403`.
- Extension routing remains: `.md`/`.mdx` to Markdown, `.mmd` to Mermaid, `.png`/`.jpg`/`.jpeg`/`.webp`/`.gif` to image, `.html` to HTML, and everything else to the diff viewer with the file selected.
- Downloads continue through the existing `/api/file/...?download=1` endpoint.
- Run every command from the repository root.
- Do not run frontend tests with `npx vitest`; use `./test.sh --quick` during development and `./test.sh` for final verification.
- Do not commit, merge, push, stash, or rewrite git history. The user owns git operations.

---

### Task 1: Share VCS-Ignore Validation

**Files:**

- Modify: `internal/dashboard/handlers_file_jump.go`
- Modify: `internal/dashboard/handlers_tabs.go`
- Modify: `internal/dashboard/handlers_diff.go`
- Test: `internal/dashboard/handlers_file_jump_test.go`
- Test: `internal/dashboard/handlers_tabs_test.go`
- Modify: `docs/api.md`

**Interfaces:**

- Consumes: `validateWorkspaceFileTarget(store state.StateStore, workspaceID, filePath string) *workspaceFileTargetError`, `GitHandlers.fileMatchesVCSIgnore`, and `localShellRun`.
- Produces: `validateWorkspaceFileTarget(ctx context.Context, store state.StateStore, workspaceID, filePath string) *workspaceFileTargetError`.
- Produces: package function `fileMatchesVCSIgnore(ctx context.Context, workspacePath, filePath, vcsType string) (bool, error)`.
- Produces: package function `localWorkspaceVCSType(ws state.Workspace) string`.

- [ ] **Step 1: Write failing jump and tab tests**

Add this helper and test to `internal/dashboard/handlers_file_jump_test.go`. Add `os/exec` to the imports.

```go
func newWorkspaceWithIgnoredFile(t *testing.T, st state.StateStore, workspaceID string) string {
	t.Helper()
	workspacePath := filepath.Join(t.TempDir(), workspaceID)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	files := map[string][]byte{
		".gitignore": []byte("secret.md\n"),
		"secret.md":  []byte("secret"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(workspacePath, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := st.AddWorkspace(state.Workspace{ID: workspaceID, Path: workspacePath}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	return workspacePath
}

func TestHandleFileJump_RejectsVCSIgnoredFile(t *testing.T) {
	server, _, st := newTestServer(t)
	newWorkspaceWithIgnoredFile(t, st, "ws-jump-ignored")

	req := httptest.NewRequest(http.MethodGet, "/jump/ws-jump-ignored/secret.md", nil)
	rr := httptest.NewRecorder()
	server.handleFileJump(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "file is ignored") {
		t.Fatalf("expected ignored-file error, got %s", rr.Body.String())
	}
	if rr.Header().Get("Location") != "" {
		t.Fatalf("ignored file must not redirect, got %q", rr.Header().Get("Location"))
	}
}
```

Add `strings` to that test file's imports if it is not already present.

Add this test to `internal/dashboard/handlers_tabs_test.go`:

```go
func TestHandleTabCreate_FileNavigationRejectsVCSIgnoredFile(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	newWorkspaceWithIgnoredFile(t, st, "ws-tab-ignored")

	body, _ := json.Marshal(createTabRequest{Kind: "file", Filepath: "secret.md"})
	req := makeTabRequest(
		t,
		http.MethodPost,
		"/api/workspaces/ws-tab-ignored/tabs",
		"ws-tab-ignored",
		"",
		body,
	)
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "file is ignored") {
		t.Fatalf("expected ignored-file error, got %s", rr.Body.String())
	}
	if tabs := st.GetWorkspaceTabs("ws-tab-ignored"); len(tabs) != 0 {
		t.Fatalf("ignored file created tabs: %+v", tabs)
	}
}
```

Add `strings` to that test file's imports if it is not already present.

- [ ] **Step 2: Run the tests and verify both fail**

Run:

```bash
go test ./internal/dashboard -run 'TestHandleFileJump_RejectsVCSIgnoredFile|TestHandleTabCreate_FileNavigationRejectsVCSIgnoredFile'
```

Expected: FAIL. The jump test receives `302` instead of `403`; the tab test receives `200` and creates a tab.

- [ ] **Step 3: Extract one ignore checker**

In `internal/dashboard/handlers_diff.go`, replace the `GitHandlers` method:

```go
func (h *GitHandlers) fileMatchesVCSIgnore(ctx context.Context, workspacePath, filePath, vcsType string) (bool, error) {
	cb := vcs.NewCommandBuilder(vcsType)
	run := localShellRun(ctx, workspacePath)
	_, err := run(cb.CheckIgnore(filePath))
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return false, nil
}
```

with the package function:

```go
// fileMatchesVCSIgnore checks if a local file path matches VCS ignore rules.
func fileMatchesVCSIgnore(ctx context.Context, workspacePath, filePath, vcsType string) (bool, error) {
	cb := vcs.NewCommandBuilder(vcsType)
	run := localShellRun(ctx, workspacePath)
	_, err := run(cb.CheckIgnore(filePath))
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
```

Then replace the call in `serveWorkspaceFile`:

```go
gitignoreMatches, err := h.fileMatchesVCSIgnore(ctx, ws.Path, filePath, h.vcsTypeForWorkspace(ws))
```

with:

```go
gitignoreMatches, err := fileMatchesVCSIgnore(ctx, ws.Path, filePath, h.vcsTypeForWorkspace(ws))
```

Check whether `errors` is already imported by `handlers_diff.go`; add it only if the compiler requires it.

- [ ] **Step 4: Extend the shared target validator**

In `internal/dashboard/handlers_file_jump.go`, add `context`, `time`, and `internal/vcs` imports. Change the validator signature to:

```go
func validateWorkspaceFileTarget(
	ctx context.Context,
	store state.StateStore,
	workspaceID string,
	filePath string,
) *workspaceFileTargetError {
```

After the regular-file and case-sensitive checks, and before `return nil`, add:

```go
ignoreCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

vcsType := localWorkspaceVCSType(ws)
ignored, err := fileMatchesVCSIgnore(ignoreCtx, ws.Path, filePath, vcsType)
if err != nil {
	return &workspaceFileTargetError{
		message: "failed to check ignore patterns",
		status:  http.StatusInternalServerError,
	}
}
if ignored {
	return &workspaceFileTargetError{
		message: "file is ignored by VCS",
		status:  http.StatusForbidden,
	}
}
```

Add:

```go
func localWorkspaceVCSType(ws state.Workspace) string {
	if ws.VCS != "" {
		return ws.VCS
	}
	return "git"
}
```

Update both call sites:

```go
// Server.handleFileJump
validateWorkspaceFileTarget(r.Context(), s.state, workspaceID, filePath)

// WorkspaceHandlers.handleTabCreate
validateWorkspaceFileTarget(r.Context(), h.state, workspaceID, req.Filepath)
```

- [ ] **Step 5: Update the API contract**

In `docs/api.md`, update the `GET /jump/{workspaceId}/{filepath}` section:

```markdown
Validate a content-agnostic link to a local workspace file, then redirect to the
dashboard view for that file type. The target must be an existing regular file whose
resolved path remains inside the named workspace; traversal, directories, missing files,
VCS-ignored files, and paths containing any symbolic link are rejected. This route uses
the same authentication policy as the dashboard.
```

Extend its error list with:

```markdown
- 403: target is outside the workspace, ignored by the workspace VCS, or is not a regular file
- 500: the VCS ignore check fails
```

In the `POST /api/workspaces/{workspaceID}/tabs` description for `kind: "file"`, change:

```markdown
For `file`, the same local-file, traversal, regular-file, and no-symbolic-link checks as
`GET /jump/...` apply.
```

to:

```markdown
For `file`, the same local-file, traversal, regular-file, VCS-ignore, and
no-symbolic-link checks as `GET /jump/...` apply.
```

- [ ] **Step 6: Run focused backend tests**

Run:

```bash
go test ./internal/dashboard -run 'TestHandleFileJump|TestHandleTabCreate_FileNavigation'
```

Expected: PASS.

---

### Task 2: Lock URI Decoding and Non-Repair Behavior

**Files:**

- Test: `assets/dashboard/src/lib/fileNavigation.test.ts`

**Interfaces:**

- Consumes: `resolveWorkspaceFileLink(href: string | undefined, workspaceId: string | undefined, workspacePath: string | undefined): WorkspaceFileLinkTarget | undefined`.
- Produces: no production interface change. This task locks existing behavior with regression tests.

- [ ] **Step 1: Add characterization tests**

Add these tests inside the existing `workspace file navigation` describe block:

```ts
it('decodes both citation forms once for spaces and literal percent sequences', () => {
  expect(
    resolveWorkspaceFileLink('/Users/dev/ws-1/docs/live%20ops/readme.md', 'ws-1', '/Users/dev/ws-1')
  ).toEqual({
    filePath: 'docs/live ops/readme.md',
    href: '/jump/ws-1/docs%2Flive%20ops%2Freadme.md',
  });

  expect(
    resolveWorkspaceFileLink('/Users/dev/ws-1/percent%252Fname.md', 'ws-1', '/Users/dev/ws-1')
  ).toEqual({
    filePath: 'percent%2Fname.md',
    href: '/jump/ws-1/percent%252Fname.md',
  });

  expect(
    resolveWorkspaceFileLink(
      'file:///Users/dev/ws-1/percent%252Fname.md',
      'ws-1',
      '/Users/dev/ws-1/'
    )
  ).toEqual({
    filePath: 'percent%2Fname.md',
    href: '/jump/ws-1/percent%252Fname.md',
  });
});

it.each([
  'https://12540.dashboard.sx:7337/sessions/review/steam-store-automation-spikes.html',
  'https://12540.dashboard.sx:7337/jump/ws-1/docs%2Freadme.md',
  '/sessions/review/steam-store-automation-spikes.html',
])('does not guess a workspace target from dashboard URL %s', (href) => {
  expect(resolveWorkspaceFileLink(href, 'ws-1', '/Users/dev/ws-1')).toBeUndefined();
});
```

- [ ] **Step 2: Run the frontend test wrapper**

Run:

```bash
./test.sh --quick
```

Expected: PASS. This is intentionally a regression-lock task; do not change `fileNavigation.ts` if these tests pass. Its decoder already performs the required one-pass decode.

---

### Task 3: Provision the Agent Citation Rule

**Files:**

- Modify: `internal/workspace/ensure/manager.go`
- Test: `internal/workspace/ensure/manager_test.go`
- Modify: `internal/session/manager.go`
- Test: `internal/session/manager_test.go`
- Modify: `docs/web.md`

**Interfaces:**

- Consumes: `ensure.AgentInstructions(workspacePath, targetName, repoName string) error`.
- Consumes: `detect.GetAgentInstructionConfig(toolName string) (detect.AgentInstructionConfig, bool)`.
- Produces: `func (m *Manager) ensureAgentInstructions(ws state.Workspace, baseTool string) error`.
- Produces: the managed instruction heading `## Workspace File Links`.

- [ ] **Step 1: Write failing instruction-content tests**

In `internal/workspace/ensure/manager_test.go`, extend `TestAgentInstructions_CreatesNewFile` after the `$SCHMUX_EVENTS_FILE` assertion:

```go
if !strings.Contains(string(content), "## Workspace File Links") {
	t.Error("File should contain workspace file link instructions")
}
if !strings.Contains(string(content), "Do not construct dashboard, preview, diff, session, or download URLs") {
	t.Error("File should prohibit constructed dashboard URLs")
}
```

Extend `TestAgentInstructions_UpdatesExisting` after the same existing assertion:

```go
if !strings.Contains(string(content), "## Workspace File Links") {
	t.Error("Updated instructions should contain workspace file link guidance")
}
```

Add this test to `internal/session/manager_test.go`:

```go
func TestEnsureAgentInstructions_ProvisionsHookSignalingHarnesses(t *testing.T) {
	m, st := newTestManager(t)
	tests := []struct {
		tool string
		path string
	}{
		{"claude", filepath.Join(".claude", "CLAUDE.md")},
		{"codex", filepath.Join(".codex", "AGENTS.md")},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			ws := state.Workspace{
				ID:   "ws-instructions-" + tt.tool,
				Path: t.TempDir(),
			}
			if err := st.AddWorkspace(ws); err != nil {
				t.Fatalf("add workspace: %v", err)
			}
			if err := m.ensureAgentInstructions(ws, tt.tool); err != nil {
				t.Fatalf("ensureAgentInstructions: %v", err)
			}

			content, err := os.ReadFile(filepath.Join(ws.Path, tt.path))
			if err != nil {
				t.Fatalf("read instructions: %v", err)
			}
			if !strings.Contains(string(content), "## Workspace File Links") {
				t.Fatalf("%s instructions missing workspace file links: %s", tt.tool, content)
			}
		})
	}
}
```

Use the imports already present in `manager_test.go`; `os`, `filepath`, `strings`, and `state` are all part of that file's existing import set.

- [ ] **Step 2: Run instruction tests and verify they fail**

Run:

```bash
go test ./internal/workspace/ensure ./internal/session -run 'TestAgentInstructions|TestEnsureAgentInstructions_ProvisionsHookSignalingHarnesses'
```

Expected: FAIL. The instruction text is absent, and `Manager.ensureAgentInstructions` does not yet exist.

- [ ] **Step 3: Add the managed rule**

In `internal/workspace/ensure/manager.go`, add this section to `SignalingInstructions` immediately after `## Web Preview Registration`:

```markdown
## Workspace File Links

When linking a workspace file in chat, use its absolute path as the Markdown
link target. Do not construct dashboard, preview, diff, session, or download
URLs. Schmux rewrites the path to the canonical jump route and selects the
correct viewer.

Percent-encode a literal percent sign in a filename as `%25`. For example:

[Steam store automation spike](/workspaces/bach-godot-003/review/steam-store-automation-spikes.html)
```

Because this text is inside a Go raw-string constant, copy the formatting pattern used by the surrounding `SignalingInstructions` sections; do not introduce Markdown backticks that are not already valid Go string content.

- [ ] **Step 4: Provision instruction files independently of signaling strategy**

In `internal/session/manager.go`, add this method near the other private setup helpers:

```go
func (m *Manager) ensureAgentInstructions(ws state.Workspace, baseTool string) error {
	if baseTool == "" {
		return nil
	}
	if _, ok := detect.GetAgentInstructionConfig(baseTool); !ok {
		return nil
	}
	if err := ensure.AgentInstructions(ws.Path, baseTool, ws.Repo); err != nil {
		return fmt.Errorf("provision agent instructions: %w", err)
	}
	return nil
}
```

In `Spawn`, keep the `SignalingCLIFlag` branch that writes `~/.schmux/signaling.md`, remove the `SignalingInstructionFile` special case from the signaling switch, and add this universal call after the switch:

```go
if err := m.ensureAgentInstructions(*w, baseTool); err != nil {
	m.logger.Warn("failed to provision agent instructions", "err", err)
}
```

The universal call must remain before the spawn event is written and before the process command is built.

- [ ] **Step 5: Run instruction tests**

Run:

```bash
go test ./internal/workspace/ensure ./internal/session -run 'TestAgentInstructions|TestEnsureAgentInstructions_ProvisionsHookSignalingHarnesses'
```

Expected: PASS.

- [ ] **Step 6: Update chat documentation**

In `docs/web.md`, immediately after the existing absolute-link bullet in the Chat sessions section, add:

```markdown
- Schmux-managed agent instructions require workspace file citations to use an absolute
  workspace path. They prohibit agents from constructing dashboard, preview, diff, session,
  or download URLs; percent signs in filenames are URI-encoded as `%25`, and the renderer
  decodes a citation exactly once.
```

---

### Task 4: Security Docs, Review, and Full Verification

**Files:**

- Modify: `docs/security.md`
- Modify: `docs/api.md` only if Task 1 left an inconsistency
- Modify: `docs/web.md` only if Task 3 left an inconsistency

**Interfaces:**

- Consumes: all behavior and tests from Tasks 1-3.
- Produces: verified implementation and documentation ready for user review.

- [ ] **Step 1: Document the navigation boundary**

In `docs/security.md`, extend the raw-file endpoint security notes with:

```markdown
**Jump and tab navigation use the same VCS-ignore gate as raw-file serving.**
`validateWorkspaceFileTarget` performs the workspace, traversal, symlink, regular-file,
case-sensitive existence, and VCS-ignore checks before `/jump` redirects or the tab API
creates navigation state. A confirmed ignored target returns 403; a failed ignore check
returns 500 and never redirects.
```

Place it near the existing `serveWorkspaceFile` allowlist note so navigation and raw-byte access are documented together.

- [ ] **Step 2: Format and build**

Run:

```bash
./format.sh
go build ./cmd/schmux
```

Expected: both commands succeed.

- [ ] **Step 3: Run the required test-review skill**

Read and follow `.agents/skills/test-rules-review/SKILL.md`, because Tasks 1-3 modify tests. At minimum run:

```bash
.agents/skills/test-rules-review/scan.sh --net --changed
```

Expected: an explicit pass verdict with no unresolved findings. Fix test structure before continuing if the skill reports a finding.

- [ ] **Step 4: Run full verification**

Run exactly:

```bash
./test.sh
```

Expected: PASS. Do not substitute `--quick` or `go test ./...` for this step.

Then run:

```bash
./badcode.sh
```

Expected: PASS.

- [ ] **Step 5: Review the final diff**

Run:

```bash
git status --short
git diff --check
git diff -- internal/dashboard internal/workspace/ensure internal/session assets/dashboard/src/lib/fileNavigation.test.ts docs/api.md docs/web.md docs/security.md
```

Expected:

- only files named by this plan plus the spec and plan are changed;
- no generated dashboard files are changed;
- no unrelated user edits are reverted;
- `git diff --check` prints nothing.

Report the exact commands and outcomes to the user. Do not commit.
