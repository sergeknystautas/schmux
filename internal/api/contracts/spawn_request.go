package contracts

// SpawnRequest is the request body for POST /api/sessions/spawn.
type SpawnRequest struct {
	Repo             string         `json:"repo"`
	Branch           string         `json:"branch"`
	Prompt           string         `json:"prompt"`
	Nickname         string         `json:"nickname,omitempty"`     // optional human-friendly name for sessions
	Targets          map[string]int `json:"targets"`                // target name -> quantity
	WorkspaceID      string         `json:"workspace_id,omitempty"` // optional: spawn into specific workspace
	Command          string         `json:"command,omitempty"`      // shell command to run directly (alternative to targets)
	QuickLaunchName  string         `json:"quick_launch_name,omitempty"`
	ActionID         string         `json:"action_id,omitempty"`         // action registry ID for usage tracking
	Resume           bool           `json:"resume,omitempty"`            // resume mode: use agent's resume command
	RemoteProfileID  string         `json:"remote_profile_id,omitempty"` // optional: spawn on remote host
	RemoteFlavor     string         `json:"remote_flavor,omitempty"`     // optional: flavor within remote profile
	RemoteHostID     string         `json:"remote_host_id,omitempty"`    // optional: spawn on specific existing remote host
	NewBranch        string         `json:"new_branch,omitempty"`        // create new workspace with this branch from source workspace
	PersonaID        string         `json:"persona_id,omitempty"`        // optional: behavioral persona for the agent
	StyleID          string         `json:"style_id,omitempty"`          // optional: communication style override ("none" to suppress global default)
	ImageAttachments []string       `json:"image_attachments,omitempty"` // base64-encoded PNGs, max 5
	IntentShared     bool           `json:"intent_shared,omitempty"`     // optional: share workspace intent with team via repofeed
}
