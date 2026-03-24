//go:build !norepofeed

package repofeed

import (
	"encoding/json"
	"testing"
)

func TestDeveloperFileRoundTrip(t *testing.T) {
	original := &DeveloperFile{
		Developer:   "alice@example.com",
		DisplayName: "Alice",
		Updated:     "2026-03-07T14:32:00Z",
		Repos: map[string]*RepoActivities{
			"schmux": {
				Activities: []Activity{
					{
						ID:           "a1b2c3",
						Intent:       "Refactoring session auth",
						Status:       StatusActive,
						Started:      "2026-03-07T13:00:00Z",
						Branches:     []string{"feature/remote-auth"},
						SessionCount: 2,
						Agents:       []string{"claude", "codex"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DeveloperFile
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Developer != original.Developer {
		t.Errorf("developer: got %q, want %q", decoded.Developer, original.Developer)
	}
	if decoded.DisplayName != original.DisplayName {
		t.Errorf("display_name: got %q, want %q", decoded.DisplayName, original.DisplayName)
	}
	if len(decoded.Repos) != 1 {
		t.Fatalf("repos: got %d, want 1", len(decoded.Repos))
	}
	repo := decoded.Repos["schmux"]
	if len(repo.Activities) != 1 {
		t.Fatalf("activities: got %d, want 1", len(repo.Activities))
	}
	act := repo.Activities[0]
	if act.Status != StatusActive {
		t.Errorf("status: got %q, want %q", act.Status, StatusActive)
	}
	if len(act.Agents) != 2 {
		t.Errorf("agents: got %d, want 2", len(act.Agents))
	}
}

func TestActivityStatus_Valid(t *testing.T) {
	for _, s := range []ActivityStatus{StatusActive, StatusInactive, StatusCompleted} {
		if !s.Valid() {
			t.Errorf("status %q should be valid", s)
		}
	}
	if ActivityStatus("bogus").Valid() {
		t.Error("bogus should be invalid")
	}
}
