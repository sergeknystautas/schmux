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

