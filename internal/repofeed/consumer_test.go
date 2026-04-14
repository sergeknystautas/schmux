//go:build !norepofeed

package repofeed

import (
	"testing"
)

func TestConsumer_MergeDevFiles(t *testing.T) {
	c := NewConsumer(ConsumerConfig{
		OwnEmail: "me@example.com",
	})

	files := []*DeveloperFile{
		{Developer: "alice@example.com", DisplayName: "Alice", Repos: map[string]*RepoActivities{
			"schmux": {Activities: []Activity{{ID: "a1", Intent: "Auth refactor", Status: StatusActive}}},
		}},
		{Developer: "me@example.com", DisplayName: "Me", Repos: map[string]*RepoActivities{
			"schmux": {Activities: []Activity{{ID: "m1", Intent: "My work", Status: StatusActive}}},
		}},
		{Developer: "bob@example.com", DisplayName: "Bob", Repos: map[string]*RepoActivities{
			"schmux": {Activities: []Activity{{ID: "b1", Intent: "E2E tests", Status: StatusActive}}},
		}},
	}

	c.UpdateFromFiles(files)

	intents := c.GetIntentsForRepo("schmux")
	if len(intents) != 2 {
		t.Fatalf("got %d intents, want 2 (own file should be excluded)", len(intents))
	}

	// Verify own developer is excluded
	for _, intent := range intents {
		if intent.Developer == "me@example.com" {
			t.Error("own developer should be excluded")
		}
	}
}

func TestConsumer_GetAllIntents_V1Files(t *testing.T) {
	c := NewConsumer(ConsumerConfig{OwnEmail: "me@example.com"})

	files := []*DeveloperFile{
		{Developer: "alice@example.com", DisplayName: "Alice", Repos: map[string]*RepoActivities{
			"repo1": {Activities: []Activity{
				{ID: "a1", Intent: "Auth refactor", Status: StatusActive, Started: "2026-04-13"},
			}},
			"repo2": {Activities: []Activity{
				{ID: "a2", Intent: "Add tests", Status: StatusCompleted, Started: "2026-04-12"},
			}},
		}},
	}
	c.UpdateFromFiles(files)

	intents := c.GetAllIntents()
	if len(intents) != 2 {
		t.Fatalf("got %d intents, want 2", len(intents))
	}

	found := map[string]bool{}
	for _, i := range intents {
		found[i.Intent] = true
		if i.Developer != "alice@example.com" {
			t.Errorf("expected alice, got %s", i.Developer)
		}
	}
	if !found["Auth refactor"] || !found["Add tests"] {
		t.Errorf("missing expected intents: %v", found)
	}
}

func TestConsumer_GetAllIntents_V2Files(t *testing.T) {
	c := NewConsumer(ConsumerConfig{OwnEmail: "me@example.com"})

	files := []*DeveloperFile{
		{
			Version:     2,
			Developer:   "bob@example.com",
			DisplayName: "Bob",
			Intents: []Intent{
				{ID: "ws-001", IntentText: "Fixing login timeout", Status: StatusActive, LastActiveDate: "2026-04-13", Started: "2026-04-10"},
				{ID: "ws-002", IntentText: "Adding metrics", Status: StatusInactive, LastActiveDate: "2026-04-11", Started: "2026-04-09"},
			},
		},
	}
	c.UpdateFromFiles(files)

	intents := c.GetAllIntents()
	if len(intents) != 2 {
		t.Fatalf("got %d intents, want 2", len(intents))
	}

	for _, i := range intents {
		if i.Developer != "bob@example.com" {
			t.Errorf("expected bob, got %s", i.Developer)
		}
		if i.WorkspaceID == "" {
			t.Error("v2 intents should have WorkspaceID set")
		}
		if i.LastActiveDate == "" {
			t.Error("v2 intents should have LastActiveDate set")
		}
	}
}

func TestConsumer_GetAllIntents_MixedV1V2(t *testing.T) {
	c := NewConsumer(ConsumerConfig{OwnEmail: "me@example.com"})

	files := []*DeveloperFile{
		// v1 file
		{Developer: "alice@example.com", DisplayName: "Alice", Repos: map[string]*RepoActivities{
			"repo1": {Activities: []Activity{{ID: "a1", Intent: "v1 work", Status: StatusActive}}},
		}},
		// v2 file
		{Version: 2, Developer: "bob@example.com", DisplayName: "Bob", Intents: []Intent{
			{ID: "ws-001", IntentText: "v2 work", Status: StatusActive, LastActiveDate: "2026-04-13"},
		}},
	}
	c.UpdateFromFiles(files)

	intents := c.GetAllIntents()
	if len(intents) != 2 {
		t.Fatalf("got %d intents, want 2 (one v1 + one v2)", len(intents))
	}

	devs := map[string]string{}
	for _, i := range intents {
		devs[i.Developer] = i.Intent
	}
	if devs["alice@example.com"] != "v1 work" {
		t.Errorf("alice should have v1 work, got %s", devs["alice@example.com"])
	}
	if devs["bob@example.com"] != "v2 work" {
		t.Errorf("bob should have v2 work, got %s", devs["bob@example.com"])
	}
}

func TestConsumer_GetAllIntents_ExcludesOwn(t *testing.T) {
	c := NewConsumer(ConsumerConfig{OwnEmail: "me@example.com"})

	files := []*DeveloperFile{
		{Version: 2, Developer: "me@example.com", DisplayName: "Me", Intents: []Intent{
			{ID: "ws-001", IntentText: "my work", Status: StatusActive},
		}},
		{Version: 2, Developer: "other@example.com", DisplayName: "Other", Intents: []Intent{
			{ID: "ws-002", IntentText: "their work", Status: StatusActive},
		}},
	}
	c.UpdateFromFiles(files)

	intents := c.GetAllIntents()
	if len(intents) != 1 {
		t.Fatalf("got %d intents, want 1 (own should be excluded)", len(intents))
	}
	if intents[0].Developer != "other@example.com" {
		t.Errorf("expected other, got %s", intents[0].Developer)
	}
}
