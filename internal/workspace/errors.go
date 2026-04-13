package workspace

import "errors"

var (
	ErrWorkspaceLocked = errors.New("workspace is locked")
	ErrNotFound        = errors.New("workspace: not found")
	ErrInvalidCommit   = errors.New("workspace: invalid commit hash")
	ErrCommitNotFound  = errors.New("workspace: commit not found")
)
