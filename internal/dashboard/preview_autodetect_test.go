package dashboard

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newAutodetectPreviewManager creates a preview manager wired with a noop workspace manager.
func newAutodetectPreviewManager(st *state.State, logger *log.Logger) *preview.Manager {
	m := preview.NewManager(st, 3, 20, false, 53000, 10, false, "", "", logger, nil)
	m.SetWorkspaceManager(&noopTabWorkspaceManager{st: st})
	return m
}

func TestDetectPortsFromChunk(t *testing.T) {
	chunk := []byte("ready in 300ms\nLocal: http://localhost:5173/\nNetwork: use --host to expose")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 1 || ports[0].Port != 5173 {
		t.Fatalf("expected [5173], got %#v", ports)
	}
}

func TestDetectPortsFromChunk_MultipleURLs(t *testing.T) {
	chunk := []byte("Server at http://localhost:3000 and http://127.0.0.1:8080/api")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %#v", ports)
	}
	if ports[0].Port != 3000 || ports[1].Port != 8080 {
		t.Fatalf("expected [3000, 8080], got %#v", ports)
	}
}

func TestDetectPortsFromChunk_DefaultPort(t *testing.T) {
	chunk := []byte("Visit http://localhost/ for info")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 1 || ports[0].Port != 80 {
		t.Fatalf("expected [80] (default http port), got %#v", ports)
	}
}

func TestDetectPortsFromChunk_HttpsDefaultPort(t *testing.T) {
	chunk := []byte("Secure: https://localhost/")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 1 || ports[0].Port != 443 {
		t.Fatalf("expected [443] (default https port), got %#v", ports)
	}
}

func TestDetectPortsFromChunk_NoFalsePositive(t *testing.T) {
	chunk := []byte("local files changed: 1\nworkers: 10\nstatus: ok")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 0 {
		t.Fatalf("expected no ports, got %#v", ports)
	}
}

func TestDetectPortsFromChunk_NonLoopbackRejected(t *testing.T) {
	// Non-loopback hosts are rejected — only localhost/127.0.0.1/::1 are accepted
	chunk := []byte("API at http://api.example.com:8080/")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 0 {
		t.Fatalf("expected no ports for non-loopback host, got %#v", ports)
	}
}

func TestDetectPortsFromChunk_DedupByHostPort(t *testing.T) {
	// Same host+port appearing twice should dedup
	chunk := []byte("http://localhost:3000 and http://localhost:3000/app and http://127.0.0.1:3000")
	ports := detectPortsFromChunk(chunk)
	// localhost:3000 and 127.0.0.1:3000 are different host+port pairs
	if len(ports) != 2 {
		t.Fatalf("expected 2 (localhost:3000 and 127.0.0.1:3000), got %#v", ports)
	}
}

func TestHandleSessionOutputChunk_DetectsURLAcrossChunkBoundaries(t *testing.T) {
	srv, st, cleanup := newPreviewAutodetectTestServer(t, log.NewWithOptions(io.Discard, log.Options{}))
	defer cleanup()

	addPreviewAutodetectFixture(t, st, "sess-1", "ws-1", 4242)

	srv.handleSessionOutputChunk("sess-1", []byte("Vellum running at http://127.0.0."))
	if len(srv.previewCandidates) != 0 {
		t.Fatalf("expected no candidate before URL is complete, got %#v", srv.previewCandidates)
	}

	srv.handleSessionOutputChunk("sess-1", []byte("1:6500\n"))
	key := previewCandidateKey("ws-1", "127.0.0.1", 6500)
	candidate := srv.previewCandidates[key]
	if candidate == nil {
		t.Fatalf("expected queued candidate for completed URL, got %#v", srv.previewCandidates)
	}
	if candidate.SessionID != "sess-1" || candidate.Port != 6500 || candidate.Host != "127.0.0.1" {
		t.Fatalf("unexpected candidate: %#v", candidate)
	}
}

func TestProcessPreviewCandidates_RetriesAfterInitialVerificationMiss(t *testing.T) {
	srv, st, cleanup := newPreviewAutodetectTestServer(t, log.NewWithOptions(io.Discard, log.Options{}))
	defer cleanup()

	addPreviewAutodetectFixture(t, st, "sess-2", "ws-2", 5252)

	srv.lookupPortOwner = func(port int) (int, error) { return 5252, nil }
	probeCalls := 0
	srv.previewHTTPProbe = func(lp preview.ListeningPort) bool {
		probeCalls++
		return probeCalls >= 2
	}

	srv.handleSessionOutputChunk("sess-2", []byte("ready at http://127.0.0.1:6501\n"))

	firstAttempt := time.Now().UTC()
	srv.processPreviewCandidates(firstAttempt)
	if _, ok := st.FindPreview("ws-2", "127.0.0.1", 6501); ok {
		t.Fatal("preview should not be created on the first failed verification")
	}
	if len(srv.previewCandidates) != 1 {
		t.Fatalf("expected candidate to remain queued after first miss, got %d", len(srv.previewCandidates))
	}

	srv.processPreviewCandidates(firstAttempt.Add(defaultPreviewCandidateInterval + 10*time.Millisecond))
	if _, ok := st.FindPreview("ws-2", "127.0.0.1", 6501); !ok {
		t.Fatal("expected preview to be created after retry succeeds")
	}
}

func TestProcessPreviewCandidates_RemovesCandidateAfterSuccess(t *testing.T) {
	srv, st, cleanup := newPreviewAutodetectTestServer(t, log.NewWithOptions(io.Discard, log.Options{}))
	defer cleanup()

	addPreviewAutodetectFixture(t, st, "sess-success", "ws-success", 7373)

	srv.lookupPortOwner = func(port int) (int, error) { return 7373, nil }
	srv.previewHTTPProbe = func(lp preview.ListeningPort) bool { return true }

	srv.handleSessionOutputChunk("sess-success", []byte("ready at http://127.0.0.1:6503\n"))
	srv.processPreviewCandidates(time.Now().UTC())

	if _, ok := st.FindPreview("ws-success", "127.0.0.1", 6503); !ok {
		t.Fatal("expected preview after successful verification")
	}
	if len(srv.previewCandidates) != 0 {
		t.Fatalf("expected candidate queue to be empty after success, got %#v", srv.previewCandidates)
	}
}

func TestProcessPreviewCandidates_LogsDecisionPoints(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{Level: log.DebugLevel})
	srv, st, cleanup := newPreviewAutodetectTestServer(t, logger)
	defer cleanup()

	addPreviewAutodetectFixture(t, st, "sess-3", "ws-3", 6262)

	srv.lookupPortOwner = func(port int) (int, error) { return 6262, nil }
	srv.previewHTTPProbe = func(lp preview.ListeningPort) bool { return false }

	srv.handleSessionOutputChunk("sess-3", []byte("ready at http://127.0.0.1:6502\n"))
	srv.processPreviewCandidates(time.Now().UTC())

	logs := buf.String()
	if !strings.Contains(logs, "candidate queued") {
		t.Fatalf("expected queued log, got %q", logs)
	}
	if !strings.Contains(logs, "candidate waiting for http readiness") {
		t.Fatalf("expected readiness log, got %q", logs)
	}
}

func newPreviewAutodetectTestServer(t *testing.T, logger *log.Logger) (*Server, *state.State, func()) {
	t.Helper()

	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if logger == nil {
		logger = log.NewWithOptions(io.Discard, log.Options{})
	}
	srv := &Server{
		config:                   &config.Config{Network: &config.NetworkConfig{Port: 7337}},
		state:                    st,
		logger:                   logger,
		shutdownCtx:              context.Background(),
		previewManager:           newAutodetectPreviewManager(st, logger),
		previewStreamBuffers:     make(map[string]string),
		previewCandidates:        make(map[string]*previewCandidate),
		previewCandidateInterval: defaultPreviewCandidateInterval,
		broadcastDone:            make(chan struct{}),
		broadcastExited:          make(chan struct{}),
		broadcastReady:           make(chan struct{}),
	}
	return srv, st, func() { srv.previewManager.Stop() }
}

func addPreviewAutodetectFixture(t *testing.T, st *state.State, sessionID, workspaceID string, pid int) {
	t.Helper()

	if err := st.AddWorkspace(state.Workspace{ID: workspaceID, Path: t.TempDir()}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	if err := st.AddSession(state.Session{ID: sessionID, WorkspaceID: workspaceID, Pid: pid}); err != nil {
		t.Fatalf("add session: %v", err)
	}
}

func TestFilterProxyPorts_IgnoresKnownProxyPorts(t *testing.T) {
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	_ = st.UpsertPreview(state.WorkspacePreview{
		ID:          "prev_1",
		WorkspaceID: "ws-1",
		TargetHost:  "127.0.0.1",
		TargetPort:  3000,
		ProxyPort:   51853,
	})
	srv := &Server{state: st}
	got := srv.filterProxyPorts([]preview.ListeningPort{{Host: "127.0.0.1", Port: 3000}, {Host: "127.0.0.1", Port: 51853}})
	if len(got) != 1 || got[0].Port != 3000 {
		t.Fatalf("expected [3000], got %#v", got)
	}
}

func TestFilterExistingPreviews(t *testing.T) {
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	_ = st.UpsertPreview(state.WorkspacePreview{
		ID:          "prev_1",
		WorkspaceID: "ws-1",
		TargetHost:  "127.0.0.1",
		TargetPort:  5173,
		ProxyPort:   51853,
	})
	srv := &Server{state: st}

	ports := []preview.ListeningPort{{Host: "127.0.0.1", Port: 5173}, {Host: "127.0.0.1", Port: 3000}} // 5173 already exists, 3000 is new

	filtered := srv.filterExistingPreviews("ws-1", ports)
	if len(filtered) != 1 || filtered[0].Port != 3000 {
		t.Fatalf("expected only port 3000, got %#v", filtered)
	}
}

func TestFilterDaemonPort_BlocksDaemonPort(t *testing.T) {
	cfg := &config.Config{Network: &config.NetworkConfig{Port: 7337}}
	srv := &Server{config: cfg}

	ports := []preview.ListeningPort{
		{Host: "127.0.0.1", Port: 7337},
		{Host: "127.0.0.1", Port: 3000},
	}

	filtered := srv.filterDaemonPort(ports)
	if len(filtered) != 1 || filtered[0].Port != 3000 {
		t.Fatalf("expected [3000], got %#v", filtered)
	}
}

func TestFilterDaemonPort_PassthroughWhenNoMatch(t *testing.T) {
	cfg := &config.Config{Network: &config.NetworkConfig{Port: 7337}}
	srv := &Server{config: cfg}

	ports := []preview.ListeningPort{
		{Host: "127.0.0.1", Port: 3000},
		{Host: "127.0.0.1", Port: 8080},
	}

	filtered := srv.filterDaemonPort(ports)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 ports, got %#v", filtered)
	}
}

func TestOwnershipMapIncludesSessionPID(t *testing.T) {
	pid := os.Getpid()
	descendantPIDs := make(map[int]bool)
	descendantPIDs[pid] = true
	for _, dpid := range getDescendantPIDs(pid) {
		descendantPIDs[dpid] = true
	}
	if !descendantPIDs[pid] {
		t.Fatal("session PID must be in ownership map for command sessions where PID == dev server")
	}
}

func TestFilterNonHTTPPorts_ValidServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	// Retry — server goroutine may not have entered Accept() yet.
	// Generous deadline + sleep to avoid busy-spin under parallel load.
	deadline := time.Now().Add(5 * time.Second)
	for {
		filtered := filterNonHTTPPorts([]preview.ListeningPort{{Host: "127.0.0.1", Port: port}})
		if len(filtered) == 1 && filtered[0].Port == port {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected filterNonHTTPPorts to keep HTTP server port")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFilterNonHTTPPorts_NonHTTPListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Write([]byte("not http\n"))
		conn.Close()
	}()
	defer ln.Close()

	filtered := filterNonHTTPPorts([]preview.ListeningPort{{Host: "127.0.0.1", Port: port}})
	if len(filtered) != 0 {
		t.Fatalf("expected no ports for non-HTTP listener, got %#v", filtered)
	}
}

func TestFilterNonHTTPPorts_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	filtered := filterNonHTTPPorts([]preview.ListeningPort{{Host: "127.0.0.1", Port: port}})
	if len(filtered) != 0 {
		t.Fatalf("expected no ports for closed port, got %#v", filtered)
	}
}

func TestFilterNonHTTPPorts_IPv6(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback not available")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	// Retry — server goroutine may not have entered Accept() yet under load
	deadline := time.Now().Add(5 * time.Second)
	for {
		filtered := filterNonHTTPPorts([]preview.ListeningPort{{Host: "::1", Port: port}})
		if len(filtered) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected 1 port for IPv6 HTTP server, got %#v", filtered)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFilterNonHTTPPorts_Mixed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpPort := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	nonHTTPPort := tcpLn.Addr().(*net.TCPAddr).Port
	go func() {
		conn, err := tcpLn.Accept()
		if err != nil {
			return
		}
		conn.Write([]byte("not http\n"))
		conn.Close()
	}()
	defer tcpLn.Close()

	ports := []preview.ListeningPort{
		{Host: "127.0.0.1", Port: httpPort},
		{Host: "127.0.0.1", Port: nonHTTPPort},
	}

	filtered := filterNonHTTPPorts(ports)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 port, got %d: %#v", len(filtered), filtered)
	}
	if filtered[0].Port != httpPort {
		t.Fatalf("expected port %d, got %d", httpPort, filtered[0].Port)
	}
}

func TestFilterNonHTTPPorts_Redirect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%d/app", port), http.StatusFound)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	filtered := filterNonHTTPPorts([]preview.ListeningPort{{Host: "127.0.0.1", Port: port}})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 port for redirecting server, got %#v", filtered)
	}
}

func TestMatchesBrainstormPIDFile_Match(t *testing.T) {
	wsPath := t.TempDir()
	stateDir := filepath.Join(wsPath, ".superpowers", "brainstorm", "12345-1700000000", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "server.pid"), []byte("42\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !matchesBrainstormPIDFile(wsPath, 42) {
		t.Error("expected match for PID 42")
	}
}

func TestMatchesBrainstormPIDFile_NoMatch(t *testing.T) {
	wsPath := t.TempDir()
	stateDir := filepath.Join(wsPath, ".superpowers", "brainstorm", "12345-1700000000", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "server.pid"), []byte("99\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if matchesBrainstormPIDFile(wsPath, 42) {
		t.Error("expected no match for PID 42 when file contains 99")
	}
}

func TestMatchesBrainstormPIDFile_NoPIDFiles(t *testing.T) {
	wsPath := t.TempDir()

	if matchesBrainstormPIDFile(wsPath, 42) {
		t.Error("expected no match when no PID files exist")
	}
}

func TestMatchesBrainstormPIDFile_GarbageContent(t *testing.T) {
	wsPath := t.TempDir()
	stateDir := filepath.Join(wsPath, ".superpowers", "brainstorm", "12345-1700000000", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "server.pid"), []byte("not-a-pid\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if matchesBrainstormPIDFile(wsPath, 42) {
		t.Error("expected no match when PID file contains garbage")
	}
}

func TestMatchesBrainstormPIDFile_MultipleSessions(t *testing.T) {
	wsPath := t.TempDir()

	// First session — different PID
	stateDir1 := filepath.Join(wsPath, ".superpowers", "brainstorm", "11111-1700000000", "state")
	if err := os.MkdirAll(stateDir1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir1, "server.pid"), []byte("99\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second session — matching PID
	stateDir2 := filepath.Join(wsPath, ".superpowers", "brainstorm", "22222-1700000000", "state")
	if err := os.MkdirAll(stateDir2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir2, "server.pid"), []byte("42\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !matchesBrainstormPIDFile(wsPath, 42) {
		t.Error("expected match when second session has matching PID")
	}
}
