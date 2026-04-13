package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
)

func TestHandleDetectionSummary(t *testing.T) {
	t.Run("returns detection summary with expected structure", func(t *testing.T) {
		logger := log.NewWithOptions(io.Discard, log.Options{})
		cfg := config.CreateDefault(t.TempDir() + "/config.json")
		h := &ConfigHandlers{
			models: models.New(cfg, nil, "", logger),
			logger: logger,
			detectedVCS: []detect.VCSTool{
				{Name: "git", Path: "/usr/bin/git"},
			},
			detectedTmux: detect.TmuxStatus{
				Available: true,
				Path:      "/usr/bin/tmux",
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/api/detection-summary", nil)
		rr := httptest.NewRecorder()

		h.handleDetectionSummary(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}

		var resp contracts.DetectionSummaryResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if resp.Status != "ready" {
			t.Errorf("expected status 'ready', got %q", resp.Status)
		}

		// Agents should be a non-nil slice (may be empty depending on test environment)
		if resp.Agents == nil {
			t.Error("expected agents to be non-nil")
		}

		// VCS should contain the git entry we set
		if len(resp.VCS) != 1 {
			t.Fatalf("expected 1 VCS entry, got %d", len(resp.VCS))
		}
		if resp.VCS[0].Name != "git" {
			t.Errorf("expected VCS name 'git', got %q", resp.VCS[0].Name)
		}
		if resp.VCS[0].Path != "/usr/bin/git" {
			t.Errorf("expected VCS path '/usr/bin/git', got %q", resp.VCS[0].Path)
		}

		// Tmux should reflect what we set
		if !resp.Tmux.Available {
			t.Error("expected tmux available to be true")
		}
		if resp.Tmux.Path != "/usr/bin/tmux" {
			t.Errorf("expected tmux path '/usr/bin/tmux', got %q", resp.Tmux.Path)
		}
	})

	t.Run("returns empty arrays when nothing detected", func(t *testing.T) {
		logger := log.NewWithOptions(io.Discard, log.Options{})
		cfg := config.CreateDefault(t.TempDir() + "/config.json")
		h := &ConfigHandlers{
			models: models.New(cfg, nil, "", logger),
			logger: logger,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/detection-summary", nil)
		rr := httptest.NewRecorder()

		h.handleDetectionSummary(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp contracts.DetectionSummaryResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if resp.Status != "ready" {
			t.Errorf("expected status 'ready', got %q", resp.Status)
		}

		if len(resp.VCS) != 0 {
			t.Errorf("expected 0 VCS entries, got %d", len(resp.VCS))
		}

		if resp.Tmux.Available {
			t.Error("expected tmux available to be false")
		}
	})
}
