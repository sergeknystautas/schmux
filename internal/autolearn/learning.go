//go:build !noautolearn

package autolearn

import "time"

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

type SourceRef struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	InputSummary string `json:"input_summary,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
	Tool         string `json:"tool,omitempty"`
}

type BatchStatus string

const (
	BatchPending   BatchStatus = "pending"
	BatchMerging   BatchStatus = "merging"
	BatchApplied   BatchStatus = "applied"
	BatchDismissed BatchStatus = "dismissed"
)

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

func (b *Batch) AllResolved() bool {
	for _, l := range b.Learnings {
		if l.Status == StatusPending {
			return false
		}
	}
	return true
}

type LearningUpdate struct {
	Status      *LearningStatus `json:"status,omitempty"`
	Title       *string         `json:"title,omitempty"`
	Description *string         `json:"description,omitempty"`
	ChosenLayer *Layer          `json:"chosen_layer,omitempty"`
	Rule        *RuleDetails    `json:"rule,omitempty"`
	Skill       *SkillDetails   `json:"skill,omitempty"`
}

type PendingMergeStatus string

const (
	PendingMergeStatusMerging PendingMergeStatus = "merging"
	PendingMergeStatusReady   PendingMergeStatus = "ready"
	PendingMergeStatusError   PendingMergeStatus = "error"
)

func IsAvailable() bool { return true }
