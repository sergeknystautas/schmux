//go:build noautolearn

package autolearn

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
)

// --- String types + constants ---

type LearningKind string

const (
	KindRule  LearningKind = "rule"
	KindSkill LearningKind = "skill"
)

type LearningStatus string

const (
	StatusPending   LearningStatus = "pending"
	StatusApproved  LearningStatus = "approved"
	StatusDismissed LearningStatus = "dismissed"
)

type Layer string

const (
	LayerRepoPublic       Layer = "repo_public"
	LayerRepoPrivate      Layer = "repo_private"
	LayerCrossRepoPrivate Layer = "cross_repo_private"
)

type BatchStatus string

const (
	BatchPending   BatchStatus = "pending"
	BatchMerging   BatchStatus = "merging"
	BatchApplied   BatchStatus = "applied"
	BatchDismissed BatchStatus = "dismissed"
)

type PendingMergeStatus string

const (
	PendingMergeStatusMerging PendingMergeStatus = "merging"
	PendingMergeStatusReady   PendingMergeStatus = "ready"
	PendingMergeStatusError   PendingMergeStatus = "error"
)

// --- Structs ---

type SourceRef struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	InputSummary string `json:"input_summary,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
	Tool         string `json:"tool,omitempty"`
}

type RuleDetails struct {
	MergedAt *time.Time `json:"merged_at,omitempty"`
}

type SkillDetails struct {
	Triggers        []string `json:"triggers,omitempty"`
	Procedure       string   `json:"procedure,omitempty"`
	QualityCriteria string   `json:"quality_criteria,omitempty"`
	Confidence      float64  `json:"confidence,omitempty"`
	SkillContent    string   `json:"skill_content,omitempty"`
	IsUpdate        bool     `json:"is_update,omitempty"`
	Changes         string   `json:"changes,omitempty"`
}

type Learning struct {
	ID          string         `json:"id"`
	Kind        LearningKind   `json:"kind"`
	Status      LearningStatus `json:"status"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Sources     []SourceRef    `json:"sources,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`

	SuggestedLayer Layer  `json:"suggested_layer"`
	ChosenLayer    *Layer `json:"chosen_layer,omitempty"`

	Rule  *RuleDetails  `json:"rule,omitempty"`
	Skill *SkillDetails `json:"skill,omitempty"`
}

func (l *Learning) EffectiveLayer() Layer {
	if l.ChosenLayer != nil {
		return *l.ChosenLayer
	}
	return l.SuggestedLayer
}

type LearningUpdate struct {
	Status      *LearningStatus `json:"status,omitempty"`
	Title       *string         `json:"title,omitempty"`
	Description *string         `json:"description,omitempty"`
	ChosenLayer *Layer          `json:"chosen_layer,omitempty"`
	Rule        *RuleDetails    `json:"rule,omitempty"`
	Skill       *SkillDetails   `json:"skill,omitempty"`
}

type Batch struct {
	ID        string      `json:"id"`
	Repo      string      `json:"repo"`
	CreatedAt time.Time   `json:"created_at"`
	Status    BatchStatus `json:"status"`
	Learnings []Learning  `json:"learnings"`
	Discarded []string    `json:"discarded,omitempty"`

	SourceCount      int               `json:"source_count,omitempty"`
	EntriesUsed      []string          `json:"entries_used,omitempty"`
	EntriesDiscarded map[string]string `json:"entries_discarded,omitempty"`
}

func (b *Batch) AllResolved() bool { return true }

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

type IntentSignal struct {
	Text      string    `json:"text"`
	Timestamp time.Time `json:"ts"`
	Target    string    `json:"target,omitempty"`
	Persona   string    `json:"persona,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	Session   string    `json:"session,omitempty"`
	Count     int       `json:"count"`
}

type FrictionCuratorResponse struct {
	Learnings        []Learning `json:"learnings"`
	DiscardedEntries []string   `json:"discarded_entries"`
}

type IntentCuratorResponse struct {
	NewLearnings     []Learning        `json:"new_learnings"`
	UpdatedLearnings []Learning        `json:"updated_learnings"`
	DiscardedSignals map[string]string `json:"discarded_signals"`
}

type MergeResponse struct {
	MergedContent string
	Summary       string
}

type PendingMerge struct {
	Repo           string             `json:"repo"`
	Status         PendingMergeStatus `json:"status"`
	BaseSHA        string             `json:"base_sha"`
	LearningIDs    []string           `json:"learning_ids"`
	BatchIDs       []string           `json:"batch_ids"`
	MergedContent  string             `json:"merged_content"`
	CurrentContent string             `json:"current_content"`
	Summary        string             `json:"summary"`
	EditedContent  *string            `json:"edited_content,omitempty"`
	SkillFiles     map[string]string  `json:"skill_files,omitempty"`
	Error          string             `json:"error,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
}

func (pm *PendingMerge) IsExpired() bool          { return false }
func (pm *PendingMerge) EffectiveContent() string { return "" }

// --- Store types ---

type BatchStore struct{}

func NewBatchStore(_ string, _ *log.Logger) *BatchStore { return &BatchStore{} }

func (s *BatchStore) Save(_ *Batch) error                                   { return nil }
func (s *BatchStore) Get(_, _ string) (*Batch, error)                       { return nil, nil }
func (s *BatchStore) List(_ string) ([]*Batch, error)                       { return nil, nil }
func (s *BatchStore) UpdateStatus(_, _ string, _ BatchStatus) error         { return nil }
func (s *BatchStore) UpdateLearning(_, _, _ string, _ LearningUpdate) error { return nil }
func (s *BatchStore) PendingLearningTitles(_ string) []string               { return nil }
func (s *BatchStore) DismissedLearningTitles(_ string) []string             { return nil }

type InstructionStore struct{}

func NewInstructionStore(_ string) *InstructionStore { return &InstructionStore{} }

func (s *InstructionStore) Read(_ Layer, _ string) (string, error) { return "", nil }
func (s *InstructionStore) Assemble(_ string, _ string) string     { return "" }

type PendingMergeStore struct{}

func NewPendingMergeStore(_ string, _ *log.Logger) *PendingMergeStore { return &PendingMergeStore{} }

func (s *PendingMergeStore) Save(_ *PendingMerge) error                            { return nil }
func (s *PendingMergeStore) Get(_ string) (*PendingMerge, error)                   { return nil, nil }
func (s *PendingMergeStore) Delete(_ string) error                                 { return nil }
func (s *PendingMergeStore) UpdateEditedContent(_ string, _ string) error          { return nil }
func (s *PendingMergeStore) InvalidateIfContainsLearning(_ string, _ string) error { return nil }

// --- Package-level functions ---

func IsAvailable() bool { return false }

func SetLogger(_ *log.Logger) {}

func FilterLearnings(_ []*Batch, _ *LearningKind, _ *LearningStatus, _ *Layer) []Learning {
	return nil
}

func FilterRaw() EntryFilter { return nil }

func ParseEntry(_ string) (Entry, error) { return Entry{}, nil }

func ReadEntries(_ string, _ EntryFilter) ([]Entry, error) { return nil, nil }

func ReadEntriesFromEvents(_, _ string, _ EntryFilter) ([]Entry, error) { return nil, nil }

func FilterByParams(_, _, _ string, _ int) EntryFilter { return nil }

func MarkEntriesDirect(_ []Entry, _ string, _ string, _ string) error { return nil }

func MarkEntriesByTextFromEntries(_ []Entry, _ string, _ string, _ []string, _ string) error {
	return nil
}

func StatePath(_ string) (string, error) { return "", nil }

func StateDir(_ string) (string, error) { return "", nil }

func CollectIntentSignals(_ []string) ([]IntentSignal, error) { return nil, nil }

func BuildFrictionPrompt(_ []Entry, _ []string, _ []string) string { return "" }

func ParseFrictionResponse(_ string) (*FrictionCuratorResponse, error) {
	return &FrictionCuratorResponse{}, nil
}

func ReadFileFromRepo(_ context.Context, _, _ string) (string, error) { return "", nil }

func BuildMergePrompt(_ string, _ []Learning) string { return "" }

func ParseMergeResponse(_ string) (*MergeResponse, error) { return &MergeResponse{}, nil }

func NormalizeLearningTitle(text string) string { return text }

func DeduplicateLearnings(learnings []Learning, _ []string) ([]Learning, int) {
	return learnings, 0
}
