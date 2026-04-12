package contracts

import "time"

// SpawnEntryType distinguishes what a spawn entry does.
type SpawnEntryType string

const (
	SpawnEntrySkill   SpawnEntryType = "skill"
	SpawnEntryCommand SpawnEntryType = "command"
	SpawnEntryAgent   SpawnEntryType = "agent"
	SpawnEntryShell   SpawnEntryType = "shell"
)

// SpawnEntrySource tracks how the entry was created.
type SpawnEntrySource string

const (
	SpawnSourceBuiltIn SpawnEntrySource = "built-in"
	SpawnSourceEmerged SpawnEntrySource = "emerged"
	SpawnSourceManual  SpawnEntrySource = "manual"
)

// SpawnEntryState is the lifecycle state.
type SpawnEntryState string

const (
	SpawnStateProposed  SpawnEntryState = "proposed"
	SpawnStatePinned    SpawnEntryState = "pinned"
	SpawnStateDismissed SpawnEntryState = "dismissed"
)

// SpawnEntry is one item in the spawn dropdown.
type SpawnEntry struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Type        SpawnEntryType   `json:"type"`
	Source      SpawnEntrySource `json:"source"`
	State       SpawnEntryState  `json:"state"`

	// Type=skill fields
	SkillRef string `json:"skill_ref,omitempty"`

	// Type=command fields
	Command string `json:"command,omitempty"`

	// Type=agent fields
	Prompt string `json:"prompt,omitempty"`
	Target string `json:"target,omitempty"`

	// Lifecycle
	UseCount int        `json:"use_count"`
	LastUsed *time.Time `json:"last_used,omitempty"`

	// Emergence metadata (populated by entries/all endpoint for skill entries)
	Metadata *EmergenceMetadata `json:"metadata,omitempty"`
}

// EmergenceMetadata tracks emergence-internal data for a skill.
type EmergenceMetadata struct {
	SkillName     string    `json:"skill_name"`
	SkillContent  string    `json:"skill_content,omitempty"`
	Confidence    float64   `json:"confidence"`
	EvidenceCount int       `json:"evidence_count"`
	Evidence      []string  `json:"evidence,omitempty"`
	EmergedAt     time.Time `json:"emerged_at"`
	LastCurated   time.Time `json:"last_curated"`
}

// --- API request/response types ---

// SpawnEntriesResponse is the body for GET /api/emergence/{repo}/entries.
type SpawnEntriesResponse struct {
	Entries []SpawnEntry `json:"entries"`
}

// CreateSpawnEntryRequest is the body for POST /api/emergence/{repo}/entries.
type CreateSpawnEntryRequest struct {
	Name    string         `json:"name"`
	Type    SpawnEntryType `json:"type"`
	Command string         `json:"command,omitempty"`
	Prompt  string         `json:"prompt,omitempty"`
	Target  string         `json:"target,omitempty"`
}

// UpdateSpawnEntryRequest is the body for PUT /api/emergence/{repo}/entries/{id}.
type UpdateSpawnEntryRequest struct {
	Name    *string `json:"name,omitempty"`
	Command *string `json:"command,omitempty"`
	Prompt  *string `json:"prompt,omitempty"`
	Target  *string `json:"target,omitempty"`
}

// PromptHistoryEntry is a single entry in the prompt history response.
type PromptHistoryEntry struct {
	Text     string    `json:"text"`
	LastSeen time.Time `json:"last_seen"`
	Count    int       `json:"count"`
}

// PromptHistoryResponse is the body for GET /api/emergence/{repo}/prompt-history.
type PromptHistoryResponse struct {
	Prompts []PromptHistoryEntry `json:"prompts"`
}
