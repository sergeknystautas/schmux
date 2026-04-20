VERDICT: NEEDS_REVISION

## Summary Assessment

The revision substantively addresses every Round 1 issue, often with the correct line-cited fixes (renaming `Nickname` → `Label`, carving out the empty-branch checks at both backend gates, enumerating five branch-touching UI elements plus the LLM call site, and proposing a shared display helper). However, three new issues surface that need to be resolved before approval: the proposed display fallback (`label → branch → id`) violates the original requirement for sapling workspaces (will display "main" instead of the workspace ID after the substitution), the display-site enumeration still misses two render sites (`RepofeedPage.tsx`, `OverlayPage.tsx`), and the spec leaves the workspace-mode-with-`workspace_label` semantics undefined.

## Round 1 Issues — Status

1. **Naming collision (`Nickname` overlap).** FIXED. New field is `Label` (Go) / `label` (JSON) on `state.Workspace`, plus `WorkspaceLabel` / `workspace_label` distinct from the existing `SpawnRequest.Nickname`. Vocabulary section explicitly forbids `nickname`/`name`/`display_name`. No collision in `state.go:300`, `contracts/sessions.go:9`, or `contracts/spawn_request.go:8`.

2. **Backend rejects empty branch.** FIXED for fresh-mode-local; correctly identifies both gates (`handlers_spawn.go:134-137` and `manager.go:416`). Workspace mode and remote sapling are correctly NOT impacted (verified — workspace-mode skips the handler check at line 128, then `resolveWorkspace` uses `GetByID` which never calls `ValidateBranchName`; remote spawns skip both the handler check and `GetOrCreate` entirely via `SpawnRemote`).

3. **Display-site count.** PARTIALLY FIXED. Spec lists 4 sites and adds a shared helper, but missed two more: `assets/dashboard/src/routes/RepofeedPage.tsx:90` (`{isShared && summary ? summary : ws.branch}`) and `assets/dashboard/src/routes/OverlayPage.tsx:586` (`<option>{ws.branch} ({ws.id.slice(0, 8)})</option>`). Both render `ws.branch` as a user-facing identifier and would show "main" for sapling workspaces post-substitution. See Critical Issue 1.

4. **Five branch UI elements.** FIXED. Table at lines 120-124 covers all five elements with correct line numbers (verified each). Element 4's claim "workspace mode reads VCS off the workspace, not the repo" is correct — `SpawnPage.tsx:237` resolves `currentWorkspace = workspaces?.find(...)`, and `WorkspaceResponseItem.vcs` exists at `contracts/sessions.go:65` and `types.generated.ts:889`.

5. **LLM short-circuit.** PARTIALLY FIXED. Element 6 in the table covers the fresh-mode call site (`SpawnPage.tsx:835/838`) but ignores the second `generateBranchName` call at `SpawnPage.tsx:864` (workspace mode + `createBranch`). The spec relies on element 4 (hiding the checkbox) to make `createBranch` always false for sapling, which is correct in practice — but the spec should explicitly state the reasoning, or add an `isSapling` guard to the line-864 conditional defensively. Currently element 6's wording only describes element 1's call site.

## Critical Issues (must fix)

### 1. Display-site enumeration is still incomplete (and the helper signature can't satisfy the original UX)

The spec lists 4 display sites. Two more exist that the spec misses:

- `assets/dashboard/src/routes/RepofeedPage.tsx:90` — `{isShared && summary ? summary : ws.branch}` shown in the repofeed intent body.
- `assets/dashboard/src/routes/OverlayPage.tsx:586` — `<option key={ws.id} value={ws.id}>{ws.branch} ({ws.id.slice(0, 8)})</option>` in the workspace selector dropdown.

Both render `ws.branch` directly as a user-facing label, so they will display the substituted "main" for sapling workspaces unless the helper is applied.

Worse, the helper as written (`return ws.label?.trim() || ws.branch || ws.id`) cannot satisfy the original requirement for sapling workspaces. The user requirement reads: "if blank, the workspace should fall back to its on-disk auto-generated ID." After this spec's backend change substitutes `Branch = "main"` for sapling, every label-less sapling workspace will render as "main" everywhere, including the sidebar, header, HomePage cards, RepofeedPage, and OverlayPage. That is the opposite of the requirement.

Three options to resolve:

- (a) Make the helper VCS-aware: `if (vcs === 'sapling' && !ws.label) return ws.id` (chain becomes `label → (vcs==='sapling' ? id : branch) → id`). Requires every call site to pass the VCS or use the workspace object directly (which it already does).
- (b) Don't substitute `"main"` into `state.Workspace.Branch` for sapling — instead persist `Branch == ""` and adapt downstream consumers (the sapling backend's `CreateWorkspace` template at `vcs_sapling.go:135-148` then needs a default; `handlers_sessions.go:75/79` returning `BranchURL` and `branch` need to handle empty cleanly).
- (c) Substitute `"main"` only at the boundary (sapling backend call) and leave `state.Workspace.Branch == ""` for sapling. Then the helper's existing `label || branch || id` chain naturally falls back to `id`, satisfying the requirement.

The spec explicitly chose (a)-with-side-effect by saying (line 96-100) "Keeping `Branch == "main"` for sapling preserves that default rendering until the label-aware helper replaces it." But the helper does not preserve the requirement once you trace through — the substituted "main" is now the display string.

### 2. Workspace-mode behavior with `workspace_label` is undefined

The spec adds `WorkspaceLabel` to `SpawnRequest` and says (line 104) "The dashboard spawn handler passes `req.WorkspaceLabel` through" to `Manager.create()`. But `req.WorkspaceLabel` arrives on every spawn request, including workspace-mode spawns where the workspace already exists.

In `internal/session/manager.go::resolveWorkspace` (line 826-832), workspace mode goes through `GetByID` and never reaches `Manager.create()`. So `req.WorkspaceLabel` for workspace mode is silently discarded. That is probably the intended behavior, but the spec must state it explicitly:

- Should the handler reject `workspace_label` when `WorkspaceID != ""`?
- Should it silently ignore?
- Should it overwrite the existing workspace's `Label` (rename-on-spawn semantics)?

This matters for forward-compat and test design. Today, the UI only renders the label input for fresh sapling spawns (per the spec), so a well-behaved client never sends it in workspace mode. But a CLI or third-party caller could.

### 3. Resume / command-target spawn paths leak the substituted branch instead of sending `""`

For sapling fresh spawns via `/resume` (`SpawnPage.tsx:678-727`) and command-target slash commands (`SpawnPage.tsx:756-779`), the code computes `actualBranch = branch.trim() || getDefaultBranch(actualRepo)`. With sapling input hidden, `branch` is empty, and `getDefaultBranch(actualRepo)` returns whatever is in `defaultBranchMap` for the repo (or `'main'` per `SpawnPage.tsx:221`).

That means these alternate spawn paths will send `branch: "main"` to the backend even though the spec promises (line 153-154) that "the spawn request is sent with `branch: ""` (backend now handles this for sapling)". The backend tolerates either, so this is not a correctness bug, but:

- `state.Workspace.Branch` then permanently records "main" for these workspaces (regardless of which substitution path was taken), worsening Issue 1.
- These paths also do not pass `workspace_label`, so users spawning via slash commands lose the labeling feature entirely.

The spec should either (a) include these paths in the table of changes or (b) explicitly mark them out of scope and document that `/resume` and command-target spawns will not support the workspace label.

## Suggestions (nice to have)

### 4. Helper signature note: doesn't compose cleanly with WorkspaceHeader's existing remote chain

The spec at line 169-172 says "Preserve the existing remote-aware fallback (uses hostname when `branch === repo`); the new helper layers on top: `label || existingDisplayBranchLogic`." But the proposed helper signature `workspaceDisplayLabel(ws: WorkspaceResponseItem): string` directly uses `ws.branch` — it doesn't know about the existing remote-hostname fallback (`WorkspaceHeader.tsx:177-180`, `AppShell.tsx:736-739`). Each call site must therefore not just call the helper but also keep its remote-aware logic and combine them — easy to get subtly wrong.

Consider one of:

- Pass the remote-aware computed branch into the helper: `workspaceDisplayLabel(ws, computedBranch)`.
- Move the remote-aware fallback into the helper itself.
- Document the wrapping pattern explicitly with code (`return ws.label?.trim() || computeRemoteAwareBranch(ws) || ws.id`).

### 5. Test plan should cover the substitution side effect

If the spec keeps option (b)/(c) above instead of (a), add tests:

- `state.Workspace.Branch == ""` for a fresh sapling spawn (asserts no substitution leaks into persisted state, only into the sapling backend call).

If the spec keeps option (a), add tests:

- `workspaceDisplayLabel` returns `id` (not `"main"`) for a sapling workspace with empty label and `branch == "main"`.

### 6. Implementation order — step 4 has a hidden dependency on step 3

Step 4 (spawn page) introduces the label input and removes branch validation for sapling. Without step 3 (display helper), the user sees the label input, types a label, spawns successfully — but the sidebar / cards still render `ws.branch` (which is "main") instead of the label. The label is silently discarded from the UI's perspective until step 3 ships. This is "shippable in isolation" only in the sense that it doesn't break anything; it just doesn't deliver the user-visible value. Worth noting in the order section: step 4 without step 3 is a semi-functional in-between state.

### 7. `__new__` flow is implicitly correct but should be documented

The spec proposes looking up VCS via `config.FindRepoByURL(req.Repo)`. For `__new__` (add new repo) flow, the handler at `handlers_spawn.go:182-198` registers a new repo if the URL is git-shaped (`isGitURL`). The new repo has no VCS field set (defaults to `""` → treated as git everywhere). Sapling repos cannot be added via `__new__` URL detection (only via the config UI), so by construction `req.Repo` for sapling will always resolve to an existing repo with `VCS == "sapling"`. This is correct but spec should state it so readers don't worry about the `__new__` case.

## Verified Claims

- `state.Workspace` (`internal/state/state.go:125-152`) does not currently have a `Label`/`Nickname` field — the new field is genuinely needed.
- `state.Session.Nickname` (`state.go:300`), `SessionResponseItem.Nickname` (`contracts/sessions.go:9`), and `SpawnRequest.Nickname` (`contracts/spawn_request.go:8`) all exist as session-scoped concepts. The spec's renaming to `Label` correctly avoids all three collisions.
- `internal/dashboard/handlers_spawn.go:128-138` only requires branch when both `WorkspaceID == ""` and `RemoteProfileID == ""`. Workspace-mode and remote-mode spawns already skip this check, so the spec only needs to handle the fresh-mode-local case as documented.
- `internal/workspace/manager.go::GetOrCreate` calls `ValidateBranchName(branch)` at line 416 before any sapling detection. The spec's fix (look up `findRepoByURL`, substitute `"main"` for empty branch when `repo.VCS == "sapling"`, then call `ValidateBranchName`) is correct.
- `internal/workspace/manager.go:344` already has the precedent `if repo, found := m.findRepoByURL(repoURL); found && repo.VCS == "sapling" { return "main", nil }`, so the substitution mirrors existing convention.
- `internal/workspace/vcs_sapling.go:135-148` uses `Branch` as a template variable. An empty branch would produce template output with an empty string substituted in — the `vars["Branch"]` would be `""`. The spec correctly identifies that the sapling backend needs SOME value, hence the substitution argument.
- `WorkspaceResponseItem` is the correct TypeScript type name (`assets/dashboard/src/lib/types.generated.ts:869`) and already exposes `vcs?: string` (line 889). The spec's helper signature compiles.
- `internal/dashboard/handlers_sessions.go:75` builds `BranchURL` only when `ws.RemoteBranchExists` is true — for sapling, this is never true (sapling backend doesn't set `RemoteBranchExists`), so the substituted "main" doesn't accidentally leak into a clickable git URL. Verified safe.
- All five `SpawnPage.tsx` line numbers in the spec table are correct: 1226-1238 (single-agent input), 1584-1599 (multi/advanced input), 256-260 (auto-set effect), 1632-1653 (createBranch checkbox), 584-587 (validateForm clause). LLM call site at 835/838 is correct.
- `SpawnPage.tsx:237` resolves `currentWorkspace = workspaces?.find((ws) => ws.id === resolvedWorkspaceId)`, so element 4's "reads VCS off the workspace" claim is verifiable: `currentWorkspace.vcs` is available.
- `SpawnPage.tsx:160` confirms `branch` state defaults to `''`. With the input hidden for sapling, `branch.trim()` is empty in `handleEngage`.
- `SpawnPage.tsx:864` is the second `generateBranchName` call site that the spec's element 6 does not explicitly cover. It's gated by `createBranch` which element 4 hides for sapling, so the issue is theoretical for compliant UIs but worth defensive guarding.
- `findNextWorkspaceNumber` (`manager.go:990-1008`) does fill gaps starting from 1, so the spec's caveat about the placeholder being best-effort is well-grounded.
- No code currently special-cases `branch === "main"` to detect sapling, so the substitution does not create false positives in any existing logic path (verified via grep across both Go and TypeScript).
- The proposed test files (`state_test.go`, `vcs_sapling_test.go`, `handlers_spawn_test.go`, `manager_test.go`, `SpawnPage.test.tsx`, new `workspace-display.test.ts`, Playwright scenario) are appropriate locations.
