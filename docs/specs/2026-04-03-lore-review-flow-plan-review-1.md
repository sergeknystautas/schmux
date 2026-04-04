VERDICT: NEEDS_REVISION

## Summary Assessment

The plan covers the core design requirements but has several critical issues: a breaking data model change that silently corrupts a production code path (the `SourceEntries` spread in `handleLoreApplyMerge`), a missing file in the breakage list (`internal/daemon/daemon.go`), structurally incorrect request body parsing in Step 3, and Step 8's test uses a `SpawnEntry` mock that does not match the generated type's required fields.

## Critical Issues (must fix)

### 1. `handleLoreApplyMerge` SourceEntries spread will break at compile time but plan says "fix any remaining compilation errors" with no specifics

At `internal/dashboard/handlers_lore.go:576-579`, the existing code does:

```go
var sourceKeys []string
for _, rule := range proposal.Rules {
    if rule.Status == lore.RuleApproved {
        sourceKeys = append(sourceKeys, rule.SourceEntries...)
    }
}
```

This spreads `SourceEntries` (currently `[]string`) into `sourceKeys []string`. After the type change to `[]RuleSourceEntry`, this will fail to compile. The plan mentions this file in Step 1e's breakage list but only says "any code that reads SourceEntries as strings" -- it does not describe _what_ the fix should be. This is the most complex breakage because `sourceKeys` is passed to `lore.MarkEntriesByTextFromEntries` which expects `[]string` text keys. The fix requires extracting text keys from the structured entries (e.g., mapping `RuleSourceEntry.Text` or `RuleSourceEntry.InputSummary` depending on type), and the plan needs to specify this mapping explicitly. Getting this wrong breaks the entry-marking pipeline silently at runtime even if it compiles.

### 2. `internal/daemon/daemon.go:1215` is not listed as a breakage site

The plan's Step 1e lists three files that will break: `curator.go`, `handlers_lore.go`, and `scratchpad.go`. But `internal/daemon/daemon.go:1215` also assigns `er.SourceEntries` (from `ExtractedRule`) to `lore.Rule.SourceEntries`. After changing `ExtractedRule.SourceEntries` to `[]RuleSourceEntry` (Step 1e), this line in `daemon.go` will compile fine since both sides change -- but the plan should explicitly verify this file, because it's a second code path that builds proposals from extracted rules (separate from `finalizeCuration` in `handlers_lore.go`). Missing it means the implementer may not test it.

### 3. Step 3 request body structure is wrong -- `mergeApplyRequest` vs the wrapping `body` struct

The plan says to add `AutoCommit bool` to the `mergeApplyRequest` struct (line 422). But `mergeApplyRequest` is the _per-layer merge_ struct, not the request body. The actual request body is an anonymous struct at line 442-444:

```go
var body struct {
    Merges []mergeApplyRequest `json:"merges"`
}
```

The `auto_commit` field must be added to this anonymous body struct, not to `mergeApplyRequest`. If added to `mergeApplyRequest`, each individual merge would have its own auto_commit flag, which is semantically wrong (auto-commit applies to the entire request, not per-layer).

### 4. Step 8 test mock `SpawnEntry` is missing required fields

The test creates a `SpawnEntry` mock missing `type`, which is a required field in the generated type (`type: string`). It also sets `state: 'proposed'` and `source: 'emerged'` but omits `description`, and the `metadata` object is missing required fields like `skill_name`, `emerged_at`, and `last_curated` (all required in `EmergenceMetadata`). The test will fail with TypeScript errors before it even runs. The mock should be:

```typescript
const mockAction: SpawnEntry = {
  id: 'a1',
  name: 'Fix lint errors',
  type: 'skill',
  prompt: './format.sh && ./test.sh --quick',
  state: 'proposed',
  source: 'emerged',
  use_count: 0,
  metadata: {
    skill_name: 'fix-lint',
    confidence: 0.8,
    evidence: ['ran format 4 times'],
    evidence_count: 4,
    emerged_at: '2026-04-01T00:00:00Z',
    last_curated: '2026-04-01T00:00:00Z',
  },
};
```

### 5. Step 2 test will pass immediately (not a failing test)

The test in Step 2a creates a `Config` with `Lore: &LoreConfig{PublicRuleMode: "create_pr"}` and marshals/unmarshals it. Since `LoreConfig` already supports arbitrary JSON fields via standard Go struct marshaling, adding a new string field is just adding a struct tag. The test will _already pass_ as soon as the field is added -- there's no intermediate failing state. The "write failing test" step is misleading because adding the field to the struct and writing the test are the same action. This isn't a real TDD cycle.

### 6. Step 5 API client changes break existing callers without migration

The plan changes `applyLoreMerge` to accept a new `autoCommit` parameter (with default `false`). But the existing caller in `LorePage.tsx:255` calls `applyLoreMerge(repoName, proposal.id, merges)`. The plan says Step 12 will rewrite LorePage, but Steps 5 and 12 are in different dependency groups (Group 3 vs Group 5). Between Steps 5 and 12, the existing LorePage code must still work -- and it will (since `autoCommit` defaults to `false`), but the plan should explicitly note this backward compatibility constraint. If someone changes the default to `true` during implementation, all existing callers break.

### 7. Step 15d uses `./test.sh --quick` as the final verification

The plan's last test step (15d) runs `./test.sh --quick`. Per CLAUDE.md: "`--quick` skips typecheck and other critical validation. Code that passes `--quick` can still be broken." Step 16a does run `./test.sh`, but 15d is labeled as the cleanup step's verification, not an intermediate check. The plan should use `./test.sh` at Step 15d, not `--quick`.

## Suggestions (nice to have)

### 1. Step 12 is too large for "2-5 minutes"

Step 12 rewrites the entire LorePage: removes three component types (LegacyProposalCard, RuleReviewCard, RuleRow), adds fan-out data loading across repos, implements a flat card wall with six different callbacks, adds empty state and disabled state handling. This is realistically 30-60 minutes of work, not 2-5 minutes. Consider splitting into sub-steps: (12a) data loading, (12b) card wall rendering, (12c) callbacks, (12d) empty/disabled states.

### 2. Step 14 references `ReactDiffViewer` but doesn't add it as an import

The plan says "ReactDiffViewer is rendered" but doesn't specify the import. The component already exists in the codebase (used in `LorePage.tsx`, `GitCommitPage.tsx`, `DiffPage.tsx`) via the `react-diff-viewer-continued` package, but the plan should note the import path.

### 3. Steps 10-11 (animations) have no testable assertions

Steps 10 and 11 add CSS animations and say "verify visually" with `go run ./cmd/build-dashboard`. This is not testable in the TDD sense. Consider adding at least a test that the `dismissing` CSS class is applied when dismiss is triggered, even if the animation itself cannot be verified programmatically.

### 4. Sidebar badge logic claim in Step 15b should be verified

The plan says "The badge logic already counts pending proposals + proposed actions across all repos (lines 53-77). No change needed." I confirmed this is correct (ToolsSection.tsx aggregates across repos), but the current badge counts _proposals_, not individual _rules_. The design switches from proposal-level to rule-level cards. If the badge should show individual pending card count rather than pending proposal count, this needs updating.

### 5. Scenario test `lore-page-repo-tabs.md` assertions will all fail

The plan mentions updating this file in Step 16c but doesn't specify the new content. Since the entire scenario (tab bar visibility, tab switching, active tab styling) is being removed, this file needs a complete rewrite, not an update. The plan should specify the new scenario assertions (flat card wall, no tabs, cards from all repos in one view).

### 6. Plan does not mention updating `docs/api.md`

CLAUDE.md states: "Changes to API-related packages (`internal/dashboard/`, ...) must include a corresponding update to `docs/api.md`." Step 3 modifies `handlers_lore.go` to add an `auto_commit` field to the apply-merge request body. The plan should include a step to update `docs/api.md` with the new request field.

## Verified Claims (things you confirmed are correct)

1. **`SourceEntries` is `[]string` at `proposals.go:42`** -- Confirmed. The field is `SourceEntries []string json:"source_entries"`.

2. **`ExtractedRule.SourceEntries` is `[]string` at `curator.go:28`** -- Confirmed.

3. **Extraction prompt at `curator.go:66` asks for timestamps** -- Confirmed. The prompt schema shows `"source_entries": ["<timestamp or entry key that led to this rule>"]`.

4. **`mergeApplyRequest` is at line 422** -- Confirmed. It's a per-layer struct with `Layer` and `Content` fields.

5. **Request body is an anonymous struct at line 442** -- Confirmed. It wraps `[]mergeApplyRequest` in a `Merges` field.

6. **`LoreConfig` struct is at line 387** -- Confirmed. It has `Enabled`, `Target`, `AutoPR`, `CurateOnDispose`, `CurateDebounceMs`, `PruneAfterDays`, `InstructionFiles` fields but no `PublicRuleMode`.

7. **TypeScript `LoreRule.source_entries` is `string[]` at types.ts:505** -- Confirmed. This is a manually-defined type (not generated), so no `go run ./cmd/gen-types` step is needed.

8. **`LoreMergeApplyResult` exists at types.ts:536** -- Confirmed. Does not currently have `commit_sha`.

9. **`applyLoreMerge` function at api.ts:1011** -- Confirmed. Currently takes 3 params (repoName, proposalID, merges).

10. **AdvancedTab.tsx lore section exists around line 106-178** -- Confirmed. Contains Lore enabled checkbox, LLM target, curate on dispose, and auto PR.

11. **ToolsSection.tsx badge aggregation at lines 53-77** -- Confirmed. Already aggregates across all repos.

12. **`LoreCard.tsx` does not exist yet** -- Confirmed via glob.

13. **`LorePage.test.tsx` does not exist yet** -- Confirmed via glob.

14. **`lore-page-repo-tabs.md` scenario exists** -- Confirmed at `test/scenarios/lore-page-repo-tabs.md`.

15. **`react-diff-viewer-continued` is already a dependency** -- Confirmed in package.json and used in multiple files.

16. **`useDevStatus` hook exists** -- Confirmed at `assets/dashboard/src/hooks/useDevStatus.ts`.

17. **`MarkEntriesDirect` at `scratchpad.go:263` uses timestamps, not SourceEntries** -- Confirmed. The curation flow already uses `MarkEntriesDirect(entries, ...)` with the full entry list's timestamps, independent of `SourceEntries`. The plan's Step 4e concern about this is valid but the code already handles it correctly.

18. **`startLoreMerge` API function exists at api.ts:1000** -- Confirmed.

19. **`pinSpawnEntry` and `dismissSpawnEntry` exist** -- Confirmed at api.ts:1264 and 1275.

20. **`LegacyProposalCard`, `RuleReviewCard`, `RuleRow` all exist in LorePage.tsx** -- Confirmed. They are local components within the file, not separate files.
