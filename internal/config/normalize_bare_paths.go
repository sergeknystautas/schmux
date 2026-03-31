package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/state"
)

// NormalizeBarePaths renames non-conforming bare repo directories to {name}.git
// and updates config, state, and worktree references.
// Skips Sapling repos (different base-path semantics).
// Should be called at daemon startup after config and state are loaded.
func NormalizeBarePaths(cfg *Config, st *state.State) {
	reposPath := cfg.GetWorktreeBasePath()
	queryPath := cfg.GetQueryRepoPath()
	for i := range cfg.Repos {
		repo := &cfg.Repos[i]

		// Skip Sapling repos
		if repo.VCS == "sapling" {
			continue
		}

		// Skip already-conforming repos
		canonical := repo.Name + ".git"
		if repo.BarePath == canonical {
			continue
		}

		// Skip repos with empty BarePath (populateBarePaths handles these)
		if repo.BarePath == "" {
			continue
		}

		// Try to normalize in both repos/ and query/ directories.
		// Track whether any rename succeeded — only update BarePath if so.
		renamed := false
		for _, basePath := range []string{reposPath, queryPath} {
			if basePath == "" {
				continue
			}

			oldPath := filepath.Join(basePath, repo.BarePath)
			if _, err := os.Stat(oldPath); err != nil {
				continue // Not on disk in this base path
			}

			newPath := filepath.Join(basePath, canonical)

			if _, err := os.Stat(newPath); err == nil {
				fmt.Fprintf(os.Stderr, "[config] cannot normalize repo %q: target %s already exists — rename one of the repos with duplicate name %q\n", repo.Name, newPath, repo.Name)
				continue
			}

			if err := RelocateBareRepo(oldPath, newPath); err != nil {
				fmt.Fprintf(os.Stderr, "[config] failed to normalize repo %q from %s to %s: %v\n", repo.Name, oldPath, newPath, err)
				continue
			}

			fmt.Fprintf(os.Stderr, "[config] normalized bare path for repo %q: %s → %s\n", repo.Name, repo.BarePath, canonical)
			renamed = true

			// Update state RepoBase if this is the repos/ directory
			if basePath == reposPath {
				if rb, found := st.GetRepoBaseByURL(repo.URL); found {
					rb.Path = newPath
					st.AddRepoBase(rb)
				}
			}
		}

		if renamed {
			repo.BarePath = canonical
			// Save config and state immediately after each successful normalization.
			// If the process crashes between rename and save, the next startup would
			// see a stale BarePath pointing to a directory that no longer exists.
			if err := cfg.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "[config] warning: could not save normalized bare paths: %v\n", err)
			}
			if err := st.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "[config] warning: could not save state after normalization: %v\n", err)
			}
		}
	}
}
