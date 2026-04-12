//go:build !noautolearn

package autolearn

import "testing"

func TestEffectiveLayer(t *testing.T) {
	t.Run("nil ChosenLayer returns SuggestedLayer", func(t *testing.T) {
		l := &Learning{
			SuggestedLayer: LayerRepoPublic,
			ChosenLayer:    nil,
		}
		if got := l.EffectiveLayer(); got != LayerRepoPublic {
			t.Errorf("EffectiveLayer() = %q, want %q", got, LayerRepoPublic)
		}
	})

	t.Run("set ChosenLayer returns it", func(t *testing.T) {
		chosen := LayerCrossRepoPrivate
		l := &Learning{
			SuggestedLayer: LayerRepoPublic,
			ChosenLayer:    &chosen,
		}
		if got := l.EffectiveLayer(); got != LayerCrossRepoPrivate {
			t.Errorf("EffectiveLayer() = %q, want %q", got, LayerCrossRepoPrivate)
		}
	})
}

func TestAllResolved(t *testing.T) {
	t.Run("all approved", func(t *testing.T) {
		b := &Batch{
			Learnings: []Learning{
				{Status: StatusApproved},
				{Status: StatusApproved},
			},
		}
		if !b.AllResolved() {
			t.Error("AllResolved() = false, want true")
		}
	})

	t.Run("one pending", func(t *testing.T) {
		b := &Batch{
			Learnings: []Learning{
				{Status: StatusApproved},
				{Status: StatusPending},
			},
		}
		if b.AllResolved() {
			t.Error("AllResolved() = true, want false")
		}
	})

	t.Run("mix of approved and dismissed", func(t *testing.T) {
		b := &Batch{
			Learnings: []Learning{
				{Status: StatusApproved},
				{Status: StatusDismissed},
				{Status: StatusApproved},
			},
		}
		if !b.AllResolved() {
			t.Error("AllResolved() = false, want true")
		}
	})

	t.Run("empty learnings", func(t *testing.T) {
		b := &Batch{
			Learnings: []Learning{},
		}
		if !b.AllResolved() {
			t.Error("AllResolved() = false, want true")
		}
	})
}

func TestIsAvailable(t *testing.T) {
	if !IsAvailable() {
		t.Error("IsAvailable() = false, want true")
	}
}
