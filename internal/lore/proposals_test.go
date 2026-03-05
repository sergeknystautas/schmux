package lore

import (
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
				SourceEntries:  []string{"2026-03-04T10:00:00Z"},
			},
			{
				ID:             "r2",
				Text:           "Prefer meta_codesearch over Explore",
				Category:       "environment",
				SuggestedLayer: LayerUserGlobal,
				Status:         RulePending,
				SourceEntries:  []string{"2026-03-04T11:00:00Z"},
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
	r.ChosenLayer = layerPtr(LayerUserGlobal)
	if r.EffectiveLayer() != LayerUserGlobal {
		t.Errorf("expected user_global, got %s", r.EffectiveLayer())
	}
}

func TestProposal_ApprovedRulesByLayer(t *testing.T) {
	p := &Proposal{
		Rules: []Rule{
			{ID: "r1", Text: "rule1", Status: RuleApproved, SuggestedLayer: LayerRepoPublic},
			{ID: "r2", Text: "rule2", Status: RuleDismissed, SuggestedLayer: LayerRepoPublic},
			{ID: "r3", Text: "rule3", Status: RuleApproved, SuggestedLayer: LayerUserGlobal},
			{ID: "r4", Text: "rule4", Status: RuleApproved, SuggestedLayer: LayerRepoPublic, ChosenLayer: layerPtr(LayerRepoPrivate)},
		},
	}

	groups := p.ApprovedRulesByLayer()
	if len(groups[LayerRepoPublic]) != 1 {
		t.Errorf("expected 1 repo_public rule, got %d", len(groups[LayerRepoPublic]))
	}
	if len(groups[LayerUserGlobal]) != 1 {
		t.Errorf("expected 1 user_global rule, got %d", len(groups[LayerUserGlobal]))
	}
	if len(groups[LayerRepoPrivate]) != 1 {
		t.Errorf("expected 1 repo_private rule (overridden), got %d", len(groups[LayerRepoPrivate]))
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
