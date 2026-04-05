package lore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// PendingMergeStatus represents the lifecycle state of a pending merge.
type PendingMergeStatus string

const (
	PendingMergeStatusMerging PendingMergeStatus = "merging"
	PendingMergeStatusReady   PendingMergeStatus = "ready"
	PendingMergeStatusError   PendingMergeStatus = "error"
)

// PendingMerge represents a unified merge operation for a repo's instruction file.
type PendingMerge struct {
	Repo           string             `json:"repo"`
	Status         PendingMergeStatus `json:"status"`
	BaseSHA        string             `json:"base_sha"`
	RuleIDs        []string           `json:"rule_ids"`
	ProposalIDs    []string           `json:"proposal_ids"`
	MergedContent  string             `json:"merged_content"`
	CurrentContent string             `json:"current_content"`
	Summary        string             `json:"summary"`
	EditedContent  *string            `json:"edited_content,omitempty"`
	Error          string             `json:"error,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
}

// IsExpired returns true if the pending merge is older than 24 hours.
func (pm *PendingMerge) IsExpired() bool {
	return time.Since(pm.CreatedAt) > 24*time.Hour
}

// EffectiveContent returns EditedContent if non-nil, otherwise MergedContent.
func (pm *PendingMerge) EffectiveContent() string {
	if pm.EditedContent != nil {
		return *pm.EditedContent
	}
	return pm.MergedContent
}

// PendingMergeStore manages pending merges on disk at baseDir/<repo>/pending-merge.json.
type PendingMergeStore struct {
	baseDir string
	logger  *log.Logger
	mu      sync.Mutex
}

// NewPendingMergeStore creates a new PendingMergeStore rooted at the given directory.
func NewPendingMergeStore(baseDir string, logger *log.Logger) *PendingMergeStore {
	return &PendingMergeStore{baseDir: baseDir, logger: logger}
}

func (s *PendingMergeStore) repoDir(repo string) string {
	return filepath.Join(s.baseDir, repo)
}

func (s *PendingMergeStore) mergePath(repo string) string {
	return filepath.Join(s.repoDir(repo), "pending-merge.json")
}

// Save writes a pending merge to disk as a JSON file.
func (s *PendingMergeStore) Save(pm *PendingMerge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(pm)
}

func (s *PendingMergeStore) saveLocked(pm *PendingMerge) error {
	if err := validateRepoName(pm.Repo); err != nil {
		return err
	}
	dir := s.repoDir(pm.Repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return err
	}
	destPath := s.mergePath(pm.Repo)
	tmp, err := os.CreateTemp(dir, ".pending-merge-*.tmp")
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
	return os.Rename(tmpPath, destPath)
}

// Get reads a pending merge from disk by repo.
func (s *PendingMergeStore) Get(repo string) (*PendingMerge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(repo)
}

func (s *PendingMergeStore) getLocked(repo string) (*PendingMerge, error) {
	if err := validateRepoName(repo); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.mergePath(repo))
	if err != nil {
		return nil, err
	}
	var pm PendingMerge
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, err
	}
	return &pm, nil
}

// Delete removes a pending merge from disk.
func (s *PendingMergeStore) Delete(repo string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateRepoName(repo); err != nil {
		return err
	}
	err := os.Remove(s.mergePath(repo))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// UpdateEditedContent sets the edited content on an existing pending merge.
func (s *PendingMergeStore) UpdateEditedContent(repo string, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pm, err := s.getLocked(repo)
	if err != nil {
		return err
	}
	pm.EditedContent = &content
	return s.saveLocked(pm)
}

// InvalidateIfContainsRule deletes the pending merge if any of its RuleIDs matches the given ruleID.
func (s *PendingMergeStore) InvalidateIfContainsRule(repo, ruleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pm, err := s.getLocked(repo)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, id := range pm.RuleIDs {
		if id == ruleID {
			if err := validateRepoName(repo); err != nil {
				return err
			}
			removeErr := os.Remove(s.mergePath(repo))
			if removeErr != nil && !os.IsNotExist(removeErr) {
				return removeErr
			}
			return nil
		}
	}
	return nil
}
