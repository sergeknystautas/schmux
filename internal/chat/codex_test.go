package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func codexFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "codex", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(line, &v); err != nil {
		t.Fatalf("%s: %v", line, err)
	}
	return v
}

func TestCodex_LaunchHandshake(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	argv, hs := p.Launch(LaunchOpts{Adapter: detect.GetAdapter("codex"), ModelValue: "gpt-5", Fenced: true, Cwd: "/ws"})
	if len(argv) != 0 {
		t.Fatalf("codex argv must be empty (auto-approve is a thread parameter): %v", argv)
	}
	if len(hs) != 4 {
		t.Fatalf("handshake lines: %d", len(hs))
	}
	init := decode(t, hs[0])
	if init["id"].(float64) != 1 || init["method"] != "initialize" {
		t.Fatalf("line 1: %s", hs[0])
	}
	if decode(t, hs[1])["method"] != "initialized" {
		t.Fatalf("line 2: %s", hs[1])
	}
	acct := decode(t, hs[2])
	if acct["id"].(float64) != 2 || acct["method"] != "account/read" {
		t.Fatalf("line 3: %s", hs[2])
	}
	start := decode(t, hs[3])
	params := start["params"].(map[string]any)
	if start["id"].(float64) != 3 || start["method"] != "thread/start" || params["cwd"] != "/ws" ||
		params["approvalPolicy"] != "never" || params["sandbox"] != "danger-full-access" || params["model"] != "gpt-5" {
		t.Fatalf("line 4: %s", hs[3])
	}

	_, hs = p.Launch(LaunchOpts{Cwd: "/ws", ResumeID: "thread-9"})
	resume := decode(t, hs[3])
	params = resume["params"].(map[string]any)
	if resume["method"] != "thread/resume" || params["threadId"] != "thread-9" || params["excludeTurns"] != true ||
		params["approvalPolicy"] != "on-request" || params["sandbox"] != "workspace-write" {
		t.Fatalf("resume: %s", hs[3])
	}
	if _, has := params["model"]; has {
		t.Fatal("no model value must mean no model key")
	}
}

// Resume without an id (the wizard's resume mode): the handshake asks for the
// workspace's newest thread, and the thread request follows the answer.
func TestCodex_ResumeMostRecent(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	_, hs := p.Launch(LaunchOpts{Cwd: "/ws", Resume: true, ModelValue: "gpt-5", Fenced: true})
	if len(hs) != 4 {
		t.Fatalf("handshake lines: %d", len(hs))
	}
	list := decode(t, hs[3])
	params := list["params"].(map[string]any)
	if list["id"].(float64) != 4 || list["method"] != "thread/list" || params["cwd"] != "/ws" || params["limit"].(float64) != 1 ||
		params["sortKey"] != "updatedAt" {
		t.Fatalf("list line: %s", hs[3])
	}
	kinds := params["sourceKinds"].([]any)
	if len(kinds) != 3 || kinds[0] != "cli" || kinds[1] != "vscode" || kinds[2] != "appServer" {
		t.Fatalf("sourceKinds: %v", kinds)
	}
	if id := p.ResumeID(hs[3]); id != "" {
		t.Fatalf("the list request carries no thread id: %q", id)
	}

	// Newest thread found: the follow-up resumes it, with the launch params.
	follow := p.Observe([]byte(`{"id":4,"result":{"data":[{"id":"t-new","cwd":"/ws"}],"nextCursor":null}}`))
	if len(follow) != 1 {
		t.Fatalf("follow-up lines: %d", len(follow))
	}
	resume := decode(t, follow[0])
	params = resume["params"].(map[string]any)
	if resume["id"].(float64) != 3 || resume["method"] != "thread/resume" || params["threadId"] != "t-new" || params["excludeTurns"] != true ||
		params["model"] != "gpt-5" || params["approvalPolicy"] != "never" || params["sandbox"] != "danger-full-access" {
		t.Fatalf("follow-up: %s", follow[0])
	}
	// Answered once: a replay of the same response (Rebuild) writes nothing.
	if again := p.Observe([]byte(`{"id":4,"result":{"data":[{"id":"t-new"}]}}`)); again != nil {
		t.Fatalf("second list response must not re-issue the thread request: %s", again[0])
	}
	// The id-3 response then makes the thread known, exactly as for a plain launch.
	p.Observe([]byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`))
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-new"}}}`))
	if !p.Addressable() {
		t.Fatal("resumed thread must be addressable")
	}
	line, _ := p.UserMessage("u1", "hi", nil)
	if decode(t, line)["id"].(float64) != 5 {
		t.Fatalf("client ids continue after the list id: %s", line)
	}

	// No thread in the workspace: the follow-up starts a fresh one.
	p, _ = ProtocolFor(ProtocolCodex)
	p.Launch(LaunchOpts{Cwd: "/ws", Resume: true})
	follow = p.Observe([]byte(`{"id":4,"result":{"data":[],"nextCursor":null}}`))
	if len(follow) != 1 {
		t.Fatalf("follow-up lines: %d", len(follow))
	}
	start := decode(t, follow[0])
	params = start["params"].(map[string]any)
	if start["id"].(float64) != 3 || start["method"] != "thread/start" || params["cwd"] != "/ws" || params["approvalPolicy"] != "on-request" {
		t.Fatalf("empty list follow-up: %s", follow[0])
	}
	if _, has := params["threadId"]; has {
		t.Fatal("a fresh thread/start carries no threadId")
	}

	// An id wins over the flag, and a plain launch never answers a list response.
	p, _ = ProtocolFor(ProtocolCodex)
	_, hs = p.Launch(LaunchOpts{Cwd: "/ws", Resume: true, ResumeID: "t-9"})
	if decode(t, hs[3])["method"] != "thread/resume" {
		t.Fatalf("resume with id: %s", hs[3])
	}
	if follow := p.Observe([]byte(`{"id":4,"result":{"data":[{"id":"t-new"}]}}`)); follow != nil {
		t.Fatalf("no pending thread must mean no follow-up: %s", follow[0])
	}
}

func TestCodex_LiveOnly(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	live := []string{
		`{"method":"item/agentMessage/delta","params":{}}`,
		`{"method":"item/reasoning/textDelta"}`, `{"method":"item/reasoning/summaryTextDelta"}`,
		`{"method":"item/reasoning/summaryPartAdded"}`, `{"method":"item/commandExecution/outputDelta"}`,
		`{"method":"item/fileChange/outputDelta"}`, `{"method":"item/fileChange/patchUpdated"}`,
		`{"method":"thread/tokenUsage/updated"}`, `{"method":"account/rateLimits/updated"}`,
	}
	for _, l := range live {
		if !p.LiveOnly([]byte(l)) {
			t.Fatalf("should be live-only: %s", l)
		}
	}
	for _, l := range []string{`{"id":3,"result":{}}`, `{"method":"item/completed"}`, `{"method":"hook/started"}`, `{"method":"thread/status/changed"}`, `{"method":"item/commandExecution/requestApproval","id":0}`} {
		if p.LiveOnly([]byte(l)) {
			t.Fatalf("should be recorded: %s", l)
		}
	}
}

func TestCodex_ResumeID(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	if got := p.ResumeID([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`)); got != "t-1" {
		t.Fatalf("response: %q", got)
	}
	if got := p.ResumeID([]byte(`{"method":"thread/started","params":{"thread":{"id":"t-2"}}}`)); got != "t-2" {
		t.Fatalf("notification: %q", got)
	}
	if got := p.ResumeID([]byte(`{"id":4,"result":{"turn":{"id":"x"}}}`)); got != "" {
		t.Fatalf("turn response: %q", got)
	}
}

func TestCodex_ObserveMakesAddressable(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	if p.Addressable() {
		t.Fatal("fresh protocol must not be addressable")
	}
	line, err := p.UserMessage("u1", "hi", nil)
	if err != ErrNotAddressable || line != nil {
		t.Fatalf("not addressable must return no line: %v %s", err, line)
	}
	p.Observe([]byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`))
	if p.Addressable() {
		t.Fatal("account alone is not enough")
	}
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
	if !p.Addressable() {
		t.Fatal("thread id plus account must be addressable")
	}
	line, err = p.UserMessage("u1", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := decode(t, line)
	if first["id"].(float64) != 5 {
		t.Fatalf("first client id after the handshake (ids 1-4) must be 5, got %v", first["id"])
	}
	line, err = p.UserMessage("u2", "again", []Image{{MediaType: "image/png", Data: "AA=="}})
	if err != nil {
		t.Fatal(err)
	}
	v := decode(t, line)
	params := v["params"].(map[string]any)
	if v["method"] != "turn/start" || params["threadId"] != "t-1" || params["clientUserMessageId"] != "u2" {
		t.Fatalf("turn/start: %s", line)
	}
	if v["id"].(float64) != first["id"].(float64)+1 {
		t.Fatalf("ids must advance: %v then %v", first["id"], v["id"])
	}
	inputs := params["input"].([]any)
	if len(inputs) != 2 || inputs[1].(map[string]any)["url"] != "data:image/png;base64,AA==" {
		t.Fatalf("inputs: %v", inputs)
	}
}

func TestCodex_TurnStartAsksForReasoningSummaries(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	p.Observe([]byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`))
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
	line, err := p.UserMessage("u1", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	params := decode(t, line)["params"].(map[string]any)
	if params["summary"] != "auto" {
		t.Fatalf("turn/start must ask for reasoning summaries: %s", line)
	}
}

func TestCodex_AbortIsAJSONRPCError(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	line, err := p.Abort("7")
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != `{"id":7,"error":{"code":-32601,"message":"schmux: unsupported server request"}}` {
		t.Fatalf("abort: %s", line)
	}
	if _, err := p.Abort("x"); err == nil {
		t.Fatal("non-numeric id must error")
	}
}

func TestCodex_LoggedOutNeverAddressable(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	p.Observe([]byte(`{"id":2,"result":{"account":null,"requiresOpenaiAuth":true}}`))
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
	if p.Addressable() {
		t.Fatal("logged out must not be addressable")
	}
}

func TestCodex_InterruptNeedsActiveTurn(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
	if _, err := p.Interrupt(); err == nil {
		t.Fatal("no active turn must error")
	}
	p.Observe([]byte(`{"method":"turn/started","params":{"turn":{"id":"turn-1"}}}`))
	line, err := p.Interrupt()
	if err != nil {
		t.Fatal(err)
	}
	params := decode(t, line)["params"].(map[string]any)
	if params["threadId"] != "t-1" || params["turnId"] != "turn-1" {
		t.Fatalf("interrupt: %s", line)
	}
	p.Observe([]byte(`{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"interrupted"}}}`))
	if _, err := p.Interrupt(); err == nil {
		t.Fatal("completed turn is no longer active")
	}
}

// A live 0.153.4 app-server stream includes the child's turn events on the
// parent's connection. Stop must still address the parent's active turn.
func TestCodex_ChildTurnDoesNotReplaceInterruptTarget(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"parent"}}}`))
	p.Observe([]byte(`{"method":"turn/started","params":{"threadId":"parent","turn":{"id":"parent-turn"}}}`))
	for _, line := range []string{
		`{"method":"turn/started","params":{"threadId":"child","turn":{"id":"child-turn"}}}`,
		`{"method":"turn/completed","params":{"threadId":"child","turn":{"id":"child-turn","status":"completed"}}}`,
	} {
		p.Observe([]byte(line))
		interrupt, err := p.Interrupt()
		if err != nil {
			t.Fatal(err)
		}
		params := decode(t, interrupt)["params"].(map[string]any)
		if params["threadId"] != "parent" || params["turnId"] != "parent-turn" {
			t.Fatalf("child event changed interrupt target: %s", interrupt)
		}
	}
}

func TestCodex_PermissionAndAnswer(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	line, _ := p.Permission("0", true, json.RawMessage(`{"ignored":1}`), "ignored")
	if string(line) != `{"id":0,"result":{"decision":"accept"}}` {
		t.Fatalf("accept: %s", line)
	}
	line, _ = p.Permission("7", false, nil, "")
	if string(line) != `{"id":7,"result":{"decision":"decline"}}` {
		t.Fatalf("decline: %s", line)
	}
	if _, err := p.Permission("not-a-number", true, nil, ""); err == nil {
		t.Fatal("non-numeric id must error")
	}
	line, _ = p.Answer("0", map[string][]string{"fruit": {"Banana"}}, nil)
	if string(line) != `{"id":0,"result":{"answers":{"fruit":{"answers":["Banana"]}}}}` {
		t.Fatalf("answer: %s", line)
	}
}

func TestCodex_RebuildFromBridgeCapture(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	paths.Ensure()
	os.WriteFile(paths.Input, codexFixture(t, "bridge-in.jsonl"), 0o644)
	os.WriteFile(paths.Output, codexFixture(t, "bridge-out.jsonl"), 0o644)
	p := newCodexProtocol()
	unsent, err := p.Rebuild(paths, nil)
	if err != nil || unsent != nil {
		t.Fatalf("rebuild: %v %v", unsent, err)
	}
	// The bridge capture's id=3 result carries a turn, not a thread; the
	// separate thread/started notification carries the thread id. The protocol
	// extracts both: the thread id and the active-turn state.
	if p.threadID != "01a0706e-47e0-7243-ade7-d2eb280bd202" {
		t.Fatalf("threadID %q", p.threadID)
	}
	if p.activeTurn != "" {
		t.Fatalf("all four turns completed; activeTurn %q", p.activeTurn)
	}
	if p.nextID != 8 {
		t.Fatalf("nextID %d (max method id in in.jsonl is 7)", p.nextID)
	}
	// The bridge run never sent account/read; without it the protocol stays
	// unaddressable.
	if p.Addressable() {
		t.Fatal("without an account/read response the protocol stays unaddressable")
	}
}

func TestCodex_RebuildDerivesUnsent(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	paths.Ensure()
	os.WriteFile(paths.Output, []byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`+"\n"+`{"id":3,"result":{"thread":{"id":"t-1"}}}`+"\n"), 0o644)
	os.WriteFile(paths.Input, []byte(`{"id":1,"method":"initialize"}`+"\n"+`{"id":4,"method":"turn/start","params":{"threadId":"t-1","clientUserMessageId":"sent-1","input":[]}}`+"\n"), 0o644)
	recs := []Record{
		{Type: RecordUserMessage, ID: "old-1", Text: "before restart seed"},
		NewSessionEnded(),
		{Type: RecordUserMessage, ID: "sent-1", Text: "sent"},
		{Type: RecordUserMessage, ID: "held-1", Text: "held"},
	}
	p := newCodexProtocol()
	unsent, err := p.Rebuild(paths, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 1 || unsent[0].ID != "held-1" {
		t.Fatalf("unsent: %+v", unsent)
	}
	if !p.Addressable() || p.nextID != 5 {
		t.Fatalf("addressable %v nextID %d", p.Addressable(), p.nextID)
	}
}

func TestRuntime_CodexHoldsUntilThreadResponse(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	paths.Ensure()
	proto, _ := ProtocolFor(ProtocolCodex)
	rt, err := NewRuntime("s1", proto, paths, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.Start()
	if _, err := rt.Send("first", nil); err != nil {
		t.Fatal(err)
	}
	if in, _ := os.ReadFile(paths.Input); len(in) != 0 {
		t.Fatalf("held message must not be written yet: %s", in)
	}
	f, _ := os.OpenFile(paths.Output, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"id":2,"result":{"account":{"type":"chatgpt"}}}` + "\n")
	f.WriteString(`{"id":3,"result":{"thread":{"id":"t-1"}}}` + "\n")
	f.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		in, _ := os.ReadFile(paths.Input)
		if strings.Contains(string(in), `"text":"first"`) && strings.Contains(string(in), `"threadId":"t-1"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("held message never flushed: %s", in)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCodex_UserMessageImagePathSuffix(t *testing.T) {
	p, _ := ProtocolFor(ProtocolCodex)
	p.Observe([]byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`))
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
	line, err := p.UserMessage("u1", "look", []Image{{MediaType: "image/png", Data: "AA==", Path: "/tmp/schmux-chat-ab12cd34.png"}})
	if err != nil {
		t.Fatal(err)
	}
	v := decode(t, line)
	inputs := v["params"].(map[string]any)["input"].([]any)
	text := inputs[0].(map[string]any)["text"].(string)
	if !strings.HasSuffix(text, "\n\nImage attachments:\nImage #1: /tmp/schmux-chat-ab12cd34.png") {
		t.Fatalf("text input lacks path suffix: %q", text)
	}
	if inputs[1].(map[string]any)["url"] != "data:image/png;base64,AA==" {
		t.Fatalf("data URL input must stay: %v", inputs)
	}
}
