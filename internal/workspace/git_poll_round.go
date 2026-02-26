package workspace

import (
	"context"
	"sync"
)

// pollRound bundles per-sweep caches used during a single polling cycle.
// Both caches are safe for concurrent use from parallel workspace goroutines.
type pollRound struct {
	fetch    *gitFetchPollRound
	worktree *worktreeListCache
}

func newPollRound() *pollRound {
	return &pollRound{
		fetch:    newGitFetchPollRound(),
		worktree: newWorktreeListCache(),
	}
}

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

// worktreeListCache caches `git worktree list --porcelain` output per bare repo path
// within a single poll sweep. Multiple workspaces sharing the same bare clone
// reuse the cached result instead of running the command repeatedly.
type worktreeListCache struct {
	mu      sync.Mutex
	entries map[string]*worktreeListEntry
}

type worktreeListEntry struct {
	done   chan struct{}
	output []byte
	err    error
}

func newWorktreeListCache() *worktreeListCache {
	return &worktreeListCache{
		entries: make(map[string]*worktreeListEntry),
	}
}

// Get returns the cached worktree list output for the given key, or runs fn to
// populate the cache. Concurrent callers for the same key wait for the first
// caller's result (same pattern as gitFetchPollRound).
func (c *worktreeListCache) Get(ctx context.Context, key string, fn func() ([]byte, error)) ([]byte, error) {
	if c == nil || key == "" {
		return fn()
	}

	c.mu.Lock()
	if existing, ok := c.entries[key]; ok {
		done := existing.done
		c.mu.Unlock()
		select {
		case <-done:
			return existing.output, existing.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	entry := &worktreeListEntry{done: make(chan struct{})}
	c.entries[key] = entry
	c.mu.Unlock()

	output, err := fn()

	c.mu.Lock()
	entry.output = output
	entry.err = err
	close(entry.done)
	c.mu.Unlock()

	return output, err
}
