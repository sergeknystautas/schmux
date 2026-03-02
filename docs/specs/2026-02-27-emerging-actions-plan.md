# Emerging Actions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace static QuickLaunch presets with a unified action registry that supports manual, migrated, and curator-proposed actions — with usage tracking, confidence decay, and prompt autocomplete.

**Architecture:** New `internal/actions/` package owns the registry data model, file I/O, and lifecycle (create/pin/dismiss/decay/track). The lore system gains a second curator mode for action proposals. The dashboard replaces the QuickLaunch dropdown with an action-backed dropdown and adds an Actions tab to the Lore page.

**Tech Stack:** Go (backend), React + TypeScript (dashboard), JSONL events (signal capture), JSON files (registry persistence)

**Design Spec:** `docs/specs/2026-02-26-emerging-actions-design.md`

---

## Architectural Decisions

### AD-1: Registry concurrency model

The registry file (`~/.schmux/actions/<repo>/registry.json`) can be read by HTTP handlers and written by spawn tracking, decay, and curation — all concurrently. Use an **in-memory cache with mutex** rather than file-level locking. The `Registry` struct holds a `sync.RWMutex`, loads from disk on startup, and writes back on every mutation (temp-file + rename, same pattern as `ProposalStore`).

### AD-2: Template matching at spawn time

For usage tracking, the spawn handler needs to match a free-text prompt against action templates like `"Fix all lint errors in {{path}}"`. In v1, use **exact prefix match after stripping parameters**: extract the static prefix of each template (text before the first `{{`), and check if the user's prompt starts with it. This is cheap, deterministic, and avoids LLM calls. Fuzzy/semantic matching can come later.

### AD-3: Decay schedule

Run decay as a **lazy check on registry load**, not a background goroutine. When the registry is loaded from disk, scan for pinned actions where `last_used` (or `pinned_at` if never used) is older than 30 days. Reduce confidence by 0.1 per 30-day period of disuse. Auto-dismiss when confidence drops below 0.3. This keeps the system simple — no timers, no background work.

### AD-4: Migration timing

Migration happens **on daemon startup** when the daemon initializes the action registry for each configured repo. If `quick_launch` exists in config and the registry file doesn't exist (or has no `migrated` entries), migrate. This is a one-time, idempotent operation.

### AD-5: API contract types

Actions are repo-scoped. The API routes are nested under `/api/actions/{repo}`. The action struct lives in `internal/api/contracts/actions.go` so it generates TypeScript types via `gen-types`.

### AD-6: Parameter handling in dropdown

Pinned agent actions with parameters are **one-click with learned defaults**. If a parameter has a default value, it's substituted into the template before spawning. If no default exists, the action opens the spawn page with the template pre-filled (the user fills in parameters manually). This means the dropdown is zero-friction for well-established actions and gracefully degrades for new ones.

---

## Task Dependency Graph

```
Phase 1: Action Registry Package
  Task 1 → Task 2 → Task 3

Phase 2: API Layer
  Task 4 → Task 5

Phase 3: Migration
  Task 6 (depends on Task 2, Task 4)

Phase 4: UI — Action Dropdown
  Task 7 → Task 8 (depends on Task 5, Task 6)

Phase 5: Usage Tracking
  Task 9 (depends on Task 2, Task 4)

Phase 6: Action Curator
  Task 10 → Task 11 (depends on Task 2)

Phase 7: UI — Lore Actions Tab
  Task 12 (depends on Task 5, Task 11)

Phase 8: UI — Spawn Autocomplete
  Task 13 (depends on Task 5)
```

---

## Phase 1: Action Registry Package

### Task 1: Action data model and contracts

Define the Action struct in the API contracts layer so TypeScript types are auto-generated.

**Files:**

- Create: `internal/api/contracts/actions.go`
- Modify: `cmd/gen-types/main.go` (register new types for generation)

**Data model:**

```go
// internal/api/contracts/actions.go

type ActionType string
const (
    ActionTypeAgent   ActionType = "agent"
    ActionTypeCommand ActionType = "command"
    ActionTypeShell   ActionType = "shell"
)

type ActionSource string
const (
    ActionSourceEmerged  ActionSource = "emerged"
    ActionSourceManual   ActionSource = "manual"
    ActionSourceMigrated ActionSource = "migrated"
)

type ActionState string
const (
    ActionStateProposed  ActionState = "proposed"
    ActionStatePinned    ActionState = "pinned"
    ActionStateDismissed ActionState = "dismissed"
)

type ActionParameter struct {
    Name    string `json:"name"`
    Default string `json:"default,omitempty"`
}

type LearnedDefault struct {
    Value      string  `json:"value"`
    Confidence float64 `json:"confidence"`
}

type Action struct {
    ID        string          `json:"id"`
    Name      string          `json:"name"`
    Type      ActionType      `json:"type"`
    Scope     string          `json:"scope"`

    // agent type
    Template   string            `json:"template,omitempty"`
    Parameters []ActionParameter `json:"parameters,omitempty"`
    Target     string            `json:"target,omitempty"`
    Persona    string            `json:"persona,omitempty"`

    // command type
    Command string `json:"command,omitempty"`

    // learned defaults (emerged actions)
    LearnedTarget  *LearnedDefault `json:"learned_target,omitempty"`
    LearnedPersona *LearnedDefault `json:"learned_persona,omitempty"`

    // lifecycle
    Source        ActionSource `json:"source"`
    Confidence    float64      `json:"confidence"`
    EvidenceCount int          `json:"evidence_count"`
    State         ActionState  `json:"state"`
    UseCount      int          `json:"use_count"`
    EditCount     int          `json:"edit_count"`

    // timestamps
    FirstSeen  time.Time  `json:"first_seen"`
    LastUsed   *time.Time `json:"last_used,omitempty"`
    ProposedAt *time.Time `json:"proposed_at,omitempty"`
    PinnedAt   *time.Time `json:"pinned_at,omitempty"`
}

// API request/response types

type ActionRegistryResponse struct {
    Actions []Action `json:"actions"`
}

type CreateActionRequest struct {
    Name      string          `json:"name"`
    Type      ActionType      `json:"type"`
    Template  string          `json:"template,omitempty"`
    Parameters []ActionParameter `json:"parameters,omitempty"`
    Target    string          `json:"target,omitempty"`
    Persona   string          `json:"persona,omitempty"`
    Command   string          `json:"command,omitempty"`
}

type UpdateActionRequest struct {
    Name      *string          `json:"name,omitempty"`
    Template  *string          `json:"template,omitempty"`
    Parameters *[]ActionParameter `json:"parameters,omitempty"`
    Target    *string          `json:"target,omitempty"`
    Persona   *string          `json:"persona,omitempty"`
    Command   *string          `json:"command,omitempty"`
}
```

**Steps:**

1. Write the contracts file with the types above
2. Register the new types in `cmd/gen-types/main.go`
3. Run `go run ./cmd/gen-types` to generate TypeScript types
4. Run `go build ./...` to verify compilation
5. Commit: "feat(actions): add action data model contracts"

---

### Task 2: Registry store with file I/O

The core registry: in-memory cache backed by JSON file, with mutex for concurrency.

**Files:**

- Create: `internal/actions/registry.go`
- Create: `internal/actions/registry_test.go`

**Key interface:**

```go
// internal/actions/registry.go

type Registry struct {
    mu       sync.RWMutex
    actions  []Action
    baseDir  string  // ~/.schmux/actions
    repo     string
}

func NewRegistry(baseDir, repo string) *Registry
func (r *Registry) Load() error                              // read from disk, apply decay
func (r *Registry) List(state ActionState) []Action           // filtered by state
func (r *Registry) Get(id string) (Action, bool)
func (r *Registry) Create(req CreateActionRequest) (Action, error)  // source=manual, state=pinned
func (r *Registry) Pin(id string) error                       // proposed → pinned
func (r *Registry) Dismiss(id string) error                   // any → dismissed
func (r *Registry) Update(id string, req UpdateActionRequest) error
func (r *Registry) Delete(id string) error                    // hard delete
func (r *Registry) RecordUse(id string, edited bool) error    // increment use_count/edit_count
func (r *Registry) AddProposed(actions []Action) error        // from curator
func (r *Registry) MigrateQuickLaunch(presets []QuickLaunch) (int, error) // one-time migration
```

**Internal methods:**

```go
func (r *Registry) save() error           // temp-file + rename (caller holds lock)
func (r *Registry) filePath() string       // baseDir/repo/registry.json
func (r *Registry) applyDecay()            // reduce confidence for stale actions
func (r *Registry) generateID() string     // "act-" + 8 random hex chars
```

**Testing strategy:**

- Use `t.TempDir()` for file storage
- Test CRUD operations
- Test concurrent access (parallel goroutines)
- Test decay logic (mock timestamps)
- Test migration from QuickLaunch presets
- Test idempotent migration (calling twice is safe)
- Test atomic file writes (crash safety)

**Steps:**

1. Write failing tests for `NewRegistry` + `Load` (empty dir, missing file)
2. Implement `NewRegistry` and `Load`
3. Write failing tests for `Create` + `List` + `Get`
4. Implement `Create`, `List`, `Get`, `save`
5. Write failing tests for `Pin`, `Dismiss`, `Update`, `Delete`
6. Implement state transition methods
7. Write failing tests for `RecordUse`
8. Implement usage tracking
9. Write failing tests for `applyDecay`
10. Implement decay logic
11. Write failing tests for `MigrateQuickLaunch`
12. Implement migration
13. Write failing tests for `AddProposed`
14. Implement proposed action insertion
15. Run full test suite
16. Commit: "feat(actions): add registry store with file I/O"

---

### Task 3: Template matching for usage tracking

Utility for matching a user prompt against action templates at spawn time.

**Files:**

- Create: `internal/actions/match.go`
- Create: `internal/actions/match_test.go`

**Interface:**

```go
// Returns the best matching action ID, or "" if no match.
// Also returns whether the prompt was edited (differs from template with defaults filled in).
func (r *Registry) MatchPrompt(prompt string) (actionID string, edited bool)
```

**Algorithm (v1 — prefix match):**

1. For each pinned agent action, extract the static prefix (text before first `{{`)
2. Normalize both prefix and prompt (lowercase, trim whitespace)
3. If prompt starts with normalized prefix, it's a candidate
4. Among candidates, pick the longest prefix (most specific match)
5. `edited` = true if the prompt doesn't exactly match the template with defaults substituted

**Testing strategy:**

- Exact match → actionID found, edited=false
- Prefix match with different parameter → actionID found, edited=true
- No match → empty actionID
- Multiple candidates → longest prefix wins
- Command/shell actions → never match (only agent actions)
- Empty template → never match

**Steps:**

1. Write failing tests for the matching scenarios above
2. Implement `MatchPrompt`
3. Run tests
4. Commit: "feat(actions): add template matching for usage tracking"

---

## Phase 2: API Layer

### Task 4: HTTP handlers for action CRUD

Wire the registry into the dashboard HTTP server.

**Files:**

- Create: `internal/dashboard/handlers_actions.go`
- Modify: `internal/dashboard/server.go` (add routes, registry field)
- Modify: `assets/dashboard/src/lib/api.ts` (frontend API client)

**Routes:**

| Method | Route                              | Handler               | Notes                  |
| ------ | ---------------------------------- | --------------------- | ---------------------- |
| GET    | `/api/actions/{repo}`              | `handleListActions`   | Returns pinned actions |
| POST   | `/api/actions/{repo}`              | `handleCreateAction`  | Manual creation        |
| PUT    | `/api/actions/{repo}/{id}`         | `handleUpdateAction`  | Edit action            |
| DELETE | `/api/actions/{repo}/{id}`         | `handleDeleteAction`  | Hard delete            |
| POST   | `/api/actions/{repo}/{id}/pin`     | `handlePinAction`     | Proposed → pinned      |
| POST   | `/api/actions/{repo}/{id}/dismiss` | `handleDismissAction` | Any → dismissed        |
| GET    | `/api/actions/{repo}/proposed`     | `handleListProposed`  | For lore page          |

**Server initialization:**

- `Server` struct gets a `registries map[string]*actions.Registry` field (keyed by repo name)
- On startup, initialize registries for each configured repo
- Lazy initialization if a repo is spawned that wasn't in initial config

**Frontend API client additions:**

```typescript
export async function getActions(repo: string): Promise<Action[]>;
export async function createAction(repo: string, req: CreateActionRequest): Promise<Action>;
export async function updateAction(
  repo: string,
  id: string,
  req: UpdateActionRequest
): Promise<Action>;
export async function deleteAction(repo: string, id: string): Promise<void>;
export async function pinAction(repo: string, id: string): Promise<void>;
export async function dismissAction(repo: string, id: string): Promise<void>;
export async function getProposedActions(repo: string): Promise<Action[]>;
```

**Steps:**

1. Add registry map to `Server` struct and initialization in `NewServer`/startup
2. Write `handlers_actions.go` with all handlers
3. Register routes in `server.go`
4. Add frontend API functions in `api.ts`
5. Run `go build ./...` to verify
6. Commit: "feat(actions): add HTTP API for action CRUD"

---

### Task 5: Prompt history endpoint

API endpoint that returns past prompts from event JSONL files, for spawn page autocomplete.

**Files:**

- Create: `internal/actions/history.go`
- Create: `internal/actions/history_test.go`
- Modify: `internal/dashboard/handlers_actions.go` (add handler)
- Modify: `internal/dashboard/server.go` (add route)

**Route:** `GET /api/actions/{repo}/prompt-history`

**Response:**

```json
{
  "prompts": [
    { "text": "fix lint errors in src/", "last_seen": "2026-02-25T...", "count": 3 },
    { "text": "add tests for auth module", "last_seen": "2026-02-24T...", "count": 1 }
  ]
}
```

**Implementation:**

- Scan all workspace event JSONL files for the given repo
- Filter for `status` events with non-empty `intent` field
- Also include spawn-time prompts (stored in events)
- Deduplicate by exact text, track count and last_seen
- Sort by last_seen descending
- Cap at 100 most recent

**Steps:**

1. Write failing tests for `CollectPromptHistory`
2. Implement the function
3. Add handler and route
4. Add frontend API function
5. Run tests
6. Commit: "feat(actions): add prompt history endpoint"

---

## Phase 3: Migration

### Task 6: QuickLaunch → Action migration

Migrate existing QuickLaunch presets to the action registry on startup.

**Files:**

- Modify: `internal/dashboard/server.go` (call migration on startup)
- Modify: `internal/actions/registry.go` (`MigrateQuickLaunch` already defined in Task 2)

**Migration logic (in `MigrateQuickLaunch`):**

```go
func (r *Registry) MigrateQuickLaunch(presets []QuickLaunch) (int, error) {
    r.mu.Lock()
    defer r.mu.Unlock()

    // Skip if already migrated (any migrated-source action exists)
    for _, a := range r.actions {
        if a.Source == ActionSourceMigrated {
            return 0, nil
        }
    }

    now := time.Now()
    count := 0
    for _, p := range presets {
        action := Action{
            ID:         r.generateID(),
            Name:       p.Name,
            Scope:      "repo",
            Source:      ActionSourceMigrated,
            Confidence: 1.0,
            State:      ActionStatePinned,
            FirstSeen:  now,
            PinnedAt:   &now,
        }
        if p.Command != "" {
            action.Type = ActionTypeCommand
            action.Command = p.Command
        } else {
            action.Type = ActionTypeAgent
            action.Target = p.Target
            if p.Prompt != nil {
                action.Template = *p.Prompt
            }
        }
        r.actions = append(r.actions, action)
        count++
    }
    return count, r.save()
}
```

**Startup integration (in `server.go` or `daemon.go`):**

```go
// For each repo in config:
//   1. Get global + repo-level quick_launch presets
//   2. Call registry.MigrateQuickLaunch(presets)
//   3. Log migration count
```

**Important:** Do NOT remove `quick_launch` from config yet. Keep it for one version cycle (the spec says "reversible"). The dropdown UI will read from the action registry, but the config editor still shows QuickLaunch for backward compat. QuickLaunch removal is a follow-up task.

**Steps:**

1. Write integration test: config with QuickLaunch → migrate → verify actions
2. Add migration call to server startup
3. Test idempotency (second call is no-op)
4. Run full test suite
5. Commit: "feat(actions): migrate QuickLaunch presets to action registry"

---

## Phase 4: UI — Action Dropdown

### Task 7: ActionDropdown component

Replace the QuickLaunch portion of the [+] dropdown with actions from the registry.

**Files:**

- Create: `assets/dashboard/src/components/ActionDropdown.tsx`
- Create: `assets/dashboard/src/components/ActionDropdown.module.css`
- Create: `assets/dashboard/src/hooks/useActions.ts`
- Modify: `assets/dashboard/src/components/SessionTabs.tsx`

**Component design:**

```
ActionDropdown
├── "Spawn a session..." (bold, navigates to spawn page)
├── ─────────────── separator ───────────────
├── PinnedAction (agent: name + confidence dots, one-click spawn)
├── PinnedAction (agent: name + confidence dots)
├── PinnedAction (command: name + ▪ marker)
├── PinnedAction (shell: name + ▪ marker)
├── ─────────────── separator ───────────────
└── "+ Add action" (opens action editor)
```

**`useActions` hook:**

```typescript
function useActions(repo: string) {
  // Fetches actions from GET /api/actions/{repo}
  // Returns { actions, loading, error, refetch }
  // Cached in React state, refetched on dropdown open
}
```

**Confidence dots rendering:**

```typescript
function ConfidenceDots({ confidence }: { confidence: number }) {
  // 4 dots: filled (●) for each 0.25 increment, empty (○) for remainder
  // ≥0.75 → ●●●○, ≥0.50 → ●●○○, etc.
  // command/shell actions show ▪ instead of dots
}
```

**One-click spawn behavior:**

- Agent action with all parameters having defaults → substitute defaults, spawn immediately
- Agent action with parameters missing defaults → navigate to spawn page with template pre-filled
- Command action → spawn with `command` field
- Shell action → spawn with shell mode

**Steps:**

1. Create `useActions` hook
2. Create `ActionDropdown` component with pinned actions list
3. Create `ConfidenceDots` sub-component
4. Wire one-click spawn: construct `SpawnRequest` from action, call `spawnSessions`
5. Add "+ Add action" button (initially navigates to config, editor comes later)
6. Style with CSS module
7. Write component tests (render, click, spawn flow)
8. Commit: "feat(ui): add ActionDropdown component"

---

### Task 8: Replace QuickLaunch in SessionTabs

Swap the old QuickLaunch dropdown for the new ActionDropdown.

**Files:**

- Modify: `assets/dashboard/src/components/SessionTabs.tsx`
- Modify: `assets/dashboard/src/lib/quicklaunch.ts` (deprecate or keep for fallback)

**Changes to SessionTabs:**

1. Replace `quickLaunch` memo with `useActions(workspace.repo)` call
2. Replace the portal dropdown content with `<ActionDropdown>`
3. Update `handleQuickLaunchSpawn` → `handleActionSpawn`
4. The spawn request now sends `action_id` instead of `quick_launch_name`

**Spawn request change:**

```go
// In SpawnRequest, add:
ActionID string `json:"action_id,omitempty"`
```

The spawn handler resolves `action_id` from the registry instead of looking up QuickLaunch by name.

**Steps:**

1. Add `action_id` field to `SpawnRequest` in Go + regenerate types
2. Add action resolution logic in spawn handler (parallel to QuickLaunch resolution)
3. Update SessionTabs to use ActionDropdown
4. Update spawn flow to send `action_id`
5. Keep QuickLaunch resolution as fallback (backward compat)
6. Write/update tests
7. Commit: "feat(ui): replace QuickLaunch with ActionDropdown in SessionTabs"

---

## Phase 5: Usage Tracking

### Task 9: Track action usage at spawn time

When a session is spawned, match the prompt against the registry and update usage stats.

**Files:**

- Modify: `internal/dashboard/handlers_spawn.go`
- Modify: `internal/actions/registry.go` (if `RecordUse` needs refinement)

**Spawn handler changes:**

After successful spawn, add:

```go
// If action_id was provided (one-click spawn), record use directly
if req.ActionID != "" {
    registry.RecordUse(req.ActionID, false)
} else if req.Prompt != "" {
    // Free-text prompt: try to match against registered actions
    if matchID, edited := registry.MatchPrompt(req.Prompt); matchID != "" {
        registry.RecordUse(matchID, edited)
    }
}
```

This is a non-blocking, cheap operation (mutex lock + file write). No LLM calls.

**Steps:**

1. Write test: spawn with `action_id` → use_count increments
2. Write test: spawn with matching prompt → use_count increments, edited flag correct
3. Write test: spawn with non-matching prompt → no registry change
4. Implement in spawn handler
5. Run tests
6. Commit: "feat(actions): track action usage at spawn time"

---

## Phase 6: Action Curator

### Task 10: Intent signal collector

Read `status` events with `intent` fields from workspace event files, deduplicate, and prepare input for the curator.

**Files:**

- Create: `internal/actions/signals.go`
- Create: `internal/actions/signals_test.go`

**Interface:**

```go
type IntentSignal struct {
    Text      string    `json:"text"`
    Timestamp time.Time `json:"ts"`
    Target    string    `json:"target,omitempty"`
    Persona   string    `json:"persona,omitempty"`
    Workspace string    `json:"workspace,omitempty"`
    Session   string    `json:"session,omitempty"`
}

// CollectIntentSignals reads all event files for a repo's workspaces
// and returns status events with non-empty intent fields.
func CollectIntentSignals(workspacePaths []string) ([]IntentSignal, error)
```

**Implementation notes:**

- Reuse `events.ReadEvents` with a filter for `type=="status"` and non-empty `intent`
- Enrich with target/persona from session metadata (look up session by ID in state)
- Deduplicate by exact `intent` text (keep highest count + latest timestamp)
- Return sorted by count descending (most frequent first)

**Steps:**

1. Write failing tests with sample JSONL event data
2. Implement `CollectIntentSignals`
3. Test deduplication and sorting
4. Run tests
5. Commit: "feat(actions): add intent signal collector"

---

### Task 11: Action curator mode

Add a second curator prompt mode to the lore system that produces action proposals.

**Files:**

- Create: `internal/lore/action_curator.go`
- Create: `internal/lore/action_curator_test.go`
- Modify: `internal/dashboard/handlers_lore.go` (add trigger endpoint)
- Modify: `internal/dashboard/server.go` (add route)

**Curator prompt structure:**

```
You are an action curator for a multi-agent development environment.

You observe what users repeatedly ask their AI agents to do and propose
reusable actions that can be triggered with one click.

CURRENT ACTIONS:
(list of existing pinned/proposed actions to avoid duplicates)

INTENT SIGNALS (what users have been asking):
- "fix lint errors in src/" (×3, target: sonnet, persona: code-engineer)
- "add tests for auth module" (×2, target: opus, persona: code-engineer)
- "run the linter and fix everything in src/" (×2, target: sonnet)
...

OUTPUT: JSON with proposed_actions[] and entries_discarded{}
(schema matches design spec)
```

**Response type:**

```go
type ActionCuratorResponse struct {
    ProposedActions  []ProposedAction  `json:"proposed_actions"`
    EntriesDiscarded map[string]string `json:"entries_discarded"`
}

type ProposedAction struct {
    Name            string            `json:"name"`
    Template        string            `json:"template"`
    Parameters      []ActionParameter `json:"parameters,omitempty"`
    LearnedDefaults map[string]LearnedDefault `json:"learned_defaults,omitempty"`
    EvidenceKeys    []string          `json:"evidence_keys"`
}
```

**Integration with lore handlers:**

New route: `POST /api/lore/{repo}/curate-actions`

This parallels the existing `POST /api/lore/{repo}/curate` but uses the action curator prompt and saves results to the action registry (as `proposed` state) instead of the proposal store.

**Steps:**

1. Write the action curator prompt builder with tests
2. Write the response parser with tests
3. Implement `ActionCurator.Curate` method
4. Add HTTP handler and route
5. Wire into CurationContext (frontend) or keep as separate trigger
6. Write integration test: sample signals → curator → proposed actions in registry
7. Run full test suite
8. Commit: "feat(lore): add action curator mode"

---

## Phase 7: UI — Lore Actions Tab

### Task 12: Actions tab on Lore page

Add an "Actions" tab to the Lore page showing proposed actions for review.

**Files:**

- Modify: `assets/dashboard/src/routes/LorePage.tsx`
- Create: `assets/dashboard/src/components/ProposedActionCard.tsx`
- Create: `assets/dashboard/src/components/ProposedActionCard.module.css`
- Modify: `assets/dashboard/src/styles/lore.module.css`

**Tab structure:**

```
LORE    [Instructions]  [Actions]  [Signals]
```

The "Actions" tab shows:

- **Proposed actions** — `ProposedActionCard` components (similar to `ProposalCard`)
- **Pinned actions** — Compact list of active actions with usage stats
- **"Trigger Action Curation"** button (calls `POST /api/lore/{repo}/curate-actions`)

**ProposedActionCard design:**

```
┌──────────────────────────────────────────────────┐
│ ○ Proposed  "Fix lint errors"                    │
│                                                  │
│ Based on 5 similar prompts:                      │
│   • "fix lint errors in src/"                    │
│   • "fix all linting issues in src/components"   │
│   • "run the linter and fix everything"          │
│                                                  │
│ Learned defaults:                                │
│   target: sonnet (4/5 times)                     │
│   persona: code-engineer (5/5 times)             │
│                                                  │
│ [Pin]  [Dismiss]  [Edit & Pin]                   │
└──────────────────────────────────────────────────┘
```

**Actions:**

- **Pin** → `POST /api/actions/{repo}/{id}/pin`, then refetch
- **Dismiss** → `POST /api/actions/{repo}/{id}/dismiss`, then refetch
- **Edit & Pin** → Opens inline editor (name, template, target, persona fields), then pin with edits

**Steps:**

1. Add tab switcher to LorePage (Instructions | Actions | Signals)
2. Create `ProposedActionCard` component
3. Add Pin/Dismiss/Edit handlers
4. Add "Trigger Action Curation" button
5. Add pinned actions list below proposed section
6. Style components
7. Write component tests
8. Commit: "feat(ui): add Actions tab to Lore page"

---

## Phase 8: UI — Spawn Autocomplete

### Task 13: Prompt autocomplete on spawn page

Add autocomplete to the prompt textarea on the spawn page, backed by action templates and prompt history.

**Files:**

- Create: `assets/dashboard/src/components/PromptAutocomplete.tsx`
- Create: `assets/dashboard/src/components/PromptAutocomplete.module.css`
- Create: `assets/dashboard/src/hooks/usePromptHistory.ts`
- Modify: `assets/dashboard/src/routes/SpawnPage.tsx`

**Data sources (loaded on mount):**

1. **Pinned action templates** — from `useActions(repo)`, shown first with `(×N)` usage count
2. **Raw prompt history** — from `GET /api/actions/{repo}/prompt-history`, shown second with date

**Matching algorithm (client-side):**

- On each keystroke, filter both lists by case-insensitive prefix match
- Then apply fuzzy substring match for non-prefix matches (ranked lower)
- Show max 8 suggestions total (templates first, then history)
- Keyboard navigation: arrow keys to select, Enter/Tab to fill, Escape to dismiss

**Behavior on selection:**

- **Action template selected** → fill prompt, apply learned target/persona defaults (if high confidence), show "from action: Fix lint errors" indicator
- **Raw prompt selected** → fill prompt only, no target/persona changes

**Steps:**

1. Create `usePromptHistory` hook
2. Create `PromptAutocomplete` component with dropdown overlay
3. Implement prefix + fuzzy matching
4. Handle keyboard navigation
5. Wire into SpawnPage prompt textarea
6. Apply learned defaults on template selection
7. Style with CSS module
8. Write component tests
9. Commit: "feat(ui): add prompt autocomplete to spawn page"

---

## Implementation Order Summary

The tasks can be parallelized as follows:

```
Sequential chains:
  T1 → T2 → T3 → T9 (registry + matching + usage tracking)
  T4 → T5 (API layer)
  T10 → T11 (curator)

Dependencies:
  T6 requires T2 + T4
  T7 requires T5
  T8 requires T5 + T7
  T9 requires T2 + T4
  T12 requires T5 + T11
  T13 requires T5

Suggested execution order:
  T1 → T2 → T3 (pure Go, no UI)
  T4 → T5 (API, needs T1)
  T6 (migration, needs T2 + T4)
  T7 → T8 (UI dropdown, needs T5 + T6)
  T9 (usage tracking, needs T2 + T4)
  T10 → T11 (curator, needs T2)
  T12 (lore UI, needs T5 + T11)
  T13 (autocomplete, needs T5)
```

## What's NOT in This Plan

Per the design spec's "out of scope" section:

- Cross-repo action sharing
- Contextual/algorithmic ranking in the dropdown
- Slash command generation from actions
- Auto-spawn (always requires user click)
- Workflow sequence detection

Also deferred:

- **QuickLaunch removal from config** — keep for one version cycle, remove in follow-up
- **QuickLaunchTab removal from config UI** — same, follow-up
- **Action editor modal** — the "+ Add action" button in the dropdown initially navigates to a simple form; a polished inline editor is a follow-up
- **Decay tuning** — the 0.1/30-day decay rate is a starting point; may need tuning based on real usage
