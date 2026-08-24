//go:build nogithub

package dashboard

import (
	"context"

	"github.com/sergeknystautas/schmux/internal/github"
)

// PRRef mirrors workspace_prs.go's PRRef so server.go and SessionHandlers
// compile under the nogithub build tag.
type PRRef struct {
	Number int
	URL    string
}

// prWorkspace mirrors workspace_prs.go's prWorkspace.
type prWorkspace struct {
	ID            string
	URL           string
	Branch        string
	ForkRemoteURL string
}

// PRTracker is a no-op stub under nogithub: the github package has nothing
// to query, so Refresh never populates anything. entries is kept (unused by
// any real logic) so white-box tests that seed it directly still compile.
type PRTracker struct {
	entries map[string]PRRef
}

func NewPRTracker() *PRTracker { return &PRTracker{entries: map[string]PRRef{}} }

func (t *PRTracker) Get(id string) (PRRef, bool) {
	ref, ok := t.entries[id]
	return ref, ok
}

func (t *PRTracker) Refresh(context.Context, string, github.RepoInfo, []prWorkspace) bool {
	return false
}

func (t *PRTracker) DropExcept(map[string]bool) bool { return false }

func (t *PRTracker) Clear() bool { return false }
