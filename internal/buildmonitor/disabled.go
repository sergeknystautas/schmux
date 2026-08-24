//go:build nobuildmonitor

package buildmonitor

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/github"
)

// IsAvailable reports whether the build monitor feature is available.
func IsAvailable() bool { return false }

// Monitor is a no-op stub. It exists so server.go can hold a *Monitor field
// regardless of build tag; under nobuildmonitor every method returns the
// zero value (no status, no URL, not ok).
type Monitor struct{}

// NewMonitor returns a stub Monitor. The arguments are unused.
func NewMonitor(_ func() time.Time, _ string) *Monitor { return &Monitor{} }

// Status returns "", "", false — no CI data when the feature is compiled out.
func (m *Monitor) Status(_ github.RepoInfo, _ string) (string, string, bool) {
	return "", "", false
}

// Test-only accessors, mirroring testhelpers.go, so dashboard-package tests
// that seed a Monitor compile under nobuildmonitor. They are no-ops: there is
// no store to seed.
func (m *Monitor) RecordCommitForTest(_ github.RepoInfo, _, _, _ string, _ bool) {}
func (m *Monitor) SetRepoMetaForTest(_ github.RepoInfo, _ bool, _ string)        {}
func (m *Monitor) SetEnabledForTest(_ bool)                                      {}
