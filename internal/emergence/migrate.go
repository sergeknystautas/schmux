package emergence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// legacyAction is the old action registry format.
type legacyAction struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Template string `json:"template,omitempty"`
	Command  string `json:"command,omitempty"`
	Target   string `json:"target,omitempty"`
	Source   string `json:"source"`
	State    string `json:"state"`
	UseCount int    `json:"use_count,omitempty"`
	LastUsed string `json:"last_used,omitempty"`
}

// MigrateFromActions reads old action registries and creates spawn entries.
// Only pinned actions are migrated. Returns the number of entries migrated.
func MigrateFromActions(actionsDir string, store *Store) (int, error) {
	entries, err := os.ReadDir(actionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	total := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoName := entry.Name()
		registryPath := filepath.Join(actionsDir, repoName, "registry.json")

		data, err := os.ReadFile(registryPath)
		if err != nil {
			continue // skip repos with no registry
		}

		var actions []legacyAction
		if err := json.Unmarshal(data, &actions); err != nil {
			continue // skip malformed registries
		}

		var spawnEntries []contracts.SpawnEntry
		for _, a := range actions {
			if a.State != "pinned" {
				continue
			}

			se := contracts.SpawnEntry{
				ID:       store.GenerateID(),
				Name:     a.Name,
				Source:   mapSource(a.Source),
				State:    contracts.SpawnStatePinned,
				UseCount: a.UseCount,
			}

			switch a.Type {
			case "command", "shell":
				se.Type = contracts.SpawnEntryCommand
				se.Command = a.Command
			case "agent":
				se.Type = contracts.SpawnEntryAgent
				se.Prompt = a.Template
				se.Target = a.Target
			default:
				se.Type = contracts.SpawnEntryAgent
				se.Prompt = a.Template
			}

			if a.LastUsed != "" {
				if t, err := time.Parse(time.RFC3339, a.LastUsed); err == nil {
					se.LastUsed = &t
				}
			}

			spawnEntries = append(spawnEntries, se)
		}

		if len(spawnEntries) > 0 {
			if err := store.Import(repoName, spawnEntries); err != nil {
				return total, err
			}
			total += len(spawnEntries)
		}
	}

	return total, nil
}

func mapSource(source string) contracts.SpawnEntrySource {
	switch source {
	case "emerged":
		return contracts.SpawnSourceEmerged
	default:
		return contracts.SpawnSourceManual
	}
}
