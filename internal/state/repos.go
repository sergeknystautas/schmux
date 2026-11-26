package state

import (
	"errors"
	"fmt"
)

var (
	// ErrRepoNotFound is returned when no repo entry has the given name.
	ErrRepoNotFound = errors.New("repo not found")
	// ErrRepoDuplicate is returned when an entry with the same name is stored.
	ErrRepoDuplicate = errors.New("repo already exists")
)

// Repo represents a repository entry. The daemon creates and changes these
// records while it runs, so they live in state.json rather than in the
// user's settings document. The fields and their JSON names are the ones
// config.json used.
type Repo struct {
	Name                  string   `json:"name"`
	URL                   string   `json:"url"`
	BarePath              string   `json:"bare_path,omitempty"`
	VCS                   string   `json:"vcs,omitempty"`
	OverlayPaths          []string `json:"overlay_paths,omitempty"`
	OverlayNudgeDismissed bool     `json:"overlay_nudge_dismissed,omitempty"`
	GitHubLogin           string   `json:"github_login,omitempty"`
}

// copyRepo returns a copy of the entry, with its own copy of OverlayPaths.
func copyRepo(r Repo) Repo {
	r.OverlayPaths = copyStringSlice(r.OverlayPaths)
	return r
}

// GetRepos returns every repo entry, in the order they were stored.
func (s *State) GetRepos() []Repo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repos := make([]Repo, len(s.Repos))
	for i, r := range s.Repos {
		repos[i] = copyRepo(r)
	}
	return repos
}

// GetRepo returns the entry stored under name.
func (s *State) GetRepo(name string) (Repo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.Repos {
		if r.Name == name {
			return copyRepo(r), true
		}
	}
	return Repo{}, false
}

// GetRepoByURL returns the last entry stored under url.
func (s *State) GetRepoByURL(url string) (Repo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var found Repo
	ok := false
	for _, r := range s.Repos {
		if r.URL == url {
			found = copyRepo(r)
			ok = true
		}
	}
	return found, ok
}

// AddRepo appends an entry. It fails when an entry with the same name is
// already stored.
func (s *State) AddRepo(r Repo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.Repos {
		if existing.Name == r.Name {
			return fmt.Errorf("%w: %q", ErrRepoDuplicate, r.Name)
		}
	}
	s.Repos = append(s.Repos, copyRepo(r))
	return nil
}

// UpdateRepo replaces the entry stored under the same name. It fails when
// there is no such entry.
func (s *State) UpdateRepo(r Repo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Repos {
		if s.Repos[i].Name == r.Name {
			s.Repos[i] = copyRepo(r)
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrRepoNotFound, r.Name)
}

// RemoveRepo removes the entry stored under name. It fails when there is no
// such entry.
func (s *State) RemoveRepo(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Repos {
		if s.Repos[i].Name == name {
			s.Repos = append(s.Repos[:i], s.Repos[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrRepoNotFound, name)
}
