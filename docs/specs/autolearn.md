# Autolearn — Unified Learning System

## Status

Spec — not yet implemented.

## Problem

Schmux has two independent systems that follow the same loop (observe → curate → triage → apply) but are architecturally separate:

- **Lore**: observes agent friction (failures, errors, reflections), curates rules, merges into instruction files.
- **Emergence**: observes user intent (prompts, spawn patterns), distills skills, injects into agent skill systems.

The triage experience is already merged in the UI (both appear on the lore page), but the backend has duplicate stores, duplicate handlers, duplicate daemon wiring, and two separate packages. The conceptual split forces users to understand two systems that feel like one.

## Solution

Replace both with a single **autolearn** system. One package, one store, one API surface, one UI page, one mental model.

A **Learning** is the atomic unit. It has a **Kind** (rule or skill) that determines where it ends up, but the triage experience is kind-agnostic — a card is a card.

## Mental model

```
Signals accumulate (friction + intent from agent sessions)
        ↓
Curation run (two LLM calls, unified output)
  ├── friction curator → []Learning (mostly rules, can produce skills)
  └── intent curator   → []Learning (mostly skills, can produce rules)
        ↓
Merge + dedup into single Batch
        ↓
User triages (approve / dismiss / edit)
  - destination (layer × kind) visible on every card
  - dismissals are durable, browsable, reversible
        ↓
System applies (routed by kind × layer)
```

## Data model

### Learning

The atomic unit of knowledge the system produces.

```go
type LearningKind string
const (
    KindRule  LearningKind = "rule"
    KindSkill LearningKind = "skill"
)

type LearningStatus string
const (
    StatusPending   LearningStatus = "pending"
    StatusApproved  LearningStatus = "approved"
    StatusDismissed LearningStatus = "dismissed"
)

type Layer string
const (
    LayerRepoPublic       Layer = "repo_public"
    LayerRepoPrivate      Layer = "repo_private"
    LayerCrossRepoPrivate Layer = "cross_repo_private"
)

type Learning struct {
    ID          string         `json:"id"`
    Kind        LearningKind   `json:"kind"`
    Status      LearningStatus `json:"status"`
    Title       string         `json:"title"`
    Description string         `json:"description,omitempty"`
    Category    string         `json:"category,omitempty"`
    Sources     []SourceRef    `json:"sources,omitempty"`
    CreatedAt   time.Time      `json:"created_at"`

    SuggestedLayer Layer  `json:"suggested_layer"`
    ChosenLayer    *Layer `json:"chosen_layer,omitempty"`

    Rule  *RuleDetails  `json:"rule,omitempty"`   // present when Kind="rule"
    Skill *SkillDetails `json:"skill,omitempty"`  // present when Kind="skill"
}

func (l *Learning) EffectiveLayer() Layer {
    if l.ChosenLayer != nil {
        return *l.ChosenLayer
    }
    return l.SuggestedLayer
}
```

### RuleDetails

Kind-specific data for rules.

```go
type RuleDetails struct {
    MergedAt *time.Time `json:"merged_at,omitempty"`
}
```

### SkillDetails

Kind-specific data for skills.

```go
type SkillDetails struct {
    Triggers        []string `json:"triggers,omitempty"`
    Procedure       string   `json:"procedure,omitempty"`
    QualityCriteria string   `json:"quality_criteria,omitempty"`
    Confidence      float64  `json:"confidence,omitempty"`
    SkillContent    string   `json:"skill_content,omitempty"` // rendered SKILL.md
    IsUpdate        bool     `json:"is_update,omitempty"`     // true = refinement of existing skill
    Changes         string   `json:"changes,omitempty"`       // what changed (for updates)
}
```

### SourceRef

Unified reference to the signal that produced a learning.

```go
type SourceRef struct {
    Type         string `json:"type"`                    // "failure", "reflection", "friction", "intent"
    Text         string `json:"text,omitempty"`
    InputSummary string `json:"input_summary,omitempty"` // failures only
    ErrorSummary string `json:"error_summary,omitempty"` // failures only
    Tool         string `json:"tool,omitempty"`
}
```

### Batch

Groups learnings from a single curation run.

```go
type BatchStatus string
const (
    BatchPending   BatchStatus = "pending"
    BatchMerging   BatchStatus = "merging"   // public-layer merge in progress
    BatchApplied   BatchStatus = "applied"
    BatchDismissed BatchStatus = "dismissed"
)

type Batch struct {
    ID        string      `json:"id"`
    Repo      string      `json:"repo"`
    CreatedAt time.Time   `json:"created_at"`
    Status    BatchStatus `json:"status"`
    Learnings []Learning  `json:"learnings"`
    Discarded []string    `json:"discarded,omitempty"`

    // Audit fields (carried from curation run)
    SourceCount      int               `json:"source_count,omitempty"`
    EntriesUsed      []string          `json:"entries_used,omitempty"`
    EntriesDiscarded map[string]string `json:"entries_discarded,omitempty"`
}
```

#### Batch status transitions

```
pending → merging     when public-layer merge is triggered
pending → applied     when all learnings are approved and private-only
pending → dismissed   when the entire batch is dismissed
merging → pending     if merge is cancelled or fails
merging → applied     when merge is pushed
applied               terminal state
dismissed             terminal state (reversible: un-dismissing any learning reopens the batch)
```

A batch with a mix of approved and dismissed learnings transitions to `applied` once all approved learnings are applied and no learnings remain pending.

### PendingMerge

Tracks the async merge-then-push flow for **public-layer rules only**. Public skills do not go through the merge curator — they are standalone files that are written directly (see Apply matrix).

```go
type PendingMerge struct {
    Repo           string             `json:"repo"`
    Status         PendingMergeStatus `json:"status"`
    BaseSHA        string             `json:"base_sha"`
    LearningIDs    []string           `json:"learning_ids"`    // public rule learning IDs
    BatchIDs       []string           `json:"batch_ids"`
    MergedContent  string             `json:"merged_content"`  // merged instruction file
    CurrentContent string             `json:"current_content"`
    Summary        string             `json:"summary"`
    EditedContent  *string            `json:"edited_content,omitempty"`
    SkillFiles     map[string]string  `json:"skill_files,omitempty"`  // path → content for public skills
    Error          string             `json:"error,omitempty"`
    CreatedAt      time.Time          `json:"created_at"`
}
```

The `SkillFiles` field carries any public-layer skills that were approved alongside public rules. At push time, the handler writes both the merged instruction file and any skill files into the worktree before committing. This ensures a single atomic commit for all public-layer learnings.

### BatchStore

```go
type BatchStore struct { /* mutex, baseDir */ }

func NewBatchStore(baseDir string, logger *log.Logger) *BatchStore

func (s *BatchStore) Save(b *Batch) error
func (s *BatchStore) Get(repo, batchID string) (*Batch, error)
func (s *BatchStore) List(repo string) ([]*Batch, error)
func (s *BatchStore) UpdateStatus(repo, batchID string, status BatchStatus) error
func (s *BatchStore) UpdateLearning(repo, batchID, learningID string, update LearningUpdate) error

// GetLearning finds a learning by ID across all batches for a repo.
// Used by push validation to verify learning status.
func (s *BatchStore) GetLearning(repo, learningID string) (*Learning, *Batch, error)

// Query helpers for curator prompt construction
func (s *BatchStore) PendingLearningTitles(repo string) []string
func (s *BatchStore) DismissedLearningTitles(repo string) []string
func (s *BatchStore) DismissedLearnings(repo string) []Learning
```

## Curation pipeline

### Two LLM calls per run

Each curation run makes up to two LLM calls:

1. **Friction curator** — receives friction signals (failures, reflections, friction events) + all dismissed learnings. Produces `[]Learning` (mostly rules, can produce skills). Blind to existing instructions (same as current lore extraction).
2. **Intent curator** — receives intent signals (user prompts, spawn patterns) + all dismissed learnings. Produces `[]Learning` (mostly skills, can produce rules).

Either call is skipped if its signal type is empty.

### Cross-kind learnings

Both curators can produce either kind. The friction curator might notice a recurring pattern worth turning into a skill. The intent curator might notice a user repeatedly working around a footgun and suggest a guardrail rule.

### Deduplication

After both curators return, results are merged into a single batch with cross-curator dedup:

1. LLM prompt includes existing pending + dismissed learning titles (hint)
2. Post-extraction text comparison against pending and dismissed learnings (safety net)
3. Cross-curator dedup within the same run (friction and intent curators might independently propose the same thing)

### Dismissal memory

- All dismissed learnings (from any batch, any time) are fed into both curator prompts
- Dismissals are stored as `status: dismissed` on the Learning record
- Users can un-dismiss a learning via the history page, making it eligible for re-extraction
- The dismissed-learning feed into the prompt is dynamically constructed from current state

### Trigger

Single trigger fires on session dispose (if `autolearn.curate_on_dispose` is enabled). Debounced via timer (same pattern as current lore). Manual trigger via `POST /api/autolearn/{repo}/curate`.

## Apply matrix

| Kind  | Layer              | Target                                          | User action                    |
| ----- | ------------------ | ----------------------------------------------- | ------------------------------ |
| rule  | repo_public        | Git-tracked instruction file (e.g., CLAUDE.md)  | LLM merge → review diff → push |
| rule  | repo_private       | `~/.schmux/autolearn/instructions/{repo}.md`    | One-click                      |
| rule  | cross_repo_private | `~/.schmux/autolearn/instructions/global.md`    | One-click                      |
| skill | repo_public        | `.claude/skills/schmux-{name}/SKILL.md` in repo | Review content → push          |
| skill | repo_private       | Injected into workspaces, `.git/info/exclude`   | One-click (pin)                |
| skill | cross_repo_private | Injected into all repo workspaces               | One-click (pin)                |

### Public rules vs public skills: different push mechanics

**Public rules** go through the merge curator. Multiple approved rules are batched into a single LLM merge call that integrates them into the existing instruction file (e.g., CLAUDE.md). The user reviews the diff and pushes.

**Public skills** do NOT go through the merge curator. Each skill is a standalone SKILL.md file — there is no existing document to merge into. The user reviews the rendered skill content on the card, then pushes. The push handler writes the skill file directly to the worktree.

Both share the same push endpoint and commit atomically. If approved learnings include both public rules and public skills, the push creates one commit containing the merged instruction file and all skill files.

### Public layer: user must understand what leaves the machine

Every learning card shows its destination layer. Anything going to `repo_public` requires explicit push — the user sees the exact diff before anything is committed or pushed. No silent publishing.

### Private layers: immediate

Private rules are written to instruction files immediately on approval. Private skills are injected into workspaces immediately on approval.

## Spawn entry relationship

Spawn entries (the spawn dropdown registry) remain a **separate concern**. When a skill learning is approved and applied, it creates/updates a spawn entry as a side effect. Spawn entries have their own store and API — future consolidation with quick launch and pastebin is out of scope.

### Package for spawn entries

The spawn entry store (`Store`) and metadata store (`MetadataStore`) move from `internal/emergence/` to `internal/spawn/`. This package has no build tag — spawn entries are always available regardless of whether autolearn is compiled in. The spawn entry CRUD handlers move from `handlers_emergence.go` to `handlers_spawn.go` (no build tag).

The autolearn handlers import `internal/spawn/` to create entries as a side effect of skill application. The `internal/spawn/` package does not import `internal/autolearn/`.

## Learning history

The system maintains a browsable, mutable history of all learnings:

- All batches (pending, applied, dismissed) are queryable
- Learnings can be filtered by kind, status, layer, repo
- Users can un-dismiss learnings (re-eligible for curation)
- Users can "forget" an applied learning (removes from instructions/skills, marks as dismissed)
- The history drives the dismissal feed-back into curator prompts

### Forget and un-dismiss operations

**Un-dismiss** (`PATCH .../learnings/{lid}` with `status: pending`): changes a dismissed learning back to pending. The learning becomes eligible for re-extraction in future curation runs. If this was the last dismissed learning in a dismissed batch, the batch reopens to `pending`.

**Forget** (`POST /api/autolearn/{repo}/forget/{lid}`): reverses an applied learning. For rules: removes the rule text from the instruction file (private layers) or marks it for removal on next merge (public layer). For skills: calls `RemoveSkill` on all workspace adapters and deletes the spawn entry side-effect. The learning status changes to `dismissed` so it won't resurface.

## Experimental feature integration

### Build tags

```
//go:build !noautolearn    ← real implementation
//go:build noautolearn     ← disabled.go stubs
```

The existing `nolore` build tag is removed (old lore package is deleted).

### Build tag scope

The `noautolearn` tag gates the learning pipeline: curation, batches, merge, push, history, and the autolearn config panel. It does **not** gate spawn entries — those are always compiled in (see "Spawn entry relationship" above). This matches the current behavior where emergence routes have no build tag.

### Features contract

```go
type Features struct {
    // ... existing fields ...
    Autolearn bool `json:"autolearn"`   // replaces Lore
    // Lore field removed
}
```

### Experimental registry (frontend)

```ts
{
  id: 'autolearn',
  name: 'Autolearn',
  description: 'Learns from agent friction and usage patterns — proposes rules and skills',
  enabledKey: 'autolearnEnabled',
  configPanel: AutolearnConfig,
  buildFeatureKey: 'autolearn',
}
```

### Handler files

```
handlers_autolearn.go          ← //go:build !noautolearn
handlers_autolearn_disabled.go ← //go:build noautolearn (503 stubs)
```

Replace `handlers_lore.go`, `handlers_lore_disabled.go`. Spawn entry handlers from `handlers_emergence.go` move to `handlers_spawn.go` (no build tag).

## Package structure

```
internal/autolearn/
├── learning.go           # Learning, Batch, LearningKind, Layer types
├── store.go              # BatchStore (JSON-file-backed, mutex + atomic rename)
├── curator_friction.go   # friction signal → []Learning prompt + parsing
├── curator_intent.go     # intent signal → []Learning prompt + parsing
├── skillfile.go          # render Learning(kind=skill) → SKILL.md
├── instructions.go       # private instruction file read/write/assemble
├── merge_curator.go      # public-layer merge prompt (LLM integrates into existing doc)
├── pending_merge.go      # PendingMerge store for public layer review
├── apply.go              # apply routing (kind × layer → target)
├── signals.go            # unified signal scanner (friction + intent from event JSONL)
├── history.go            # learning history queries (filter by status/kind/layer)
├── disabled.go           # noautolearn build tag stubs

internal/spawn/
├── store.go              # SpawnEntry CRUD (from emergence/store.go)
├── metadata.go           # EmergenceMetadata store (from emergence/metadata.go)
├── migrate.go            # One-time migration from old actions registry

internal/dashboard/
├── handlers_autolearn.go          # //go:build !noautolearn
├── handlers_autolearn_disabled.go # //go:build noautolearn
├── handlers_spawn.go              # no build tag — always compiled
```

Deleted packages: `internal/lore/`, `internal/emergence/`.

### Wiring: `ensure/manager.go`

The ensure package currently imports `internal/lore` (for `InstructionStore`) and `internal/emergence` (for `Store` and `MetadataStore`). After the refactor:

- `ensure.SetInstructionStore()` takes `*autolearn.InstructionStore` (same interface, new package)
- `ensure.SetSpawnStores()` takes `*spawn.Store` and `*spawn.MetadataStore` (same logic, new package)

Both are set via package-level setter functions, same pattern as today.

## API surface

### Autolearn routes (behind `noautolearn` build tag)

```
GET    /api/autolearn/{repo}/status                             system status
POST   /api/autolearn/{repo}/curate                             trigger curation
GET    /api/autolearn/{repo}/batches                            list batches
GET    /api/autolearn/{repo}/batches/{id}                       get batch
DELETE /api/autolearn/{repo}/batches/{id}                       dismiss batch
PATCH  /api/autolearn/{repo}/batches/{bid}/learnings/{lid}      approve/dismiss/edit/un-dismiss
POST   /api/autolearn/{repo}/forget/{lid}                       forget an applied learning
POST   /api/autolearn/{repo}/merge                              trigger public-layer merge
GET    /api/autolearn/{repo}/pending-merge                      get pending merge
PATCH  /api/autolearn/{repo}/pending-merge                      edit pending merge content
DELETE /api/autolearn/{repo}/pending-merge                      discard pending merge
POST   /api/autolearn/{repo}/push                               push merged public content
GET    /api/autolearn/{repo}/entries                            raw signal entries
DELETE /api/autolearn/{repo}/entries                            clear raw signals
GET    /api/autolearn/{repo}/prompt-history                     intent signal history
GET    /api/autolearn/{repo}/history                            all learnings (filterable)
```

### Spawn entry routes (always compiled, no build tag)

```
GET    /api/spawn/{repo}/entries                                list pinned entries
GET    /api/spawn/{repo}/entries/all                            list all entries
POST   /api/spawn/{repo}/entries                                create manual entry
PUT    /api/spawn/{repo}/entries/{id}                           update entry
DELETE /api/spawn/{repo}/entries/{id}                           delete entry
POST   /api/spawn/{repo}/entries/{id}/pin                       pin entry
POST   /api/spawn/{repo}/entries/{id}/dismiss                   dismiss entry
POST   /api/spawn/{repo}/entries/{id}/use                       record usage
GET    /api/spawn/{repo}/prompt-history                         prompt autocomplete data
```

## Storage layout

```
~/.schmux/autolearn/
├── batches/{repo}/*.json               # one file per batch
├── instructions/
│   ├── repo_private/{repo}.md
│   └── cross_repo_private/global.md
├── pending-merges/{repo}.json
└── curator-runs/{repo}/{id}/           # debug artifacts
    ├── prompt-friction.txt
    ├── prompt-intent.txt
    ├── output-friction.txt
    ├── output-intent.txt
    ├── events.jsonl
    └── run.sh
```

## Config

```json
{
  "autolearn": {
    "enabled": true,
    "curate_on_dispose": true,
    "curate_debounce_ms": 30000,
    "llm_target": "claude-sonnet",
    "instruction_files": ["CLAUDE.md"],
    "public_rule_mode": "direct_push",
    "prune_after_days": 30
  }
}
```

All fields from the current `lore` config section are preserved:

| Field                | From lore            | Notes     |
| -------------------- | -------------------- | --------- |
| `enabled`            | `enabled`            | unchanged |
| `curate_on_dispose`  | `curate_on_dispose`  | unchanged |
| `curate_debounce_ms` | `curate_debounce_ms` | unchanged |
| `llm_target`         | `llm_target`         | unchanged |
| `instruction_files`  | `instruction_files`  | unchanged |
| `public_rule_mode`   | `public_rule_mode`   | unchanged |
| `prune_after_days`   | `prune_after_days`   | unchanged |

The `auto_pr` field from lore is removed — its behavior is covered by `public_rule_mode: "create_pr"`.

The config loader accepts both `autolearn` and `lore` as the key name during transition. If both are present, `autolearn` takes precedence. The `lore` key is a read-only alias — config saves always write `autolearn`.

## Migration

Clean break for batch/proposal data. New storage path `~/.schmux/autolearn/`. Old data in `~/.schmux/lore-proposals/`, `~/.schmux/emergence/`, `~/.schmux/lore-pending-merges/` is ignored (not read, not migrated, not deleted). Single-user system — no need to preserve in-flight state.

**Note on private instructions:** The old instruction files at `~/.schmux/instructions/` contain already-applied rules — the actual useful output of past curation runs. These are not migrated automatically (the new path is `~/.schmux/autolearn/instructions/`). If you have valuable private instructions, copy them manually. Since this is a single-user system, this is acceptable.

## Implementation phases

1. **Spawn extraction** — Move `emergence.Store`, `emergence.MetadataStore`, and `emergence.Migrate*` into `internal/spawn/`. Move spawn entry handlers into `handlers_spawn.go`. Update daemon wiring and ensure package imports. Tests pass.
2. **Data model + store** — `learning.go`, `store.go`, `history.go` in `internal/autolearn/` with tests.
3. **Curators + signals** — `curator_friction.go`, `curator_intent.go`, `signals.go`, `skillfile.go`. Absorb from old packages. Port `CollectIntentSignals` from `emergence/history.go`.
4. **Apply + merge** — `apply.go`, `instructions.go`, `merge_curator.go`, `pending_merge.go`. Extend push for mixed rule+skill commits.
5. **Handlers + API** — `handlers_autolearn.go`, `handlers_autolearn_disabled.go`. Collapse old lore handlers. Add forget endpoint.
6. **Daemon wiring** — Single trigger, unified stores, update ensure imports, delete `internal/lore/` and `internal/emergence/`.
7. **Frontend** — Regenerate types, update components, replace experimental registry entry. Likely minimal since the UI already shows both in one page.

## Architecture decisions

- **Two LLM calls, not one.** Each curator focuses on its signal type (friction or intent) but can produce either kind of learning (rule or skill). This preserves prompt quality while enabling cross-kind learnings. The prompts evolve independently.
- **Cross-kind learnings allowed.** The friction curator can propose skills; the intent curator can propose rules. Both signal types feed into both kinds.
- **Dismissals are durable and reversible.** Users can browse all past learnings, un-dismiss rejected items, and "forget" applied items. This ensures the user feels in full control.
- **Destination visible on every card.** The user must always know where a learning will end up and whether it leaves the machine. Public-layer items always go through the merge/review/push workflow.
- **Spawn entries remain separate.** Skill application creates spawn entries as a side effect. Future consolidation with quick launch and pastebin is out of scope.
- **Clean break, no migration.** Single-user system — old data is not preserved.
- **Experimental feature.** Gated behind the `noautolearn` build tag and the experimental tab in /config. Same pattern as all other experimental features.

## Gotchas

- **`CollectIntentSignals` already exists.** Despite being listed as unimplemented in the emergence doc, `emergence/history.go` has a full implementation called from `handlers_emergence.go`. Port this to `autolearn/signals.go` — don't rewrite it.
- **Public skill push is new.** Current emergence only injects skills locally (git-invisible). Public skill push (committing to `.claude/skills/` in the repo) is new behavior. Skills are standalone files — no LLM merge needed, just file write + commit.
- **Mixed-content push.** If approved learnings include both public rules and public skills, the push handler writes the merged instruction file AND skill files into the worktree before committing. The `PendingMerge.SkillFiles` field carries skill content for this purpose.
- **The `schmux-` prefix on skill directories.** Currently managed by the adapter's `InjectSkill`/`RemoveSkill`. For public skills committed to the repo, keep the prefix (prevents collisions with user-created skills). Collaborators see `schmux-{name}/` directories — the prefix makes the provenance clear.
- **Config key alias.** The config loader must accept both `lore` and `autolearn` keys. Saves always write `autolearn`. This avoids breaking existing configs.
- **Private instruction data loss on migration.** Already-applied private rules at `~/.schmux/instructions/` are not auto-migrated to `~/.schmux/autolearn/instructions/`. Manual copy is needed if valuable.
- **`Category` feeds the merge prompt.** The merge curator formats rules as `[Category] Text`. The `Category` field on `Learning` must be populated by both curators — don't drop it from the prompt output schema.
- **Forget for public rules is complex.** "Forgetting" a public rule that was already pushed to CLAUDE.md requires a new commit removing the text. The forget endpoint should stage a removal for the next push, not auto-push a revert.
