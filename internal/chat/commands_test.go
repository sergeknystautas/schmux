package chat

import (
	"fmt"
	"os"
	"testing"
)

func TestCodex_Compact(t *testing.T) {
	for _, text := range []string{"/compact", " \n/compact\t"} {
		t.Run(text, func(t *testing.T) {
			p := newCodexProtocol()
			p.Observe([]byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`))
			p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
			rt, paths := newTestRuntime(t)
			rt.proto = p
			rec, err := rt.Send(text, nil)
			if err != nil {
				t.Fatal(err)
			}
			line, err := os.ReadFile(paths.Input)
			if err != nil {
				t.Fatal(err)
			}
			v := decode(t, line)
			params := v["params"].(map[string]any)
			if v["method"] != "thread/compact/start" || params["threadId"] != "t-1" || params["clientUserMessageId"] != rec.ID || params["input"] != nil {
				t.Fatalf("compaction request = %s", line)
			}
			ack := []byte(fmt.Sprintf(`{"id":%v,"result":{}}`, v["id"]))
			p.Observe(ack)
			if p.Addressable() {
				t.Fatal("acknowledgement released the queue before compaction started")
			}
			if next, err := p.UserMessage("next", "continue", nil); err != ErrNotAddressable || next != nil {
				t.Fatalf("follow-up during compaction: %s, %v", next, err)
			}

			output := `{"id":2,"result":{"account":{"type":"chatgpt"}}}` + "\n" +
				`{"id":3,"result":{"thread":{"id":"t-1"}}}` + "\n" + string(ack) + "\n"
			if err := os.WriteFile(paths.Output, []byte(output), 0o600); err != nil {
				t.Fatal(err)
			}
			rebuilt := newCodexProtocol()
			unsent, err := rebuilt.Rebuild(paths, []Record{rec})
			if err != nil || len(unsent) != 0 || rebuilt.Addressable() {
				t.Fatalf("rebuild: unsent=%+v err=%v addressable=%v", unsent, err, rebuilt.Addressable())
			}
			for _, protocol := range []*codexProtocol{p, rebuilt} {
				protocol.Observe([]byte(`{"method":"turn/started","params":{"threadId":"t-1","turn":{"id":"compact-turn"}}}`))
				if protocol.Addressable() {
					t.Fatal("compaction turn must hold the queue")
				}
				protocol.Observe([]byte(`{"method":"turn/completed","params":{"threadId":"t-1","turn":{"id":"compact-turn","status":"completed"}}}`))
				if !protocol.Addressable() {
					t.Fatal("compaction completion must release the queue")
				}
			}
		})
	}
}

func TestCodex_CompactErrorReleasesQueue(t *testing.T) {
	p := newCodexProtocol()
	p.Observe([]byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`))
	p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
	line, err := p.Command("compact-1", "/compact")
	if err != nil {
		t.Fatal(err)
	}
	p.CommitUserMessage("compact-1")
	p.Observe([]byte(fmt.Sprintf(`{"id":%v,"error":{"code":-32603,"message":"compaction failed"}}`, decode(t, line)["id"])))
	if !p.Addressable() {
		t.Fatal("failed compaction must release the queue")
	}
}

func TestCodex_CompactTextWithContentRemainsPrompt(t *testing.T) {
	for _, tc := range []struct {
		text   string
		images []Image
	}{
		{text: "explain /compact"},
		{text: "/compactness"},
		{text: "/compact", images: []Image{{MediaType: "image/png", Data: "AA=="}}},
	} {
		p := newCodexProtocol()
		p.Observe([]byte(`{"id":2,"result":{"account":{"type":"chatgpt"}}}`))
		p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t-1"}}}`))
		rt, paths := newTestRuntime(t)
		rt.proto = p
		_, err := rt.Send(tc.text, tc.images)
		if err != nil {
			t.Fatal(err)
		}
		line, err := os.ReadFile(paths.Input)
		if err != nil {
			t.Fatal(err)
		}
		if decode(t, line)["method"] != "turn/start" {
			t.Fatalf("message with content must remain a prompt: %s", line)
		}
	}
}

func TestClaude_CompactCommand(t *testing.T) {
	rt, paths := newTestRuntime(t)
	rec, err := rt.Send(" \n/compact\t", nil)
	if err != nil {
		t.Fatal(err)
	}
	line, err := os.ReadFile(paths.Input)
	if err != nil {
		t.Fatal(err)
	}
	v := decode(t, line)
	message := v["message"].(map[string]any)
	if v["type"] != "user" || message["content"] != "/compact" || rt.proto.Addressable() {
		t.Fatalf("command: %s, addressable=%v", line, rt.proto.Addressable())
	}
	records, err := rt.log.ReadAll()
	if err != nil || len(records) != 2 || records[0].ID != rec.ID || records[0].Text != " \n/compact\t" || records[1].Type != RecordUserMessageDispatch {
		t.Fatalf("command records: %+v, err=%v", records, err)
	}
}
