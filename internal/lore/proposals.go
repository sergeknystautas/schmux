package lore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// Layer represents an instruction storage destination.
type Layer string

const (
	LayerRepoPublic  Layer = "repo_public"
	LayerRepoPrivate Layer = "repo_private"
	LayerUserGlobal  Layer = "user_global"
)

// RuleStatus represents the review state of an individual rule.
type RuleStatus string

const (
	RulePending   RuleStatus = "pending"
	RuleApproved  RuleStatus = "approved"
	RuleDismissed RuleStatus = "dismissed"
)

// Rule is a discrete, self-contained instruction extracted by the curator.
type Rule struct {
	ID             string     `json:"id"`
	Text           string     `json:"text"`
	Category       string     `json:"category"`
	SuggestedLayer Layer      `json:"suggested_layer"`
	ChosenLayer    *Layer     `json:"chosen_layer,omitempty"`
	Status         RuleStatus `json:"status"`
	SourceEntries  []string   `json:"source_entries"`
	MergedAt       *time.Time `json:"merged_at,omitempty"`
}

// EffectiveLayer returns ChosenLayer if set, otherwise SuggestedLayer.
func (r *Rule) EffectiveLayer() Layer {
	if r.ChosenLayer != nil {
		return *r.ChosenLayer
	}
	return r.SuggestedLayer
}

// RuleUpdate holds optional fields for updating a rule.
type RuleUpdate struct {
	Status      RuleStatus
	Text        *string
	ChosenLayer *Layer
}

// ProposalStatus represents the lifecycle state of a proposal.
type ProposalStatus string

const (
	ProposalPending   ProposalStatus = "pending"
	ProposalMerging   ProposalStatus = "merging"
	ProposalApplied   ProposalStatus = "applied"
	ProposalDismissed ProposalStatus = "dismissed"
)

// Proposal represents a curation run's output: a set of discrete rules.
type Proposal struct {
	ID        string         `json:"id"`
	Repo      string         `json:"repo"`
	CreatedAt time.Time      `json:"created_at"`
	Status    ProposalStatus `json:"status"`
	Rules     []Rule         `json:"rules"`
	Discarded []string       `json:"discarded,omitempty"`

	// Merge preview state — populated by the background merge job.
	MergePreviews []MergePreview `json:"merge_previews,omitempty"`
	MergeError    string         `json:"merge_error,omitempty"`

	// Deprecated: v1 fields kept for backward compatibility during migration.
	// These will be removed in the cleanup task once all consumers are updated.
	SourceCount      int               `json:"source_count,omitempty"`
	Sources          []string          `json:"sources,omitempty"`
	FileHashes       map[string]string `json:"file_hashes,omitempty"`
	CurrentFiles     map[string]string `json:"current_files,omitempty"`
	ProposedFiles    map[string]string `json:"proposed_files,omitempty"`
	DiffSummary      string            `json:"diff_summary,omitempty"`
	EntriesUsed      []string          `json:"entries_used,omitempty"`
	EntriesDiscarded map[string]string `json:"entries_discarded,omitempty"`
}

// MergePreview holds a per-layer merge result for the frontend.
type MergePreview struct {
	Layer          Layer  `json:"layer"`
	CurrentContent string `json:"current_content"`
	MergedContent  string `json:"merged_content"`
	Summary        string `json:"summary"`
}

// AllRulesResolved returns true if every rule is approved or dismissed.
func (p *Proposal) AllRulesResolved() bool {
	for _, r := range p.Rules {
		if r.Status == RulePending {
			return false
		}
	}
	return true
}

// ApprovedRulesByLayer groups approved rules by their effective layer.
func (p *Proposal) ApprovedRulesByLayer() map[Layer][]Rule {
	groups := make(map[Layer][]Rule)
	for _, r := range p.Rules {
		if r.Status == RuleApproved {
			groups[r.EffectiveLayer()] = append(groups[r.EffectiveLayer()], r)
		}
	}
	return groups
}

// ProposalStore manages proposals on disk at baseDir/<repo>/<id>.json.
type ProposalStore struct {
	baseDir string
	logger  *log.Logger
	mu      sync.Mutex
}

// NewProposalStore creates a new ProposalStore rooted at the given directory.
func NewProposalStore(baseDir string, logger *log.Logger) *ProposalStore {
	return &ProposalStore{baseDir: baseDir, logger: logger}
}

func (s *ProposalStore) repoDir(repo string) string {
	return filepath.Join(s.baseDir, repo)
}

func (s *ProposalStore) proposalPath(repo, id string) string {
	return filepath.Join(s.repoDir(repo), id+".json")
}

// validateProposalID rejects IDs containing path separators or directory traversal components.
func validateProposalID(id string) error {
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid proposal ID: %s", id)
	}
	return nil
}

// validateRepoName rejects repo names containing path traversal components.
func validateRepoName(repo string) error {
	if strings.Contains(repo, "..") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "\\") {
		return fmt.Errorf("invalid repo name: %s", repo)
	}
	return nil
}

// Save writes a proposal to disk as a JSON file.
func (s *ProposalStore) Save(p *Proposal) error {
	if err := validateRepoName(p.Repo); err != nil {
		return err
	}
	if err := validateProposalID(p.ID); err != nil {
		return err
	}
	dir := s.repoDir(p.Repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	destPath := s.proposalPath(p.Repo, p.ID)
	tmp, err := os.CreateTemp(dir, ".proposals-*.tmp")
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

// Get reads a proposal from disk by repo and ID.
func (s *ProposalStore) Get(repo, id string) (*Proposal, error) {
	if err := validateRepoName(repo); err != nil {
		return nil, err
	}
	if err := validateProposalID(id); err != nil {
		return nil, err
	}
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
	if err := validateRepoName(repo); err != nil {
		return nil, err
	}
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
			if s.logger != nil {
				s.logger.Warn("skipping malformed proposal", "file", entry.Name(), "err", err)
			}
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
	if err := validateProposalID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.Get(repo, id)
	if err != nil {
		return err
	}
	p.Status = status
	return s.Save(p)
}

// UpdateRule updates a specific rule within a proposal.
func (s *ProposalStore) UpdateRule(repo, proposalID, ruleID string, update RuleUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.Get(repo, proposalID)
	if err != nil {
		return err
	}
	found := false
	for i := range p.Rules {
		if p.Rules[i].ID == ruleID {
			if update.Status != "" {
				p.Rules[i].Status = update.Status
			}
			if update.Text != nil {
				p.Rules[i].Text = *update.Text
			}
			if update.ChosenLayer != nil {
				p.Rules[i].ChosenLayer = update.ChosenLayer
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("rule %s not found in proposal %s", ruleID, proposalID)
	}
	return s.Save(p)
}
