VERDICT: NEEDS_REVISION

## Summary Assessment

The spec correctly identifies the UX gap (sapling repos have no branch label, so workspace lists collapse to opaque IDs) and proposes a sensible direction (optional workspace nickname with auto-ID fallback). However, it does not survey the existing code carefully enough: the name `nickname` is already a session-level concept on both the API and persisted state, the spawn handler currently rejects empty `branch`, and `ValidateBranchName` rejects empty branches inside `GetOrCreate` before sapling is even consulted. As written the design will collide with prior art and break sapling spawn at the validation gate.

## Critical Issues (must fix)

### 1. Name collision: `Nickname` is already a session-level concept on every layer

The design says "Add one field to `state.Workspace`: `Nickname string`" and "Mirror the field in `internal/api/contracts/sessions.go` on the workspace response struct" and "Add `nickname` to the spawn request contract" — but each of these already exists with session semantics.

- `internal/state/state.go:300` — `Session.Nickname string \`json:"nickname,omitempty"\`` (comment: "Optional human-friendly name").
- `internal/api/contracts/sessions.go:9` — `SessionResponseItem.Nickname`.
- `internal/api/contracts/spawn_request.go:8` — `SpawnRequest.Nickname` (comment: "optional human-friendly name for sessions"). Already auto-suffixed `"<nickname> (1)", "<nickname> (2)"` when multiple sessions are spawned (`internal/dashboard/handlers_spawn.go:359-363`).
- `internal/dashboard/handlers_spawn.go:243, 413` — passes `req.Nickname` into `session.SpawnOptions`.
- `assets/dashboard/src/routes/SpawnPage.tsx:163, 883` — there is already a `nickname` state and it is already wired into the spawn payload (although `setNickname` is never called from any UI element today, so the state is dead UI bound to a live API field).
- `docs/api.md:247, 446, 469-470, 962-982` — nickname is documented as a session concept with sanitization rules (`PUT /api/sessions-nickname/{sessionId}`, conflict 409, character allowlist, tmux-name derivation).

Adding a second, semantically-different `Nickname` (this time for the workspace) on the same JSON key in the same request creates an immediate schema ambiguity:

- The spawn request already has `nickname` meaning "session label, will be sanitized into the tmux session name and unique-suffixed". The design would have it ALSO mean "workspace cosmetic display label" — passed straight through to `state.Workspace.Nickname` with no sanitization or uniqueness logic.
- The workspace response already nests `SessionResponseItem.Nickname`. Adding `WorkspaceResponseItem.Nickname` puts two unrelated `nickname` fields on the same JSON shape and silently overloads frontend code that does `workspace.nickname` or grep-based refactors.

This is the single biggest problem. The design needs to either:

- pick a different field name on the workspace (`label`, `display_name`, `alias`, `caption` — anything that doesn't collide), or
- explicitly redesign `nickname` to mean "workspace label, optionally inherited as session prefix when N>1", consolidating the two concepts. That is a bigger change but coherent.

Either way, the spec must call this out and pick one. As written it acts like the field doesn't exist.

### 2. Backend rejects `branch: ""` before sapling code runs — design will not work end-to-end

The design says (line 88): "On submit, the spawn request is sent with `branch: ""` (the backend already handles this for sapling)". This is incorrect. The backend currently rejects empty branches in two places:

- `internal/dashboard/handlers_spawn.go:134-137` — `if req.Branch == "" { writeJSONError(w, "branch is required ...", http.StatusBadRequest) }` when `WorkspaceID` and `RemoteProfileID` are both empty.
- `internal/workspace/manager.go:416-418` — `GetOrCreate` calls `ValidateBranchName(branch)` first thing, and `ValidateBranchName` (`internal/workspace/git.go:40-44`) returns `ErrInvalidBranchName: branch name cannot be empty` for an empty input.

Today the only known-working sapling spawn path (`TestSaplingBackend_ManagerCreate_UsesSaplingBackend`, `vcs_sapling_test.go:328`) explicitly passes `"main"` as the branch. There is no "the backend already handles this for sapling" — sapling spawns currently always carry a non-empty branch, even if it is meaningless to sl.

Required fixes the spec must specify:

- Carve out the `req.Branch == ""` check in `handlers_spawn.go` for sapling repos (look up `repo.VCS == "sapling"` from config).
- Either skip `ValidateBranchName` for sapling in `GetOrCreate`, or make the spawn page send a placeholder branch (e.g. `"main"`, mirroring `Manager.GetDefaultBranch`'s sapling shortcut at `manager.go:344-346`) and document that `state.Workspace.Branch` is meaningless for sapling.
- Decide what `state.Workspace.Branch` and the response's `branch` field contain for a sapling workspace going forward — since the sidebar/HomePage today displays `workspace.branch` as the label, that fallback breaks immediately if the field becomes empty (see Issue 4).

### 3. Display-site count is wrong — it's at least three render sites, not "one or two"

The spec says (line 93-94): "Wherever workspaces are rendered with their identifying string (sidebar, `WorkspaceHeader`, workspace list cards), prefer `nickname` when present...". The implication is that this is small. It isn't quite — and the spec misses one site:

- `assets/dashboard/src/components/WorkspaceHeader.tsx:177-188, 210-214` — computes `displayBranch` and `displayName`, renders `displayBranch` as the primary label and `workspace.id` as the secondary `app-header__name`.
- `assets/dashboard/src/components/AppShell.tsx:736-784` — sidebar `nav-workspace__name` renders `displayBranch` (computed from `workspace.branch`).
- `assets/dashboard/src/routes/HomePage.tsx:656, 1205` — workspace cards render `<span className={styles.workspaceBranch}>{ws.branch}</span>` as the primary identifier in two separate sections (active workspaces + backburner list).

That's four call sites across three components. None is huge but they all need consistent fallback logic. The spec should at minimum enumerate them and recommend a tiny shared helper (e.g. `workspaceDisplayLabel(ws)`) to avoid drift — the sapling case is an aliasing/null-fallback rule that will end up subtly different in each render site if hand-coded.

Also note: the WorkspaceHeader already has remote-aware fallback logic for `displayBranch` (uses hostname when `branch === repo`). The new sapling branch needs to slot into that same chain without breaking remote sapling cases.

### 4. The "branch input" hide is not the only branch-related UI on the spawn page

The spec talks about hiding the branch input, but the SpawnPage actually has FIVE branch-touching elements that need consideration:

1. **Single-agent fresh mode branch input** (`SpawnPage.tsx:1226-1238`) — gated on `showBranchInput`. This is the one the spec describes.
2. **Multi/advanced fresh mode branch input** (`SpawnPage.tsx:1584-1599`) — also gated on `showBranchInput && modelSelectionMode !== 'single'`. Different DOM, separate code path. The spec doesn't mention this.
3. **`showBranchInput` auto-set effect** (`SpawnPage.tsx:256-260`) — reveals the branch input when `branch_suggest.target` is unset. For sapling this should also be skipped (no value in revealing it just to hide it).
4. **Workspace-mode "Create new branch from here" checkbox** (`SpawnPage.tsx:1632-1653`) — this calls the LLM branch suggester (line 861) and then sends the result as `new_branch`. For a sapling workspace this checkbox is meaningless (sapling has no branch). The spec's "Out of scope: surfacing nickname for git workspaces" implies workspace mode is untouched, but it doesn't address whether the checkbox should be hidden/disabled for sapling workspaces — and the LLM branch-suggester pipeline should not run for them either.
5. **`validateForm` branch check** (`SpawnPage.tsx:584-587`) — `if (mode === 'fresh' && !branchSuggestTarget && !branch.trim())`. Needs an `isSapling` exemption or it will fire incorrectly. The spec does mention skipping this; just confirm the gating clause is `(mode === 'fresh' && !isSapling && !branchSuggestTarget && !branch.trim())`.

The spec needs to address all five, not just (1) and (5).

### 5. The "branch suggestion" pipeline branches off `prompt + branchSuggestTarget` and will run for sapling

`SpawnPage.tsx:835` — `else if (prompt.trim() && branchSuggestTarget) { ... await generateBranchName(prompt) ... actualBranch = result.branch ... }`. For sapling, the spec wants `branch: ""`, but the current code path will happily call the LLM branch namer and produce a branch string anyway. The `handleEngage` flow needs an `isSapling` short-circuit alongside the input-hiding so that LLM branch suggestion doesn't run for a repo that has no branches. The spec doesn't mention this.

## Suggestions (nice to have)

### 6. Drop the "predict the next workspace ID client-side" placeholder, OR commit to the endpoint

The placeholder UX ("show `myrepo-008` so the user sees what they'll get") is a nice touch, but the spec hedges with "Initial implementation can derive it client-side by counting existing workspaces for the same repo (good enough; the daemon arbitrates conflicts on actual spawn)." Counting workspaces client-side will be wrong because:

- `findNextWorkspaceNumber` (`manager.go:992-1008`) **fills gaps** — it returns the lowest unused N, not `count+1`. If a repo has workspaces 1, 2, 4, the next is 3, not 5.
- The client only sees workspaces in the broadcast snapshot. Workspaces in `provisioning` state may or may not be visible depending on lifecycle timing.
- Concurrent spawns from different browser tabs will see the same "next" number; the daemon resolves the conflict but the placeholder shown to one user will be stale by the time the workspace is created.

Since the placeholder is purely cosmetic and only documents intent, all of this is "good enough" — but the spec should either (a) accept that the placeholder is a best-effort hint and may be off by one or two, or (b) commit to the small daemon endpoint. Don't punt the decision.

If going the endpoint route, `GET /api/repos/{name}/next-workspace-id` is fine; piggybacking on `/api/config` is not — that response is already very large and recomputing on every config fetch is wasteful.

### 7. Use a different word than "nickname" in the design even if the field is renamed

Even if you rename the field to `Label` or `DisplayName`, the prose throughout the spec calls it "nickname" or "name", which will be confusing in a codebase where `nickname` is owned by sessions. Pick a vocabulary up front (`label`, `alias`, `caption`) and use it everywhere — including the testing section.

### 8. Spec says "sapling bookmark coupling" and "commit-message coupling" are non-goals — say it the same way for sessions

The non-goals list (lines 24-32) talks about sapling internals but doesn't explicitly address the existing session-nickname feature. Given how confusing the name overlap is, a single line in the non-goals stating "this workspace label is independent of the session nickname feature, which continues to work as today" would prevent reviewers from making the same mistake the design itself made.

### 9. Backward-compatibility claim is over-stated

The spec says (line 113): "The spawn API gains an optional `nickname` field. Old clients that don't send it work fine." The field already exists and is already optional, so this claim is technically true but vacuous — and obscures the fact that the field's _meaning_ would change.

## Verified Claims (things I confirmed are correct)

- The `vcs` discriminator on the repo data returned by `getConfig()` is real: `RepoResponse.vcs?: string` exists in `assets/dashboard/src/lib/types.ts:88-93` (and is also reflected in `types.generated.ts:577, 587`). The string literal `"sapling"` is the canonical value used throughout the Go and TypeScript code (see `internal/workspace/manager.go:344, 854, 1575`, `internal/state/state.go:108, 117`, `internal/config/config.go:645, 657, 669`, `assets/dashboard/src/routes/config/WorkspacesTab.tsx:100, 104`, `assets/dashboard/src/routes/ConfigPage.tsx:1151`).
- The `repos.find(r => r.url === repo)?.vcs === "sapling"` pattern is consistent with how the codebase already detects sapling on the frontend.
- The `state.Workspace` struct (lines 125-152 of `internal/state/state.go`) does not currently have a workspace-level cosmetic label. The closest existing fields (`ID`, `Branch`, `Repo`, `Path`) are all functional, not display-only. So a new field is genuinely needed if this UX is wanted; the only argument is naming and overlap (Issue 1).
- `omitempty` correctly preserves backward compatibility for unmarshalling old state files — confirmed by the migration patterns in `state.Load()` (lines 384-414) which only initialize nil collections, no migration is required for new optional fields.
- The sapling backend at `internal/workspace/vcs_sapling.go:135-148` uses the `Branch` parameter as a template variable in `CreateWorkspace`. Today the only working sapling test passes `"main"`. So the design would have to either keep passing a placeholder string (`"main"`) at the boundary or change this signature.
- The conventions for sapling tests (`vcs_sapling_test.go`, `vcs_guard_test.go`) follow standard `internal/testing` patterns — table-driven where applicable, otherwise direct `t.Run` cases. New sapling-specific tests should live in `vcs_sapling_test.go` or a new `manager_sapling_test.go`. The existing pattern uses synthetic `cfg.SaplingCommands` with shell stubs (`mkdir`, `rm`, `echo`) — this works in CI without `sl` installed and the design's testing plan can use the same approach.
- `findNextWorkspaceNumber` correctness analysis (Issue 6): confirmed at `manager.go:990-1008` it fills gaps starting from 1.
- The pre-commit/test contract (`./test.sh`) is enforced via `/commit`. Any implementation will need to keep tests green there; the spec's testing section is reasonable but should be promoted from a list to specific test file targets given how spread-out the change ends up being (`state/state_test.go`, `workspace/vcs_sapling_test.go`, `dashboard/handlers_spawn_test.go`, `assets/dashboard/src/routes/SpawnPage.test.tsx`, plus a Playwright scenario).
