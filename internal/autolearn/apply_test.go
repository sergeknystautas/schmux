package autolearn

import (
	"testing"
)

func TestNormalizeLearningTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Use go run ./cmd/build-dashboard", "use go run ./cmd/build-dashboard"},
		{"  Use  go   run  ", "use go run"},
		{"\tAlways run tests\n", "always run tests"},
		{"", ""},
		{"UPPER CASE", "upper case"},
		{"trailing period.", "trailing period"},
		{"trailing exclamation!", "trailing exclamation"},
		{"trailing comma,", "trailing comma"},
		{"no trailing punct", "no trailing punct"},
		{"multiple dots...", "multiple dots"},
	}
	for _, tt := range tests {
		got := NormalizeLearningTitle(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeLearningTitle(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDeduplicateLearnings(t *testing.T) {
	existing := []string{
		"Use go run ./cmd/build-dashboard",
		"Run tests before committing",
	}
	learnings := []Learning{
		{ID: "l1", Title: "use go run ./cmd/build-dashboard", Kind: KindRule},   // duplicate (case difference)
		{ID: "l2", Title: "Always check lint output", Kind: KindRule},           // new
		{ID: "l3", Title: "  Run  tests  before  committing  ", Kind: KindRule}, // duplicate (whitespace difference)
		{ID: "l4", Title: "Prefer table-driven tests", Kind: KindRule},          // new
	}

	kept, removed := DeduplicateLearnings(learnings, existing)
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d", len(kept))
	}
	if kept[0].ID != "l2" {
		t.Errorf("expected l2 first, got %s", kept[0].ID)
	}
	if kept[1].ID != "l4" {
		t.Errorf("expected l4 second, got %s", kept[1].ID)
	}
}

func TestDeduplicateLearnings_NoExisting(t *testing.T) {
	learnings := []Learning{
		{ID: "l1", Title: "learning one"},
		{ID: "l2", Title: "learning two"},
	}
	kept, removed := DeduplicateLearnings(learnings, nil)
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
	if len(kept) != 2 {
		t.Errorf("expected 2 kept, got %d", len(kept))
	}
}

func TestDeduplicateLearnings_TrailingPunctuation(t *testing.T) {
	existing := []string{
		"Always run tests before committing.",
	}
	learnings := []Learning{
		{ID: "l1", Title: "Always run tests before committing", Kind: KindRule}, // duplicate (trailing punctuation difference)
		{ID: "l2", Title: "New unique learning", Kind: KindRule},                // new
	}

	kept, removed := DeduplicateLearnings(learnings, existing)
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if len(kept) != 1 {
		t.Fatalf("expected 1 kept, got %d", len(kept))
	}
	if kept[0].ID != "l2" {
		t.Errorf("expected l2, got %s", kept[0].ID)
	}
}
