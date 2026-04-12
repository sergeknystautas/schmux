# Plan: Autolearn — Unified Learning System (final)

**Goal**: Replace `internal/lore/` and `internal/emergence/` with a single `internal/autolearn/` package and extract spawn entries into `internal/spawn/`. One data model, one API surface, one build tag.

**Architecture**: Spec at `docs/specs/autolearn.md`. Key decisions: two LLM calls per curation run (friction + intent), unified `Batch`/`Learning` data model, `internal/spawn/` for always-compiled spawn entry CRUD, `noautolearn` build tag for the learning pipeline.

**Tech Stack**: Go 1.22+, chi router, TypeScript/React (Vite), Vitest

## Changes from previous version

v1→v2 addresses all critical issues from `docs/plans/autolearn-review-1.md`. v2→final addresses all critical issues from `docs/plans/autolearn-review-2.md`:

- **Fixed compile gap**: Step 4c no longer renames `loreExecutor` or removes the `emergence` import from `server.go` — `handlers_emergence.go` still needs them until Step 23 deletes it. Only spawn-specific field renames happen in Step 4.
- **Fixed dependency ordering**: Config step (Step 20) now comes before daemon wiring (Step 21). Groups renumbered: config is Group 6, daemon wiring is Group 7.
- **Fixed missing route**: `prompt-history` added back to autolearn route group per spec. Both `/api/spawn/{repo}/prompt-history` and `/api/autolearn/{repo}/prompt-history` exist.
- **Fixed handlers_features.go**: Step 4f no longer claims to remove a non-existent `emergence` import.
- **Added `TriggerAutolearnCuration`**: Explicitly listed in handler mapping (replaces `TriggerEmergenceCuration`).

Full list of v1→v2 changes:

1. **Fixed line reference**: `loreStore` field is at `server.go` line 212, not 243.
2. **Fixed line reference**: instruction store wiring in `daemon.go` is at line 1125, not 498.
3. **Added**: `internal/session/manager.go` has `loreCallback` field (line 56) and `SetLoreCallback()` (line 188) — renamed to `autolearnCallback`/`SetAutolearnCallback()` in Step 21.
4. **Added**: `internal/dashboard/handlers_config.go` calls `s.refreshLoreExecutor(cfg)` at line 822 — Step 17 now defines `refreshAutolearnExecutor` and Step 17d updates the call site.
5. **Added**: WebSocket event name `lore_merge_complete` is renamed to `autolearn_merge_complete` in Steps 17 and 26.
6. **Added**: `handleLoreApplyMerge` (server.go line 797, handlers_lore.go line 314) — explicitly removed in Step 17 (its batch-scoped functionality is replaced by the per-learning approve+apply flow).
7. **Fixed**: `handleLoreCurationsActive` route stays repo-independent (outside the `{repo}` group), matching the current API semantics.
8. **Expanded**: Frontend scope now lists all 31 affected files. Step 26 is broken into 6 sub-steps.
9. **Fixed**: `ReadFileFromRepo` is in `internal/lore/curator.go` (line 192), not `merge_curator.go`.
10. **Added**: `contracts/emergence.go` cleanup in Step 23 — `SkillProposal` type is removed, spawn-related types are kept.
11. **Added**: `CollectPromptHistory` moves to `internal/spawn/history.go` (spawn-related, not autolearn). Step 4 now includes this extraction.
12. **Added**: `LearningUpdate` type definition in Step 5.
13. **Consolidated**: `disabled.go` is created once in Step 19 after all types are defined. Step 8 is removed; the note in Step 19 explains that `go build -tags noautolearn` won't work until Step 19 is complete.
14. **Updated**: All `git commit` commands replaced with "commit per the project's commit workflow" (CLAUDE.md requires `/commit`).
15. **Added**: Data path note — spawn entry data path stays `~/.schmux/emergence/` (package moves, data does not).
16. **Added**: CI/docs references to `nolore` build tag must be updated (Step 23).
17. **Split**: Step 4 broken into sub-steps (4a-4f). Step 17 broken into sub-steps (17a-17h).

## Dependency groups

| Group | Steps | Can Parallelize | Notes                                                                           |
| ----- | ----- | --------------- | ------------------------------------------------------------------------------- |
| 1     | 1–4   | Yes             | `internal/spawn/` package (pure extraction, no new logic)                       |
| 2     | 5–7   | Yes             | `internal/autolearn/` data model + store (independent of spawn)                 |
| 3     | 8–11  | Yes             | Curators + signals (depends on data model from Group 2)                         |
| 4     | 12–15 | Yes             | Apply + merge + pending merge (depends on data model)                           |
| 5     | 16–19 | No              | Handlers + API + disabled stubs (depends on everything above)                   |
| 6     | 20    | No              | Config (must come before daemon wiring — provides `GetAutolearnEnabled()` etc.) |
| 7     | 21–23 | No              | Daemon wiring + delete old packages (depends on config from Group 6)            |
| 8     | 24–25 | No              | Features contract, frontend                                                     |
| 9     | 26    | No              | End-to-end verification                                                         |

---

## Group 1: Extract `internal/spawn/`

### Step 1: Create `internal/spawn/store.go`

**File**: `internal/spawn/store.go` (new)

Copy `internal/emergence/store.go` into `internal/spawn/store.go`. Change `package emergence` to `package spawn`. Update the import path for `internal/api/contracts` (unchanged, contracts still live there). No logic changes — this is a pure move.

The file contains: `Store` struct, `NewStore()`, `List()`, `ListAll()`, `Get()`, `Create()`, `Update()`, `Delete()`, `Pin()`, `Dismiss()`, `RecordUse()`, `AddProposed()`, `ProposedAndPinnedNames()`, `GenerateID()`, `generateID()`.

**Data path note**: The daemon currently passes `filepath.Join(schmuxDir, "emergence")` as the base dir. This path stays the same — the package moves but the on-disk data at `~/.schmux/emergence/` does not move.

### 1a. Write test

**File**: `internal/spawn/store_test.go` (new)

Copy `internal/emergence/store_test.go` to `internal/spawn/store_test.go`. Change package to `spawn`. Update imports from `internal/emergence` to `internal/spawn`. Run to confirm all tests pass.

### 1b. Run test

```bash
go test ./internal/spawn/ -v -run TestStore
```

### 1c. Commit

Commit per the project's commit workflow with message: `refactor(spawn): extract spawn entry store from emergence`

---

### Step 2: Create `internal/spawn/metadata.go`

**File**: `internal/spawn/metadata.go` (new)

Copy `internal/emergence/metadata.go` to `internal/spawn/metadata.go`. Change package to `spawn`. Update import for `internal/api/contracts`.

The file contains: `MetadataStore` struct, `NewMetadataStore()`, `Save()`, `Get()`, `Delete()`.

### 2a. Write test

**File**: `internal/spawn/metadata_test.go` (new)

Copy `internal/emergence/metadata_test.go`. Change package + imports.

### 2b. Run test

```bash
go test ./internal/spawn/ -v -run TestMetadata
```

### 2c. Commit

Commit per the project's commit workflow with message: `refactor(spawn): extract metadata store from emergence`

---

### Step 3: Create `internal/spawn/migrate.go`

**File**: `internal/spawn/migrate.go` (new)

Copy `internal/emergence/migrate.go` to `internal/spawn/migrate.go`. Change package to `spawn`. The `MigrateFromActions()` function takes a `*Store` — the type now lives in the same package, so the import is internal.

### 3a. Write test

**File**: `internal/spawn/migrate_test.go` (new)

Copy `internal/emergence/migrate_test.go`. Change package + imports.

### 3b. Run test

```bash
go test ./internal/spawn/ -v -run TestMigrate
```

### 3c. Commit

Commit per the project's commit workflow with message: `refactor(spawn): extract actions migration from emergence`

---

### Step 4: Create `handlers_spawn.go`, extract `spawn/history.go`, and rewire imports

This step is large (6 files across 4 packages). The sub-steps below break it into independently verifiable pieces.

#### Step 4a: Create `internal/spawn/history.go`

**File**: `internal/spawn/history.go` (new)

Extract `CollectPromptHistory()` from `internal/emergence/history.go` (line 21) into `internal/spawn/history.go`. This function reads event JSONL files and extracts prompt history entries for autocomplete — it is spawn-related (prompt history for the spawn dropdown), not autolearn-related.

Change package to `spawn`. The function signature stays the same: `func CollectPromptHistory(workspacePaths []string, maxResults int) []contracts.PromptHistoryEntry`.

Also copy the relevant test cases from `internal/emergence/history_test.go` (tests named `TestCollectPromptHistory_*`) into `internal/spawn/history_test.go`.

```bash
go test ./internal/spawn/ -v -run TestCollectPromptHistory
```

#### Step 4b: Create `internal/dashboard/handlers_spawn.go`

**File**: `internal/dashboard/handlers_spawn.go` (new)

Extract these handlers from `internal/dashboard/handlers_emergence.go` into `handlers_spawn.go`:

- `extractSkillDescription()`
- `validateEmergenceRepo()` — rename to `validateSpawnRepo()`
- `handleListSpawnEntries()`
- `handleListAllSpawnEntries()`
- `handleCreateSpawnEntry()`
- `handleUpdateSpawnEntry()`
- `handleDeleteSpawnEntry()`
- `handlePinSpawnEntry()`
- `handleDismissSpawnEntry()`
- `handleRecordSpawnEntryUse()`
- `handlePromptHistory()` — update to call `spawn.CollectPromptHistory()` instead of `emergence.CollectPromptHistory()`

No build tag on this file — spawn entries are always compiled.

Update all handlers to reference `s.spawnStore` and `s.spawnMetadataStore` instead of `s.emergenceStore` and `s.emergenceMetadataStore`.

```bash
go build ./internal/dashboard/...
```

#### Step 4c: Rewire `server.go` fields and routes

**File**: `internal/dashboard/server.go`

Server fields — **only rename the emergence-specific fields**. Do NOT rename lore fields or the `loreExecutor` — `handlers_emergence.go` still references them and will break. Lore fields are renamed later in Step 16a.

- Replace `emergenceStore *emergence.Store` to `spawnStore *spawn.Store` (line 243)
- Replace `emergenceMetadataStore *emergence.MetadataStore` to `spawnMetadataStore *spawn.MetadataStore` (line 244)
- Replace `SetEmergenceStore()` to `SetSpawnStore()` (line 436)
- Replace `SetEmergenceMetadataStore()` to `SetSpawnMetadataStore()` (line 441)
- **Keep** `import "internal/emergence"` — still needed by `handlers_emergence.go` until Step 22 deletes it. Add `import "internal/spawn"` alongside it.

Route group (line 811): change path from `/emergence/{repo}` to `/spawn/{repo}`, change middleware from `validateEmergenceRepo` to `validateSpawnRepo`. Remove the curate route (`r.Post("/curate", s.handleEmergenceCurate)`) — this moves to autolearn handlers later.

**Update `handlers_emergence.go` field references**: The three remaining functions (`handleEmergenceCurate`, `runEmergenceCuration`, `TriggerEmergenceCuration`) reference `s.emergenceStore` and `s.emergenceMetadataStore`. Update these references to `s.spawnStore` and `s.spawnMetadataStore`. These functions still reference `s.loreExecutor` and the `emergence` package import — both are left untouched until they are replaced in Steps 16/22.

#### Step 4d: Rewire `ensure/manager.go`

**File**: `internal/workspace/ensure/manager.go`

- Replace `import "internal/emergence"` to `import "internal/spawn"` (line 13)
- Replace `emergenceStore *emergence.Store` to `spawnStore *spawn.Store` (line 26)
- Replace `emergenceMetadataStore *emergence.MetadataStore` to `spawnMetadataStore *spawn.MetadataStore` (line 29)
- Replace `SetEmergenceStores()` to `SetSpawnStores()` (line 45)

Update `internal/workspace/ensure/manager_test.go` — change imports from `internal/emergence` to `internal/spawn`.

#### Step 4e: Rewire `daemon.go` spawn references

**File**: `internal/daemon/daemon.go`

- Replace `import "internal/emergence"` to `import "internal/spawn"` (line 32)
- Replace `emergence.NewStore()` to `spawn.NewStore()` (line 1086)
- Replace `emergence.NewMetadataStore()` to `spawn.NewMetadataStore()` (line 1087)
- Replace `server.SetEmergenceStore()` to `server.SetSpawnStore()` (line 1088)
- Replace `server.SetEmergenceMetadataStore()` to `server.SetSpawnMetadataStore()` (line 1089)
- Replace `ensure.SetEmergenceStores()` to `ensure.SetSpawnStores()` (line 1090)
- Replace `emergence.MigrateFromActions()` to `spawn.MigrateFromActions()` (line 1099)

**Data path note**: The base dir passed to `spawn.NewStore()` remains `filepath.Join(schmuxDir, "emergence")`. The package name changes but the data directory does not. Existing spawn entry data at `~/.schmux/emergence/` continues to be read.

#### Step 4f: Verify imports compile

Note: `handlers_features.go` does NOT import `internal/emergence` — it imports `internal/lore`, which is updated later in Step 22. No changes needed to this file in Step 4.

#### 4-final. Run tests

```bash
go build ./... && go test ./internal/dashboard/... ./internal/workspace/ensure/... ./internal/spawn/... -v -count=1
```

#### 4-commit. Commit

Commit per the project's commit workflow with message: `refactor(spawn): rewire all imports from emergence to spawn`

---

## Group 2: `internal/autolearn/` data model + store

### Step 5: Create `internal/autolearn/learning.go`

**File**: `internal/autolearn/learning.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Define all types from the spec: `LearningKind`, `LearningStatus`, `Layer`, `Learning`, `RuleDetails`, `SkillDetails`, `SourceRef`, `BatchStatus`, `Batch`, `PendingMergeStatus`. Include `EffectiveLayer()` on `Learning` and `AllResolved()` on `Batch`.

Also define `LearningUpdate` — the parameter type for `BatchStore.UpdateLearning()`:

```go
type LearningUpdate struct {
    Status      *LearningStatus `json:"status,omitempty"`
    Title       *string         `json:"title,omitempty"`
    Description *string         `json:"description,omitempty"`
    ChosenLayer *Layer          `json:"chosen_layer,omitempty"`
    Rule        *RuleDetails    `json:"rule,omitempty"`
    Skill       *SkillDetails   `json:"skill,omitempty"`
}
```

### 5a. Write test

**File**: `internal/autolearn/learning_test.go`

Test `EffectiveLayer()` (nil ChosenLayer returns SuggestedLayer; set ChosenLayer returns it). Test `AllResolved()` (all approved = true, one pending = false, mix of approved+dismissed = true).

### 5b. Run test

```bash
go test ./internal/autolearn/ -v -run TestLearning
```

### 5c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): define Learning, Batch, Layer, and LearningUpdate types`

---

### Step 6: Create `internal/autolearn/store.go`

**File**: `internal/autolearn/store.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Implement `BatchStore` with all methods from the spec: `NewBatchStore()`, `Save()`, `Get()`, `List()`, `UpdateStatus()`, `UpdateLearning()`, `GetLearning()`, `PendingLearningTitles()`, `DismissedLearningTitles()`, `DismissedLearnings()`.

`UpdateLearning()` takes the `LearningUpdate` type defined in Step 5.

Storage: one JSON file per batch at `{baseDir}/batches/{repo}/{batchID}.json`. Use mutex + temp-file + rename pattern from `internal/lore/proposals.go` (or `internal/spawn/store.go`).

Add `IsAvailable() bool { return true }` to this file (used by feature detection).

### 6a. Write test

**File**: `internal/autolearn/store_test.go`

Table-driven tests:

- Save and Get round-trip
- List returns all batches for a repo, sorted by CreatedAt descending
- UpdateLearning changes status, text, chosen_layer
- GetLearning finds a learning across batches
- PendingLearningTitles returns titles of pending learnings only
- DismissedLearningTitles returns titles of dismissed learnings only
- DismissedLearnings returns full Learning objects

### 6b. Run test

```bash
go test ./internal/autolearn/ -v -run TestBatchStore
```

### 6c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): implement BatchStore with JSON persistence`

---

### Step 7: Create `internal/autolearn/history.go`

**File**: `internal/autolearn/history.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Implement query functions:

- `FilterLearnings(batches []*Batch, kind *LearningKind, status *LearningStatus, layer *Layer) []Learning`
- `AllLearnings(batches []*Batch) []Learning` — flattens batches into a single slice

These are stateless filter functions over batch data returned by `BatchStore.List()`.

### 7a. Write test

**File**: `internal/autolearn/history_test.go`

Test filtering by kind, status, layer, and combinations. Test AllLearnings flattening.

### 7b. Run test

```bash
go test ./internal/autolearn/ -v -run TestHistory
```

### 7c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): implement learning history queries`

---

## Group 3: Curators + signals

### Step 8: Create `internal/autolearn/signals.go`

**File**: `internal/autolearn/signals.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port two functions:

1. `ReadFrictionSignals()` — adapted from `internal/lore/scratchpad.go`'s `ReadEntriesFromEvents()` and `ReadEntries()`. Keep the `Entry` type and `EntryFilter` type here. Keep `FilterRaw()`, `FilterByParams()`, `MarkEntriesDirect()`, `MarkEntriesByTextFromEntries()`, `PruneEntries()`.
2. `CollectIntentSignals()` — port directly from `internal/emergence/history.go` (line 97, it already exists and is fully implemented). Returns `[]IntentSignal`.

Also port `LoreStatePath()` to rename as `StatePath()`, and `LoreStateDir()` to `StateDir()`.

### 8a. Write test

**File**: `internal/autolearn/signals_test.go`

Port relevant tests from `internal/lore/scratchpad_test.go` and `internal/emergence/history_test.go` (only the `TestCollectIntentSignals_*` tests, not the `TestCollectPromptHistory_*` tests which moved to `internal/spawn/` in Step 4a).

### 8b. Run test

```bash
go test ./internal/autolearn/ -v -run TestSignals
```

### 8c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): unified signal scanner (friction + intent)`

---

### Step 9: Create `internal/autolearn/curator_friction.go`

**File**: `internal/autolearn/curator_friction.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Adapt from `internal/lore/curator.go`:

- `BuildFrictionPrompt(entries []Entry, existingTitles []string, dismissedTitles []string) string` — same logic as `BuildExtractionPrompt()` but the output schema produces `[]Learning` (with `kind`, `title`, `category`, `suggested_layer`, `sources`, and optional `skill` sub-object for cross-kind).
- `ParseFrictionResponse(response string) (*FrictionCuratorResponse, error)` — same fallback chain (direct JSON, strip fences, extract outermost object).
- `FrictionCuratorResponse` struct: `Learnings []Learning`, `DiscardedEntries []string`.

Also port `ReadFileFromRepo()` from `internal/lore/curator.go` (line 192) — this reads a file from HEAD in a git repo. Note: the disabled stub signature uses 2 string args (`_, _ string`) but the real function takes `ctx context.Context, repoDir, relPath string` (3 args). The `disabled.go` stub must match the real signature.

Register the schema label `schema.LabelAutolearnFriction` (add to `internal/schema/schema.go`).

### 9a. Write test

**File**: `internal/autolearn/curator_friction_test.go`

Port from `internal/lore/curator_test.go`. Update to test cross-kind output (a response containing both rule and skill learnings). Include `ReadFileFromRepo` tests (currently in `internal/lore/curator_test.go`).

### 9b. Run test

```bash
go test ./internal/autolearn/ -v -run TestFrictionCurator
```

### 9c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): friction curator with cross-kind learning output`

---

### Step 10: Create `internal/autolearn/curator_intent.go`

**File**: `internal/autolearn/curator_intent.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Adapt from `internal/emergence/curator.go`:

- `BuildIntentPrompt(signals []IntentSignal, existingTitles []string, dismissedTitles []string, repoName string) string` — same logic as `BuildEmergencePrompt()` but output schema produces `[]Learning` (mostly skills, can include rules).
- `ParseIntentResponse(response string) (*IntentCuratorResponse, error)` — same fallback chain.
- `IntentCuratorResponse` struct: `NewLearnings []Learning`, `UpdatedLearnings []Learning`, `DiscardedSignals map[string]string`.

Register `schema.LabelAutolearnIntent` in `internal/schema/schema.go`.

### 10a. Write test

**File**: `internal/autolearn/curator_intent_test.go`

Port from `internal/emergence/curator_test.go`. Test cross-kind (intent curator producing a rule).

### 10b. Run test

```bash
go test ./internal/autolearn/ -v -run TestIntentCurator
```

### 10c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): intent curator with cross-kind learning output`

---

### Step 11: Create `internal/autolearn/skillfile.go`

**File**: `internal/autolearn/skillfile.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port `internal/emergence/skillfile.go` as `GenerateSkillFile(learning Learning) string`. Takes a `Learning` (must be `KindSkill`) and renders the SKILL.md with YAML frontmatter from `learning.Skill.*` fields.

Note: The current `GenerateSkillFile` takes `contracts.SkillProposal`. The new version takes `Learning` instead. The `SkillProposal` type in `contracts/emergence.go` becomes orphaned after this change — cleanup is handled in Step 22.

### 11a. Write test

**File**: `internal/autolearn/skillfile_test.go`

Port from `internal/emergence/skillfile_test.go`.

### 11b. Run test

```bash
go test ./internal/autolearn/ -v -run TestSkillFile
```

### 11c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): skill file renderer`

---

## Group 4: Apply + merge + pending merge

### Step 12: Create `internal/autolearn/instructions.go`

**File**: `internal/autolearn/instructions.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port `internal/lore/instructions.go` directly. Same `InstructionStore` struct with `Read()`, `Write()`, `Assemble()`. Change base dir default to `~/.schmux/autolearn/instructions/`.

### 12a. Write test

**File**: `internal/autolearn/instructions_test.go`

Port from `internal/lore/instructions_test.go`.

### 12b. Run test

```bash
go test ./internal/autolearn/ -v -run TestInstruction
```

### 12c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): instruction file storage`

---

### Step 13: Create `internal/autolearn/merge_curator.go`

**File**: `internal/autolearn/merge_curator.go` (new)

Port `internal/lore/merge_curator.go`. Keep `BuildMergePrompt()`, `ParseMergeResponse()`, `MergeResponse` struct. The merge prompt takes `[]Learning` (filtered to rules only) instead of `[]Rule` — format each as `[Category] Title`.

Note: `ReadFileFromRepo()` was already ported into `curator_friction.go` in Step 9 (from `internal/lore/curator.go` line 192). It is NOT in `merge_curator.go` despite the old plan stating otherwise.

### 13a. Write test

**File**: `internal/autolearn/merge_curator_test.go`

Port from `internal/lore/merge_curator_test.go`. Update to use `Learning` instead of `Rule`.

### 13b. Run test

```bash
go test ./internal/autolearn/ -v -run TestMergeCurator
```

### 13c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): merge curator for public-layer rules`

---

### Step 14: Create `internal/autolearn/pending_merge.go`

**File**: `internal/autolearn/pending_merge.go` (new)

Port `internal/lore/pending_merge.go`. Update struct to use `LearningIDs`/`BatchIDs` instead of `RuleIDs`/`ProposalIDs`. Add `SkillFiles map[string]string` field. Keep `PendingMergeStore` with `Save()`, `Get()`, `Delete()`, `UpdateEditedContent()`, `InvalidateIfContainsLearning()` (renamed from `InvalidateIfContainsRule()`).

### 14a. Write test

**File**: `internal/autolearn/pending_merge_test.go`

Port from `internal/lore/pending_merge_test.go`. Add test for `SkillFiles` round-trip.

### 14b. Run test

```bash
go test ./internal/autolearn/ -v -run TestPendingMerge
```

### 14c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): pending merge store with skill files support`

---

### Step 15: Create `internal/autolearn/apply.go`

**File**: `internal/autolearn/apply.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port `internal/lore/apply.go`. Expand `ApplyToLayer()` to handle both kinds:

- `KindRule` + private layer: write to instruction file (existing logic)
- `KindRule` + public layer: return error (must go through merge/push flow)
- `KindSkill` + private layer: call adapter `InjectSkill()` on all workspaces
- `KindSkill` + public layer: return error (must go through push flow)

Also add `NormalizeLearningTitle()` (from `NormalizeRuleText()`) and `DeduplicateLearnings()` (from `DeduplicateRules()`).

### 15a. Write test

**File**: `internal/autolearn/apply_test.go`

Port from `internal/lore/apply_test.go`. Add test for skill private-layer apply.

### 15b. Run test

```bash
go test ./internal/autolearn/ -v -run TestApply
```

### 15c. Commit

Commit per the project's commit workflow with message: `feat(autolearn): apply routing for kind x layer matrix`

---

## Group 5: Handlers + API + disabled stubs

### Step 16: Create `handlers_autolearn.go`

This step is large. The sub-steps below break it into independently verifiable pieces.

#### Step 16a: Define server fields

**File**: `internal/dashboard/server.go`

Replace lore-related server fields:

- Replace `loreStore *lore.ProposalStore` (line 212) with `autolearnStore *autolearn.BatchStore`
- Replace `loreExecutor` (line 215) with `autolearnExecutor` (same signature)
- Replace `loreInstructionStore *lore.InstructionStore` (line 218) with `autolearnInstructionStore *autolearn.InstructionStore`
- Replace `lorePendingMergeStore *lore.PendingMergeStore` (line 221) with `autolearnPendingMergeStore *autolearn.PendingMergeStore`

Add setter methods: `SetAutolearnStore()`, `SetAutolearnInstructionStore()`, `SetAutolearnPendingMergeStore()`, `SetAutolearnExecutor()`.

Update import from `internal/lore` to `internal/autolearn`.

#### Step 16b: Create handler file

**File**: `internal/dashboard/handlers_autolearn.go` (new)

```go
//go:build !noautolearn

package dashboard
```

Consolidate handlers from `handlers_lore.go` and the curation handler from `handlers_emergence.go`.

**Handler mapping** (old to new):

- `handleLoreStatus` to `handleAutolearnStatus`
- `handleLoreProposals` to `handleAutolearnBatches`
- `handleLoreProposalGet` to `handleAutolearnBatchGet`
- `handleLoreDismiss` to `handleAutolearnBatchDismiss`
- `handleLoreRuleUpdate` to `handleAutolearnLearningUpdate`
- `handleLoreEntries` to `handleAutolearnEntries`
- `handleLoreEntriesClear` to `handleAutolearnEntriesClear`
- `handleLoreCurate` to `handleAutolearnCurate` (runs both friction + intent curators)
- `handleLoreUnifiedMerge` to `handleAutolearnMerge`
- `handleLorePendingMerge*` to `handleAutolearnPendingMerge*`
- `handleLorePush` to `handleAutolearnPush` (extended for `SkillFiles`)
- `handleLoreCurationsList` to `handleAutolearnCurationsList`
- `handleLoreCurationsActive` to `handleAutolearnCurationsActive`
- `handleLoreCurationLog` to `handleAutolearnCurationLog`
- `handleEmergenceCurate` to absorbed into `handleAutolearnCurate`
- `TriggerEmergenceCuration` (handlers_emergence.go line 431, called from daemon.go line 1297) to `TriggerAutolearnCuration` — the daemon callback calls this method directly, so it must exist on the Server. The unified version triggers both friction and intent curation.
- New: `handleAutolearnForget` — reverses an applied learning
- New: `handleAutolearnHistory` — filterable learning history across all batches
- New: `handleAutolearnPromptHistory` — intent signal history for curation pipeline

**Removed handler** (not ported):

- `handleLoreApplyMerge` (server.go line 797, handlers_lore.go line 314) — this handler applied a reviewed merge to a specific proposal. In the autolearn model, the per-learning approve+apply flow and the unified merge+push flow replace this. There is no equivalent route.

**WebSocket event rename**:

- All broadcasts of `lore_merge_complete` event type must use `autolearn_merge_complete` instead (currently at handlers_lore.go lines 1011, 1024, 1041).

**`refreshAutolearnExecutor` method**:
Define `refreshAutolearnExecutor(cfg *config.Config)` in this file. This method recreates the autolearn LLM executor from the current config, same pattern as the existing `refreshLoreExecutor` (handlers_lore.go line 1318). This method is called from `handlers_config.go` when config is saved.

#### Step 16c: Register routes

**File**: `internal/dashboard/server.go`

Replace the lore route block (lines 788-808) and add new autolearn routes. **Important**: `curations/active` stays OUTSIDE the `{repo}` group to preserve the current repo-independent API semantics (server.go line 790).

```go
// Autolearn routes (global, repo-independent)
r.Get("/autolearn/status", s.handleAutolearnStatus)
r.Get("/autolearn/curations/active", s.handleAutolearnCurationsActive)

// Autolearn routes (repo-scoped)
r.Route("/autolearn/{repo}", func(r chi.Router) {
    r.Use(validateAutolearnRepo)
    r.Get("/batches", s.handleAutolearnBatches)
    r.Get("/batches/{batchID}", s.handleAutolearnBatchGet)
    r.Delete("/batches/{batchID}", s.handleAutolearnBatchDismiss)
    r.Patch("/batches/{batchID}/learnings/{learningID}", s.handleAutolearnLearningUpdate)
    r.Post("/forget/{learningID}", s.handleAutolearnForget)
    r.Get("/entries", s.handleAutolearnEntries)
    r.Delete("/entries", s.handleAutolearnEntriesClear)
    r.Post("/curate", s.handleAutolearnCurate)
    r.Post("/merge", s.handleAutolearnMerge)
    r.Get("/pending-merge", s.handleAutolearnPendingMergeGet)
    r.Patch("/pending-merge", s.handleAutolearnPendingMergePatch)
    r.Delete("/pending-merge", s.handleAutolearnPendingMergeDelete)
    r.Post("/push", s.handleAutolearnPush)
    r.Get("/history", s.handleAutolearnHistory)
    r.Get("/prompt-history", s.handleAutolearnPromptHistory)
    r.Get("/curations", s.handleAutolearnCurationsList)
    r.Get("/curations/{curationID}/log", s.handleAutolearnCurationLog)
})
```

Note: `prompt-history` exists in both route groups per the spec. The spawn route (`/api/spawn/{repo}/prompt-history`) serves prompt autocomplete data for the spawn dropdown. The autolearn route (`/api/autolearn/{repo}/prompt-history`) serves intent signal history for the curation pipeline. These may share an implementation initially but serve different purposes.

#### Step 16d: Update `handlers_config.go`

**File**: `internal/dashboard/handlers_config.go`

- Replace `s.refreshLoreExecutor(cfg)` (line 822) with `s.refreshAutolearnExecutor(cfg)`

#### Step 16e: Verify build

```bash
go build ./internal/dashboard/...
```

#### Step 16f: Commit

Commit per the project's commit workflow with message: `feat(autolearn): unified API handlers`

---

### Step 17: Create `handlers_autolearn_disabled.go`

**File**: `internal/dashboard/handlers_autolearn_disabled.go` (new)

```go
//go:build noautolearn

package dashboard
```

Stub every handler method and the `validateAutolearnRepo` middleware. Each returns 503 "Autolearn is not available in this build". Follow the exact pattern of `internal/lore/disabled.go` / `handlers_lore_disabled.go`.

Also stub: `refreshAutolearnExecutor()`, `autolearnWorkspace` struct, `getAutolearnWorkspaces()`, any helper types used in handler signatures.

### 17a. Verify both builds

```bash
go build ./internal/dashboard/...
go build -tags noautolearn ./internal/dashboard/...
```

### 17b. Commit

Commit per the project's commit workflow with message: `feat(autolearn): disabled stubs for noautolearn build tag`

---

### Step 18: Create `internal/autolearn/disabled.go`

**File**: `internal/autolearn/disabled.go` (new)

```go
//go:build noautolearn

package autolearn
```

Now that all types and functions are finalized in Steps 5-15, create `disabled.go` with stubs for everything:

From `learning.go` (Step 5): `LearningKind`, `LearningStatus`, `Layer`, `Learning`, `RuleDetails`, `SkillDetails`, `SourceRef`, `BatchStatus`, `Batch`, `LearningUpdate`, `PendingMergeStatus`, `EffectiveLayer()`, `AllResolved()`.

From `store.go` (Step 6): `BatchStore`, `NewBatchStore()`, `Save()`, `Get()`, `List()`, `UpdateStatus()`, `UpdateLearning()`, `GetLearning()`, `PendingLearningTitles()`, `DismissedLearningTitles()`, `DismissedLearnings()`, `IsAvailable() bool { return false }`.

From `history.go` (Step 7): `FilterLearnings()`, `AllLearnings()`.

From `signals.go` (Step 8): `Entry`, `EntryFilter`, `IntentSignal`, `ReadEntriesFromEvents()`, `ReadEntries()`, `FilterRaw()`, `FilterByParams()`, `MarkEntriesDirect()`, `MarkEntriesByTextFromEntries()`, `PruneEntries()`, `StatePath()`, `StateDir()`, `SetLogger()`, `ParseEntry()`.

From `curator_friction.go` (Step 9): `BuildFrictionPrompt()`, `ParseFrictionResponse()`, `FrictionCuratorResponse`, `ReadFileFromRepo()` (note: 3 args — `ctx context.Context, repoDir, relPath string`).

From `curator_intent.go` (Step 10): `BuildIntentPrompt()`, `ParseIntentResponse()`, `IntentCuratorResponse`.

From `skillfile.go` (Step 11): `GenerateSkillFile()`.

From `instructions.go` (Step 12): `InstructionStore`, `NewInstructionStore()`, `Read()`, `Write()`, `Assemble()`.

From `merge_curator.go` (Step 13): `BuildMergePrompt()`, `ParseMergeResponse()`, `MergeResponse`.

From `pending_merge.go` (Step 14): `PendingMerge`, `PendingMergeStore`, `NewPendingMergeStore()`, `Save()`, `Get()`, `Delete()`, `UpdateEditedContent()`, `InvalidateIfContainsLearning()`.

From `apply.go` (Step 15): `ApplyToLayer()`, `NormalizeLearningTitle()`, `DeduplicateLearnings()`.

**Note**: `go build -tags noautolearn` will not work until this step is complete. Steps 5-15 only compile with the default (non-disabled) build tag.

### 18a. Verify both builds

```bash
go build ./internal/autolearn/
go build -tags noautolearn ./internal/autolearn/
go build -tags noautolearn ./...
```

### 18b. Commit

Commit per the project's commit workflow with message: `feat(autolearn): complete disabled.go stubs`

---

### Step 19: Port handler tests

**File**: `internal/dashboard/handlers_autolearn_test.go` (new)

Port from `internal/dashboard/handlers_lore_test.go`. Update all types (`Proposal` to `Batch`, `Rule` to `Learning`, etc.), endpoint paths (`/lore/` to `/autolearn/`), and field names.

### 19a. Run tests

```bash
go test ./internal/dashboard/ -v -run TestAutolearn -count=1
```

### 19b. Commit

Commit per the project's commit workflow with message: `test(autolearn): port handler tests from lore`

---

## Group 6: Config (must come before daemon wiring)

### Step 20: Update config

**File**: `internal/config/config.go`

1. Add `Autolearn *AutolearnConfig` field to `Config` struct (alongside existing `Lore`)
2. Define `AutolearnConfig` with all fields from spec (same as `LoreConfig` minus `AutoPR`):
   ```go
   type AutolearnConfig struct {
       Enabled          *bool    `json:"enabled,omitempty"`
       CurateOnDispose  string   `json:"curate_on_dispose,omitempty"`
       CurateDebounceMs int      `json:"curate_debounce_ms,omitempty"`
       Target           string   `json:"llm_target,omitempty"`
       InstructionFiles []string `json:"instruction_files,omitempty"`
       PublicRuleMode   string   `json:"public_rule_mode,omitempty"`
       PruneAfterDays   int      `json:"prune_after_days,omitempty"`
   }
   ```
3. Add config alias logic: if `Autolearn` is nil but `Lore` is non-nil, copy `Lore` to `Autolearn` on load
4. Add accessor methods: `GetAutolearnEnabled()`, `GetAutolearnTarget()`, `GetAutolearnTargetRaw()`, `GetAutolearnCurateOnDispose()`, `GetAutolearnDebounceMs()`, `GetAutolearnPruneAfterDays()`, `GetAutolearnInstructionFiles()`, `GetAutolearnPublicRuleMode()`
5. Config save: always write `autolearn`, never write `lore`

**File**: `internal/api/contracts/config.go`

Update contract types if the config tab reads from them.

### 20a. Write test

**File**: `internal/config/config_test.go` (add test cases)

Test: loading config with `lore` key populates `Autolearn`. Test: saving config writes `autolearn` not `lore`.

### 20b. Run test

```bash
go test ./internal/config/ -v -run TestAutolearn -count=1
```

### 20c. Commit

Commit per the project's commit workflow with message: `feat(config): add autolearn config with lore key alias`

---

## Group 7: Daemon wiring + delete old packages

### Step 21: Rewire daemon

**File**: `internal/daemon/daemon.go`

Replace the lore wiring block (lines 1116-1301) with autolearn wiring:

1. Replace `import "internal/lore"` to `import "internal/autolearn"` (line 37)
2. Replace `loreLog := logging.Sub(logger, "lore")` to `autolearnLog := logging.Sub(logger, "autolearn")` (line 322)
3. Replace `lore.SetLogger(loreLog)` to `autolearn.SetLogger(autolearnLog)` (line 332)
4. Replace instruction store wiring (line 1125): `autolearn.NewInstructionStore(filepath.Join(schmuxDir, "autolearn", "instructions"))` — note the path change from `~/.schmux/instructions/` to `~/.schmux/autolearn/instructions/` (see spec's migration note about manual copy)
5. Replace `ensure.SetInstructionStore()` call to use new autolearn type
6. Replace the `if cfg.GetLoreEnabled()` block (lines 1117-1301):
   - Use `cfg.GetAutolearnEnabled()` (config method from Step 20)
   - Create `autolearn.NewBatchStore(filepath.Join(schmuxDir, "autolearn", "batches"), autolearnLog)`
   - Create `autolearn.NewPendingMergeStore(filepath.Join(schmuxDir, "autolearn", "pending-merges"), autolearnLog)`
   - Wire executor using `cfg.GetAutolearnTarget()`
   - Replace on-dispose callback to call unified curation (both friction + intent)
   - Replace `server.TriggerEmergenceCuration()` (line 1297) with integrated trigger in unified callback

**File**: `internal/session/manager.go`

7. Rename `loreCallback` field (line 56) to `autolearnCallback`
8. Rename `SetLoreCallback()` method (line 188) to `SetAutolearnCallback()`
9. Update callback invocation (line 1467-1473) to use `m.autolearnCallback`

**File**: `internal/daemon/daemon.go`

10. Replace `sm.SetLoreCallback(...)` (line 1150) with `sm.SetAutolearnCallback(...)`

### 21a. Verify build

```bash
go build ./cmd/schmux/
```

### 21b. Commit

Commit per the project's commit workflow with message: `feat(autolearn): rewire daemon from lore+emergence to autolearn`

---

### Step 22: Update `ensure/manager.go`

**File**: `internal/workspace/ensure/manager.go`

1. Replace `import "internal/lore"` to `import "internal/autolearn"` (line 14)
2. Replace `instrStore *lore.InstructionStore` to `instrStore *autolearn.InstructionStore` (line 23)
3. Replace `SetInstructionStore(s *lore.InstructionStore)` to `SetInstructionStore(s *autolearn.InstructionStore)` (line 40)
4. Update all `instrStore.Assemble()` calls (unchanged signature, new type)

Note: the `spawnStore` and `spawnMetadataStore` were already rewired in Step 4d.

### 22a. Run tests

```bash
go test ./internal/workspace/ensure/ -v -count=1
```

### 22b. Commit

Commit per the project's commit workflow with message: `refactor(ensure): use autolearn.InstructionStore`

---

### Step 23: Delete old packages

Remove:

- `internal/lore/` (entire directory)
- `internal/emergence/` (entire directory)
- `internal/dashboard/handlers_lore.go`
- `internal/dashboard/handlers_lore_disabled.go`
- `internal/dashboard/handlers_lore_test.go`
- `internal/dashboard/handlers_emergence.go`

**Clean up `internal/api/contracts/emergence.go`**:

- Remove `SkillProposal` type (lines 71-82) — it is orphaned after `emergence/skillfile.go` and `emergence/curator.go` are deleted. Nothing else references it.
- Keep all spawn-related types: `SpawnEntryType`, `SpawnEntrySource`, `SpawnEntryState`, `SpawnEntry`, `EmergenceMetadata`, `SpawnEntriesResponse`, `CreateSpawnEntryRequest`, `UpdateSpawnEntryRequest`, `PromptHistoryEntry`, `PromptHistoryResponse`.
- Consider renaming the file to `contracts/spawn.go` for clarity (optional but recommended).

Remove stale schema labels from `internal/schema/schema.go`:

- Delete `LabelLoreCurator` and `LabelEmergenceCurator`
- Keep `LabelAutolearnFriction` and `LabelAutolearnIntent` (added in Steps 9-10)

Update `internal/dashboard/handlers_features.go`:

- Remove `import "internal/lore"` (line 13)
- Add `import "internal/autolearn"`
- Replace `Lore: lore.IsAvailable()` to `Autolearn: autolearn.IsAvailable()` (line 37)

Update any remaining test files:

- `internal/schmuxdir/integration_test.go` — update `lore` imports
- `internal/oneshot/schema_integration_test.go` — update `lore`/`emergence` schema labels

**Update CI/docs references to `nolore` build tag**:

- `.claude/commands/commit.md` — lines 119-120 reference `nolore` in the build tag table, and line 146 has `go build -tags nolore`. Replace with `noautolearn`.
- `docs/dev/experimental-features.md` — lines 55, 63, 66, 69 reference `nolore`. Replace with `noautolearn`.
- `docs/api.md` — line 85 references `nolore`. Replace with `noautolearn`.

### 22a. Full build + test

```bash
go build ./... && go test ./... -count=1
go build -tags noautolearn ./...
```

### 22b. Commit

Commit per the project's commit workflow with message: `refactor: delete internal/lore and internal/emergence`

---

## Group 8: Features contract, frontend

### Step 24: Update features contract and generated types

**File**: `internal/api/contracts/features.go`

Replace `Lore bool` with `Autolearn bool` (line 15).

Then regenerate TypeScript types:

```bash
go run ./cmd/gen-types
```

This regenerates `assets/dashboard/src/lib/types.generated.ts` with the new `Autolearn` feature flag and any updated contract types.

### 24a. Verify

Check the generated file has `autolearn: boolean` in the `Features` type and no more `lore: boolean`.

### 24b. Commit

Commit per the project's commit workflow with message: `chore: regenerate TypeScript types for autolearn`

---

### Step 25: Update frontend

All 31 affected files are listed below, organized into sub-steps by functional area.

#### Step 25a: Rename core files

Rename (git mv) and update internal references:

- `assets/dashboard/src/routes/LorePage.tsx` to `AutolearnPage.tsx`
- `assets/dashboard/src/routes/LorePage.test.tsx` to `AutolearnPage.test.tsx`
- `assets/dashboard/src/routes/config/LoreConfig.tsx` to `AutolearnConfig.tsx`
- `assets/dashboard/src/components/LoreCard.tsx` to `AutolearnCard.tsx`
- `assets/dashboard/src/components/LoreCard.module.css` to `AutolearnCard.module.css`
- `assets/dashboard/src/lib/emergence-api.ts` to `autolearn-api.ts`
- `assets/dashboard/src/styles/lore.module.css` to `autolearn.module.css` (if exists)

Update all internal imports, component names, and CSS module references within these files.

#### Step 25b: Update API endpoints

**File**: `assets/dashboard/src/lib/autolearn-api.ts` (renamed in 25a)

- Update all endpoint paths from `/emergence/` to `/spawn/` (for spawn entry CRUD)
- Update all endpoint paths from `/lore/` to `/autolearn/` (for learning pipeline)
- Rename exported functions from `lore*`/`emergence*` to `autolearn*`/`spawn*`

**File**: `assets/dashboard/src/lib/api.ts`

- Update any `lore`/`emergence` API references

#### Step 25c: Update routing and navigation

**File**: `assets/dashboard/src/App.tsx`

- Update route from `/lore` to `/autolearn`
- Update lazy import from `LorePage` to `AutolearnPage`

**File**: `assets/dashboard/src/components/AppShell.tsx`

- Update lore badge counts (lines 212, 241-242) to use autolearn naming
- Update navigation link from `/lore` to `/autolearn`

#### Step 25d: Update components that import renamed modules

**File**: `assets/dashboard/src/components/ActionDropdown.tsx`

- Update imports from `emergence-api` to `autolearn-api`

**File**: `assets/dashboard/src/components/ActionDropdown.test.tsx`

- Update imports

**File**: `assets/dashboard/src/components/CreateActionForm.tsx`

- Update imports from `emergence-api` to `autolearn-api`

**File**: `assets/dashboard/src/components/CreateActionForm.test.tsx`

- Update imports

**File**: `assets/dashboard/src/components/ToolsSection.tsx`

- Update feature flag check from `lore` to `autolearn`

**File**: `assets/dashboard/src/components/ToolsSection.test.tsx`

- Update feature flag references

#### Step 25e: Update config and context files

**File**: `assets/dashboard/src/routes/config/experimentalRegistry.ts`

- Replace lore entry with autolearn entry (change `enabledKey: 'loreEnabled'` to `'autolearnEnabled'`, `buildFeatureKey: 'lore'` to `'autolearn'`)

**File**: `assets/dashboard/src/routes/config/useConfigForm.ts`

- Update lore config field references

**File**: `assets/dashboard/src/routes/config/buildConfigUpdate.ts`

- Update lore config field references

**File**: `assets/dashboard/src/routes/config/ConfigPage.test.tsx`

- Update test references

**File**: `assets/dashboard/src/routes/config/ExperimentalTab.test.tsx`

- Update test references

**File**: `assets/dashboard/src/routes/ConfigPage.tsx`

- Update references to LoreConfig component

**File**: `assets/dashboard/src/contexts/FeaturesContext.tsx`

- Update feature flag from `lore` to `autolearn`

**File**: `assets/dashboard/src/contexts/ConfigContext.tsx`

- Update config key references

#### Step 25f: Update remaining files

**File**: `assets/dashboard/src/routes/SpawnPage.tsx`

- Update any emergence/lore references

**File**: `assets/dashboard/src/routes/SpawnPage.agent-select.test.tsx`

- Update imports/references

**File**: `assets/dashboard/src/routes/tips-page/power-tools-tab.tsx`

- Update lore references

**File**: `assets/dashboard/src/routes/tips-page/prompts-tab.tsx`

- Update lore references

**File**: `assets/dashboard/src/routes/HomePage.dashboardsx.test.tsx`

- Update lore/emergence references

**File**: `assets/dashboard/src/lib/ansiStrip.test.ts`

- Update any lore-related test data

**File**: `assets/dashboard/src/demo/mockTransport.test.ts`

- Update mock data references

**WebSocket event listener**:
In `AutolearnPage.tsx` (formerly `LorePage.tsx`), update the WebSocket event listener that checks for `lore_merge_complete` (line 187) to check for `autolearn_merge_complete` instead.

### 25-build. Build dashboard

```bash
go run ./cmd/build-dashboard
```

### 25-test. Run tests

```bash
./test.sh --quick
```

### 25-commit. Commit

Commit per the project's commit workflow with message: `feat(dashboard): rename lore/emergence UI to autolearn`

---

## Group 8: End-to-end verification

### Step 26: Full verification

### 26a. Full test suite

```bash
./test.sh
```

### 26b. Build with all tags

```bash
go build ./cmd/schmux/
go build -tags noautolearn ./cmd/schmux/
```

### 26c. Verify old packages are gone

```bash
# Should find zero results (excluding test files and docs)
grep -r 'internal/lore' --include='*.go' . | grep -v vendor/ || echo "CLEAN"
grep -r 'internal/emergence' --include='*.go' . | grep -v vendor/ || echo "CLEAN"
```

### 26d. Verify no stale imports

```bash
go vet ./...
```

### 26e. Update docs

- Update `docs/lore.md` to `docs/autolearn.md` (or delete and note that `docs/specs/autolearn.md` is the reference)
- Delete `docs/emergence.md`
- Update `docs/api.md` — replace all `/api/lore/` and `/api/emergence/` references with `/api/autolearn/` and `/api/spawn/`. Also update the `nolore` build tag reference (line 85) to `noautolearn`.
- Update `CLAUDE.md` — replace lore/emergence references in the architecture diagram

**Note**: `docs/api.md` should ideally be updated incrementally alongside handler changes (Steps 16-17) since CI enforces that API-related package changes include api.md updates. If CI is not blocking during development (feature branch), batch the update here. If CI is blocking, update `docs/api.md` in Step 16f alongside the handler commit.

### 26f. Final commit

Commit per the project's commit workflow with message: `docs: update all references from lore/emergence to autolearn`
