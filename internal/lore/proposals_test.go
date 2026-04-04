package lore

import (
	"encoding/json"
	"testing"
	"time"
)

func stringPtr(s string) *string { return &s }
func layerPtr(l Layer) *Layer    { return &l }

func TestProposalStore_SaveAndLoad_V2(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	proposal := &Proposal{
		ID:        "prop-20260304-100000-ab12",
		Repo:      "schmux",
		Status:    ProposalPending,
		CreatedAt: time.Now().UTC(),
		Rules: []Rule{
			{
				ID:             "r1",
				Text:           "Use go run ./cmd/build-dashboard",
				Category:       "build",
				SuggestedLayer: LayerRepoPublic,
				Status:         RulePending,
				SourceEntries:  []RuleSourceEntry{{Type: "failure", InputSummary: "2026-03-04T10:00:00Z"}},
			},
			{
				ID:             "r2",
				Text:           "Prefer meta_codesearch over Explore",
				Category:       "environment",
				SuggestedLayer: LayerCrossRepoPrivate,
				Status:         RulePending,
				SourceEntries:  []RuleSourceEntry{{Type: "reflection", Text: "2026-03-04T11:00:00Z"}},
			},
		},
		Discarded: []string{"2026-03-04T08:00:00Z"},
	}

	if err := store.Save(proposal); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Get("schmux", proposal.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(loaded.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(loaded.Rules))
	}
	if loaded.Rules[0].Text != "Use go run ./cmd/build-dashboard" {
		t.Errorf("unexpected rule text: %s", loaded.Rules[0].Text)
	}
	if loaded.Status != ProposalPending {
		t.Errorf("expected pending, got %s", loaded.Status)
	}
}

func TestProposalStore_UpdateRule(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)
	store.Save(&Proposal{
		ID:     "prop-001",
		Repo:   "myrepo",
		Status: ProposalPending,
		Rules: []Rule{
			{ID: "r1", Text: "original", Status: RulePending, SuggestedLayer: LayerRepoPublic},
			{ID: "r2", Text: "other", Status: RulePending, SuggestedLayer: LayerRepoPrivate},
		},
	})

	// Approve r1 with edits and a layer override
	err := store.UpdateRule("myrepo", "prop-001", "r1", RuleUpdate{
		Status:      RuleApproved,
		Text:        stringPtr("edited rule"),
		ChosenLayer: layerPtr(LayerRepoPrivate),
	})
	if err != nil {
		t.Fatalf("update rule failed: %v", err)
	}

	loaded, _ := store.Get("myrepo", "prop-001")
	r1 := loaded.Rules[0]
	if r1.Status != RuleApproved {
		t.Errorf("expected approved, got %s", r1.Status)
	}
	if r1.Text != "edited rule" {
		t.Errorf("expected edited text, got %s", r1.Text)
	}
	if *r1.ChosenLayer != LayerRepoPrivate {
		t.Errorf("expected repo_private, got %s", *r1.ChosenLayer)
	}
	// Proposal should still be pending (r2 is unresolved)
	if loaded.Status != ProposalPending {
		t.Errorf("proposal should remain pending, got %s", loaded.Status)
	}
}

func TestProposalStore_AllRulesResolved(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)
	store.Save(&Proposal{
		ID:     "prop-001",
		Repo:   "myrepo",
		Status: ProposalPending,
		Rules: []Rule{
			{ID: "r1", Text: "rule1", Status: RulePending, SuggestedLayer: LayerRepoPublic},
		},
	})

	store.UpdateRule("myrepo", "prop-001", "r1", RuleUpdate{Status: RuleApproved})
	loaded, _ := store.Get("myrepo", "prop-001")
	if !loaded.AllRulesResolved() {
		t.Error("all rules should be resolved")
	}
}

func TestProposalStore_List(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	for _, id := range []string{"prop-001", "prop-002"} {
		store.Save(&Proposal{ID: id, Repo: "myrepo", Status: ProposalPending})
	}
	store.Save(&Proposal{ID: "prop-003", Repo: "otherrepo", Status: ProposalPending})

	proposals, err := store.List("myrepo")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(proposals) != 2 {
		t.Fatalf("expected 2 proposals for myrepo, got %d", len(proposals))
	}
}

func TestProposalStore_UpdateStatus(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)
	store.Save(&Proposal{ID: "prop-001", Repo: "myrepo", Status: ProposalPending})

	if err := store.UpdateStatus("myrepo", "prop-001", ProposalApplied); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	loaded, _ := store.Get("myrepo", "prop-001")
	if loaded.Status != ProposalApplied {
		t.Errorf("expected applied, got %s", loaded.Status)
	}
}

func TestRule_EffectiveLayer(t *testing.T) {
	// Without ChosenLayer, returns SuggestedLayer
	r := Rule{SuggestedLayer: LayerRepoPublic}
	if r.EffectiveLayer() != LayerRepoPublic {
		t.Errorf("expected repo_public, got %s", r.EffectiveLayer())
	}

	// With ChosenLayer, returns ChosenLayer
	r.ChosenLayer = layerPtr(LayerCrossRepoPrivate)
	if r.EffectiveLayer() != LayerCrossRepoPrivate {
		t.Errorf("expected cross_repo_private, got %s", r.EffectiveLayer())
	}
}

func TestProposal_ApprovedRulesByLayer(t *testing.T) {
	p := &Proposal{
		Rules: []Rule{
			{ID: "r1", Text: "rule1", Status: RuleApproved, SuggestedLayer: LayerRepoPublic},
			{ID: "r2", Text: "rule2", Status: RuleDismissed, SuggestedLayer: LayerRepoPublic},
			{ID: "r3", Text: "rule3", Status: RuleApproved, SuggestedLayer: LayerCrossRepoPrivate},
			{ID: "r4", Text: "rule4", Status: RuleApproved, SuggestedLayer: LayerRepoPublic, ChosenLayer: layerPtr(LayerRepoPrivate)},
		},
	}

	groups := p.ApprovedRulesByLayer()
	if len(groups[LayerRepoPublic]) != 1 {
		t.Errorf("expected 1 repo_public rule, got %d", len(groups[LayerRepoPublic]))
	}
	if len(groups[LayerCrossRepoPrivate]) != 1 {
		t.Errorf("expected 1 cross_repo_private rule, got %d", len(groups[LayerCrossRepoPrivate]))
	}
	if len(groups[LayerRepoPrivate]) != 1 {
		t.Errorf("expected 1 repo_private rule (overridden), got %d", len(groups[LayerRepoPrivate]))
	}
}

func TestNormalizeRuleText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Use go run ./cmd/build-dashboard", "use go run ./cmd/build-dashboard"},
		{"  Use  go   run  ", "use go run"},
		{"\tAlways run tests\n", "always run tests"},
		{"", ""},
		{"UPPER CASE", "upper case"},
	}
	for _, tt := range tests {
		got := NormalizeRuleText(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeRuleText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDeduplicateRules(t *testing.T) {
	existing := []string{
		"Use go run ./cmd/build-dashboard",
		"Run tests before committing",
	}
	rules := []Rule{
		{ID: "r1", Text: "use go run ./cmd/build-dashboard", Category: "build"},  // duplicate (case difference)
		{ID: "r2", Text: "Always check lint output", Category: "workflow"},       // new
		{ID: "r3", Text: "  Run  tests  before  committing  ", Category: "test"}, // duplicate (whitespace difference)
		{ID: "r4", Text: "Prefer table-driven tests", Category: "test"},          // new
	}

	kept, removed := DeduplicateRules(rules, existing)
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d", len(kept))
	}
	if kept[0].ID != "r2" {
		t.Errorf("expected r2 first, got %s", kept[0].ID)
	}
	if kept[1].ID != "r4" {
		t.Errorf("expected r4 second, got %s", kept[1].ID)
	}
}

func TestDeduplicateRules_NoExisting(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Text: "rule one"},
		{ID: "r2", Text: "rule two"},
	}
	kept, removed := DeduplicateRules(rules, nil)
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
	if len(kept) != 2 {
		t.Errorf("expected 2 kept, got %d", len(kept))
	}
}

func TestPendingRuleTexts(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	// Pending proposal with mix of rule statuses
	store.Save(&Proposal{
		ID:     "prop-001",
		Repo:   "myrepo",
		Status: ProposalPending,
		Rules: []Rule{
			{ID: "r1", Text: "pending rule", Status: RulePending},
			{ID: "r2", Text: "approved rule", Status: RuleApproved},
			{ID: "r3", Text: "dismissed rule", Status: RuleDismissed},
		},
	})
	// Applied proposal — should be excluded
	store.Save(&Proposal{
		ID:     "prop-002",
		Repo:   "myrepo",
		Status: ProposalApplied,
		Rules: []Rule{
			{ID: "r4", Text: "applied proposal rule", Status: RuleApproved},
		},
	})
	// Merging proposal — should be included
	store.Save(&Proposal{
		ID:     "prop-003",
		Repo:   "myrepo",
		Status: ProposalMerging,
		Rules: []Rule{
			{ID: "r5", Text: "merging rule", Status: RulePending},
		},
	})

	texts := store.PendingRuleTexts("myrepo")

	// Should include pending + approved from pending proposals, and rules from merging proposals.
	// Should exclude dismissed rules and rules from applied proposals.
	want := map[string]bool{
		"pending rule":  true,
		"approved rule": true,
		"merging rule":  true,
	}
	got := make(map[string]bool, len(texts))
	for _, t := range texts {
		got[t] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected %q in pending rule texts", w)
		}
	}
	if got["dismissed rule"] {
		t.Error("dismissed rule should not appear in pending rule texts")
	}
	if got["applied proposal rule"] {
		t.Error("rules from applied proposals should not appear")
	}
}

func TestPendingRuleTexts_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	texts := store.PendingRuleTexts("nonexistent")
	if len(texts) != 0 {
		t.Errorf("expected empty texts for nonexistent repo, got %d", len(texts))
	}
}

func TestDismissedRuleTexts(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	// Pending proposal with one dismissed rule
	store.Save(&Proposal{
		ID:     "prop-001",
		Repo:   "myrepo",
		Status: ProposalPending,
		Rules: []Rule{
			{ID: "r1", Text: "pending rule", Status: RulePending},
			{ID: "r2", Text: "dismissed in pending", Status: RuleDismissed},
		},
	})
	// Fully dismissed proposal — all rules should appear
	store.Save(&Proposal{
		ID:     "prop-002",
		Repo:   "myrepo",
		Status: ProposalDismissed,
		Rules: []Rule{
			{ID: "r3", Text: "rule from dismissed proposal", Status: RulePending},
			{ID: "r4", Text: "another dismissed proposal rule", Status: RuleApproved},
		},
	})
	// Applied proposal — individually dismissed rules should appear
	store.Save(&Proposal{
		ID:     "prop-003",
		Repo:   "myrepo",
		Status: ProposalApplied,
		Rules: []Rule{
			{ID: "r5", Text: "applied rule", Status: RuleApproved},
			{ID: "r6", Text: "dismissed in applied", Status: RuleDismissed},
		},
	})

	texts := store.DismissedRuleTexts("myrepo")

	want := map[string]bool{
		"dismissed in pending":            true,
		"rule from dismissed proposal":    true,
		"another dismissed proposal rule": true,
		"dismissed in applied":            true,
	}
	got := make(map[string]bool, len(texts))
	for _, t := range texts {
		got[t] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected %q in dismissed rule texts", w)
		}
	}
	if got["pending rule"] {
		t.Error("pending rule should not appear in dismissed texts")
	}
	if got["applied rule"] {
		t.Error("applied rule should not appear in dismissed texts")
	}
}

func TestDismissedRuleTexts_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)

	texts := store.DismissedRuleTexts("nonexistent")
	if len(texts) != 0 {
		t.Errorf("expected empty texts for nonexistent repo, got %d", len(texts))
	}
}

func TestRuleSourceEntryJSON(t *testing.T) {
	rule := Rule{
		ID:             "r1",
		Text:           "Always run tests from root",
		Category:       "testing",
		SuggestedLayer: LayerRepoPrivate,
		Status:         RulePending,
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

func TestRuleSourceEntryJSON_LegacyStringFormat(t *testing.T) {
	// Old proposals stored source_entries as plain strings (timestamps).
	raw := `{
		"id": "r1",
		"text": "Always run tests from root",
		"category": "testing",
		"suggested_layer": "repo_private",
		"status": "pending",
		"source_entries": ["2026-03-23T14:11:59Z", "entry-key-abc"]
	}`
	var rule Rule
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		t.Fatalf("failed to unmarshal legacy format: %v", err)
	}
	if len(rule.SourceEntries) != 2 {
		t.Fatalf("expected 2 source entries, got %d", len(rule.SourceEntries))
	}
	if rule.SourceEntries[0].Type != "unknown" {
		t.Errorf("expected type unknown for legacy entry, got %s", rule.SourceEntries[0].Type)
	}
	if rule.SourceEntries[0].Text != "2026-03-23T14:11:59Z" {
		t.Errorf("expected timestamp as text, got %s", rule.SourceEntries[0].Text)
	}
	if rule.Text != "Always run tests from root" {
		t.Errorf("expected rule text preserved, got %s", rule.Text)
	}
}

func TestProposalStore_UpdateRule_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewProposalStore(dir, nil)
	store.Save(&Proposal{
		ID:     "prop-001",
		Repo:   "myrepo",
		Status: ProposalPending,
		Rules: []Rule{
			{ID: "r1", Text: "rule1", Status: RulePending, SuggestedLayer: LayerRepoPublic},
		},
	})

	err := store.UpdateRule("myrepo", "prop-001", "nonexistent", RuleUpdate{Status: RuleApproved})
	if err == nil {
		t.Error("expected error for nonexistent rule ID")
	}
}
