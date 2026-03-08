# Emergence System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace the current actions system with an emergence engine that observes user prompts, distills them into reusable skills, and injects them into agents' native skill systems.

**Architecture:** New `internal/emergence/` package with spawn entries store + emergence metadata. Adapters gain `InjectSkill`/`RemoveSkill` methods to write skill files into workspace-level agent paths (`.claude/skills/`, `.opencode/commands/`). Built-in skills ship embedded in the binary. Emergence curator runs as a second LLM pass alongside lore, producing skill proposals. Frontend shows proposals on the Lore page and skill-backed entries in the spawn dropdown.

**Tech Stack:** Go (backend), React/TypeScript (dashboard), existing lore LLM executor infrastructure

**Design spec:** `docs/specs/2026-03-07-emergence-system-design.md`

---

## Phase 1: Data Layer

### Task 1: API Contracts for Emergence

**Files:**

- Create: `internal/api/contracts/emergence.go`
- Test: `internal/api/contracts/emergence_test.go`

**Step 1: Write the types**

```go
package contracts

import "time"

// SpawnEntryType distinguishes what a spawn entry does.
type SpawnEntryType string

const (
	SpawnEntrySkill   SpawnEntryType = "skill"
	SpawnEntryCommand SpawnEntryType = "command"
	SpawnEntryAgent   SpawnEntryType = "agent"
	SpawnEntryShell   SpawnEntryType = "shell"
)

// SpawnEntrySource tracks how the entry was created.
type SpawnEntrySource string

const (
	SpawnSourceBuiltIn SpawnEntrySource = "built-in"
	SpawnSourceEmerged SpawnEntrySource = "emerged"
	SpawnSourceManual  SpawnEntrySource = "manual"
)

// SpawnEntryState is the lifecycle state.
type SpawnEntryState string

const (
	SpawnStateProposed  SpawnEntryState = "proposed"
	SpawnStatePinned    SpawnEntryState = "pinned"
	SpawnStateDismissed SpawnEntryState = "dismissed"
)

// SpawnEntry is one item in the spawn dropdown.
type SpawnEntry struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Type     SpawnEntryType   `json:"type"`
	Source   SpawnEntrySource `json:"source"`
	State    SpawnEntryState  `json:"state"`

	// Type=skill fields
	SkillRef string `json:"skill_ref,omitempty"`

	// Type=command fields
	Command string `json:"command,omitempty"`

	// Type=agent fields
	Prompt string `json:"prompt,omitempty"`
	Target string `json:"target,omitempty"`

	// Lifecycle
	UseCount int        `json:"use_count"`
	LastUsed *time.Time `json:"last_used,omitempty"`
}

// EmergenceMetadata tracks emergence-internal data for a skill.
type EmergenceMetadata struct {
	SkillName     string    `json:"skill_name"`
	Confidence    float64   `json:"confidence"`
	EvidenceCount int       `json:"evidence_count"`
	Evidence      []string  `json:"evidence,omitempty"`
	EmergedAt     time.Time `json:"emerged_at"`
	LastCurated   time.Time `json:"last_curated"`
}

// SkillProposal is a proposed or updated skill from the curator.
type SkillProposal struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Triggers        []string `json:"triggers"`
	Procedure       string   `json:"procedure"`
	QualityCriteria string   `json:"quality_criteria"`
	Evidence        []string `json:"evidence"`
	Confidence      float64  `json:"confidence"`
	IsUpdate        bool     `json:"is_update"`
	Changes         string   `json:"changes,omitempty"`
}

// --- API request/response types ---

type SpawnEntriesResponse struct {
	Entries []SpawnEntry `json:"entries"`
}

type CreateSpawnEntryRequest struct {
	Name    string         `json:"name"`
	Type    SpawnEntryType `json:"type"`
	Command string         `json:"command,omitempty"`
	Prompt  string         `json:"prompt,omitempty"`
	Target  string         `json:"target,omitempty"`
}

type UpdateSpawnEntryRequest struct {
	Name    *string `json:"name,omitempty"`
	Command *string `json:"command,omitempty"`
	Prompt  *string `json:"prompt,omitempty"`
	Target  *string `json:"target,omitempty"`
}
```

**Step 2: Write a basic test to verify types compile and JSON round-trip**

```go
package contracts

import (
	"encoding/json"
	"testing"
)

func TestSpawnEntryJSON(t *testing.T) {
	entry := SpawnEntry{
		ID:     "test-1",
		Name:   "Test entry",
		Type:   SpawnEntrySkill,
		Source: SpawnSourceEmerged,
		State:  SpawnStatePinned,
		SkillRef: "code-review",
		UseCount: 5,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SpawnEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != entry.ID || decoded.SkillRef != entry.SkillRef {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
}
```

**Step 3: Run tests**

```bash
go test ./internal/api/contracts/ -run TestSpawnEntry
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/api/contracts/emergence.go internal/api/contracts/emergence_test.go
```

Message: `feat(emergence): add API contract types for spawn entries and skill proposals`

---

### Task 2: Spawn Entries Store

**Files:**

- Create: `internal/emergence/store.go`
- Test: `internal/emergence/store_test.go`

**Step 1: Write the failing test**

Test CRUD operations: Create, List (sorted by use_count desc), Get, Update, Delete, Pin, Dismiss, RecordUse. Model tests after `internal/actions/registry_test.go` — use `t.TempDir()` for isolation.

Key behaviors to test:

- `List` returns only pinned entries, sorted by `use_count` descending
- `ListAll` returns all entries regardless of state
- `Create` sets source=manual, state=pinned
- `Pin` changes state from proposed to pinned
- `Dismiss` changes state from proposed to dismissed
- `RecordUse` increments use_count and sets last_used
- `AddProposed` creates with state=proposed, source=emerged
- JSON file persistence (create store, add entries, create new store from same path, verify data loads)

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/emergence/ -run TestStore
```

Expected: FAIL (package doesn't exist yet)

**Step 3: Implement the store**

Create `internal/emergence/store.go`:

- `Store` struct: `baseDir string`, `mu sync.Mutex`, `entries map[string][]contracts.SpawnEntry` (keyed by repo name)
- File path: `<baseDir>/<repo>/spawn-entries.json`
- `NewStore(baseDir)` — creates dir if needed
- `load(repo)` — lazy-load from JSON file
- `save(repo)` — atomic write (temp+rename), same pattern as `lore.ProposalStore`
- `List(repo)` — returns pinned entries sorted by use_count desc
- `ListAll(repo)` — returns all entries
- `Get(repo, id)` — by ID
- `Create(repo, req CreateSpawnEntryRequest)` — generates ID, sets manual/pinned
- `Update(repo, id, req UpdateSpawnEntryRequest)` — partial update
- `Delete(repo, id)` — removes entry
- `Pin(repo, id)` — sets state=pinned
- `Dismiss(repo, id)` — sets state=dismissed
- `RecordUse(repo, id)` — increments use_count, sets last_used
- `AddProposed(repo, entries []SpawnEntry)` — bulk add with state=proposed

ID generation: use `crypto/rand` hex (8 bytes → 16 hex chars), same pattern as existing codebase.

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/emergence/ -run TestStore
```

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): add spawn entries store with CRUD and lifecycle`

---

### Task 3: Emergence Metadata Store

**Files:**

- Create: `internal/emergence/metadata.go`
- Test: `internal/emergence/metadata_test.go`

**Step 1: Write the failing test**

Test: Save metadata, Load metadata, list all for a repo. The metadata store tracks per-skill emergence data (confidence, evidence count, evidence text, timestamps). File path: `~/.schmux/emergence/<repo>/metadata.json`.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/emergence/ -run TestMetadata
```

Expected: FAIL

**Step 3: Implement metadata store**

Simple JSON-backed store keyed by skill name. Much simpler than the spawn store — just `map[string]EmergenceMetadata` per repo, with load/save.

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): add emergence metadata store for skill lifecycle tracking`

---

## Phase 2: Adapter Extensions

### Task 4: Add InjectSkill/RemoveSkill to Adapter Interface

**Files:**

- Modify: `internal/detect/adapter.go`
- Modify: all adapter implementations (to add no-op stubs for compile)

**Step 1: Define the SkillModule type and extend the interface**

Add to `adapter.go`:

```go
// SkillModule is the data needed to inject a skill into an agent's native format.
type SkillModule struct {
	Name        string // skill name (used for directory/file naming)
	Content     string // full markdown content (frontmatter + body)
}

// Add to ToolAdapter interface:
// InjectSkill writes a skill into the agent's native skill location in the workspace.
InjectSkill(workspacePath string, skill SkillModule) error
// RemoveSkill removes a previously injected skill from the workspace.
RemoveSkill(workspacePath string, skillName string) error
```

**Step 2: Add no-op implementations to all adapters**

Every adapter file (`adapter_claude.go`, `adapter_opencode.go`, `adapter_codex.go`, `adapter_gemini.go`, `adapter_aider.go`, `adapter_goose.go`, and any others) needs stub methods returning nil. Find all files implementing `ToolAdapter` with:

```bash
grep -l 'func.*ToolAdapter' internal/detect/adapter_*.go
```

Or more precisely, find all types registered via `registerAdapter`:

```bash
grep 'registerAdapter' internal/detect/adapter_*.go
```

**Step 3: Verify compile**

```bash
go build ./internal/detect/
```

Expected: compiles without error

**Step 4: Commit**

Message: `feat(detect): add InjectSkill/RemoveSkill to adapter interface`

---

### Task 5: Claude Code Adapter — InjectSkill/RemoveSkill

**Files:**

- Modify: `internal/detect/adapter_claude.go`
- Create: `internal/detect/adapter_claude_skills.go` (or add to existing file)
- Test: `internal/detect/adapter_claude_skills_test.go`

**Step 1: Write the failing test**

```go
func TestClaudeInjectSkill(t *testing.T) {
	dir := t.TempDir()
	adapter := &ClaudeAdapter{}
	skill := SkillModule{
		Name:    "code-review",
		Content: "---\nname: code-review\n---\n\n## Procedure\n1. Read the PR\n",
	}
	if err := adapter.InjectSkill(dir, skill); err != nil {
		t.Fatal(err)
	}
	// Verify file exists at .claude/skills/schmux-code-review/SKILL.md
	path := filepath.Join(dir, ".claude", "skills", "schmux-code-review", "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skill file not created: %v", err)
	}
	if string(content) != skill.Content {
		t.Errorf("content mismatch: got %q", string(content))
	}
}

func TestClaudeRemoveSkill(t *testing.T) {
	dir := t.TempDir()
	adapter := &ClaudeAdapter{}
	skill := SkillModule{Name: "code-review", Content: "test"}
	adapter.InjectSkill(dir, skill)
	if err := adapter.RemoveSkill(dir, "code-review"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "skills", "schmux-code-review", "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("skill file should be removed")
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/detect/ -run TestClaude.*Skill
```

**Step 3: Implement**

- `InjectSkill`: create `.claude/skills/schmux-<name>/SKILL.md`, write content
- `RemoveSkill`: remove `.claude/skills/schmux-<name>/` directory
- Prefix with `schmux-` to namespace and avoid collisions with user skills

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(detect): implement InjectSkill/RemoveSkill for Claude Code adapter`

---

### Task 6: OpenCode Adapter — InjectSkill/RemoveSkill

**Files:**

- Modify: `internal/detect/adapter_opencode_commands.go`
- Test: `internal/detect/adapter_opencode_commands_test.go` (extend existing)

**Step 1: Write the failing test**

Similar to Claude test but writes to `.opencode/commands/schmux-<name>.md`.

**Step 2: Run to verify failure**

**Step 3: Implement**

- `InjectSkill`: write `.opencode/commands/schmux-<name>.md`
- `RemoveSkill`: delete the file

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(detect): implement InjectSkill/RemoveSkill for opencode adapter`

---

## Phase 3: Git Exclude Patterns

### Task 7: Update Gitignore for Skill Paths

**Files:**

- Modify: `internal/workspace/ensure/manager.go` (update `excludePatterns`)
- Test: `internal/workspace/ensure/manager_test.go` (extend existing)

**Step 1: Write the failing test**

Test that `GitExclude` writes patterns covering `.claude/skills/schmux-*/` and `.opencode/commands/schmux-*.md`.

**Step 2: Run to verify failure**

**Step 3: Update `excludePatterns`**

Change the `excludePatterns` slice in `manager.go` (currently at line 420):

```go
var excludePatterns = []string{
	".schmux/hooks/",
	".schmux/events/",
	".opencode/plugins/schmux.ts",
	".opencode/commands/schmux-*.md",   // emerged + built-in skills
	".opencode/commands/commit.md",     // legacy built-in
	".claude/skills/schmux-*/",          // emerged + built-in skills
}
```

Note: `.opencode/commands/commit.md` stays for now (legacy). When the commit skill migrates to `schmux-commit.md`, we can remove the old pattern.

**Step 4: Run tests**

```bash
go test ./internal/workspace/ensure/ -run TestGitExclude
```

Expected: PASS

**Step 5: Commit**

Message: `feat(ensure): add gitignore patterns for emerged and built-in skill files`

---

## Phase 4: Built-in Skills

### Task 8: Embed Built-in Skill Content

**Files:**

- Create: `internal/emergence/builtins/commit.md` (skill content for /commit)
- Create: `internal/emergence/builtins.go` (embed + registry)
- Test: `internal/emergence/builtins_test.go`

**Step 1: Write the commit skill content**

Convert the existing `opencodeCommitCommand` constant from `internal/detect/adapter_opencode_commands.go` into a standalone markdown file that works as a Claude Code SKILL.md and as an opencode command. Add frontmatter:

```markdown
---
name: commit
description: Create a git commit with definition-of-done enforcement
source: built-in
---

## How This Command Works

...
```

The body is the existing `/commit` workflow content (the 5-step definition-of-done from `adapter_opencode_commands.go` lines 11-106).

**Step 2: Embed and register**

```go
package emergence

import "embed"

//go:embed builtins/*.md
var builtinFS embed.FS

// BuiltinSkill describes an embedded skill.
type BuiltinSkill struct {
	Name    string
	Content string
}

// ListBuiltins returns all embedded built-in skills.
func ListBuiltins() ([]BuiltinSkill, error) {
	entries, err := builtinFS.ReadDir("builtins")
	// ... read each .md file, extract name from filename
}
```

**Step 3: Write test**

```go
func TestListBuiltins(t *testing.T) {
	skills, err := ListBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) == 0 {
		t.Fatal("expected at least one built-in skill")
	}
	// Verify commit skill exists and has content
	found := false
	for _, s := range skills {
		if s.Name == "commit" {
			found = true
			if len(s.Content) < 100 {
				t.Error("commit skill content too short")
			}
		}
	}
	if !found {
		t.Error("commit skill not found in builtins")
	}
}
```

**Step 4: Run tests**

```bash
go test ./internal/emergence/ -run TestListBuiltins
```

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): embed built-in commit skill`

---

### Task 9: Inject Built-in Skills at Workspace Setup

**Files:**

- Modify: `internal/workspace/ensure/manager.go` (add skill injection to `ensureWorkspace`)
- Test: `internal/workspace/ensure/manager_test.go`

**Step 1: Write the failing test**

Test that `ForSpawn` (or `ensureWorkspace`) writes built-in skill files into the workspace via the adapter's `InjectSkill`. Need to check that `.claude/skills/schmux-commit/SKILL.md` exists after workspace setup for a Claude Code target.

**Step 2: Run to verify failure**

**Step 3: Implement**

In `ensureWorkspace`, after calling `adapter.SetupCommands(workspacePath)`:

1. Call `emergence.ListBuiltins()` to get built-in skills
2. Check config for disabled skills (skip those)
3. For each enabled built-in, call `adapter.InjectSkill(workspacePath, skill)`

This requires the ensure package to have access to the built-in skills list and config. Pass via package-level setter (same pattern as `SetInstructionStore`).

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(ensure): inject built-in skills at workspace setup`

---

### Task 10: Config for Disabling Built-in Skills

**Files:**

- Modify: `internal/config/config.go` (add `built_in_skills` field)
- Test: add test case to `internal/config/config_test.go`

**Step 1: Write the failing test**

Test that config can be loaded with `built_in_skills` map, and that `IsBuiltinEnabled(name)` returns the right value (default true if not in map).

**Step 2: Run to verify failure**

**Step 3: Implement**

Add to the config struct:

```go
BuiltInSkills map[string]bool `json:"built_in_skills,omitempty"`
```

Add method:

```go
func (c *Config) IsBuiltinEnabled(name string) bool {
	if c.BuiltInSkills == nil {
		return true // default: all enabled
	}
	enabled, exists := c.BuiltInSkills[name]
	if !exists {
		return true // default: enabled if not explicitly listed
	}
	return enabled
}
```

Note: `internal/config/config.go` is very large. Use `Grep` to find the config struct and add the field. Do NOT read the full file.

**Step 4: Run tests**

```bash
go test ./internal/config/ -run TestBuiltin
```

Expected: PASS

**Step 5: Commit**

Message: `feat(config): add built_in_skills toggle for disabling shipped skills`

---

## Phase 5: Emergence Curator

### Task 11: Curator Prompt Builder

**Files:**

- Create: `internal/emergence/curator.go`
- Test: `internal/emergence/curator_test.go`

**Step 1: Write the failing test**

Test `BuildEmergencePrompt(signals []IntentSignal, existingSkills []BuiltinSkill, repoName string)` returns a non-empty string containing key sections: the system instructions, the signals list, and the existing skills list.

Reuse `IntentSignal` from `internal/actions/signals.go` (or copy the type into emergence package — decide based on whether the actions package is being kept temporarily).

**Step 2: Run to verify failure**

**Step 3: Implement**

The prompt instructs the LLM to:

1. Cluster semantically similar intent signals
2. Distill each cluster into a skill (procedure, quality criteria, parameters, triggers)
3. For existing skills, propose updates only if meaningfully different
4. Discard one-off signals that don't form patterns
5. Require minimum 3 signals per cluster, from 2+ sessions, 2+ days

Output schema matches `SkillProposal` type from contracts.

Register schema with `internal/schema` package (same pattern as lore curator: `schema.Register(schema.LabelEmergenceCurator, EmergenceCuratorResponse{})`). Add the new label to `internal/schema/`.

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): add curator prompt builder for skill distillation`

---

### Task 12: Curator Response Parser

**Files:**

- Modify: `internal/emergence/curator.go` (add parser)
- Test: `internal/emergence/curator_test.go` (extend)

**Step 1: Write the failing test**

Test `ParseEmergenceResponse(response string)` with valid JSON, JSON in code fences, and malformed input. Should return `(*EmergenceCuratorResponse, error)`.

```go
type EmergenceCuratorResponse struct {
	NewSkills       []SkillProposal      `json:"new_skills"`
	UpdatedSkills   []SkillProposal      `json:"updated_skills"`
	DiscardedSignals map[string]string    `json:"discarded_signals"`
}
```

**Step 2: Run to verify failure**

**Step 3: Implement**

Reuse the same JSON parsing pattern from `lore.ParseExtractionResponse` — try direct unmarshal, fall back to stripping code fences.

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): add curator response parser`

---

### Task 13: Skill File Generator

**Files:**

- Create: `internal/emergence/skillfile.go`
- Test: `internal/emergence/skillfile_test.go`

**Step 1: Write the failing test**

Test `GenerateSkillFile(proposal SkillProposal)` returns valid markdown with YAML frontmatter containing name, description, triggers, source=emerged. Body contains procedure and quality criteria sections.

**Step 2: Run to verify failure**

**Step 3: Implement**

```go
func GenerateSkillFile(proposal contracts.SkillProposal) string {
	var sb strings.Builder
	// Write YAML frontmatter
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s\n", proposal.Name)
	fmt.Fprintf(&sb, "description: %s\n", proposal.Description)
	sb.WriteString("source: emerged\n")
	if len(proposal.Triggers) > 0 {
		sb.WriteString("triggers:\n")
		for _, t := range proposal.Triggers {
			fmt.Fprintf(&sb, "  - %q\n", t)
		}
	}
	sb.WriteString("---\n\n")
	// Write procedure
	if proposal.Procedure != "" {
		sb.WriteString("## Procedure\n\n")
		sb.WriteString(proposal.Procedure)
		sb.WriteString("\n\n")
	}
	// Write quality criteria
	if proposal.QualityCriteria != "" {
		sb.WriteString("## Quality Criteria\n\n")
		sb.WriteString(proposal.QualityCriteria)
		sb.WriteString("\n")
	}
	return sb.String()
}
```

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): add skill file generator from curator proposals`

---

## Phase 6: Signal Collection (Reuse + Adapt)

### Task 14: Move Signal Collection to Emergence Package

**Files:**

- Create: `internal/emergence/signals.go`
- Create: `internal/emergence/signals_test.go`

**Step 1: Write the failing test**

Test `CollectIntentSignals(workspacePaths)` — same behavior as `actions.CollectIntentSignals`. Copy or adapt the existing tests from `internal/actions/signals_test.go`.

**Step 2: Run to verify failure**

**Step 3: Implement**

Copy `internal/actions/signals.go` into `internal/emergence/signals.go`. Update the package name. This is intentional duplication — we'll delete the `actions` package later. Keep `IntentSignal` type in the emergence package.

Alternatively, if the old `actions` package will coexist for a while, import and re-export. But clean copy is simpler for a full replacement.

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): add intent signal collection`

---

## Phase 7: API Handlers

### Task 15: Spawn Entries API Handlers

**Files:**

- Create: `internal/dashboard/handlers_emergence.go`
- Test: via `./test.sh --quick` (integration)

**Step 1: Write the handlers**

```
GET    /api/emergence/{repo}/entries          → list pinned spawn entries (sorted by use_count)
GET    /api/emergence/{repo}/entries/all       → list all entries (for lore page)
POST   /api/emergence/{repo}/entries          → create manual entry
PUT    /api/emergence/{repo}/entries/{id}     → update entry
DELETE /api/emergence/{repo}/entries/{id}     → delete entry
POST   /api/emergence/{repo}/entries/{id}/pin → pin proposed entry
POST   /api/emergence/{repo}/entries/{id}/dismiss → dismiss proposed entry
POST   /api/emergence/{repo}/entries/{id}/use → record usage
```

Follow existing handler patterns from `handlers_actions.go`. Use `s.emergenceStore` (will be wired in Phase 8).

**Step 2: Add route registration**

Add to `server.go` route registration, alongside existing lore routes.

**Step 3: Add `emergenceStore` to Server struct**

Add field and setter to `internal/dashboard/server.go`:

```go
emergenceStore *emergence.Store
```

```go
func (s *Server) SetEmergenceStore(store *emergence.Store) {
	s.emergenceStore = store
}
```

**Step 4: Run tests**

```bash
./test.sh --quick
```

Expected: PASS (compile check — full API testing comes with frontend)

**Step 5: Commit**

Message: `feat(dashboard): add emergence spawn entries API handlers`

---

### Task 16: Emergence Curation API Handler

**Files:**

- Modify: `internal/dashboard/handlers_emergence.go` (add curation endpoint)

**Step 1: Write the handler**

```
POST /api/emergence/{repo}/curate → trigger emergence curation
```

This handler:

1. Collects intent signals from workspace event files (reuse `emergence.CollectIntentSignals`)
2. Reads existing pinned skills (from spawn entries store + skill files)
3. Builds the emergence curator prompt
4. Calls the LLM executor (reuse lore's streaming executor)
5. Parses response into skill proposals
6. Generates skill files for each proposal
7. Adds proposed spawn entries to the store
8. Saves emergence metadata

Follow the same pattern as `handleLoreCurate` in `handlers_lore.go` — return 202, run in background with WebSocket progress streaming.

**Step 2: Run tests**

```bash
./test.sh --quick
```

Expected: PASS (compile check)

**Step 3: Commit**

Message: `feat(dashboard): add emergence curation trigger endpoint`

---

### Task 17: Skill Injection on Pin

**Files:**

- Modify: `internal/dashboard/handlers_emergence.go` (update pin handler)

**Step 1: Implement**

When a user pins a proposed entry (POST `.../pin`):

1. Update state to pinned in spawn entries store
2. Read the skill file content from emergence metadata/staging
3. For each active workspace of the repo, call `adapter.InjectSkill()` to write the skill into the workspace

This requires the handlers to access the workspace list (from state) and the adapter registry.

**Step 2: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 3: Commit**

Message: `feat(dashboard): inject skills into workspaces on pin`

---

## Phase 8: TypeScript Types and Frontend

### Task 18: Generate TypeScript Types

**Step 1: Run type generation**

```bash
go run ./cmd/gen-types
```

Verify that `assets/dashboard/src/lib/types.generated.ts` now includes `SpawnEntry`, `SpawnEntryType`, `SpawnEntrySource`, `SpawnEntryState`, and related types.

**Step 2: Commit**

Message: `chore: regenerate TypeScript types for emergence contracts`

---

### Task 19: Emergence API Client Functions

**Files:**

- Create: `assets/dashboard/src/lib/emergence-api.ts`

**Step 1: Implement API functions**

```typescript
export async function getSpawnEntries(repo: string): Promise<SpawnEntry[]>;
export async function getAllSpawnEntries(repo: string): Promise<SpawnEntry[]>;
export async function createSpawnEntry(
  repo: string,
  req: CreateSpawnEntryRequest
): Promise<SpawnEntry>;
export async function updateSpawnEntry(
  repo: string,
  id: string,
  req: UpdateSpawnEntryRequest
): Promise<void>;
export async function deleteSpawnEntry(repo: string, id: string): Promise<void>;
export async function pinSpawnEntry(repo: string, id: string): Promise<void>;
export async function dismissSpawnEntry(repo: string, id: string): Promise<void>;
export async function recordSpawnEntryUse(repo: string, id: string): Promise<void>;
export async function triggerEmergenceCuration(repo: string): Promise<void>;
```

Follow existing API function patterns from the codebase (check how `getActions`, `pinAction` etc. are structured in the existing API client files).

**Step 2: Commit**

Message: `feat(dashboard): add emergence API client functions`

---

### Task 20: Spawn Dropdown Component — Replace ActionDropdown

**Files:**

- Modify: `assets/dashboard/src/components/ActionDropdown.tsx` (rewrite)
- Modify: `assets/dashboard/src/components/ActionDropdown.module.css`

**Step 1: Rewrite the dropdown**

Replace the current ActionDropdown with the new design:

- Fetch pinned spawn entries via `getSpawnEntries(repo)`
- Sort by `use_count` descending (API already returns sorted)
- Show provenance markers: ■ (built-in), ◉ (emerged), ○ (manual)
- "Spawn a session..." at top opens spawn page
- "+ Create action" at bottom
- Click handler: for type=skill or type=agent → spawn session; for type=command → spawn shell
- On successful spawn, call `recordSpawnEntryUse` to update usage stats

**Step 2: Run tests**

```bash
./test.sh --quick
```

Check for existing ActionDropdown tests and update them.

**Step 3: Commit**

Message: `feat(dashboard): replace ActionDropdown with emergence-backed spawn dropdown`

---

### Task 21: Lore Page — Update Actions Tab for Skill Proposals

**Files:**

- Modify: `assets/dashboard/src/routes/LorePage.tsx`
- Modify: `assets/dashboard/src/components/ProposedActionCard.tsx` (rename/rewrite)

**Step 1: Update the Actions tab**

Replace the current proposed/pinned action lists with:

- Proposed skill cards showing: name, confidence %, evidence prompts, procedure preview, quality criteria, Pin/Dismiss/Edit buttons
- Update available cards showing: name, changes summary, View diff/Accept/Keep current buttons
- Remove confidence dots from pinned items

Use `getAllSpawnEntries(repo)` to get both proposed and pinned entries.

**Step 2: Update ProposedActionCard**

Rewrite to show the richer skill proposal data: procedure steps, quality criteria, evidence prompts.

**Step 3: Run tests**

```bash
./test.sh --quick
```

Update existing LorePage tests for the new component structure.

**Step 4: Commit**

Message: `feat(dashboard): update Lore page actions tab for skill proposals`

---

### Task 22: Manual Action Creation Form

**Files:**

- Modify or create component for the "+ Create action" flow

**Step 1: Implement the creation form**

Two options: Shell command or Agent session.

- Name (required)
- Type radio: Shell command / Agent session
- Shell: command text input
- Agent: prompt textarea + target dropdown (optional)
- Save button calls `createSpawnEntry`

**Step 2: Run tests**

```bash
./test.sh --quick
```

**Step 3: Commit**

Message: `feat(dashboard): add manual action creation form`

---

## Phase 9: Daemon Wiring

### Task 23: Initialize Emergence System in Daemon

**Files:**

- Modify: `internal/daemon/daemon.go`

**Step 1: Add emergence store initialization**

In the daemon startup sequence (around line 875 where actions are currently initialized):

1. Create emergence store: `emergence.NewStore(emergenceBaseDir)` where `emergenceBaseDir = ~/.schmux/emergence`
2. Wire to dashboard server: `server.SetEmergenceStore(store)`
3. Wire emergence metadata store similarly

**Step 2: Add emergence curation to session dispose callback**

Extend or replace the existing `sm.SetLoreCallback` to also trigger emergence curation when thresholds are met. The callback should:

1. Count new intent signals since last curation
2. If above threshold → trigger emergence curation (same debounce pattern as lore)

**Step 3: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 4: Commit**

Message: `feat(daemon): wire emergence system initialization and auto-curation`

---

### Task 24: Inject Pinned Skills at Spawn Time

**Files:**

- Modify: `internal/workspace/ensure/manager.go`

**Step 1: Extend ForSpawn to inject pinned emerged skills**

After built-in skills are injected (Task 9), also inject pinned emerged skills:

1. Read pinned skill entries from emergence store for the repo
2. Read skill file content from emergence metadata store
3. Call `adapter.InjectSkill()` for each

This requires the ensure package to have access to the emergence store. Add a package-level setter similar to `SetInstructionStore`.

**Step 2: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 3: Commit**

Message: `feat(ensure): inject pinned emerged skills at spawn time`

---

## Phase 10: Migration and Cleanup

### Task 25: Migrate Existing Action Registry

**Files:**

- Create: `internal/emergence/migrate.go`
- Test: `internal/emergence/migrate_test.go`

**Step 1: Write the failing test**

Test `MigrateFromActions(actionsDir, emergenceStore)`:

- Reads existing `~/.schmux/actions/<repo>/registry.json`
- Converts pinned actions to spawn entries (type=agent for agent actions, type=command for commands)
- Writes spawn entries to emergence store
- Skips proposed/dismissed actions

**Step 2: Run to verify failure**

**Step 3: Implement**

Read the old registry format, map fields:

- `Action.Type` → `SpawnEntry.Type`
- `Action.Template` → `SpawnEntry.Prompt` (for agent type)
- `Action.Command` → `SpawnEntry.Command` (for command type)
- `Action.Source` "migrated"/"manual" → `SpawnEntry.Source` "manual"
- `Action.Source` "emerged" → `SpawnEntry.Source` "emerged"
- Preserve `use_count`, `last_used`

**Step 4: Run tests**

Expected: PASS

**Step 5: Commit**

Message: `feat(emergence): add migration from old actions registry`

---

### Task 26: Run Migration at Daemon Startup

**Files:**

- Modify: `internal/daemon/daemon.go`

**Step 1: Add one-time migration check**

At daemon startup, after emergence store is initialized:

1. Check if `~/.schmux/actions/` exists and has registries
2. If yes, run `MigrateFromActions` for each repo
3. After successful migration, rename `~/.schmux/actions/` to `~/.schmux/actions.migrated/` (preserves data but prevents re-migration)

**Step 2: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 3: Commit**

Message: `feat(daemon): run one-time migration from actions to emergence at startup`

---

### Task 27: Remove Old Actions Package

**Files:**

- Delete: `internal/actions/` (entire directory)
- Delete: `internal/api/contracts/actions.go`
- Modify: `internal/dashboard/handlers_actions.go` → delete
- Modify: `internal/dashboard/server.go` (remove action registry fields and getters)
- Modify: `internal/dashboard/handlers_spawn.go` (update spawn to use emergence store for usage tracking)
- Modify: `internal/daemon/daemon.go` (remove old action initialization)
- Modify: routes in `server.go` (remove old `/api/actions/` routes)

**Step 1: Remove old code**

Delete files and remove all imports of `internal/actions` package. Update `handlers_spawn.go` to call `emergenceStore.RecordUse()` instead of `registry.RecordUse()`.

**Step 2: Verify compile**

```bash
go build ./cmd/schmux
```

**Step 3: Run full test suite**

```bash
./test.sh --quick
```

Expected: PASS (some tests may need updating if they referenced action types)

**Step 4: Commit**

Message: `refactor: remove old actions package, replaced by emergence system`

---

### Task 28: Update API Documentation

**Files:**

- Modify: `docs/api.md`

**Step 1: Document new endpoints**

Replace the `/api/actions/` section with `/api/emergence/` endpoints:

- `GET /api/emergence/{repo}/entries` — list pinned spawn entries
- `GET /api/emergence/{repo}/entries/all` — list all entries
- `POST /api/emergence/{repo}/entries` — create manual entry
- `PUT /api/emergence/{repo}/entries/{id}` — update entry
- `DELETE /api/emergence/{repo}/entries/{id}` — delete entry
- `POST /api/emergence/{repo}/entries/{id}/pin` — pin proposed entry
- `POST /api/emergence/{repo}/entries/{id}/dismiss` — dismiss proposed entry
- `POST /api/emergence/{repo}/entries/{id}/use` — record usage
- `POST /api/emergence/{repo}/curate` — trigger emergence curation

**Step 2: Commit**

Message: `docs: update API documentation for emergence system`

---

## Phase 11: Spawn Page Integration

### Task 29: Update Spawn Handler for Emergence

**Files:**

- Modify: `internal/dashboard/handlers_spawn.go`

**Step 1: Update spawn to work with emergence**

After successful spawn:

1. If `req.ActionID` is set (from dropdown click), call `emergenceStore.RecordUse(repo, id)`
2. If prompt matches a pinned spawn entry template, call `RecordUse` with match
3. Remove old action registry references

**Step 2: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 3: Commit**

Message: `feat(spawn): integrate spawn handler with emergence store for usage tracking`

---

### Task 30: Prompt Autocomplete from Emergence

**Files:**

- Modify: `internal/dashboard/handlers_emergence.go` (add prompt-history endpoint)

**Step 1: Add endpoint**

```
GET /api/emergence/{repo}/prompt-history → prompt autocomplete data
```

Returns two sections:

1. Pinned skill entries with triggers and use counts
2. Raw prompt history from event files (reuse existing `CollectPromptHistory` logic)

**Step 2: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 3: Commit**

Message: `feat(dashboard): add prompt history endpoint for spawn autocomplete`

---

## Notes for the implementer

- **DO NOT run `npm install` or `npm run build` directly.** Use `go run ./cmd/build-dashboard` for dashboard builds.
- **DO NOT edit `types.generated.ts` directly.** Edit Go contracts, then `go run ./cmd/gen-types`.
- **Run `./test.sh --quick` frequently** — it covers both Go and frontend tests.
- **Run `./format.sh` before staging** — the pre-commit hook handles this but running it early catches issues.
- **Large files warning:** `internal/config/config.go`, `internal/config/config_test.go`, and `assets/dashboard/src/styles/global.css` exceed read limits. Use `Grep` to find specific symbols.
- **Commit after every task** using the `/commit` command. Do not batch multiple tasks into one commit.
- The `internal/actions/` package continues to exist until Task 27. During the transition (Tasks 1-26), both systems coexist. No functionality is removed until the new system is fully wired.
