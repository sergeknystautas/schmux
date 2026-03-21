package dashboard

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestDetectPortsFromChunk(t *testing.T) {
	chunk := []byte("ready in 300ms\nLocal: http://localhost:5173/\nNetwork: use --host to expose")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 1 || ports[0] != 5173 {
		t.Fatalf("expected [5173], got %#v", ports)
	}
}

func TestDetectPortsFromChunk_MultipleURLs(t *testing.T) {
	chunk := []byte("Server at http://localhost:3000 and http://127.0.0.1:8080/api")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %#v", ports)
	}
	if ports[0] != 3000 || ports[1] != 8080 {
		t.Fatalf("expected [3000, 8080], got %#v", ports)
	}
}

func TestDetectPortsFromChunk_DefaultPort(t *testing.T) {
	chunk := []byte("Visit http://localhost/ for info")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 1 || ports[0] != 80 {
		t.Fatalf("expected [80] (default http port), got %#v", ports)
	}
}

func TestDetectPortsFromChunk_HttpsDefaultPort(t *testing.T) {
	chunk := []byte("Secure: https://localhost/")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 1 || ports[0] != 443 {
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

func TestDetectPortsFromChunk_AnyHost(t *testing.T) {
	// We don't filter by host - lsof/ss will verify the port is actually listening
	chunk := []byte("API at http://api.example.com:8080/")
	ports := detectPortsFromChunk(chunk)
	if len(ports) != 1 || ports[0] != 8080 {
		t.Fatalf("expected [8080], got %#v", ports)
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

func TestIntersectPorts(t *testing.T) {
	candidates := []int{3000, 5173, 8080}
	listening := []preview.ListeningPort{
		{Host: "127.0.0.1", Port: 5173},
		{Host: "127.0.0.1", Port: 8080},
		{Host: "127.0.0.1", Port: 9000},
	} // 3000 not listening, 9000 not in candidates

	ports := intersectPorts(candidates, listening)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %#v", ports)
	}
	// Should be sorted
	if ports[0].Port != 5173 || ports[1].Port != 8080 {
		t.Fatalf("expected [5173, 8080], got %#v", ports)
	}
}

func TestIntersectPorts_PrefersIPv4(t *testing.T) {
	candidates := []int{5173}
	// Both IPv4 and IPv6 listening on same port
	listening := []preview.ListeningPort{
		{Host: "::1", Port: 5173},
		{Host: "127.0.0.1", Port: 5173},
	}

	ports := intersectPorts(candidates, listening)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %#v", ports)
	}
	// Should prefer IPv4
	if ports[0].Host != "127.0.0.1" {
		t.Fatalf("expected IPv4 (127.0.0.1), got %s", ports[0].Host)
	}
}

func TestIntersectPorts_IPv6Only(t *testing.T) {
	candidates := []int{5173}
	listening := []preview.ListeningPort{
		{Host: "::1", Port: 5173},
	}

	ports := intersectPorts(candidates, listening)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %#v", ports)
	}
	if ports[0].Host != "::1" || ports[0].Port != 5173 {
		t.Fatalf("expected [::1:5173], got %#v", ports)
	}
}

func TestProbeHTTP_ValidServer(t *testing.T) {
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

	if !probeHTTP("127.0.0.1", port, 1*time.Second) {
		t.Fatal("expected probeHTTP to return true for HTTP server")
	}
}

func TestProbeHTTP_NonHTTPListener(t *testing.T) {
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

	if probeHTTP("127.0.0.1", port, 1*time.Second) {
		t.Fatal("expected probeHTTP to return false for non-HTTP listener")
	}
}

func TestProbeHTTP_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if probeHTTP("127.0.0.1", port, 500*time.Millisecond) {
		t.Fatal("expected probeHTTP to return false for closed port")
	}
}

func TestProbeHTTP_IPv6(t *testing.T) {
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

	if !probeHTTP("::1", port, 1*time.Second) {
		t.Fatal("expected probeHTTP to return true for IPv6 HTTP server")
	}
}

func TestFilterNonHTTPPorts(t *testing.T) {
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

func TestFilterAgentPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	agentPort := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()

	ports := []preview.ListeningPort{
		{Host: "127.0.0.1", Port: agentPort},
		{Host: "127.0.0.1", Port: 5173},
	}

	// Current process owns agentPort, so it should be filtered out.
	// Port 5173 is not owned by this process, so it survives.
	filtered := filterAgentPorts(os.Getpid(), ports)
	for _, lp := range filtered {
		if lp.Port == agentPort {
			t.Fatalf("expected agent port %d to be filtered out, got %#v", agentPort, filtered)
		}
	}
}

func TestFilterAgentPorts_ZeroPID(t *testing.T) {
	ports := []preview.ListeningPort{
		{Host: "127.0.0.1", Port: 3000},
	}
	filtered := filterAgentPorts(0, ports)
	if len(filtered) != 1 {
		t.Fatalf("expected passthrough with zero PID, got %#v", filtered)
	}
}

func TestProbeHTTP_Redirect(t *testing.T) {
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

	if !probeHTTP("127.0.0.1", port, 1*time.Second) {
		t.Fatal("expected probeHTTP to return true for server that redirects")
	}
}
