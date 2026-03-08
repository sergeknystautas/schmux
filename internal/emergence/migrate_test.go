package emergence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestMigrateFromActions(t *testing.T) {
	t.Parallel()

	t.Run("migrates pinned actions", func(t *testing.T) {
		t.Parallel()
		actionsDir := t.TempDir()
		emergenceDir := t.TempDir()

		now := time.Now().UTC().Truncate(time.Second)
		lastUsed := now.Add(-time.Hour)

		// Write a legacy registry.json with mixed states
		repoDir := filepath.Join(actionsDir, "my-repo")
		os.MkdirAll(repoDir, 0755)

		actions := []map[string]interface{}{
			{
				"id":        "act-001",
				"name":      "Run tests",
				"type":      "command",
				"command":   "go test ./...",
				"source":    "manual",
				"state":     "pinned",
				"use_count": 5,
				"last_used": lastUsed.Format(time.RFC3339),
			},
			{
				"id":       "act-002",
				"name":     "Fix lint",
				"type":     "agent",
				"template": "Fix all lint errors in the project",
				"target":   "claude-code",
				"source":   "emerged",
				"state":    "pinned",
			},
			{
				"id":     "act-003",
				"name":   "Dismissed action",
				"type":   "agent",
				"source": "manual",
				"state":  "dismissed",
			},
			{
				"id":     "act-004",
				"name":   "Proposed action",
				"type":   "agent",
				"source": "emerged",
				"state":  "proposed",
			},
		}

		data, _ := json.MarshalIndent(actions, "", "  ")
		os.WriteFile(filepath.Join(repoDir, "registry.json"), data, 0644)

		store := NewStore(emergenceDir)
		count, err := MigrateFromActions(actionsDir, store)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("migrated %d, want 2", count)
		}

		// Verify the migrated entries
		entries, err := store.List("my-repo")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("got %d pinned entries, want 2", len(entries))
		}

		// Find the command entry
		var cmdEntry, agentEntry contracts.SpawnEntry
		for _, e := range entries {
			if e.Name == "Run tests" {
				cmdEntry = e
			}
			if e.Name == "Fix lint" {
				agentEntry = e
			}
		}

		if cmdEntry.Type != contracts.SpawnEntryCommand {
			t.Errorf("command entry type = %q, want %q", cmdEntry.Type, contracts.SpawnEntryCommand)
		}
		if cmdEntry.Command != "go test ./..." {
			t.Errorf("command = %q, want %q", cmdEntry.Command, "go test ./...")
		}
		if string(cmdEntry.Source) != "manual" {
			t.Errorf("source = %q, want %q", cmdEntry.Source, "manual")
		}
		if cmdEntry.UseCount != 5 {
			t.Errorf("use_count = %d, want 5", cmdEntry.UseCount)
		}

		if agentEntry.Type != contracts.SpawnEntryAgent {
			t.Errorf("agent entry type = %q, want %q", agentEntry.Type, contracts.SpawnEntryAgent)
		}
		if agentEntry.Prompt != "Fix all lint errors in the project" {
			t.Errorf("prompt = %q, want %q", agentEntry.Prompt, "Fix all lint errors in the project")
		}
		if agentEntry.Target != "claude-code" {
			t.Errorf("target = %q, want %q", agentEntry.Target, "claude-code")
		}
		if string(agentEntry.Source) != "emerged" {
			t.Errorf("source = %q, want %q", agentEntry.Source, "emerged")
		}
	})

	t.Run("skips empty actions dir", func(t *testing.T) {
		t.Parallel()
		actionsDir := t.TempDir()
		emergenceDir := t.TempDir()
		store := NewStore(emergenceDir)

		count, err := MigrateFromActions(actionsDir, store)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("migrated %d, want 0", count)
		}
	})

	t.Run("skips nonexistent actions dir", func(t *testing.T) {
		t.Parallel()
		emergenceDir := t.TempDir()
		store := NewStore(emergenceDir)

		count, err := MigrateFromActions("/nonexistent/path", store)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("migrated %d, want 0", count)
		}
	})

	t.Run("migrates source correctly", func(t *testing.T) {
		t.Parallel()
		actionsDir := t.TempDir()
		emergenceDir := t.TempDir()

		repoDir := filepath.Join(actionsDir, "test-repo")
		os.MkdirAll(repoDir, 0755)

		actions := []map[string]interface{}{
			{
				"id":     "act-010",
				"name":   "Migrated action",
				"type":   "command",
				"source": "migrated",
				"state":  "pinned",
			},
		}
		data, _ := json.MarshalIndent(actions, "", "  ")
		os.WriteFile(filepath.Join(repoDir, "registry.json"), data, 0644)

		store := NewStore(emergenceDir)
		count, err := MigrateFromActions(actionsDir, store)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("migrated %d, want 1", count)
		}

		entries, _ := store.List("test-repo")
		if len(entries) != 1 {
			t.Fatalf("got %d entries, want 1", len(entries))
		}
		// "migrated" source maps to "manual"
		if string(entries[0].Source) != "manual" {
			t.Errorf("source = %q, want %q", entries[0].Source, "manual")
		}
	})
}
