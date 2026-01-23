package difftool

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TempDirForWorkspace creates a temp directory prefixing with the workspace ID.
func TempDirForWorkspace(workspaceID string) (string, error) {
	return os.MkdirTemp("", fmt.Sprintf("schmux-difftool-%s-", workspaceID))
}

// CleanupWorkspaceTempDirs removes any temp dirs created for the workspace.
func CleanupWorkspaceTempDirs(workspaceID string) error {
	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("schmux-difftool-%s-*", workspaceID))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, match := range matches {
		_ = os.RemoveAll(match)
	}
	return nil
}

// SweepAndScheduleTempDirs removes expired temp dirs and schedules cleanup for the rest.
func SweepAndScheduleTempDirs(cleanupAfter time.Duration, logger func(string, ...interface{})) (deleted, scheduled int) {
	if cleanupAfter <= 0 {
		return 0, 0
	}
	pattern := filepath.Join(os.TempDir(), "schmux-difftool-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		if logger != nil {
			logger("[difftool] failed to glob temp dirs: %v", err)
		}
		return 0, 0
	}
	now := time.Now()
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		if age >= cleanupAfter {
			if err := os.RemoveAll(match); err != nil && logger != nil {
				logger("[difftool] failed to remove temp dir: %v", err)
			}
			deleted++
			continue
		}
		delay := cleanupAfter - age
		time.AfterFunc(delay, func() {
			if err := os.RemoveAll(match); err != nil && logger != nil {
				logger("[difftool] failed to remove temp dir: %v", err)
			}
		})
		scheduled++
	}
	return deleted, scheduled
}
