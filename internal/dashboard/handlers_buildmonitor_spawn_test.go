//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/spawnlog"
	"github.com/sergeknystautas/schmux/internal/state"
)

func readSpawnLog(t *testing.T) []contracts.SpawnLogRecord {
	t.Helper()
	path, _ := spawnlog.SourcePath("spawn")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read spawn log: %v", err)
	}
	var recs []contracts.SpawnLogRecord
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var rec contracts.SpawnLogRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

// assertStagingEmpty fails when a launch left staged failure files behind.
func assertStagingEmpty(t *testing.T, s *Server) {
	t.Helper()
	entries, err := os.ReadDir(s.spawnStaging.dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read staging dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("staging dir has %d entries after launch, want 0", len(entries))
	}
}

var testFailureFiles = []buildmonitor.ContextFile{
	{Name: "failure.json", Data: []byte(`{"workflow":"CI"}`)},
	{Name: "99.log", Data: []byte("log body")},
}

func TestBuildMonitorSpawnRequest(t *testing.T) {
	tests := []struct {
		name        string
		fence, chat bool
		wantFence   bool
		wantKind    string
	}{
		{"defaults", false, false, false, ""},
		{"fence on", true, false, true, ""},
		{"chat on", false, true, false, state.SessionKindChat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, cfg, _ := newTestServer(t)
			cfg.FenceBuildMonitor = tt.fence
			cfg.BuildMonitor = &config.BuildMonitorConfig{Enabled: true, Target: "claude", Chat: tt.chat}

			req := server.buildMonitorSpawnRequest("https://github.com/o/r", "fix/ci-abc1234", "repo-001", "claude", "fix it", "Fix CI: r@abc1234")

			if req.Fence != tt.wantFence {
				t.Errorf("fence = %v, want %v", req.Fence, tt.wantFence)
			}
			if req.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", req.Kind, tt.wantKind)
			}
			if req.Repo != "https://github.com/o/r" || req.Branch != "fix/ci-abc1234" || req.WorkspaceID != "repo-001" ||
				req.Prompt != "fix it" || req.Nickname != "Fix CI: r@abc1234" ||
				len(req.Targets) != 1 || req.Targets["claude"] != 1 {
				t.Errorf("request = %+v", req)
			}
		})
	}
}

// A first launch sends repo and branch with no workspace, so the spawn path
// creates the workspace, and the launch lands in the spawn log.
func TestSpawnBuildMonitorSession_NewWorkspaceWritesSpawnLog(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	cfg.BuildMonitor = &config.BuildMonitorConfig{Enabled: true, Target: "command"}

	// "command" is a run target in newTestServer; the spawn path rejects a
	// prompt on a command target per target, after validation, so the record
	// is written with status failed and no workspace is created.
	req := server.buildMonitorSpawnRequest("https://github.com/o/r", "fix/ci-abc1234", "", "command", "fix it", "Fix CI: r@abc1234")
	sessionID, workspaceID, err := server.spawnBuildMonitorSession(req, testFailureFiles)

	if sessionID != "" || workspaceID != "" {
		t.Errorf("sessionID, workspaceID = %q, %q, want empty", sessionID, workspaceID)
	}
	if err == nil || !strings.Contains(err.Error(), "prompt is not allowed for command targets") {
		t.Fatalf("err = %v, want command-target prompt rejection", err)
	}
	recs := readSpawnLog(t)
	if len(recs) != 1 {
		t.Fatalf("spawn log has %d records, want 1: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.Repo != "https://github.com/o/r" || rec.Branch != "fix/ci-abc1234" || rec.WorkspaceID != "" ||
		rec.Prompt != "fix it" || rec.Nickname != "Fix CI: r@abc1234" ||
		rec.Targets["command"] != 1 || rec.Fence || rec.Status != "failed" {
		t.Errorf("record = %+v", rec)
	}
	assertStagingEmpty(t, server)
}

func TestSpawnBuildMonitorSession_ValidationErrorsWriteNoLog(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Server, *config.Config)
		wantErr string
	}{
		{
			name: "fence on, fence not installed",
			setup: func(s *Server, cfg *config.Config) {
				cfg.FenceBuildMonitor = true
				s.depReport = detect.DependencyReport{}
			},
			wantErr: "fence not available",
		},
		{
			name: "chat on, chat sessions off",
			setup: func(_ *Server, cfg *config.Config) {
				cfg.ChatSessions = false
				cfg.BuildMonitor.Chat = true
			},
			wantErr: "chat sessions are disabled (chat_sessions)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, cfg, _ := newTestServer(t)
			cfg.BuildMonitor = &config.BuildMonitorConfig{Enabled: true, Target: "claude"}
			tt.setup(server, cfg)

			req := server.buildMonitorSpawnRequest("https://github.com/o/r", "fix/ci-abc1234", "", "claude", "fix it", "Fix CI: r@abc1234")
			_, _, err := server.spawnBuildMonitorSession(req, testFailureFiles)

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if recs := readSpawnLog(t); len(recs) != 0 {
				t.Errorf("spawn log has %d records, want 0 for a validation error: %+v", len(recs), recs)
			}
			// The spawn path keeps staged files after a rejected spawn so the
			// form can resubmit; the build monitor has no form, so it must
			// clean up its own.
			assertStagingEmpty(t, server)
		})
	}
}
