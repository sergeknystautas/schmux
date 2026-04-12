VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is well-structured and demonstrates thorough understanding of the codebase, but has several incorrect line number references, misses multiple files that need updating, omits critical wiring details (session manager callback, config handler refresh, WebSocket events), and underestimates the frontend scope (29 files touch lore/emergence, not the ~11 listed).

## Critical Issues (must fix)

### 1. Line number references in Step 4 are wrong for server.go

The plan says:

- `emergenceStore` is at line 243, `emergenceMetadataStore` at line 244: **Correct.**
- `SetEmergenceStore()` at line 436, `SetEmergenceMetadataStore()` at line 441: **Correct.**
- Route group at line 811: **Correct** (emergence route block starts at line 811).
- `loreStore` field is at line 243 (plan says to replace it): **Wrong.** `loreStore` is at line 212, not 243. Line 243 is `emergenceStore`.

### 2. Line number references in Step 4 are wrong for `ensure/manager.go`

The plan says:

- `import "internal/emergence"` at line 13: **Correct.**
- `emergenceStore` field at line 26: **Correct.**
- `emergenceMetadataStore` at line 29: **Correct.**
- `SetEmergenceStores()` at line 45: **Correct.**
- `import "internal/lore"` at line 14 (implied by Step 22 `line 14`): Actually at line 14, **correct**.
- `instrStore *lore.InstructionStore` at line 23 (Step 22): **Correct.**
- `SetInstructionStore` at line 40 (Step 22 says `line 40`): Actually at line 39-41, close enough.

### 3. Line number references in Step 21 are wrong for `daemon.go`

The plan says:

- `import "internal/emergence"` at line 32: **Correct.**
- `import "internal/lore"` at line 37: **Correct.**
- `loreLog` at line 322: **Correct.**
- `lore.SetLogger(loreLog)` at line 332: **Correct.**
- Instruction store wiring at line 498: **Wrong.** Line 498 has `dashboardsx.StartAutoRenewal`, not instruction store wiring. The instruction store wiring is at line 1125 (`loreInstructionsDir := filepath.Join(schmuxDir, "instructions")`).
- `emergence.NewStore()` at line 1086, `emergence.NewMetadataStore()` at line 1087: **Correct.**
- `server.SetEmergenceStore()` at line 1088, `server.SetEmergenceMetadataStore()` at line 1089: **Correct.**
- `ensure.SetEmergenceStores()` at line 1090: **Correct.**
- `emergence.MigrateFromActions()` at line 1099: **Correct.**
- Lore wiring block at lines 1112-1292: The block starts at line 1112. The plan says it ends at 1292, but the actual lore block extends to at least line 1298 (the `TriggerEmergenceCuration` call at line 1297 is inside the lore callback closure, and the mutex unlock is at line 1299). **Off by ~7 lines.**

### 4. Missing: `session/manager.go` callback rename

The plan never mentions `internal/session/manager.go`. The session manager has:

- `loreCallback` field (line 56)
- `SetLoreCallback()` method (line 188)
- Callback invocation (line 1467-1473)

These need to be renamed (e.g., `SetAutolearnCallback` / `autolearnCallback`), or at minimum documented as an intentional keep. The daemon's `sm.SetLoreCallback()` call at line 1150 also needs updating. The plan's Step 21 lists "Replace the `if cfg.GetLoreEnabled()` block" but does not mention renaming `sm.SetLoreCallback()` specifically. This is a compile error waiting to happen.

### 5. Missing: `handlers_config.go` reference to `refreshLoreExecutor`

`internal/dashboard/handlers_config.go` line 822 calls `s.refreshLoreExecutor(cfg)`. This method is defined in `handlers_lore.go` (line 1318-1321) and stubbed in `handlers_lore_disabled.go` (line 23). When `handlers_lore.go` is deleted (Step 23) and replaced by `handlers_autolearn.go`, the new file must define `refreshAutolearnExecutor` (or equivalent), and `handlers_config.go` must be updated to call it. The plan never mentions `handlers_config.go` as a file to update.

### 6. Missing: WebSocket event type rename

`handlers_lore.go` broadcasts `lore_merge_complete` WebSocket events (lines 1011, 1024, 1041). The frontend likely listens for this event type. The plan does not mention updating the WebSocket event name to something like `autolearn_merge_complete`, nor does it list the frontend WebSocket listener as a file to update.

### 7. Missing: `handleLoreApplyMerge` handler

The plan's Step 17 maps lore handlers to autolearn handlers. It lists `handleLoreUnifiedMerge` -> `handleAutolearnMerge` but completely omits `handleLoreApplyMerge` (defined at line 314 of `handlers_lore.go`, routed at server.go line 797). The route registration in the plan's Step 17 does not have an equivalent for `POST /proposals/{proposalID}/apply-merge`. Either this handler needs to be ported to the new API surface or explicitly deprecated with a note about why.

### 8. Missing: `handleLoreCurationsActive` route placement

The route `GET /lore/curations/active` is registered _outside_ the `r.Route("/lore/{repo}")` block (at server.go line 790, before the route group). It takes no `{repo}` parameter. The plan's Step 17 route registration puts `curations/active` _inside_ the `r.Route("/autolearn/{repo}")` block, which changes the API semantics: it would require a repo parameter that the current endpoint does not need. This is either intentional (in which case the spec should note it) or a bug.

### 9. Frontend scope is significantly underestimated

The plan's Step 26 lists ~11 frontend files. Actual grep shows **29 files** reference `lore` or `emergence-api`. Missing from the plan:

- `assets/dashboard/src/components/LoreCard.tsx` (the card component itself)
- `assets/dashboard/src/components/AppShell.tsx` (lore badge counts, lines 212, 241-242)
- `assets/dashboard/src/contexts/FeaturesContext.tsx`
- `assets/dashboard/src/contexts/ConfigContext.tsx`
- `assets/dashboard/src/routes/config/useConfigForm.ts`
- `assets/dashboard/src/routes/config/buildConfigUpdate.ts`
- `assets/dashboard/src/routes/config/ConfigPage.test.tsx`
- `assets/dashboard/src/routes/config/ExperimentalTab.test.tsx`
- `assets/dashboard/src/routes/ConfigPage.tsx`
- `assets/dashboard/src/routes/SpawnPage.tsx`
- `assets/dashboard/src/routes/SpawnPage.agent-select.test.tsx`
- `assets/dashboard/src/routes/tips-page/power-tools-tab.tsx`
- `assets/dashboard/src/routes/tips-page/prompts-tab.tsx`
- `assets/dashboard/src/routes/HomePage.dashboardsx.test.tsx`
- `assets/dashboard/src/lib/api.ts`
- `assets/dashboard/src/lib/ansiStrip.test.ts`
- `assets/dashboard/src/demo/mockTransport.test.ts`
- `assets/dashboard/src/lib/types.generated.ts` (handled by Step 25, but the manual check in 25a is incomplete)

This makes Step 26 far larger than a "2-5 minute" task. It should be broken into multiple sub-steps.

### 10. Missing: `ReadFileFromRepo` in `merge_curator.go` has wrong source

Step 14 says to include `ReadFileFromRepo()` in the autolearn merge*curator.go, but states it's from `internal/lore/merge_curator.go`. It is actually defined in `internal/lore/curator.go` (line 192), not `merge_curator.go`. The function signature is `func ReadFileFromRepo(ctx context.Context, repoDir, relPath string) (string, error)` -- it takes 3 args, but the disabled stub at `disabled.go:204` takes only 2 (`*, \_ string`). The plan should reference the correct source file.

### 11. `GenerateSkillFile` signature mismatch

Step 12 says to port `GenerateSkillFile` to take `Learning` instead of `SkillProposal`. The current signature is `func GenerateSkillFile(proposal contracts.SkillProposal) string` (in `emergence/skillfile.go`). This is fine conceptually, but the plan does not mention that `contracts.SkillProposal` (in `internal/api/contracts/emergence.go`) will become orphaned when the emergence package is deleted. Should `SkillProposal` be removed from contracts, or does anything else reference it? The plan's Step 23 deletes `internal/emergence/` but does not mention cleaning up `internal/api/contracts/emergence.go`. The spawn entry types (`SpawnEntry`, `EmergenceMetadata`, etc.) are still needed by `internal/spawn/`, so the contracts file cannot be deleted wholesale.

### 12. `CollectPromptHistory` missing from spawn extraction

`handlers_emergence.go` line 296 calls `emergence.CollectPromptHistory()`. This function lives in `internal/emergence/history.go`. When the spawn handlers are extracted to `handlers_spawn.go` (Step 4), the `handlePromptHistory` handler references `emergence.CollectPromptHistory`. But Step 4 copies this handler to `handlers_spawn.go` without noting that `CollectPromptHistory` must also be moved to `internal/spawn/` or the handler won't compile. The function is currently in the same file as `CollectIntentSignals` (emergence/history.go), but `CollectIntentSignals` is slated to move to `internal/autolearn/signals.go` (Step 9), not `internal/spawn/`. The plan needs to explicitly state where `CollectPromptHistory` lands.

## Suggestions (nice to have)

### 1. Steps are not all "2-5 minutes"

Step 4 (create `handlers_spawn.go` and rewire imports) touches 6 files across 4 packages, renames fields and methods, extracts ~11 handler functions into a new file, and updates imports everywhere. This is easily 15-30 minutes of work, not 2-5 minutes. Step 17 (create `handlers_autolearn.go`) is even larger. Step 26 (frontend updates across 29 files) is a multi-hour task. Consider breaking these into smaller steps.

### 2. The plan commits after each step with `git commit` directly

The project's CLAUDE.md says "ALWAYS use `/commit` to create commits. NEVER run `git commit` directly." Every step's commit command violates this rule. The commit commands should use `/commit` or at minimum note that the `/commit` pre-checks must be satisfied.

### 3. `docs/api.md` update is deferred to the very end

The CI enforces that API-related package changes include api.md updates (`scripts/check-api-docs.sh`). Deferring all docs/api.md changes to Step 27e means every intermediate step that touches `internal/dashboard/` may fail CI. Consider updating api.md incrementally alongside handler changes.

### 4. Step 8 `disabled.go` will be incomplete and need Step 19

Step 8 creates `disabled.go` for the types defined in Steps 5-7 only. Step 19 is then needed to add stubs for everything from Steps 9-16. This two-pass approach on the same file is fragile and could leave the `noautolearn` build broken between Groups 2 and 5. Consider creating `disabled.go` once, after all types are defined, or maintaining a running stub file.

### 5. No migration path for `~/.schmux/emergence/` spawn entry data

The plan creates `internal/spawn/store.go` that presumably uses the same on-disk format. But `NewStore()` in the current code takes a `baseDir` and the daemon passes `filepath.Join(schmuxDir, "emergence")`. After the rename, if the daemon passes a new base dir like `filepath.Join(schmuxDir, "spawn")`, existing spawn entry data at `~/.schmux/emergence/` would be lost. The plan's Step 4 does not specify the new base dir path. If it stays `emergence/`, that should be stated explicitly. If it changes, a migration is needed.

### 6. The spec mentions a `LearningUpdate` type but the plan never defines it

The spec's `BatchStore.UpdateLearning()` takes a `LearningUpdate` parameter. Step 6 mentions it in the method list but never defines the struct. It should be defined in `learning.go` (Step 5).

### 7. No explicit handling for the `nolore` -> `noautolearn` build tag transition

Existing build scripts, CI configs, or user documentation that reference `-tags nolore` will silently stop working (nolore would become a no-op since nothing uses it after the old package is deleted). The plan should mention updating any CI config or Makefile that uses `nolore`.

## Verified Claims (things you confirmed are correct)

1. **`handlers_emergence.go` has no build tag** -- confirmed, the file has `package dashboard` with no `//go:build` directive. This supports the plan's assertion that spawn entry handlers should have no build tag.
2. **`handlers_lore.go` uses `//go:build !nolore`** -- confirmed at line 1.
3. **`handlers_lore_disabled.go` exists** -- confirmed at `internal/dashboard/handlers_lore_disabled.go` with `//go:build nolore`.
4. **`emergence/store.go` has the `Store` struct and `NewStore()`** -- confirmed at lines 18, 25.
5. **`emergence/history.go` has `CollectIntentSignals`** -- confirmed at line 99.
6. **`emergence/skillfile.go` has `GenerateSkillFile(proposal contracts.SkillProposal) string`** -- confirmed at line 11.
7. **`lore/proposals.go` has the `ProposalStore` type** -- confirmed (imported as `lore.ProposalStore` throughout).
8. **`lore/scratchpad.go` has `Entry`, `ReadEntries`, `ReadEntriesFromEvents`, `FilterRaw`, `FilterByParams`, `MarkEntriesDirect`, `MarkEntriesByTextFromEntries`, `PruneEntries`, `LoreStatePath`, `LoreStateDir`** -- all confirmed.
9. **`lore/apply.go` has `ApplyToLayer(store *InstructionStore, layer Layer, repo, content string) error`** -- confirmed at line 9.
10. **`lore/merge_curator.go` has `BuildMergePrompt(currentContent string, rules []Rule) string`** -- confirmed at line 23.
11. **`config.go` has `LoreConfig` struct at line 401 and `Lore *LoreConfig` field at line 83** -- confirmed.
12. **`contracts/features.go` has `Lore bool` at line 15** -- confirmed.
13. **`schema.go` has both `LabelLoreCurator` and `LabelEmergenceCurator`** -- confirmed at lines 20 and 22.
14. **`experimentalRegistry.ts` has the lore entry** -- confirmed at lines 38-44, with `enabledKey: 'loreEnabled'` and `buildFeatureKey: 'lore'`.
15. **`handlers_features.go` imports `internal/lore` and calls `lore.IsAvailable()` at line 37** -- confirmed.
16. **All files listed for deletion exist**: `internal/lore/` (15 files), `internal/emergence/` (12 files), `handlers_lore.go`, `handlers_lore_disabled.go`, `handlers_lore_test.go`, `handlers_emergence.go` -- all confirmed.
17. **`ensure/manager_test.go` imports `internal/lore`** -- confirmed.
18. **`schmuxdir/integration_test.go` imports `internal/lore`** -- confirmed.
19. **`oneshot/schema_integration_test.go` imports `internal/lore` (blank import)** -- confirmed.
20. **`docs/lore.md` and `docs/emergence.md` exist** -- confirmed.
21. **`docs/api.md` has extensive lore and emergence API references** -- confirmed (49 matches).
22. **The plan correctly identifies that `emergence/history.go` has `CollectPromptHistory` (line 24)** -- confirmed.
23. **`server.go` fields: `loreStore` at line 212, `loreExecutor` at line 215, `loreInstructionStore` at line 218, `lorePendingMergeStore` at line 221** -- confirmed.
24. **Daemon wiring: the lore callback at line 1150 triggers `server.TriggerEmergenceCuration(repoName)` at line 1297** -- confirmed, this integration point is noted in the plan.
25. **Frontend file `lore.module.css` exists** -- confirmed.
26. **`emergence-api.ts` exists as a single frontend API module** -- confirmed.
