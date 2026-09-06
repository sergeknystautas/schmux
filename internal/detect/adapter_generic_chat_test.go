package detect

import (
	"reflect"
	"testing"
)

func chatTestAdapter(t *testing.T, yaml string) *GenericAdapter {
	t.Helper()
	d, err := ParseDescriptor([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseDescriptor: %v", err)
	}
	a, err := NewGenericAdapter(d)
	if err != nil {
		t.Fatalf("NewGenericAdapter: %v", err)
	}
	return a
}

const chatYAML = `
name: chatty
display_name: chatty
detect:
  - type: path_lookup
    command: chatty
capabilities: [interactive]
interactive:
  resume_id_args: ['--resume', '{resume_id}']
chat:
  protocol: claude-stream-json
  base_args: ['-p', '--input-format', 'stream-json']
  resume_id_args: ['--resume', '{resume_id}']
`

func TestChatArgs_NoChatMode(t *testing.T) {
	a := chatTestAdapter(t, `
name: plain
display_name: plain
detect:
  - type: path_lookup
    command: plain
`)
	if got := a.ChatArgs(nil, ""); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	for _, c := range a.Capabilities() {
		if c == "chat" {
			t.Fatal("capabilities must not include chat without a chat mode")
		}
	}
}

func TestChatArgs_BaseAndResume(t *testing.T) {
	a := chatTestAdapter(t, chatYAML)
	if got, want := a.ChatArgs(nil, ""), []string{"-p", "--input-format", "stream-json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("base: got %v want %v", got, want)
	}
	if got, want := a.ChatArgs(nil, "abc"), []string{"-p", "--input-format", "stream-json", "--resume", "abc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resume: got %v want %v", got, want)
	}
	caps := a.Capabilities()
	if !reflect.DeepEqual(caps, []string{"interactive", "chat"}) {
		t.Fatalf("capabilities: got %v", caps)
	}
}

func TestClaudeDescriptor_HasChatMode(t *testing.T) {
	a := GetAdapter("claude")
	if a == nil {
		t.Fatal("claude adapter missing")
	}
	args := a.ChatArgs(nil, "")
	want := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--include-partial-messages", "--replay-user-messages", "--permission-prompt-tool", "stdio"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("claude chat args: got %v", args)
	}
}
