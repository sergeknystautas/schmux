# Lore: Continual Knowledge Feedback Loop Design

## Problem

Agents working in schmux workspaces constantly discover operational patterns and codebase knowledge that isn't documented in project instruction files (CLAUDE.md, AGENTS.md, etc.). Examples:

- "Must run `go run ./cmd/build-dashboard` instead of `npm run build` directly"
- "Tests need `--race` flag for overlay-related tests"
- "The session manager lives in `internal/session/`, not `internal/daemon/`"

This knowledge is lost when the agent session ends. Other agents working on the same repo — even concurrently — rediscover the same things through trial and error. The instruction files remain static while the agents accumulate experience.

## Solution

A three-stage feedback loop that captures friction from agents via hooks and self-capture, curates it via LLM, and surfaces merge proposals for human approval:

```
Agent Work Session           Curator Process            Human Review
──────────────────          ────────────────           ──────────────

 Claude Code: hooks fire
 on tool failures (auto)
 and session stop (reflection)

 Other agents: self-capture
 friction entries per
 instruction file template
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

- Zero-cost capture for Claude Code — hooks run outside the agent's context window
- Friction-focused — captures what went wrong, not general knowledge
- One-directional flow — agents write to workspace scratchpads, backend reads and aggregates
- Multi-file aware — curator routes lore to the correct instruction file(s)
- Human control — nothing touches git without explicit approval

## Stage 1: Friction Capture

### Capture Mechanism

Lore capture is split by agent type:

**Claude Code (hook-based, automatic):**
Two Claude Code hooks capture friction automatically — no agent instruction needed:

1. **`PostToolUseFailure` hook** (`capture-failure.sh`) — fires on every tool failure. Reads JSON from stdin (`tool_name`, `tool_input`, `error`, `is_interrupt`). Skips user interrupts. Extracts input summaries based on tool type (command for Bash, file_path for Read/Edit/Write/Glob, pattern for Grep, raw input for others). Classifies the error into a category and appends a structured `failure` entry to `.schmux/lore.jsonl`.
2. **`Stop` hook** (`stop-gate.sh`) — gates agent completion on two requirements: (a) the schmux status file must be updated beyond the default "working" value, and (b) a `reflection` entry for the current session must exist in `.schmux/lore.jsonl`. If either is missing, exit code 2 halts the agent with instructions. Includes infinite loop prevention: if `stop_hook_active` is true, just signals completed and exits.

Hook scripts are embedded in the Go binary and installed to `<workspace>/.schmux/hooks/` at session spawn via `provision.EnsureLoreHookScripts`.

**Other agents (self-capture via instruction files):**
Non-Claude agents (Codex, Gemini, Cursor) receive a "Friction Capture" section in their instruction files (AGENTS.md, .cursorrules, etc.) that instructs them to append `friction` entries when they hit walls.

### File Format

Each workspace gets `.schmux/lore.jsonl` — a gitignored, append-only JSONL file. Each line is one lore entry:

```jsonl
{"ts":"2026-02-18T10:30:00Z","ws":"ws-abc123","session":"sess-001","agent":"claude-code","type":"failure","tool":"Bash","input_summary":"npm run build","error_summary":"Missing script: build","category":"wrong_command"}
{"ts":"2026-02-18T10:31:00Z","ws":"ws-abc123","session":"sess-001","agent":"claude-code","type":"reflection","text":"When building dashboard, use go run ./cmd/build-dashboard not npm directly"}
{"ts":"2026-02-18T11:00:00Z","ws":"ws-def456","agent":"codex","type":"friction","text":"When looking for session logic, check internal/session/ not internal/daemon/"}
```

### Schema

| Field           | Type   | Description                                                                                                                                      |
| --------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `ts`            | string | ISO 8601 timestamp                                                                                                                               |
| `ws`            | string | Workspace ID                                                                                                                                     |
| `session`       | string | Session ID — used by the stop-gate hook to verify reflection is from the current session                                                         |
| `agent`         | string | Agent type: `claude-code`, `codex`, `cursor`, `copilot`, etc.                                                                                    |
| `type`          | string | `failure` (auto-captured), `reflection` (stop-gate), `friction` (self-capture)                                                                   |
| `text`          | string | Free-form description (used by `reflection` and `friction` types)                                                                                |
| `tool`          | string | Tool name, e.g. `Bash`, `Read` (failure entries only)                                                                                            |
| `input_summary` | string | Summarized tool input (failure entries only)                                                                                                     |
| `error_summary` | string | Summarized error message (failure entries only)                                                                                                  |
| `category`      | string | Error category (failure entries only): `not_found`, `permission`, `syntax`, `wrong_command`, `build_failure`, `test_failure`, `timeout`, `other` |
| `state_change`  | string | State-change records only: `proposed`, `applied`, or `dismissed`                                                                                 |
| `entry_ts`      | string | State-change records only: the `ts` of the entry being promoted                                                                                  |
| `proposal_id`   | string | State-change records only: the proposal ID associated with the state change                                                                      |

### Entry Key

Each entry has a canonical key used for deduplication, curator validation, and state marking:

- **Failure entries**: `"Tool: InputSummary"` (e.g., `"Bash: npm run build"`)
- **Text-based entries** (reflection, friction): the `text` field value

This key is computed by `Entry.EntryKey()` and used throughout the system.

### How Agents Know to Capture

**Claude Code:** Capture is fully automatic via hooks installed at session spawn. No instruction file modification needed for Claude Code agents.

**Other agents:** The instruction file template (`SignalingInstructions` in `internal/provision/provision.go`) includes a "Friction Capture" section that instructs agents to append entries when they hit walls:

```markdown
## Friction Capture

When you hit a wall — wrong command, missing file, failed build, wrong assumption —
append what went wrong and the fix to `.schmux/lore.jsonl`. One JSON line:
{"ts":"<ISO8601>","ws":"<workspace-id>","agent":"<your-agent-type>","type":"friction","text":"When <trigger>, do <correction> instead"}

Only write when something tripped you up. Don't write what you built or learned —
write what would have saved you time if you'd known it before starting.
```

### Capture Cost

For Claude Code, capture is zero-cost to the agent — hooks run outside the agent's context window. The `PostToolUseFailure` hook fires automatically on failures. The `Stop` hook adds one reflection prompt per session.

For other agents, the cost is one file append per friction event — the same as the previous approach, but focused on friction (what went wrong) rather than general knowledge capture.

### Data Architecture

Lore uses a **one-directional** data flow: agents write raw entries to their workspace, the backend reads from all workspaces.

- **Raw entries** (written by agents): `<workspace>/.schmux/lore.jsonl` — each workspace has its own append-only JSONL file
- **State-change records** (written by backend): `~/.schmux/lore/<repoName>/state.jsonl` — a central file tracking proposed/applied/dismissed markers

This separation means:

- Raw entries are never broadcast between workspaces (no overlay compounding for lore)
- The backend aggregates entries from all workspace directories using `ReadEntriesMulti`
- State-change records live in a shared location so the curator can see which entries have already been processed regardless of which workspace they came from
- Workspace lore files are pure append-only logs that never need pruning; only the central state file is pruned

## Stage 2: Curator Agent

### Trigger Events

The curator runs as a headless LLM call (no tmux session, no workspace needed). It is triggered by:

1. **Session dispose** — after an agent session ends, schmux triggers curation based on the `curate_on_dispose` mode (`"session"`: any session dispose, `"workspace"`: only the last session in a workspace, `"never"`: disabled). A debounce of 30 seconds (configurable) prevents rapid-fire curation when multiple sessions end close together. Uses a 5-minute context timeout.
2. **Manual trigger** — user clicks "Trigger Curation" in the dashboard. Returns immediately with a curation ID; the curator runs in the background with a 3-minute context timeout. Only one manual curation per repo at a time (enforced by `CurationTracker`).

### Inputs

The curator receives:

1. **Raw scratchpad entries** — aggregated from `<workspace>/.schmux/lore.jsonl` for all workspaces matching the repo, plus `~/.schmux/lore/<repoName>/state.jsonl` for state-change records, filtered to entries in `raw` state only
2. **All instruction files** — read from the bare repo via `git show HEAD:<file>` for each configured instruction file pattern

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
      ".github/copilot-instructions.md",
      "CONVENTIONS.md"
    ]
  }
}
```

Only files that actually exist in the repo (via `git show HEAD:<file>`) are included. The curator adapts to whatever instruction files the project uses. If no instruction files are found, curation fails with an error.

### Curator Prompt

The full prompt is built by `BuildCuratorPrompt()` in `internal/lore/curator.go`:

```
You are a curator for a software project's agent instruction files.

You will receive:
1. Failure records: tool calls that failed during agent work sessions
2. Friction reflections: agent-reported papercuts and wrong assumptions
3. The current content of all instruction files

Your job is to produce a merge proposal — changes to the instruction files that
prevent future agents from repeating these mistakes.

Rules:
- SYNTHESIZE: Turn failure patterns into actionable rules
  (e.g., 5 "npm run build" failures → "Always use go run ./cmd/build-dashboard")
- DEDUPLICATE: Multiple agents hitting the same wall → one rule
- FILTER: Discard one-off failures that don't indicate systemic issues
  (e.g., a single typo in a file path is not lore-worthy)
- FILTER: Discard failures already covered by existing instructions
- ROUTE: Universal rules → all instruction files. Agent-specific → that file only
- CATEGORIZE: Place under appropriate existing section, or propose new section
- PRESERVE VOICE: Match tone, formatting, and style of each file
- NEVER REMOVE existing content — only add or refine
- Write rules as imperatives: "Use X, not Y" / "Always run X before Y"
- Output ONLY valid JSON matching the schema below, no markdown fencing

Output schema:
{
  "proposed_files": {"<filename>": "<full proposed content>", ...},
  "diff_summary": "<one-line summary of changes>",
  "entries_used": ["<for reflections: the text; for failures: 'Tool: input_summary'>", ...],
  "entries_discarded": {"<entry text or input_summary>": "<reason for discarding>", ...}
}

INSTRUCTION FILES:

=== <filename> ===
<content>

FAILURE RECORDS:
- [<agent>] [<tool>] [<category>] [<workspace>] command: "<input>" → error: "<error>"

FRICTION REFLECTIONS:
- [<agent>] [<type>] [<workspace>] <text>
```

Entries are separated into two sections: `FAILURE RECORDS` (type `failure`) and `FRICTION REFLECTIONS` (everything else — reflection, friction).

### Curator Output

The curator produces a JSON response:

```json
{
  "proposed_files": {
    "CLAUDE.md": "<full proposed content>",
    "AGENTS.md": "<full proposed content>"
  },
  "diff_summary": "Added 2 universal items to CLAUDE.md and AGENTS.md, 1 claude-specific item to CLAUDE.md only",
  "entries_used": [
    "When building dashboard, use go run ./cmd/build-dashboard",
    "Bash: npm run build"
  ],
  "entries_discarded": {
    "Read: /nonexistent/file.txt": "One-off typo, not a systemic issue"
  }
}
```

The `entries_used` values must match the `EntryKey()` of actual input entries — for text-based entries, the literal `text` field; for failure entries, the `"Tool: input_summary"` format.

### Response Parsing and Validation

`ParseCuratorResponse()` strips markdown fencing if present (handles LLMs that wrap output in `` `json `) then unmarshals JSON.

`BuildProposal()` validates the response against the input data:

1. **File key validation**: All keys in `proposed_files` must exist in the instruction files that were discovered. Rejects unknown files (prevents the LLM from inventing new files).
2. **Entry reference validation**: All values in `entries_used` must match an `EntryKey()` from the input entries. Rejects hallucinated entry references.

If validation fails, the curation fails with an error and no proposal is created.

### Streaming Curation

Manual curation uses a **streaming executor** when available (`oneshot.ExecuteTargetStreaming`). This provides real-time visibility into the curator's work:

1. Each LLM stream event is wrapped as a `CuratorEvent` with metadata (repo, timestamp, event type, subtype, raw JSON)
2. Events are accumulated in the `CurationTracker` (one active run per repo)
3. Events are broadcast to all connected dashboard WebSocket clients via `BroadcastCuratorEvent()` on the `/ws/dashboard` endpoint
4. Events are persisted to `~/.schmux/lore-curator-runs/<repo>/<curationId>.jsonl` for later review

When a streaming executor is not available, the fallback non-streaming executor is used (the LLM call blocks until completion with no intermediate events).

Auto-curation (triggered by session dispose) always uses the non-streaming executor.

### Error Handling

Errors are handled at five layers, each flowing through `completeCurationWithError()`:

| Error Layer   | What Fails                                                               | Effect                                                                 |
| ------------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| LLM execution | Streaming executor returns error, timeout, or process failure            | Error logged + broadcast via WebSocket + written to curation JSONL log |
| JSON parsing  | LLM response is not valid JSON (even after stripping markdown fences)    | Same as above                                                          |
| Validation    | LLM proposed an unknown file, or referenced an entry not in the input    | Same as above                                                          |
| Proposal save | Disk write fails when saving `~/.schmux/lore-proposals/<repo>/<id>.json` | Same as above                                                          |
| Entry marking | State-change records fail to write to `state.jsonl`                      | Logged as warning; proposal is still saved (non-fatal)                 |

`completeCurationWithError()` does three things:

1. Logs the error server-side with structured logging
2. Calls `curationTracker.Complete(repo, err)` to mark the run as done-with-error
3. Broadcasts a `curator_error` event via WebSocket to all connected dashboard clients

**Timeouts:**

- Manual curation context: 3 minutes
- LLM call within either executor: 2 minutes
- Auto-curation context: 5 minutes

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

The curator's executor is refreshed at runtime when the config is saved (`refreshLoreCurator()`), so changing `llm_target` takes effect without a daemon restart.

## Stage 3: Proposal Storage and Review

### Proposal Format

Proposals are stored at `~/.schmux/lore-proposals/<repo>/<id>.json`:

```json
{
  "id": "prop-20260213-143200-a1b2",
  "repo": "schmux",
  "created_at": "2026-02-13T14:32:00Z",
  "status": "pending",
  "source_count": 12,
  "sources": ["ws-abc123", "ws-def456"],
  "file_hashes": {
    "CLAUDE.md": "sha256:abc...",
    "AGENTS.md": "sha256:def..."
  },
  "current_files": {
    "CLAUDE.md": "<content at curation time>",
    "AGENTS.md": "<content at curation time>"
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

The proposal ID format is `prop-<YYYYMMDD-HHMMSS>-<4 random hex chars>` to avoid collisions.

The `file_hashes` field records the SHA-256 of each instruction file at curation time. If a file changes before the proposal is applied (e.g., someone manually edits CLAUDE.md), the proposal can be detected as stale.

The `current_files` field stores the full content of each instruction file at curation time, enabling the dashboard to render a diff between current and proposed content without needing access to the bare repo.

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

Only `raw` entries are fed to the curator. The state is tracked by appending state-change records to the central state file `~/.schmux/lore/<repoName>/state.jsonl` (preserving append-only semantics in workspace files):

```jsonl
{
  "ts": "...",
  "state_change": "proposed",
  "entry_ts": "2026-02-18T10:30:00Z",
  "proposal_id": "prop-20260213-143200-a1b2"
}
```

State resolution uses `FilterRaw()`: it builds a set of entry timestamps that have state-change records, then excludes those entries. Only entries with no state-change record are considered "raw."

### Scratchpad Pruning

Entries in `applied` or `dismissed` state older than `prune_after_days` (default 30) are pruned from the central state file on daemon startup. `raw` and `proposed` entries are never auto-pruned. Workspace lore files are never pruned (they are append-only logs).

Pruning is atomic: writes to a temp file, then renames. Protected by `scratchpadMu` mutex for safe concurrent use with `AppendEntry`.

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
│ Proposal: prop-20260213-143200-a1b2                  [Dismiss]  [Apply]  │
│ 3 lore entries from 2 workspaces • Feb 13, 2026                          │
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

#### Curation Status

When a curation is running, the sidebar shows a `CurationStatus` component with:

- Spinner with repo name and elapsed time
- Expandable `CuratorTerminal` showing streamed chain-of-thought (assistant text, thinking blocks, tool use)
- Error display for failed curations

The `CuratorTerminal` component handles multiple error formats from the streaming API: wrapped errors, direct errors, curator-specific errors, and assistant errors.

#### Past Curation Runs

A log of previous curation runs stored at `~/.schmux/lore-curator-runs/<repo>/<curationId>.jsonl`. Each run's JSONL log can be reviewed in the dashboard to see what the curator did.

#### Raw Lore Section

A scrollable log of all scratchpad entries across workspaces, with filters:

- By agent type
- By lore type (failure / reflection / friction)
- By state (raw / proposed / applied / dismissed)
- By limit

This lets the user see what agents are discovering before the curator runs, and manually trigger curation when enough raw lore accumulates.

### Sidebar Integration

The lore nav item shows a badge with the count of pending proposals.

### WebSocket Integration

The lore system uses the existing `/ws/dashboard` WebSocket endpoint (shared with session/workspace updates) for real-time curator event streaming.

**Server → Client messages:**

1. **`curator_event`**: Individual stream events during curation

   ```json
   {"type":"curator_event","event":{"repo":"schmux","timestamp":"...","event_type":"assistant","subtype":"","raw":{...}}}
   ```

2. **`curator_state`**: Full curation run state (sent on WebSocket connect for reconnecting clients)
   ```json
   {"type":"curator_state","run":{"id":"cur-...","repo":"schmux","events":[...],"done":false}}
   ```

The `CurationTracker` manages active curation runs in memory:

- One active curation per repo at a time
- Accumulates all streamed events for catch-up on reconnect
- Provides `Active()` and `Recent(duration)` for WebSocket state sync
- Opportunistically cleans up completed runs older than 5 minutes

**Client-side processing** (`useSessionsWebSocket.ts`):

- `curator_event` messages append to a per-repo event array
- `curator_state` messages replace the entire event array (bulk sync on reconnect)
- `CurationContext` derives active curations from event streams and detects completion

### API Endpoints

#### `GET /api/lore/status`

Returns the lore system configuration status: whether lore is enabled, whether the curator is configured (has an LLM target), the curate-on-dispose mode, and any issues (e.g., "No LLM target configured").

#### `GET /api/lore/curations/active`

Returns all active (in-progress) curation runs with their buffered events. Used for reconnecting clients to catch up on ongoing curations.

#### `GET /api/lore/{repo}/proposals`

Returns all proposals for a repo.

```json
{
  "proposals": [
    {
      "id": "prop-20260213-143200-a1b2",
      "created_at": "2026-02-13T14:32:00Z",
      "status": "pending",
      "source_count": 12,
      "diff_summary": "Added 3 items..."
    }
  ]
}
```

#### `GET /api/lore/{repo}/proposals/{id}`

Returns a single proposal with full content (including `current_files` and `proposed_files` for diff rendering).

#### `POST /api/lore/{repo}/proposals/{id}/apply`

Applies the proposal:

1. Validates the proposal is still `pending` (rejects if already applied/dismissed)
2. Accepts optional `overrides` in request body (keys must exist in the original proposal)
3. Creates branch `schmux/lore-<id-suffix>` from the default branch
4. Creates a temporary worktree
5. Writes proposed files (with path traversal protection)
6. Commits with message: `chore: update instruction files with agent lore\n\n<diff_summary>` (or `(<N> files)` if no summary)
7. Cleans up the temporary worktree (deferred, always runs even on error)
8. Pushes the branch to `origin`
9. Optionally creates a PR via `gh` CLI if `auto_pr` is enabled (failure is logged but non-fatal)
10. Updates the proposal status to `applied`
11. Marks source entries as `applied` in the central state JSONL via `MarkEntriesByTextMulti()`

Request body (optional, for edited content):

```json
{
  "overrides": {
    "CLAUDE.md": "<user-edited content>"
  }
}
```

#### `POST /api/lore/{repo}/proposals/{id}/dismiss`

Marks the proposal as `dismissed` and source entries as `dismissed`. Rejects if the proposal is already `applied`.

#### `POST /api/lore/{repo}/curate`

Manually triggers the curator. Returns immediately with a curation ID and `"status": "started"`. The curation runs in a background goroutine, streaming events via WebSocket.

Guards against concurrent curations: returns `409 Conflict` if a curation is already running for the repo. Returns `"status": "no_raw_entries"` if there are no raw entries to process.

#### `GET /api/lore/{repo}/entries`

Returns lore entries aggregated from all workspace directories and the central state file. Supports query parameters:

```
GET /api/lore/schmux/entries?state=raw&agent=claude-code&type=failure&limit=50
```

#### `GET /api/lore/{repo}/curations`

Lists past curation run logs (ID, file size, creation timestamp), sorted newest first.

#### `GET /api/lore/{repo}/curations/{id}/log`

Returns the JSONL log content for a specific curation run as an array of parsed JSON events.

## Git Commit Strategy

The base repo in schmux is a **bare clone** — it has no working directory. Each workspace is a worktree on its own branch, and workspaces may be disposed before lore is curated. To handle this, the apply step creates its own temporary worktree.

### Workflow

When a proposal is applied:

```
1. Determine default branch   → git symbolic-ref HEAD (fallback: "main")
2. Create branch              → git branch schmux/lore-<id-suffix> <default>
3. Create temporary worktree  → git worktree add <path> schmux/lore-<id-suffix>
4. Configure git user         → schmux-lore <schmux@localhost>
5. Write proposed files       → overwrite instruction files with curated content
                                (path traversal protection on every path)
6. Stage and commit           → git add <files> && git commit -m "<message>"
7. Cleanup worktree (deferred)→ git worktree remove --force <path>
                                (runs even if commit fails)
8. Push branch                → git push origin schmux/lore-<id-suffix>
9. Optional: create PR        → gh pr create (if auto_pr enabled)
```

The temporary worktree is short-lived — it exists only for the duration of the commit. The push and PR creation happen against the bare repo after the worktree is cleaned up.

### Merge Path

The pushed branch is available for the team to merge through their normal workflow:

- GitHub/GitLab: create a PR via `gh pr create` or equivalent
- Direct merge: `git merge schmux/lore-<id-suffix>` in any worktree
- Schmux auto-creates a PR when `auto_pr` is enabled (see below)

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
gh pr create --head schmux/lore-<id-suffix>
             --base <default-branch>
             --title "chore: update instruction files with agent lore"
             --body "<diff summary from curator>"
```

If `gh` is not installed or the PR creation fails, the error is logged but the apply still succeeds (the branch is already pushed). When `auto_pr` is `false` (the default), the branch is pushed but no PR is created.

## Configuration

Config fields in `~/.schmux/config.json`:

```json
{
  "lore": {
    "enabled": true,
    "llm_target": "claude-sonnet",
    "auto_pr": false,
    "curate_on_dispose": "session",
    "curate_debounce_ms": 30000,
    "prune_after_days": 30,
    "instruction_files": [
      "CLAUDE.md",
      "AGENTS.md",
      ".cursorrules",
      ".github/copilot-instructions.md",
      "CONVENTIONS.md"
    ]
  }
}
```

| Field                | Default         | Description                                                                                          |
| -------------------- | --------------- | ---------------------------------------------------------------------------------------------------- |
| `enabled`            | `true`          | Enable/disable the lore system                                                                       |
| `llm_target`         | compound target | LLM for curator calls                                                                                |
| `auto_pr`            | `false`         | Auto-create a PR after pushing the lore branch                                                       |
| `curate_on_dispose`  | `"session"`     | When to auto-curate: `"session"` (any session), `"workspace"` (last session in workspace), `"never"` |
| `curate_debounce_ms` | `30000`         | Debounce window for auto-curation (milliseconds)                                                     |
| `prune_after_days`   | `30`            | Days before applied/dismissed state-change records are pruned                                        |
| `instruction_files`  | see above       | Instruction file patterns to manage                                                                  |

**Backward compatibility:** `curate_on_dispose` was originally a boolean (`true`/`false`). Old configs are handled via custom `UnmarshalJSON`: `true` maps to `"session"`, `false` maps to `"never"`.

## Architecture

### Package: `internal/lore/`

| File            | Responsibility                                                                                                         |
| --------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `scratchpad.go` | Parse, append, query, prune scratchpad entries, multi-path reading with dedup, state resolution                        |
| `curator.go`    | Curator struct, LLM prompt construction, response parsing, proposal building, instruction file reading from bare repos |
| `proposals.go`  | ProposalStore: disk-based proposal storage, staleness detection via file hashes, status transitions                    |
| `apply.go`      | ApplyProposal: temp worktree, commit, cleanup. PushBranch, CreatePR via gh CLI                                         |

### Dashboard Integration

| File                | Responsibility                                                                               |
| ------------------- | -------------------------------------------------------------------------------------------- |
| `handlers_lore.go`  | All lore HTTP API handlers                                                                   |
| `curation_state.go` | CurationTracker and CurationRun for tracking active/completed curations with streamed events |
| `server.go`         | BroadcastCuratorEvent, WebSocket curator_event/curator_state messages                        |

### Frontend

| File                             | Responsibility                                                                   |
| -------------------------------- | -------------------------------------------------------------------------------- |
| `routes/LorePage.tsx`            | Main lore page: repo tabs, proposals, diff viewer, raw signals, past runs        |
| `components/CuratorTerminal.tsx` | Renders streamed curator events: thinking, text, tool use, errors                |
| `components/CurationStatus.tsx`  | Sidebar component showing active curations with spinner and elapsed time         |
| `contexts/CurationContext.tsx`   | React context for curation state: derives active curations from WebSocket events |
| `hooks/useSessionsWebSocket.ts`  | WebSocket hook processing curator_event and curator_state messages               |
| `lib/types.ts`                   | TypeScript interfaces for lore types                                             |
| `lib/api.ts`                     | API client functions for lore endpoints                                          |
| `styles/lore.module.css`         | CSS module for lore page styling                                                 |

### Integration Points

| Component                          | Role                                                                                                                                                   |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/config/`                 | `LoreConfig` struct with getter methods and backward-compat unmarshaling                                                                               |
| `internal/daemon/daemon.go`        | Lore initialization: creates ProposalStore and Curator, wires streaming executor, sets lore callback on session dispose with debounce, startup pruning |
| `internal/oneshot/`                | `ExecuteTarget` (non-streaming) and `ExecuteTargetStreaming` (streaming) LLM executors                                                                 |
| `internal/workspace/ensure/hooks/` | Embedded hook scripts (`capture-failure.sh`, `stop-gate.sh`)                                                                                           |

### Data Flow

```
 Agent in ws-abc (claude-code)         Agent in ws-def (codex)
 ─────────────────────────            ────────────────────────
 PostToolUseFailure hook fires:       self-captures friction:
 "npm run build" → wrong_command      "tests need --race for
 Stop hook captures reflection:        compound package tests"
 "use go run ./cmd/build-dashboard"
      │                                    │
      ▼                                    ▼
 appends to                           appends to
 ws-abc/.schmux/lore.jsonl            ws-def/.schmux/lore.jsonl
      │                                    │
      └──────────────┬─────────────────────┘
                     │
                     ▼  (session dispose or manual trigger)
 ┌──────────────────────────────────────────────┐
 │  Backend aggregates from all workspaces      │
 │  ReadEntriesMulti([                          │
 │    ws-abc/.schmux/lore.jsonl,                │
 │    ws-def/.schmux/lore.jsonl,                │
 │    ~/.schmux/lore/<repo>/state.jsonl         │
 │  ])                                          │
 │  deduplicates by {ts, ws, EntryKey()}        │
 │  filters to raw entries only                 │
 └──────────────────────────────────────────────┘
      │
      ▼
 ┌──────────────────────────────────────────────┐
 │  Curator (headless LLM call)                 │
 │  reads: aggregated failure + reflection      │
 │         entries (raw only)                   │
 │  reads: instruction files from bare repo     │
 │         via git show HEAD:<file>             │
 │  synthesizes: failure patterns → rules       │
 │  deduplicates: similar friction → one rule   │
 │  routes: universal → both files              │
 │  produces: multi-file merge proposal         │
 │                                              │
 │  [manual trigger only]:                      │
 │  streams events → WebSocket → dashboard      │
 │  persists events → JSONL log file            │
 └──────────────────────────────────────────────┘
      │
      ▼
 ~/.schmux/lore-proposals/<repo>/
     prop-20260213-143200-a1b2.json

 State changes written to:
 ~/.schmux/lore/<repo>/state.jsonl
      │
      ▼  (dashboard badge appears)
 ┌──────────────────────────────────────────────┐
 │  Dashboard: user reviews diff per file       │
 │  clicks "Apply" (with optional edits)        │
 └──────────────────────────────────────────────┘
      │
      ▼
 Temp worktree spawned on schmux/lore-<id-suffix>
 Instruction files committed and pushed
 Worktree disposed (deferred cleanup)
      │
      ▼
 State-change records appended to
 ~/.schmux/lore/<repo>/state.jsonl
 (kept for audit trail, pruned after 30 days)
```
