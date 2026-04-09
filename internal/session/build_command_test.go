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
			got, err := buildCommand(tt.target, tt.prompt, tt.model, tt.resume, tt.remoteMode)
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
	got, err := buildCommand(target, "", nil, false, false)
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
	got, err := buildCommand(target, "do something", nil, false, false)
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
	got, err := buildCommand(target, "", model, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "-m 'codex-mini-latest'") {
		t.Errorf("should contain model flag: %q", got)
	}
}

func TestBuildCommand_ResumeMode(t *testing.T) {
	target := ResolvedTarget{
		Name:       "claude",
		Command:    "claude",
		Promptable: true,
	}
	got, err := buildCommand(target, "", nil, true, false)
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
		Promptable: true,
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "sk-test",
		},
	}
	got, err := buildCommand(target, "", nil, true, false)
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
