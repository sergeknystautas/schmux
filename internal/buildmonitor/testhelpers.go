//go:build !nobuildmonitor

package buildmonitor

import (
	"github.com/sergeknystautas/schmux/internal/github"
)

// Test-only accessors. Callers outside the buildmonitor package use these to
// seed state directly when exercising downstream readers. Production code must
// go through CheckPass; these exist so dashboard-package tests can verify
// read paths without spinning up the full GitHub API surface.

// RecordCommitForTest seeds the commit store from outside the package.
func (m *Monitor) RecordCommitForTest(repo github.RepoInfo, branch, sha, status, url string, terminal bool) {
	m.recordCommit(repo, branch, sha, status, url, terminal)
}

// SetRepoMetaForTest seeds per-repo metadata from outside the package.
func (m *Monitor) SetRepoMetaForTest(repo github.RepoInfo, hasWorkflows bool, errMsg string) {
	m.setRepoMeta(repo, hasWorkflows, errMsg)
}

// SetEnabledForTest sets the monitor's enabled flag from outside the package.
func (m *Monitor) SetEnabledForTest(enabled bool) {
	if enabled {
		m.setEnabled()
		return
	}
	m.setDisabled()
}
