package session

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestCodexPersonaCommandDelivery(t *testing.T) {
	target := ResolvedTarget{Name: "codex", Command: "codex", ToolName: "codex", Promptable: true}
	prompt := "## Persona: Reviewer\nDon't edit. Keep \"quotes\", \\paths, $HOME, $(exit 91), `exit 92`, and 🧪.\n\n## Communication Style\nBe concise."
	for _, mode := range []string{"terminal", "resume", "chat", "remote"} {
		t.Run(mode, func(t *testing.T) {
			var command string
			var err error
			if mode == "chat" {
				command, _, _, err = buildChatCommand(target, nil, false, false, "", "/ws")
			} else {
				command, err = buildCommand(target, "Review", nil, mode == "resume", mode == "remote", false, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			command = appendPersonaFlags(command, "codex", "/workspace with spaces/persona.md", prompt)
			if strings.ContainsAny(command, "\n\r") {
				t.Fatalf("command breaks remote control mode: %q", command)
			}
			// Execute the generated shell command against an argv recorder. The
			// prompt must reach Codex as data, without shell expansion or execution.
			out, err := exec.Command("sh", "-c", "codex() { printf '%s\\n' \"$@\"; }; "+command).CombinedOutput()
			if err != nil {
				t.Fatalf("shell: %v\n%s", err, out)
			}
			args := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
			const prefix = "developer_instructions="
			if len(args) < 2 || args[len(args)-2] != "-c" || !strings.HasPrefix(args[len(args)-1], prefix) {
				t.Fatalf("persona override missing from argv: %q", args)
			}
			var got string
			if err := json.Unmarshal([]byte(strings.TrimPrefix(args[len(args)-1], prefix)), &got); err != nil {
				t.Fatalf("invalid encoded instructions %q: %v", args, err)
			}
			if got != prompt {
				t.Fatalf("instructions = %q, want %q", got, prompt)
			}
		})
	}
}
