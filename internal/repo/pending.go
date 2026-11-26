package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sergeknystautas/schmux/internal/state"
)

// AddPending reads the pending file and adds every entry it holds. The file
// is the hand-off between the config migration, which can only record what it
// saw, and this manager, which owns the entries.
//
// A line that cannot be read is logged and skipped; the rest are added. The
// file is removed only after the state has been saved, so a failed save leaves
// the entries to be added again.
func (m *Manager) AddPending(pendingPath string) error {
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read pending repos: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry state.Repo
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			m.logger.Warn("skipping malformed pending repo entry", "err", err)
			continue
		}
		if _, err := m.Add(entry); err != nil {
			m.logger.Warn("skipping pending repo entry", "name", entry.Name, "err", err)
			continue
		}
	}

	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save pending repos: %w", err)
	}
	if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove pending repos file: %w", err)
	}
	return nil
}
