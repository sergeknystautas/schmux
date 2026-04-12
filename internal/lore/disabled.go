//go:build nolore

package lore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/charmbracelet/log"
)

// --- String types + constants ---

type Layer string

const (
	LayerRepoPublic       Layer = "repo_public"
	LayerRepoPrivate      Layer = "repo_private"
	LayerCrossRepoPrivate Layer = "cross_repo_private"
)

type RuleStatus string

const (
	RulePending   RuleStatus = "pending"
	RuleApproved  RuleStatus = "approved"
	RuleDismissed RuleStatus = "dismissed"
)

type ProposalStatus string

const (
	ProposalPending   ProposalStatus = "pending"
	ProposalMerging   ProposalStatus = "merging"
	ProposalApplied   ProposalStatus = "applied"
	ProposalDismissed ProposalStatus = "dismissed"
)

type PendingMergeStatus string

const (
	PendingMergeStatusMerging PendingMergeStatus = "merging"
	PendingMergeStatusReady   PendingMergeStatus = "ready"
	PendingMergeStatusError   PendingMergeStatus = "error"
)

// --- Structs ---

type RuleSourceEntry struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	InputSummary string `json:"input_summary,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
	Tool         string `json:"tool,omitempty"`
}

type Rule struct {
	ID             string            `json:"id"`
	Text           string            `json:"text"`
	Category       string            `json:"category"`
	SuggestedLayer Layer             `json:"suggested_layer"`
	ChosenLayer    *Layer            `json:"chosen_layer,omitempty"`
	Status         RuleStatus        `json:"status"`
	SourceEntries  []RuleSourceEntry `json:"source_entries"`
	MergedAt       *time.Time        `json:"merged_at,omitempty"`
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	type Alias Rule
	return json.Unmarshal(data, (*Alias)(r))
}

func (r *Rule) EffectiveLayer() Layer {
	if r.ChosenLayer != nil {
		return *r.ChosenLayer
	}
	return r.SuggestedLayer
}

type RuleUpdate struct {
	Status      RuleStatus
	Text        *string
	ChosenLayer *Layer
}

type Proposal struct {
	ID               string            `json:"id"`
	Repo             string            `json:"repo"`
	CreatedAt        time.Time         `json:"created_at"`
	Status           ProposalStatus    `json:"status"`
	Rules            []Rule            `json:"rules"`
	Discarded        []string          `json:"discarded,omitempty"`
	SourceCount      int               `json:"source_count,omitempty"`
	Sources          []string          `json:"sources,omitempty"`
	FileHashes       map[string]string `json:"file_hashes,omitempty"`
	CurrentFiles     map[string]string `json:"current_files,omitempty"`
	ProposedFiles    map[string]string `json:"proposed_files,omitempty"`
	DiffSummary      string            `json:"diff_summary,omitempty"`
	EntriesUsed      []string          `json:"entries_used,omitempty"`
	EntriesDiscarded map[string]string `json:"entries_discarded,omitempty"`
}

func (p *Proposal) AllRulesResolved() bool { return true }

func (p *Proposal) ApprovedRulesByLayer() map[Layer][]Rule { return nil }

type ProposalStore struct{}

func NewProposalStore(_ string, _ *log.Logger) *ProposalStore { return &ProposalStore{} }

func (s *ProposalStore) Save(_ *Proposal) error                           { return nil }
func (s *ProposalStore) Get(_, _ string) (*Proposal, error)               { return nil, nil }
func (s *ProposalStore) List(_ string) ([]*Proposal, error)               { return nil, nil }
func (s *ProposalStore) PendingRuleTexts(_ string) []string               { return nil }
func (s *ProposalStore) DismissedRuleTexts(_ string) []string             { return nil }
func (s *ProposalStore) UpdateStatus(_, _ string, _ ProposalStatus) error { return nil }
func (s *ProposalStore) UpdateRule(_, _, _ string, _ RuleUpdate) error    { return nil }

type ExtractionResponse struct {
	Rules            []ExtractedRule `json:"rules"`
	DiscardedEntries []string        `json:"discarded_entries"`
}

type ExtractedRule struct {
	Text           string            `json:"text"`
	Category       string            `json:"category"`
	SuggestedLayer string            `json:"suggested_layer"`
	SourceEntries  []RuleSourceEntry `json:"source_entries"`
}

type InstructionStore struct{}

func NewInstructionStore(_ string) *InstructionStore { return &InstructionStore{} }

func (s *InstructionStore) Read(_ Layer, _ string) (string, error)  { return "", nil }
func (s *InstructionStore) Write(_ Layer, _ string, _ string) error { return nil }
func (s *InstructionStore) Assemble(_ string, _ string) string      { return "" }

type MergeResponse struct {
	MergedContent string
	Summary       string
}

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

func (pm *PendingMerge) IsExpired() bool          { return false }
func (pm *PendingMerge) EffectiveContent() string { return "" }

type PendingMergeStore struct{}

func NewPendingMergeStore(_ string, _ *log.Logger) *PendingMergeStore { return &PendingMergeStore{} }

func (s *PendingMergeStore) Save(_ *PendingMerge) error                        { return nil }
func (s *PendingMergeStore) Get(_ string) (*PendingMerge, error)               { return nil, nil }
func (s *PendingMergeStore) Delete(_ string) error                             { return nil }
func (s *PendingMergeStore) UpdateEditedContent(_ string, _ string) error      { return nil }
func (s *PendingMergeStore) InvalidateIfContainsRule(_ string, _ string) error { return nil }

type Entry struct {
	Timestamp    time.Time `json:"ts"`
	Workspace    string    `json:"ws,omitempty"`
	Session      string    `json:"session,omitempty"`
	Agent        string    `json:"agent,omitempty"`
	Type         string    `json:"type,omitempty"`
	Text         string    `json:"text,omitempty"`
	Tool         string    `json:"tool,omitempty"`
	InputSummary string    `json:"input_summary,omitempty"`
	ErrorSummary string    `json:"error_summary,omitempty"`
	Category     string    `json:"category,omitempty"`
	StateChange  string    `json:"state_change,omitempty"`
	EntryTS      string    `json:"entry_ts,omitempty"`
	ProposalID   string    `json:"proposal_id,omitempty"`
}

func (e Entry) EntryKey() string { return "" }

type EntryFilter func(entries []Entry) []Entry

// --- Package-level functions ---

func SetLogger(_ *log.Logger) {}

func ApplyToLayer(_ *InstructionStore, _ Layer, _, _ string) error { return nil }

func BuildExtractionPrompt(_ []Entry, _ []string, _ []string) string { return "" }

func ParseExtractionResponse(_ string) (*ExtractionResponse, error) {
	return &ExtractionResponse{}, nil
}

func ReadFileFromRepo(_ context.Context, _, _ string) (string, error) { return "", nil }

func BuildMergePrompt(_ string, _ []Rule) string { return "" }

func ParseMergeResponse(_ string) (*MergeResponse, error) { return &MergeResponse{}, nil }

func NormalizeRuleText(text string) string { return text }

func DeduplicateRules(rules []Rule, _ []string) ([]Rule, int) { return rules, 0 }

func FilterRaw() EntryFilter { return nil }

func ParseEntry(_ string) (Entry, error) { return Entry{}, nil }

func ReadEntries(_ string, _ EntryFilter) ([]Entry, error) { return nil, nil }

func MarkEntriesDirect(_ []Entry, _ string, _ string, _ string) error { return nil }

func MarkEntriesByTextFromEntries(_ []Entry, _ string, _ string, _ []string, _ string) error {
	return nil
}

func FilterByParams(_, _, _ string, _ int) EntryFilter { return nil }

func PruneEntries(_ string, _ time.Duration) (pruned int, err error) { return 0, nil }

func ReadEntriesFromEvents(_, _ string, _ EntryFilter) ([]Entry, error) { return nil, nil }

func LoreStateDir(_ string) (string, error) { return "", nil }

func LoreStatePath(_ string) (string, error) { return "", nil }

func IsAvailable() bool { return false }
