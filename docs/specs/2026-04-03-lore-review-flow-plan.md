# Plan: Lore Review Flow Redesign

**Goal**: Replace the current repo-tabbed, proposal-grouped lore page with a flat card wall of agent learnings, and replace the manual workspace-based public persistence flow with inline diff + one-click commit.

**Design**: `docs/specs/2026-04-03-lore-review-flow-design.md`

**Architecture**: Backend data model gains embedded source entries on rules. Public rule persistence reuses workspace infrastructure but automates commit+push. Frontend becomes a flat card wall with two card types (instruction/action), triage-then-persist flow.

**Tech Stack**: Go (backend), React + TypeScript (frontend), CSS Modules (styling), Vitest (frontend tests), Go testing (backend tests)

## Dependency Groups

| Group | Steps               | Can Parallelize           | Notes                                      |
| ----- | ------------------- | ------------------------- | ------------------------------------------ |
| 1     | Steps 1-3           | Yes                       | Backend data model + config changes        |
| 2     | Step 4              | No (depends on 1)         | Extraction prompt update                   |
| 3     | Steps 5-6           | Yes (depends on 2)        | TypeScript types + config UI               |
| 4     | Steps 7-11          | Yes (depends on 6)        | Frontend card components                   |
| 5     | Steps 12-12g, 13-14 | Sequential (depends on 4) | Page rewrite (sub-steps), persistence flow |
| 6     | Steps 15-16         | No (depends on 5)         | Cleanup, api docs, integration test        |

**Note on backward compatibility:** Step 5 adds an optional `autoCommit` param (default `false`) to `applyLoreMerge`. Existing callers in the old LorePage continue to work until Step 12 replaces them. Do not change the default to `true`.

---

## Step 1: Add RuleSourceEntry struct to Go data model

**Files**: `internal/lore/proposals.go`, `internal/lore/curator.go`

### 1a. Write failing test

**File**: `internal/lore/proposals_test.go`

Add a test that creates a Rule with structured source entries and verifies JSON round-trip:

```go
func TestRuleSourceEntryJSON(t *testing.T) {
	rule := Rule{
		ID:       "r1",
		Text:     "Always run tests from root",
		Category: "testing",
		SuggestedLayer: LayerRepoPrivate,
		Status:   RulePending,
		SourceEntries: []RuleSourceEntry{
			{Type: "failure", InputSummary: "cd sub && go test", ErrorSummary: "module not found"},
			{Type: "reflection", Text: "tests must run from root"},
		},
	}
	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Rule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.SourceEntries) != 2 {
		t.Fatalf("expected 2 source entries, got %d", len(decoded.SourceEntries))
	}
	if decoded.SourceEntries[0].Type != "failure" {
		t.Errorf("expected type failure, got %s", decoded.SourceEntries[0].Type)
	}
	if decoded.SourceEntries[1].Text != "tests must run from root" {
		t.Errorf("expected reflection text, got %s", decoded.SourceEntries[1].Text)
	}
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/lore/ -run TestRuleSourceEntryJSON -count=1
```

### 1c. Write implementation

**File**: `internal/lore/proposals.go`

Add the `RuleSourceEntry` struct before the `Rule` struct (around line 33):

```go
// RuleSourceEntry holds displayable data from a raw friction signal.
type RuleSourceEntry struct {
	Type         string `json:"type"`                    // "failure", "reflection", "friction"
	Text         string `json:"text,omitempty"`           // reflection/friction text
	InputSummary string `json:"input_summary,omitempty"`  // for failures: what was attempted
	ErrorSummary string `json:"error_summary,omitempty"`  // for failures: what went wrong
	Tool         string `json:"tool,omitempty"`
}
```

Change the `SourceEntries` field on `Rule` (line 42) from `[]string` to `[]RuleSourceEntry`:

```go
SourceEntries  []RuleSourceEntry `json:"source_entries"`
```

### 1d. Run test to verify it passes

```bash
go test ./internal/lore/ -run TestRuleSourceEntryJSON -count=1
```

### 1e. Fix compilation errors

Changing `SourceEntries` from `[]string` to `[]RuleSourceEntry` will break these files:

**1. `internal/lore/curator.go:24-29`** — Update `ExtractedRule.SourceEntries` to match:

```go
type ExtractedRule struct {
	Text           string            `json:"text"`
	Category       string            `json:"category"`
	SuggestedLayer string            `json:"suggested_layer"`
	SourceEntries  []RuleSourceEntry `json:"source_entries"`
}
```

**2. `internal/dashboard/handlers_lore.go:576-579`** — The existing code spreads `SourceEntries` into a `[]string` for `MarkEntriesByTextFromEntries`:

```go
var sourceKeys []string
for _, rule := range proposal.Rules {
    if rule.Status == lore.RuleApproved {
        sourceKeys = append(sourceKeys, rule.SourceEntries...)
    }
}
```

This must be replaced with a mapping from structured entries to text keys:

```go
var sourceKeys []string
for _, rule := range proposal.Rules {
    if rule.Status == lore.RuleApproved {
        for _, se := range rule.SourceEntries {
            switch se.Type {
            case "failure":
                sourceKeys = append(sourceKeys, se.InputSummary)
            default:
                sourceKeys = append(sourceKeys, se.Text)
            }
        }
    }
}
```

**3. `internal/daemon/daemon.go:1215`** — This is a second code path that builds proposals from extracted rules (separate from `finalizeCuration` in handlers). Since both `ExtractedRule.SourceEntries` and `Rule.SourceEntries` change to `[]RuleSourceEntry` together, the assignment will still compile. Verify this file explicitly and run its tests.

Grep for any other `SourceEntries` usage across the codebase to catch remaining breakages.

### 1f. Run all lore tests

```bash
go test ./internal/lore/... -count=1
```

---

## Step 2: Add PublicRuleMode to LoreConfig

**File**: `internal/config/config.go`

### 2a. Write failing test

**File**: `internal/config/lore_save_test.go`

Add a test for the new config field:

```go
func TestLoreConfigPublicRuleMode(t *testing.T) {
	cfg := &Config{
		Lore: &LoreConfig{
			PublicRuleMode: "create_pr",
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Lore == nil || decoded.Lore.PublicRuleMode != "create_pr" {
		t.Errorf("expected create_pr, got %v", decoded.Lore)
	}
}
```

### 2b. Run test to verify it fails

```bash
go test ./internal/config/ -run TestLoreConfigPublicRuleMode -count=1
```

### 2c. Write implementation

**File**: `internal/config/config.go`

Add to `LoreConfig` struct (after `AutoPR` field, around line 390):

```go
PublicRuleMode string `json:"public_rule_mode,omitempty"` // "direct_push" (default) or "create_pr"
```

Add a helper method:

```go
func (lc *LoreConfig) GetPublicRuleMode() string {
	if lc == nil || lc.PublicRuleMode == "" {
		return "direct_push"
	}
	return lc.PublicRuleMode
}
```

### 2d. Run test to verify it passes

```bash
go test ./internal/config/ -run TestLoreConfigPublicRuleMode -count=1
```

---

## Step 3: Add auto-commit handler to backend

**File**: `internal/dashboard/handlers_lore.go`

### 3a. Write failing test

**File**: `internal/dashboard/handlers_lore_test.go`

Add a test for the new auto-commit behavior. This test should verify that when `auto_commit: true` is passed in the apply-merge request, the handler commits and pushes instead of leaving unstaged changes. The exact test depends on how the existing `TestHandleLoreApplyMerge` tests are structured — follow the same pattern but assert on git log output instead of unstaged diff.

```go
func TestHandleLoreApplyMergeAutoCommit(t *testing.T) {
	// Follow the pattern in the existing TestHandleLoreApplyMerge tests.
	// Set up a proposal with approved public rules.
	// Call apply-merge with auto_commit: true.
	// Assert: the workspace has a commit with the merged content (not unstaged).
	// Assert: response includes commit_sha.
}
```

### 3b. Run test to verify it fails

```bash
go test ./internal/dashboard/ -run TestHandleLoreApplyMergeAutoCommit -count=1
```

### 3c. Write implementation

**File**: `internal/dashboard/handlers_lore.go`

Modify `handleLoreApplyMerge` (line 428). The existing flow for public rules:

1. Creates/reuses `schmux/lore` workspace (line 483)
2. Writes merged content as unstaged change (lines 497-509)
3. Spawns a bash shell session (lines 514-531)

Add an `auto_commit` field to the anonymous request body struct at line 442 (NOT `mergeApplyRequest`, which is the per-layer struct):

```go
var body struct {
	Merges     []mergeApplyRequest `json:"merges"`
	AutoCommit bool                `json:"auto_commit"`
}
```

After writing the file (line 509), if `body.AutoCommit` is true:

```go
if body.AutoCommit {
	// Stage the file
	if err := exec.CommandContext(r.Context(), "git", "-C", ws.Path, "add", targetPath).Run(); err != nil {
		http.Error(w, "failed to stage: "+err.Error(), 500)
		return
	}
	// Commit
	msg := fmt.Sprintf("lore: add %d rules from agent learnings", len(approvedRules))
	if err := exec.CommandContext(r.Context(), "git", "-C", ws.Path, "commit", "-m", msg).Run(); err != nil {
		http.Error(w, "failed to commit: "+err.Error(), 500)
		return
	}
	// Get commit SHA via exec (no getHeadSHA helper exists)
	shaOut, err := exec.CommandContext(r.Context(), "git", "-C", ws.Path, "rev-parse", "HEAD").Output()
	commitSHA := strings.TrimSpace(string(shaOut))

	// Push based on config mode
	mode := "direct_push"
	if s.config != nil && s.config.Lore != nil {
		mode = s.config.Lore.GetPublicRuleMode()
	}
	if mode == "create_pr" {
		branch := fmt.Sprintf("lore/rules-%s", time.Now().Format("2006-01-02"))
		// Push to branch, create PR via GitHub API
		// ... (use existing git helpers or exec git push)
	} else {
		// Push to main
		if err := exec.CommandContext(r.Context(), "git", "-C", ws.Path, "push", "origin", "main").Run(); err != nil {
			http.Error(w, "push failed: "+err.Error(), 500)
			return
		}
	}
	// Add commit_sha to the result map (results are []map[string]string)
	resultMap["commit_sha"] = commitSHA
}
```

Note: `results` is `[]map[string]string`, not a typed struct. Add `commit_sha` as a map key alongside existing `"layer"`, `"status"`, `"workspace_id"` keys. The exact implementation should follow existing git patterns in the codebase (check `internal/workspace/` for git helpers).

### 3d. Run test to verify it passes

```bash
go test ./internal/dashboard/ -run TestHandleLoreApplyMergeAutoCommit -count=1
```

---

## Step 4: Update extraction prompt to embed source entry data

**File**: `internal/lore/curator.go`

### 4a. Write failing test

**File**: `internal/lore/curator_test.go`

Add a test that verifies `ParseExtractionResponse` correctly parses structured source entries:

```go
func TestParseExtractionResponseStructuredSources(t *testing.T) {
	response := `{
		"rules": [{
			"text": "Always run tests from root",
			"category": "testing",
			"suggested_layer": "repo_private",
			"source_entries": [
				{"type": "failure", "input_summary": "cd sub && go test", "error_summary": "module not found"},
				{"type": "reflection", "text": "tests must run from root"}
			]
		}],
		"discarded_entries": []
	}`
	result, err := ParseExtractionResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(result.Rules))
	}
	if len(result.Rules[0].SourceEntries) != 2 {
		t.Fatalf("expected 2 source entries, got %d", len(result.Rules[0].SourceEntries))
	}
	if result.Rules[0].SourceEntries[0].Type != "failure" {
		t.Errorf("expected failure type, got %s", result.Rules[0].SourceEntries[0].Type)
	}
	if result.Rules[0].SourceEntries[0].InputSummary != "cd sub && go test" {
		t.Errorf("expected input summary, got %s", result.Rules[0].SourceEntries[0].InputSummary)
	}
}
```

### 4b. Run test to verify it fails

```bash
go test ./internal/lore/ -run TestParseExtractionResponseStructuredSources -count=1
```

### 4c. Write implementation

**File**: `internal/lore/curator.go`

Update the extraction prompt in `BuildExtractionPrompt` (line 38). The JSON schema section currently asks for `"source_entries": ["<timestamp or entry key>"]`. Change it to request structured data:

```
"source_entries": [
  {
    "type": "failure|reflection|friction",
    "text": "reflection or friction text (omit for failures)",
    "input_summary": "what was attempted (for failures only)",
    "error_summary": "what went wrong (for failures only)",
    "tool": "tool name if applicable"
  }
]
```

The entries fed to the LLM already include all this data (failure records have `input_summary` and `error_summary` at lines 118-123, reflections have `text` at line 125). The LLM just needs to echo the relevant fields back in structured form rather than as opaque timestamps.

### 4d. Run test to verify it passes

```bash
go test ./internal/lore/ -run TestParseExtractionResponseStructuredSources -count=1
```

### 4e. Fix MarkEntriesDirect compatibility

The `MarkEntriesDirect` function in `scratchpad.go` uses `SourceEntries` for timestamp-based marking. Since we're changing source entries to structured data, update `handleLoreCurate` (in `handlers_lore.go`, around line 672) to mark ALL entries sent to the LLM as "proposed" using their timestamps directly (which it already does via `MarkEntriesDirect` at line 880), not via `SourceEntries`. Verify this doesn't regress.

### 4f. Run all lore tests

```bash
go test ./internal/lore/... ./internal/dashboard/... -count=1
```

---

## Step 5: Update TypeScript types and API client

**File**: `assets/dashboard/src/lib/types.ts`

### 5a. Update LoreRule type

Add `RuleSourceEntry` interface and update `LoreRule.source_entries`:

```typescript
export interface RuleSourceEntry {
  type: 'failure' | 'reflection' | 'friction';
  text?: string;
  input_summary?: string;
  error_summary?: string;
  tool?: string;
}

export interface LoreRule {
  id: string;
  text: string;
  category: string;
  suggested_layer: LoreLayer;
  chosen_layer?: LoreLayer;
  status: LoreRuleStatus;
  source_entries: RuleSourceEntry[];
  merged_at?: string;
}
```

### 5b. Update LoreMergeApplyResult

Add `commit_sha` field:

```typescript
export interface LoreMergeApplyResult {
  layer: LoreLayer;
  status: string;
  workspace_id?: string;
  branch?: string;
  pr_url?: string;
  commit_sha?: string;
}
```

### 5c. Update API client

**File**: `assets/dashboard/src/lib/api.ts`

Update `applyLoreMerge` (line 1011) to accept and pass `autoCommit`:

```typescript
export async function applyLoreMerge(
  repoName: string,
  proposalID: string,
  merges: { layer: string; content: string }[],
  autoCommit = false
): Promise<{ results: LoreMergeApplyResult[] }> {
  const resp = await fetch(`/api/lore/${repoName}/proposals/${proposalID}/apply-merge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ merges, auto_commit: autoCommit }),
  });
  if (!resp.ok) throw new Error(await resp.text());
  return resp.json();
}
```

### 5d. Verify types compile

```bash
go run ./cmd/build-dashboard
```

---

## Step 6: Add LoreConfig public_rule_mode to full config plumbing

Adding a new config field requires touching 5 files across the full plumbing chain. The existing `AutoPR` field is deprecated in favor of `PublicRuleMode` — if `AutoPR` is true and `PublicRuleMode` is empty, treat it as `"create_pr"` for backward compat.

### 6a. API contracts

**File**: `internal/api/contracts/config.go`

Add `PublicRuleMode string` to both the `Lore` struct (read path) and `LoreUpdate` struct (write path), with json tag `"public_rule_mode,omitempty"`.

### 6b. Regenerate TypeScript types

```bash
go run ./cmd/gen-types
```

### 6c. Config handler mapping

**File**: `internal/dashboard/handlers_config.go`

Add read mapping (config → API response) and write mapping (API request → config) for `PublicRuleMode`, following the pattern used by `CurateOnDispose`.

### 6d. Config form state

**File**: `assets/dashboard/src/routes/config/useConfigForm.ts`

Add `lorePublicRuleMode` to `ConfigFormState`, `ConfigSnapshot`, default values, `hasChanges` check, and `snapshotConfig` mapping. Follow the pattern of `loreCurateOnDispose`.

### 6e. UI component

**File**: `assets/dashboard/src/routes/config/AdvancedTab.tsx`

In the lore config section, add a select using the `setField` dispatch pattern:

```tsx
<label>
  Public Rule Mode
  <select
    value={state.lorePublicRuleMode || 'direct_push'}
    onChange={(e) => setField('lorePublicRuleMode', e.target.value)}
  >
    <option value="direct_push">Direct push to main</option>
    <option value="create_pr">Create pull request</option>
  </select>
</label>
```

### 6f. Verify it renders and round-trips

```bash
go run ./cmd/build-dashboard && go test ./internal/dashboard/ -run TestConfig -count=1
```

---

## Step 7: Create LoreCard component — instruction variant

**File**: `assets/dashboard/src/components/LoreCard.tsx` (new file)
**File**: `assets/dashboard/src/components/LoreCard.module.css` (new file)

### 7a. Write test

**File**: `assets/dashboard/src/components/LoreCard.test.tsx` (new file)

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { LoreCard } from './LoreCard';

const mockRule = {
  id: 'r1',
  text: 'Always run tests from root',
  category: 'testing',
  suggested_layer: 'repo_private' as const,
  status: 'pending' as const,
  source_entries: [
    { type: 'failure' as const, input_summary: 'cd sub && go test', error_summary: 'module not found' },
    { type: 'reflection' as const, text: 'tests must run from root' },
  ],
};

describe('LoreCard (instruction)', () => {
  it('renders rule text and source signals', () => {
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onLayerChange={vi.fn()}
      />
    );
    expect(screen.getByText('Always run tests from root')).toBeTruthy();
    expect(screen.getByText(/module not found/)).toBeTruthy();
    expect(screen.getByText(/tests must run from root/)).toBeTruthy();
  });

  it('shows category and repo name', () => {
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onLayerChange={vi.fn()}
      />
    );
    expect(screen.getByText('testing')).toBeTruthy();
    expect(screen.getByText('schmux')).toBeTruthy();
  });

  it('defaults to private with commit-to-repo unchecked', () => {
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onLayerChange={vi.fn()}
      />
    );
    const checkbox = screen.getByLabelText(/commit to repo/i);
    expect(checkbox).not.toBeChecked();
  });

  it('calls onApprove when Approve is clicked', () => {
    const onApprove = vi.fn();
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={onApprove}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onLayerChange={vi.fn()}
      />
    );
    fireEvent.click(screen.getByText('Approve'));
    expect(onApprove).toHaveBeenCalledWith('r1');
  });

  it('calls onDismiss when Dismiss is clicked', () => {
    const onDismiss = vi.fn();
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={vi.fn()}
        onDismiss={onDismiss}
        onEdit={vi.fn()}
        onLayerChange={vi.fn()}
      />
    );
    fireEvent.click(screen.getByText('Dismiss'));
    expect(onDismiss).toHaveBeenCalledWith('r1');
  });
});
```

### 7b. Run test to verify it fails

```bash
./test.sh --quick
```

### 7c. Write implementation

**File**: `assets/dashboard/src/components/LoreCard.tsx`

Build the LoreCard component with:

- Props: `type`, `rule` (LoreRule), `repoName`, `proposalId`, `onApprove`, `onDismiss`, `onEdit`, `onLayerChange`
- Top row: type label + category tag (left), repo name (right)
- Body: rule text (for instructions)
- Source signals section: map `rule.source_entries` to signal rows with type-colored left borders
- Privacy controls: "Commit to repo" checkbox, nested "Apply to all repos" checkbox. Checking/unchecking calls `onLayerChange` with the appropriate layer.
- Action buttons: Dismiss, Edit, Approve
- When status is `approved`: render collapsed single-line variant
- When status is `dismissed`: don't render (parent handles removal)

**File**: `assets/dashboard/src/components/LoreCard.module.css`

Style the card with:

- `.card` — border, border-radius, padding, background matching existing `proposalCard` pattern
- `.cardHeader` — flex row with type/category left, repo right
- `.typeLabel` — small caps, muted color
- `.categoryTag` — accent-colored chip
- `.repoName` — muted, right-aligned
- `.ruleText` — prominent, larger font
- `.signalDivider` — subtle dashed border
- `.signalRow` — left border colored by type (danger/accent/warning), small text
- `.privacyControls` — checkbox group with nesting
- `.actions` — flex row, right-aligned, matching existing button styles
- `.collapsed` — single-line flex row for approved cards
- Transitions: `transition: opacity 150ms, border-color 150ms, max-height 300ms`

### 7d. Run test to verify it passes

```bash
./test.sh --quick
```

---

## Step 8: Create LoreCard component — action variant

**File**: `assets/dashboard/src/components/LoreCard.tsx` (extend existing)

### 8a. Write test

**File**: `assets/dashboard/src/components/LoreCard.test.tsx` (add to existing)

```typescript
import type { SpawnEntry } from '../lib/types.generated';

const mockAction: SpawnEntry = {
  id: 'a1',
  name: 'Fix lint errors',
  type: 'skill',
  prompt: './format.sh && ./test.sh --quick',
  state: 'proposed',
  source: 'emerged',
  use_count: 0,
  description: 'Auto-format and run quick tests',
  metadata: {
    skill_name: 'fix-lint',
    confidence: 0.8,
    evidence: ['ran format 4 times'],
    evidence_count: 4,
    emerged_at: '2026-04-01T00:00:00Z',
    last_curated: '2026-04-01T00:00:00Z',
  },
};

describe('LoreCard (action)', () => {
  it('renders action name and prompt', () => {
    render(
      <LoreCard
        type="action"
        action={mockAction}
        repoName="schmux"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
      />
    );
    expect(screen.getByText('Fix lint errors')).toBeTruthy();
    expect(screen.getByText(/format\.sh/)).toBeTruthy();
  });

  it('does not show privacy controls for actions', () => {
    render(
      <LoreCard
        type="action"
        action={mockAction}
        repoName="schmux"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
      />
    );
    expect(screen.queryByLabelText(/commit to repo/i)).toBeNull();
  });
});
```

### 8b. Run test to verify it fails

```bash
./test.sh --quick
```

### 8c. Write implementation

Extend `LoreCard` to accept an optional `action` prop (SpawnEntry) alongside the `rule` prop. When `type === 'action'`:

- Body shows action name + prompt/command (in monospace)
- Source signals come from `action.metadata.evidence` array
- No privacy controls
- Approve calls the pin API, Dismiss calls the dismiss API

### 8d. Run test to verify it passes

```bash
./test.sh --quick
```

---

## Step 9: Add inline edit flow to LoreCard

**File**: `assets/dashboard/src/components/LoreCard.tsx`

### 9a. Write test

**File**: `assets/dashboard/src/components/LoreCard.test.tsx` (add)

```typescript
describe('LoreCard edit flow', () => {
  it('shows textarea when Edit is clicked', () => {
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onLayerChange={vi.fn()}
      />
    );
    fireEvent.click(screen.getByText('Edit'));
    expect(screen.getByRole('textbox')).toBeTruthy();
  });

  it('calls onEdit with new text when Save is clicked', () => {
    const onEdit = vi.fn();
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={onEdit}
        onLayerChange={vi.fn()}
      />
    );
    fireEvent.click(screen.getByText('Edit'));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Updated rule text' } });
    fireEvent.click(screen.getByText('Save'));
    expect(onEdit).toHaveBeenCalledWith('r1', 'Updated rule text');
  });

  it('restores original text when Cancel is clicked', () => {
    render(
      <LoreCard
        type="instruction"
        rule={mockRule}
        repoName="schmux"
        proposalId="p1"
        onApprove={vi.fn()}
        onDismiss={vi.fn()}
        onEdit={vi.fn()}
        onLayerChange={vi.fn()}
      />
    );
    fireEvent.click(screen.getByText('Edit'));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Changed' } });
    fireEvent.click(screen.getByText('Cancel'));
    expect(screen.getByText('Always run tests from root')).toBeTruthy();
  });
});
```

### 9b. Run test, implement, run test

Add `editing` state to `LoreCard`. When editing: replace rule text with textarea + Save/Cancel buttons. Save calls `onEdit(ruleId, newText)`. Cancel resets.

```bash
./test.sh --quick
```

---

## Step 10: Add card dismiss animation

**File**: `assets/dashboard/src/components/LoreCard.module.css`

### 10a. Implementation

Add CSS for dismiss animation:

```css
.card {
  transition:
    opacity 200ms ease,
    transform 200ms ease,
    max-height 300ms ease;
  overflow: hidden;
}

.cardDismissing {
  opacity: 0;
  transform: translateX(-20px);
  max-height: 0;
  padding: 0;
  margin: 0;
  border: none;
}
```

In `LoreCard.tsx`, add a `dismissing` state. When `onDismiss` is called, set `dismissing = true`, then after 200ms call the actual dismiss callback. The parent component removes the card from the list after the callback.

### 10b. Verify visually

```bash
go run ./cmd/build-dashboard
```

---

## Step 11: Add card approve collapse animation

**File**: `assets/dashboard/src/components/LoreCard.module.css`

### 11a. Implementation

Add CSS for the collapsed (approved) state:

```css
.cardCollapsed {
  padding: 0.5rem 0.75rem;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  border-left: 3px solid var(--color-success);
}

.collapsedCheck {
  color: var(--color-success);
  font-size: 0.8rem;
}

.collapsedText {
  font-size: 0.8rem;
  color: var(--text-primary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.collapsedLayer {
  font-size: 0.7rem;
  color: var(--text-secondary);
}
```

In `LoreCard.tsx`, when `rule.status === 'approved'`, render the collapsed variant instead of the full card.

### 11b. Verify visually

```bash
go run ./cmd/build-dashboard
```

---

## Step 12: Rewrite LorePage — data loading and card flattening

**File**: `assets/dashboard/src/routes/LorePage.tsx`

This step replaces the data loading layer. Steps 12-12d handle the data model; Step 12e handles the card wall rendering; Step 12f handles callbacks; Step 12g handles empty/disabled/warning states.

### 12a. Write test

**File**: `assets/dashboard/src/routes/LorePage.test.tsx` (new file)

```typescript
describe('LorePage card wall', () => {
  it('renders cards from all repos in a flat list', () => {
    // Mock getLoreProposals for 2 repos, each with pending rules
    // Assert: all rules appear as cards without repo tabs
  });

  it('does not render repo tabs', () => {
    // Assert: no elements with data-testid="repo-tab"
  });
});
```

### 12b. Run test to verify it fails

```bash
./test.sh --quick
```

### 12c. Write implementation — data loading

Replace the per-repo data loading in `LorePage` with cross-repo fan-out:

1. Fan out `getLoreProposals` + `getAllSpawnEntries` for each configured repo.
2. Flatten all pending rules and proposed actions into a single `cards` array, each tagged with `repoName` and `proposalId` (internal, not displayed to user).
3. Sort by creation date descending.

Remove: `activeRepo` state, repo tab logic, sub-tab logic.

### 12d. Run test to verify it passes

```bash
./test.sh --quick
```

---

## Step 12e: Rewrite LorePage — card wall rendering

**File**: `assets/dashboard/src/routes/LorePage.tsx`

### 12e-a. Write test

```typescript
describe('LorePage card wall rendering', () => {
  it('shows Approve All button when 2+ cards pending', () => {
    // Mock 3 pending rules
    // Assert: "Approve All" button visible
  });
});
```

### 12e-b. Implement

Map `cards` to `<LoreCard>` components. Add "Approve All" button at top when `pendingCards.length >= 2`. Remove `LegacyProposalCard`, `RuleReviewCard`, `RuleRow` (all replaced by `LoreCard`).

```bash
./test.sh --quick
```

---

## Step 12f: Rewrite LorePage — callbacks

**File**: `assets/dashboard/src/routes/LorePage.tsx`

### 12f-a. Implement callbacks

Wire up card callbacks:

- `onApprove(ruleId)` → call `updateLoreRule(repo, proposalId, ruleId, { status: 'approved' })`, update local state
- `onDismiss(ruleId)` → call `updateLoreRule(repo, proposalId, ruleId, { status: 'dismissed' })`, remove from cards
- `onEdit(ruleId, text)` → call `updateLoreRule(repo, proposalId, ruleId, { text })`, update local state
- `onLayerChange(ruleId, layer)` → call `updateLoreRule(repo, proposalId, ruleId, { chosen_layer: layer })`, update local state
- For actions: `onApprove` → `pinSpawnEntry`, `onDismiss` → `dismissSpawnEntry`

```bash
./test.sh --quick
```

---

## Step 12g: Rewrite LorePage — empty, disabled, warning states

**File**: `assets/dashboard/src/routes/LorePage.tsx`

### 12g-a. Write test

```typescript
describe('LorePage states', () => {
  it('shows empty state when no pending rules', () => {
    // Mock empty proposals
    // Assert: "Nothing to review" message
  });
});
```

### 12g-b. Implement

- **Empty state**: "Nothing to review. Rules will appear here automatically as agents encounter friction."
- **Lore disabled state**: Keep existing disabled state check.
- **Warning banner**: Keep existing warning banner.

```bash
./test.sh --quick
```

---

## Step 13: Add persistence summary phase

**File**: `assets/dashboard/src/routes/LorePage.tsx`

### 13a. Write test

```typescript
describe('LorePage persistence summary', () => {
  it('shows summary when all cards are resolved', () => {
    // Mock: all rules approved/dismissed, none pending
    // Assert: summary shows "N learnings approved" and Apply button
  });

  it('groups by destination in summary', () => {
    // Mock: 2 private rules, 1 public rule, 1 action
    // Assert: summary shows correct grouping
  });
});
```

### 13b. Run test, implement, run test

Add a `phase` state to LorePage: `'triage' | 'summary' | 'applying' | 'done'`.

When all pending cards are resolved (approved or dismissed) and there are approved cards, transition to `phase = 'summary'`. Render a `PersistenceSummary` component that:

- Counts approved rules by destination (private/this-repo, private/all-repos, public)
- Counts approved actions
- Shows the breakdown
- Has an "Apply" button

```bash
./test.sh --quick
```

---

## Step 14: Add inline diff + commit/push flow

**File**: `assets/dashboard/src/routes/LorePage.tsx`

### 14a. Write test

```typescript
describe('LorePage public rule persistence', () => {
  it('shows diff viewer after merge completes', () => {
    // Mock: merge returns preview with current/merged content
    // Assert: ReactDiffViewer is rendered
  });

  it('shows Commit & Push button in direct_push mode', () => {
    // Mock: config.lore.public_rule_mode = 'direct_push'
    // Assert: "Commit & Push" button visible
  });

  it('shows Create PR button in create_pr mode', () => {
    // Mock: config.lore.public_rule_mode = 'create_pr'
    // Assert: "Create PR" button visible
  });
});
```

### 14b. Run test, implement, run test

When "Apply" is clicked in the persistence summary:

1. Private rules + actions: persist immediately (call APIs), show success.
2. Public rules: group by `(repo, proposalId)`, call `startLoreMerge` for each, poll for completion, show merge previews inline with `ReactDiffViewer`.
3. After reviewing diff: "Commit & Push" or "Create PR" button calls `applyLoreMerge` with `autoCommit: true`.
4. On success: show confirmation message, transition to `phase = 'done'`.
5. On error: show error inline with Retry button.

```bash
./test.sh --quick
```

---

## Step 15: Dev mode debug section + sidebar badge + cleanup

### 15a. Dev mode debug section

**File**: `assets/dashboard/src/routes/LorePage.tsx`

Add at the bottom of the page, gated by `useDevStatus().isDevMode`:

```tsx
{
  isDevMode && (
    <section className={styles.debugSection}>
      <button className={styles.toggleButton} onClick={toggleDebug}>
        {showDebug ? '▼' : '▶'} Debug
      </button>
      {showDebug && (
        <>
          {/* Repo selector for curation trigger */}
          <select value={debugRepo} onChange={(e) => setDebugRepo(e.target.value)}>
            {repos.map((r) => (
              <option key={r.name} value={r.name}>
                {r.name}
              </option>
            ))}
          </select>
          {/* Existing filter bar, entries list, trigger curation, delete signals */}
          {/* Reuse the raw signals UI from the current implementation */}
        </>
      )}
    </section>
  );
}
```

### 15b. Update sidebar badge

**File**: `assets/dashboard/src/components/ToolsSection.tsx`

The badge logic already counts pending proposals + proposed actions across all repos (lines 53-77). The aggregation is correct for cross-repo totals. However, the current badge counts pending **proposals**, not individual **rules**. If the badge should show the number of pending cards (rules + actions) instead of pending proposals, update the count logic to sum `proposal.rules.filter(r => r.status === 'pending').length` instead of counting proposals. Otherwise, keep as-is.

### 15c. Remove old CSS and components

- Delete unused styles from `assets/dashboard/src/styles/lore.module.css` (repo tabs, sub-tabs, legacy proposal styles)
- Keep styles that are reused (filter bar, entry cards, curate button) for the debug section
- Remove `LegacyProposalCard`, `RuleReviewCard`, `RuleRow` from LorePage.tsx (already removed in step 12)

### 15d. Update docs/api.md

**File**: `docs/api.md`

Add documentation for the new `auto_commit` field on the `POST /api/lore/{repo}/proposals/{proposalID}/apply-merge` request body, the new `commit_sha` field on the response, and the new `public_rule_mode` config field. CI enforces that `docs/api.md` is updated when API-related packages change.

### 15e. Run full test suite

```bash
./test.sh
```

### 15f. Build and verify

```bash
go run ./cmd/build-dashboard && go build ./cmd/schmux
```

---

## Step 16: End-to-end verification

### 16a. Run full test suite

```bash
./test.sh
```

### 16b. Manual verification checklist

1. Start dev mode: `./dev.sh`
2. Navigate to `/lore` with no pending proposals → verify empty state message
3. Trigger a curation (dev mode debug section) → verify cards appear
4. Review cards: approve one, dismiss one, edit one → verify animations and state changes
5. Check privacy controls: toggle "Commit to repo" checkbox → verify layer changes
6. Approve all remaining → verify persistence summary appears
7. Click Apply → verify private rules saved, public rules show diff
8. Click "Commit & Push" → verify commit created and pushed
9. Verify sidebar badge updates correctly throughout

### 16c. Update scenario tests

Update or replace the existing lore scenario tests in `test/scenarios/`:

- `lore-page-repo-tabs.md` — remove or rewrite (no more repo tabs)
- `lore-status-warning-banner.md` — keep (warning banner unchanged)
- `configure-lore-settings.md` — update for new public_rule_mode config
- `persist-lore-curator-model.md` — keep (config persistence unchanged)

### 16d. Run scenario tests

```bash
./test.sh --scenarios
```
