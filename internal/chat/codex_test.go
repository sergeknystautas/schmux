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
	if first["id"].(float64) != 4 {
		t.Fatalf("first client id after the handshake must be 4, got %v", first["id"])
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
	rt, err := NewRuntime("s1", proto, paths, "", nil, nil)
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
