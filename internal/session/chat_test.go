package session

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestBuildChatCommand_Claude(t *testing.T) {
	target := ResolvedTarget{Name: "claude", Command: "claude", ToolName: "claude", Promptable: true}
	cmd, proto, hs, err := buildChatCommand(target, nil, false, false, "", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if proto.Name() != chat.ProtocolClaude || hs != nil {
		t.Fatalf("proto %s handshake %v", proto.Name(), hs)
	}
	if !strings.HasPrefix(cmd, "claude -p --input-format stream-json --output-format stream-json") ||
		!strings.Contains(cmd, "--permission-prompt-tool stdio") {
		t.Fatalf("cmd: %s", cmd)
	}
	if strings.Contains(cmd, "--dangerously-skip-permissions") || strings.Contains(cmd, "model_instructions_file") || strings.Contains(cmd, "--continue") {
		t.Fatalf("unfenced claude chat: %s", cmd)
	}

	cmd, _, _, err = buildChatCommand(target, nil, true, false, "conv-1", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--resume conv-1", "--dangerously-skip-permissions"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("fenced resume cmd %q lacks %q", cmd, want)
		}
	}

	// The wizard's resume mode: no id, so Claude continues the workspace's
	// most recent conversation.
	cmd, _, _, err = buildChatCommand(target, nil, false, true, "", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cmd, "--permission-prompt-tool stdio --continue") || strings.Contains(cmd, "--resume") {
		t.Fatalf("resume-most-recent cmd: %s", cmd)
	}

	target.Env = map[string]string{"ANTHROPIC_BASE_URL": "https://x.example"}
	cmd, _, _, _ = buildChatCommand(target, nil, false, false, "", "/ws")
	if !strings.HasPrefix(cmd, "ANTHROPIC_BASE_URL=") {
		t.Fatalf("env prefix missing: %s", cmd)
	}

	if _, _, _, err := buildChatCommand(ResolvedTarget{Name: "gemini", Command: "gemini", ToolName: "gemini"}, nil, false, false, "", "/ws"); err == nil {
		t.Fatal("expected error for a harness without a chat mode")
	}
}

func TestPrepareChatFiles_SeedAndHandshake(t *testing.T) {
	dir := t.TempDir()
	old := chat.PathsFor(filepath.Join(dir, "old"))
	old.Ensure()
	l, _ := chat.OpenLog(old.Conversation)
	l.Append(chat.NewUserMessage("earlier", nil))

	p := chat.PathsFor(filepath.Join(dir, "new"))
	hs := [][]byte{[]byte(`{"id":1,"method":"initialize"}`), []byte(`{"method":"initialized"}`)}
	if err := prepareChatFiles(p, old.Conversation, hs); err != nil {
		t.Fatal(err)
	}
	recs, _ := (mustOpen(t, p.Conversation)).ReadAll()
	if len(recs) != 1 || recs[0].Text != "earlier" {
		t.Fatalf("records: %+v (the first prompt is sent through the runtime, not here)", recs)
	}
	in, _ := os.ReadFile(p.Input)
	if string(in) != `{"id":1,"method":"initialize"}`+"\n"+`{"method":"initialized"}`+"\n" {
		t.Fatalf("input: %q", in)
	}
	p2 := chat.PathsFor(filepath.Join(dir, "new2"))
	if err := prepareChatFiles(p2, "", nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p2.Input); len(b) != 0 {
		t.Fatalf("expected empty input, got %q", b)
	}
	if _, err := os.Stat(filepath.Join(p2.Dir, "conversation.jsonl")); err != nil {
		t.Fatal("conversation record must exist even when empty")
	}
}

func TestDefaultChatProtocolMatchesChatPackage(t *testing.T) {
	if state.DefaultChatProtocol != chat.ProtocolClaude {
		t.Fatalf("state.DefaultChatProtocol %q != chat.ProtocolClaude %q", state.DefaultChatProtocol, chat.ProtocolClaude)
	}
}

func TestBuildChatCommand_Codex(t *testing.T) {
	target := ResolvedTarget{Name: "codex", Command: "codex", ToolName: "codex", Promptable: true}
	cmd, proto, hs, err := buildChatCommand(target, nil, true, false, "thread-1", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if proto.Name() != chat.ProtocolCodex || len(hs) != 4 {
		t.Fatalf("proto %s handshake %d", proto.Name(), len(hs))
	}
	if !strings.HasPrefix(cmd, "codex app-server --stdio -c features.default_mode_request_user_input=true") {
		t.Fatalf("cmd: %s", cmd)
	}
	if strings.Contains(cmd, "--dangerously-bypass-approvals-and-sandbox") || strings.Contains(cmd, "thread-1") {
		t.Fatalf("fence and resume are thread parameters, not argv: %s", cmd)
	}
	if strings.Contains(cmd, "model_instructions_file=") {
		t.Fatalf("codex signals through hooks; no instruction flag: %s", cmd)
	}
	if !strings.Contains(string(hs[3]), `"thread/resume"`) || !strings.Contains(string(hs[3]), `"danger-full-access"`) {
		t.Fatalf("handshake thread line: %s", hs[3])
	}
}

func TestEnsureChatRuntime_UsesPersistedProtocol(t *testing.T) {
	m, st, _ := newTestManagerWithWorkspace(t)
	if err := st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex}); err != nil {
		t.Fatal(err)
	}
	rt, err := m.GetChatRuntime("c1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Protocol() != chat.ProtocolCodex {
		t.Fatalf("runtime protocol %q; the session's persisted value wins over the target", rt.Protocol())
	}
	if err := st.AddSession(state.Session{ID: "c0", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}
	rt0, _ := m.GetChatRuntime("c0")
	if rt0.Protocol() != chat.ProtocolClaude {
		t.Fatalf("empty field must read as claude, got %q", rt0.Protocol())
	}
}

func TestChatRuntime_PersistsImagesInTmp(t *testing.T) {
	m, st, _ := newTestManagerWithWorkspace(t)
	if err := st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}
	rt, err := m.GetChatRuntime("c1")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := rt.Send("look", []chat.Image{{MediaType: "image/png", Data: "aGVsbG8="}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Images[0].Path == "" || filepath.Dir(rec.Images[0].Path) != "/tmp" {
		t.Fatalf("path %q must live in /tmp, like the terminal clipboard flow", rec.Images[0].Path)
	}
	if _, err := os.Stat(rec.Images[0].Path); err != nil {
		t.Fatalf("persisted file: %v", err)
	}
	t.Cleanup(func() { os.Remove(rec.Images[0].Path) })
}

func mustOpen(t *testing.T, path string) *chat.Log {
	t.Helper()
	l, err := chat.OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestGetTracker_RejectsChatSessions(t *testing.T) {
	m, st := newTestManager(t)
	if err := st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws", Target: "claude", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetTracker("c1"); err != ErrChatSession {
		t.Fatalf("expected ErrChatSession, got %v", err)
	}
}

// newTestManagerWithWorkspace builds a manager whose session's workspace has a
// real temp path, so ensureChatRuntime can build chat.PathsFor and the runtime
// can write its conversation file.
func newTestManagerWithWorkspace(t *testing.T) (*Manager, *state.State, string) {
	t.Helper()
	cfg := &config.Config{}
	cfg.WorkspacePath = "/tmp/workspaces"
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	m := New(cfg, st, statePath, wm, nil, nil)
	// Chat session dirs live under the schmux home, not the workspace.
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	wsPath := t.TempDir()
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-1",
		Repo:   "https://example.com/r.git",
		Branch: "main",
		Path:   wsPath,
	}); err != nil {
		t.Fatal(err)
	}
	return m, st, wsPath
}

func TestDispose_ChatSessionWritesEndedRecord(t *testing.T) {
	m, st, _ := newTestManagerWithWorkspace(t)
	if err := st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetChatRuntime("c1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Dispose(context.Background(), "c1"); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	recPath := chat.ConversationPath(schmuxdir.ChatSessionDir("ws-1", "c1"))
	l, err := chat.OpenLog(recPath)
	if err != nil {
		t.Fatalf("open record: %v", err)
	}
	recs, _ := l.ReadAll()
	if n := len(recs); n == 0 {
		t.Fatalf("expected at least one record (the ended record), got 0")
	}
	last := recs[len(recs)-1]
	if last.Type != chat.RecordSession || last.Event != "ended" {
		t.Fatalf("last record = %+v, want session ended", last)
	}
}

func TestStop_DoesNotWriteEndedRecord(t *testing.T) {
	m, st, _ := newTestManagerWithWorkspace(t)
	if err := st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetChatRuntime("c1"); err != nil {
		t.Fatal(err)
	}
	m.Stop() // daemon shutdown path: must not write a session ended record.
	recPath := chat.ConversationPath(schmuxdir.ChatSessionDir("ws-1", "c1"))
	recs, err := readJSONL(recPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	for _, r := range recs {
		if r["type"] == "session" && r["event"] == "ended" {
			t.Fatalf("daemon shutdown wrote an ended record: %v", r)
		}
	}
}

func readJSONL(path string) ([]map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []map[string]any
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
