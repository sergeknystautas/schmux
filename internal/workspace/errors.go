package workspace

import "errors"

var (
	ErrWorkspaceLocked = errors.New("workspace is locked")
	ErrNotFound        = errors.New("workspace: not found")
	ErrInvalidCommit   = errors.New("workspace: invalid commit hash")
	ErrCommitNotFound  = errors.New("workspace: commit not found")

	// ErrMalformedHash is returned by PushCommits when the hash is not a full hex sha.
	ErrMalformedHash = errors.New("hash must be a full commit sha")
	// ErrStaleHash is returned by PushCommits when the hash is unknown, not a
	// commit, or not an ancestor of HEAD (the UI's graph is stale).
	ErrStaleHash = errors.New("commit graph has changed, please refresh and try again")
	// ErrInvalidPushTarget is returned by PushCommits for a target other than "default" or "branch".
	ErrInvalidPushTarget = errors.New(`target must be "default" or "branch"`)

	// ErrNotConnectEligible is returned by RunGitHubConnect for a workspace that
	// is already fully connected (neither state nor config references a local: repo).
	ErrNotConnectEligible = errors.New("workspace is not eligible for github connect")
	// ErrConnectMissingTarget is returned when repo creation or origin setup is
	// needed but the request has no owner/name.
	ErrConnectMissingTarget = errors.New("owner and repo name are required to create the repository")
)
