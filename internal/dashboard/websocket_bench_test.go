package dashboard_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/benchutil"
)

// wsOutputMessage mirrors dashboard.WSOutputMessage for decoding.
type wsOutputMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// sessionResponseItem is the minimal subset of the /api/sessions response
// needed to auto-discover a running session.
type sessionResponseItem struct {
	ID      string `json:"id"`
	Running bool   `json:"running"`
}

type workspaceResponseItem struct {
	Sessions []sessionResponseItem `json:"sessions"`
}

// wsBenchSetup connects to a running schmux daemon via WebSocket, matching
// the same path the browser takes. It returns the WebSocket connection and
// a cleanup function.
//
// Environment variables:
//   - BENCH_DAEMON_URL: daemon base URL (default http://localhost:7337)
//   - BENCH_SESSION_ID: target session ID (auto-discovered if unset)
func wsBenchSetup(tb testing.TB) (conn *websocket.Conn, cleanup func()) {
	tb.Helper()

	baseURL := os.Getenv("BENCH_DAEMON_URL")
	if baseURL == "" {
		baseURL = "http://localhost:7337"
	}

	sessionID := os.Getenv("BENCH_SESSION_ID")
	if sessionID == "" {
		sessionID = discoverRunningSession(tb, baseURL)
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

	// Read until we get the bootstrap "full" message and discard it.
	for {
		_, rawMsg, err := c.ReadMessage()
		if err != nil {
			c.Close()
			tb.Fatalf("error reading bootstrap message: %v", err)
		}
		var msg wsOutputMessage
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue // skip non-JSON frames
		}
		if msg.Type == "full" {
			break
		}
	}

	cleanup = func() {
		c.Close()
	}
	return c, cleanup
}

// discoverRunningSession calls GET /api/sessions and returns the first
// running session ID.
func discoverRunningSession(tb testing.TB, baseURL string) string {
	tb.Helper()

	resp, err := http.Get(baseURL + "/api/sessions")
	if err != nil {
		tb.Fatalf("failed to reach daemon at %s/api/sessions: %v", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		tb.Fatalf("GET /api/sessions returned %d: %s", resp.StatusCode, string(body))
	}

	var workspaces []workspaceResponseItem
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		tb.Fatalf("failed to decode sessions response: %v", err)
	}

	for _, ws := range workspaces {
		for _, sess := range ws.Sessions {
			if sess.Running {
				return sess.ID
			}
		}
	}

	tb.Fatal("no running sessions found — start a session running 'cat' before running this benchmark")
	return "" // unreachable
}

// sendInputAndWaitForAppend sends a keystroke over WebSocket and waits for
// the next "append" output message. Returns the round-trip duration.
func sendInputAndWaitForAppend(tb testing.TB, conn *websocket.Conn, key string) time.Duration {
	tb.Helper()

	inputMsg, _ := json.Marshal(map[string]string{"type": "input", "data": key})

	start := time.Now()
	if err := conn.WriteMessage(websocket.TextMessage, inputMsg); err != nil {
		tb.Fatalf("failed to send input: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			tb.Fatalf("timeout or error waiting for output: %v", err)
		}
		var msg wsOutputMessage
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue
		}
		if msg.Type == "append" {
			conn.SetReadDeadline(time.Time{}) // clear deadline
			return time.Since(start)
		}
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
		sendInputAndWaitForAppend(t, conn, "w")
	}

	// Measured keystrokes.
	var gcBefore runtime.MemStats
	runtime.ReadMemStats(&gcBefore)

	durations := make([]time.Duration, 0, measured)
	for i := 0; i < measured; i++ {
		d := sendInputAndWaitForAppend(t, conn, "x")
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
//
// Unlike the idle test, we do NOT drain pending messages before each send.
// Deadline-based draining can corrupt a gorilla/websocket connection if a
// timeout fires mid-frame (which is inevitable under continuous flood).
// Instead, like the PTY stressed benchmark, we measure how long it takes
// for *any* output to arrive after SendInput — this captures the real
// contention without risking connection corruption.
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
		sendInputAndWaitForAppend(t, conn, "w")
	}

	var gcBefore runtime.MemStats
	runtime.ReadMemStats(&gcBefore)

	durations := make([]time.Duration, 0, measured)
	for i := 0; i < measured; i++ {
		d := sendInputAndWaitForAppend(t, conn, "x")
		durations = append(durations, d)
		time.Sleep(1 * time.Millisecond)
	}

	var gcAfter runtime.MemStats
	runtime.ReadMemStats(&gcAfter)

	result := benchutil.ComputeBenchResult("WSEcho", "stressed", durations, &gcBefore, &gcAfter)
	benchutil.ReportJSON(t, result)
}

// BenchmarkWSEcho is a standard Go benchmark: send one keystroke over
// WebSocket, wait for one output message. Compatible with `go test -bench`.
func BenchmarkWSEcho(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	conn, cleanup := wsBenchSetup(b)
	defer cleanup()

	inputMsg, _ := json.Marshal(map[string]string{"type": "input", "data": "x"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := conn.WriteMessage(websocket.TextMessage, inputMsg); err != nil {
			b.Fatalf("failed to send input: %v", err)
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, rawMsg, err := conn.ReadMessage()
			if err != nil {
				b.Fatalf("timeout or error waiting for output: %v", err)
			}
			var msg wsOutputMessage
			if err := json.Unmarshal(rawMsg, &msg); err != nil {
				continue
			}
			if msg.Type == "append" {
				conn.SetReadDeadline(time.Time{})
				break
			}
		}
	}
}
