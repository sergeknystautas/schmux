package dashboard

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func addChatSession(t *testing.T, st *state.State, id string) state.Session {
	t.Helper()
	sess := state.Session{
		ID:          id,
		WorkspaceID: "ws-test",
		Target:      "claude",
		Nickname:    "chat",
		TmuxSession: id,
		Kind:        state.SessionKindChat,
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func addTerminalSession(t *testing.T, st *state.State, id string) state.Session {
	t.Helper()
	sess := state.Session{
		ID:          id,
		WorkspaceID: "ws-test",
		Target:      "command",
		Nickname:    "term",
		TmuxSession: id,
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

// TestChatNudge_StoresJSONAndIncrementsSeq verifies that the headless
// writer's payload is what lands on disk and bumps NudgeSeq.
func TestChatNudge_StoresJSONAndIncrementsSeq(t *testing.T) {
	server, _, st := newTestServer(t)
	addChatSession(t, st, "s1")

	server.UpdateChatNudge("s1", "Needs Input", "Approve Bash: ls")
	got, ok := st.GetSession("s1")
	if !ok {
		t.Fatal("session not found")
	}
	if !strings.Contains(got.Nudge, `"state":"Needs Input"`) ||
		!strings.Contains(got.Nudge, `"summary":"Approve Bash: ls"`) ||
		!strings.Contains(got.Nudge, `"source":"headless"`) {
		t.Fatalf("unexpected nudge: %s", got.Nudge)
	}
	if got.NudgeSeq != 1 {
		t.Fatalf("expected NudgeSeq 1, got %d", got.NudgeSeq)
	}
}

// TestChatNudge_NoOpDedup verifies that an identical snapshot does
// not increment NudgeSeq. Restoration is the typical case: the rebuilt
// tracker emits the same value that is already stored.
func TestChatNudge_NoOpDedup(t *testing.T) {
	server, _, st := newTestServer(t)
	addChatSession(t, st, "s1")

	server.UpdateChatNudge("s1", "Working", "")
	if got, _ := st.GetSession("s1"); got.NudgeSeq != 1 {
		t.Fatalf("first update: NudgeSeq = %d", got.NudgeSeq)
	}
	// Same snapshot.
	server.UpdateChatNudge("s1", "Working", "")
	if got, _ := st.GetSession("s1"); got.NudgeSeq != 1 {
		t.Fatalf("dedup: NudgeSeq = %d", got.NudgeSeq)
	}
	// Changed snapshot.
	server.UpdateChatNudge("s1", "Completed", "Done")
	if got, _ := st.GetSession("s1"); got.NudgeSeq != 2 {
		t.Fatalf("change: NudgeSeq = %d", got.NudgeSeq)
	}
}

// TestChatNudge_MissingSessionIsNoop verifies that the trusted
// in-process entry point is a no-op for sessions that no longer exist
// (the runtime was disposed, etc.).
func TestChatNudge_MissingSessionIsNoop(t *testing.T) {
	server, _, _ := newTestServer(t)
	// Should not panic, should not write anything.
	server.UpdateChatNudge("does-not-exist", "Needs Input", "x")
}

// TestChatNudge_TerminalSessionRejected verifies that the trusted
// in-process entry point is a no-op for non-chat sessions. The
// manager only wires chat runtimes; a defensive guard keeps an
// accidental call from corrupting a terminal session.
func TestChatNudge_TerminalSessionRejected(t *testing.T) {
	server, _, st := newTestServer(t)
	addTerminalSession(t, st, "s1")
	// Set a sentinel value to detect overwrites.
	st.UpdateSessionFunc("s1", func(sess *state.Session) {
		sess.Nudge = `{"state":"Needs Input","summary":"terminal legacy","source":"agent"}`
	})
	server.UpdateChatNudge("s1", "Working", "")
	got, _ := st.GetSession("s1")
	if got.Nudge != `{"state":"Needs Input","summary":"terminal legacy","source":"agent"}` {
		t.Fatalf("terminal session overwritten: %s", got.Nudge)
	}
}

// TestChatNudge_LegacyPrecedenceDoesNotVeto ensures that the
// tier-based precedence rule from HandleStatusEvent does not veto the
// authoritative chat path. We pre-populate the session with a
// Completed payload (the highest tier) and confirm the chat writer
// can still write a Working update.
func TestChatNudge_LegacyPrecedenceDoesNotVeto(t *testing.T) {
	server, _, st := newTestServer(t)
	addChatSession(t, st, "s1")
	// Set a Completed (terminal) state from a prior step.
	st.UpdateSessionFunc("s1", func(sess *state.Session) {
		sess.Nudge = `{"state":"Completed","summary":"Done","source":"agent"}`
	})
	// A new turn starts; chat writer should still publish Working.
	server.UpdateChatNudge("s1", "Working", "")
	got, _ := st.GetSession("s1")
	if !strings.Contains(got.Nudge, `"state":"Working"`) {
		t.Fatalf("veto: %s", got.Nudge)
	}
}

// TestChatNudge_RespectsUnrelatedFields verifies that the headless
// writer preserves all unrelated session fields. UpdateSessionFunc
// takes the session's pointer; we should only touch Nudge and
// NudgeSeq.
func TestChatNudge_RespectsUnrelatedFields(t *testing.T) {
	server, _, st := newTestServer(t)
	sess := addChatSession(t, st, "s1")
	st.UpdateSessionFunc("s1", func(sess *state.Session) {
		sess.Nickname = "renamed"
		sess.PersonaID = "p1"
		sess.Pid = 12345
	})
	server.UpdateChatNudge("s1", "Working", "")
	got, _ := st.GetSession("s1")
	if got.Nickname != "renamed" || got.PersonaID != "p1" || got.Pid != 12345 {
		t.Fatalf("unrelated fields touched: %+v", got)
	}
	_ = sess
}

// TestChatNudge_PayloadShape verifies that the exact JSON shape
// expected by the dashboard's parseNudgeSummary is what we write.
func TestChatNudge_PayloadShape(t *testing.T) {
	server, _, st := newTestServer(t)
	addChatSession(t, st, "s1")
	server.UpdateChatNudge("s1", "Needs Input", "Which fruit?")
	got, _ := st.GetSession("s1")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got.Nudge), &parsed); err != nil {
		t.Fatalf("not JSON: %s", err)
	}
	if parsed["state"] != "Needs Input" {
		t.Fatalf("state: %v", parsed["state"])
	}
	if parsed["summary"] != "Which fruit?" {
		t.Fatalf("summary: %v", parsed["summary"])
	}
	if parsed["source"] != "headless" {
		t.Fatalf("source: %v", parsed["source"])
	}
}

func TestChatNudge_ConcurrentIdenticalSnapshots(t *testing.T) {
	server, _, st := newTestServer(t)
	addChatSession(t, st, "s1")
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { server.UpdateChatNudge("s1", "Working", "") })
	}
	wg.Wait()
	if got, _ := st.GetSession("s1"); got.NudgeSeq != 1 {
		t.Fatalf("identical concurrent updates bumped sequence to %d", got.NudgeSeq)
	}
}

// Drive the real runtime -> manager callback -> stored JSON -> API path with
// captured requests and no chat WebSocket/subscriber. Answers use the runtime
// encoders, including Codex request ID zero.
func TestChatNudge_RuntimeToAPIWithoutSubscriber(t *testing.T) {
	for _, tc := range []struct {
		protocol, fixture, question string
	}{
		{chat.ProtocolClaude, "claude/question.jsonl", "Which option do you prefer?"},
		{chat.ProtocolCodex, "codex/userinput.out.jsonl", "Which fruit?"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			server, _, st := newTestServer(t)
			t.Cleanup(server.session.Stop)
			if err := st.AddWorkspace(state.Workspace{ID: "ws-test", Path: t.TempDir(), Branch: "main"}); err != nil {
				t.Fatal(err)
			}
			addChatSession(t, st, "s1")
			created := time.Now().Add(-time.Hour)
			st.UpdateSessionFunc("s1", func(s *state.Session) {
				s.ChatProtocol = tc.protocol
				s.CreatedAt = created
			})
			server.session.SetChatNudgeCallback(func(id string, u chat.NudgeUpdate) {
				server.UpdateChatNudge(id, u.State, u.Summary)
			})
			// Spawn creates the bridge before constructing its runtime.
			paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-test", "s1"))
			if err := paths.Ensure(); err != nil {
				t.Fatal(err)
			}
			rt, err := server.session.GetChatRuntime("s1")
			if err != nil {
				t.Fatal(err)
			}
			if s, _ := st.GetSession("s1"); !s.LastOutputAt.Equal(created) {
				t.Fatalf("fresh chat activity = %s, want creation time %s", s.LastOutputAt, created)
			}
			initial := server.sessionHandlers.buildSessionsResponse()[0].Sessions[0]
			if initial.NudgeState != "Idle" || initial.NudgeSummary != "" {
				t.Fatalf("fresh chat must not request attention: %+v", initial)
			}
			if _, err := rt.Send("ask me", nil); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open("../../assets/dashboard/src/lib/chat/__fixtures__/" + tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 65536), 16*1024*1024)
			var prefix []string
			var requestID, turnID string
			for scanner.Scan() {
				var v struct {
					Type      string `json:"type"`
					Method    string `json:"method"`
					ID        *int   `json:"id"`
					RequestID string `json:"request_id"`
					Params    struct {
						TurnID string `json:"turnId"`
					} `json:"params"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &v); err != nil {
					t.Fatal(err)
				}
				prefix = append(prefix, scanner.Text())
				if v.Type == "control_request" {
					requestID = v.RequestID
					break
				}
				if v.Method == "item/tool/requestUserInput" && v.ID != nil {
					requestID, turnID = strconv.Itoa(*v.ID), v.Params.TurnID
					break
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if requestID == "" {
				t.Fatal("fixture request not found")
			}
			appendOutput := func(lines ...string) {
				t.Helper()
				out, err := os.OpenFile(paths.Output, os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer out.Close()
				if _, err := out.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
					t.Fatal(err)
				}
			}
			check := func(wantState, summary string) {
				t.Helper()
				deadline := time.Now().Add(2 * time.Second)
				for {
					s, _ := st.GetSession("s1")
					gotState, gotSummary := parseNudgeSummary(s.Nudge)
					if gotState == wantState && gotSummary == summary {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("stored %s; want %s %q", s.Nudge, wantState, summary)
					}
					time.Sleep(10 * time.Millisecond)
				}
				response := server.sessionHandlers.buildSessionsResponse()
				if len(response) != 1 || len(response[0].Sessions) != 1 {
					t.Fatalf("response: %+v", response)
				}
				s := response[0].Sessions[0]
				if s.NudgeState != wantState || s.NudgeSummary != summary {
					t.Fatalf("API nudge: %+v", s)
				}
				stored, _ := st.GetSession("s1")
				if !stored.LastOutputAt.After(created) || s.LastOutputAt != stored.LastOutputAt.Format(time.RFC3339) {
					t.Fatalf("API activity = %q, stored = %s", s.LastOutputAt, stored.LastOutputAt)
				}
			}
			check("Working", "")
			appendOutput(prefix...)
			check("Needs Input", tc.question)
			if err := rt.AnswerQuestion(requestID, map[string][]string{tc.question: {"A"}}, nil); err != nil {
				t.Fatal(err)
			}
			check("Working", "")
			if tc.protocol == chat.ProtocolClaude {
				appendOutput(`{"type":"result","subtype":"success"}`)
			} else {
				b, _ := json.Marshal(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": turnID, "status": "completed"}}})
				appendOutput(string(b))
			}
			check("Completed", "Done")
			rt.Stop() // settle the callback's save before reading the state file
			persisted, err := state.Load(server.statePath, nil)
			if err != nil {
				t.Fatal(err)
			}
			s, _ := persisted.GetSession("s1")
			if !strings.Contains(s.Nudge, `"source":"headless"`) {
				t.Fatalf("persisted Nudge = %s", s.Nudge)
			}
			if ns, summary := parseNudgeSummary(s.Nudge); ns != "Completed" || summary != "Done" {
				t.Fatalf("persisted Nudge = %s", s.Nudge)
			}
			before := s.NudgeSeq
			proto, err := chat.ProtocolFor(tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := chat.NewRuntime("s1", proto, paths, "", "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restored.Stop)
			publications := 0
			restored.SetNudgeCallback(func(u chat.NudgeUpdate) {
				publications++
				server.UpdateChatNudge("s1", u.State, u.Summary)
			})
			restored.Start()
			restored.Stop()
			s, _ = st.GetSession("s1")
			if publications != 1 || s.NudgeSeq != before {
				t.Fatalf("restore publications = %d, sequence %d -> %d", publications, before, s.NudgeSeq)
			}
		})
	}
}
