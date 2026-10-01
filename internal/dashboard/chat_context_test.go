package dashboard

import (
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/state"
)

const claudeDeltaLine = `{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"message_delta","usage":{"input_tokens":2,"cache_creation_input_tokens":1799,"cache_read_input_tokens":510390,"output_tokens":219}}}`

const codexTokenUsageLine = `{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"last":{"totalTokens":19015,"inputTokens":18088,"outputTokens":927},"modelContextWindow":258400}}}`

func TestChatContextUsage_SetReportsChange(t *testing.T) {
	var c chatContextUsage
	if !c.set("s1", 512191) {
		t.Fatal("first value must report a change")
	}
	if c.set("s1", 512191) {
		t.Fatal("identical value must not report a change")
	}
	// Compaction or a model switch shrinks the context; that is a change too.
	if !c.set("s1", 40000) {
		t.Fatal("lower value must report a change")
	}
	if got := c.get("s1"); got != 40000 {
		t.Fatalf("get = %d, want 40000", got)
	}
	if got := c.get("unknown"); got != 0 {
		t.Fatalf("unknown session = %d, want 0", got)
	}
}

func addContextTestSessions(t *testing.T, st *state.State) {
	t.Helper()
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, sess := range []state.Session{
		{ID: "s-claude", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, CreatedAt: time.Now()},
		{ID: "s-codex", WorkspaceID: "ws-1", Target: "codex", Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex, CreatedAt: time.Now()},
		{ID: "s-term", WorkspaceID: "ws-1", Target: "claude", CreatedAt: time.Now()},
	} {
		if err := st.AddSession(sess); err != nil {
			t.Fatal(err)
		}
	}
}

func TestObserveChatUsage_ExposesContextTokens(t *testing.T) {
	srv, _, st := newTestServer(t)
	addContextTestSessions(t, st)

	srv.observeChatUsage("s-claude", chat.NewHarness([]byte(claudeDeltaLine)))
	srv.observeChatUsage("s-codex", chat.NewHarness([]byte(codexTokenUsageLine)))
	// A Codex line on a Claude session is not that session's dialect.
	srv.observeChatUsage("s-claude", chat.NewHarness([]byte(codexTokenUsageLine)))
	// A line for a session that no longer exists stores nothing.
	srv.observeChatUsage("s-gone", chat.NewHarness([]byte(claudeDeltaLine)))

	got := map[string]int{}
	for _, ws := range srv.sessionHandlers.buildSessionsResponse() {
		for _, s := range ws.Sessions {
			got[s.ID] = s.ContextTokens
		}
	}
	want := map[string]int{"s-claude": 512191, "s-codex": 18088, "s-term": 0}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s context_tokens = %d, want %d (all: %v)", id, got[id], w, got)
		}
	}
	if v := srv.chatContext.get("s-gone"); v != 0 {
		t.Errorf("unknown session stored %d", v)
	}
}

// A context change must reach dashboard clients without any other trigger.
func TestObserveChatUsage_BroadcastsContextChange(t *testing.T) {
	srv, _, st := newTestServer(t)
	addContextTestSessions(t, st)

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()
	readDashboardMsg(t, conn, 2*time.Second) // initial sessions snapshot

	srv.observeChatUsage("s-claude", chat.NewHarness([]byte(claudeDeltaLine)))

	// Deadline is a failure backstop over the 100ms broadcast debounce.
	msg := readDashboardMsg(t, conn, 3*time.Second)
	if msg["type"] != "sessions" {
		t.Fatalf("message type = %v, want sessions: %v", msg["type"], msg)
	}
	var found any
	for _, ws := range msg["workspaces"].([]any) {
		for _, s := range ws.(map[string]any)["sessions"].([]any) {
			if sess := s.(map[string]any); sess["id"] == "s-claude" {
				found = sess["context_tokens"]
			}
		}
	}
	if found != float64(512191) {
		t.Fatalf("broadcast context_tokens = %v, want 512191; message: %v", found, msg)
	}
}
