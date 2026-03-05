//go:build bench

package dashboard_test

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/benchutil"
)

// wsBenchSetup connects to a running schmux daemon via WebSocket, matching
// the same path the browser takes. It returns the WebSocket connection and
// a cleanup function.
//
// Environment variables:
//   - BENCH_DAEMON_URL: daemon base URL (default http://localhost:7337)
//   - BENCH_SESSION_ID: target session ID (required — must be a dedicated
//     benchmark session, not a user's active session)
func wsBenchSetup(tb testing.TB) (conn *websocket.Conn, cleanup func()) {
	tb.Helper()

	baseURL := os.Getenv("BENCH_DAEMON_URL")
	if baseURL == "" {
		baseURL = "http://localhost:7337"
	}

	sessionID := os.Getenv("BENCH_SESSION_ID")
	if sessionID == "" {
		tb.Skip("BENCH_SESSION_ID not set — refusing to auto-discover sessions to avoid hijacking active user sessions. Use ./test.sh --bench which spawns dedicated benchmark sessions.")
	}

	// Convert http(s) URL to ws(s) URL.
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = fmt.Sprintf("%s/ws/terminal/%s", wsURL, url.PathEscape(sessionID))

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	c, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		tb.Fatalf("failed to dial WebSocket at %s: %v", wsURL, err)
	}

	// Read the bootstrap binary message and discard it.
	msgType, _, err := c.ReadMessage()
	if err != nil {
		c.Close()
		tb.Fatalf("error reading bootstrap message: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		c.Close()
		tb.Fatalf("expected binary bootstrap message, got type %d", msgType)
	}

	cleanup = func() {
		c.Close()
	}
	return c, cleanup
}

// sendInputAndWaitForEcho sends a keystroke and waits for a binary frame
// whose data payload contains the sent character. This skips background
// output frames (e.g., flood from `seq 1 50`) and measures the actual
// echo round-trip through tmux.
//
// Binary frames have an 8-byte big-endian uint64 sequence header followed
// by terminal data. For a `cat` session, the echo of key 'Q' will appear
// in the payload bytes. Content matching serves double duty: it skips both
// flood frames AND any stale frames buffered from previous iterations,
// without needing a separate drain phase (which is impossible with gorilla
// WebSocket — it permanently caches read errors from deadline timeouts).
func sendInputAndWaitForEcho(tb testing.TB, conn *websocket.Conn, key byte) time.Duration {
	tb.Helper()

	start := time.Now()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{key}); err != nil {
		tb.Fatalf("failed to send input: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	skipped := 0
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			tb.Fatalf("timeout waiting for echo of %q after skipping %d frames: %v",
				string(key), skipped, err)
		}
		if msgType != websocket.BinaryMessage {
			continue // skip text messages (stats, sync, etc.)
		}
		// Binary frame: 8-byte seq header + terminal data.
		if len(data) > 8 {
			payload := data[8:]
			if bytes.Contains(payload, []byte{key}) {
				conn.SetReadDeadline(time.Time{})
				return time.Since(start)
			}
		}
		skipped++
	}
}

// TestWSLatencyPercentiles measures WebSocket round-trip typing latency
// against an idle session running cat.
func TestWSLatencyPercentiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	conn, cleanup := wsBenchSetup(t)
	defer cleanup()

	const warmup = 10
	const measured = 1000

	// Warm-up keystrokes (discarded).
	for i := 0; i < warmup; i++ {
		sendInputAndWaitForEcho(t, conn, 'w')
	}

	// Measured keystrokes. Use 'Q' — not present in `seq 1 50` output
	// (which only produces '0'-'9' and '\n'), so content matching is
	// unambiguous even under stress.
	var gcBefore runtime.MemStats
	runtime.ReadMemStats(&gcBefore)

	durations := make([]time.Duration, 0, measured)
	for i := 0; i < measured; i++ {
		d := sendInputAndWaitForEcho(t, conn, 'Q')
		durations = append(durations, d)
		time.Sleep(1 * time.Millisecond)
	}

	var gcAfter runtime.MemStats
	runtime.ReadMemStats(&gcAfter)

	result := benchutil.ComputeBenchResult("WSEcho", "idle", durations, &gcBefore, &gcAfter)
	benchutil.ReportJSON(t, result)
}

// TestWSLatencyPercentilesStressed measures WebSocket round-trip latency
// against a session with background output flood.
func TestWSLatencyPercentilesStressed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	sessionID := os.Getenv("BENCH_SESSION_ID_STRESSED")
	if sessionID == "" {
		t.Skip("BENCH_SESSION_ID_STRESSED not set — skipping stressed WS benchmark")
	}

	// Override BENCH_SESSION_ID for wsBenchSetup.
	orig := os.Getenv("BENCH_SESSION_ID")
	os.Setenv("BENCH_SESSION_ID", sessionID)
	defer func() {
		if orig != "" {
			os.Setenv("BENCH_SESSION_ID", orig)
		} else {
			os.Unsetenv("BENCH_SESSION_ID")
		}
	}()

	conn, cleanup := wsBenchSetup(t)
	defer cleanup()

	const warmup = 10
	const measured = 1000

	// Warm-up.
	for i := 0; i < warmup; i++ {
		sendInputAndWaitForEcho(t, conn, 'w')
	}

	var gcBefore runtime.MemStats
	runtime.ReadMemStats(&gcBefore)

	durations := make([]time.Duration, 0, measured)
	for i := 0; i < measured; i++ {
		d := sendInputAndWaitForEcho(t, conn, 'Q')
		durations = append(durations, d)
		time.Sleep(1 * time.Millisecond)
	}

	var gcAfter runtime.MemStats
	runtime.ReadMemStats(&gcAfter)

	result := benchutil.ComputeBenchResult("WSEcho", "stressed", durations, &gcBefore, &gcAfter)
	benchutil.ReportJSON(t, result)
}

// BenchmarkWSEcho is a standard Go benchmark: send one keystroke over
// WebSocket, wait for the echo. Compatible with `go test -bench`.
func BenchmarkWSEcho(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	conn, cleanup := wsBenchSetup(b)
	defer cleanup()

	// Flush the terminal's line discipline buffer — previous tests
	// (percentile tests) may have sent 1000+ characters without a
	// newline, filling the canonical-mode input buffer (MAX_CANON=1024
	// on macOS). Once full, the line discipline stops echoing.
	// Sending '\n' causes cat to read the buffered line, freeing it.
	// The 'Z' barrier consumes cat's output (which contains Q's from
	// the previous test) so it doesn't confuse subsequent Q matching.
	sendInputAndWaitForEcho(b, conn, '\n')
	sendInputAndWaitForEcho(b, conn, 'Z')

	// Warmup: verify the session echoes and consume any pending
	// text frames (bootstrapComplete, stats) from the read buffer.
	for i := 0; i < 5; i++ {
		sendInputAndWaitForEcho(b, conn, 'Q')
	}

	// Flush the line buffer every 500 iterations to prevent overflow.
	// Each flush: send '\n' (cat outputs buffered Q's), then send 'Z'
	// as a barrier (consumed after all flush output, since tmux
	// preserves event order). StopTimer/StartTimer excludes flush
	// overhead from the measurement.
	const flushInterval = 500

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i > 0 && i%flushInterval == 0 {
			b.StopTimer()
			sendInputAndWaitForEcho(b, conn, '\n')
			sendInputAndWaitForEcho(b, conn, 'Z')
			b.StartTimer()
		}
		sendInputAndWaitForEcho(b, conn, 'Q')
	}
}
