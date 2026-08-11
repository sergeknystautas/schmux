package contracts

// PushCommitsResult is the response body for POST /api/workspaces/{id}/push-commits.
// Reason values (set when Success is false) are defined in internal/workspace/push_commits.go:
// "dirty", "nothing_to_push", "behind", "diverged", "no_remote_default",
// "no_base", "push_rejected", "unsupported".
type PushCommitsResult struct {
	Success         bool     `json:"success"`
	TargetBranch    string   `json:"target_branch"`              // branch name pushed to (without "origin/")
	PerCommit       bool     `json:"per_commit"`                 // echo of the requested mode
	TotalCommits    int      `json:"total_commits"`              // commits that needed pushing when the operation started
	PushesSucceeded int      `json:"pushes_succeeded"`           // pushes that landed (per-commit mode may stop early)
	FailedHash      string   `json:"failed_hash,omitempty"`      // commit whose push was rejected
	Reason          string   `json:"reason,omitempty"`           // machine-readable failure reason
	Message         string   `json:"message,omitempty"`          // human-readable detail (may include git output)
	NeedsConfirm    bool     `json:"needs_confirm,omitempty"`    // branch target diverged; retry with confirm=true to force
	DivergedCommits []string `json:"diverged_commits,omitempty"` // "sha message" lines that a force push would overwrite
}
