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
func AdaptersDir() string   { return filepath.Join(Get(), "adapters") }
func LogsDir() string       { return filepath.Join(Get(), "logs") }

// FenceWorkspaceDir returns the per-workspace directory holding every fenced
// session's launch subdir for that workspace. Lives outside any workspace so a
// fenced process cannot tamper with its own future respawns.
func FenceWorkspaceDir(workspaceID string) string {
	return filepath.Join(Get(), "fence", workspaceID)
}

// FenceLaunchDir returns the per-session directory (under the workspace dir)
// holding fence launch artifacts (settings.json, cmd.sh, monitor.log).
func FenceLaunchDir(workspaceID, sessionID string) string {
	return filepath.Join(FenceWorkspaceDir(workspaceID), sessionID)
}
