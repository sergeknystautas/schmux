package dashboard

import (
	"path/filepath"
	"testing"

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
	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	_ = st.UpsertPreview(state.WorkspacePreview{
		ID:          "prev_1",
		WorkspaceID: "ws-1",
		TargetHost:  "127.0.0.1",
		TargetPort:  3000,
		ProxyPort:   51853,
	})
	srv := &Server{state: st}
	got := srv.filterProxyPorts([]int{3000, 51853})
	if len(got) != 1 || got[0] != 3000 {
		t.Fatalf("expected [3000], got %#v", got)
	}
}

func TestFilterExistingPreviews(t *testing.T) {
	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	_ = st.UpsertPreview(state.WorkspacePreview{
		ID:          "prev_1",
		WorkspaceID: "ws-1",
		TargetHost:  "127.0.0.1",
		TargetPort:  5173,
		ProxyPort:   51853,
	})
	srv := &Server{state: st}

	ports := []int{5173, 3000} // 5173 already exists, 3000 is new

	filtered := srv.filterExistingPreviews("ws-1", ports)
	if len(filtered) != 1 || filtered[0] != 3000 {
		t.Fatalf("expected only port 3000, got %#v", filtered)
	}
}

func TestIntersectPorts(t *testing.T) {
	candidates := []int{3000, 5173, 8080}
	listening := []int{5173, 8080, 9000} // 3000 not listening, 9000 not in candidates

	ports := intersectPorts(candidates, listening)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %#v", ports)
	}
	// Should be sorted
	if ports[0] != 5173 || ports[1] != 8080 {
		t.Fatalf("expected [5173, 8080], got %#v", ports)
	}
}
