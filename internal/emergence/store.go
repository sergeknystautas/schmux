package emergence

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// Store manages spawn entries per repository.
type Store struct {
	mu      sync.Mutex
	entries map[string][]contracts.SpawnEntry // keyed by repo name
	baseDir string
}

// NewStore creates a new emergence store.
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir: baseDir,
		entries: make(map[string][]contracts.SpawnEntry),
	}
}

func (s *Store) filePath(repo string) string {
	return filepath.Join(s.baseDir, repo, "spawn-entries.json")
}

func (s *Store) load(repo string) error {
	if _, ok := s.entries[repo]; ok {
		return nil // already loaded
	}
	data, err := os.ReadFile(s.filePath(repo))
	if err != nil {
		if os.IsNotExist(err) {
			s.entries[repo] = nil
			return nil
		}
		return fmt.Errorf("read spawn entries: %w", err)
	}
	var entries []contracts.SpawnEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse spawn entries: %w", err)
	}

	// Dedup migration: remove duplicate names in the same active state.
	seen := make(map[string]bool, len(entries))
	deduped := entries[:0]
	for _, e := range entries {
		if e.State == contracts.SpawnStateProposed || e.State == contracts.SpawnStatePinned {
			if seen[e.Name] {
				continue
			}
			seen[e.Name] = true
		}
		deduped = append(deduped, e)
	}
	s.entries[repo] = deduped
	if len(deduped) < len(entries) {
		// Persist the cleaned-up list.
		s.save(repo)
	}
	return nil
}

func (s *Store) save(repo string) error {
	dir := filepath.Dir(s.filePath(repo))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create emergence dir: %w", err)
	}
	data, err := json.MarshalIndent(s.entries[repo], "", "  ")
	if err != nil {
		return fmt.Errorf("marshal spawn entries: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".spawn-entries-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	return os.Rename(tmpPath, s.filePath(repo))
}

func (s *Store) generateID() string {
	b := make([]byte, 8) // 8 bytes = 16 hex chars
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("se-%x", time.Now().UnixNano())
	}
	return "se-" + hex.EncodeToString(b)
}

// GenerateID returns a new unique spawn entry ID.
func (s *Store) GenerateID() string {
	return s.generateID()
}

// List returns pinned entries sorted by use_count descending.
func (s *Store) List(repo string) ([]contracts.SpawnEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return nil, err
	}
	var result []contracts.SpawnEntry
	for _, e := range s.entries[repo] {
		if e.State == contracts.SpawnStatePinned {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UseCount > result[j].UseCount
	})
	return result, nil
}

// ListAll returns all entries regardless of state.
func (s *Store) ListAll(repo string) ([]contracts.SpawnEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return nil, err
	}
	result := make([]contracts.SpawnEntry, len(s.entries[repo]))
	copy(result, s.entries[repo])
	return result, nil
}

// Get returns an entry by ID.
func (s *Store) Get(repo, id string) (contracts.SpawnEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return contracts.SpawnEntry{}, false, err
	}
	for _, e := range s.entries[repo] {
		if e.ID == id {
			return e, true, nil
		}
	}
	return contracts.SpawnEntry{}, false, nil
}

// Create adds a new manual, pinned spawn entry.
func (s *Store) Create(repo string, req contracts.CreateSpawnEntryRequest) (contracts.SpawnEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return contracts.SpawnEntry{}, err
	}
	entry := contracts.SpawnEntry{
		ID:      s.generateID(),
		Name:    req.Name,
		Type:    req.Type,
		Source:  contracts.SpawnSourceManual,
		State:   contracts.SpawnStatePinned,
		Command: req.Command,
		Prompt:  req.Prompt,
		Target:  req.Target,
	}
	s.entries[repo] = append(s.entries[repo], entry)
	if err := s.save(repo); err != nil {
		s.entries[repo] = s.entries[repo][:len(s.entries[repo])-1]
		return contracts.SpawnEntry{}, err
	}
	return entry, nil
}

// Update partially updates an entry by ID.
func (s *Store) Update(repo, id string, req contracts.UpdateSpawnEntryRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}
	for i := range s.entries[repo] {
		if s.entries[repo][i].ID == id {
			if req.Name != nil {
				s.entries[repo][i].Name = *req.Name
			}
			if req.Command != nil {
				s.entries[repo][i].Command = *req.Command
			}
			if req.Prompt != nil {
				s.entries[repo][i].Prompt = *req.Prompt
			}
			if req.Target != nil {
				s.entries[repo][i].Target = *req.Target
			}
			return s.save(repo)
		}
	}
	return fmt.Errorf("spawn entry not found: %s", id)
}

// Delete removes an entry by ID.
func (s *Store) Delete(repo, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}
	for i, e := range s.entries[repo] {
		if e.ID == id {
			s.entries[repo] = append(s.entries[repo][:i], s.entries[repo][i+1:]...)
			return s.save(repo)
		}
	}
	return fmt.Errorf("spawn entry not found: %s", id)
}

// Pin changes a proposed entry's state to pinned.
func (s *Store) Pin(repo, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}
	for i := range s.entries[repo] {
		if s.entries[repo][i].ID == id {
			s.entries[repo][i].State = contracts.SpawnStatePinned
			return s.save(repo)
		}
	}
	return fmt.Errorf("spawn entry not found: %s", id)
}

// Dismiss changes a proposed entry's state to dismissed.
func (s *Store) Dismiss(repo, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}
	for i := range s.entries[repo] {
		if s.entries[repo][i].ID == id {
			s.entries[repo][i].State = contracts.SpawnStateDismissed
			return s.save(repo)
		}
	}
	return fmt.Errorf("spawn entry not found: %s", id)
}

// RecordUse increments use_count and sets last_used.
func (s *Store) RecordUse(repo, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}
	for i := range s.entries[repo] {
		if s.entries[repo][i].ID == id {
			s.entries[repo][i].UseCount++
			now := time.Now()
			s.entries[repo][i].LastUsed = &now
			return s.save(repo)
		}
	}
	return fmt.Errorf("spawn entry not found: %s", id)
}

// AddProposed adds proposed entries in bulk with state=proposed, source=emerged.
// Entries whose Name matches an existing proposed or pinned entry are skipped.
func (s *Store) AddProposed(repo string, entries []contracts.SpawnEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}

	// Build set of existing names (proposed or pinned) for dedup.
	existing := make(map[string]bool, len(s.entries[repo]))
	for _, e := range s.entries[repo] {
		if e.State == contracts.SpawnStateProposed || e.State == contracts.SpawnStatePinned {
			existing[e.Name] = true
		}
	}

	var toAdd []contracts.SpawnEntry
	for i := range entries {
		if existing[entries[i].Name] {
			continue
		}
		entries[i].State = contracts.SpawnStateProposed
		entries[i].Source = contracts.SpawnSourceEmerged
		toAdd = append(toAdd, entries[i])
		existing[entries[i].Name] = true // prevent intra-batch duplicates
	}
	if len(toAdd) == 0 {
		return nil
	}

	s.entries[repo] = append(s.entries[repo], toAdd...)
	if err := s.save(repo); err != nil {
		s.entries[repo] = s.entries[repo][:len(s.entries[repo])-len(toAdd)]
		return err
	}
	return nil
}

// ProposedAndPinnedNames returns the names of all proposed and pinned entries for a repo.
func (s *Store) ProposedAndPinnedNames(repo string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return nil
	}
	var names []string
	for _, e := range s.entries[repo] {
		if e.State == contracts.SpawnStateProposed || e.State == contracts.SpawnStatePinned {
			names = append(names, e.Name)
		}
	}
	return names
}

// Import adds entries preserving their existing state and source.
// Used for migration from legacy action registries.
func (s *Store) Import(repo string, entries []contracts.SpawnEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(repo); err != nil {
		return err
	}
	s.entries[repo] = append(s.entries[repo], entries...)
	if err := s.save(repo); err != nil {
		s.entries[repo] = s.entries[repo][:len(s.entries[repo])-len(entries)]
		return err
	}
	return nil
}
