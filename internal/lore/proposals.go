package lore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ProposalStatus represents the lifecycle state of a proposal.
type ProposalStatus string

const (
	ProposalPending   ProposalStatus = "pending"
	ProposalStale     ProposalStatus = "stale"
	ProposalApplied   ProposalStatus = "applied"
	ProposalDismissed ProposalStatus = "dismissed"
)

// Proposal represents a curator-generated merge proposal.
type Proposal struct {
	ID               string            `json:"id"`
	Repo             string            `json:"repo"`
	CreatedAt        time.Time         `json:"created_at"`
	Status           ProposalStatus    `json:"status"`
	SourceCount      int               `json:"source_count"`
	Sources          []string          `json:"sources"`
	FileHashes       map[string]string `json:"file_hashes"`
	ProposedFiles    map[string]string `json:"proposed_files"`
	DiffSummary      string            `json:"diff_summary"`
	EntriesUsed      []string          `json:"entries_used"`
	EntriesDiscarded map[string]string `json:"entries_discarded,omitempty"`
}

// IsStale checks whether any instruction file has changed since the proposal was created.
func (p *Proposal) IsStale(repoDir string) (bool, error) {
	for relPath, expectedHash := range p.FileHashes {
		fullPath := filepath.Join(repoDir, relPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				return true, nil // file deleted = stale
			}
			return false, err
		}
		hash := sha256.Sum256(content)
		actualHash := "sha256:" + hex.EncodeToString(hash[:])
		if actualHash != expectedHash {
			return true, nil
		}
	}
	return false, nil
}

// ProposalStore manages proposals on disk at baseDir/<repo>/<id>.json.
type ProposalStore struct {
	baseDir string
}

// NewProposalStore creates a new ProposalStore rooted at the given directory.
func NewProposalStore(baseDir string) *ProposalStore {
	return &ProposalStore{baseDir: baseDir}
}

func (s *ProposalStore) repoDir(repo string) string {
	return filepath.Join(s.baseDir, repo)
}

func (s *ProposalStore) proposalPath(repo, id string) string {
	return filepath.Join(s.repoDir(repo), id+".json")
}

// Save writes a proposal to disk as a JSON file.
func (s *ProposalStore) Save(p *Proposal) error {
	dir := s.repoDir(p.Repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.proposalPath(p.Repo, p.ID), data, 0644)
}

// Get reads a proposal from disk by repo and ID.
func (s *ProposalStore) Get(repo, id string) (*Proposal, error) {
	data, err := os.ReadFile(s.proposalPath(repo, id))
	if err != nil {
		return nil, err
	}
	var p Proposal
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// List returns all proposals for a given repo, sorted newest first.
func (s *ProposalStore) List(repo string) ([]*Proposal, error) {
	dir := s.repoDir(repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var proposals []*Proposal
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-5] // strip .json
		p, err := s.Get(repo, id)
		if err != nil {
			fmt.Printf("[lore] skipping malformed proposal %s: %v\n", entry.Name(), err)
			continue
		}
		proposals = append(proposals, p)
	}

	// Sort newest first
	sort.Slice(proposals, func(i, j int) bool {
		return proposals[i].CreatedAt.After(proposals[j].CreatedAt)
	})
	return proposals, nil
}

// UpdateStatus updates the status of a proposal on disk.
func (s *ProposalStore) UpdateStatus(repo, id string, status ProposalStatus) error {
	p, err := s.Get(repo, id)
	if err != nil {
		return err
	}
	p.Status = status
	return s.Save(p)
}

// HashFileContent returns a "sha256:<hex>" hash of a file's content.
func HashFileContent(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}
