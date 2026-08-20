package contracts

// DivergenceCommit is one commit in a branch-divergence listing.
type DivergenceCommit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"` // ISO 8601 committer date
	Subject   string `json:"subject"`   // first line of the commit message
}

// BranchDivergenceResponse is the response body for
// GET /api/workspaces/{id}/branch-divergence.
// LocalHead + RemoteHead + Branch are the "reviewed tuple" a confirmed
// force push is bound to (see PushToBranch's expected_local/expected_remote).
type BranchDivergenceResponse struct {
	Branch        string             `json:"branch"`
	LocalHead     string             `json:"local_head"`
	RemoteHead    string             `json:"remote_head"` // empty when origin/<branch> does not exist
	LocalCommits  []DivergenceCommit `json:"local_commits"`
	RemoteCommits []DivergenceCommit `json:"remote_commits"`
	LocalTotal    int                `json:"local_total"`
	RemoteTotal   int                `json:"remote_total"`
}
