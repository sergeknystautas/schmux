# Continual Learning Feedback Loop Design

## Problem

Agents working in schmux workspaces constantly discover operational patterns and codebase knowledge that isn't documented in project instruction files (CLAUDE.md, AGENTS.md, etc.). Examples:

- "Must run `go run ./cmd/build-dashboard` instead of `npm run build` directly"
- "Tests need `--race` flag for overlay-related tests"
- "The session manager lives in `internal/session/`, not `internal/daemon/`"

This knowledge is lost when the agent session ends. Other agents working on the same repo — even concurrently — rediscover the same things through trial and error. The instruction files remain static while the agents accumulate experience.

## Solution

A three-stage feedback loop that captures agent learnings, curates them via LLM, and surfaces merge proposals for human approval:

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
                             → routes learnings to correct files
                             → produces merge proposal
                                                        User reviews in dashboard
                                                        → diff view per file
                                                        → accepts/edits/rejects
                                                        → instruction files committed
                                                        → all workspaces get changes via git
```

**Key properties:**

- Zero-cost capture — agents append raw text, no evaluation overhead
- Immediate sharing — overlay compounding syncs raw learnings to sibling agents in real-time
- Multi-file aware — curator routes learnings to the correct instruction file(s)
- Human control — nothing touches git without explicit approval

## Stage 1: Scratchpad Capture

### File Format

Each workspace gets `.claude/learnings.jsonl` — a gitignored, append-only JSONL file. Each line is one learning:

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
| `text`  | string | The raw learning, free-form text                                     |

### How Agents Know to Capture

The instruction to capture learnings is self-bootstrapping — it lives in the instruction files themselves. Each file gets an equivalent section adapted to its conventions:

**CLAUDE.md:**

```markdown
## Learning Capture

As you work, append discoveries to `.claude/learnings.jsonl` — things you learned
that aren't already documented in this file. One JSON line per learning:
{"ts":"<ISO8601>","ws":"<workspace-id>","agent":"claude-code","type":"operational|codebase","text":"<what you learned>"}

Don't evaluate importance. Don't read the file first. Just append.
```

**AGENTS.md:**

```markdown
## Learning Capture

Append discoveries to `.claude/learnings.jsonl` as you work. One JSON line per entry:
{"ts":"<ISO8601>","ws":"<workspace-id>","agent":"<your-agent-type>","type":"operational|codebase","text":"<what you learned>"}

Append only. Do not read or parse the file.
```

### Capture Cost

The agent performs one file append per learning. No reading the file, no diffing against existing instructions, no formatting, no evaluation of importance. The separation of recording from evaluating is deliberate — it avoids context-switching during focused work and avoids information loss from context compression in long sessions.

### Overlay Integration

`.claude/learnings.jsonl` is added to the default overlay paths:

```go
var DefaultOverlayPaths = []string{
    ".claude/settings.json",
    ".claude/settings.local.json",
    ".claude/learnings.jsonl",     // new
}
```

This means:

- The file is copied to new workspaces from the overlay
- Changes are synced to sibling workspaces via the existing compounding loop
- All agents across all workspaces see each other's raw learnings immediately

### Merge Strategy for Learnings

The learnings file is append-only JSONL, so the existing LLM merge in the compounding loop handles it naturally — the merge instruction to "union arrays / keep all entries / never remove" produces the correct result (concatenation of unique entries). However, a simpler optimization: since entries have unique `ts`+`ws` keys, the compounder can use a line-level union (deduplicate by full line content) without needing LLM involvement. This is a fast-path optimization.

## Stage 2: Curator Agent

### Trigger Events

The curator runs as a headless LLM call (no tmux session, no workspace needed). It is triggered by:

1. **Session dispose** — after an agent session ends, schmux triggers curation. A debounce of 30 seconds prevents rapid-fire curation when multiple sessions end close together.
2. **Manual trigger** — user clicks "Curate Learnings" in the dashboard.

### Inputs

The curator receives:

1. **Raw scratchpad entries** — from `~/.schmux/overlays/<repo>/.claude/learnings.jsonl`, filtered to entries in `raw` state only
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
  "learning": {
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
1. A list of raw learnings discovered by AI agents working on this project
2. The current content of all instruction files

Your job is to produce a merge proposal — changes to the instruction files that
incorporate the new learnings.

Rules:
- DEDUPLICATE: Collapse similar learnings from different agents into one
- FILTER: Discard learnings already covered by existing content
- ROUTE: Decide which file(s) each learning belongs in:
  - Universal learnings (apply to any agent) → add to ALL instruction files,
    adapted to each file's style
  - Agent-specific learnings → add to that agent's file only
- CATEGORIZE: Place each learning under the appropriate existing section,
  or propose a new section if none fits
- PRESERVE VOICE: Match the tone, formatting, and style of each file
- NEVER REMOVE existing content — only add or refine
- Output the full proposed content for each modified file

INSTRUCTION FILES:
<for each file>
=== <filename> ===
<content>
</for each>

RAW LEARNINGS:
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
  "learning": {
    "llm_target": "claude-sonnet"
  }
}
```

A more capable model than the compounding merge target is appropriate here, since the curator is doing creative work (adapting learnings to fit file structure and style) rather than mechanical merging.

## Stage 3: Proposal Storage and Review

### Proposal Format

Proposals are stored at `~/.schmux/learning-proposals/<repo>/<id>.json`:

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

### Learnings Page

**Route:** `/learnings/:repoName`

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
│ Proposal: prop-20260213-143200                    [Dismiss]  [Apply]    │
│ 3 learnings from 2 workspaces • Feb 13, 2026                           │
│                                                                         │
│ ┌─────────────┬──────────────┐                                          │
│ │  CLAUDE.md  │  AGENTS.md   │                                          │
│ └─────────────┴──────────────┘                                          │
│                                                                         │
│ ## Build Commands                                                       │
│                                                                         │
│   ```bash                                                               │
│   go build ./cmd/schmux                                                 │
│   ```                                                                   │
│                                                                         │
│ + > **Important**: Never run `npm install` or `npm run build`           │
│ + > directly in the dashboard directory. Always use                     │
│ + > `go run ./cmd/build-dashboard` which handles dependencies           │
│ + > and output paths correctly.                                         │
│                                                                         │
│                                          [Edit & Apply]  [Apply as-is]  │
└──────────────────────────────────────────────────────────────────────────┘
````

The diff view shows tabs per affected file. Each tab shows a unified diff with additions highlighted. The user can:

- **Apply as-is** — schmux writes the proposed content to each instruction file in the repo directory, stages them, and commits with message: `chore: update instruction files with agent learnings (<N> additions)`
- **Edit & Apply** — opens proposed content in an editable text area for tweaking before applying
- **Dismiss** — discards the proposal, marks source entries as `dismissed`
- **Re-curate** (shown when stale) — re-runs the curator against current file state

#### Raw Learnings Section

A scrollable log of all scratchpad entries across workspaces, with filters:

- By workspace
- By agent type
- By learning type (operational / codebase)
- By state (raw / proposed / applied / dismissed)
- By time range

This lets the user see what agents are discovering before the curator runs, and manually trigger curation when enough raw learnings accumulate.

### Sidebar Integration

The learnings nav item shows a badge with the count of pending proposals.

### API Endpoints

#### `GET /api/learnings/:repoName/proposals`

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

#### `GET /api/learnings/:repoName/proposals/:id`

Returns a single proposal with full content and diffs.

#### `POST /api/learnings/:repoName/proposals/:id/apply`

Applies the proposal:

1. Validates that `file_hashes` still match current files (rejects if stale)
2. Writes proposed content to each instruction file in the repo directory
3. Runs `git add <files>` and `git commit -m "chore: update instruction files with agent learnings"`
4. Marks the proposal as `applied`
5. Marks source scratchpad entries as `applied`

Request body (optional, for edited content):

```json
{
  "overrides": {
    "CLAUDE.md": "<user-edited content>"
  }
}
```

#### `POST /api/learnings/:repoName/proposals/:id/dismiss`

Marks the proposal as `dismissed` and source entries as `dismissed`.

#### `POST /api/learnings/:repoName/curate`

Manually triggers the curator. Returns the new proposal ID.

#### `GET /api/learnings/:repoName/entries`

Returns raw scratchpad entries with optional filters:

```
GET /api/learnings/schmux/entries?state=raw&agent=claude-code&limit=50
```

## Git Commit Strategy

When a proposal is applied, schmux commits directly in the **repo directory** from `~/.schmux/config.json`. This is the original checkout that worktrees are created from — it always exists and has a full git state.

The commit is surgical:

1. Only stage the modified instruction files (`git add CLAUDE.md AGENTS.md`)
2. Commit with a descriptive message
3. Do not push — the user decides when to push

If the repo directory has uncommitted changes to instruction files, the apply endpoint rejects the request and surfaces the conflict in the dashboard.

## Configuration

New config fields in `~/.schmux/config.json`:

```json
{
  "learning": {
    "enabled": true,
    "llm_target": "claude-sonnet",
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
| `enabled`            | `true`          | Enable/disable the learning system               |
| `llm_target`         | compound target | LLM for curator calls                            |
| `curate_on_dispose`  | `true`          | Auto-trigger curator on session dispose          |
| `curate_debounce_ms` | `30000`         | Debounce window for auto-curation                |
| `prune_after_days`   | `30`            | Days before applied/dismissed entries are pruned |
| `instruction_files`  | see above       | Instruction file patterns to manage              |

## Architecture

### New Package: `internal/learning/`

| File            | Responsibility                                    |
| --------------- | ------------------------------------------------- |
| `scratchpad.go` | Parse, append, query, prune scratchpad entries    |
| `curator.go`    | Headless LLM call that produces proposals         |
| `proposals.go`  | Read, write, list, update proposals on disk       |
| `apply.go`      | Git commit of instruction files in repo directory |

### Integration Points

| Component                                      | Change                                                 |
| ---------------------------------------------- | ------------------------------------------------------ |
| `internal/config/`                             | Add `LearningConfig` struct, `GetInstructionFiles()`   |
| `internal/workspace/overlay.go`                | Add `.claude/learnings.jsonl` to default overlay paths |
| `internal/compound/merge.go`                   | Add JSONL line-union fast path for `.jsonl` files      |
| `internal/daemon/daemon.go`                    | Trigger curator on session dispose, wire learning API  |
| `internal/dashboard/handlers_learning.go`      | REST endpoints for proposals and entries               |
| `assets/dashboard/src/pages/LearningsPage.tsx` | Review UI with diff view and tabs                      |
| `assets/dashboard/src/components/Sidebar.tsx`  | Badge count for pending proposals                      |

### Data Flow

```
 Agent in ws-abc (claude-code)         Agent in ws-def (codex)
 ─────────────────────────            ────────────────────────
 discovers: "tests need               discovers: "tests need
 --race flag for overlay tests"       --race for compound tests"
      │                                    │
      ▼                                    ▼
 appends to                           appends to
 ws-abc/.claude/learnings.jsonl       ws-def/.claude/learnings.jsonl
      │                                    │
      ▼                                    ▼
 ┌──────────────────────────────────────────────┐
 │  Overlay Compounder (existing system)        │
 │  merges both into:                           │
 │  ~/.schmux/overlays/<repo>/                  │
 │      .claude/learnings.jsonl                 │
 │  and propagates to sibling workspaces        │
 └──────────────────────────────────────────────┘
      │
      ▼  (session dispose or manual trigger)
 ┌──────────────────────────────────────────────┐
 │  Curator (headless LLM call)                 │
 │  reads: overlay learnings.jsonl (raw only)   │
 │  reads: CLAUDE.md from repo dir              │
 │  reads: AGENTS.md from repo dir              │
 │  deduplicates: both --race entries → one     │
 │  routes: universal → both files              │
 │  produces: multi-file merge proposal         │
 └──────────────────────────────────────────────┘
      │
      ▼
 ~/.schmux/learning-proposals/<repo>/
     prop-20260213-143200.json
      │
      ▼  (dashboard badge appears)
 ┌──────────────────────────────────────────────┐
 │  Dashboard: user reviews diff per file       │
 │  clicks "Apply"                              │
 └──────────────────────────────────────────────┘
      │
      ▼
 git commit in repo directory
 (only instruction files staged and committed)
      │
      ▼
 Scratchpad entries marked "applied"
 (kept for audit trail, pruned after 30 days)
```

## Implementation Steps

1. **Scratchpad package** — `internal/learning/scratchpad.go`: JSONL parser, append function, state-change tracking, entry queries with filters, pruning logic. Unit tests for all operations.

2. **Overlay integration** — Add `.claude/learnings.jsonl` to `DefaultOverlayPaths`. Add JSONL line-union fast path to `internal/compound/merge.go` (deduplicate by full line content, no LLM needed for append-only JSONL).

3. **Curator** — `internal/learning/curator.go`: instruction file discovery, LLM prompt construction, response parsing. Unit tests with mocked LLM.

4. **Proposal store** — `internal/learning/proposals.go`: disk-based proposal storage, staleness detection via file hashes, state transitions. Unit tests.

5. **Apply logic** — `internal/learning/apply.go`: write files to repo dir, git add + commit, conflict detection. Unit tests with temp git repos.

6. **Config** — Add `LearningConfig` to config schema. Wire defaults.

7. **Daemon integration** — Trigger curator on session dispose with debounce. Wire learning package into daemon lifecycle.

8. **API endpoints** — `internal/dashboard/handlers_learning.go`: proposals CRUD, entries list, curate trigger, apply/dismiss actions.

9. **Dashboard frontend** — `LearningsPage.tsx` with proposals list, diff view with file tabs, raw entries log. Sidebar badge.

10. **Self-bootstrap** — Add learning capture instructions to CLAUDE.md and AGENTS.md in the schmux repo itself.

11. **Integration test** — End-to-end: agent appends learning → overlay syncs → curator produces proposal → apply commits to repo. Verify multi-file routing and deduplication.
