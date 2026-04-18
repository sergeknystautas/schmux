package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/logging"
)

// MigrateModes walks schmuxDir and tightens file/dir modes to 0600/0700.
// Symlinks are detected via Lstat and skipped (with warning logged).
// If chmod fails on any entry and allowInsecure is false, returns the error
// and the daemon must refuse to start. If allowInsecure is true, the error
// is logged at warn and migration continues.
//
// parentLogger must be non-nil; pass d.logger (set in initConfigAndState).
func MigrateModes(schmuxDir string, allowInsecure bool, parentLogger *log.Logger) error {
	logger := logging.Sub(parentLogger, "modes-migration")

	return filepath.WalkDir(schmuxDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Use Lstat so we can detect symlinks.
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			logger.Warn("skipping symlink during mode migration", "path", path)
			return nil
		}

		var want os.FileMode
		if d.IsDir() {
			want = 0700
		} else {
			want = 0600
		}

		if info.Mode().Perm() == want {
			return nil
		}

		if err := os.Chmod(path, want); err != nil {
			msg := fmt.Sprintf("failed to chmod %s to %o: %v", path, want, err)
			if allowInsecure {
				logger.Warn(msg + " (continuing because security.allow_insecure_modes=true)")
				return nil
			}
			return fmt.Errorf("%s (set security.allow_insecure_modes=true to override)", msg)
		}
		logger.Info("tightened mode", "path", path, "from", info.Mode().Perm(), "to", want)
		return nil
	})
}
