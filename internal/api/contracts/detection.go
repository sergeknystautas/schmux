package contracts

// DetectionAgent represents a detected AI agent.
type DetectionAgent struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Source  string `json:"source"`
}

// DetectionVCS represents a detected version control system.
type DetectionVCS struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DetectionTmux represents tmux availability.
type DetectionTmux struct {
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
}

// DetectionSummaryResponse is the response for GET /api/detection-summary.
type DetectionSummaryResponse struct {
	Status string           `json:"status"` // "pending" or "ready"
	Agents []DetectionAgent `json:"agents"`
	VCS    []DetectionVCS   `json:"vcs"`
	Tmux   DetectionTmux    `json:"tmux"`
}
