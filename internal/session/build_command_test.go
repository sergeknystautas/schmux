package session

import (
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestBuildCommand_PromptableTarget(t *testing.T) {
	tests := []struct {
		name       string
		target     ResolvedTarget
		prompt     string
		model      *detect.Model
		resume     bool
		remoteMode bool
		fence      bool
		wantErr    string
		wantSub    []string // substrings that must appear
		wantNot    []string // substrings that must NOT appear
	}{
		{
			name: "promptable with prompt",
			target: ResolvedTarget{
				Name:       "codex",
				Command:    "codex",
				Promptable: true,
			},
			prompt:  "fix the bug",
			wantSub: []string{"codex", "'fix the bug'"},
		},
		{
			name: "promptable with empty prompt runs without prompt arg",
			target: ResolvedTarget{
				Name:       "codex",
				Command:    "codex",
				Promptable: true,
			},
			prompt:  "",
			wantSub: []string{"codex"},
		},
		{
			name: "promptable with whitespace-only prompt runs without prompt arg",
			target: ResolvedTarget{
				Name:       "codex",
				Command:    "codex",
				Promptable: true,
			},
			prompt:  "   ",
			wantSub: []string{"codex"},
		},
		{
			name: "command target with prompt fails",
			target: ResolvedTarget{
				Name:       "custom-cmd",
				Command:    "my-tool run",
				Promptable: false,
			},
			prompt:  "should not be here",
			wantErr: "prompt is not allowed",
		},
		{
			name: "command target without prompt succeeds",
			target: ResolvedTarget{
				Name:       "custom-cmd",
				Command:    "my-tool run",
				Promptable: false,
			},
			prompt:  "",
			wantSub: []string{"my-tool run"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCommand(tt.target, tt.prompt, tt.model, tt.resume, tt.remoteMode, tt.fence)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("command %q missing substring %q", got, sub)
				}
			}
			for _, sub := range tt.wantNot {
				if strings.Contains(got, sub) {
					t.Errorf("command %q should not contain %q", got, sub)
				}
			}
		})
	}
}

func TestBuildCommand_EnvPrefix(t *testing.T) {
	target := ResolvedTarget{
		Name:       "custom",
		Command:    "my-tool",
		Promptable: false,
		Env: map[string]string{
			"API_KEY":   "secret123",
			"API_MODEL": "gpt-4",
		},
	}
	got, err := buildCommand(target, "", nil, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Env vars should be sorted and prefixed
	if !strings.Contains(got, "API_KEY='secret123'") {
		t.Errorf("missing API_KEY env var in %q", got)
	}
	if !strings.Contains(got, "API_MODEL='gpt-4'") {
		t.Errorf("missing API_MODEL env var in %q", got)
	}
	if !strings.HasSuffix(got, "my-tool") {
		t.Errorf("command should end with 'my-tool': %q", got)
	}
	// Env vars should be sorted: API_KEY before API_MODEL
	keyIdx := strings.Index(got, "API_KEY")
	modelIdx := strings.Index(got, "API_MODEL")
	if keyIdx > modelIdx {
		t.Errorf("env vars should be sorted: API_KEY before API_MODEL in %q", got)
	}
}

func TestBuildCommand_EnvPrefixWithPromptable(t *testing.T) {
	target := ResolvedTarget{
		Name:       "custom",
		Command:    "my-tool",
		Promptable: true,
		Env: map[string]string{
			"TOKEN": "abc",
		},
	}
	got, err := buildCommand(target, "do something", nil, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "TOKEN='abc'") {
		t.Errorf("should start with env prefix: %q", got)
	}
	if !strings.Contains(got, "'do something'") {
		t.Errorf("should contain quoted prompt: %q", got)
	}
}

func TestBuildCommand_ModelFlagInjection(t *testing.T) {
	target := ResolvedTarget{
		Name:       "codex",
		Command:    "codex",
		ToolName:   "codex",
		Promptable: false,
	}
	model := &detect.Model{
		ID: "codex-mini",
		Runners: map[string]detect.RunnerSpec{
			"codex": {ModelValue: "codex-mini-latest"},
		},
	}
	got, err := buildCommand(target, "", model, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "-m 'codex-mini-latest'") {
		t.Errorf("should contain model flag: %q", got)
	}
}

// TestBuildCommand_Antigravity asserts the exact spawn strings agy must receive.
// -i is the prompt flag (not command_args), so blank launches bare `agy`, and
// --model is injected before -i for discovered models. Resume uses -c.
func TestBuildCommand_Antigravity(t *testing.T) {
	agTarget := ResolvedTarget{
		Name:       "antigravity",
		Command:    "agy",
		ToolName:   "antigravity",
		Promptable: true,
	}
	opusModel := &detect.Model{
		ID: "antigravity-claude-opus-4-6-thinking",
		Runners: map[string]detect.RunnerSpec{
			"antigravity": {ModelValue: "Claude Opus 4.6 (Thinking)"},
		},
	}

	tests := []struct {
		name   string
		prompt string
		model  *detect.Model
		resume bool
		want   string
	}{
		{"default with prompt", "fix the bug", nil, false, "agy -i 'fix the bug'"},
		{"discovered model with prompt", "fix the bug", opusModel, false, "agy --model 'Claude Opus 4.6 (Thinking)' -i 'fix the bug'"},
		{"resume default", "", nil, true, "agy -c"},
		// Resume carries the session's model; the value must stay shell-quoted so
		// the spaces/parens don't break the command.
		{"resume with model", "", opusModel, true, "agy -c --model 'Claude Opus 4.6 (Thinking)'"},
		{"blank interactive", "", nil, false, "agy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCommand(agTarget, tt.prompt, tt.model, tt.resume, false, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("buildCommand =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestBuildCommand_ResumeMode(t *testing.T) {
	target := ResolvedTarget{
		Name:       "claude",
		Command:    "claude",
		ToolName:   "claude",
		Promptable: true,
	}
	got, err := buildCommand(target, "", nil, true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "--continue") {
		t.Errorf("resume mode for claude should use --continue: %q", got)
	}
}

func TestBuildCommand_ResumeModeWithEnv(t *testing.T) {
	target := ResolvedTarget{
		Name:       "claude",
		Command:    "claude",
		ToolName:   "claude",
		Promptable: true,
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "sk-test",
		},
	}
	got, err := buildCommand(target, "", nil, true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "--continue") {
		t.Errorf("resume mode should use --continue: %q", got)
	}
	if !strings.Contains(got, "ANTHROPIC_API_KEY='sk-test'") {
		t.Errorf("resume mode should preserve env vars: %q", got)
	}
}

func TestBuildCommandFenceAppendsAutoApproveInteractive(t *testing.T) {
	target := ResolvedTarget{Name: "claude", ToolName: "claude", Command: "claude", Promptable: true}
	got, err := buildCommand(target, "do something", nil, false, false, true)
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}
	if !strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("fenced interactive command = %q, want it to contain --dangerously-skip-permissions", got)
	}
}

func TestBuildCommandFenceOffOmitsAutoApprove(t *testing.T) {
	target := ResolvedTarget{Name: "claude", ToolName: "claude", Command: "claude", Promptable: true}
	got, err := buildCommand(target, "do something", nil, false, false, false)
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}
	if strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("unfenced command = %q, should not contain --dangerously-skip-permissions", got)
	}
}

func TestBuildCommandFenceAppendsAutoApproveResume(t *testing.T) {
	target := ResolvedTarget{Name: "claude", ToolName: "claude", Command: "claude", Promptable: true}
	got, err := buildCommand(target, "", nil, true, false, true)
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}
	if !strings.Contains(got, "--continue") || !strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("fenced resume command = %q, want both --continue and --dangerously-skip-permissions", got)
	}
}

func TestBuildCommandFenceNoAutoApproveForUserTarget(t *testing.T) {
	// User-defined run target: ToolName is empty, so its name must not be used
	// to infer a harness or append harness-specific flags.
	target := ResolvedTarget{Name: "claude", Kind: TargetKindUser, Command: "my-custom-claude-wrapper"}
	got, err := buildCommand(target, "", nil, false, false, true)
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}
	if got != "my-custom-claude-wrapper" {
		t.Errorf("fenced user target = %q, want command unchanged", got)
	}
	if strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("fenced user target = %q, should not append auto_approve_args", got)
	}
}

func TestBuildCommandResumeRequiresToolName(t *testing.T) {
	target := ResolvedTarget{Name: "claude", Kind: TargetKindUser, Command: "my-custom-claude-wrapper"}
	_, err := buildCommand(target, "", nil, true, false, true)
	if err == nil || !strings.Contains(err.Error(), "resume requires a descriptor-backed target") {
		t.Fatalf("buildCommand resume err = %v, want descriptor-backed target error", err)
	}
}

func TestAppendSignalingFlags(t *testing.T) {
	tests := []struct {
		name       string
		cmd        string
		baseTool   string
		isRemote   bool
		wantSub    string // substring that should appear
		wantPrefix string // command should start with this
	}{
		{
			name:       "claude uses hooks, no flag added",
			cmd:        "claude --continue",
			baseTool:   "claude",
			isRemote:   false,
			wantPrefix: "claude --continue",
		},
		{
			name:     "codex local gets -c flag",
			cmd:      "codex",
			baseTool: "codex",
			isRemote: false,
			wantSub:  "-c",
		},
		{
			name:       "codex remote gets no flag (no remote mechanism)",
			cmd:        "codex",
			baseTool:   "codex",
			isRemote:   true,
			wantPrefix: "codex",
		},
		{
			name:       "unknown tool no flags",
			cmd:        "my-tool",
			baseTool:   "my-tool",
			isRemote:   false,
			wantPrefix: "my-tool",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendSignalingFlags(tt.cmd, tt.baseTool, tt.isRemote)
			if tt.wantPrefix != "" && !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("appendSignalingFlags() = %q, want prefix %q", got, tt.wantPrefix)
			}
			if tt.wantSub != "" && !strings.Contains(got, tt.wantSub) {
				t.Errorf("appendSignalingFlags() = %q, missing %q", got, tt.wantSub)
			}
		})
	}
}
