package detect

import (
	"reflect"
	"strings"
	"testing"
)

func TestChatProtocol_RequiredUnderChat(t *testing.T) {
	_, err := ParseDescriptor([]byte(`
name: nop
display_name: nop
detect:
  - type: path_lookup
    command: nop
chat:
  base_args: ['-p']
`))
	if err == nil || !strings.Contains(err.Error(), "chat.protocol") {
		t.Fatalf("expected chat.protocol error, got %v", err)
	}
	_, err = ParseDescriptor([]byte(`
name: nop
display_name: nop
detect:
  - type: path_lookup
    command: nop
chat:
  protocol: telnet
  base_args: ['-p']
`))
	if err == nil || !strings.Contains(err.Error(), "chat.protocol") {
		t.Fatalf("expected unknown protocol error, got %v", err)
	}
}

func TestChatProtocol_Values(t *testing.T) {
	a := chatTestAdapter(t, `
name: plain
display_name: plain
detect:
  - type: path_lookup
    command: plain
`)
	if got := a.ChatProtocol(); got != "" {
		t.Fatalf("no chat mode: got %q", got)
	}
	if got := GetAdapter("claude").ChatProtocol(); got != "claude-stream-json" {
		t.Fatalf("claude: got %q", got)
	}
}

func TestCodexDescriptor_HasChatMode(t *testing.T) {
	a := GetAdapter("codex")
	if a == nil {
		t.Fatal("codex adapter missing")
	}
	want := []string{"app-server", "--stdio", "-c", "features.default_mode_request_user_input=true"}
	if got := a.ChatArgs(nil, "thread-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("codex chat args must be base args only (resume is a request parameter): %v", got)
	}
	if a.ChatProtocol() != "codex-app-server" {
		t.Fatalf("protocol %q", a.ChatProtocol())
	}
	caps := a.Capabilities()
	n := 0
	for _, c := range caps {
		if c == "chat" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("capabilities must list chat exactly once: %v", caps)
	}
}
