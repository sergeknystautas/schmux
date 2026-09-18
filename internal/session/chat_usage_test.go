package session

import (
	"os"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestChatUsageCallbackWiresRestoredRuntime(t *testing.T) {
	m, st, _ := newTestManagerWithWorkspace(t)
	t.Cleanup(m.Stop)
	if err := st.AddSession(state.Session{
		ID: "usage-chat", WorkspaceID: "ws-1", Kind: state.SessionKindChat,
	}); err != nil {
		t.Fatal(err)
	}
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "usage-chat"))
	if err := prepareChatFiles(paths, "", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := m.GetChatRuntime("usage-chat"); err != nil {
		t.Fatal(err)
	}
	records := make(chan chat.Record, 1)
	sessionIDs := make(chan string, 1)
	m.SetChatUsageCallback(func(sessionID string, record chat.Record) {
		select {
		case sessionIDs <- sessionID:
		default:
		}
		select {
		case records <- record:
		default:
		}
	})

	output, err := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.WriteString(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.27}}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-records:
		if record.Type != chat.RecordHarness {
			t.Fatalf("usage callback record = %+v", record)
		}
		select {
		case got := <-sessionIDs:
			if got != "usage-chat" {
				t.Fatalf("usage callback session ID = %q", got)
			}
		default:
			t.Fatal("usage callback did not deliver session ID")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("usage callback was not wired")
	}
}
