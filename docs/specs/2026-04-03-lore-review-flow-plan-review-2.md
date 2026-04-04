VERDICT: NEEDS_REVISION

## Summary Assessment

The revised plan addressed most of the 7 critical issues from Review 1 but introduced new problems. The most significant: Step 6 uses a fabricated `updateDraft({...draft, lore: ...})` API that does not exist in this codebase (the config form uses a `dispatch`/`setField` pattern), and the plan omits the full plumbing chain required to add a new config field (API contracts, gen-types, useConfigForm state/snapshot/hasChanges). Step 3's implementation references a `result.CommitSHA` struct field that does not exist (results are `[]map[string]string`).

## Previous Issues Status

### 1. `handleLoreApplyMerge` SourceEntries spread (Critical) -- ADDRESSED

Step 1e now explicitly shows the broken code at `handlers_lore.go:576-579` and provides the concrete fix: mapping `RuleSourceEntry` to text keys by type. The mapping logic (failure -> InputSummary, default -> Text) is specified.

### 2. `internal/daemon/daemon.go:1215` not listed as breakage (Critical) -- ADDRESSED

Step 1e now explicitly calls out this file and explains that both `ExtractedRule.SourceEntries` and `Rule.SourceEntries` change to `[]RuleSourceEntry` together, so the assignment compiles. It instructs to verify the file explicitly and run its tests.

### 3. `AutoCommit` added to wrong struct (Critical) -- ADDRESSED

Step 3c now correctly adds `AutoCommit` to the anonymous `body` struct at line 442, not to `mergeApplyRequest`. The plan specifies the exact struct modification.

### 4. SpawnEntry mock missing required fields (Critical) -- ADDRESSED

Step 8a now includes `type: 'skill'`, `description`, and a complete `metadata` object with all required `EmergenceMetadata` fields (`skill_name`, `confidence`, `evidence`, `evidence_count`, `emerged_at`, `last_curated`).

### 5. Step 2 test passes immediately / not real TDD (Critical) -- PARTIALLY ADDRESSED

The test in Step 2a still tests the same thing (JSON round-trip of a string field), which will pass as soon as the field is added. The plan does not restructure this into a meaningful TDD cycle. However, this is a minor concern for a simple string field -- the real risk was always the more complex steps. Accepting as-is.

### 6. Step 5 backward compatibility (Critical) -- ADDRESSED

The plan now includes a note at the top of the Dependency Groups section: "Step 5 adds an optional `autoCommit` param (default `false`) to `applyLoreMerge`. Existing callers in the old LorePage continue to work until Step 12 replaces them. Do not change the default to `true`."

### 7. Step 15d used `--quick` as final verification (Critical) -- ADDRESSED

Step 15e now runs `./test.sh` (not `--quick`). Step 16a also runs `./test.sh`.

## Critical Issues (must fix) -- NEW issues only

### 1. Step 6 uses a fabricated `updateDraft` API that does not exist

The plan's Step 6a shows:

```tsx
onChange={(e) => updateDraft({
  ...draft,
  lore: { ...draft.lore, public_rule_mode: e.target.value }
})}
```

This is not how the config form works. `AdvancedTab` receives a `dispatch` prop and uses `setField(field, value)` calls, which dispatch `{ type: 'SET_FIELD', field, value }` actions. There is no `updateDraft` function and no `draft` object with a nested `lore` property. The actual pattern in the file is:

```tsx
onChange={(e) => setField('lorePublicRuleMode', e.target.value)}
```

Adding the field requires changes to multiple files, none of which are mentioned:

1. `internal/api/contracts/config.go` -- add `PublicRuleMode` to both `Lore` and `LoreUpdate` structs
2. `go run ./cmd/gen-types` -- regenerate `types.generated.ts` to include the new field
3. `internal/dashboard/handlers_config.go` -- add read/write mapping between API contracts and `config.LoreConfig`
4. `assets/dashboard/src/routes/config/useConfigForm.ts` -- add `lorePublicRuleMode` to `ConfigFormState`, `ConfigSnapshot`, defaults, `hasChanges`, and `snapshotConfig`
5. `assets/dashboard/src/routes/config/AdvancedTab.tsx` -- add prop + UI (the only file Step 6 covers, and even that is wrong)

Missing any of these files means the config field silently does nothing -- the UI renders but the value is never persisted or read.

### 2. Step 3c references `result.CommitSHA` but results are `[]map[string]string`

The plan's Step 3c implementation says:

```go
result.CommitSHA = getHeadSHA(ws.Path)
```

But the handler's `results` variable is `[]map[string]string` (line 454), not a struct with a `CommitSHA` field. The actual pattern for adding data to the result is:

```go
results = append(results, map[string]string{
    "layer":        m.Layer,
    "status":       "applied",
    "workspace_id": ws.ID,
    "commit_sha":   getHeadSHA(ws.Path),
})
```

Alternatively, the results type could be changed to a proper struct, but that would require updating the JSON encoding at line 588 and the TypeScript response type. The plan needs to specify which approach to take and ensure the types are consistent end-to-end. As written, the Go code will not compile.

### 3. Step 15 has duplicate sub-step labels (15e appears twice)

Lines 1157-1159 define Step 15e as "Run full test suite" (`./test.sh`). Lines 1165-1167 define another Step 15e as "Build and verify" (`go run ./cmd/build-dashboard && go build ./cmd/schmux`). The second should be labeled 15f. This is a task-ordering ambiguity that will cause confusion during execution.

### 4. Step 3c calls `getHeadSHA` which does not exist

The plan references `getHeadSHA(ws.Path)` but this function does not exist in the codebase. Grep for `getHeadSHA` returns zero results. The plan needs to either define this helper (e.g., `exec.CommandOutput("git", "-C", ws.Path, "rev-parse", "HEAD")`) or use an existing git helper from `internal/workspace/`.

## Suggestions (nice to have)

### 1. Sidebar badge counts proposals, not individual rules

The plan's Step 15b says "the badge logic already counts pending proposals + proposed actions across all repos" and notes the count is per-proposal, not per-rule. The design switches to a card-per-rule model, so showing "3 pending proposals" when there are actually "12 pending rules" is misleading. The plan hedges ("If the badge should show the number of pending cards...") but does not commit to a decision. Since the design explicitly says "The sidebar badge only lights up when there are actually pending cards across any repo," the badge should count individual pending rules + proposed actions, not proposals.

### 2. Step 12 sub-steps improved but 12c is still large

The previous review asked for Step 12 to be split, and the plan now has sub-steps 12a-12g. However, Step 12c ("Write implementation -- data loading") replaces the entire data loading layer: removing `activeRepo` state, repo tab logic, sub-tab logic, adding cross-repo fan-out, flattening rules and actions into a unified cards array, and sorting by date. This is still a large chunk of work for a single sub-step with no test verification between 12c and 12d.

### 3. Step 13/14 test mocks are underspecified

Steps 13a and 14a show test skeletons with comments like `// Mock 3 pending rules` and `// Assert: "Approve All" button visible` but no actual code. Every other step provides concrete test code. These steps are the most architecturally complex (persistence phases, diff viewers, polling for merge completion), and vague test descriptions increase the risk of the implementer writing insufficient tests.

### 4. The `auto_pr` config field may conflict with `public_rule_mode`

The existing `LoreConfig.AutoPR` field already controls whether to auto-create PRs after applying proposals. The new `PublicRuleMode` field adds a `"create_pr"` option. The plan does not address the interaction or deprecation of `AutoPR` in favor of `PublicRuleMode`. If both are present, the behavior is ambiguous -- which one wins? The plan should explicitly state whether `AutoPR` is deprecated or how the two fields interact.

### 5. Step 16c references scenario files that do not exist

The plan says to update `configure-lore-settings.md` and `persist-lore-curator-model.md`. The first exists at `test/scenarios/configure-lore-settings.md`. The second only exists as a generated spec file at `test/scenarios/generated/persist-lore-curator-model.spec.ts` -- there is no `persist-lore-curator-model.md` source file. The plan should reference the correct file paths.

## Verified Claims (things you confirmed are correct)

1. **Step 1e breakage list is now accurate** -- All four `SourceEntries` usage sites (`proposals.go:42`, `curator.go:28`, `daemon.go:1215`, `handlers_lore.go:579`) are accounted for, plus `handlers_lore.go:905` (in `finalizeCuration`). The grep confirms no other usage exists.

2. **Step 3c anonymous body struct placement is correct** -- The `var body struct` at line 442 is the right place to add `AutoCommit`, not `mergeApplyRequest`.

3. **Step 5 TypeScript types are manual, not generated** -- `LoreRule` and `LoreMergeApplyResult` are in `types.ts` (manual), not `types.generated.ts`. No `go run ./cmd/gen-types` needed for Step 5.

4. **Step 8 SpawnEntry mock now includes all required fields** -- `EmergenceMetadata` requires `skill_name`, `confidence`, `evidence_count`, `emerged_at`, `last_curated`. All are present in the revised mock.

5. **`MarkEntriesDirect` uses timestamps, not SourceEntries** -- Confirmed at `scratchpad.go:263`. The curation flow marks entries by timestamp independent of `SourceEntries`, so the data model change does not break entry marking.

6. **`react-diff-viewer-continued` is already a dependency** -- Confirmed in `package.json`.

7. **`finalizeCuration` is in `handlers_lore.go`**, not `daemon.go` -- The plan's Step 1e reference to `daemon.go:1215` is a different code path (the one inside the curation goroutine). Both are accounted for.

8. **Backward compatibility note for Step 5 is present** -- The note at the top of the Dependency Groups section correctly documents the constraint.

9. **Test commands use correct patterns** -- All Go test commands use `go test ./internal/... -count=1`. Frontend tests use `./test.sh --quick` (intermediate) and `./test.sh` (final). Build uses `go run ./cmd/build-dashboard`. No `npm`/`npx` invocations.

10. **Scenario test `lore-page-repo-tabs.md` exists and will need a complete rewrite** -- Its assertions (tab bar visible, tab switching, active tab styling) are all being removed by this design.
