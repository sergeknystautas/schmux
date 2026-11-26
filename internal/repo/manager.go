package repo

import (
	"fmt"
	"io"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// Manager owns the repo entries. Every create, read, update, and delete of a
// repo entry in the daemon goes through it. The entries live in state.json;
// the manager writes them one at a time and never replaces the list.
type Manager struct {
	config *config.Config // clone paths only: ConflictingBaseNames and the bare_path repairs
	state  state.StateStore
	logger *log.Logger
	mu     sync.Mutex // serializes every write
}

// New creates a repo manager over the config and the state store.
func New(cfg *config.Config, st state.StateStore, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.NewWithOptions(io.Discard, log.Options{})
	}
	return &Manager{
		config: cfg,
		state:  st,
		logger: logger,
	}
}

// Add stores one repo entry and returns the stored entry.
//
// Under the manager's lock it takes the four steps of the design: the URL
// must be there, the name is either given or derived, the bare path is either
// given or derived, and the entry is stored and saved.
func (m *Manager) Add(r state.Repo) (state.Repo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r.URL == "" {
		return state.Repo{}, fmt.Errorf("%w: repo URL must not be empty", ErrInvalid)
	}

	entry := r
	if entry.Name == "" {
		name := m.deriveName(entry.URL)
		if name == "" {
			return state.Repo{}, fmt.Errorf("%w: cannot derive a repo name from %q", ErrInvalid, entry.URL)
		}
		entry.Name = name
	} else if _, taken := m.state.GetRepo(entry.Name); taken {
		return state.Repo{}, fmt.Errorf("%w: %q", ErrDuplicateName, entry.Name)
	}

	if entry.BarePath == "" {
		entry.BarePath = m.deriveBarePath(entry)
	}

	if err := m.state.AddRepo(entry); err != nil {
		return state.Repo{}, err
	}
	if err := m.state.Save(); err != nil {
		return state.Repo{}, err
	}
	return entry, nil
}

// deriveName returns a name for url that avoids every stored name and every
// bare base on disk that belongs to a different remote.
func (m *Manager) deriveName(url string) string {
	stored := m.state.GetRepos()
	existing := make([]string, 0, len(stored))
	for _, r := range stored {
		existing = append(existing, r.Name)
	}
	existing = append(existing, m.config.ConflictingBaseNames(url)...)
	return repoNameFromURL(url, existing)
}

// deriveBarePath returns the bare path of an entry that carries none: the
// bare path of a stored entry with the same URL, else the entry's name with
// the extension of its VCS.
func (m *Manager) deriveBarePath(entry state.Repo) string {
	if stored, ok := m.state.GetRepoByURL(entry.URL); ok && stored.BarePath != "" {
		return stored.BarePath
	}
	if entry.VCS == "sapling" {
		return entry.Name
	}
	return entry.Name + ".git"
}

// List returns every repo entry, in the order they were stored.
func (m *Manager) List() []state.Repo {
	return m.state.GetRepos()
}

// Get returns the entry stored under name.
func (m *Manager) Get(name string) (state.Repo, bool) {
	return m.state.GetRepo(name)
}

// GetByURL returns the last entry stored under url.
func (m *Manager) GetByURL(url string) (state.Repo, bool) {
	return m.state.GetRepoByURL(url)
}

// SetURL rewrites the URL of the entry named name. The other fields are kept.
func (m *Manager) SetURL(name, url string) error {
	if url == "" {
		return fmt.Errorf("%w: repo URL must not be empty", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.state.GetRepo(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	entry.URL = url
	if err := m.state.UpdateRepo(entry); err != nil {
		return err
	}
	return m.state.Save()
}

// AddOverlayPaths appends paths to one entry's overlay paths, without
// duplicates.
func (m *Manager) AddOverlayPaths(name string, paths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.state.GetRepo(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	existing := make(map[string]bool, len(entry.OverlayPaths))
	for _, p := range entry.OverlayPaths {
		existing[p] = true
	}
	for _, p := range paths {
		if !existing[p] {
			entry.OverlayPaths = append(entry.OverlayPaths, p)
			existing[p] = true
		}
	}
	if err := m.state.UpdateRepo(entry); err != nil {
		return err
	}
	return m.state.Save()
}

// DismissOverlayNudge marks one entry's overlay nudge as dismissed.
func (m *Manager) DismissOverlayNudge(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.state.GetRepo(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	entry.OverlayNudgeDismissed = true
	if err := m.state.UpdateRepo(entry); err != nil {
		return err
	}
	return m.state.Save()
}

// Remove deletes the entry named name.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.state.GetRepo(name); !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	if err := m.state.RemoveRepo(name); err != nil {
		return err
	}
	return m.state.Save()
}
