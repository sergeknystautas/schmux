package contracts

// GitHubStatus represents the gh CLI authentication state.
type GitHubStatus struct {
	Available bool   `json:"available"`
	Username  string `json:"username,omitempty"`
}
