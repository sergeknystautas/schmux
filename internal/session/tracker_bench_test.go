//go:build bench

package session

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/benchutil"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

// benchSetup creates a tmux session running `cat` (pure echo), a
// SessionTracker wired to it, and an attached output channel. The
// returned cleanup function kills the tmux session and stops the tracker.
func benchSetup(tb testing.TB) (tracker *SessionTracker, outputCh chan []byte, tmuxName string, cleanup func()) {
	tb.Helper()

	tmuxName = fmt.Sprintf("bench-%d", time.Now().UnixNano())
	ctx := context.Background()

	if err := tmux.CreateSession(ctx, tmuxName, "/tmp", "cat"); err != nil {
		tb.Fatalf("failed to create tmux session: %v", err)
	}

	st := state.New("")
	tracker = NewSessionTracker("bench-session", tmuxName, st, "", nil, nil)
	tracker.Start()
	outputCh = tracker.AttachWebSocket()

	// Wait for tracker to attach to the PTY.
	deadline := time.Now().Add(5 * time.Second)
	for !tracker.IsAttached() {
		if time.Now().After(deadline) {
			tmux.KillSession(ctx, tmuxName)
			tracker.Stop()
			tb.Fatal("tracker did not attach within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Drain bootstrap noise (tmux attach produces some initial output).
	drainTimeout := time.After(200 * time.Millisecond)
drain:
	for {
		select {
		case <-outputCh:
		case <-drainTimeout:
			break drain
		}
	}

	cleanup = func() {
		tracker.Stop()
		_ = tmux.KillSession(ctx, tmuxName)
	}
	return
}

// benchSetupStressed is like benchSetup but runs a background output flood
// in the same tmux session to create realistic PTY read-loop contention.
func benchSetupStressed(tb testing.TB) (tracker *SessionTracker, outputCh chan []byte, tmuxName string, cleanup func()) {
	tb.Helper()

	tmuxName = fmt.Sprintf("bench-%d", time.Now().UnixNano())
	ctx := context.Background()

	cmd := `sh -c 'while true; do seq 1 50; sleep 0.05; done & exec cat'`
	if err := tmux.CreateSession(ctx, tmuxName, "/tmp", cmd); err != nil {
		tb.Fatalf("failed to create stressed tmux session: %v", err)
	}

	st := state.New("")
	tracker = NewSessionTracker("bench-session-stressed", tmuxName, st, "", nil, nil)
	tracker.Start()
	outputCh = tracker.AttachWebSocket()

	deadline := time.Now().Add(5 * time.Second)
	for !tracker.IsAttached() {
		if time.Now().After(deadline) {
			tmux.KillSession(ctx, tmuxName)
			tracker.Stop()
			tb.Fatal("stressed tracker did not attach within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Drain initial output (more here because of the flood).
	drainTimeout := time.After(500 * time.Millisecond)
drain:
	for {
		select {
		case <-outputCh:
		case <-drainTimeout:
			break drain
		}
	}

	cleanup = func() {
		tracker.Stop()
		_ = tmux.KillSession(ctx, tmuxName)
	}
	return
}

// BenchmarkSendInputEcho is a standard Go benchmark: send one char, wait for
// echo. Compatible with `go test -bench` for quick A/B comparisons.
func BenchmarkSendInputEcho(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	tracker, outputCh, _, cleanup := benchSetup(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := tracker.SendInput("x"); err != nil {
			b.Fatalf("SendInput failed: %v", err)
		}
		select {
		case <-outputCh:
		case <-time.After(5 * time.Second):
			b.Fatal("timeout waiting for echo")
		}
	}
}

// TestLatencyPercentiles measures idle typing latency with percentile reporting.
func TestLatencyPercentiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	tracker, outputCh, _, cleanup := benchSetup(t)
	defer cleanup()

	const warmup = 10
	const measured = 1000

	// Warm-up keystrokes (discarded).
	for i := 0; i < warmup; i++ {
		if err := tracker.SendInput("w"); err != nil {
			t.Fatalf("warmup SendInput failed: %v", err)
		}
		select {
		case <-outputCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout during warmup")
		}
	}

	// Measured keystrokes.
	var gcBefore runtime.MemStats
	runtime.ReadMemStats(&gcBefore)

	durations := make([]time.Duration, 0, measured)
	for i := 0; i < measured; i++ {
		start := time.Now()
		if err := tracker.SendInput("x"); err != nil {
			t.Fatalf("SendInput failed at iteration %d: %v", i, err)
		}
		select {
		case <-outputCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for echo at iteration %d", i)
		}
		durations = append(durations, time.Since(start))
		time.Sleep(1 * time.Millisecond)
	}

	var gcAfter runtime.MemStats
	runtime.ReadMemStats(&gcAfter)

	result := benchutil.ComputeBenchResult("SendInputEcho", "idle", durations, &gcBefore, &gcAfter)
	benchutil.ReportJSON(t, result)
}

// TestLatencyPercentilesStressed measures typing latency under output flood.
func TestLatencyPercentilesStressed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	tracker, outputCh, _, cleanup := benchSetupStressed(t)
	defer cleanup()

	// Under flood, the tracker's clientCh (buffer 64) can overflow and drop
	// chunks. Instead of trying to detect a specific marker through the
	// lossy channel, we measure how long it takes for *any* output to appear
	// after SendInput. This captures the real contention: the PTY read loop
	// must interleave our echo with flood data. We drain stale chunks before
	// each send to ensure we're timing fresh output.

	const warmup = 10
	const measured = 1000

	// Warm-up.
	for i := 0; i < warmup; i++ {
		if err := tracker.SendInput("x"); err != nil {
			t.Fatalf("warmup SendInput failed: %v", err)
		}
		select {
		case <-outputCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout during warmup")
		}
	}

	var gcBefore runtime.MemStats
	runtime.ReadMemStats(&gcBefore)

	durations := make([]time.Duration, 0, measured)
	for i := 0; i < measured; i++ {
		// Drain any queued flood output so the next receive is fresh.
	drain:
		for {
			select {
			case <-outputCh:
			default:
				break drain
			}
		}

		start := time.Now()
		if err := tracker.SendInput("x"); err != nil {
			t.Fatalf("SendInput failed at iteration %d: %v", i, err)
		}
		select {
		case <-outputCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for output at iteration %d", i)
		}
		durations = append(durations, time.Since(start))
		time.Sleep(1 * time.Millisecond)
	}

	var gcAfter runtime.MemStats
	runtime.ReadMemStats(&gcAfter)

	result := benchutil.ComputeBenchResult("SendInputEcho", "stressed", durations, &gcBefore, &gcAfter)
	benchutil.ReportJSON(t, result)
}
