package state

import (
	"testing"
)

func TestCopyStringSlice(t *testing.T) {
	tests := []struct {
		name string
		src  []string
	}{
		{"nil input", nil},
		{"empty input", []string{}},
		{"single element", []string{"a"}},
		{"multiple elements", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := copyStringSlice(tt.src)

			if len(tt.src) == 0 {
				if dst != nil {
					t.Errorf("expected nil for empty/nil input, got %v", dst)
				}
				return
			}

			if len(dst) != len(tt.src) {
				t.Fatalf("length mismatch: got %d, want %d", len(dst), len(tt.src))
			}
			for i := range tt.src {
				if dst[i] != tt.src[i] {
					t.Errorf("element %d: got %q, want %q", i, dst[i], tt.src[i])
				}
			}
		})
	}

	t.Run("mutation isolation", func(t *testing.T) {
		src := []string{"x", "y", "z"}
		dst := copyStringSlice(src)
		src[0] = "MUTATED"
		if dst[0] == "MUTATED" {
			t.Error("modifying source affected copy — not a deep copy")
		}
	})
}

func TestCopyConflictDiffs(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		if got := copyConflictDiffs(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := copyConflictDiffs(map[string][]string{}); got != nil {
			t.Errorf("expected nil for empty map, got %v", got)
		}
	})

	t.Run("populated map", func(t *testing.T) {
		src := map[string][]string{
			"file1.go": {"diff line 1", "diff line 2"},
			"file2.go": {"diff line 3"},
		}
		dst := copyConflictDiffs(src)

		if len(dst) != len(src) {
			t.Fatalf("length mismatch: got %d, want %d", len(dst), len(src))
		}
		for k, v := range src {
			dv, ok := dst[k]
			if !ok {
				t.Errorf("missing key %q", k)
				continue
			}
			if len(dv) != len(v) {
				t.Errorf("key %q: length %d, want %d", k, len(dv), len(v))
			}
		}
	})

	t.Run("mutation isolation - map key", func(t *testing.T) {
		src := map[string][]string{
			"a.go": {"line1"},
		}
		dst := copyConflictDiffs(src)
		src["new.go"] = []string{"added"}
		if _, ok := dst["new.go"]; ok {
			t.Error("adding key to source affected copy")
		}
	})

	t.Run("mutation isolation - slice value", func(t *testing.T) {
		src := map[string][]string{
			"a.go": {"line1", "line2"},
		}
		dst := copyConflictDiffs(src)
		src["a.go"][0] = "MUTATED"
		if dst["a.go"][0] == "MUTATED" {
			t.Error("modifying source slice affected copy — inner slices not deep-copied")
		}
	})
}

func TestCopyResolveConflicts(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		if got := CopyResolveConflicts(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := CopyResolveConflicts([]ResolveConflict{}); got != nil {
			t.Errorf("expected nil for empty slice, got %v", got)
		}
	})

	t.Run("full deep copy", func(t *testing.T) {
		createdVal := true
		src := []ResolveConflict{
			{
				Type:        "rebase",
				WorkspaceID: "ws-1",
				Status:      "in_progress",
				Hash:        "abc123",
				HashMessage: "fix bug",
				StartedAt:   "2025-01-01T00:00:00Z",
				Steps: []ResolveConflictStep{
					{
						Action:        "merge",
						Status:        "pending",
						Message:       []string{"msg1", "msg2"},
						At:            "2025-01-01T00:01:00Z",
						Files:         []string{"a.go", "b.go"},
						ConflictDiffs: map[string][]string{"a.go": {"diff1", "diff2"}},
						Created:       &createdVal,
					},
				},
				Resolutions: []ResolveConflictResolution{
					{
						LocalCommit:        "def456",
						LocalCommitMessage: "resolved",
						AllResolved:        true,
						Confidence:         "high",
						Summary:            "all good",
						Files:              []string{"a.go"},
					},
				},
			},
		}

		dst := CopyResolveConflicts(src)

		// Verify basic structure is correct
		if len(dst) != 1 {
			t.Fatalf("expected 1 conflict, got %d", len(dst))
		}
		if dst[0].Type != "rebase" {
			t.Errorf("Type: got %q, want %q", dst[0].Type, "rebase")
		}
		if dst[0].WorkspaceID != "ws-1" {
			t.Errorf("WorkspaceID: got %q, want %q", dst[0].WorkspaceID, "ws-1")
		}
		if len(dst[0].Steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(dst[0].Steps))
		}
		if len(dst[0].Resolutions) != 1 {
			t.Fatalf("expected 1 resolution, got %d", len(dst[0].Resolutions))
		}

		// Verify Created pointer is deep-copied
		if dst[0].Steps[0].Created == nil {
			t.Fatal("Created pointer is nil in copy")
		}
		if *dst[0].Steps[0].Created != true {
			t.Error("Created value wrong in copy")
		}

		// --- Mutate the source and verify copy is unaffected ---

		// Mutate scalar fields
		src[0].Type = "MUTATED"
		src[0].Status = "MUTATED"
		if dst[0].Type == "MUTATED" {
			t.Error("mutating source Type affected copy")
		}

		// Mutate step message slice
		src[0].Steps[0].Message[0] = "MUTATED"
		if dst[0].Steps[0].Message[0] == "MUTATED" {
			t.Error("mutating source step Message affected copy")
		}

		// Mutate step files slice
		src[0].Steps[0].Files[0] = "MUTATED"
		if dst[0].Steps[0].Files[0] == "MUTATED" {
			t.Error("mutating source step Files affected copy")
		}

		// Mutate conflict diffs inner slice
		src[0].Steps[0].ConflictDiffs["a.go"][0] = "MUTATED"
		if dst[0].Steps[0].ConflictDiffs["a.go"][0] == "MUTATED" {
			t.Error("mutating source ConflictDiffs affected copy")
		}

		// Mutate Created pointer value
		mutatedCreated := false
		src[0].Steps[0].Created = &mutatedCreated
		if *dst[0].Steps[0].Created != true {
			t.Error("mutating source Created pointer affected copy")
		}

		// Mutate resolution files slice
		src[0].Resolutions[0].Files[0] = "MUTATED"
		if dst[0].Resolutions[0].Files[0] == "MUTATED" {
			t.Error("mutating source resolution Files affected copy")
		}

		// Mutate resolution scalar
		src[0].Resolutions[0].AllResolved = false
		if !dst[0].Resolutions[0].AllResolved {
			t.Error("mutating source resolution AllResolved affected copy")
		}
	})

	t.Run("Created nil pointer preserved", func(t *testing.T) {
		src := []ResolveConflict{
			{
				Steps: []ResolveConflictStep{
					{Created: nil},
				},
			},
		}
		dst := CopyResolveConflicts(src)
		if dst[0].Steps[0].Created != nil {
			t.Error("nil Created should remain nil in copy")
		}
	})

	t.Run("multiple conflicts and steps", func(t *testing.T) {
		src := []ResolveConflict{
			{
				Type: "rebase",
				Steps: []ResolveConflictStep{
					{Action: "step1", Message: []string{"a"}},
					{Action: "step2", Files: []string{"b.go"}},
				},
			},
			{
				Type: "merge",
				Resolutions: []ResolveConflictResolution{
					{LocalCommit: "abc", Files: []string{"c.go"}},
				},
			},
		}
		dst := CopyResolveConflicts(src)
		if len(dst) != 2 {
			t.Fatalf("expected 2 conflicts, got %d", len(dst))
		}
		if len(dst[0].Steps) != 2 {
			t.Errorf("first conflict: expected 2 steps, got %d", len(dst[0].Steps))
		}
		if len(dst[1].Resolutions) != 1 {
			t.Errorf("second conflict: expected 1 resolution, got %d", len(dst[1].Resolutions))
		}

		// Ensure independence between conflicts
		src[1].Resolutions[0].Files[0] = "MUTATED"
		if dst[1].Resolutions[0].Files[0] == "MUTATED" {
			t.Error("mutating second conflict resolution Files affected copy")
		}
	})
}
