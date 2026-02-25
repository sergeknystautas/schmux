package workspace

import (
	"context"
	"sync"
)

// gitFetchPollRound deduplicates git fetch operations within a single poll sweep.
// The key is the effective fetch target (for worktrees: shared base repo path).
//
// It caches the first result for the duration of the round and allows concurrent
// callers to wait for the in-flight fetch instead of issuing duplicate fetches.
type gitFetchPollRound struct {
	mu      sync.Mutex
	entries map[string]*gitFetchPollRoundEntry
}

type gitFetchPollRoundEntry struct {
	done chan struct{}
	err  error
}

func newGitFetchPollRound() *gitFetchPollRound {
	return &gitFetchPollRound{
		entries: make(map[string]*gitFetchPollRoundEntry),
	}
}

func (r *gitFetchPollRound) Do(ctx context.Context, key string, fn func(context.Context) error) error {
	if r == nil || key == "" {
		return fn(ctx)
	}

	r.mu.Lock()
	if existing, ok := r.entries[key]; ok {
		done := existing.done
		r.mu.Unlock()
		select {
		case <-done:
			return existing.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	entry := &gitFetchPollRoundEntry{done: make(chan struct{})}
	r.entries[key] = entry
	r.mu.Unlock()

	err := fn(ctx)

	r.mu.Lock()
	entry.err = err
	close(entry.done)
	r.mu.Unlock()

	return err
}
