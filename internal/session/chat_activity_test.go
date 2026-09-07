package session

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestChatActivity_RestoredWithoutSubscriber(t *testing.T) {
	m, st, _ := newTestManagerWithWorkspace(t)
	t.Cleanup(m.Stop)
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	finished := created.Add(time.Hour)
	if err := st.AddSession(state.Session{ID: "c1", WorkspaceID: "ws-1", Kind: state.SessionKindChat, CreatedAt: created, NudgeSeq: 7}); err != nil {
		t.Fatal(err)
	}
	p := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "c1"))
	if err := prepareChatFiles(p, "", nil); err != nil {
		t.Fatal(err)
	}
	rec := chat.NewHarness([]byte(`{"type":"result","subtype":"success"}`))
	rec.Ts = finished.Format(time.RFC3339Nano)
	if err := mustOpen(t, p.Conversation).Append(rec); err != nil {
		t.Fatal(err)
	}
	var broadcasts atomic.Int32
	m.SetChatActivityCallback(func() { broadcasts.Add(1) })
	rt, err := m.GetChatRuntime("c1")
	if err != nil {
		t.Fatal(err)
	}
	rt.Stop()
	s, _ := st.GetSession("c1")
	if !s.LastOutputAt.Equal(finished) {
		t.Fatalf("restored activity = %s, want %s", s.LastOutputAt, finished)
	}
	if broadcasts.Load() != 1 || s.NudgeSeq != 7 {
		t.Fatalf("activity broadcasts = %d, NudgeSeq = %d", broadcasts.Load(), s.NudgeSeq)
	}
}
