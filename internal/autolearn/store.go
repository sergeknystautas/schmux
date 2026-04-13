//go:build !noautolearn

package autolearn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/charmbracelet/log"
)

// BatchStore is a JSON-file-backed store for Batch objects.
// Each batch is stored as one JSON file at {baseDir}/batches/{repo}/{batchID}.json.
type BatchStore struct {
	baseDir string
	mu      sync.Mutex
	logger  *log.Logger
}

// NewBatchStore creates a new BatchStore rooted at the given base directory.
func NewBatchStore(baseDir string, logger *log.Logger) *BatchStore {
	return &BatchStore{
		baseDir: baseDir,
		logger:  logger,
	}
}

func (s *BatchStore) batchDir(repo string) string {
	return filepath.Join(s.baseDir, repo)
}

func (s *BatchStore) batchPath(repo, batchID string) string {
	return filepath.Join(s.batchDir(repo), batchID+".json")
}

// Save persists a batch to disk using atomic temp-file + rename.
func (s *BatchStore) Save(b *Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(b)
}

func (s *BatchStore) save(b *Batch) error {
	dir := s.batchDir(b.Repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create batch dir: %w", err)
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".batch-*.tmp")
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
	return os.Rename(tmpPath, s.batchPath(b.Repo, b.ID))
}

// Get retrieves a single batch by repo and batch ID.
func (s *BatchStore) Get(repo, batchID string) (*Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(repo, batchID)
}

func (s *BatchStore) load(repo, batchID string) (*Batch, error) {
	data, err := os.ReadFile(s.batchPath(repo, batchID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("batch not found: %s/%s", repo, batchID)
		}
		return nil, fmt.Errorf("read batch: %w", err)
	}
	var b Batch
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse batch: %w", err)
	}
	return &b, nil
}

// List returns all batches for a repo, sorted by CreatedAt descending.
func (s *BatchStore) List(repo string) ([]*Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(repo)
}

func (s *BatchStore) list(repo string) ([]*Batch, error) {
	dir := s.batchDir(repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read batch dir: %w", err)
	}
	var batches []*Batch
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		batchID := entry.Name()[:len(entry.Name())-len(".json")]
		b, err := s.load(repo, batchID)
		if err != nil {
			s.logger.Warn("skipping unreadable batch file", "file", entry.Name(), "err", err)
			continue
		}
		batches = append(batches, b)
	}
	sort.Slice(batches, func(i, j int) bool {
		return batches[i].CreatedAt.After(batches[j].CreatedAt)
	})
	return batches, nil
}

// UpdateStatus changes the status of a batch.
func (s *BatchStore) UpdateStatus(repo, batchID string, status BatchStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.load(repo, batchID)
	if err != nil {
		return err
	}
	b.Status = status
	return s.save(b)
}

// UpdateLearning applies a LearningUpdate to a specific learning within a batch.
func (s *BatchStore) UpdateLearning(repo, batchID, learningID string, update LearningUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.load(repo, batchID)
	if err != nil {
		return err
	}
	for i := range b.Learnings {
		if b.Learnings[i].ID == learningID {
			applyLearningUpdate(&b.Learnings[i], update)
			return s.save(b)
		}
	}
	return fmt.Errorf("learning not found: %s in batch %s", learningID, batchID)
}

func applyLearningUpdate(l *Learning, u LearningUpdate) {
	if u.Status != nil {
		l.Status = *u.Status
	}
	if u.Title != nil {
		l.Title = *u.Title
	}
	if u.Description != nil {
		l.Description = *u.Description
	}
	if u.ChosenLayer != nil {
		l.ChosenLayer = u.ChosenLayer
	}
	if u.Rule != nil {
		l.Rule = u.Rule
	}
	if u.Skill != nil {
		l.Skill = u.Skill
	}
}

// PendingLearningTitles returns titles of all pending learnings across all batches for a repo.
func (s *BatchStore) PendingLearningTitles(repo string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	batches, err := s.list(repo)
	if err != nil {
		return nil
	}
	var titles []string
	for _, b := range batches {
		for _, l := range b.Learnings {
			if l.Status == StatusPending {
				titles = append(titles, l.Title)
			}
		}
	}
	return titles
}

// DismissedLearningTitles returns titles of all dismissed learnings across all batches for a repo.
func (s *BatchStore) DismissedLearningTitles(repo string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	batches, err := s.list(repo)
	if err != nil {
		return nil
	}
	var titles []string
	for _, b := range batches {
		for _, l := range b.Learnings {
			if l.Status == StatusDismissed {
				titles = append(titles, l.Title)
			}
		}
	}
	return titles
}
