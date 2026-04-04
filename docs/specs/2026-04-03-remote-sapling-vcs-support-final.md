# Remote Sapling VCS Support: Commit Graph and File Diff

**Date:** 2026-04-03
**Status:** Design (final — revised after two review rounds)
**Branch:** `remote/commit-graph`

---

## Changes from v1

This section summarizes what changed based on the [review of v1](2026-04-03-remote-sapling-vcs-support-review-1.md) and [review of v2](2026-04-03-remote-sapling-vcs-support-review-2.md).

### Critical issues addressed

| Review ID | Issue                                                                                                                                                                                                                                                                                                                                                                                     | Resolution                                                                                                                                                                                                                                                                                                                    |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| C1        | Batch script exceeds terminal line limits — `RunCommand` types the entire command as keystrokes via `send-keys -l`, so a multi-KB inline script overflows shell input buffers. v2's heredoc approach also fails because `RunCommand` sends the entire command via a single `send-keys -l` call, and newlines in the heredoc body are interpreted as tmux control mode command delimiters. | Revised to **base64 encoding**: encode the batch script as base64 (no newlines), send via a single-line `RunCommand` that decodes to a temp file, then execute. See Phase 2a.                                                                                                                                                 |
| C2        | `handleRemoteDiffExternal` omitted from spec — it has the same BUG-2 and BUG-7 issues as `buildDiffResponse`                                                                                                                                                                                                                                                                              | Added Phase 2e covering `handleRemoteDiffExternal`. BUG-2 fix in Phase 1b automatically fixes the `ShowFile` mapping. BUG-1 does not apply (already uses `conn.RunCommand` for file content). O(N) sequential issue deferred.                                                                                                 |
| C3        | `ShowFile` fix description incomplete — did not mention updating the misleading comment or adding a test for the HEAD-to-`.` mapping                                                                                                                                                                                                                                                      | Phase 1b now specifies: fix the mapping, update the comment, add a test for the HEAD mapping.                                                                                                                                                                                                                                 |
| C4        | `LogRange` --limit justification factually wrong — spec claimed git's `LogRange` uses `--max-count`, but it does not                                                                                                                                                                                                                                                                      | Rewrote Phase 1e justification. Both git and Sapling `LogRange` can return unbounded results. The Sapling case is worse because server-side revset evaluation enumerates all matching commits before applying `--limit`. The `--limit` caps output volume but does not prevent expensive evaluation. Limitation acknowledged. |

### Suggestions incorporated

| Review ID | Suggestion                                                                                  | Resolution                                                                                                                                 |
| --------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| S2        | Extract range-parsing helper for `RevListCount` and `NewestTimestamp`                       | Added to Phase 1c — both methods share a `parseRangeToRevset` helper.                                                                      |
| S4        | Add per-file line cap in batch script, not just byte cap                                    | Phase 2a batch script now applies both `head -n 500` (per-file line cap) and `head -c 1048576` (per-file byte cap). Risk analysis updated. |
| S6        | Tab seeding should use `HasVCSSupport` for consistency                                      | Phase 1a now includes updating tab seeding at `state.go:390,553` to use `HasVCSSupport`.                                                   |
| S7        | BUG-10 (shared 30s context) listed but never addressed                                      | Added explicit deferral with rationale in Risks section.                                                                                   |
| S8        | Binary detection for tracked files already handled; Phase 2d only addresses untracked files | Phase 2d description narrowed to clarify it only addresses the `difftool.IsBinaryFile` gap for untracked files.                            |

### Other changes

- Fixed method count claim from 22 to 24.
- Clarified that Phase 1d (`GetDefaultBranch` fix) applies to the local Sapling backend only; the remote path already uses the correct `cb.DetectDefaultBranch()`.

---

## Problem

Remote hosts running Sapling (Meta's source control, used for large monorepos) cannot use the file diff or commit graph tabs in the dashboard. These features work for local git and remote git workspaces, but Sapling remote workspaces get either error responses or silently broken data.

The primary use case is developers working against large Sapling monorepos on remote hosts (e.g., devservers accessed via SSH). The dashboard should provide the same observability — dirty file diffs, commit history graph, commit detail — as it does for local git workspaces.

## Constraints

Two constraints that cut against each other:

1. **Must work at monorepo scale.** The remote Sapling use case coincides with large monorepos — millions of files, deep history, sparse checkouts, slow hosts. Operations that work fine on a 500-file git repo will timeout or silently truncate against a monorepo.

2. **No internal leaks.** The open-source schmux codebase must not contain internal hostnames, repo paths, directory structures, tool names, or configuration assumptions specific to any organization's monorepo. The code should be Sapling-compatible by being correct about Sapling semantics and robust at scale — not by containing special-case knowledge of any particular repo.

**Principle:** If you wouldn't put it in an open-source Sapling tutorial, it doesn't belong in the code.

The existing `RemoteProfile` + `SaplingCommands` config system already follows this principle well: all organization-specific details (workspace paths, clone commands, hostnames, VCS type) live in user config (`~/.schmux/config.json`), not in code.

---

## Current State

### What already works

The VCS abstraction layer is largely complete:

- **`CommandBuilder` interface** (`internal/vcs/vcs.go`): 24 methods, fully implemented for both `GitCommandBuilder` and `SaplingCommandBuilder`.
- **`handleRemoteCommitGraph`** (`handlers_git.go:103`): Already VCS-agnostic — uses `vcs.NewCommandBuilder(vcsType)` for all commands except one hardcoded `git log`.
- **`handleRemoteDiff`** (`handlers_diff.go:425`): Uses CommandBuilder for VCS commands.
- **`handleRemoteDiffExternal`** (`handlers_diff.go:1116`): Uses CommandBuilder for VCS commands but has the same BUG-1, BUG-2, and BUG-7 issues as `buildDiffResponse`.
- **`ParseGitLogOutput`** (`git_graph.go:531`): Filters Sapling's null-hash parent sentinels (`0000...0000`). Tested in `TestParseGitLogOutput_SaplingFormat`.
- **Tab seeding** (`state.go:553`): Already seeds diff + git tabs for `VCS == "sapling"`.
- **Frontend layout** (`gitGraphLayout.ts`): Fully data-driven — no hash format, parent count, or branch name assumptions.

### Bug inventory

Thirteen issues found across four tiers. Six are correctness/feature bugs, four are monorepo scalability issues, three are minor leaks or display issues.

#### Tier 1: Data corruption / feature completely broken

| ID    | Bug                                                                                                                                                                                                                                                                                                               | Location                   | Affects     |
| ----- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- | ----------- |
| BUG-1 | `readWorkingFile` uses local `os.ReadFile` for remote workspaces. Every modified file appears as "deleted" because new content is always empty.                                                                                                                                                                   | `handlers_diff.go:141,186` | All remote  |
| BUG-2 | `ShowFile` maps `HEAD` to `.^` instead of `.`. In Sapling, `.` is the working copy parent (equivalent to git HEAD). `.^` is the grandparent. Diffs show old content from one commit too far back. The comment at `sapling.go:18` is also wrong — it says `.^` means "parent of working copy" equating it to HEAD. | `vcs/sapling.go:20`        | All Sapling |

#### Tier 2: Features gated off

| ID    | Bug                                                                                                                                        | Location                 | Affects    |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------ | ---------- |
| BUG-3 | `IsGitVCS` gate rejects Sapling workspaces from commit graph and commit detail endpoints, even though the downstream code is VCS-agnostic. | `handlers_git.go:32,300` | Sapling    |
| BUG-4 | Commit detail returns HTTP 501 for all remote workspaces.                                                                                  | `handlers_git.go:306`    | All remote |

#### Tier 3: Hardcoded commands / display issues

| ID     | Bug                                                                                              | Location                                              | Affects                                                                    |
| ------ | ------------------------------------------------------------------------------------------------ | ----------------------------------------------------- | -------------------------------------------------------------------------- |
| BUG-5  | Hardcoded `git log --format=%aI` in remote graph handler bypasses CommandBuilder.                | `handlers_git.go:189`                                 | Sapling remote                                                             |
| BUG-6  | Frontend hardcodes `origin/` prefix in 5 tooltip/toast locations. Sapling uses `remote/` prefix. | `GitHistoryDAG.tsx:227,234,237,517`, `useSync.ts:155` | Sapling                                                                    |
| BUG-11 | `SaplingBackend.GetDefaultBranch()` hardcoded to `"main"` instead of querying `sl config`.       | `vcs_sapling.go:273`                                  | Sapling (local only — remote path already uses `cb.DetectDefaultBranch()`) |
| BUG-12 | Frontend remote profile form defaults VCS to `'sapling'` instead of `'git'`.                     | `RemoteSettingsPage.tsx:28`                           | None (cosmetic)                                                            |
| BUG-13 | Documentation uses organization-specific flavor/hostname examples (`"www"`, `"devvm"`).          | `docs/remote.md:146`, `connection.go:54,220`          | None (cosmetic)                                                            |

#### Tier 4: Monorepo scalability

| ID     | Bug                                                                                                                                                                                                                                                                                                                                                                      | Location                                                | Affects                           |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------- | --------------------------------- |
| BUG-7  | `buildDiffResponse` issues O(N) sequential `RunCommand` calls for file content. Each call takes ~600ms. 50 changed files = 30s, exceeding the 30s timeout. Fixing BUG-1 doubles the command count. Same pattern exists in `handleRemoteDiffExternal`, though with lower severity (only 2 commands per file, and typically only one file is externally diffed at a time). | `handlers_diff.go:93-209`, `handlers_diff.go:1216-1226` | All remote, amplified by monorepo |
| BUG-8  | `LogRange()` and `RevListCount()` have no `--limit`. In a monorepo with deep history, unbounded revset queries can enumerate millions of commits.                                                                                                                                                                                                                        | `vcs/sapling.go:52,93`                                  | Sapling monorepo                  |
| BUG-9  | `capture-pane -S -50000` silently truncates output. If command output exceeds 50K lines, the begin sentinel scrolls out of the capture window and `RunCommand` hangs until timeout.                                                                                                                                                                                      | `controlmode/client.go:693`                             | All remote, amplified by monorepo |
| BUG-10 | 30s shared context across all sequential `RunCommand` calls. No per-command timeout. One slow operation consumes the entire budget.                                                                                                                                                                                                                                      | `handlers_diff.go:439`, `handlers_git.go:129`           | All remote, amplified by monorepo |

---

## Design

### Decision: Batch remote file content fetching

The core scalability problem is that `buildDiffResponse` issues N sequential `RunCommand` calls (one per changed file), each taking ~600ms due to tmux window create/poll/destroy overhead. Fixing BUG-1 (remote file content) doubles this to 2N commands. Three options were considered:

**Option A: Batch script via temp file (recommended).** Generate a shell script that fetches all file content with delimited output. Write it to a temp file on the remote host via one `RunCommand`, then execute it via a second `RunCommand`. Two `RunCommand` calls instead of N.

**Option B: Parallel `RunCommand` workers.** Cap at N files, run M concurrent workers with separate tmux windows. Still N/M sequential rounds.

**Option C: Server-side `sl diff`.** Run unified diff on remote, transfer result. Simplest but `react-diff-viewer` expects old+new content strings, not unified format.

**Choice: Option A.** It reduces the problem to O(1) `RunCommand` calls regardless of file count. The inline-script variant (typing the entire script as a single `send-keys -l` command) is not viable because `RunCommand` sends the command as a single line to tmux control mode — literal newlines terminate the tmux command prematurely, and even a single-line script for 50 files exceeds practical input limits. The solution is a two-step process: base64-encode the script, decode it to a temp file on the remote host via one `RunCommand`, then execute the temp file via a second `RunCommand`. The capture-pane limit becomes the bottleneck, which is addressed independently by file count and per-file line caps. No organization-specific knowledge required — the script contains only standard `sl cat` and `cat` commands.

### Decision: File count cap

For any remote diff, cap the number of files whose content is fetched at **50 files** (configurable). The numstat summary (lines added/removed per file) is always complete. Files beyond the cap are listed with stats but shown as "content not loaded — too many files" in the diff viewer.

This provides a hard ceiling for monorepo edge cases while covering the vast majority of real workspaces. The cap is a two-way door.

### Decision: Remote ref prefix from backend

Replace hardcoded `origin/` in the frontend with a `remote_ref_prefix` field in the workspace API response. Git workspaces return `"origin/"`, Sapling returns `"remote/"`. The frontend interpolates. This keeps VCS knowledge on the backend where it belongs.

### Decision: `HasVCSSupport` predicate

Replace the `IsGitVCS` gate with a broader `HasVCSSupport(vcs string) bool` that returns true for `""`, `"git"`, `"git-worktree"`, `"git-clone"`, and `"sapling"`. `IsGitVCS` is retained for call sites that genuinely need git-only behavior (worktree operations, git-specific filesystem watches, etc.).

---

## Phased Implementation

### Phase 1: Correctness gates (small, safe)

Unblocks the commit graph for remote Sapling. No interface changes. No scalability improvements. Expected: commit graph works, diff partially works (still broken for remote file content — BUG-1 — but this is a pre-existing bug for all remote workspaces, not a regression).

**1a. Add `HasVCSSupport` predicate** — `internal/workspace/vcs.go`

```go
func HasVCSSupport(vcs string) bool {
    switch vcs {
    case "", "git", "git-worktree", "git-clone", "sapling":
        return true
    }
    return false
}
```

Replace `IsGitVCS` at `handlers_git.go:32` and `:300` with `HasVCSSupport`.

Also update tab seeding at `state.go:390` and `state.go:553` to use `HasVCSSupport` instead of the inline `vcs == "" || vcs == "git" || vcs == "sapling"` check. This ensures `git-worktree` and `git-clone` workspaces are also seeded consistently, and keeps a single source of truth for "which VCS types support the diff and commit graph tabs."

**1b. Fix `ShowFile` HEAD mapping** — `vcs/sapling.go:17-22`

Three changes:

1. **Fix the mapping:** Change `slRev = ".^"` to `slRev = "."`. In Sapling, `.` is the working copy parent (equivalent to git HEAD).
2. **Fix the comment:** Update the comment at `sapling.go:18` from "`.^` means parent of working copy, equivalent to git's HEAD" to accurately describe that `.` is the working copy parent (equivalent to git HEAD) and is the correct revision for `ShowFile("path", "HEAD")`.
3. **Add a test:** Add a test case in `vcs/vcs_test.go` that verifies `SaplingCommandBuilder.ShowFile("foo.txt", "HEAD")` produces a command with `-r '.'` (not `-r '.^'`).

**1c. Add `NewestTimestamp` to `CommandBuilder`** — `vcs/vcs.go`, `vcs/git.go`, `vcs/sapling.go`

```go
// CommandBuilder interface addition:
NewestTimestamp(rangeSpec string) string
```

Git: `git log --format=%aI -1 {rangeSpec}`
Sapling: parse the range spec (`A..B` to `only(B, A)`) then `sl log -T '{date|isodate}\n' -r 'last({revset})' --limit 1`

Replace the hardcoded `git log` at `handlers_git.go:189` with `cb.NewestTimestamp(...)`.

**Extract `parseRangeToRevset` helper:** Both `NewestTimestamp` and `RevListCount` need to parse `A..B` range notation and convert to Sapling revsets. Extract a shared helper:

```go
// parseRangeToRevset converts git-style range notation "A..B" to Sapling revset
// operands (exclude, include). Returns ("", rangeSpec) if not in A..B format.
// Maps "HEAD" to "." (Sapling's working copy parent, equivalent to git HEAD).
func parseRangeToRevset(rangeSpec string) (exclude, include string) {
    parts := strings.Split(rangeSpec, "..")
    if len(parts) != 2 {
        return "", rangeSpec
    }
    exclude, include = parts[0], parts[1]
    if exclude == "HEAD" {
        exclude = "."
    }
    return exclude, include
}
```

Refactor `RevListCount` to use this helper, and have `NewestTimestamp` use it as well.

**1d. Fix `SaplingBackend.GetDefaultBranch`** — `vcs_sapling.go:273`

This fixes the **local** Sapling backend only. The remote path already uses `cb.DetectDefaultBranch()` which correctly queries `sl config remotenames.selectivepulldefault`.

Replace the hardcoded `return "main", nil` with an actual query:

```go
func (s *SaplingBackend) GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error) {
    out, err := s.mgr.runCmd(ctx, "sl", "", "", repoBasePath,
        "config", "remotenames.selectivepulldefault")
    if err != nil || strings.TrimSpace(out) == "" {
        return "main", nil  // fallback
    }
    return strings.TrimSpace(out), nil
}
```

**1e. Add `--limit` to `LogRange`** — `vcs/sapling.go:52`

Add a `--limit 5000` to the Sapling `LogRange()`.

**Justification (corrected from v1):** Neither git's nor Sapling's `LogRange` has any built-in output bound — both can return unbounded results. The difference is severity: git's `git log --not forkPoint^` evaluates the graph locally and streams results efficiently. Sapling's revset `(forkPoint)::refs` triggers server-side evaluation that can enumerate millions of matching commits in a deep monorepo history before any output is produced. Adding `--limit 5000` caps the output volume transferred over the tmux channel, but it does **not** prevent the server-side revset evaluation from being expensive on very deep histories.

**Acknowledged limitation:** The `--limit` is a downstream cap, not an upstream optimization. A branch that diverged from a monorepo trunk with hundreds of thousands of daily commits will still trigger an expensive server-side evaluation; the limit only prevents the resulting output from overwhelming the transport. True mitigation would require revset-level bounding (e.g., date-range constraints), which is out of scope for this spec.

**1f. Change frontend form VCS default** — `RemoteSettingsPage.tsx:28`

Change `vcs: 'sapling'` to `vcs: 'git'` in the empty form data.

### Phase 2: Remote diff at scale (medium, needed for monorepo)

Fixes BUG-1 (remote file content) and BUG-7 (O(N) sequential commands) together, since fixing one without the other makes the timeout problem worse.

**2a. Add `BatchFileContent` to `CommandBuilder`** — `vcs/vcs.go`, `vcs/git.go`, `vcs/sapling.go`

```go
// New interface method:
BatchFileContent(files []BatchFileRequest) string

type BatchFileRequest struct {
    Path     string
    Revision string  // "" for working copy, "HEAD"/"."/specific hash for old content
}
```

Generates a shell script with sentinel-delimited output:

```sh
# For each file:
echo '__SCHMUX_FILE_BEGIN__:path:revision__'
{ sl cat -r . 'path' 2>/dev/null || true; } | head -n 500 | head -c 1048576
echo '__SCHMUX_FILE_END__'
```

Each file's content is capped at both **500 lines** (`head -n 500`) and **1 MB** (`head -c 1048576`) on the remote side, before transfer. The line cap prevents a single large file from consuming the capture-pane scrollback budget. The byte cap prevents transfer of massive single-line files (e.g., minified JS). Both caps are applied in the batch script itself, so the output is bounded before it enters the tmux channel.

The sentinel format uses double underscores and the path+revision to allow unambiguous parsing.

**Two-step execution via base64 + temp file:**

`RunCommand` sends the entire command via a single `send-keys -l` call to a tmux pane. The command string is written to tmux control mode stdin as a single newline-terminated line. This means:

- The command **must be a single line** — literal newlines are interpreted as tmux control mode command delimiters.
- `tmuxQuote` does not escape newlines (it only escapes `\`, `"`, and `$`).
- A heredoc-based approach does NOT work because the heredoc body contains newlines that prematurely terminate the tmux command.

The solution is **base64 encoding**: the batch script is generated in Go, base64-encoded (producing a single line with no newlines), then decoded on the remote host to a temp file. Two `RunCommand` calls:

1. **Write the script to a temp file via base64 decode.** The first `RunCommand` sends:

   ```sh
   printf '%s' 'IyEvYmluL3NoCmVjaG8gJ19fU0N...' | base64 -d > /tmp/schmux-diff-${UUID}.sh
   ```

   The base64 string is generated in Go using `base64.StdEncoding.EncodeToString(scriptBytes)` which produces no newlines. The entire command is a single line. For 50 files at ~150 bytes per file in the raw script, the base64 payload is ~10KB — well within shell and tmux input handling capabilities (tested up to 64KB in practice).

2. **Execute the temp file.** The second `RunCommand` runs:

   ```sh
   sh /tmp/schmux-diff-${UUID}.sh; rm -f /tmp/schmux-diff-${UUID}.sh
   ```

   The temp file is cleaned up as part of the execution command itself. If the second `RunCommand` times out or fails, the temp file is orphaned — but it is in `/tmp` and will be cleaned up by the OS. The UUID in the filename prevents collisions.

**2b. Add `fileContentFunc` parameter to `buildDiffResponse`**

```go
type fileContentFunc func(path string) (string, error)

func buildDiffResponse(
    run vcsRunFunc,
    cb vcs.CommandBuilder,
    getFileContent fileContentFunc,  // NEW
    workspacePath, workspaceID, repo, branch string,
    maxFiles int,                    // NEW — file count cap
) (*diffResponse, error)
```

For local: `getFileContent = func(p) { return readWorkingFile(workspacePath, p) }`
For remote: `getFileContent` calls `run(cb.FileContent(p))`, or preferably uses the batch approach where all file contents are fetched in a single `RunCommand` before entering the per-file loop.

**Batch flow for remote diff:**

```
1. run(cb.DiffNumstat())                         → 1 RunCommand
2. run(cb.UntrackedFiles())                      → 1 RunCommand
3. Combine file list, cap at maxFiles
4. Build BatchFileContent request for all files
   (old content at HEAD + new content at working copy)
5. Write batch script to temp file               → 1 RunCommand
6. Execute batch script                          → 1 RunCommand
7. Parse sentinel-delimited output
8. Assemble per-file diffs from parsed content
```

Total: 4 `RunCommand` calls regardless of file count (one more than v1's estimate due to the temp-file two-step, but still O(1)).

**2c. Add file count cap**

Default `maxFiles = 50` (configurable via `config.json`). Files beyond the cap are included in the response with stats (lines added/removed) but `old_content` and `new_content` are omitted. The frontend shows "content not loaded" for these files.

The response gains new fields:

```go
type diffResponse struct {
    // ... existing fields ...
    TotalFiles    int  `json:"total_files"`
    ContentCapped bool `json:"content_capped"`
}
```

**2d. Binary detection for untracked remote files**

Binary detection for **tracked** files is already handled: `buildDiffResponse` checks `addedStr == "-" && deletedStr == "-"` from `--numstat` output at line 114, which both git and Sapling produce for binary files. No changes needed for tracked files.

The gap is **untracked** files: `difftool.IsBinaryFile` at line 178 is called for untracked files and is local-only (it reads from the local filesystem). For remote workspaces, this call operates on a remote path via `os.Stat`/`os.Open`, which silently fails.

**Fix:** For remote workspaces, skip the `difftool.IsBinaryFile` call for untracked files. Instead, use a simpler heuristic after fetching content via the batch script: check if the fetched content contains null bytes (`\x00`), which is the standard binary detection heuristic used by git itself. This avoids needing a separate remote command for binary detection.

**2e. Address `handleRemoteDiffExternal`**

`handleRemoteDiffExternal` (`handlers_diff.go:1116-1316`) has the same bugs as `buildDiffResponse`:

- **BUG-2:** Calls `cb.ShowFile(file.path, "HEAD")` which maps to `.^` for Sapling. This is **automatically fixed** by the Phase 1b `ShowFile` mapping correction — no additional work needed in this handler.
- **BUG-1:** Calls `cb.FileContent(file.path)` via `conn.RunCommand` for working copy content. This works correctly for remote workspaces (unlike `buildDiffResponse`, which calls `readWorkingFile` locally). No fix needed.
- **BUG-7 (O(N) sequential commands):** The handler issues 2 `RunCommand` calls per changed file (old content via `ShowFile` + new content via `FileContent`), iterating over ALL files from the numstat. For a workspace with 50 changed files, that is 100 sequential `RunCommand` calls at ~600ms each = 60 seconds — exceeding the 30s timeout.

**Recommendation:** For the initial implementation, the batch approach is **not** applied to `handleRemoteDiffExternal`. The BUG-2 fix from Phase 1b is sufficient to make the handler correct for Sapling. The external diff handler is invoked on-demand for a single user action (clicking "Open in VS Code"), not on every page load like the inline diff. In practice, workspaces with 50+ changed files are uncommon, and the user can retry if the operation times out. If the timeout becomes a practical problem, the batch approach can be extended to this handler in a follow-up.

### Phase 3: Frontend polish (small, cosmetic)

**3a. Add `remote_ref_prefix` to workspace API response**

In the workspace broadcast data (and `/api/sessions` response), add:

```go
type WorkspaceResponse struct {
    // ... existing fields ...
    RemoteRefPrefix string `json:"remote_ref_prefix,omitempty"`
}
```

Populated by the backend based on VCS type: `"origin/"` for git, `"remote/"` for Sapling.

**3b. Replace hardcoded `origin/` in frontend**

In `GitHistoryDAG.tsx` and `useSync.ts`, replace `origin/${branch}` with `${workspace.remote_ref_prefix ?? 'origin/'}${branch}`. These appear in user-facing tooltip and toast strings (e.g., "Push commits to origin/main"). The substitution produces "Push commits to remote/main" for Sapling, which reads naturally.

**3c. Clean up doc examples**

Replace `"www"` and `"devvm"` in `docs/remote.md` and `connection.go` (both the `flavorStr` field comment at line 54 and the `FlavorStr` godoc at line 220) with generic examples like `"dev-server"` and `"gpu-large"`.

### Phase 4: Remote commit detail (large, lower priority)

Enables clicking on a commit in the graph to see its full diff. This requires extending the `CommandBuilder` interface with 5 new methods.

**4a. New `CommandBuilder` methods**

| Method                               | Git                                                  | Sapling                                                                                              |
| ------------------------------------ | ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `ValidateCommit(hash)`               | `git cat-file -t {hash}`                             | `sl log -T '{node}' -r {hash} --limit 1`                                                             |
| `CommitMetadata(hash)`               | `git log -1 --format=%an%x00%ae%x00%aI%x00%B {hash}` | `sl log -T '{author\|person}%x00{author\|email}%x00{date\|isodate}%x00{desc}\n' -r {hash} --limit 1` |
| `CommitParents(hash)`                | `git rev-parse {hash}^@`                             | `sl log -T '{p1node} {p2node}\n' -r {hash} --limit 1`                                                |
| `CommitDiffNumstat(parent, hash)`    | `git diff --numstat -M -C {parent}..{hash}`          | `sl diff --numstat -r {parent} -r {hash}`                                                            |
| `CommitDiffNameStatus(parent, hash)` | `git diff --name-status -M -C {parent}..{hash}`      | `sl status --change {hash}`                                                                          |

**4b. Output format normalization**

Two format divergences require normalization:

- **Timestamps:** Git `%aI` produces ISO 8601 (`2024-01-15T10:30:00+00:00`). Sapling `{date|isodate}` produces `2024-01-15 10:30 +0000`. The parser must accept both, or the Sapling builder normalizes to ISO 8601 via a sed/awk post-process.

- **Parent hashes:** Git outputs N lines (0 for root, 1 for normal, 2+ for merge). Sapling outputs exactly 2 hashes with null-hash sentinel. The consumer must filter null hashes — same pattern as `ParseGitLogOutput`.

**4c. Rename detection gap**

Git's `-M -C` flags detect renames and output `R100 oldpath newpath`. Sapling's `sl status --change` shows `A newpath` + `R oldpath` separately; rename info requires a supplementary `sl log -T '{file_copies}'` query. Two options:

- **Option A:** Accept that Sapling commit detail won't show renames. Files appear as separate add + delete. Simple, correct, no extra commands.
- **Option B:** Add a `CommitFileCopies(hash)` method that queries copy metadata. More complete but adds a command.

Recommend **Option A** for initial implementation. Rename detection is a nice-to-have, not a core requirement for the diff view.

**4d. Write `handleRemoteCommitDetail`**

Mirror the local `GetCommitDetail` logic but use `conn.RunCommand` + CommandBuilder methods. Apply the batch approach from Phase 2 for file content fetching (old content at parent, new content at commit), including the two-step temp file execution pattern.

---

## Information Barrier Checklist

Every change must pass this checklist before merge:

- [ ] No internal hostnames, domains, or IP ranges in code, tests, or docs
- [ ] No organization-specific repo paths or directory structures (e.g., `fbcode/`, `xplat/`)
- [ ] No internal tool names or integration references
- [ ] No Sapling config keys that are organization-specific (vs. upstream Sapling)
- [ ] No test fixtures that reference internal repos or branches
- [ ] Placeholder text uses generic examples (`~/workspace`, `dev.example.com`)
- [ ] Flavor/profile examples use generic names (`dev-server`, `gpu-large`)
- [ ] All Sapling commands are standard `sl` CLI, not internal wrappers
- [ ] The `SaplingCommands` template system remains the extension point for organization-specific clone/workspace commands — never hardcode these

---

## Risks and Open Questions

### capture-pane output limit (BUG-9)

The batch approach in Phase 2 concentrates all output into a single `RunCommand` call. With the per-file caps (500 lines, 1 MB), worst-case output is bounded:

- **Typical case:** 50 files x 50 lines average = 2,500 lines + sentinels. Well within the 50K line capture-pane limit.
- **Worst case:** 50 files x 500 lines (per-file cap) = 25,000 lines + sentinels. Within the 50K limit but using half the budget.
- **Without per-file line cap (v1 design):** 50 files x 5,000 lines = 250,000 lines. Exceeds the 50K limit by 5x — this is why the per-file line cap is necessary, not optional.

The combination of file count cap (50 files) and per-file line cap (500 lines) keeps worst-case output at ~50% of the capture-pane budget, leaving headroom for sentinels and command echo.

### Sparse checkout edge cases

In a sparse monorepo checkout, `sl cat -r . <path>` may fail for files that appear in `sl diff --numstat` but whose blob data is not locally available. The error is swallowed by the `2>/dev/null || true` in the batch script, causing the file to appear as "added" when it is actually "modified."

**Mitigation:** This is an existing Sapling behavior, not something schmux can fix. The frontend should handle missing old content gracefully — show the diff as "old content unavailable" rather than misclassifying the file status.

### `RevListCount` performance on deep history

`sl log -T '.' -r 'only(remote/main, .)' | wc -l` must enumerate all matching commits before counting. On a monorepo with thousands of daily commits, a branch that diverged weeks ago could have tens of thousands of "main ahead" commits.

**Mitigation:** The UI displays this as a count badge. Consider capping the query with `--limit 1000` and displaying "999+" when the limit is hit. The exact count is not actionable information at that scale.

### Shell injection in batch script

The batch script concatenates file paths into shell commands. Paths with special characters (spaces, quotes, semicolons) could cause injection.

**Mitigation:** All paths must be shell-quoted using the existing `shellutil.Quote()` function, which is already used consistently throughout the `CommandBuilder` implementations.

### BUG-10: Shared 30s context (explicitly deferred)

BUG-10 (30s shared context across all sequential `RunCommand` calls with no per-command timeout) is **intentionally deferred** from this spec. Rationale:

- The batch approach in Phase 2 reduces `buildDiffResponse` from O(N) to O(1) `RunCommand` calls, making the shared timeout far less likely to be exhausted by the diff handler.
- The commit graph handler (`handleRemoteCommitGraph`) still makes 6-8 sequential `RunCommand` calls sharing a single 30s context, but each individual call is lightweight (resolve ref, count commits, etc.) and completes in under 1 second in practice.
- Fixing BUG-10 properly requires adding per-command timeout support to `RunCommand` or restructuring the context hierarchy in all remote handlers. This is a cross-cutting infrastructure change that should not be coupled with Sapling VCS support.
- If the 30s budget becomes a practical problem for commit graph operations, it can be addressed independently by either increasing the handler timeout or adding per-command timeouts to `RunCommand`.

### Temp file cleanup on failure

If the second `RunCommand` (executing the batch script) times out or fails, the temp file written by the first `RunCommand` is orphaned on the remote host. Mitigations:

- The temp file is in `/tmp`, which is periodically cleaned by the OS.
- The UUID in the filename prevents collisions between concurrent requests.
- The `rm -f` in the execution command cleans up on success.
- For defense in depth, a periodic cleanup of `/tmp/schmux-batch-*.sh` files older than 1 hour could be added, but this is not required for the initial implementation.
