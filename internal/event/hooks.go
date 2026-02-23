package event

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed hooks/capture-failure.sh
var captureFailureScript []byte

//go:embed hooks/stop-status-check.sh
var stopStatusCheckScript []byte

//go:embed hooks/stop-lore-check.sh
var stopLoreCheckScript []byte

// EnsureGlobalHookScripts writes the global hook scripts to ~/.schmux/hooks/.
// Called once at daemon startup. Returns the absolute path to the hooks directory.
func EnsureGlobalHookScripts(homeDir string) (string, error) {
	hooksDir := filepath.Join(homeDir, ".schmux", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create hooks directory: %w", err)
	}

	scripts := map[string][]byte{
		"capture-failure.sh":   captureFailureScript,
		"stop-status-check.sh": stopStatusCheckScript,
		"stop-lore-check.sh":   stopLoreCheckScript,
	}

	for name, content := range scripts {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, content, 0755); err != nil {
			return "", fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return hooksDir, nil
}
