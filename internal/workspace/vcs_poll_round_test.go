package workspace

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitFetchPollRoundDo_DedupesSequentialCalls(t *testing.T) {
	t.Parallel()

	round := newGitFetchPollRound()
	var calls atomic.Int32

	fn := func(ctx context.Context) error {
		calls.Add(1)
		return nil
	}

	if err := round.Do(context.Background(), "repo-a", fn); err != nil {
		t.Fatalf("first Do() error: %v", err)
	}
	if err := round.Do(context.Background(), "repo-a", fn); err != nil {
		t.Fatalf("second Do() error: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying call, got %d", got)
	}
}

func TestGitFetchPollRoundDo_PropagatesCachedError(t *testing.T) {
	t.Parallel()

	round := newGitFetchPollRound()
	var calls atomic.Int32
	wantErr := errors.New("fetch failed")

	fn := func(ctx context.Context) error {
		calls.Add(1)
		return wantErr
	}

	if err := round.Do(context.Background(), "repo-a", fn); !errors.Is(err, wantErr) {
		t.Fatalf("first Do() error = %v, want %v", err, wantErr)
	}
	if err := round.Do(context.Background(), "repo-a", fn); !errors.Is(err, wantErr) {
		t.Fatalf("second Do() error = %v, want cached %v", err, wantErr)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying call, got %d", got)
	}
}

func TestGitFetchPollRoundDo_WaitsForInflight(t *testing.T) {
	t.Parallel()

	round := newGitFetchPollRound()
	var calls atomic.Int32

	started := make(chan struct{})
	release := make(chan struct{})

	fn := func(ctx context.Context) error {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = round.Do(context.Background(), "repo-a", fn)
	}()

	<-started

	go func() {
		defer wg.Done()
		errs[1] = round.Do(context.Background(), "repo-a", fn)
	}()

	// Give the waiter a moment to block on the in-flight entry.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying call, got %d", got)
	}
}
