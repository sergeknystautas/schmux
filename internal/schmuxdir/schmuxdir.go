package schmuxdir

import (
	"os"
	"path/filepath"
)

var dir string

// Set stores the resolved schmux directory. Called once at startup.
func Set(d string) { dir = d }

// Get returns the schmux directory. Falls back to ~/.schmux if unset.
func Get() string {
	if dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".schmux")
}

// Path helpers — centralize well-known file and directory paths under ~/.schmux/.

func ConfigPath() string    { return filepath.Join(Get(), "config.json") }
func StatePath() string     { return filepath.Join(Get(), "state.json") }
func PIDPath() string       { return filepath.Join(Get(), "daemon.pid") }
func RecordingsDir() string { return filepath.Join(Get(), "recordings") }
func BackupsDir() string    { return filepath.Join(Get(), "backups") }
