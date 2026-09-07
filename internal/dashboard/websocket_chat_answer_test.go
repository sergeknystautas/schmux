package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestChatWebSocket_AnswerFrameCarriesLabelArrays(t *testing.T) {
	srv, _, st := newTestServer(t)
	t.Cleanup(srv.session.Stop)
	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: wsPath})
	st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: os.Getpid(), CreatedAt: time.Now()})
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "c1"))
	paths.Ensure()
	l, _ := chat.OpenLog(paths.Conversation)
	l.Append(chat.NewUserMessage("earlier", nil))

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var frame struct {
		Type    string        `json:"type"`
		Records []chat.Record `json:"records"`
		Record  chat.Record   `json:"record"`
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "answer", "request_id": "r1", "answers": map[string][]string{"Which?": {"A", "B"}}, "input": map[string]any{"questions": []any{}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn.SetReadDeadline(deadline)
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("record: %v", err)
		}
		if frame.Type == "record" {
			break
		}
	}
	if frame.Record.Type != chat.RecordControl {
		t.Fatalf("expected control record, got %+v", frame.Record)
	}
	if !strings.Contains(string(frame.Record.Line), "A, B") {
		t.Fatalf("line missing joined labels: %s", frame.Record.Line)
	}
	var decoded map[string]any
	_ = json.Unmarshal(frame.Record.Line, &decoded)
	_ = decoded
}

func TestChatWebSocket_AbortFrameReachesRuntime(t *testing.T) {
	srv, _, st := newTestServer(t)
	t.Cleanup(srv.session.Stop)
	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: wsPath})
	st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "codex", Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex, Pid: os.Getpid(), CreatedAt: time.Now()})
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "c1"))
	paths.Ensure()
	log, _ := chat.OpenLog(paths.Conversation)
	log.Append(chat.NewUserMessage("earlier", nil))

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var frame struct {
		Type   string      `json:"type"`
		Record chat.Record `json:"record"`
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "abort", "request_id": "5"}); err != nil {
		t.Fatal(err)
	}
	for {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("record: %v", err)
		}
		if frame.Type == "record" {
			break
		}
	}
	if frame.Record.Type != chat.RecordControl || !strings.Contains(string(frame.Record.Line), `"id":5`) || !strings.Contains(string(frame.Record.Line), `"error"`) {
		t.Fatalf("expected Codex abort control record, got %+v", frame.Record)
	}
}

// TestChatWebSocket_UserActionsPublishWorking verifies that a user
// send from the chat socket drives the headless Nudge to Working,
// not to an empty value (the previous behavior cleared the field).
func TestChatWebSocket_UserActionsPublishWorking(t *testing.T) {
	srv, _, st := newTestServer(t)
	t.Cleanup(srv.session.Stop)
	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: wsPath})
	st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: os.Getpid(), CreatedAt: time.Now()})
	if err := st.UpdateSessionNudge("c1", `{"state":"Needs Input","summary":"x","source":"agent"}`); err != nil {
		t.Fatal(err)
	}
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "c1"))
	paths.Ensure()
	// Wire the chat Nudge callback to the dashboard server so the
	// runtime's publication reaches the stored field. The
	// production wiring happens in the daemon; tests need to do
	// it explicitly.
	srv.session.SetChatNudgeCallback(func(sessionID string, update chat.NudgeUpdate) {
		srv.UpdateChatNudge(sessionID, update.State, update.Summary)
	})

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var frame map[string]any
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "send", "text": "ok"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		sess, _ := st.GetSession("c1")
		if strings.Contains(sess.Nudge, `"state":"Working"`) && strings.Contains(sess.Nudge, `"source":"headless"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected Working/headless, got: %s", sess.Nudge)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestChatWebSocket_SendWhileQuestionPending asserts that a user
// send does not erase a still-pending Needs Input state. The
// runtime's tracker is the source of truth; the chat socket's
// success path no longer clears the field.
func TestChatWebSocket_SendWhileQuestionPending(t *testing.T) {
	srv, _, st := newTestServer(t)
	t.Cleanup(srv.session.Stop)
	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: wsPath})
	st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: os.Getpid(), CreatedAt: time.Now()})
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "c1"))
	paths.Ensure()
	srv.session.SetChatNudgeCallback(func(sessionID string, update chat.NudgeUpdate) {
		srv.UpdateChatNudge(sessionID, update.State, update.Summary)
	})

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var frame map[string]any
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}

	// Simulate a Claude control_request arriving while the socket is
	// open: write it to the output file. The runtime will read it,
	// record it, and the tracker will publish Needs Input.
	out, _ := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	out.WriteString(`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","display_name":"AskUserQuestion","input":{"questions":[{"question":"Which?"}]},"requires_user_interaction":true}}` + "\n")
	out.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		sess, _ := st.GetSession("c1")
		if strings.Contains(sess.Nudge, `"state":"Needs Input"`) && strings.Contains(sess.Nudge, "Which?") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected Needs Input, got: %s", sess.Nudge)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// User sends a follow-up while the question is still pending.
	if err := conn.WriteJSON(map[string]any{"type": "send", "text": "follow-up"}); err != nil {
		t.Fatal(err)
	}
	// Give the runtime a moment to (incorrectly) clear the pending
	// state. We poll the session and assert it never flips away.
	stable := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(stable) {
		sess, _ := st.GetSession("c1")
		if !strings.Contains(sess.Nudge, `"state":"Needs Input"`) ||
			!strings.Contains(sess.Nudge, "Which?") {
			t.Fatalf("send erased pending Needs Input: %s", sess.Nudge)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestChatWebSocket_TwoRequestsShowFirstAndCount verifies that two
// unanswered requests in a row render as the first question with
// `(+1 more)` in the summary. The runtime's tracker aggregates
// requests in arrival order.
func TestChatWebSocket_TwoRequestsShowFirstAndCount(t *testing.T) {
	srv, _, st := newTestServer(t)
	t.Cleanup(srv.session.Stop)
	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: wsPath})
	st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: os.Getpid(), CreatedAt: time.Now()})
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "c1"))
	paths.Ensure()
	srv.session.SetChatNudgeCallback(func(sessionID string, update chat.NudgeUpdate) {
		srv.UpdateChatNudge(sessionID, update.State, update.Summary)
	})

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var frame map[string]any
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}

	out, _ := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	out.WriteString(`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}` + "\n")
	out.WriteString(`{"type":"control_request","request_id":"r2","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"pwd"}}}` + "\n")
	out.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		sess, _ := st.GetSession("c1")
		if strings.Contains(sess.Nudge, "(+1 more)") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected (+1 more), got: %s", sess.Nudge)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
