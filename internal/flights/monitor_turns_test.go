package flights

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/chat"
)

func loadFixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	path := filepath.Join("testdata", "monitor", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitJSONL(t, data)
	return lines
}

func splitJSONL(t *testing.T, data []byte) [][]byte {
	t.Helper()
	raw := strings.Split(string(data), "\n")
	out := make([][]byte, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, []byte(line))
	}
	return out
}

// TestPlannerTurn_ClaudeTwoConsecutiveTurns replays a fixture with two
// completed root turns and checks the joined response text and the work key
// (SessionID, EndSeq) are stable across replays.
func TestPlannerTurn_ClaudeTwoConsecutiveTurns(t *testing.T) {
	lines := loadFixtureLines(t, "claude_two_turns.jsonl")
	turns := collectTurns(t, chat.ProtocolClaude, "sess-1", lines)
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(turns))
	}
	wantTexts := []string{
		"First turn: I will explore the codebase.",
		"Second turn: I will plan the work.",
	}
	for i, want := range wantTexts {
		if turns[i].Text != want {
			t.Errorf("turn %d text = %q, want %q", i, turns[i].Text, want)
		}
		if turns[i].Outcome != "completed" {
			t.Errorf("turn %d outcome = %s, want completed", i, turns[i].Outcome)
		}
		if turns[i].SessionID != "sess-1" {
			t.Errorf("turn %d session = %q, want sess-1", i, turns[i].SessionID)
		}
		if turns[i].EndSeq == 0 {
			t.Errorf("turn %d EndSeq = 0, want a sequence", i)
		}
	}

	// Replay the same fixture into a fresh collector and confirm the work
	// keys are stable.
	again := collectTurns(t, chat.ProtocolClaude, "sess-1", lines)
	if len(again) != 2 {
		t.Fatalf("replay turns = %d, want 2", len(again))
	}
	for i := range again {
		if again[i].Key() != turns[i].Key() {
			t.Errorf("replay key mismatch at %d: %s vs %s", i, again[i].Key(), turns[i].Key())
		}
	}
}

// TestPlannerTurn_ClaudeExcludesChildAgentBlocks covers the parent_tool_use_id
// guard: child responses never open or close a turn, and their text is never
// part of the parent's response.
func TestPlannerTurn_ClaudeExcludesChildAgentBlocks(t *testing.T) {
	lines := loadFixtureLines(t, "claude_with_child.jsonl")
	turns := collectTurns(t, chat.ProtocolClaude, "sess-1", lines)
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns))
	}
	if strings.Contains(turns[0].Text, "child") {
		t.Errorf("parent text included child content: %q", turns[0].Text)
	}
	if !strings.Contains(turns[0].Text, "Parent only") {
		t.Errorf("parent text missing expected block: %q", turns[0].Text)
	}
}

// TestPlannerTurn_ClaudeFailed records a failed Claude turn with the harness
// error string, not a fabricated empty response. The partial text is retained
// as evidence for the pilot notification, but no Jev request is made for it.
func TestPlannerTurn_ClaudeFailed(t *testing.T) {
	lines := loadFixtureLines(t, "claude_failed.jsonl")
	turns := collectTurns(t, chat.ProtocolClaude, "sess-1", lines)
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns))
	}
	if turns[0].Outcome != "failed" {
		t.Errorf("outcome = %s, want failed", turns[0].Outcome)
	}
	if !strings.Contains(turns[0].Error, "boom") {
		t.Errorf("error = %q, want to contain boom", turns[0].Error)
	}
	if !strings.Contains(turns[0].Text, "Started") {
		t.Errorf("text = %q, want to contain the partial block as evidence", turns[0].Text)
	}
}

// TestPlannerTurn_CodexTwoConsecutiveTurns replays a Codex fixture with two
// turns and checks the joined agentMessage text, in order, with the items
// from turn/completed taking precedence over earlier item/completed snapshots.
func TestPlannerTurn_CodexTwoConsecutiveTurns(t *testing.T) {
	lines := loadFixtureLines(t, "codex_two_turns.jsonl")
	turns := collectTurns(t, chat.ProtocolCodex, "sess-1", lines)
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(turns))
	}
	wantTexts := []string{
		"First answer.",
		"Second answer.",
	}
	for i, want := range wantTexts {
		if turns[i].Text != want {
			t.Errorf("turn %d text = %q, want %q", i, turns[i].Text, want)
		}
		if turns[i].Outcome != "completed" {
			t.Errorf("turn %d outcome = %s, want completed", i, turns[i].Outcome)
		}
	}
}

// TestPlannerTurn_QuestionDoesNotCloseTurn verifies that a question or
// permission request during a Claude turn does not close it.
func TestPlannerTurn_QuestionDoesNotCloseTurn(t *testing.T) {
	lines := loadFixtureLines(t, "claude_with_question.jsonl")
	turns := collectTurns(t, chat.ProtocolClaude, "sess-1", lines)
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1 (question mid-turn)", len(turns))
	}
	if !strings.Contains(turns[0].Text, "Plan ready") {
		t.Errorf("turn text = %q, want to contain the post-question block", turns[0].Text)
	}
}

// TestPlannerTurn_EmptySuccessfulResponseProducesNoTurn covers the "a
// successful turn with no assistant text needs no Jev request" rule: an
// empty result is still emitted (so the monitor can advance its cursor), but
// the text is empty.
func TestPlannerTurn_EmptySuccessfulResponseProducesNoTurn(t *testing.T) {
	lines := loadFixtureLines(t, "claude_empty.jsonl")
	turns := collectTurns(t, chat.ProtocolClaude, "sess-1", lines)
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns))
	}
	if turns[0].Text != "" {
		t.Errorf("text = %q, want empty", turns[0].Text)
	}
	if turns[0].Outcome != "completed" {
		t.Errorf("outcome = %s, want completed", turns[0].Outcome)
	}
}

// collectTurns feeds each line through a fresh turnCollector and returns the
// completed turns. Synthetic Seq numbers are assigned in order so the work
// key has a meaningful value to compare.
func collectTurns(t *testing.T, protocol, sessionID string, lines [][]byte) []PlannerTurn {
	t.Helper()
	c := newTurnCollector(protocol, sessionID)
	var out []PlannerTurn
	var seq uint64 = 1
	for _, line := range lines {
		rec := chat.Record{Type: chat.RecordHarness, Line: line, Seq: seq}
		seq++
		turn, err := c.consume(rec)
		if err != nil {
			t.Fatalf("consume error: %v", err)
		}
		if turn != nil {
			out = append(out, *turn)
		}
	}
	return out
}

func TestPlannerTurnRecordedCodexInterruption(t *testing.T) {
	path := "../../assets/dashboard/src/lib/chat/__fixtures__/codex/interrupt.out.jsonl"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	turns := collectTurns(t, chat.ProtocolCodex, "planner", splitJSONL(t, data))
	if len(turns) != 2 || turns[0].Outcome != "interrupted" || turns[1].Text != "SECOND-TURN-OK PELICAN" || turns[1].FinalText != "SECOND-TURN-OK PELICAN" {
		t.Fatalf("recorded Codex turns = %+v", turns)
	}
}

func TestPlannerTurnRecordedClaudeQuestion(t *testing.T) {
	data, err := os.ReadFile("../../assets/dashboard/src/lib/chat/__fixtures__/claude/question.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	turns := collectTurns(t, chat.ProtocolClaude, "planner", splitJSONL(t, data))
	if len(turns) != 1 || turns[0].Text != "CHOSE Beta" || turns[0].FinalText != "CHOSE Beta" {
		t.Fatalf("recorded Claude question turn=%+v", turns)
	}
}

func TestPlannerTurnRecordedCodexFullTurn(t *testing.T) {
	data, err := os.ReadFile("../../assets/dashboard/src/lib/chat/__fixtures__/codex/edits.out.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	turns := collectTurns(t, chat.ProtocolCodex, "planner", splitJSONL(t, data))
	if len(turns) != 1 {
		t.Fatalf("recorded Codex turns=%+v", turns)
	}
	want := "I’ll record the requested two-step plan, then perform the file update.\nDONE"
	if turns[0].Text != want || turns[0].FinalText != "DONE" {
		t.Fatalf("text=%q final=%q; want text=%q final=DONE", turns[0].Text, turns[0].FinalText, want)
	}
}

func TestPlannerTurnRecordedCodexExcludesChildTurns(t *testing.T) {
	data, err := os.ReadFile("../../assets/dashboard/src/lib/chat/__fixtures__/codex/activity-live.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	turns := collectTurns(t, chat.ProtocolCodex, "planner", splitJSONL(t, data))
	if len(turns) != 1 || !strings.Contains(turns[0].Text, "23 × 29 = 667") || turns[0].FinalText == "323" {
		t.Fatalf("root turn lost or replaced by child: %+v", turns)
	}
}

func TestPlannerTurnCodexCommentaryIsNotFinal(t *testing.T) {
	lines := splitJSONL(t, []byte(`{"method":"turn/started","params":{"threadId":"root","turn":{"id":"one"}}}
{"method":"item/completed","params":{"threadId":"root","turnId":"one","item":{"type":"agentMessage","id":"comment","phase":"commentary","text":"{\"guidance\":\"Premature guidance\"}"}}}
{"method":"turn/completed","params":{"threadId":"root","turn":{"id":"one","status":"completed","items":[]}}}`))
	turns := collectTurns(t, chat.ProtocolCodex, "pilot", lines)
	if len(turns) != 1 || turns[0].Text == "" || turns[0].FinalText != "" {
		t.Fatalf("commentary must be evidence only: %+v", turns)
	}
}

func TestCollectorRecordsClaudeSkillInvocation(t *testing.T) {
	skillCall := func(parent string) string {
		field := ""
		if parent != "" {
			field = `"parent_tool_use_id":"` + parent + `",`
		}
		return `{"type":"assistant",` + field + `"uuid":"u1","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"superpowers:executing-plans"}}]}}`
	}
	root := collectTurns(t, chat.ProtocolClaude, "sess-1", [][]byte{[]byte(skillCall("")), []byte(`{"type":"result","subtype":"success"}`)})
	if len(root) != 1 {
		t.Fatalf("turns = %d, want 1", len(root))
	}
	if want := []SkillUse{{Skill: "superpowers:executing-plans", Seq: 1, Evidence: "invoked"}}; !slices.Equal(root[0].Skills, want) {
		t.Errorf("root skills = %+v, want %+v", root[0].Skills, want)
	}
	child := collectTurns(t, chat.ProtocolClaude, "sess-1", [][]byte{[]byte(skillCall("toolu_1")), []byte(`{"type":"result","subtype":"success"}`)})
	if len(child) != 1 || len(child[0].Skills) != 0 {
		t.Errorf("child agent skill call recorded: %+v", child)
	}
}

func TestCollectorClearsSkillsBetweenTurns(t *testing.T) {
	turns := collectTurns(t, chat.ProtocolClaude, "sess-1", [][]byte{
		[]byte(`{"type":"assistant","uuid":"u1","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"superpowers:brainstorming"}}]}}`),
		[]byte(`{"type":"result","subtype":"success"}`),
		[]byte(`{"type":"assistant","uuid":"u2","message":{"content":[{"type":"text","text":"Second"}]}}`),
		[]byte(`{"type":"result","subtype":"success"}`),
	})
	if len(turns) != 2 || len(turns[0].Skills) != 1 || len(turns[1].Skills) != 0 {
		t.Fatalf("skills per turn = %+v", turns)
	}
}

func TestCollectorRecordsCodexSkillFileRead(t *testing.T) {
	command := `{"type":"commandExecution","id":"c1","command":"/bin/zsh -lc 'cat /x/skills/executing-plans/SKILL.md'"}`
	turns := collectTurns(t, chat.ProtocolCodex, "sess-1", [][]byte{
		[]byte(`{"method":"thread/started","params":{"thread":{"id":"th"}}}`),
		[]byte(`{"method":"turn/started","params":{"threadId":"th","turnId":"t1"}}`),
		[]byte(`{"method":"item/started","params":{"turnId":"t1","item":` + command + `}}`),
		[]byte(`{"method":"item/completed","params":{"turnId":"t1","item":` + command + `}}`),
		[]byte(`{"method":"turn/completed","params":{"threadId":"th","turnId":"t1","turn":{"items":[],"status":"completed","error":null}}}`),
	})
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns))
	}
	// The read is recorded once, at the item/started record (seq 3).
	if want := []SkillUse{{Skill: "executing-plans", Seq: 3, Evidence: "read"}}; !slices.Equal(turns[0].Skills, want) {
		t.Errorf("skills = %+v, want %+v", turns[0].Skills, want)
	}
}

// A turn records the user messages it answered, which is how the monitor tells
// a pilot response to schmux from a reply to the user. Claude: the dispatch
// markers written before the turn. Codex: the client IDs of the turn's
// userMessage items.
func TestCollectorRecordsTheMessagesATurnAnswered(t *testing.T) {
	claude := newTurnCollector(chat.ProtocolClaude, "pilot")
	var claudeTurns []PlannerTurn
	for i, rec := range []chat.Record{
		{Type: chat.RecordUserMessage, ID: "typed"},
		{Type: chat.RecordUserMessageDispatch, ID: "typed"},
		{Type: chat.RecordHarness, Line: []byte(`{"type":"assistant","uuid":"a1","message":{"content":[{"type":"text","text":"Hello."}]}}`)},
		{Type: chat.RecordHarness, Line: []byte(`{"type":"result","subtype":"success","result":"Hello."}`)},
		{Type: chat.RecordUserMessageDispatch, ID: "notification"},
		{Type: chat.RecordHarness, Line: []byte(`{"type":"result","subtype":"success","result":"{\"guidance\":null}"}`)},
	} {
		rec.Seq = uint64(i + 1)
		turn, err := claude.consume(rec)
		if err != nil {
			t.Fatal(err)
		}
		if turn != nil {
			claudeTurns = append(claudeTurns, *turn)
		}
	}
	if len(claudeTurns) != 2 || !slices.Equal(claudeTurns[0].InputIDs, []string{"typed"}) || !slices.Equal(claudeTurns[1].InputIDs, []string{"notification"}) {
		t.Fatalf("claude turns = %+v; want inputs [typed] then [notification]", claudeTurns)
	}

	codex := collectTurns(t, chat.ProtocolCodex, "pilot", splitJSONL(t, []byte(`{"method":"turn/started","params":{"threadId":"root","turn":{"id":"one"}}}
{"method":"item/started","params":{"threadId":"root","turnId":"one","item":{"type":"userMessage","id":"u1","clientId":"typed"}}}
{"method":"item/completed","params":{"threadId":"root","turnId":"one","item":{"type":"userMessage","id":"u1","clientId":"typed"}}}
{"method":"item/completed","params":{"threadId":"root","turnId":"one","item":{"type":"agentMessage","id":"m1","phase":"final_answer","text":"Hello."}}}
{"method":"turn/completed","params":{"threadId":"root","turn":{"id":"one","status":"completed","items":[]}}}
{"method":"turn/started","params":{"threadId":"root","turn":{"id":"two"}}}
{"method":"turn/completed","params":{"threadId":"root","turn":{"id":"two","status":"completed","items":[]}}}`)))
	if len(codex) != 2 || !slices.Equal(codex[0].InputIDs, []string{"typed"}) || len(codex[1].InputIDs) != 0 {
		t.Fatalf("codex turns = %+v; want inputs [typed] then none", codex)
	}
}
