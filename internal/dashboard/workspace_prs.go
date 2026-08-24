//go:build !nogithub

package dashboard

import (
	"context"
	"sync"

	"github.com/sergeknystautas/schmux/internal/github"
)

// PRRef is an open pull request reference surfaced alongside CI status.
type PRRef struct {
	Number int
	URL    string
}

// prWorkspace is the subset of a workspace needed for PR lookup.
type prWorkspace struct {
	ID  string
	URL string // base repo URL
	// Branch is the workspace's branch; the PR query is "owner:branch".
	Branch string
	// ForkRemoteURL is set when the workspace's remote is a fork; the PR
	// query then targets the fork owner, not the base repo owner.
	ForkRemoteURL string
}

// PRTracker caches open-PR lookups per workspace. PRs appear without new
// commits, so this is dashboard-owned and refreshed every check pass. It is
// independent of buildmonitor — CI status lives in the monitor's commit
// store, PR visibility lives here.
type PRTracker struct {
	mu      sync.Mutex
	entries map[string]PRRef
}

func NewPRTracker() *PRTracker {
	return &PRTracker{entries: map[string]PRRef{}}
}

// Get returns the cached PR for a workspace, if any.
func (t *PRTracker) Get(workspaceID string) (PRRef, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ref, ok := t.entries[workspaceID]
	return ref, ok
}

// Refresh fetches open PRs for the given workspaces of one base repo. Returns
// true when the tracked set changed (so the caller can broadcast).
func (t *PRTracker) Refresh(ctx context.Context, token string, base github.RepoInfo, workspaces []prWorkspace) bool {
	changed := false
	for _, w := range workspaces {
		owner := base.Owner
		if w.ForkRemoteURL != "" {
			if parsed, err := github.ParseRepoURL(w.ForkRemoteURL); err == nil {
				owner = parsed.Owner
			}
		}
		pr, err := github.FetchOpenPRForBranch(ctx, token, base, owner+":"+w.Branch)
		if err != nil {
			// Errors keep the previous PR (graceful: stale > blanked).
			continue
		}
		t.mu.Lock()
		if pr == nil {
			if _, had := t.entries[w.ID]; had {
				delete(t.entries, w.ID)
				changed = true
			}
		} else {
			ref := PRRef{Number: pr.Number, URL: pr.HTMLURL}
			if prev, had := t.entries[w.ID]; !had || prev != ref {
				t.entries[w.ID] = ref
				changed = true
			}
		}
		t.mu.Unlock()
	}
	return changed
}

// DropExcept removes PR entries for workspaces not in live. Returns whether
// anything was dropped.
func (t *PRTracker) DropExcept(live map[string]bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	removed := false
	for id := range t.entries {
		if !live[id] {
			delete(t.entries, id)
			removed = true
		}
	}
	return removed
}

// Clear removes all entries; returns whether anything was removed. Used when
// the build monitor is disabled so PR badges vanish alongside chips.
func (t *PRTracker) Clear() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	removed := len(t.entries) > 0
	t.entries = map[string]PRRef{}
	return removed
}
