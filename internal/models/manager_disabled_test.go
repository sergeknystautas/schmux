//go:build nomodelregistry

package models

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
)

// In nomodelregistry builds RegistryURL is empty. StartBackgroundFetch must
// detect this and return immediately rather than spawning a fetch loop that
// calls http.Get("") and logs "unsupported protocol scheme" on every cycle.
func TestStartBackgroundFetchIsNoopWhenRegistryDisabled(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	logger := log.NewWithOptions(&safeWriter{mu: &mu, buf: &buf}, log.Options{Level: log.DebugLevel})

	mm := New(&config.Config{}, nil, t.TempDir(), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mm.StartBackgroundFetch(ctx)

	// Poll briefly: if the fetch goroutine spawned, it would log the
	// "unsupported protocol scheme" / "failed to fetch registry" error
	// well within this window. Negative assertion — must wait to verify
	// the goroutine did not start.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		output := buf.String()
		mu.Unlock()
		if strings.Contains(output, "failed to fetch registry") ||
			strings.Contains(output, "unsupported protocol scheme") {
			t.Fatalf("disabled build attempted HTTP fetch; log:\n%s", output)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// safeWriter serializes writes to the underlying buffer so the polling
// reader and the (potentially-spawned) fetch goroutine don't race.
type safeWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w *safeWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func TestIsAvailableFalseWhenRegistryDisabled(t *testing.T) {
	if IsAvailable() {
		t.Error("IsAvailable() = true in nomodelregistry build, want false")
	}
	if RegistryURL != "" {
		t.Errorf("RegistryURL = %q, want empty string", RegistryURL)
	}
}
