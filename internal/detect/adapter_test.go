package detect

import (
	"testing"
)

func TestGetAdapter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		wantNil bool
	}{
		{"claude", false},
		{"codex", false},
		{"gemini", false},
		{"opencode", false},
		{"unknown", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := GetAdapter(tt.name)
			if tt.wantNil && adapter != nil {
				t.Errorf("GetAdapter(%q) = %v, want nil", tt.name, adapter)
			}
			if !tt.wantNil && adapter == nil {
				t.Fatalf("GetAdapter(%q) = nil, want non-nil", tt.name)
			}
			if !tt.wantNil && adapter.Name() != tt.name {
				t.Errorf("GetAdapter(%q).Name() = %q", tt.name, adapter.Name())
			}
		})
	}
}

func TestAllAdaptersRegistered(t *testing.T) {
	t.Parallel()
	adapters := AllAdapters()
	if len(adapters) != 4 {
		t.Fatalf("AllAdapters() returned %d, want 4", len(adapters))
	}
	names := map[string]bool{}
	for _, a := range adapters {
		names[a.Name()] = true
	}
	for _, want := range []string{"claude", "codex", "gemini", "opencode"} {
		if !names[want] {
			t.Errorf("AllAdapters() missing %q", want)
		}
	}
}

func TestAdapterInteractiveArgs(t *testing.T) {
	t.Parallel()
	// Claude interactive with no model: no extra args
	a := GetAdapter("claude")
	args := a.InteractiveArgs(nil)
	if len(args) != 0 {
		t.Errorf("claude InteractiveArgs(nil) = %v, want empty", args)
	}

	// Claude interactive with model flag
	model := &Model{ModelFlag: "--model", ModelValue: "sonnet"}
	args = a.InteractiveArgs(model)
	assertSliceEqual(t, args, []string{"--model", "sonnet"})

	// Empty ModelValue should not produce --model ""
	emptyModel := &Model{ModelFlag: "--model", ModelValue: ""}
	for _, tool := range []string{"claude", "codex", "gemini", "opencode"} {
		a := GetAdapter(tool)
		args := a.InteractiveArgs(emptyModel)
		if len(args) != 0 {
			t.Errorf("%s InteractiveArgs with empty ModelValue = %v, want empty", tool, args)
		}
	}
}

func TestAdapterResumeArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool string
		want []string
	}{
		{"claude", []string{"--continue"}},
		{"codex", []string{"resume", "--last"}},
		{"gemini", []string{"-r", "latest"}},
		{"opencode", []string{"--continue"}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			a := GetAdapter(tt.tool)
			got := a.ResumeArgs()
			assertSliceEqual(t, got, tt.want)
		})
	}
}

func TestAdapterOneshotArgs(t *testing.T) {
	t.Parallel()
	// Claude oneshot
	a := GetAdapter("claude")
	args, err := a.OneshotArgs(nil, `{"type":"object"}`)
	if err != nil {
		t.Fatalf("claude OneshotArgs error: %v", err)
	}
	assertContains(t, args, "-p")
	assertContains(t, args, "--output-format")
	assertContains(t, args, "--json-schema")

	// Codex oneshot
	a = GetAdapter("codex")
	args, err = a.OneshotArgs(nil, `{"type":"object"}`)
	if err != nil {
		t.Fatalf("codex OneshotArgs error: %v", err)
	}
	assertContains(t, args, "exec")
	assertContains(t, args, "--json")

	// Gemini oneshot should error
	a = GetAdapter("gemini")
	_, err = a.OneshotArgs(nil, `{"type":"object"}`)
	if err == nil {
		t.Error("gemini OneshotArgs should return error")
	}

	// Opencode oneshot
	a = GetAdapter("opencode")
	args, err = a.OneshotArgs(nil, "")
	if err != nil {
		t.Fatalf("opencode OneshotArgs error: %v", err)
	}
	assertContains(t, args, "run")
}

func TestAdapterStreamingArgs(t *testing.T) {
	t.Parallel()
	// Claude streaming should work
	a := GetAdapter("claude")
	args, err := a.StreamingArgs(nil, "")
	if err != nil {
		t.Fatalf("claude StreamingArgs error: %v", err)
	}
	assertContains(t, args, "stream-json")

	// Codex streaming should error
	a = GetAdapter("codex")
	_, err = a.StreamingArgs(nil, "")
	if err == nil {
		t.Error("codex StreamingArgs should return error")
	}
}

func TestAdapterInstructionConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool     string
		wantDir  string
		wantFile string
	}{
		{"claude", ".claude", "CLAUDE.md"},
		{"codex", ".codex", "AGENTS.md"},
		{"gemini", ".gemini", "GEMINI.md"},
		{"opencode", ".opencode", "AGENTS.md"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			a := GetAdapter(tt.tool)
			cfg := a.InstructionConfig()
			if cfg.InstructionDir != tt.wantDir {
				t.Errorf("InstructionDir = %q, want %q", cfg.InstructionDir, tt.wantDir)
			}
			if cfg.InstructionFile != tt.wantFile {
				t.Errorf("InstructionFile = %q, want %q", cfg.InstructionFile, tt.wantFile)
			}
		})
	}
}

func TestAdapterSignalingStrategy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool string
		want SignalingStrategy
	}{
		{"claude", SignalingHooks},
		{"codex", SignalingCLIFlag},
		{"gemini", SignalingInstructionFile},
		{"opencode", SignalingInstructionFile},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			a := GetAdapter(tt.tool)
			got := a.SignalingStrategy()
			if got != tt.want {
				t.Errorf("SignalingStrategy() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test helpers

func assertSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func assertContains(t *testing.T, slice []string, want string) {
	t.Helper()
	for _, s := range slice {
		if s == want {
			return
		}
	}
	t.Errorf("slice %v does not contain %q", slice, want)
}
