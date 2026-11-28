package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/repo"
	"github.com/sergeknystautas/schmux/internal/state"
)

// RemoveRepo removes the repo entry named name and deletes its clones. A repo
// that any workspace uses is rejected with ErrInUse and nothing is removed.
// The in-use check, the entry removal, and the clone deletion run under the
// repo's lock, so a spawn that is mid-flight finishes first and is then seen
// by the check. Clone-deletion errors are logged, not returned.
func (m *Manager) RemoveRepo(name string) error {
	entry, found := m.repos.Get(name)
	if !found {
		return fmt.Errorf("%w: %q", repo.ErrNotFound, name)
	}

	lock := m.repoLock(entry.URL)
	lock.Lock()
	defer lock.Unlock()

	var ids []string
	for _, ws := range m.state.GetWorkspaces() {
		if ws.Repo == entry.URL {
			ids = append(ids, ws.ID)
		}
	}
	if len(ids) > 0 {
		return fmt.Errorf("%w: %s", ErrInUse, strings.Join(ids, ", "))
	}

	if err := m.repos.Remove(name); err != nil {
		return err
	}
	if err := m.deleteRepoClones(entry); err != nil {
		m.logger.Warn("failed to delete repo clones", "repo", name, "err", err)
	}
	return nil
}

// deleteRepoClones deletes the worktree base and the origin query repo of a
// removed git repo. Sapling bases can be user-managed directories schmux did
// not create, and local repos have no clone, so both are left alone.
func (m *Manager) deleteRepoClones(entry state.Repo) error {
	if entry.VCS == "sapling" || isLocalRepoURL(entry.URL) {
		return nil
	}

	var errs error
	for _, p := range []string{
		filepath.Join(m.config.GetWorktreeBasePath(), entry.BarePath),
		filepath.Join(m.config.GetQueryRepoPath(), entry.BarePath),
	} {
		if err := os.RemoveAll(p); err != nil {
			errs = errors.Join(errs, fmt.Errorf("remove repo clone %s: %w", p, err))
			continue
		}
		m.logger.Info("removed repo clone", "url", entry.URL, "path", p)
	}
	return errs
}
