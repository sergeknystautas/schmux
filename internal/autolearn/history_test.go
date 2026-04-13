//go:build !noautolearn

package autolearn

import "testing"

func ptr[T any](v T) *T { return &v }

func TestHistoryFilterByKind(t *testing.T) {
	batches := []*Batch{
		{Learnings: []Learning{
			{ID: "r1", Kind: KindRule, Status: StatusPending, SuggestedLayer: LayerRepoPublic},
			{ID: "s1", Kind: KindSkill, Status: StatusPending, SuggestedLayer: LayerRepoPublic},
			{ID: "r2", Kind: KindRule, Status: StatusApproved, SuggestedLayer: LayerRepoPrivate},
		}},
	}

	t.Run("rules only", func(t *testing.T) {
		got := FilterLearnings(batches, ptr(KindRule), nil, nil)
		if len(got) != 2 {
			t.Fatalf("got %d learnings, want 2", len(got))
		}
		for _, l := range got {
			if l.Kind != KindRule {
				t.Errorf("got kind %q, want %q", l.Kind, KindRule)
			}
		}
	})

	t.Run("skills only", func(t *testing.T) {
		got := FilterLearnings(batches, ptr(KindSkill), nil, nil)
		if len(got) != 1 {
			t.Fatalf("got %d learnings, want 1", len(got))
		}
		if got[0].ID != "s1" {
			t.Errorf("got ID %q, want %q", got[0].ID, "s1")
		}
	})
}

func TestHistoryFilterByStatus(t *testing.T) {
	batches := []*Batch{
		{Learnings: []Learning{
			{ID: "a1", Kind: KindRule, Status: StatusPending, SuggestedLayer: LayerRepoPublic},
			{ID: "a2", Kind: KindRule, Status: StatusDismissed, SuggestedLayer: LayerRepoPublic},
			{ID: "a3", Kind: KindSkill, Status: StatusPending, SuggestedLayer: LayerRepoPrivate},
			{ID: "a4", Kind: KindSkill, Status: StatusApproved, SuggestedLayer: LayerRepoPrivate},
		}},
	}

	t.Run("pending only", func(t *testing.T) {
		got := FilterLearnings(batches, nil, ptr(StatusPending), nil)
		if len(got) != 2 {
			t.Fatalf("got %d learnings, want 2", len(got))
		}
		for _, l := range got {
			if l.Status != StatusPending {
				t.Errorf("got status %q, want %q", l.Status, StatusPending)
			}
		}
	})

	t.Run("dismissed only", func(t *testing.T) {
		got := FilterLearnings(batches, nil, ptr(StatusDismissed), nil)
		if len(got) != 1 {
			t.Fatalf("got %d learnings, want 1", len(got))
		}
		if got[0].ID != "a2" {
			t.Errorf("got ID %q, want %q", got[0].ID, "a2")
		}
	})
}

func TestHistoryFilterByLayer(t *testing.T) {
	chosen := LayerCrossRepoPrivate
	batches := []*Batch{
		{Learnings: []Learning{
			{ID: "l1", Kind: KindRule, Status: StatusPending, SuggestedLayer: LayerRepoPublic},
			{ID: "l2", Kind: KindSkill, Status: StatusPending, SuggestedLayer: LayerRepoPrivate},
			{ID: "l3", Kind: KindRule, Status: StatusApproved, SuggestedLayer: LayerRepoPublic, ChosenLayer: &chosen},
		}},
	}

	t.Run("repo_public layer", func(t *testing.T) {
		got := FilterLearnings(batches, nil, nil, ptr(LayerRepoPublic))
		if len(got) != 1 {
			t.Fatalf("got %d learnings, want 1", len(got))
		}
		if got[0].ID != "l1" {
			t.Errorf("got ID %q, want %q", got[0].ID, "l1")
		}
	})

	t.Run("cross_repo_private via ChosenLayer", func(t *testing.T) {
		got := FilterLearnings(batches, nil, nil, ptr(LayerCrossRepoPrivate))
		if len(got) != 1 {
			t.Fatalf("got %d learnings, want 1", len(got))
		}
		if got[0].ID != "l3" {
			t.Errorf("got ID %q, want %q (ChosenLayer should override SuggestedLayer)", got[0].ID, "l3")
		}
	})
}

func TestHistoryFilterMultipleCriteria(t *testing.T) {
	batches := []*Batch{
		{Learnings: []Learning{
			{ID: "m1", Kind: KindRule, Status: StatusPending, SuggestedLayer: LayerRepoPublic},
			{ID: "m2", Kind: KindRule, Status: StatusApproved, SuggestedLayer: LayerRepoPublic},
			{ID: "m3", Kind: KindSkill, Status: StatusPending, SuggestedLayer: LayerRepoPublic},
			{ID: "m4", Kind: KindRule, Status: StatusPending, SuggestedLayer: LayerRepoPrivate},
		}},
	}

	got := FilterLearnings(batches, ptr(KindRule), ptr(StatusPending), ptr(LayerRepoPublic))
	if len(got) != 1 {
		t.Fatalf("got %d learnings, want 1", len(got))
	}
	if got[0].ID != "m1" {
		t.Errorf("got ID %q, want %q", got[0].ID, "m1")
	}
}

func TestHistoryFilterNilReturnsAll(t *testing.T) {
	batches := []*Batch{
		{Learnings: []Learning{
			{ID: "n1", Kind: KindRule, Status: StatusPending, SuggestedLayer: LayerRepoPublic},
			{ID: "n2", Kind: KindSkill, Status: StatusApproved, SuggestedLayer: LayerRepoPrivate},
		}},
		{Learnings: []Learning{
			{ID: "n3", Kind: KindRule, Status: StatusDismissed, SuggestedLayer: LayerCrossRepoPrivate},
		}},
	}

	got := FilterLearnings(batches, nil, nil, nil)
	if len(got) != 3 {
		t.Fatalf("got %d learnings, want 3", len(got))
	}
}
