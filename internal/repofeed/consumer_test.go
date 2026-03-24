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
