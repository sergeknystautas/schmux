# Plan: Autolearn — Unified Learning System

**Goal**: Replace `internal/lore/` and `internal/emergence/` with a single `internal/autolearn/` package and extract spawn entries into `internal/spawn/`. One data model, one API surface, one build tag.

**Architecture**: Spec at `docs/specs/autolearn.md`. Key decisions: two LLM calls per curation run (friction + intent), unified `Batch`/`Learning` data model, `internal/spawn/` for always-compiled spawn entry CRUD, `noautolearn` build tag for the learning pipeline.

**Tech Stack**: Go 1.22+, chi router, TypeScript/React (Vite), Vitest

## Dependency groups

| Group | Steps | Can Parallelize | Notes                                                           |
| ----- | ----- | --------------- | --------------------------------------------------------------- |
| 1     | 1–4   | Yes             | `internal/spawn/` package (pure extraction, no new logic)       |
| 2     | 5–8   | Yes             | `internal/autolearn/` data model + store (independent of spawn) |
| 3     | 9–12  | Yes             | Curators + signals (depends on data model from Group 2)         |
| 4     | 13–16 | Yes             | Apply + merge + pending merge (depends on data model)           |
| 5     | 17–20 | No              | Handlers + API (depends on everything above)                    |
| 6     | 21–23 | No              | Daemon wiring + delete old packages (integration)               |
| 7     | 24–26 | No              | Config, features contract, frontend                             |
| 8     | 27    | No              | End-to-end verification                                         |

---

## Group 1: Extract `internal/spawn/`

### Step 1: Create `internal/spawn/store.go`

**File**: `internal/spawn/store.go` (new)

Copy `internal/emergence/store.go` into `internal/spawn/store.go`. Change `package emergence` → `package spawn`. Update the import path for `internal/api/contracts` (unchanged, contracts still live there). No logic changes — this is a pure move.

The file contains: `Store` struct, `NewStore()`, `List()`, `ListAll()`, `Get()`, `Create()`, `Update()`, `Delete()`, `Pin()`, `Dismiss()`, `RecordUse()`, `AddProposed()`, `ProposedAndPinnedNames()`, `GenerateID()`, `generateID()`.

### 1a. Write test

**File**: `internal/spawn/store_test.go` (new)

Copy `internal/emergence/store_test.go` → `internal/spawn/store_test.go`. Change package to `spawn`. Update imports from `internal/emergence` → `internal/spawn`. Run to confirm all tests pass.

### 1b. Run test

```bash
go test ./internal/spawn/ -v -run TestStore
```

### 1c. Commit

```bash
git add internal/spawn/
git commit -m "refactor(spawn): extract spawn entry store from emergence"
```

---

### Step 2: Create `internal/spawn/metadata.go`

**File**: `internal/spawn/metadata.go` (new)

Copy `internal/emergence/metadata.go` → `internal/spawn/metadata.go`. Change package to `spawn`. Update import for `internal/api/contracts`.

The file contains: `MetadataStore` struct, `NewMetadataStore()`, `Save()`, `Get()`, `Delete()`.

### 2a. Write test

**File**: `internal/spawn/metadata_test.go` (new)

Copy `internal/emergence/metadata_test.go`. Change package + imports.

### 2b. Run test

```bash
go test ./internal/spawn/ -v -run TestMetadata
```

### 2c. Commit

```bash
git add internal/spawn/
git commit -m "refactor(spawn): extract metadata store from emergence"
```

---

### Step 3: Create `internal/spawn/migrate.go`

**File**: `internal/spawn/migrate.go` (new)

Copy `internal/emergence/migrate.go` → `internal/spawn/migrate.go`. Change package to `spawn`. The `MigrateFromActions()` function takes a `*Store` — the type now lives in the same package, so the import is internal.

### 3a. Write test

**File**: `internal/spawn/migrate_test.go` (new)

Copy `internal/emergence/migrate_test.go`. Change package + imports.

### 3b. Run test

```bash
go test ./internal/spawn/ -v -run TestMigrate
```

### 3c. Commit

```bash
git add internal/spawn/
git commit -m "refactor(spawn): extract actions migration from emergence"
```

---

### Step 4: Create `handlers_spawn.go` and rewire imports

**File**: `internal/dashboard/handlers_spawn.go` (new)

Extract these handlers from `internal/dashboard/handlers_emergence.go` into `handlers_spawn.go`:

- `extractSkillDescription()`
- `validateEmergenceRepo()` → rename to `validateSpawnRepo()`
- `handleListSpawnEntries()`
- `handleListAllSpawnEntries()`
- `handleCreateSpawnEntry()`
- `handleUpdateSpawnEntry()`
- `handleDeleteSpawnEntry()`
- `handlePinSpawnEntry()`
- `handleDismissSpawnEntry()`
- `handleRecordSpawnEntryUse()`
- `handlePromptHistory()`

No build tag on this file — spawn entries are always compiled.

Update `internal/dashboard/server.go`:

- Replace `emergenceStore *emergence.Store` → `spawnStore *spawn.Store` (line 243)
- Replace `emergenceMetadataStore *emergence.MetadataStore` → `spawnMetadataStore *spawn.MetadataStore` (line 244)
- Replace `SetEmergenceStore()` → `SetSpawnStore()` (line 436)
- Replace `SetEmergenceMetadataStore()` → `SetSpawnMetadataStore()` (line 441)
- Update import from `internal/emergence` → `internal/spawn`
- Update route group (line 811): change path from `/emergence/{repo}` to `/spawn/{repo}`, change middleware from `validateEmergenceRepo` to `validateSpawnRepo`
- Remove the emergence curate route (`r.Post("/curate", s.handleEmergenceCurate)`) — this moves to autolearn handlers later

Update `internal/workspace/ensure/manager.go`:

- Replace `import "internal/emergence"` → `import "internal/spawn"` (line 13)
- Replace `emergenceStore *emergence.Store` → `spawnStore *spawn.Store` (line 26)
- Replace `emergenceMetadataStore *emergence.MetadataStore` → `spawnMetadataStore *spawn.MetadataStore` (line 29)
- Replace `SetEmergenceStores()` → `SetSpawnStores()` (line 45)

Update `internal/daemon/daemon.go`:

- Replace `import "internal/emergence"` → `import "internal/spawn"` (line 32)
- Replace `emergence.NewStore()` → `spawn.NewStore()` (line 1086)
- Replace `emergence.NewMetadataStore()` → `spawn.NewMetadataStore()` (line 1087)
- Replace `server.SetEmergenceStore()` → `server.SetSpawnStore()` (line 1088)
- Replace `server.SetEmergenceMetadataStore()` → `server.SetSpawnMetadataStore()` (line 1089)
- Replace `ensure.SetEmergenceStores()` → `ensure.SetSpawnStores()` (line 1090)
- Replace `emergence.MigrateFromActions()` → `spawn.MigrateFromActions()` (line 1099)

Update `internal/dashboard/handlers_features.go`:

- Remove `import "internal/emergence"` (not needed after spawn extraction)

Update all handlers in `handlers_spawn.go` to reference `s.spawnStore` and `s.spawnMetadataStore` instead of `s.emergenceStore` and `s.emergenceMetadataStore`.

### 4a. Write test

Update `internal/workspace/ensure/manager_test.go` — change imports from `internal/emergence` → `internal/spawn`.

### 4b. Run tests

```bash
go build ./... && go test ./internal/dashboard/... ./internal/workspace/ensure/... ./internal/spawn/... -v -count=1
```

### 4c. Commit

```bash
git add internal/spawn/ internal/dashboard/handlers_spawn.go internal/dashboard/server.go internal/daemon/daemon.go internal/workspace/ensure/ internal/dashboard/handlers_features.go
git commit -m "refactor(spawn): rewire all imports from emergence to spawn"
```

---

## Group 2: `internal/autolearn/` data model + store

### Step 5: Create `internal/autolearn/learning.go`

**File**: `internal/autolearn/learning.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Define all types from the spec: `LearningKind`, `LearningStatus`, `Layer`, `Learning`, `RuleDetails`, `SkillDetails`, `SourceRef`, `BatchStatus`, `Batch`, `LearningUpdate`, `PendingMergeStatus`. Include `EffectiveLayer()` on `Learning` and `AllResolved()` on `Batch`.

### 5a. Write test

**File**: `internal/autolearn/learning_test.go`

Test `EffectiveLayer()` (nil ChosenLayer returns SuggestedLayer; set ChosenLayer returns it). Test `AllResolved()` (all approved = true, one pending = false, mix of approved+dismissed = true).

### 5b. Run test

```bash
go test ./internal/autolearn/ -v -run TestLearning
```

### 5c. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): define Learning, Batch, and Layer types"
```

---

### Step 6: Create `internal/autolearn/store.go`

**File**: `internal/autolearn/store.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Implement `BatchStore` with all methods from the spec: `NewBatchStore()`, `Save()`, `Get()`, `List()`, `UpdateStatus()`, `UpdateLearning()`, `GetLearning()`, `PendingLearningTitles()`, `DismissedLearningTitles()`, `DismissedLearnings()`.

Storage: one JSON file per batch at `{baseDir}/batches/{repo}/{batchID}.json`. Use mutex + temp-file + rename pattern from `internal/lore/proposals.go` (or `internal/spawn/store.go`).

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

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): implement BatchStore with JSON persistence"
```

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

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): implement learning history queries"
```

---

### Step 8: Create `internal/autolearn/disabled.go`

**File**: `internal/autolearn/disabled.go` (new)

```go
//go:build noautolearn

package autolearn
```

Stub every exported type, function, and method from `learning.go`, `store.go`, `history.go` with no-op implementations. Follow the exact pattern of `internal/lore/disabled.go`. Include `IsAvailable() bool { return false }`.

Add `IsAvailable() bool { return true }` to `store.go` (in the real build).

### 8a. Verify both builds compile

```bash
go build ./internal/autolearn/
go build -tags noautolearn ./internal/autolearn/
```

### 8b. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): add noautolearn build tag stubs"
```

---

## Group 3: Curators + signals

### Step 9: Create `internal/autolearn/signals.go`

**File**: `internal/autolearn/signals.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port two functions:

1. `ReadFrictionSignals()` — adapted from `internal/lore/scratchpad.go`'s `ReadEntriesFromEvents()` and `ReadEntries()`. Keep the `Entry` type and `EntryFilter` type here. Keep `FilterRaw()`, `FilterByParams()`, `MarkEntriesDirect()`, `MarkEntriesByTextFromEntries()`, `PruneEntries()`.
2. `CollectIntentSignals()` — port directly from `internal/emergence/history.go` (it already exists and is fully implemented). Returns `[]IntentSignal`.

Also port `LoreStatePath()` → rename to `StatePath()`, and `LoreStateDir()` → `StateDir()`.

### 9a. Write test

**File**: `internal/autolearn/signals_test.go`

Port relevant tests from `internal/lore/scratchpad_test.go` and `internal/emergence/history_test.go`.

### 9b. Run test

```bash
go test ./internal/autolearn/ -v -run TestSignals
```

### 9c. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): unified signal scanner (friction + intent)"
```

---

### Step 10: Create `internal/autolearn/curator_friction.go`

**File**: `internal/autolearn/curator_friction.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Adapt from `internal/lore/curator.go`:

- `BuildFrictionPrompt(entries []Entry, existingTitles []string, dismissedTitles []string) string` — same logic as `BuildExtractionPrompt()` but the output schema produces `[]Learning` (with `kind`, `title`, `category`, `suggested_layer`, `sources`, and optional `skill` sub-object for cross-kind).
- `ParseFrictionResponse(response string) (*FrictionCuratorResponse, error)` — same fallback chain (direct JSON → strip fences → extract outermost object).
- `FrictionCuratorResponse` struct: `Learnings []Learning`, `DiscardedEntries []string`.

Register the schema label `schema.LabelAutolearnFriction` (add to `internal/schema/schema.go`).

### 10a. Write test

**File**: `internal/autolearn/curator_friction_test.go`

Port from `internal/lore/curator_test.go`. Update to test cross-kind output (a response containing both rule and skill learnings).

### 10b. Run test

```bash
go test ./internal/autolearn/ -v -run TestFrictionCurator
```

### 10c. Commit

```bash
git add internal/autolearn/ internal/schema/
git commit -m "feat(autolearn): friction curator with cross-kind learning output"
```

---

### Step 11: Create `internal/autolearn/curator_intent.go`

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

### 11a. Write test

**File**: `internal/autolearn/curator_intent_test.go`

Port from `internal/emergence/curator_test.go`. Test cross-kind (intent curator producing a rule).

### 11b. Run test

```bash
go test ./internal/autolearn/ -v -run TestIntentCurator
```

### 11c. Commit

```bash
git add internal/autolearn/ internal/schema/
git commit -m "feat(autolearn): intent curator with cross-kind learning output"
```

---

### Step 12: Create `internal/autolearn/skillfile.go`

**File**: `internal/autolearn/skillfile.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port `internal/emergence/skillfile.go` → `GenerateSkillFile(learning Learning) string`. Takes a `Learning` (must be `KindSkill`) and renders the SKILL.md with YAML frontmatter from `learning.Skill.*` fields.

### 12a. Write test

**File**: `internal/autolearn/skillfile_test.go`

Port from `internal/emergence/skillfile_test.go`.

### 12b. Run test

```bash
go test ./internal/autolearn/ -v -run TestSkillFile
```

### 12c. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): skill file renderer"
```

---

## Group 4: Apply + merge + pending merge

### Step 13: Create `internal/autolearn/instructions.go`

**File**: `internal/autolearn/instructions.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port `internal/lore/instructions.go` directly. Same `InstructionStore` struct with `Read()`, `Write()`, `Assemble()`. Change base dir default to `~/.schmux/autolearn/instructions/`.

### 13a. Write test

**File**: `internal/autolearn/instructions_test.go`

Port from `internal/lore/instructions_test.go`.

### 13b. Run test

```bash
go test ./internal/autolearn/ -v -run TestInstruction
```

### 13c. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): instruction file storage"
```

---

### Step 14: Create `internal/autolearn/merge_curator.go`

**File**: `internal/autolearn/merge_curator.go` (new)

Port `internal/lore/merge_curator.go`. Keep `BuildMergePrompt()`, `ParseMergeResponse()`, `MergeResponse` struct. The merge prompt takes `[]Learning` (filtered to rules only) instead of `[]Rule` — format each as `[Category] Title`.

Also include `ReadFileFromRepo()` (reads a file from HEAD in a git repo).

### 14a. Write test

**File**: `internal/autolearn/merge_curator_test.go`

Port from `internal/lore/merge_curator_test.go`. Update to use `Learning` instead of `Rule`.

### 14b. Run test

```bash
go test ./internal/autolearn/ -v -run TestMergeCurator
```

### 14c. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): merge curator for public-layer rules"
```

---

### Step 15: Create `internal/autolearn/pending_merge.go`

**File**: `internal/autolearn/pending_merge.go` (new)

Port `internal/lore/pending_merge.go`. Update struct to use `LearningIDs`/`BatchIDs` instead of `RuleIDs`/`ProposalIDs`. Add `SkillFiles map[string]string` field. Keep `PendingMergeStore` with `Save()`, `Get()`, `Delete()`, `UpdateEditedContent()`, `InvalidateIfContainsLearning()` (renamed from `InvalidateIfContainsRule()`).

### 15a. Write test

**File**: `internal/autolearn/pending_merge_test.go`

Port from `internal/lore/pending_merge_test.go`. Add test for `SkillFiles` round-trip.

### 15b. Run test

```bash
go test ./internal/autolearn/ -v -run TestPendingMerge
```

### 15c. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): pending merge store with skill files support"
```

---

### Step 16: Create `internal/autolearn/apply.go`

**File**: `internal/autolearn/apply.go` (new)

```go
//go:build !noautolearn

package autolearn
```

Port `internal/lore/apply.go`. Expand `ApplyToLayer()` to handle both kinds:

- `KindRule` + private layer → write to instruction file (existing logic)
- `KindRule` + public layer → return error (must go through merge/push flow)
- `KindSkill` + private layer → call adapter `InjectSkill()` on all workspaces
- `KindSkill` + public layer → return error (must go through push flow)

Also add `NormalizeLearningTitle()` (from `NormalizeRuleText()`) and `DeduplicateLearnings()` (from `DeduplicateRules()`).

### 16a. Write test

**File**: `internal/autolearn/apply_test.go`

Port from `internal/lore/apply_test.go`. Add test for skill private-layer apply.

### 16b. Run test

```bash
go test ./internal/autolearn/ -v -run TestApply
```

### 16c. Commit

```bash
git add internal/autolearn/
git commit -m "feat(autolearn): apply routing for kind × layer matrix"
```

---

## Group 5: Handlers + API

### Step 17: Create `handlers_autolearn.go`

**File**: `internal/dashboard/handlers_autolearn.go` (new)

```go
//go:build !noautolearn

package dashboard
```

Consolidate handlers from `handlers_lore.go` and the curation handler from `handlers_emergence.go`. Key changes:

**Server fields** (update in `server.go`):

- Replace `loreStore *lore.ProposalStore` → `autolearnStore *autolearn.BatchStore`
- Replace `loreInstructionStore *lore.InstructionStore` → `autolearnInstructionStore *autolearn.InstructionStore`
- Replace `lorePendingMergeStore *lore.PendingMergeStore` → `autolearnPendingMergeStore *autolearn.PendingMergeStore`
- Replace `loreExecutor` → `autolearnExecutor` (same signature)
- Add setter methods: `SetAutolearnStore()`, `SetAutolearnInstructionStore()`, `SetAutolearnPendingMergeStore()`, `SetAutolearnExecutor()`

**Handler mapping** (old → new):

- `handleLoreStatus` → `handleAutolearnStatus`
- `handleLoreProposals` → `handleAutolearnBatches`
- `handleLoreProposalGet` → `handleAutolearnBatchGet`
- `handleLoreDismiss` → `handleAutolearnBatchDismiss`
- `handleLoreRuleUpdate` → `handleAutolearnLearningUpdate`
- `handleLoreEntries` → `handleAutolearnEntries`
- `handleLoreEntriesClear` → `handleAutolearnEntriesClear`
- `handleLoreCurate` → `handleAutolearnCurate` (runs both friction + intent curators)
- `handleLoreUnifiedMerge` → `handleAutolearnMerge`
- `handleLorePendingMerge*` → `handleAutolearnPendingMerge*`
- `handleLorePush` → `handleAutolearnPush` (extended for `SkillFiles`)
- `handleEmergenceCurate` → absorbed into `handleAutolearnCurate`
- New: `handleAutolearnForget` — reverses an applied learning
- New: `handleAutolearnHistory` — filterable learning history across all batches

**Route registration** (update in `server.go`, replace lines 788–808):

```go
r.Route("/autolearn/{repo}", func(r chi.Router) {
    r.Use(validateAutolearnRepo)
    r.Get("/status", s.handleAutolearnStatus)
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
    r.Get("/prompt-history", s.handleAutolearnPromptHistory)
    r.Get("/history", s.handleAutolearnHistory)
    r.Get("/curations", s.handleAutolearnCurationsList)
    r.Get("/curations/active", s.handleAutolearnCurationsActive)
    r.Get("/curations/{curationID}/log", s.handleAutolearnCurationLog)
})
```

### 17a. Verify build

```bash
go build ./internal/dashboard/...
```

### 17b. Commit

```bash
git add internal/dashboard/
git commit -m "feat(autolearn): unified API handlers"
```

---

### Step 18: Create `handlers_autolearn_disabled.go`

**File**: `internal/dashboard/handlers_autolearn_disabled.go` (new)

```go
//go:build noautolearn

package dashboard
```

Stub every handler method and the `validateAutolearnRepo` middleware. Each returns 503 "Autolearn is not available in this build". Follow the exact pattern of `internal/lore/disabled.go` / `handlers_lore_disabled.go`.

Also stub: `refreshAutolearnExecutor()`, `autolearnWorkspace` struct, `getAutolearnWorkspaces()`, any helper types used in handler signatures.

### 18a. Verify both builds

```bash
go build ./internal/dashboard/...
go build -tags noautolearn ./internal/dashboard/...
```

### 18b. Commit

```bash
git add internal/dashboard/
git commit -m "feat(autolearn): disabled stubs for noautolearn build tag"
```

---

### Step 19: Update `disabled.go` with all new stubs

**File**: `internal/autolearn/disabled.go`

After all types and functions are finalized in Steps 5–16, update `disabled.go` to stub everything. This includes signal types (`Entry`, `EntryFilter`, `IntentSignal`), curator types, `InstructionStore`, `PendingMerge`, `PendingMergeStore`, and all package-level functions (`SetLogger`, `ParseEntry`, `FilterRaw`, etc.).

### 19a. Verify

```bash
go build -tags noautolearn ./...
```

### 19b. Commit

```bash
git add internal/autolearn/disabled.go
git commit -m "feat(autolearn): complete disabled.go stubs"
```

---

### Step 20: Port handler tests

**File**: `internal/dashboard/handlers_autolearn_test.go` (new)

Port from `internal/dashboard/handlers_lore_test.go`. Update all types (`Proposal` → `Batch`, `Rule` → `Learning`, etc.), endpoint paths (`/lore/` → `/autolearn/`), and field names.

### 20a. Run tests

```bash
go test ./internal/dashboard/ -v -run TestAutolearn -count=1
```

### 20b. Commit

```bash
git add internal/dashboard/
git commit -m "test(autolearn): port handler tests from lore"
```

---

## Group 6: Daemon wiring + cleanup

### Step 21: Rewire daemon

**File**: `internal/daemon/daemon.go`

Replace the lore wiring block (lines 1112–1292) with autolearn wiring:

1. Replace `import "internal/lore"` → `import "internal/autolearn"` (line 37)
2. Replace `loreLog := logging.Sub(logger, "lore")` → `autolearnLog := logging.Sub(logger, "autolearn")` (line 322)
3. Replace `lore.SetLogger(loreLog)` → `autolearn.SetLogger(autolearnLog)` (line 332)
4. Replace instruction store wiring (line 498): `autolearn.NewInstructionStore(filepath.Join(schmuxDir, "autolearn", "instructions"))`
5. Replace `ensure.SetInstructionStore()` call to use new autolearn type
6. Replace the `if cfg.GetLoreEnabled()` block (lines 1117–1292):
   - Use `cfg.GetAutolearnEnabled()` (new config method)
   - Create `autolearn.NewBatchStore(filepath.Join(schmuxDir, "autolearn", "batches"), autolearnLog)`
   - Create `autolearn.NewPendingMergeStore(filepath.Join(schmuxDir, "autolearn", "pending-merges"), autolearnLog)`
   - Wire executor using `cfg.GetAutolearnTarget()`
   - Replace on-dispose callback to call unified curation (both friction + intent)
   - Replace `server.TriggerEmergenceCuration()` → integrated into unified trigger

### 21a. Verify build

```bash
go build ./cmd/schmux/
```

### 21b. Commit

```bash
git add internal/daemon/
git commit -m "feat(autolearn): rewire daemon from lore+emergence to autolearn"
```

---

### Step 22: Update `ensure/manager.go`

**File**: `internal/workspace/ensure/manager.go`

1. Replace `import "internal/lore"` → `import "internal/autolearn"` (line 14)
2. Replace `instrStore *lore.InstructionStore` → `instrStore *autolearn.InstructionStore` (line 23)
3. Replace `SetInstructionStore(s *lore.InstructionStore)` → `SetInstructionStore(s *autolearn.InstructionStore)` (line 40)
4. Update all `instrStore.Assemble()` calls (unchanged signature, new type)

Note: the `spawnStore` and `spawnMetadataStore` were already rewired in Step 4.

### 22a. Run tests

```bash
go test ./internal/workspace/ensure/ -v -count=1
```

### 22b. Commit

```bash
git add internal/workspace/ensure/
git commit -m "refactor(ensure): use autolearn.InstructionStore"
```

---

### Step 23: Delete old packages

Remove:

- `internal/lore/` (entire directory)
- `internal/emergence/` (entire directory)
- `internal/dashboard/handlers_lore.go`
- `internal/dashboard/handlers_lore_disabled.go`
- `internal/dashboard/handlers_lore_test.go`
- `internal/dashboard/handlers_emergence.go`

Remove stale schema labels from `internal/schema/schema.go`:

- Delete `LabelLoreCurator` and `LabelEmergenceCurator`
- Keep `LabelAutolearnFriction` and `LabelAutolearnIntent` (added in Steps 10–11)

Update `internal/dashboard/handlers_features.go`:

- Remove `import "internal/lore"` (line 13)
- Add `import "internal/autolearn"`
- Replace `Lore: lore.IsAvailable()` → `Autolearn: autolearn.IsAvailable()` (line 37)

Update any remaining test files:

- `internal/schmuxdir/integration_test.go` — grep for `lore` imports and update
- `internal/oneshot/schema_integration_test.go` — grep for `lore`/`emergence` schema labels and update

### 23a. Full build + test

```bash
go build ./... && go test ./... -count=1
go build -tags noautolearn ./...
```

### 23b. Commit

```bash
git add -A
git commit -m "refactor: delete internal/lore and internal/emergence"
```

---

## Group 7: Config, features contract, frontend

### Step 24: Update config

**File**: `internal/config/config.go`

1. Add `Autolearn *AutolearnConfig` field to `Config` struct (alongside existing `Lore`)
2. Define `AutolearnConfig` with all fields from spec (same as `LoreConfig` minus `AutoPR`):
   ```go
   type AutolearnConfig struct {
       Enabled          *bool   `json:"enabled,omitempty"`
       CurateOnDispose  string  `json:"curate_on_dispose,omitempty"`
       CurateDebounceMs int     `json:"curate_debounce_ms,omitempty"`
       Target           string  `json:"llm_target,omitempty"`
       InstructionFiles []string `json:"instruction_files,omitempty"`
       PublicRuleMode   string  `json:"public_rule_mode,omitempty"`
       PruneAfterDays   int     `json:"prune_after_days,omitempty"`
   }
   ```
3. Add config alias logic: if `Autolearn` is nil but `Lore` is non-nil, copy `Lore` → `Autolearn` on load
4. Add accessor methods: `GetAutolearnEnabled()`, `GetAutolearnTarget()`, `GetAutolearnTargetRaw()`, `GetAutolearnCurateOnDispose()`, `GetAutolearnDebounceMs()`, `GetAutolearnPruneAfterDays()`, `GetAutolearnInstructionFiles()`, `GetAutolearnPublicRuleMode()`
5. Config save: always write `autolearn`, never write `lore`

**File**: `internal/api/contracts/config.go`

Update contract types if the config tab reads from them.

**File**: `internal/api/contracts/features.go`

Replace `Lore bool` → `Autolearn bool` (line 15).

### 24a. Write test

**File**: `internal/config/config_test.go` (add test cases)

Test: loading config with `lore` key populates `Autolearn`. Test: saving config writes `autolearn` not `lore`.

### 24b. Run test

```bash
go test ./internal/config/ -v -run TestAutolearn -count=1
```

### 24c. Commit

```bash
git add internal/config/ internal/api/contracts/
git commit -m "feat(config): add autolearn config with lore key alias"
```

---

### Step 25: Update generated types

```bash
go run ./cmd/gen-types
```

This regenerates `assets/dashboard/src/lib/types.generated.ts` with the new `Autolearn` feature flag and any updated contract types.

### 25a. Verify

Check the generated file has `autolearn: boolean` in the `Features` type and no more `lore: boolean`.

### 25b. Commit

```bash
git add assets/dashboard/src/lib/types.generated.ts
git commit -m "chore: regenerate TypeScript types for autolearn"
```

---

### Step 26: Update frontend

**Files** to update:

- `assets/dashboard/src/routes/config/experimentalRegistry.ts` — replace lore entry with autolearn entry
- `assets/dashboard/src/routes/config/LoreConfig.tsx` → rename to `AutolearnConfig.tsx`, update props
- `assets/dashboard/src/routes/LorePage.tsx` → rename to `AutolearnPage.tsx`, update imports
- `assets/dashboard/src/routes/LorePage.test.tsx` → rename to `AutolearnPage.test.tsx`
- `assets/dashboard/src/lib/emergence-api.ts` → rename to `autolearn-api.ts`, update endpoints from `/emergence/` to `/autolearn/` and `/spawn/`
- `assets/dashboard/src/App.tsx` — update route from `/lore` to `/autolearn`
- `assets/dashboard/src/components/ActionDropdown.tsx` — update imports from `emergence-api` → `autolearn-api`
- `assets/dashboard/src/components/CreateActionForm.tsx` — update imports
- `assets/dashboard/src/components/ToolsSection.tsx` — update feature flag check from `lore` to `autolearn`
- `assets/dashboard/src/styles/lore.module.css` → rename to `autolearn.module.css`
- Update any test files importing renamed modules

### 26a. Build dashboard

```bash
go run ./cmd/build-dashboard
```

### 26b. Run tests

```bash
./test.sh --quick
```

### 26c. Commit

```bash
git add assets/dashboard/
git commit -m "feat(dashboard): rename lore/emergence UI to autolearn"
```

---

## Group 8: End-to-end verification

### Step 27: Full verification

### 27a. Full test suite

```bash
./test.sh
```

### 27b. Build with all tags

```bash
go build ./cmd/schmux/
go build -tags noautolearn ./cmd/schmux/
```

### 27c. Verify old packages are gone

```bash
# Should find zero results
grep -r 'internal/lore' --include='*.go' . | grep -v '_test.go' | grep -v vendor/ || echo "CLEAN"
grep -r 'internal/emergence' --include='*.go' . | grep -v _test.go | grep -v vendor/ || echo "CLEAN"
```

### 27d. Verify no stale imports

```bash
go vet ./...
```

### 27e. Update docs

- Update `docs/lore.md` → `docs/autolearn.md` (or delete and note that `docs/specs/autolearn.md` is the reference)
- Delete `docs/emergence.md`
- Update `docs/api.md` — replace all `/api/lore/` and `/api/emergence/` references with `/api/autolearn/` and `/api/spawn/`
- Update `CLAUDE.md` — replace lore/emergence references in the architecture diagram

### 27f. Final commit

```bash
git add -A
git commit -m "docs: update all references from lore/emergence to autolearn"
```
