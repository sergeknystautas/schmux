# Delete Remote Branch on Post-Push Workspace Cleanup — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Delete remote branch" checkbox, checked by default, to the post-push workspace cleanup confirmation, and delete the branch from `origin` before disposing when it is checked.

**Architecture:** Five layers, bottom-up. A new `Manager.DeleteRemoteBranch` proves the remote branch is contained in the default branch and deletes it under a `--force-with-lease`, so a concurrent push can never cost commits. The `dispose-all` handler gains an optional request body and runs the deletion _before_ marking anything disposing, so a failed deletion destroys nothing. On the client, `ModalProvider` gains a checkbox-carrying confirm, and `useSync` gathers the workspace facts into one context object that decides whether to offer the checkbox.

**Tech Stack:** Go 1.x (chi router, standard `testing`), React 18 + TypeScript, Vitest + React Testing Library, git CLI.

**Spec:** `docs/superpowers/specs/2026-08-20-delete-remote-branch-on-cleanup-design.md`

## Global Constraints

- Run every command from the **repository root**, never from `assets/dashboard/`.
- Build the dashboard only with `go run ./cmd/build-dashboard`. Never `npm install`, `npm run build`, or `vite build`.
- Run frontend tests only with `./test.sh --quick` from the root. Never `cd assets/dashboard && npx vitest`.
- Never edit `assets/dashboard/src/lib/types.generated.ts`. Edit `internal/api/contracts/*.go` and run `go run ./cmd/gen-types`.
- Changes under `internal/dashboard/` and `internal/workspace/` **must** update `docs/api.md`. CI enforces this via `scripts/check-api-docs.sh`.
- Go code must be `gofmt`-clean. Run `./format.sh` before committing.
- Do not run `git commit` directly. Use the `/commit` command, which runs `./test.sh` and `./badcode.sh`. The commit steps below give the message to use.
- Dashboard CSS/markup must follow `docs/dashboard-style-guide.md`. `global.css` has **no bare `input` rules**, so a checkbox inside `.checkbox-list__item` is the only styled form. Run the `dashboard-style-check` skill before presenting UI.
- Fully-qualified `refs/heads/<branch>` destinations in every git push, matching `internal/workspace/push_commits.go:264`, so a ref is never misresolved to a tag.

---

## File Structure

| File                                                    | Responsibility                                                                                                                                                         |
| ------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/workspace/delete_remote_branch.go` (new)      | `DeleteRemoteBranch` — gate re-validation, containment proof, leased delete. New file rather than growing `push_commits.go` (already ~300 lines with a different job). |
| `internal/workspace/delete_remote_branch_test.go` (new) | Bare-remote fixture tests for the above.                                                                                                                               |
| `internal/workspace/errors.go`                          | Two new sentinel errors.                                                                                                                                               |
| `internal/api/contracts/sessions.go`                    | `DisposeWorkspaceAllRequest`.                                                                                                                                          |
| `internal/dashboard/handlers_dispose.go`                | Decode body; delete before marking disposing.                                                                                                                          |
| `assets/dashboard/src/components/ModalProvider.tsx`     | Checkbox state + `confirmWithCheckbox`.                                                                                                                                |
| `assets/dashboard/src/hooks/useSync.ts`                 | `DisposeSuggestionContext`, gate, checkbox wiring.                                                                                                                     |
| `assets/dashboard/src/components/PushCommitsModal.tsx`  | `disposeContext` prop replacing `workspacePath`.                                                                                                                       |
| `assets/dashboard/src/components/CommitHistoryDAG.tsx`  | Build the context at both call sites.                                                                                                                                  |
| `assets/dashboard/src/components/WorkspaceHeader.tsx`   | Build the context.                                                                                                                                                     |
| `assets/dashboard/src/lib/api.ts`                       | `disposeWorkspaceAll` optional body.                                                                                                                                   |
| `docs/api.md`, `docs/web.md`                            | Documentation.                                                                                                                                                         |

Tasks run bottom-up: 1–2 backend core, 3 API surface, 4 modal primitive, 5–6 client wiring, 7 docs. Tasks 1–3 leave the product working unchanged (nothing sends the new flag yet). Task 6 is the first that changes user-visible behavior.

---

### Task 1: `DeleteRemoteBranch` — gates and containment

**Files:**

- Create: `internal/workspace/delete_remote_branch.go`
- Create: `internal/workspace/delete_remote_branch_test.go`
- Modify: `internal/workspace/errors.go`

**Interfaces:**

- Consumes: `Manager.state` (`state.Workspace`), `Manager.GetDefaultBranch(ctx, repoURL) (string, error)` (`internal/workspace/manager.go:349`), `Manager.gitHasOriginRemote(ctx, dir) bool` (`internal/workspace/git.go:265`), `Manager.LockWorkspace(id) bool` / `UnlockWorkspace(id)` (`manager.go:159`, `manager.go:201`).
- Produces: `func (m *Manager) DeleteRemoteBranch(ctx context.Context, workspaceID string) error`, plus `ErrRemoteBranchNotDeletable` and `ErrRemoteBranchNotMerged`.

**Background for the implementer.** A schmux _workspace_ is a git worktree checked out on its own branch. Disposal removes the worktree. This method deletes the branch from the `origin` remote. It must never delete a branch holding commits that exist only on the remote, because those commits are unrecoverable once the ref is gone. Proving containment then deleting is a check-then-act race, so the delete carries a `--force-with-lease` on the exact SHA that was proved: if anything moved the branch in between, git rejects the push.

- [ ] **Step 1: Add the two sentinel errors**

In `internal/workspace/errors.go`, alongside the existing `ErrMalformedHash` / `ErrInvalidPushTarget` declarations:

```go
// ErrRemoteBranchNotDeletable means the workspace does not qualify for remote
// branch deletion (fork, default branch, remote host, or non-git VCS).
var ErrRemoteBranchNotDeletable = errors.New("workspace's remote branch is not deletable")

// ErrRemoteBranchNotMerged means origin/<branch> holds commits that are not on
// the default branch, so deleting it would destroy them.
var ErrRemoteBranchNotMerged = errors.New("remote branch has commits not on the default branch")
```

- [ ] **Step 2: Write the failing tests**

Create `internal/workspace/delete_remote_branch_test.go`. The fixture mirrors `connectFixture` in `internal/workspace/github_connect_test.go:20` and reuses its `gitIn` helper (same package, no import needed).

```go
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// deleteFixture builds a workspace on feature/x with a bare origin that already
// has main and feature/x. Returns the manager and the bare remote's path.
func deleteFixture(t *testing.T) (m *Manager, st *state.State, wsPath, remotePath string) {
	t.Helper()
	tmpDir := t.TempDir()
	remotePath = filepath.Join(tmpDir, "remote.git")
	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remotePath, "init", "--bare", "-b", "main")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := config.CreateDefault(filepath.Join(tmpDir, "config.json"))
	cfg.WorkspacePath = tmpDir
	cfg.Repos = []config.Repo{{Name: "talkback", URL: remotePath, BarePath: "talkback.git", DefaultBranch: "main"}}
	st = state.New(statePath, nil)

	wsPath = filepath.Join(tmpDir, "talkback-001")
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wsPath, "init", "-b", "main")
	gitIn(t, wsPath, "config", "user.email", "test@test")
	gitIn(t, wsPath, "config", "user.name", "test")
	gitIn(t, wsPath, "remote", "add", "origin", remotePath)
	gitIn(t, wsPath, "commit", "--allow-empty", "-m", "Initial commit")
	gitIn(t, wsPath, "push", "origin", "main:refs/heads/main")
	gitIn(t, wsPath, "checkout", "-b", "feature/x")
	gitIn(t, wsPath, "commit", "--allow-empty", "-m", "feature work")
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/feature/x")
	gitIn(t, wsPath, "fetch", "origin")

	st.AddWorkspace(state.Workspace{
		ID: "talkback-001", Repo: remotePath, Branch: "feature/x", Path: wsPath,
	})
	m = New(cfg, st, statePath, testLogger())
	return m, st, wsPath, remotePath
}

// remoteHas reports whether the bare remote still has refs/heads/<branch>.
func remoteHas(t *testing.T, remotePath, branch string) bool {
	t.Helper()
	out := gitIn(t, remotePath, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
	return out != ""
}

func TestDeleteRemoteBranch_RefusesWhenNotMerged(t *testing.T) {
	m, _, _, remotePath := deleteFixture(t)
	// feature/x carries a commit that never reached main.
	err := m.DeleteRemoteBranch(context.Background(), "talkback-001")
	if !errors.Is(err, ErrRemoteBranchNotMerged) {
		t.Fatalf("DeleteRemoteBranch() error = %v, want ErrRemoteBranchNotMerged", err)
	}
	if !remoteHas(t, remotePath, "feature/x") {
		t.Fatal("remote branch was deleted despite unmerged commits")
	}
}

func TestDeleteRemoteBranch_DeletesWhenMerged(t *testing.T) {
	m, _, wsPath, remotePath := deleteFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")

	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DeleteRemoteBranch() error = %v, want nil", err)
	}
	if remoteHas(t, remotePath, "feature/x") {
		t.Fatal("remote branch still present after delete")
	}
	// Section 5 of the spec: the delete prunes the local tracking ref, which is
	// what later lets cleanupLocalBranch reap the local branch during disposal.
	if err := gitErrIn(t, wsPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/feature/x"); err == nil {
		t.Fatal("refs/remotes/origin/feature/x survived the delete")
	}
}

func TestDeleteRemoteBranch_AlreadyGoneSucceeds(t *testing.T) {
	m, _, wsPath, remotePath := deleteFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")
	gitIn(t, remotePath, "update-ref", "-d", "refs/heads/feature/x")

	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DeleteRemoteBranch() on absent branch error = %v, want nil", err)
	}
}

func TestDeleteRemoteBranch_LeaseRejectsConcurrentPush(t *testing.T) {
	m, _, wsPath, remotePath := deleteFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")

	// Another clone advances feature/x after our fixture's tracking ref settled.
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", remotePath, other)
	gitIn(t, other, "config", "user.email", "other@test")
	gitIn(t, other, "config", "user.name", "other")
	gitIn(t, other, "checkout", "feature/x")
	gitIn(t, other, "commit", "--allow-empty", "-m", "concurrent work")
	gitIn(t, other, "push", "origin", "feature/x")

	// The fetch inside DeleteRemoteBranch sees the new head, which is no longer
	// contained in main, so containment refuses before the lease is even tried.
	err := m.DeleteRemoteBranch(context.Background(), "talkback-001")
	if err == nil {
		t.Fatal("DeleteRemoteBranch() succeeded despite a concurrent push")
	}
	if !remoteHas(t, remotePath, "feature/x") {
		t.Fatal("remote branch was deleted despite a concurrent push")
	}
}

func TestDeleteRemoteBranch_RefusesUnqualifiedWorkspaces(t *testing.T) {
	tests := []struct {
		name  string
		mutate func(w *state.Workspace)
	}{
		{"default branch", func(w *state.Workspace) { w.Branch = "main" }},
		{"fork branch", func(w *state.Workspace) { w.RemoteBranchIsFork = true }},
		{"remote host", func(w *state.Workspace) { w.RemoteHostID = "host-1" }},
		{"sapling", func(w *state.Workspace) { w.VCS = "sapling" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, st, _, remotePath := deleteFixture(t)
			w, _ := st.GetWorkspace("talkback-001")
			tt.mutate(&w)
			if err := st.UpdateWorkspace(w); err != nil {
				t.Fatal(err)
			}
			err := m.DeleteRemoteBranch(context.Background(), "talkback-001")
			if !errors.Is(err, ErrRemoteBranchNotDeletable) {
				t.Fatalf("DeleteRemoteBranch() error = %v, want ErrRemoteBranchNotDeletable", err)
			}
			if !remoteHas(t, remotePath, "feature/x") {
				t.Fatal("remote branch was deleted for an unqualified workspace")
			}
		})
	}
}
```

```go
// deleteWorktreeFixture is deleteFixture but with the workspace as a real git
// worktree of a base clone, which is what production workspaces are. Only this
// shape exercises cleanupLocalBranch, since that runs against the worktree base.
func deleteWorktreeFixture(t *testing.T) (m *Manager, st *state.State, basePath, wsPath, remotePath string) {
	t.Helper()
	tmpDir := t.TempDir()
	remotePath = filepath.Join(tmpDir, "remote.git")
	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remotePath, "init", "--bare", "-b", "main")

	basePath = filepath.Join(tmpDir, "base")
	gitIn(t, tmpDir, "clone", remotePath, basePath)
	gitIn(t, basePath, "config", "user.email", "test@test")
	gitIn(t, basePath, "config", "user.name", "test")
	gitIn(t, basePath, "commit", "--allow-empty", "-m", "init")
	gitIn(t, basePath, "push", "origin", "main:refs/heads/main")
	gitIn(t, basePath, "branch", "feature/x", "main")
	gitIn(t, basePath, "push", "origin", "feature/x:refs/heads/feature/x")
	gitIn(t, basePath, "fetch", "origin")

	wsPath = filepath.Join(tmpDir, "talkback-001")
	gitIn(t, basePath, "worktree", "add", wsPath, "feature/x")
	gitIn(t, wsPath, "commit", "--allow-empty", "-m", "feature work")
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/feature/x")
	gitIn(t, wsPath, "fetch", "origin")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := config.CreateDefault(filepath.Join(tmpDir, "config.json"))
	cfg.WorkspacePath = tmpDir
	cfg.Repos = []config.Repo{{Name: "talkback", URL: remotePath, BarePath: "talkback.git"}}
	st = state.New(statePath, nil)
	st.AddWorkspace(state.Workspace{
		ID: "talkback-001", Repo: remotePath, Branch: "feature/x", Path: wsPath,
	})
	m = New(cfg, st, statePath, testLogger())
	return m, st, basePath, wsPath, remotePath
}

func TestDeleteRemoteBranch_ThenDisposeReapsLocalBranch(t *testing.T) {
	m, _, basePath, wsPath, _ := deleteWorktreeFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")

	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatal(err)
	}
	// Spec section 5: with recycling off, disposal now reaps the local branch,
	// because cleanupLocalBranch no longer sees a remote branch to preserve it for.
	m.config.RecycleWorkspaces = false
	if err := m.DisposeForce(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DisposeForce() error = %v", err)
	}
	if out := gitIn(t, basePath, "branch", "--list", "feature/x"); out != "" {
		t.Fatalf("local branch survived disposal: %q", out)
	}
}

func TestDeleteRemoteBranch_RecyclingKeepsWorkspace(t *testing.T) {
	m, st, basePath, wsPath, _ := deleteWorktreeFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")
	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatal(err)
	}
	// Spec section 5: DisposeForce passes skipRecycling=false, so with recycling
	// on the workspace becomes recyclable and keeps its local branch. This spec
	// deliberately does not override that.
	m.config.RecycleWorkspaces = true
	if err := m.DisposeForce(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DisposeForce() error = %v", err)
	}
	w, found := st.GetWorkspace("talkback-001")
	if !found {
		t.Fatal("workspace was removed despite recycling being on")
	}
	if w.Status != state.WorkspaceStatusRecyclable {
		t.Fatalf("workspace status = %q, want recyclable", w.Status)
	}
	if out := gitIn(t, basePath, "branch", "--list", "feature/x"); out == "" {
		t.Fatal("local branch was deleted despite recycling being on")
	}
}
```

Add this helper to the same file — `gitIn` fails the test on error, so the negative assertion needs a variant that returns the error:

```go
// gitErrIn runs git and returns the error instead of failing the test.
func gitErrIn(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}
```

Add `"os/exec"` to the import block for it.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/workspace/ -run TestDeleteRemoteBranch -v`
Expected: FAIL to compile — `m.DeleteRemoteBranch undefined`.

- [ ] **Step 4: Implement `DeleteRemoteBranch`**

Create `internal/workspace/delete_remote_branch.go`:

```go
package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DeleteRemoteBranch deletes the workspace's branch from origin.
//
// A successful push to the default branch proves only that local HEAD landed
// there; it does not prove that every commit on origin/<branch> landed. Commits
// that live only on the remote are unrecoverable once the ref is gone, so this
// method proves containment itself and makes the proof binding with a lease:
// the delete lands only if origin/<branch> still equals the SHA whose
// containment was proved.
func (m *Manager) DeleteRemoteBranch(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	if w.RemoteBranchIsFork || w.RemoteHostID != "" || (w.VCS != "" && w.VCS != "git") || w.Branch == "" {
		return ErrRemoteBranchNotDeletable
	}

	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err != nil {
		defaultBranch = "main" // same fallback as PushCommits
	}
	if w.Branch == defaultBranch {
		return ErrRemoteBranchNotDeletable
	}

	if !m.LockWorkspace(workspaceID) {
		return ErrWorkspaceLocked
	}
	defer m.UnlockWorkspace(workspaceID)

	dir := w.Path
	if !m.gitHasOriginRemote(ctx, dir) {
		return ErrRemoteBranchNotDeletable
	}

	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	// Fetch with prune for the same reason PushCommits does: a branch already
	// deleted on the remote otherwise leaves a stale tracking ref that poisons
	// both the ancestor check and the lease.
	if out, err := run("fetch", "origin", "--prune"); err != nil {
		return fmt.Errorf("git fetch origin --prune failed: %w: %s", err, out)
	}

	remoteRef := "refs/remotes/origin/" + w.Branch
	sha, err := run("rev-parse", "--verify", "--quiet", remoteRef)
	if err != nil || sha == "" {
		// Already gone. Deletion is idempotent so a stale RemoteBranchExists
		// never blocks cleanup.
		m.logger.Info("delete-remote-branch: already absent", "workspace", workspaceID, "branch", w.Branch)
		return nil
	}

	if _, err := run("merge-base", "--is-ancestor", sha, "refs/remotes/origin/"+defaultBranch); err != nil {
		return ErrRemoteBranchNotMerged
	}

	m.logger.Info("delete-remote-branch: deleting", "workspace", workspaceID, "branch", w.Branch, "sha", sha)
	lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", w.Branch, sha)
	if out, err := run("push", lease, "origin", "--delete", "refs/heads/"+w.Branch); err != nil {
		return fmt.Errorf("failed to delete origin/%s: %w: %s", w.Branch, err, out)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/workspace/ -run TestDeleteRemoteBranch -v`
Expected: PASS, all six test functions.

If `TestDeleteRemoteBranch_RefusesUnqualifiedWorkspaces/default_branch` fails, check that `config.Repo` in this repo has a `DefaultBranch` field; if it does not, drop it from the fixture and rely on the `"main"` fallback in the implementation.

- [ ] **Step 6: Format and commit**

Run `./format.sh`, then use `/commit` with the message:

```
feat(workspace): delete a workspace's remote branch under a containment proof
```

---

### Task 2: `dispose-all` accepts and honors the flag

**Files:**

- Modify: `internal/api/contracts/sessions.go`
- Modify: `internal/dashboard/handlers_dispose.go:139-160`
- Test: `internal/dashboard/api_contract_test.go`

**Interfaces:**

- Consumes: `Manager.DeleteRemoteBranch(ctx, workspaceID) error`, `ErrRemoteBranchNotMerged`, `ErrRemoteBranchNotDeletable` from Task 1.
- Produces: `contracts.DisposeWorkspaceAllRequest{ DeleteRemoteBranch bool json:"delete_remote_branch,omitempty" }`, and `POST /api/workspaces/{id}/dispose-all` honoring it.

**Background.** `handleDisposeWorkspaceAll` (`internal/dashboard/handlers_dispose.go:139`) currently takes no body. It marks the workspace disposing, broadcasts, disposes sessions, then disposes the workspace. The deletion must run **before `MarkWorkspaceDisposing`**, so a failed deletion leaves the workspace completely untouched — nothing marked, nothing broadcast, nothing destroyed.

- [ ] **Step 1: Add the contract struct**

In `internal/api/contracts/sessions.go`, next to `RestartRequest` (around line 64):

```go
// DisposeWorkspaceAllRequest is the optional body for
// POST /api/workspaces/{id}/dispose-all. An absent or empty body decodes to the
// zero value, so existing callers that send no body keep working.
type DisposeWorkspaceAllRequest struct {
	DeleteRemoteBranch bool `json:"delete_remote_branch,omitempty"`
}
```

- [ ] **Step 2: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`
Expected: `assets/dashboard/src/lib/types.generated.ts` gains `DisposeWorkspaceAllRequest`. Do not hand-edit that file.

- [ ] **Step 3: Write the failing handler tests**

Add to `internal/dashboard/api_contract_test.go`. The chi-route-context pattern
below is copied from the existing dispose subtests at `api_contract_test.go:799`;
reuse that file's `newTestWorkspaceHandlers(server)` helper and its `st`
(`*state.State`) and `cfg` variables.

The workspace needs a real git repo with a bare origin, so add this helper to
the file:

```go
// gitAt runs git in dir, failing the test on error.
func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

// mergedWorkspaceRepo builds a workspace on feature/x with a bare origin.
// When merged is true, feature/x has also been pushed to origin/main, so the
// branch qualifies for deletion.
func mergedWorkspaceRepo(t *testing.T, merged bool) (wsPath, remotePath string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	remotePath = filepath.Join(base, "remote.git")
	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitAt(t, remotePath, "init", "--bare", "-b", "main")

	wsPath = filepath.Join(base, "ws")
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitAt(t, wsPath, "init", "-b", "main")
	gitAt(t, wsPath, "config", "user.email", "test@test")
	gitAt(t, wsPath, "config", "user.name", "test")
	gitAt(t, wsPath, "remote", "add", "origin", remotePath)
	gitAt(t, wsPath, "commit", "--allow-empty", "-m", "init")
	gitAt(t, wsPath, "push", "origin", "main:refs/heads/main")
	gitAt(t, wsPath, "checkout", "-b", "feature/x")
	gitAt(t, wsPath, "commit", "--allow-empty", "-m", "work")
	gitAt(t, wsPath, "push", "origin", "feature/x:refs/heads/feature/x")
	if merged {
		gitAt(t, wsPath, "push", "origin", "feature/x:refs/heads/main")
	}
	gitAt(t, wsPath, "fetch", "origin")
	return wsPath, remotePath
}

// remoteHasBranch reports whether the bare remote still has the branch.
func remoteHasBranch(t *testing.T, remotePath, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
	cmd.Dir = remotePath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("for-each-ref in %s: %v", remotePath, err)
	}
	return strings.TrimSpace(string(out)) != ""
}

// postDisposeAll invokes the handler with an optional JSON body.
func postDisposeAll(t *testing.T, wsH *WorkspaceHandlers, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(http.MethodPost, "/api/workspaces/"+id+"/dispose-all", nil)
	} else {
		r = httptest.NewRequest(http.MethodPost, "/api/workspaces/"+id+"/dispose-all", strings.NewReader(body))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceID", id)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	wsH.handleDisposeWorkspaceAll(rr, r)
	return rr
}
```

Then the three subtests:

```go
t.Run("dispose-all with empty body still disposes", func(t *testing.T) {
	wsPath, remotePath := mergedWorkspaceRepo(t, true)
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws-plain", Repo: remotePath, Branch: "feature/x", Path: wsPath,
	}); err != nil {
		t.Fatal(err)
	}
	rr := postDisposeAll(t, newTestWorkspaceHandlers(server), "ws-plain", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !remoteHasBranch(t, remotePath, "feature/x") {
		t.Fatal("remote branch deleted without being asked")
	}
})

t.Run("dispose-all deletes the remote branch before disposing", func(t *testing.T) {
	wsPath, remotePath := mergedWorkspaceRepo(t, true)
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws-del", Repo: remotePath, Branch: "feature/x", Path: wsPath,
	}); err != nil {
		t.Fatal(err)
	}
	rr := postDisposeAll(t, newTestWorkspaceHandlers(server), "ws-del", `{"delete_remote_branch":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if remoteHasBranch(t, remotePath, "feature/x") {
		t.Fatal("remote branch survived the delete")
	}
})

t.Run("dispose-all aborts entirely when the remote delete fails", func(t *testing.T) {
	// merged=false: feature/x has a commit that never reached main, so the
	// containment guard refuses.
	wsPath, remotePath := mergedWorkspaceRepo(t, false)
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws-abort", Repo: remotePath, Branch: "feature/x", Path: wsPath,
	}); err != nil {
		t.Fatal(err)
	}
	rr := postDisposeAll(t, newTestWorkspaceHandlers(server), "ws-abort", `{"delete_remote_branch":true}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	// This is the guarantee the whole ordering exists to provide.
	got, found := st.GetWorkspace("ws-abort")
	if !found {
		t.Fatal("workspace was removed despite the delete failing")
	}
	if got.Status == "disposing" {
		t.Fatal("workspace was marked disposing despite the delete failing")
	}
	if !remoteHasBranch(t, remotePath, "feature/x") {
		t.Fatal("remote branch was deleted despite the containment guard")
	}
})
```

Add `"os/exec"`, `"strings"`, and `"path/filepath"` to the test file's imports if absent.

- [ ] **Step 4: Run to verify they fail**

Run: `go test ./internal/dashboard/ -run TestAPIContract -v`
Expected: FAIL — the flag is ignored, so the remote branch survives and the abort case disposes anyway.

- [ ] **Step 5: Implement the handler change**

In `internal/dashboard/handlers_dispose.go`, inside `handleDisposeWorkspaceAll`, immediately after the dev-mode guard and **before** the `MarkWorkspaceDisposing` call:

```go
	// Optional body. An absent body is valid and means "dispose only".
	var req contracts.DisposeWorkspaceAllRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	// Deleting the remote branch runs first, before anything is marked or
	// destroyed: if it fails, the workspace is left completely untouched and
	// the user can retry or clear the checkbox.
	if req.DeleteRemoteBranch {
		if err := h.workspace.DeleteRemoteBranch(r.Context(), workspaceID); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, workspace.ErrRemoteBranchNotMerged) {
				status = http.StatusConflict
			}
			logging.Sub(h.logger, "workspace").Error("dispose-all remote branch delete failed",
				"workspace_id", workspaceID, "err", err)
			writeJSONError(w, err.Error(), status)
			return
		}
	}
```

Add `"errors"`, `"io"`, and the `contracts` and `workspace` package imports if they are not already present in the file.

If `h.workspace` is an interface rather than the concrete `*workspace.Manager`, add `DeleteRemoteBranch(ctx context.Context, workspaceID string) error` to that interface and to any test fake implementing it. Check the field's declared type before editing.

- [ ] **Step 6: Run to verify they pass**

Run: `go test ./internal/dashboard/ -run TestAPIContract -v`
Expected: PASS.

- [ ] **Step 7: Format and commit**

Run `./format.sh`, then `/commit` with:

```
feat(dashboard): dispose-all can delete the workspace's remote branch first
```

---

### Task 3: `ModalProvider` gains a checkbox-carrying confirm

**Files:**

- Modify: `assets/dashboard/src/components/ModalProvider.tsx`
- Test: `assets/dashboard/src/components/ModalProvider.test.tsx` (create if absent)

**Interfaces:**

- Consumes: nothing from earlier tasks.
- Produces: `confirmWithCheckbox(message: string, options: ModalOptions & { checkbox: { label: string; defaultChecked: boolean } }): Promise<{ confirmed: boolean; checked: boolean } | null>` on the value returned by `useModal()`.

**Background.** `confirm()` resolves `boolean | null` and has many callers, so its signature must not change. Add one new method beside it that resolves an object. `null` still means "user cancelled".

- [ ] **Step 1: Write the failing test**

Create or extend `assets/dashboard/src/components/ModalProvider.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ModalProvider, { useModal } from './ModalProvider';

function Harness({ onResult }: { onResult: (r: unknown) => void }) {
  const { confirmWithCheckbox } = useModal();
  return (
    <button
      onClick={async () =>
        onResult(
          await confirmWithCheckbox('All done?', {
            checkbox: { label: 'Delete remote branch origin/feature/x', defaultChecked: true },
          })
        )
      }
    >
      open
    </button>
  );
}

describe('confirmWithCheckbox', () => {
  it('renders the checkbox checked by default and reports it', async () => {
    const user = userEvent.setup();
    let result: unknown;
    render(
      <ModalProvider>
        <Harness onResult={(r) => (result = r)} />
      </ModalProvider>
    );
    await user.click(screen.getByText('open'));
    const box = screen.getByTestId('modal-checkbox') as HTMLInputElement;
    expect(box.checked).toBe(true);
    await user.click(screen.getByText('Confirm'));
    await waitFor(() => expect(result).toEqual({ confirmed: true, checked: true }));
  });

  it('reports an unchecked box', async () => {
    const user = userEvent.setup();
    let result: unknown;
    render(
      <ModalProvider>
        <Harness onResult={(r) => (result = r)} />
      </ModalProvider>
    );
    await user.click(screen.getByText('open'));
    await user.click(screen.getByTestId('modal-checkbox'));
    await user.click(screen.getByText('Confirm'));
    await waitFor(() => expect(result).toEqual({ confirmed: true, checked: false }));
  });

  it('resolves null on cancel', async () => {
    const user = userEvent.setup();
    let result: unknown = 'unset';
    render(
      <ModalProvider>
        <Harness onResult={(r) => (result = r)} />
      </ModalProvider>
    );
    await user.click(screen.getByText('open'));
    await user.click(screen.getByText('Cancel'));
    await waitFor(() => expect(result).toBeNull());
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — `confirmWithCheckbox is not a function`.

- [ ] **Step 3: Implement the checkbox**

In `assets/dashboard/src/components/ModalProvider.tsx`:

Add to `ModalBase`:

```ts
  checkbox?: { label: string; defaultChecked: boolean };
```

Add to `ModalOptions`:

```ts
  checkbox?: { label: string; defaultChecked: boolean };
```

Widen the resolve type on `ModalBase` and in `close`:

```ts
  resolve: (value: boolean | string | null | { confirmed: boolean; checked: boolean }) => void;
```

Add checkbox state next to the existing `modal` state:

```ts
const [checked, setChecked] = useState(false);
```

Add the new API method beside `confirm`:

```ts
const confirmWithCheckbox = (
  message: string,
  options: ModalOptions & { checkbox: { label: string; defaultChecked: boolean } }
) =>
  new Promise<{ confirmed: boolean; checked: boolean } | null>((resolve) => {
    setChecked(options.checkbox.defaultChecked);
    setModal({
      title: 'Confirm Action',
      message,
      confirmText: options.confirmText || 'Confirm',
      cancelText: options.cancelText !== undefined ? options.cancelText : 'Cancel',
      danger: options.danger || false,
      detailedMessage: options.detailedMessage || '',
      wide: options.wide || false,
      checkbox: options.checkbox,
      resolve: resolve as (
        value: boolean | string | null | { confirmed: boolean; checked: boolean }
      ) => void,
    });
  });
```

Include it in the memoized api:

```ts
const api = useMemo(() => ({ show, alert, confirm, prompt, confirmWithCheckbox }), []);
```

Add it to `ModalContextValue`:

```ts
confirmWithCheckbox: (
  message: string,
  options: ModalOptions & { checkbox: { label: string; defaultChecked: boolean } }
) => Promise<{ confirmed: boolean; checked: boolean } | null>;
```

In `close`, translate the boolean result for checkbox modals so callers get the object shape:

```ts
const close = (result: boolean | string | null) => {
  if (!modal) return;
  if (modal.checkbox && typeof result === 'boolean') {
    modal.resolve({ confirmed: result, checked });
  } else {
    modal.resolve(result);
  }
  setModal(null);
};
```

Render the checkbox in `modal__body`, immediately after the `detailedMessage` paragraph in the non-prompt branch. `global.css` has no bare `input` rules, so the `.checkbox-list` / `.checkbox-list__item` classes used by `PushCommitsModal` are required for it to render styled:

```tsx
{
  modal.checkbox ? (
    <div className="checkbox-list mt-sm">
      <label className="checkbox-list__item">
        <input
          type="checkbox"
          data-testid="modal-checkbox"
          checked={checked}
          onChange={(e) => setChecked(e.target.checked)}
        />
        <span>{modal.checkbox.label}</span>
      </label>
    </div>
  ) : null;
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `./test.sh --quick`
Expected: PASS, including the existing `ModalProvider` and `useSync` suites.

- [ ] **Step 5: Run the style check**

Invoke the `dashboard-style-check` skill (`.claude/skills/schmux-dashboard-style-check/SKILL.md`) against the changed markup. Fix anything it reports.

- [ ] **Step 6: Format and commit**

Run `./format.sh`, then `/commit` with:

```
feat(dashboard): ModalProvider supports a confirm with one checkbox
```

---

### Task 4: `disposeWorkspaceAll` sends the flag

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts:380-392`

**Interfaces:**

- Consumes: the `dispose-all` body from Task 2.
- Produces: `disposeWorkspaceAll(workspaceId: string, opts?: { deleteRemoteBranch?: boolean })`.

- [ ] **Step 1: Change the client function**

Replace the existing `disposeWorkspaceAll` with:

```ts
export async function disposeWorkspaceAll(
  workspaceId: string,
  opts?: { deleteRemoteBranch?: boolean }
): Promise<{ status: string; sessions_disposed: number }> {
  const response = await apiFetch(`/api/workspaces/${workspaceId}/dispose-all`, {
    method: 'POST',
    headers: { ...csrfHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ delete_remote_branch: opts?.deleteRemoteBranch ?? false }),
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to dispose workspace and sessions');
  }
  return response.json();
}
```

The second argument is optional, so the existing call at `assets/dashboard/src/components/WorkspaceHeader.tsx:199` keeps compiling and keeps its current behavior — it sends `false`.

- [ ] **Step 2: Verify nothing broke**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 3: Format and commit**

Run `./format.sh`, then `/commit` with:

```
feat(dashboard): disposeWorkspaceAll accepts a delete-remote-branch flag
```

---

### Task 5: `DisposeSuggestionContext` replaces the positional arguments

**Files:**

- Modify: `assets/dashboard/src/hooks/useSync.ts:99-160`, `:190-250`
- Modify: `assets/dashboard/src/components/PushCommitsModal.tsx`
- Modify: `assets/dashboard/src/components/CommitHistoryDAG.tsx:384`, `:945-968`
- Modify: `assets/dashboard/src/components/WorkspaceHeader.tsx`
- Test: `assets/dashboard/src/hooks/useSync.test.tsx`

**Interfaces:**

- Consumes: nothing new.
- Produces: exported `DisposeSuggestionContext`; `handleLinearSyncToMain(ctx: DisposeSuggestionContext)`; `handlePushCommits(workspaceId, opts)` where `opts.workspacePath` is replaced by `opts.disposeContext: DisposeSuggestionContext`; `PushCommitsModal` prop `disposeContext` replacing `workspacePath`.

**Background.** This task is a pure refactor: same behavior, different argument shape. It exists separately so the next task's behavior change lands against a stable signature. Both call sites already hold a `WorkspaceResponseItem` (`ws`), so building the context is a literal.

- [ ] **Step 1: Add the type and change the signatures**

In `assets/dashboard/src/hooks/useSync.ts`, above `useSync`:

```ts
/** Everything the post-push cleanup prompt needs about a workspace. */
export interface DisposeSuggestionContext {
  workspaceId: string;
  workspacePath?: string;
  branch: string;
  defaultBranch: string;
  remoteBranchExists: boolean;
  remoteBranchIsFork: boolean;
  remoteHostId?: string;
  vcs?: string;
  prNumber?: number;
}
```

Change `suggestDisposeAfterPush` to `async (ctx: DisposeSuggestionContext, summary: string)`, reading `ctx.workspaceId` and `ctx.workspacePath` where the old parameters were used. Behavior is otherwise unchanged in this task.

Change `handleLinearSyncToMain` to `async (ctx: DisposeSuggestionContext)`, using `ctx.defaultBranch` where `defaultBranch` was used and passing `ctx` straight through to `suggestDisposeAfterPush`.

In `handlePushCommits`, replace `workspacePath?: string` in the options type with `disposeContext: DisposeSuggestionContext`, and pass `opts.disposeContext` to `suggestDisposeAfterPush`.

- [ ] **Step 2: Update `PushCommitsModal`**

Replace the `workspacePath?: string` prop and its JSDoc with:

```ts
/** everything the post-push cleanup prompt needs; also skips the prompt for the live dev workspace */
disposeContext: DisposeSuggestionContext;
```

Import the type from `../hooks/useSync`, destructure `disposeContext` instead of `workspacePath`, and pass `disposeContext` in the `handlePushCommits` call in place of `workspacePath`.

- [ ] **Step 3: Update both call sites**

In `assets/dashboard/src/components/CommitHistoryDAG.tsx`, add a helper near the top of the component:

```ts
const disposeContext = (): DisposeSuggestionContext => ({
  workspaceId: ws!.id,
  workspacePath: ws!.path,
  branch: ws!.branch,
  defaultBranch: defaultBranchName,
  remoteBranchExists: ws!.remote_branch_exists,
  remoteBranchIsFork: ws!.remote_branch_is_fork,
  remoteHostId: ws!.remote_host_id,
  vcs: ws!.vcs,
  prNumber: ws!.pr_number,
});
```

Replace line 384's `await handleLinearSyncToMain(ws.id, defaultBranch, ws.path);` with `await handleLinearSyncToMain(disposeContext());`, and replace the modal's `workspacePath={ws.path}` prop with `disposeContext={disposeContext()}`.

In `assets/dashboard/src/components/WorkspaceHeader.tsx`, find the `handleLinearSyncToMain` call and replace its three arguments with the same literal, built from that component's workspace object. Use the field names exactly as above.

- [ ] **Step 4: Update the existing tests**

`assets/dashboard/src/hooks/useSync.test.tsx` calls these functions with the old signatures. Update every call to pass a context literal. A helper at the top of the file keeps it readable:

```ts
const ctx = (over: Partial<DisposeSuggestionContext> = {}): DisposeSuggestionContext => ({
  workspaceId: 'ws-1',
  workspacePath: '/tmp/ws-1',
  branch: 'feature/x',
  defaultBranch: 'main',
  remoteBranchExists: true,
  remoteBranchIsFork: false,
  ...over,
});
```

- [ ] **Step 5: Run to verify everything passes**

Run: `./test.sh --quick`
Expected: PASS. Behavior is unchanged, so no test assertions should need rewriting — only call shapes.

- [ ] **Step 6: Format and commit**

Run `./format.sh`, then `/commit` with:

```
refactor(dashboard): gather post-push cleanup inputs into one context object
```

---

### Task 6: The checkbox

**Files:**

- Modify: `assets/dashboard/src/hooks/useSync.ts` (`suggestDisposeAfterPush`)
- Test: `assets/dashboard/src/hooks/useSync.test.tsx`

**Interfaces:**

- Consumes: `confirmWithCheckbox` (Task 3), `disposeWorkspaceAll(id, opts)` (Task 4), `DisposeSuggestionContext` (Task 5).
- Produces: the user-visible behavior.

**Background.** This is the first task that changes what a user sees. The gate lives in exactly one function, so the "Push to main" button and the per-commit push modal cannot diverge.

- [ ] **Step 1: Write the failing tests**

Two existing assertions in `assets/dashboard/src/hooks/useSync.test.tsx` read
`expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1')`. This task adds a
second argument, so update both to
`toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: false })` — those tests use
`confirm`, not the checkbox, because `baseOpts` carries no eligible context.

Also update `baseOpts` (around line 76) to carry a context:

```ts
const baseOpts = {
  hash: 'a'.repeat(40),
  target: 'default' as const,
  perCommit: false,
  targetBranchName: 'main',
  headCommit: true,
  disposeContext: ctx(),
};
```

Mock `confirmWithCheckbox` alongside the existing `confirm` mock:

```ts
const confirmWithCheckbox = vi.fn();
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert, confirm, show, confirmWithCheckbox }),
}));
```

Then add the suite. It uses the file's existing `renderSync()` probe harness and
the `ctx()` helper added in Task 5:

```tsx
describe('post-push cleanup: delete remote branch', () => {
  it('offers the checkbox, checked, for an eligible workspace', async () => {
    confirmWithCheckbox.mockResolvedValue({ confirmed: true, checked: true });
    renderSync();

    await sync.handleLinearSyncToMain(ctx());

    expect(confirmWithCheckbox).toHaveBeenCalledWith(
      expect.stringContaining('Are you done?'),
      expect.objectContaining({
        checkbox: { label: 'Delete remote branch origin/feature/x', defaultChecked: true },
      })
    );
    await waitFor(() =>
      expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: true })
    );
    expect(navigate).toHaveBeenCalledWith('/');
  });

  it('names the PR in the label when one is open', async () => {
    confirmWithCheckbox.mockResolvedValue({ confirmed: true, checked: true });
    renderSync();

    await sync.handleLinearSyncToMain(ctx({ prNumber: 123 }));

    expect(confirmWithCheckbox).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        checkbox: {
          label: 'Delete remote branch origin/feature/x (closes PR #123)',
          defaultChecked: true,
        },
      })
    );
  });

  it('passes deleteRemoteBranch false when the user clears the box', async () => {
    confirmWithCheckbox.mockResolvedValue({ confirmed: true, checked: false });
    renderSync();

    await sync.handleLinearSyncToMain(ctx());

    await waitFor(() =>
      expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: false })
    );
  });

  it.each([
    ['no remote branch', { remoteBranchExists: false }],
    ['fork branch', { remoteBranchIsFork: true }],
    ['on the default branch', { branch: 'main' }],
    ['remote host workspace', { remoteHostId: 'host-1' }],
    ['sapling workspace', { vcs: 'sapling' }],
  ])('falls back to a plain confirm when %s', async (_name, over) => {
    confirm.mockResolvedValue(true);
    renderSync();

    await sync.handleLinearSyncToMain(ctx(over));

    expect(confirm).toHaveBeenCalled();
    expect(confirmWithCheckbox).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: false })
    );
  });

  it('does not dispose when the user cancels', async () => {
    confirmWithCheckbox.mockResolvedValue(null);
    renderSync();

    await sync.handleLinearSyncToMain(ctx());

    expect(confirmWithCheckbox).toHaveBeenCalled();
    expect(disposeWorkspaceAll).not.toHaveBeenCalled();
  });
});
```

`handleLinearSyncToMain` calls the mocked `linearSyncToMain`, so set it to
resolve `{ success: true, branch: 'main', success_count: 2 }` in this suite's
`beforeEach`, mirroring how `successResult()` feeds the push tests. The
top-level `vi.mock('../lib/api', ...)` already stubs it as `vi.fn()`; capture it
the same way the other mocks are captured so it can be given a value.

- [ ] **Step 2: Run to verify they fail**

Run: `./test.sh --quick`
Expected: FAIL — `confirmWithCheckbox` is never called; `disposeWorkspaceAll` receives one argument.

- [ ] **Step 3: Implement the gate**

In `assets/dashboard/src/hooks/useSync.ts`, replace the `confirm(...)` block at the end of `suggestDisposeAfterPush` with:

```ts
const message = `${summary} Are you done? Shall I dispose this workspace and sessions?`;

// The branch is deletable only when it exists on origin (not a fork), is
// not the default branch, and lives in a local git workspace. The backend
// re-checks all of this and holds the real safety property; this gate only
// decides whether to offer the choice.
const canDeleteRemoteBranch =
  ctx.remoteBranchExists &&
  !ctx.remoteBranchIsFork &&
  ctx.branch !== ctx.defaultBranch &&
  !ctx.remoteHostId &&
  (!ctx.vcs || ctx.vcs === 'git');

let disposeConfirmed = false;
let deleteRemoteBranch = false;

if (canDeleteRemoteBranch) {
  const label = ctx.prNumber
    ? `Delete remote branch origin/${ctx.branch} (closes PR #${ctx.prNumber})`
    : `Delete remote branch origin/${ctx.branch}`;
  const result = await confirmWithCheckbox(message, {
    checkbox: { label, defaultChecked: true },
  });
  disposeConfirmed = result?.confirmed ?? false;
  deleteRemoteBranch = disposeConfirmed && (result?.checked ?? false);
} else {
  disposeConfirmed = (await confirm(message)) ?? false;
}

if (disposeConfirmed) {
  await disposeWorkspaceAll(ctx.workspaceId, { deleteRemoteBranch });
  navigate('/');
}
```

Add `confirmWithCheckbox` to the `useModal()` destructure at the top of `useSync`, and to the `useCallback` dependency array for `suggestDisposeAfterPush`.

The existing `alert` path in `handleLinearSyncToMain` and `handlePushCommits` already catches and surfaces the error when `disposeWorkspaceAll` rejects, so a refused deletion reaches the user with the backend's message. No new error handling is needed.

- [ ] **Step 4: Run to verify they pass**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `./test.sh`
Expected: PASS. `--quick` skips typecheck and is not sufficient here.

- [ ] **Step 6: Format and commit**

Run `./format.sh`, then `/commit` with:

```
feat(dashboard): offer deleting the remote branch when cleaning up after a push
```

---

### Task 7: Documentation

**Files:**

- Modify: `docs/api.md`
- Modify: `docs/web.md`

**Background.** `scripts/check-api-docs.sh` fails CI when `internal/dashboard/` or `internal/workspace/` changes without a `docs/api.md` update. Tasks 1 and 2 both touched those packages.

- [ ] **Step 1: Document the endpoint**

Find the `dispose-all` entry in `docs/api.md` and add its request body, matching the surrounding entries' formatting:

- Body (optional): `{"delete_remote_branch": bool}`. Absent or empty body means `false`.
- When true, the branch is deleted from `origin` **before** anything is disposed. The deletion refuses unless `origin/<branch>` is contained in `origin/<default>`, and carries a `--force-with-lease` on the SHA it proved, so a concurrent push rejects the delete rather than losing commits.
- `409 Conflict` — `origin/<branch>` has commits not on the default branch.
- `400 Bad Request` — the workspace does not qualify (fork branch, default branch, remote host, non-git VCS, no origin remote).
- On any deletion failure nothing is disposed and the workspace is left untouched.

- [ ] **Step 2: Document the UX**

In `docs/web.md`, in the section covering the push/cleanup flow, add:

> After a full push to the default branch, the cleanup prompt offers **Delete remote branch**, checked by default, when the workspace's branch exists on `origin`, is not a fork branch, is not the default branch, and is a local git workspace. When an open PR is known, the label names it, because deleting the branch closes the PR. Confirming with the box checked deletes `origin/<branch>` before disposing; if that deletion fails, nothing is disposed. With `recycle_workspaces` off, the local branch is removed along with the worktree; with recycling on, the workspace becomes recyclable and keeps its local branch.

- [ ] **Step 3: Verify the docs check passes**

Run: `./scripts/check-api-docs.sh`
Expected: exit 0.

- [ ] **Step 4: Run everything**

Run: `./test.sh` and `./badcode.sh`
Expected: both PASS.

- [ ] **Step 5: Commit**

`/commit` with:

```
docs: document delete-remote-branch on workspace cleanup
```

---

## Verification

The feature is done when all of these hold:

- [ ] `./test.sh` passes (not `--quick`).
- [ ] `./badcode.sh` passes.
- [ ] `./scripts/check-api-docs.sh` exits 0.
- [ ] A workspace whose branch is fully merged into the default branch shows the checkbox; confirming deletes `origin/<branch>` and disposes.
- [ ] A workspace whose remote branch has an unmerged commit is refused with a 409, and the workspace still exists afterward.
- [ ] A fork-branch workspace, a workspace on the default branch, a remote-host workspace, and a Sapling workspace all show the prompt with no checkbox.
- [ ] The standalone Dispose button in `WorkspaceHeader` still disposes without touching the remote branch.
