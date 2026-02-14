# Lore: Continual Knowledge Feedback Loop Design

## Problem

Agents working in schmux workspaces constantly discover operational patterns and codebase knowledge that isn't documented in project instruction files (CLAUDE.md, AGENTS.md, etc.). Examples:

- "Must run `go run ./cmd/build-dashboard` instead of `npm run build` directly"
- "Tests need `--race` flag for overlay-related tests"
- "The session manager lives in `internal/session/`, not `internal/daemon/`"

This knowledge is lost when the agent session ends. Other agents working on the same repo — even concurrently — rediscover the same things through trial and error. The instruction files remain static while the agents accumulate experience.

## Solution

A three-stage feedback loop that captures project lore from agents, curates it via LLM, and surfaces merge proposals for human approval:

```
Agent Work Session           Curator Process            Human Review
──────────────────          ────────────────           ──────────────

 Agent discovers fact
 → appends to scratchpad
 (zero-cost, no eval)

 Session ends / checkpoint
 → scratchpad persisted
                             Curator agent wakes up
                             → reads all scratchpads
                             → reads all instruction files
                             → deduplicates
                             → routes lore to correct files
                             → produces merge proposal
                                                        User reviews in dashboard
                                                        → diff view per file
                                                        → accepts/edits/rejects
                                                        → instruction files committed
                                                        → all workspaces get changes via git
```

**Key properties:**

- Zero-cost capture — agents append raw text, no evaluation overhead
- Immediate sharing — overlay compounding syncs raw lore to sibling agents in real-time
- Multi-file aware — curator routes lore to the correct instruction file(s)
- Human control — nothing touches git without explicit approval

## Stage 1: Scratchpad Capture

### File Format

Each workspace gets `.claude/lore.jsonl` — a gitignored, append-only JSONL file. Each line is one lore entry:

```jsonl
{"ts":"2026-02-13T14:32:00Z","ws":"ws-abc123","agent":"claude-code","type":"operational","text":"Must run go run ./cmd/build-dashboard instead of npm directly — the Go wrapper handles deps and output paths"}
{"ts":"2026-02-13T14:45:00Z","ws":"ws-abc123","agent":"claude-code","type":"codebase","text":"Overlay files must be gitignored — CopyOverlay skips tracked files silently (internal/workspace/overlay.go:91)"}
{"ts":"2026-02-13T15:01:00Z","ws":"ws-def456","agent":"codex","type":"operational","text":"Tests need --race flag for compound package tests to catch race conditions"}
```

### Schema

| Field   | Type   | Description                                                          |
| ------- | ------ | -------------------------------------------------------------------- |
| `ts`    | string | ISO 8601 timestamp                                                   |
| `ws`    | string | Workspace ID                                                         |
| `agent` | string | Agent type: `claude-code`, `codex`, `cursor`, `copilot`, etc.        |
| `type`  | string | `operational` (how-to patterns) or `codebase` (structural knowledge) |
| `text`  | string | The raw lore entry, free-form text                                   |

### How Agents Know to Capture

The instruction to capture lore is self-bootstrapping — it lives in the instruction files themselves. Each file gets an equivalent section adapted to its conventions:

**CLAUDE.md:**

```markdown
## Lore Capture

As you work, append discoveries to `.claude/lore.jsonl` — things you learned
that aren't already documented in this file. One JSON line per entry:
{"ts":"<ISO8601>","ws":"<workspace-id>","agent":"claude-code","type":"operational|codebase","text":"<what you learned>"}

Don't evaluate importance. Don't read the file first. Just append.
```

**AGENTS.md:**

```markdown
## Lore Capture

Append discoveries to `.claude/lore.jsonl` as you work. One JSON line per entry:
{"ts":"<ISO8601>","ws":"<workspace-id>","agent":"<your-agent-type>","type":"operational|codebase","text":"<what you learned>"}

Append only. Do not read or parse the file.
```

### Capture Cost

The agent performs one file append per lore entry. No reading the file, no diffing against existing instructions, no formatting, no evaluation of importance. The separation of recording from evaluating is deliberate — it avoids context-switching during focused work and avoids information loss from context compression in long sessions.

### Overlay Integration

`.claude/lore.jsonl` is added to the default overlay paths:

```go
var DefaultOverlayPaths = []string{
    ".claude/settings.json",
    ".claude/settings.local.json",
    ".claude/lore.jsonl",     // new
}
```

This means:

- The file is copied to new workspaces from the overlay
- Changes are synced to sibling workspaces via the existing compounding loop
- All agents across all workspaces see each other's raw lore immediately

### Merge Strategy for Lore

The lore file is append-only JSONL, so the existing LLM merge in the compounding loop handles it naturally — the merge instruction to "union arrays / keep all entries / never remove" produces the correct result (concatenation of unique entries). However, a simpler optimization: since entries have unique `ts`+`ws` keys, the compounder can use a line-level union (deduplicate by full line content) without needing LLM involvement. This is a fast-path optimization.

## Stage 2: Curator Agent

### Trigger Events

The curator runs as a headless LLM call (no tmux session, no workspace needed). It is triggered by:

1. **Session dispose** — after an agent session ends, schmux triggers curation. A debounce of 30 seconds prevents rapid-fire curation when multiple sessions end close together.
2. **Manual trigger** — user clicks "Curate Lore" in the dashboard.

### Inputs

The curator receives:

1. **Raw scratchpad entries** — from `~/.schmux/overlays/<repo>/.claude/lore.jsonl`, filtered to entries in `raw` state only
2. **All instruction files** — read from the repo directory configured in `~/.schmux/config.json`

### Instruction File Discovery

The curator scans for known instruction file patterns:

| File                              | Agent                 |
| --------------------------------- | --------------------- |
| `CLAUDE.md`                       | Claude Code           |
| `AGENTS.md`                       | Codex, generic agents |
| `.cursorrules`                    | Cursor                |
| `.github/copilot-instructions.md` | GitHub Copilot        |
| `CONVENTIONS.md`                  | Shared/generic        |

The set of known patterns is configurable in `~/.schmux/config.json`:

```json
{
  "lore": {
    "instruction_files": [
      "CLAUDE.md",
      "AGENTS.md",
      ".cursorrules",
      ".github/copilot-instructions.md"
    ]
  }
}
```

Only files that actually exist in the repo are included. The curator adapts to whatever instruction files the project uses.

### Curator Prompt

```
You are a curator for a software project's agent instruction files.

You will receive:
1. A list of raw lore entries discovered by AI agents working on this project
2. The current content of all instruction files

Your job is to produce a merge proposal — changes to the instruction files that
incorporate the new lore.

Rules:
- DEDUPLICATE: Collapse similar entries from different agents into one
- FILTER: Discard entries already covered by existing content
- ROUTE: Decide which file(s) each entry belongs in:
  - Universal lore (applies to any agent) → add to ALL instruction files,
    adapted to each file's style
  - Agent-specific lore → add to that agent's file only
- CATEGORIZE: Place each entry under the appropriate existing section,
  or propose a new section if none fits
- PRESERVE VOICE: Match the tone, formatting, and style of each file
- NEVER REMOVE existing content — only add or refine
- Output the full proposed content for each modified file

INSTRUCTION FILES:
<for each file>
=== <filename> ===
<content>
</for each>

RAW LORE:
<entries>
```

### Curator Output

The curator produces a JSON response:

```json
{
  "proposed_files": {
    "CLAUDE.md": "<full proposed content>",
    "AGENTS.md": "<full proposed content>"
  },
  "diff_summary": "Added 2 universal items to CLAUDE.md and AGENTS.md, 1 claude-specific item to CLAUDE.md only",
  "entries_used": ["<entry identifiers incorporated>"],
  "entries_discarded": {
    "<entry identifier>": "Already documented in Build Commands section"
  }
}
```

### LLM Target

The curator uses a configurable LLM target, defaulting to the compound LLM target:

```json
{
  "lore": {
    "llm_target": "claude-sonnet"
  }
}
```

A more capable model than the compounding merge target is appropriate here, since the curator is doing creative work (adapting lore to fit file structure and style) rather than mechanical merging.

## Stage 3: Proposal Storage and Review

### Proposal Format

Proposals are stored at `~/.schmux/lore-proposals/<repo>/<id>.json`:

```json
{
  "id": "prop-20260213-143200",
  "repo": "schmux",
  "created_at": "2026-02-13T14:32:00Z",
  "status": "pending",
  "source_count": 12,
  "sources": ["ws-abc123", "ws-def456"],
  "file_hashes": {
    "CLAUDE.md": "sha256:abc...",
    "AGENTS.md": "sha256:def..."
  },
  "proposed_files": {
    "CLAUDE.md": "<full proposed content>",
    "AGENTS.md": "<full proposed content>"
  },
  "diff_summary": "Added 3 items: build command clarification, test ordering note, overlay gitignore requirement",
  "entries_used": ["..."],
  "entries_discarded": { "...": "..." }
}
```

The `file_hashes` field records the SHA-256 of each instruction file at curation time. If a file changes before the proposal is applied (e.g., someone manually edits CLAUDE.md), the proposal is marked stale.

### Proposal States

| State       | Meaning                                                              |
| ----------- | -------------------------------------------------------------------- |
| `pending`   | Ready for review                                                     |
| `stale`     | An instruction file changed since curation — re-curation recommended |
| `applied`   | User applied the proposal                                            |
| `dismissed` | User rejected the proposal                                           |

### Entry States

Scratchpad entries track their lifecycle:

| State       | Meaning                                          |
| ----------- | ------------------------------------------------ |
| `raw`       | Captured, not yet curated                        |
| `proposed`  | Included in a pending proposal                   |
| `applied`   | Incorporated into an instruction file            |
| `dismissed` | User rejected the proposal containing this entry |

Only `raw` entries are fed to the curator. The state is tracked by appending state-change records to the scratchpad (preserving append-only semantics):

```jsonl
{"ts":"...","ws":"ws-abc","agent":"claude-code","type":"operational","text":"Must use go run ./cmd/build-dashboard"}
{"ts":"...","state_change":"proposed","entry_ts":"...","proposal_id":"prop-20260213-143200"}
```

### Scratchpad Pruning

Entries in `applied` or `dismissed` state older than 30 days are pruned. `raw` and `proposed` entries are never auto-pruned.

## Dashboard Integration

### Lore Page

**Route:** `/lore/:repoName`

Accessible from the sidebar. Shows a badge count of pending proposals.

#### Pending Proposals Section

A list of curator-generated proposals, newest first. Each shows:

- Timestamp and source workspaces
- One-line diff summary
- Number of entries incorporated vs discarded
- Staleness warning if applicable

Clicking a proposal opens a diff view:

````
┌──────────────────────────────────────────────────────────────────────────┐
│ Proposal: prop-20260213-143200                    [Dismiss]  [Apply]     │
│ 3 lore entries from 2 workspaces • Feb 13, 2026                         │
│                                                                          │
│ ┌─────────────┬──────────────┐                                           │
│ │  CLAUDE.md  │  AGENTS.md   │                                           │
│ └─────────────┴──────────────┘                                           │
│                                                                          │
│ ## Build Commands                                                        │
│                                                                          │
│   ```bash                                                                │
│   go build ./cmd/schmux                                                  │
│   ```                                                                    │
│                                                                          │
│ + > **Important**: Never run `npm install` or `npm run build`            │
│ + > directly in the dashboard directory. Always use                      │
│ + > `go run ./cmd/build-dashboard` which handles dependencies            │
│ + > and output paths correctly.                                          │
│                                                                          │
│                                          [Edit & Apply]  [Apply as-is]   │
└──────────────────────────────────────────────────────────────────────────┘
````

The diff view shows tabs per affected file. Each tab shows a unified diff with additions highlighted. The user can:

- **Apply as-is** — schmux spawns a temporary worktree, commits the proposed content, and pushes the branch (see Git Commit Strategy)
- **Edit & Apply** — opens proposed content in an editable text area for tweaking before applying
- **Dismiss** — discards the proposal, marks source entries as `dismissed`
- **Re-curate** (shown when stale) — re-runs the curator against current file state

#### Raw Lore Section

A scrollable log of all scratchpad entries across workspaces, with filters:

- By workspace
- By agent type
- By lore type (operational / codebase)
- By state (raw / proposed / applied / dismissed)
- By time range

This lets the user see what agents are discovering before the curator runs, and manually trigger curation when enough raw lore accumulates.

### Sidebar Integration

The lore nav item shows a badge with the count of pending proposals.

### API Endpoints

#### `GET /api/lore/:repoName/proposals`

Returns all proposals for a repo.

```json
{
  "proposals": [
    {
      "id": "prop-20260213-143200",
      "created_at": "2026-02-13T14:32:00Z",
      "status": "pending",
      "source_count": 12,
      "diff_summary": "Added 3 items...",
      "files_affected": ["CLAUDE.md", "AGENTS.md"]
    }
  ]
}
```

#### `GET /api/lore/:repoName/proposals/:id`

Returns a single proposal with full content and diffs.

#### `POST /api/lore/:repoName/proposals/:id/apply`

Applies the proposal:

1. Spawns a temporary worktree on a new branch (e.g., `schmux/lore-20260213-143200`)
2. Writes proposed instruction file content into the worktree
3. Commits with message: `chore: update instruction files with agent lore (<N> additions)`
4. Pushes the branch to remote
5. Disposes the temporary worktree
6. Marks the proposal as `applied`
7. Marks source scratchpad entries as `applied`

The lore branch can then be merged through the team's normal workflow (PR, merge, etc.).

Request body (optional, for edited content):

```json
{
  "overrides": {
    "CLAUDE.md": "<user-edited content>"
  }
}
```

#### `POST /api/lore/:repoName/proposals/:id/dismiss`

Marks the proposal as `dismissed` and source entries as `dismissed`.

#### `POST /api/lore/:repoName/curate`

Manually triggers the curator. Returns the new proposal ID.

#### `GET /api/lore/:repoName/entries`

Returns raw scratchpad entries with optional filters:

```
GET /api/lore/schmux/entries?state=raw&agent=claude-code&limit=50
```

## Git Commit Strategy

The base repo in schmux is a **bare clone** — it has no working directory. Each workspace is a worktree on its own branch, and workspaces may be disposed before lore is curated. To handle this, the curator creates its own temporary worktree.

### Workflow

When a proposal is applied:

```
1. Create branch                → schmux/lore-<timestamp>
                                  branched from the default branch (main)
2. Spawn temporary worktree     → git worktree add <path> schmux/lore-<timestamp>
3. Read current instruction     → read CLAUDE.md, AGENTS.md, etc. from worktree
   files and validate hashes      (reject if stale — files changed since curation)
4. Write proposed content       → overwrite instruction files with curated content
5. Commit                       → git add <files> && git commit
6. Push                         → git push origin schmux/lore-<timestamp>
7. Dispose worktree             → git worktree remove <path>
```

The temporary worktree is short-lived — it exists only for the duration of the commit and push. Schmux manages it through the existing workspace machinery but marks it as a system workspace (not user-visible in the dashboard session list).

### Merge Path

The pushed branch is available for the team to merge through their normal workflow:

- GitHub/GitLab: create a PR via `gh pr create` or equivalent
- Direct merge: `git merge schmux/lore-<timestamp>` in any worktree
- Schmux could optionally auto-create a PR (configurable, see below)

Once merged, all worktrees see the updated instruction files on their next rebase/pull from the default branch.

### Auto-PR (Optional)

If the repo is hosted on GitHub and `gh` is available, schmux can auto-create a PR:

```json
{
  "lore": {
    "auto_pr": true
  }
}
```

When enabled, after pushing the branch, schmux runs:

```
gh pr create --title "chore: agent lore (<N> additions)"
             --body "<diff summary from curator>"
             --base main
             --head schmux/lore-<timestamp>
```

When `auto_pr` is `false` (the default), the branch is pushed but no PR is created. The dashboard shows a link to the remote branch.

## Configuration

New config fields in `~/.schmux/config.json`:

```json
{
  "lore": {
    "enabled": true,
    "llm_target": "claude-sonnet",
    "auto_pr": false,
    "curate_on_dispose": true,
    "curate_debounce_ms": 30000,
    "prune_after_days": 30,
    "instruction_files": [
      "CLAUDE.md",
      "AGENTS.md",
      ".cursorrules",
      ".github/copilot-instructions.md"
    ]
  }
}
```

| Field                | Default         | Description                                      |
| -------------------- | --------------- | ------------------------------------------------ |
| `enabled`            | `true`          | Enable/disable the lore system                   |
| `llm_target`         | compound target | LLM for curator calls                            |
| `auto_pr`            | `false`         | Auto-create a PR after pushing the lore branch   |
| `curate_on_dispose`  | `true`          | Auto-trigger curator on session dispose          |
| `curate_debounce_ms` | `30000`         | Debounce window for auto-curation                |
| `prune_after_days`   | `30`            | Days before applied/dismissed entries are pruned |
| `instruction_files`  | see above       | Instruction file patterns to manage              |

## Architecture

### New Package: `internal/lore/`

| File            | Responsibility                                 |
| --------------- | ---------------------------------------------- |
| `scratchpad.go` | Parse, append, query, prune scratchpad entries |
| `curator.go`    | Headless LLM call that produces proposals      |
| `proposals.go`  | Read, write, list, update proposals on disk    |
| `apply.go`      | Spawn temp worktree, commit, push, dispose     |

### Integration Points

| Component                                     | Change                                            |
| --------------------------------------------- | ------------------------------------------------- |
| `internal/config/`                            | Add `LoreConfig` struct, `GetInstructionFiles()`  |
| `internal/workspace/overlay.go`               | Add `.claude/lore.jsonl` to default overlay paths |
| `internal/compound/merge.go`                  | Add JSONL line-union fast path for `.jsonl` files |
| `internal/daemon/daemon.go`                   | Trigger curator on session dispose, wire lore API |
| `internal/dashboard/handlers_lore.go`         | REST endpoints for proposals and entries          |
| `assets/dashboard/src/pages/LorePage.tsx`     | Review UI with diff view and tabs                 |
| `assets/dashboard/src/components/Sidebar.tsx` | Badge count for pending proposals                 |

### Data Flow

```
 Agent in ws-abc (claude-code)         Agent in ws-def (codex)
 ─────────────────────────            ────────────────────────
 discovers: "tests need               discovers: "tests need
 --race flag for overlay tests"       --race for compound tests"
      │                                    │
      ▼                                    ▼
 appends to                           appends to
 ws-abc/.claude/lore.jsonl            ws-def/.claude/lore.jsonl
      │                                    │
      ▼                                    ▼
 ┌──────────────────────────────────────────────┐
 │  Overlay Compounder (existing system)        │
 │  merges both into:                           │
 │  ~/.schmux/overlays/<repo>/                  │
 │      .claude/lore.jsonl                      │
 │  and propagates to sibling workspaces        │
 └──────────────────────────────────────────────┘
      │
      ▼  (session dispose or manual trigger)
 ┌──────────────────────────────────────────────┐
 │  Curator (headless LLM call)                 │
 │  reads: overlay lore.jsonl (raw only)        │
 │  reads: CLAUDE.md from bare repo             │
 │  reads: AGENTS.md from bare repo             │
 │  deduplicates: both --race entries → one     │
 │  routes: universal → both files              │
 │  produces: multi-file merge proposal         │
 └──────────────────────────────────────────────┘
      │
      ▼
 ~/.schmux/lore-proposals/<repo>/
     prop-20260213-143200.json
      │
      ▼  (dashboard badge appears)
 ┌──────────────────────────────────────────────┐
 │  Dashboard: user reviews diff per file       │
 │  clicks "Apply"                              │
 └──────────────────────────────────────────────┘
      │
      ▼
 Temp worktree spawned on schmux/lore-<timestamp>
 Instruction files committed and pushed
 Worktree disposed
      │
      ▼
 Scratchpad entries marked "applied"
 (kept for audit trail, pruned after 30 days)
```

## Implementation Steps

1. **Scratchpad package** — `internal/lore/scratchpad.go`: JSONL parser, append function, state-change tracking, entry queries with filters, pruning logic. Unit tests for all operations.

2. **Overlay integration** — Add `.claude/lore.jsonl` to `DefaultOverlayPaths`. Add JSONL line-union fast path to `internal/compound/merge.go` (deduplicate by full line content, no LLM needed for append-only JSONL).

3. **Curator** — `internal/lore/curator.go`: instruction file discovery, LLM prompt construction, response parsing. Unit tests with mocked LLM.

4. **Proposal store** — `internal/lore/proposals.go`: disk-based proposal storage, staleness detection via file hashes, state transitions. Unit tests.

5. **Apply logic** — `internal/lore/apply.go`: spawn temp worktree, write files, commit, push, dispose worktree. Unit tests with temp git repos.

6. **Config** — Add `LoreConfig` to config schema. Wire defaults.

7. **Daemon integration** — Trigger curator on session dispose with debounce. Wire lore package into daemon lifecycle.

8. **API endpoints** — `internal/dashboard/handlers_lore.go`: proposals CRUD, entries list, curate trigger, apply/dismiss actions.

9. **Dashboard frontend** — `LorePage.tsx` with proposals list, diff view with file tabs, raw entries log. Sidebar badge.

10. **Self-bootstrap** — Add lore capture instructions to CLAUDE.md and AGENTS.md in the schmux repo itself.

11. **Integration test** — End-to-end: agent appends lore → overlay syncs → curator produces proposal → apply commits and pushes. Verify multi-file routing and deduplication.
