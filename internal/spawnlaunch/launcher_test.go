package spawnlaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/spawnlog"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/style"
)

type fakeConfig struct {
	chatSessions bool
	fenceMode    string
	commStyles   map[string]string
}

func (c fakeConfig) GetChatSessions() bool            { return c.chatSessions }
func (c fakeConfig) GetFenceMode() string             { return c.fenceMode }
func (c fakeConfig) GetCommStyles() map[string]string { return c.commStyles }
func (c fakeConfig) GetGitCloneTimeoutMs() int        { return 1000 }

// fakeModels knows "claude-model" (a model resolving to the claude tool),
// "claude" (a builtin tool), and "lint" (a command target).
type fakeModels struct{}

func (fakeModels) IsModel(name string) (promptable, found bool) {
	switch name {
	case "claude-model", "claude":
		return true, true
	case "lint":
		return false, true
	}
	return false, false
}
func (fakeModels) ResolveTargetToTool(name string) string {
	if name == "claude-model" || name == "claude" {
		return "claude"
	}
	return ""
}
func (fakeModels) FindModel(id string) (detect.Model, bool) {
	if id == "claude-model" {
		return detect.Model{ID: id}, true
	}
	return detect.Model{}, false
}
func (fakeModels) ResolveToolForModel(detect.Model) string { return "claude" }

type fakePersonas struct{}

func (fakePersonas) Get(id string) (*persona.Persona, error) {
	if id == "qa" {
		return &persona.Persona{ID: "qa", Name: "QA", Prompt: "Test things."}, nil
	}
	return nil, errors.New("no such persona")
}

type fakeStyles struct{}

func (fakeStyles) Get(id string) (*style.Style, error) {
	switch id {
	case "terse":
		return &style.Style{ID: "terse", Name: "Terse", Prompt: "Be brief."}, nil
	case "verbose":
		return &style.Style{ID: "verbose", Name: "Verbose", Prompt: "Explain."}, nil
	}
	return nil, errors.New("no such style")
}

type fakeSessions struct {
	mu   sync.Mutex
	opts []session.SpawnOptions
	err  error
}

func (f *fakeSessions) Spawn(_ context.Context, opts session.SpawnOptions) (*state.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opts = append(f.opts, opts)
	if f.err != nil {
		return nil, f.err
	}
	ws := opts.WorkspaceID
	if ws == "" {
		ws = "repo-001"
	}
	return &state.Session{ID: ws + "-s1", WorkspaceID: ws, Nickname: opts.Nickname}, nil
}

type harness struct {
	l         *Launcher
	sessions  *fakeSessions
	broadcast chan struct{}
	prompts   []string
}

func newHarness(t *testing.T, cfg fakeConfig, fenceCommand string) *harness {
	t.Helper()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	h := &harness{sessions: &fakeSessions{}, broadcast: make(chan struct{}, 4)}
	h.l = &Launcher{
		Config:         cfg,
		Models:         fakeModels{},
		Personas:       fakePersonas{},
		Styles:         fakeStyles{},
		Sessions:       h.sessions,
		FenceCommand:   func() (string, bool) { return fenceCommand, fenceCommand != "" },
		RegisterPrompt: func(p string) { h.prompts = append(h.prompts, p) },
		Broadcast:      func() { h.broadcast <- struct{}{} },
		Logger:         log.New(&bytes.Buffer{}),
	}
	return h
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("error %v is not *Error", err)
	}
	return se.Status
}

func readSpawnLog(t *testing.T) []contracts.SpawnLogRecord {
	t.Helper()
	path, _ := spawnlog.SourcePath("spawn")
	data, err := os.ReadFile(path)
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

func TestLocalTarget_ChatTargetSpawnsLogsAndBroadcasts(t *testing.T) {
	h := newHarness(t, fakeConfig{chatSessions: true}, "")

	got, err := h.l.LocalTarget(context.Background(), LocalTargetRequest{
		WorkspaceID: "repo-001",
		TargetName:  "claude-model",
		Prompt:      "do the thing",
		Nickname:    "planner",
		Kind:        state.SessionKindChat,
	})
	if err != nil {
		t.Fatalf("LocalTarget() error = %v", err)
	}

	want := LocalTargetResult{SessionID: "repo-001-s1", WorkspaceID: "repo-001", Target: "claude-model", Prompt: "do the thing", Nickname: "planner"}
	if got != want {
		t.Errorf("result = %+v, want %+v", got, want)
	}
	if len(h.sessions.opts) != 1 {
		t.Fatalf("Spawn called %d times, want 1", len(h.sessions.opts))
	}
	o := h.sessions.opts[0]
	if o.TargetName != "claude-model" || o.WorkspaceID != "repo-001" || o.Prompt != "do the thing" || o.Kind != state.SessionKindChat {
		t.Errorf("spawn options = %+v", o)
	}
	// LocalTarget launches the broadcast asynchronously. Five seconds bounds
	// callback scheduling; expiry fails rather than satisfying the assertion.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	select {
	case <-h.broadcast:
	case <-deadline.C:
		t.Fatal("received 0/1 successful-spawn broadcasts within 5s")
	}
	if len(h.prompts) != 1 || h.prompts[0] != "do the thing" {
		t.Errorf("registered prompts = %v, want the spawn prompt", h.prompts)
	}
	recs := readSpawnLog(t)
	if len(recs) != 1 || recs[0].Status != "ok" || recs[0].Results[0].SessionID != "repo-001-s1" || recs[0].Prompt != "do the thing" {
		t.Errorf("spawn log = %+v", recs)
	}
}

func TestLocalTarget_RejectsBeforeSpawning(t *testing.T) {
	tests := []struct {
		name       string
		cfg        fakeConfig
		fence      string
		req        LocalTargetRequest
		wantStatus int // 0 = plain per-target error, not *Error
		wantText   string
	}{
		{
			name:       "chat sessions disabled",
			cfg:        fakeConfig{chatSessions: false},
			req:        LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", Kind: state.SessionKindChat},
			wantStatus: http.StatusBadRequest,
			wantText:   "chat sessions are disabled",
		},
		{
			name:       "chat kind on target without chat mode",
			cfg:        fakeConfig{chatSessions: true},
			req:        LocalTargetRequest{WorkspaceID: "w", TargetName: "lint", Prompt: "", Kind: state.SessionKindChat},
			wantStatus: http.StatusBadRequest,
			wantText:   "target lint has no chat mode",
		},
		{
			name:       "unknown kind",
			cfg:        fakeConfig{chatSessions: true},
			req:        LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", Kind: "weird"},
			wantStatus: http.StatusBadRequest,
			wantText:   "unknown session kind: weird",
		},
		{
			name:     "unavailable target",
			cfg:      fakeConfig{},
			req:      LocalTargetRequest{WorkspaceID: "w", TargetName: "nope", Prompt: "x"},
			wantText: "target not found: nope",
		},
		{
			name:     "prompt on command target",
			cfg:      fakeConfig{},
			req:      LocalTargetRequest{WorkspaceID: "w", TargetName: "lint", Prompt: "x"},
			wantText: "prompt is not allowed for command targets",
		},
		{
			name:       "fence requested but not installed",
			cfg:        fakeConfig{fenceMode: "enabled"},
			fence:      "",
			req:        LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", Fence: true},
			wantStatus: http.StatusBadRequest,
			wantText:   "fence not available",
		},
		{
			name:       "fence requested but disabled by mode",
			cfg:        fakeConfig{fenceMode: config.FenceModeDisabled},
			fence:      "/usr/bin/fence",
			req:        LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", Fence: true},
			wantStatus: http.StatusBadRequest,
			wantText:   "fenced sessions are disabled",
		},
		{
			name:       "missing persona",
			cfg:        fakeConfig{},
			req:        LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", PersonaID: "ghost"},
			wantStatus: http.StatusBadRequest,
			wantText:   "persona not found: ghost",
		},
		{
			name:       "missing style",
			cfg:        fakeConfig{},
			req:        LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", StyleID: "ghost"},
			wantStatus: http.StatusBadRequest,
			wantText:   "style not found: ghost",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.cfg, tt.fence)

			_, err := h.l.LocalTarget(context.Background(), tt.req)

			if err == nil || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantText)
			}
			var se *Error
			if isReq := errors.As(err, &se); isReq != (tt.wantStatus != 0) {
				t.Fatalf("errors.As(*Error) = %v, want %v (error %v)", isReq, tt.wantStatus != 0, err)
			}
			if tt.wantStatus != 0 && se.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", se.Status, tt.wantStatus)
			}
			if len(h.sessions.opts) != 0 {
				t.Errorf("Spawn called despite rejection: %+v", h.sessions.opts)
			}
			select {
			case <-h.broadcast:
				t.Error("rejected request broadcast")
			default:
			}
		})
	}
}

func TestLocalTarget_FenceAvailablePassesCommand(t *testing.T) {
	h := newHarness(t, fakeConfig{fenceMode: "enabled"}, "/usr/bin/fence")

	if _, err := h.l.LocalTarget(context.Background(), LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", Fence: true}); err != nil {
		t.Fatalf("LocalTarget() error = %v", err)
	}

	o := h.sessions.opts[0]
	if !o.Fence || o.FenceCommand != "/usr/bin/fence" {
		t.Errorf("fence options = (%v, %q)", o.Fence, o.FenceCommand)
	}
}

func TestSpawnLocal_PersonaAndStyleResolution(t *testing.T) {
	tests := []struct {
		name          string
		cfg           fakeConfig
		req           LocalTargetRequest
		wantStyleID   string
		wantPromptHas []string
		wantNoPrompt  bool
	}{
		{
			name:          "explicit style and persona",
			req:           LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", PersonaID: "qa", StyleID: "terse"},
			wantStyleID:   "terse",
			wantPromptHas: []string{"## Persona: QA", "## Communication Style: Terse"},
		},
		{
			name:          "default style by resolved tool",
			cfg:           fakeConfig{commStyles: map[string]string{"claude": "verbose"}},
			req:           LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model"},
			wantStyleID:   "verbose",
			wantPromptHas: []string{"## Communication Style: Verbose"},
		},
		{
			name:         "explicit none suppresses default style",
			cfg:          fakeConfig{commStyles: map[string]string{"claude": "verbose"}},
			req:          LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", StyleID: "none"},
			wantStyleID:  "",
			wantNoPrompt: true,
		},
		{
			name:         "no persona and no style",
			req:          LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model"},
			wantNoPrompt: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.cfg, "")

			if _, err := h.l.SpawnLocal(context.Background(), tt.req); err != nil {
				t.Fatalf("SpawnLocal() error = %v", err)
			}

			o := h.sessions.opts[0]
			if o.StyleID != tt.wantStyleID {
				t.Errorf("StyleID = %q, want %q", o.StyleID, tt.wantStyleID)
			}
			if o.PersonaID != tt.req.PersonaID {
				t.Errorf("PersonaID = %q, want %q", o.PersonaID, tt.req.PersonaID)
			}
			if tt.wantNoPrompt && o.PersonaPrompt != "" {
				t.Errorf("PersonaPrompt = %q, want empty", o.PersonaPrompt)
			}
			for _, s := range tt.wantPromptHas {
				if !strings.Contains(o.PersonaPrompt, s) {
					t.Errorf("PersonaPrompt %q missing %q", o.PersonaPrompt, s)
				}
			}
		})
	}
}

func TestSpawnLocal_DoesNotLogOrBroadcast(t *testing.T) {
	h := newHarness(t, fakeConfig{}, "")

	if _, err := h.l.SpawnLocal(context.Background(), LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model"}); err != nil {
		t.Fatalf("SpawnLocal() error = %v", err)
	}

	path, _ := spawnlog.SourcePath("spawn")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("SpawnLocal wrote a spawn log record (stat err = %v)", err)
	}
	select {
	case <-h.broadcast:
		t.Error("SpawnLocal broadcast; only LocalTarget does")
	default:
	}
}

func TestLocalTarget_SpawnFailureIsLoggedNotBroadcast(t *testing.T) {
	h := newHarness(t, fakeConfig{}, "")
	h.sessions.err = errors.New("tmux exploded")

	_, err := h.l.LocalTarget(context.Background(), LocalTargetRequest{WorkspaceID: "w", TargetName: "claude-model", Prompt: "p"})

	if err == nil || !strings.Contains(err.Error(), "tmux exploded") {
		t.Fatalf("error = %v, want spawn failure", err)
	}
	var se *Error
	if errors.As(err, &se) {
		t.Errorf("spawn failure surfaced as request rejection: %v", err)
	}
	recs := readSpawnLog(t)
	if len(recs) != 1 || recs[0].Status != "failed" || recs[0].Results[0].Error != "tmux exploded" {
		t.Errorf("spawn log = %+v", recs)
	}
	select {
	case <-h.broadcast:
		t.Error("failed spawn broadcast")
	default:
	}
}

func TestCheckHelpers(t *testing.T) {
	h := newHarness(t, fakeConfig{chatSessions: true, fenceMode: "enabled"}, "/usr/bin/fence")

	if err := h.l.CheckChat([]string{"claude-model", "claude"}); err != nil {
		t.Errorf("CheckChat(chat-capable) = %v", err)
	}
	if err := h.l.CheckChat([]string{"claude-model", "lint"}); err == nil || statusOf(t, err) != http.StatusBadRequest {
		t.Errorf("CheckChat(with command target) = %v, want 400", err)
	}
	if cmd, err := h.l.ResolveFence(); err != nil || cmd != "/usr/bin/fence" {
		t.Errorf("ResolveFence() = (%q, %v)", cmd, err)
	}
	if err := h.l.CheckPersonaStyle("qa", "none"); err != nil {
		t.Errorf("CheckPersonaStyle(valid) = %v", err)
	}
	if err := h.l.CheckPersonaStyle("ghost", ""); err == nil || statusOf(t, err) != http.StatusBadRequest {
		t.Errorf("CheckPersonaStyle(ghost) = %v, want 400", err)
	}
}

func TestFormatAgentSystemPrompt(t *testing.T) {
	p := &persona.Persona{Name: "QA Engineer", Expectations: "Find bugs.", Prompt: "You are QA."}
	st := &style.Style{Name: "Concise", Prompt: "Be brief."}

	tests := []struct {
		name    string
		p       *persona.Persona
		s       *style.Style
		has     []string
		hasNot  []string
		isEmpty bool
	}{
		{"persona with expectations", p, nil, []string{"## Persona: QA Engineer", "### Behavioral Expectations", "### Instructions"}, nil, false},
		{"persona without expectations", &persona.Persona{Name: "Coder", Prompt: "Write."}, nil, []string{"### Instructions"}, []string{"### Behavioral Expectations"}, false},
		{"style only", nil, st, []string{"## Communication Style: Concise", "Be brief."}, []string{"---"}, false},
		{"persona and style", p, st, []string{"## Persona: QA Engineer", "---", "## Communication Style: Concise"}, nil, false},
		{"neither", nil, nil, nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAgentSystemPrompt(tt.p, tt.s)
			if tt.isEmpty && got != "" {
				t.Fatalf("got %q, want empty", got)
			}
			for _, s := range tt.has {
				if !strings.Contains(got, s) {
					t.Errorf("missing %q in %q", s, got)
				}
			}
			for _, s := range tt.hasNot {
				if strings.Contains(got, s) {
					t.Errorf("unexpected %q in %q", s, got)
				}
			}
		})
	}
}
