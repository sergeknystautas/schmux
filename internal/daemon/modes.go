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
// The repos/ subtree (bare clones and Sapling/EdenFS working copies) is
// crossed only at its top entry — descending would force materialization of
// virtual monorepo mounts and rewrite permissions on upstream code that
// schmux does not own. workspacePath, when non-empty and located inside
// schmuxDir, is treated the same way: workspaces can themselves be VCS
// working copies (including EdenFS mounts) and must not be recursed into.
// Files keep their owner exec bit so generated hook scripts stay executable;
// group/other bits are always stripped.
// If chmod fails on any entry and allowInsecure is false, returns the error
// and the daemon must refuse to start. If allowInsecure is true, the error
// is logged at warn and migration continues.
//
// parentLogger must be non-nil; pass d.logger (set in initConfigAndState).
func MigrateModes(schmuxDir string, workspacePath string, allowInsecure bool, parentLogger *log.Logger) error {
	logger := logging.Sub(parentLogger, "modes-migration")
	reposDir := filepath.Join(schmuxDir, "repos")
	workspacesBoundary := resolveBoundary(schmuxDir, workspacePath)

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
			// Preserve owner exec bit so executable scripts stay runnable.
			want = 0600 | (info.Mode().Perm() & 0100)
		}

		if info.Mode().Perm() != want {
			if err := os.Chmod(path, want); err != nil {
				msg := fmt.Sprintf("failed to chmod %s to %o: %v", path, want, err)
				if allowInsecure {
					logger.Warn(msg + " (continuing because security.allow_insecure_modes=true)")
				} else {
					return fmt.Errorf("%s (set security.allow_insecure_modes=true to override)", msg)
				}
			} else {
				logger.Info("tightened mode", "path", path, "from", info.Mode().Perm(), "to", want)
			}
		}

		// Stop at boundaries (repos/ and the configured workspace path)
		// after tightening the entry itself.
		if d.IsDir() && (path == reposDir || (workspacesBoundary != "" && path == workspacesBoundary)) {
			return filepath.SkipDir
		}
		return nil
	})
}

// resolveBoundary returns p as an absolute path if it is non-empty and
// located inside schmuxDir; otherwise returns "". A boundary outside
// schmuxDir is unreachable by the walk and would never match, so we drop it.
func resolveBoundary(schmuxDir, p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(schmuxDir, abs)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) ||
		(len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)) {
		return ""
	}
	return abs
}
