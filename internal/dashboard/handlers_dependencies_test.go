package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestBuildDependenciesResponse_GroupsAndOSFilter(t *testing.T) {
	rep := detect.DependencyReport{
		PackageManagers: map[string]bool{"homebrew": true},
		Statuses: []detect.DependencyStatus{
			{Dependency: detect.Dependency{ID: "git", DisplayName: "git", Group: "vcs",
				Install: []detect.InstallMethod{
					{OS: "macos", Label: "Homebrew", Command: "brew install git", Requires: "homebrew"},
					{OS: "linux", Label: "apt", Command: "sudo apt install git"},
				}}, Detected: true},
		},
	}
	resp := buildDependenciesResponse(rep, "darwin")
	if resp.OS != "macos" {
		t.Fatalf("OS = %q, want macos", resp.OS)
	}
	if len(resp.Groups) != 1 || resp.Groups[0].ID != "vcs" {
		t.Fatalf("groups wrong: %+v", resp.Groups)
	}
	got := resp.Groups[0].Dependencies[0].Install
	if len(got) != 1 || got[0].Label != "Homebrew" {
		t.Fatalf("macOS install filter wrong: %+v", got)
	}
}

// TestHandleDependencies_RefreshRedetects verifies that ?refresh=1 re-runs the
// native detectors and replaces the cached report. A recognizable stale marker
// is seeded into the cache; after refresh the response must contain a native
// entry (tmux is always present — Detected only varies by host) and the marker
// must be gone, proving detection re-ran and overwrote the cache.
func TestHandleDependencies_RefreshRedetects(t *testing.T) {
	server, _, _ := newTestServer(t)

	server.depReport = detect.DependencyReport{
		PackageManagers: map[string]bool{"__stale__": true},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dependencies?refresh=1", nil)
	rec := httptest.NewRecorder()
	server.handleDependencies(rec, req)

	var resp contracts.DependenciesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var sawTmux bool
	for _, g := range resp.Groups {
		if g.ID != "terminal" {
			continue
		}
		for _, d := range g.Dependencies {
			if d.ID == "tmux" {
				sawTmux = true
			}
		}
	}
	if !sawTmux {
		t.Fatalf("refresh did not repopulate native dependencies: %+v", resp.Groups)
	}

	if server.dependencyReport().PackageManagers["__stale__"] {
		t.Fatalf("refresh did not replace the cached report")
	}
}
