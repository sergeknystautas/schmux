# Delete Remote Branch on Post-Push Workspace Cleanup

Date: 2026-08-20
Status: Draft — awaiting review

## Problem

After a full push to the default branch, schmux asks whether to dispose the
workspace and its sessions. Disposal removes the worktree and, when no remote
branch exists, the local branch. The remote branch on GitHub survives. Stale
branches accumulate on the remote, and the user deletes them by hand.

## Goal

Add a **Delete remote branch** checkbox, checked by default, to the post-push
cleanup confirmation. When the user confirms with the box checked, schmux
deletes the branch from `origin` and then disposes the workspace.

## Non-goals

- Deleting branches that live on a fork.
- Deleting branches for remote-host workspaces or Sapling workspaces.
- A config setting for the checkbox default. It is checked, always.
- Retroactive cleanup of branches left by workspaces disposed earlier.
- The standalone Dispose button on the workspace header
  (`assets/dashboard/src/components/WorkspaceHeader.tsx:199`), which calls
  `disposeWorkspaceAll` directly. It keeps today's behavior and never deletes a
  remote branch. The divergence is deliberate: the post-push prompt fires at a
  moment that establishes the branch's work has landed on the default branch,
  and that fact is what makes deleting the branch safe. A dispose from the
  header carries no such evidence.
- Closing or merging the pull request. GitHub closes an open PR as a side
  effect of deleting its head branch; schmux does not act on the PR directly.

## Current behavior

`suggestDisposeAfterPush` in `assets/dashboard/src/hooks/useSync.ts:99` runs the
post-push flow. Two call sites reach it:

- `handleLinearSyncToMain`, behind the "Push to main" button
  (`assets/dashboard/src/components/CommitHistoryDAG.tsx:384`, and
  `assets/dashboard/src/components/WorkspaceHeader.tsx`).
- `handlePushCommits`, when the per-commit push modal pushes the branch head to
  the default branch (`assets/dashboard/src/hooks/useSync.ts:238`).

The flow honors `notifications.suggest_dispose_after_push`, refuses to suggest
disposing the workspace that is live in dev mode, calls
`confirm()` from `ModalProvider`, and on confirmation calls
`disposeWorkspaceAll(workspaceId)` and navigates home.

`ModalProvider.confirm()` resolves `boolean | null`. It has no checkbox.

`POST /api/workspaces/{id}/dispose-all` (`internal/dashboard/server.go:993`)
takes no request body.

`cleanupLocalBranch` (`internal/workspace/worktree.go:130`) deletes the local
branch during disposal, but returns early when the remote branch still exists.

## Design

Ten files change for one checkbox. The count comes from the prompt's position:
it is raised by a hook, reached from two components, and its confirmation
crosses the HTTP boundary into a git operation. Six of the ten files pass a
value through; four hold new logic. Section 9 gives the per-file reason.

Both surfaces that raise the prompt, the "Push to main" button and the
per-commit push modal, call `suggestDisposeAfterPush`. The gate, the label, and
the checkbox default live in that one function, so the two surfaces cannot
diverge.

### 1. Gate: when the checkbox appears

The dashboard already receives every fact the gate needs on
`WorkspaceResponseItem` over `/ws/dashboard`. Show the checkbox when all of
these hold:

| Condition                   | Reason                                                                                                                    |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `remote_branch_exists`      | Nothing to delete otherwise.                                                                                              |
| `!remote_branch_is_fork`    | The branch lives on a fork, not `origin`.                                                                                 |
| `branch !== default_branch` | Never offer to delete the default branch.                                                                                 |
| `!remote_host_id`           | Remote-host workspaces have no local path to run git in, matching the guard at `internal/dashboard/handlers_diff.go:102`. |
| `vcs` is `git` or absent    | `git push --delete` has no Sapling equivalent.                                                                            |

When any condition fails, the confirmation renders exactly as it does today.

The checkbox label names the branch: `Delete remote branch origin/<branch>`.
When `pr_number` is set, the label becomes
`Delete remote branch origin/<branch> (closes PR #123)`, so the user sees the
consequence before confirming. The box starts checked in both cases.

**The backend re-checks every condition in this table.** The client asks for a
deletion; it does not name a branch and does not authorize one.

### 2. Modal primitive

`ModalProvider.confirm()` resolves `boolean | null` and has many callers, so its
signature does not change. Instead:

- `ModalState` gains `checkbox?: { label: string; defaultChecked: boolean }`.
- `ModalContextValue` gains one method:
  `confirmWithCheckbox(message, options): Promise<{ confirmed: boolean; checked: boolean } | null>`.

The modal renders the checkbox between the message and the footer, inside the
existing chrome and focus trap. `show`, `alert`, `confirm`, and `prompt` keep
their current shapes.

### 3. Frontend plumbing

`suggestDisposeAfterPush(workspaceId, summary, workspacePath?)` needs five more
facts. Rather than grow to seven positional arguments, it takes one object,
exported from `useSync.ts`:

```ts
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

Both call sites hold a `WorkspaceResponseItem` and build the context inline.
Three consequences:

- `handleLinearSyncToMain(workspaceId, defaultBranch, workspacePath)` becomes
  `handleLinearSyncToMain(ctx)`.
- `PushCommitsModal`'s `workspacePath?: string` prop becomes
  `disposeContext: DisposeSuggestionContext`, passed through `handlePushCommits`
  options in place of `workspacePath`.
- `disposeWorkspaceAll(workspaceId)` in `assets/dashboard/src/lib/api.ts:380`
  gains a second argument, `opts?: { deleteRemoteBranch?: boolean }`, and sends
  a JSON body when `opts` is present.

The argument count drops relative to today.

### 4. Backend

**`internal/workspace/delete_remote_branch.go`** (new) holds
`DeleteRemoteBranch(ctx, workspaceID) error`. A successful push to the default
branch proves only that local HEAD landed there. It does not prove that every
commit on `origin/<branch>` landed, so deleting on that evidence alone can
destroy commits that exist nowhere else. The method therefore proves
containment itself and makes the proof binding:

1. `git fetch origin --prune`, then `git rev-parse --verify refs/remotes/origin/<branch>`
   records `SHA`. No such ref means the branch is already gone; return nil.
   `PushCommits` fetches with prune for exactly this reason
   (`internal/workspace/push_commits.go:99`): a branch already deleted on the
   remote otherwise leaves a stale tracking ref that poisons both the ancestor
   check and the lease.
2. `git merge-base --is-ancestor <SHA> refs/remotes/origin/<default>`. A
   non-zero exit means `origin/<branch>` carries commits that are not on the
   default branch. Refuse, naming the branch.
3. `git push --force-with-lease=refs/heads/<branch>:<SHA> origin --delete refs/heads/<branch>`.

Step 2 reads local refs only. The lease in step 3 makes its conclusion binding
at the remote: the delete lands only if
`origin/<branch>` still equals the `SHA` whose containment was proved. If
anything moved the branch in the meantime, git rejects the push and nothing is
deleted. The fetch in step 1 is what makes the common case succeed rather than
trip the lease on stale local state; it is not what makes the operation safe. Fully-qualified `refs/heads/` destinations follow `push_commits.go:264`
so a ref is never misresolved to a tag.

Before step 1 the method re-validates the gate from section 1 against
`state.Workspace` and returns a typed error naming the guard that tripped. The
frontend gate is cosmetic; this method holds the safety property.

The delete is idempotent by step 1, not by git's tolerance of missing refs: a
stale `remote_branch_exists` resolves to "already gone, success".

**`internal/api/contracts`** gains:

```go
// DisposeWorkspaceAllRequest is the optional body for
// POST /api/workspaces/{id}/dispose-all.
type DisposeWorkspaceAllRequest struct {
	DeleteRemoteBranch bool `json:"delete_remote_branch,omitempty"`
}
```

An absent or empty body decodes to the zero value, so existing callers keep
working.

**`handleDisposeWorkspaceAll`** (`internal/dashboard/handlers_dispose.go:139`)
decodes the body and, when `DeleteRemoteBranch` is true, calls
`DeleteRemoteBranch` **before `MarkWorkspaceDisposing`**. Ordering carries the
error semantics: if the deletion fails, the handler returns the error and stops.
No workspace was marked, no broadcast went out, no session was disposed, and the
worktree still exists. The user retries or clears the checkbox. On success the
existing dispose-all path runs unchanged.

The delete push runs while the worktree exists and while the branch is checked
out in it. A spike confirmed git permits this.

### 5. Local branch deletion follows, except under recycling

`cleanupLocalBranch` keeps the local branch whenever
`refs/remotes/origin/<branch>` still resolves. A spike confirmed that the
leased delete prunes that ref, and that the prune is visible from the worktree
base, which is the directory `cleanupLocalBranch` inspects. Disposal therefore
deletes the local branch as well, with no new code.

**This holds only when `recycle_workspaces` is false.** `DisposeForce` passes
`skipRecycling=false` (`internal/workspace/manager.go:1487`), so with recycling
on, `dispose` marks the workspace recyclable and returns at
`internal/workspace/manager.go:1634`, never reaching `cleanupLocalBranch`. The
worktree and its local branch are preserved as a reservation for reuse.

This spec does not override recycling. Under recycling the remote branch is
deleted and the recyclable worktree keeps its local branch. No lifecycle
invariant changes: `cleanupLocalBranch` is never consulted on that path, so its
"keep the local branch while the remote exists" rule is not being violated,
merely bypassed as it already is today.

When recycling is off, checking the box removes the branch from the remote
**and** from the local clone. That is a real behavior change, not an incidental
one. Section 7 asserts both outcomes so neither regresses silently.

### 6. Error handling

| Case                                                                            | Result                                                                                                                                                                                                                |
| ------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deletion rejected by the remote (protected branch, missing permission, network) | HTTP error carrying git's stderr. Nothing marked, nothing disposed, workspace intact.                                                                                                                                 |
| Branch already absent on the remote                                             | Succeeds. The delete is idempotent (section 4), so a stale `remote_branch_exists` never blocks cleanup. Disposal proceeds.                                                                                            |
| A gate condition fails server-side                                              | HTTP 400 naming the condition. Nothing disposed.                                                                                                                                                                      |
| Deletion succeeds, disposal fails                                               | Existing dispose-all error path. The remote branch is gone; the workspace remains, with `remote_branch_exists` briefly stale until the next git status refresh (`internal/workspace/git_watcher.go:429`) corrects it. |

The frontend surfaces the error through the existing `alert` path in
`handleLinearSyncToMain` and `handlePushCommits`.

State ownership is unchanged. `internal/workspace` remains the only writer of
workspace git state; the handler orchestrates and never mutates it directly.

### 7. Testing

**Go, `internal/workspace`** — `DeleteRemoteBranch` against a bare-repo fixture,
following `internal/workspace/github_connect_test.go`:

- Deletes the branch on the remote and returns nil.
- Prunes `refs/remotes/origin/<branch>`, observed from the worktree base. This
  is the section 5 assertion.
- Returns nil when the remote branch is already gone, proving idempotence.
- Refuses, without pushing, when `origin/<branch>` holds a commit that is not
  on `origin/<default>`. This is the containment guard.
- Refuses when the remote branch moves after the SHA is observed, proving the
  lease binds.
- With `recycle_workspaces` false, the local branch is gone after disposal;
  with it true, the workspace is recyclable and the local branch survives.
  These are the section 5 assertions.
- Refuses, without pushing, for a fork branch, the default branch, a
  remote-host workspace, and a Sapling workspace.

**Go, `internal/dashboard`** — handler behavior, following the dispose cases in
`internal/dashboard/api_contract_test.go:800`:

- An empty body disposes exactly as today.
- `delete_remote_branch: true` deletes, then disposes.
- When the deletion fails, assert the workspace is still present and **not**
  marked disposing, and that no session was disposed.

**Vitest** — `ModalProvider` renders the checkbox, respects `defaultChecked`,
and resolves `{ confirmed, checked }`. `useSync.test.tsx` covers the gate matrix
from section 1, the PR-number label, and that `disposeWorkspaceAll` receives
`deleteRemoteBranch` matching the checkbox.

### 8. Documentation

`docs/api.md` gains the `dispose-all` request body. CI enforces this for changes
under `internal/dashboard/` and `internal/workspace/`
(`scripts/check-api-docs.sh`). `docs/web.md` gains a sentence on the cleanup
prompt.

Run `go run ./cmd/gen-types` after adding the contract struct.

### 9. Files touched

| File                                                   | Change                                          |
| ------------------------------------------------------ | ----------------------------------------------- |
| `assets/dashboard/src/components/ModalProvider.tsx`    | Checkbox state, `confirmWithCheckbox`           |
| `assets/dashboard/src/hooks/useSync.ts`                | `DisposeSuggestionContext`, gate, checkbox call |
| `assets/dashboard/src/components/PushCommitsModal.tsx` | `disposeContext` prop                           |
| `assets/dashboard/src/components/CommitHistoryDAG.tsx` | Build context at both call sites                |
| `assets/dashboard/src/components/WorkspaceHeader.tsx`  | Build context                                   |
| `assets/dashboard/src/lib/api.ts`                      | `disposeWorkspaceAll` body                      |
| `internal/api/contracts/sessions.go`                   | `DisposeWorkspaceAllRequest`                    |
| `internal/workspace/push_commits.go`                   | `DeleteRemoteBranch`                            |
| `internal/dashboard/handlers_dispose.go`               | Decode body, delete before marking              |
| `docs/api.md`, `docs/web.md`                           | Documentation                                   |

### 10. Risks

- **Deleting a branch closes its open PR.** The label names the PR number, and
  the box defaults to checked. A user who pushes to the default branch while a
  PR is open and confirms without reading closes that PR. The label is the
  mitigation.
- **The gate depends on cached state.** `remote_branch_exists` comes from the
  workspace git status refresh, not a live remote query, so the checkbox can
  appear for a branch someone already deleted on GitHub. Idempotent deletion
  (section 4) makes this harmless: the push succeeds and disposal proceeds.
- **A session can recreate the branch after a successful delete.** Deletion runs
  before sessions are stopped, so an agent whose push lands after the delete
  recreates the branch on the remote. The lease covers the opposite order: a
  push landing before the delete rejects it. The residue of the remaining case
  is a stale remote branch, never lost data, since the recreated branch holds
  whatever the agent pushed. Stopping sessions first would close it, at the cost
  of making a failed deletion a partially-cleaned workspace. Keeping every
  destructive step behind a successful deletion is worth more than eliminating a
  leftover branch.
- **Abort-on-failure destroys nothing, but it also finishes nothing.** A user
  who lacks delete permission on a protected branch sees the dispose refused
  outright rather than partially applied. Clearing the checkbox completes the
  cleanup. This is the deliberate trade for never disposing a workspace whose
  requested cleanup only half happened.
