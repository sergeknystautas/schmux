package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newRestartHandler builds a SpawnHandlers over a fresh state seeded with
// sessions covering each restart guard. models is wired so the resume_id_args
// check can resolve builtin targets.
func newRestartHandler(t *testing.T) *SpawnHandlers {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	now := time.Now()
	sessions := []state.Session{
		{ID: "no-resume", WorkspaceID: "ws-1", Target: "claude", ResumeID: "", CreatedAt: now},
		{ID: "disposing-1", WorkspaceID: "ws-1", Target: "claude", ResumeID: "conv", Status: "disposing", CreatedAt: now},
		{ID: "gemini-1", WorkspaceID: "ws-1", Target: "gemini", ResumeID: "conv", CreatedAt: now},
		{ID: "codex-1", WorkspaceID: "ws-1", Target: "codex", ResumeID: "0199aa-bb", CreatedAt: now},
		{ID: "codex-no-id", WorkspaceID: "ws-1", Target: "codex", ResumeID: "", CreatedAt: now},
		{ID: "chat-1", WorkspaceID: "ws-1", Target: "claude", ResumeID: "conv", Kind: state.SessionKindChat, CreatedAt: now},
		{ID: "chat-codex-as-claude", WorkspaceID: "ws-1", Target: "claude", ResumeID: "conv", Kind: state.SessionKindChat, ChatProtocol: "codex-app-server", CreatedAt: now},
		{ID: "chat-gemini", WorkspaceID: "ws-1", Target: "gemini", ResumeID: "conv", Kind: state.SessionKindChat, CreatedAt: now},
	}
	for _, s := range sessions {
		if err := st.AddSession(s); err != nil {
			t.Fatalf("AddSession %s: %v", s.ID, err)
		}
	}
	cfg := &config.Config{}
	return &SpawnHandlers{
		logger: discardLogger(),
		state:  st,
		config: cfg,
		models: models.New(cfg, []detect.Tool{{Name: "claude"}, {Name: "gemini"}, {Name: "codex"}}, "", discardLogger()),
	}
}

func postRestart(t *testing.T, h *SpawnHandlers, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/sessions/{sessionID}/restart", h.handleRestart)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/restart", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestHandleRestart_Guards(t *testing.T) {
	h := newRestartHandler(t)

	cases := []struct {
		id       string
		wantCode int
		wantBody string
	}{
		{"nope", http.StatusNotFound, ""},
		{"no-resume", http.StatusBadRequest, "resume id"},
		{"disposing-1", http.StatusConflict, "disposing"},
		{"gemini-1", http.StatusBadRequest, "resume by id"},
	}
	for _, c := range cases {
		rr := postRestart(t, h, c.id)
		if rr.Code != c.wantCode {
			t.Errorf("%s: status = %d, want %d; body=%s", c.id, rr.Code, c.wantCode, rr.Body.String())
		}
		if c.wantBody != "" && !strings.Contains(rr.Body.String(), c.wantBody) {
			t.Errorf("%s: body = %q, want containing %q", c.id, rr.Body.String(), c.wantBody)
		}
	}
}

// TestRestartEligibility_Codex covers codex resume-by-id: a session with a
// resume id is eligible, one without is rejected by the shared guard.
func TestRestartEligibility_Codex(t *testing.T) {
	h := newRestartHandler(t)
	cases := []struct {
		id       string
		wantMsg  string
		wantCode int
	}{
		{"codex-1", "", 0},
		{"codex-no-id", "session has no resume id", http.StatusBadRequest},
	}
	for _, c := range cases {
		sess, ok := h.state.GetSession(c.id)
		if !ok {
			t.Fatalf("session %s not found", c.id)
		}
		msg, code := h.restartEligibility(sess, sess.Target)
		if msg != c.wantMsg || code != c.wantCode {
			t.Errorf("%s: got (%q, %d), want (%q, %d)", c.id, msg, code, c.wantMsg, c.wantCode)
		}
	}
}

// TestRestartEligibility_Chat covers chat sessions: eligible when the
// resolved harness declares a chat mode, rejected otherwise.
func TestRestartEligibility_Chat(t *testing.T) {
	h := newRestartHandler(t)
	cases := []struct {
		id       string
		wantMsg  string
		wantCode int
	}{
		{"chat-1", "", 0},
		{"chat-codex-as-claude", "claude (default) is running with codex. This is no longer configured, so you cannot restart.", http.StatusBadRequest},
		{"chat-gemini", "harness does not support chat resume", http.StatusBadRequest},
	}
	for _, c := range cases {
		sess, ok := h.state.GetSession(c.id)
		if !ok {
			t.Fatalf("session %s not found", c.id)
		}
		msg, code := h.restartEligibility(sess, sess.Target)
		if msg != c.wantMsg || code != c.wantCode {
			t.Errorf("%s: got (%q, %d), want (%q, %d)", c.id, msg, code, c.wantMsg, c.wantCode)
		}
		if c.wantCode == 0 {
			if rr := getRestartOptions(t, h, c.id); rr.Code != http.StatusOK {
				t.Errorf("%s: restart-options status = %d, want 200; body=%s", c.id, rr.Code, rr.Body.String())
			}
		}
	}
}

func newRestartOptionsHandler(t *testing.T) *SpawnHandlers {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if err := st.AddSession(state.Session{ID: "claude-1", WorkspaceID: "ws-1", Target: "claude", ResumeID: "conv", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	cfg := &config.Config{}
	mm := models.New(cfg, []detect.Tool{{Name: "claude"}, {Name: "gemini"}}, "", discardLogger())
	return &SpawnHandlers{logger: discardLogger(), state: st, config: cfg, models: mm}
}

func getRestartOptions(t *testing.T, h *SpawnHandlers, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/sessions/{sessionID}/restart-options", h.handleRestartOptions)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/restart-options", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestHandleRestartOptions(t *testing.T) {
	h := newRestartOptionsHandler(t)
	rr := getRestartOptions(t, h, "claude-1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp contracts.RestartOptionsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CurrentTarget != "claude" {
		t.Errorf("current_target = %q, want claude", resp.CurrentTarget)
	}
	found := false
	for _, tgt := range resp.Targets {
		if tgt == "claude" {
			found = true
		}
		if tgt == "gemini" {
			t.Errorf("targets must exclude the other-harness target 'gemini', got %v", resp.Targets)
		}
	}
	if !found {
		t.Errorf("targets must include current harness target 'claude', got %v", resp.Targets)
	}
	if resp.FenceAvailable {
		t.Errorf("fence_available should be false when no dependency report is wired")
	}
}

func postRestartBody(t *testing.T, h *SpawnHandlers, sessionID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/sessions/{sessionID}/restart", h.handleRestart)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/restart", strings.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestHandleRestart_CrossHarnessRejected(t *testing.T) {
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if err := st.AddSession(state.Session{ID: "claude-1", WorkspaceID: "ws-1", Target: "claude", ResumeID: "conv", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	cfg := &config.Config{}
	mm := models.New(cfg, []detect.Tool{{Name: "claude"}, {Name: "gemini"}}, "", discardLogger())
	h := &SpawnHandlers{logger: discardLogger(), state: st, config: cfg, models: mm}

	rr := postRestartBody(t, h, "claude-1", `{"target":"gemini"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "same harness") {
		t.Errorf("body = %s; want 'same harness' message", rr.Body.String())
	}
}

// TestHandleRestartOptions_ChatHarnessMoved covers a codex chat whose target
// now resolves to claude: the options list what the conversation can still
// resume on, which leaves out the session's own target.
func TestHandleRestartOptions_ChatHarnessMoved(t *testing.T) {
	h := newRestartHandler(t)
	rr := getRestartOptions(t, h, "chat-codex-as-claude")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want 200", rr.Code, rr.Body.String())
	}
	var resp contracts.RestartOptionsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CurrentTarget != "claude" {
		t.Errorf("current_target = %q, want claude", resp.CurrentTarget)
	}
	if !slices.Contains(resp.Targets, "codex") {
		t.Errorf("targets = %v, want codex included", resp.Targets)
	}
	for _, target := range resp.Targets {
		if tool := h.resolveTargetTool(target); tool != "codex" {
			t.Errorf("target %q resolves to %q, want only codex targets", target, tool)
		}
	}
}

// TestHandleRestartOptions_NoTargets covers a session no target can resume: the
// response is the reason its own target is rejected.
func TestHandleRestartOptions_NoTargets(t *testing.T) {
	h := newRestartHandler(t)
	rr := getRestartOptions(t, h, "gemini-1")
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "resume by id") {
		t.Errorf("status = %d, body = %s; want 400 'resume by id'", rr.Code, rr.Body.String())
	}
}

// TestHandleRestart_ChatHarnessMoved covers restarting a codex chat whose
// target now resolves to claude. Requesting fence stops an accepted target at
// the fence guard, the last check before the session is disposed.
func TestHandleRestart_ChatHarnessMoved(t *testing.T) {
	h := newRestartHandler(t)
	_, fenceErr, _ := h.fenceCommandOrError()
	if fenceErr == "" {
		t.Fatal("fence must be unavailable in this handler")
	}
	h.models.SetRegistryModels([]detect.Model{{
		ID: "glm-5.3", DisplayName: "GLM-5.3", Provider: "zai",
		Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "glm-5.3"}, "codex": {ModelValue: "glm-5.3"}},
	}, {
		ID: "glm-5.4", DisplayName: "GLM-5.4", Provider: "zai",
		Runners: map[string]detect.RunnerSpec{"claude": {ModelValue: "glm-5.4"}, "codex": {ModelValue: "glm-5.4"}},
	}})
	h.config.Models = &config.ModelsConfig{Enabled: map[string]string{"glm-5.3": "claude", "glm-5.4": "codex"}}
	if err := h.state.AddSession(state.Session{ID: "chat-glm", WorkspaceID: "ws-1", Target: "glm-5.3", ResumeID: "conv", Kind: state.SessionKindChat, ChatProtocol: "codex-app-server", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := h.state.AddSession(state.Session{ID: "chat-glm-on-claude", WorkspaceID: "ws-1", Target: "glm-5.4", ResumeID: "conv", Kind: state.SessionKindChat, ChatProtocol: "claude-stream-json", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	cases := []struct {
		name     string
		id       string
		body     string
		wantBody string
	}{
		{"own target", "chat-glm", `{"fence":true}`, "GLM-5.3 is running with codex. This is no longer configured, so you cannot restart."},
		{"own target, claude to codex", "chat-glm-on-claude", `{"fence":true}`, "GLM-5.4 is running with claude. This is no longer configured, so you cannot restart."},
		{"target on the session's protocol", "chat-glm", `{"target":"codex","fence":true}`, fenceErr},
		{"another model on the session's protocol", "chat-glm", `{"target":"glm-5.4","fence":true}`, fenceErr},
		{"other target off the session's protocol", "chat-glm", `{"target":"claude","fence":true}`, "same harness"},
		{"target off the session's protocol", "chat-1", `{"target":"codex","fence":true}`, "same harness"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := postRestartBody(t, h, c.id, c.body)
			var resp struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode %s: %v", rr.Body.String(), err)
			}
			if !strings.Contains(resp.Error, c.wantBody) {
				t.Errorf("error = %q, want containing %q", resp.Error, c.wantBody)
			}
		})
	}
}
