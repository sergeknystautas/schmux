package contracts

import "time"

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	State        string    `json:"state"`
	RepoName     string    `json:"repo_name"`
	RepoURL      string    `json:"repo_url"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	Author       string    `json:"author"`
	CreatedAt    time.Time `json:"created_at"`
	HTMLURL      string    `json:"html_url"`
	ForkOwner    string    `json:"fork_owner,omitempty"`
	IsFork       bool      `json:"is_fork"`
}

// PRsResponse is the response for GET /api/prs.
type PRsResponse struct {
	PullRequests  []PullRequest `json:"prs"`
	LastFetchedAt *time.Time    `json:"last_fetched_at"`
	Error         string        `json:"error,omitempty"`
}

// PRRefreshResponse is the response for POST /api/prs/refresh.
type PRRefreshResponse struct {
	PullRequests  []PullRequest `json:"prs"`
	FetchedCount  int           `json:"fetched_count"`
	Error         string        `json:"error,omitempty"`
	RetryAfterSec *int          `json:"retry_after_sec"`
}

// PRCheckoutRequest is the request for POST /api/prs/checkout.
type PRCheckoutRequest struct {
	RepoURL  string `json:"repo_url"`
	PRNumber int    `json:"pr_number"`
}

// PRCheckoutResponse is the response for POST /api/prs/checkout.
type PRCheckoutResponse struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}
