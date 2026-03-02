package contracts

import "time"

// ActionType describes what kind of action this is.
type ActionType string

const (
	ActionTypeAgent   ActionType = "agent"
	ActionTypeCommand ActionType = "command"
	ActionTypeShell   ActionType = "shell"
)

// ActionSource describes how the action was created.
type ActionSource string

const (
	ActionSourceEmerged  ActionSource = "emerged"
	ActionSourceManual   ActionSource = "manual"
	ActionSourceMigrated ActionSource = "migrated"
)

// ActionState describes the lifecycle state of the action.
type ActionState string

const (
	ActionStateProposed  ActionState = "proposed"
	ActionStatePinned    ActionState = "pinned"
	ActionStateDismissed ActionState = "dismissed"
)

// ActionParameter defines a named parameter in an action template.
type ActionParameter struct {
	Name    string `json:"name"`
	Default string `json:"default,omitempty"`
}

// LearnedDefault is a parameter value learned from usage patterns.
type LearnedDefault struct {
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// Action represents a reusable action in the registry.
type Action struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Type  ActionType `json:"type"`
	Scope string     `json:"scope"`

	// Agent type fields.
	Template   string            `json:"template,omitempty"`
	Parameters []ActionParameter `json:"parameters,omitempty"`
	Target     string            `json:"target,omitempty"`
	Persona    string            `json:"persona,omitempty"`

	// Command type fields.
	Command string `json:"command,omitempty"`

	// Learned defaults (emerged actions).
	LearnedTarget  *LearnedDefault `json:"learned_target,omitempty"`
	LearnedPersona *LearnedDefault `json:"learned_persona,omitempty"`

	// Lifecycle.
	Source        ActionSource `json:"source"`
	Confidence    float64      `json:"confidence"`
	EvidenceCount int          `json:"evidence_count,omitempty"`
	State         ActionState  `json:"state"`
	UseCount      int          `json:"use_count,omitempty"`
	EditCount     int          `json:"edit_count,omitempty"`

	// Timestamps.
	FirstSeen  time.Time  `json:"first_seen"`
	LastUsed   *time.Time `json:"last_used,omitempty"`
	ProposedAt *time.Time `json:"proposed_at,omitempty"`
	PinnedAt   *time.Time `json:"pinned_at,omitempty"`
}

// ActionRegistryResponse is the body for GET /api/actions/{repo}.
type ActionRegistryResponse struct {
	Actions []Action `json:"actions"`
}

// CreateActionRequest is the body for POST /api/actions/{repo}.
type CreateActionRequest struct {
	Name       string            `json:"name"`
	Type       ActionType        `json:"type"`
	Template   string            `json:"template,omitempty"`
	Parameters []ActionParameter `json:"parameters,omitempty"`
	Target     string            `json:"target,omitempty"`
	Persona    string            `json:"persona,omitempty"`
	Command    string            `json:"command,omitempty"`
}

// UpdateActionRequest is the body for PUT /api/actions/{repo}/{id}.
type UpdateActionRequest struct {
	Name       *string            `json:"name,omitempty"`
	Template   *string            `json:"template,omitempty"`
	Parameters *[]ActionParameter `json:"parameters,omitempty"`
	Target     *string            `json:"target,omitempty"`
	Persona    *string            `json:"persona,omitempty"`
	Command    *string            `json:"command,omitempty"`
}

// PromptHistoryEntry is a single entry in the prompt history response.
type PromptHistoryEntry struct {
	Text     string    `json:"text"`
	LastSeen time.Time `json:"last_seen"`
	Count    int       `json:"count"`
}

// PromptHistoryResponse is the body for GET /api/actions/{repo}/prompt-history.
type PromptHistoryResponse struct {
	Prompts []PromptHistoryEntry `json:"prompts"`
}
