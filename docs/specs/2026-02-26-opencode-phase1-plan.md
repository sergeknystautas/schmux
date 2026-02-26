# OpenCode Phase 1: ToolAdapter Refactor + Detection + Basic Spawn

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add opencode as the 4th tool in schmux, refactoring the tool system from switch-statement sprawl into a clean adapter pattern.

**Architecture:** Introduce a `ToolAdapter` interface that each tool implements. Move detection, command building, and signaling config out of scattered switch statements and into per-tool adapter files. The existing `ToolDetector` interface and `BuildCommandParts` function are replaced by the unified adapter.

**Tech Stack:** Go (backend), existing `internal/detect` package

---

### Task 1: Define the ToolAdapter interface

**Files:**

- Create: `internal/detect/adapter.go`
- Test: `internal/detect/adapter_test.go`

**Step 1: Write the failing test**

Create `internal/detect/adapter_test.go`:

```go
package detect

import (
	"context"
	"testing"
)

func TestGetAdapter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		wantNil  bool
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
	args, err := a.StreamingArgs(nil)
	if err != nil {
		t.Fatalf("claude StreamingArgs error: %v", err)
	}
	assertContains(t, args, "stream-json")

	// Codex streaming should error
	a = GetAdapter("codex")
	_, err = a.StreamingArgs(nil)
	if err == nil {
		t.Error("codex StreamingArgs should return error")
	}
}

func TestAdapterInstructionConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool    string
		wantDir string
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/detect/ -run TestGetAdapter -v`
Expected: FAIL — `GetAdapter` undefined

**Step 3: Write the ToolAdapter interface and registry**

Create `internal/detect/adapter.go`:

```go
package detect

import "context"

// SignalingStrategy defines how a tool receives schmux signaling instructions.
type SignalingStrategy int

const (
	// SignalingHooks means the tool uses lifecycle hooks (e.g., Claude's settings.local.json).
	SignalingHooks SignalingStrategy = iota
	// SignalingCLIFlag means signaling is injected via a CLI flag pointing to a file.
	SignalingCLIFlag
	// SignalingInstructionFile means signaling is appended to the tool's instruction file.
	SignalingInstructionFile
)

// ToolAdapter defines how a tool is detected, invoked, and configured.
// Each built-in tool (claude, codex, gemini, opencode) implements this interface.
type ToolAdapter interface {
	// Name returns the canonical tool name (e.g., "claude", "opencode").
	Name() string

	// Detect attempts to find the tool on the system.
	// Returns (tool, true) if found, (Tool{}, false) otherwise.
	Detect(ctx context.Context) (Tool, bool)

	// InteractiveArgs returns extra CLI args for interactive (TUI) mode.
	// The model parameter is optional.
	InteractiveArgs(model *Model) []string

	// OneshotArgs returns extra CLI args for non-interactive oneshot mode.
	// jsonSchema is the inline schema string (may be empty).
	OneshotArgs(model *Model, jsonSchema string) ([]string, error)

	// StreamingArgs returns extra CLI args for streaming oneshot mode.
	StreamingArgs(model *Model) ([]string, error)

	// ResumeArgs returns extra CLI args for resuming the last session.
	ResumeArgs() []string

	// InstructionConfig returns the instruction file location for this tool.
	InstructionConfig() AgentInstructionConfig

	// SignalingStrategy returns how this tool receives schmux signaling.
	SignalingStrategy() SignalingStrategy

	// SignalingArgs returns CLI args for injecting the signaling instructions file.
	// Only meaningful when SignalingStrategy() == SignalingCLIFlag.
	SignalingArgs(filePath string) []string
}

// adapters is the registry of all built-in tool adapters.
var adapters = map[string]ToolAdapter{}

// registerAdapter adds a tool adapter to the registry.
// Called from init() in each adapter_*.go file.
func registerAdapter(a ToolAdapter) {
	adapters[a.Name()] = a
}

// GetAdapter returns the adapter for the named tool, or nil if not found.
func GetAdapter(name string) ToolAdapter {
	return adapters[name]
}

// AllAdapters returns all registered adapters.
func AllAdapters() []ToolAdapter {
	out := make([]ToolAdapter, 0, len(adapters))
	for _, a := range adapters {
		out = append(out, a)
	}
	return out
}
```

**Step 4: Run test to verify it still fails**

Run: `go test ./internal/detect/ -run TestGetAdapter -v`
Expected: FAIL — no adapters registered yet (all return nil)

**Step 5: Commit interface definition**

```
feat(detect): add ToolAdapter interface and registry
```

---

### Task 2: Implement ClaudeAdapter

**Files:**

- Create: `internal/detect/adapter_claude.go`

**Step 1: Write the ClaudeAdapter**

Create `internal/detect/adapter_claude.go`:

```go
package detect

import "context"

// ClaudeAdapter implements ToolAdapter for Claude Code.
type ClaudeAdapter struct{}

func init() { registerAdapter(&ClaudeAdapter{}) }

func (a *ClaudeAdapter) Name() string { return "claude" }

func (a *ClaudeAdapter) Detect(ctx context.Context) (Tool, bool) {
	return (&claudeDetector{}).Detect(ctx)
}

func (a *ClaudeAdapter) InteractiveArgs(model *Model) []string {
	if model != nil && model.ModelFlag != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *ClaudeAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"-p", "--dangerously-skip-permissions", "--output-format", "json"}
	if jsonSchema != "" {
		args = append(args, "--json-schema", jsonSchema)
	}
	return args, nil
}

func (a *ClaudeAdapter) StreamingArgs(model *Model) ([]string, error) {
	return []string{"-p", "--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose"}, nil
}

func (a *ClaudeAdapter) ResumeArgs() []string {
	return []string{"--continue"}
}

func (a *ClaudeAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".claude", InstructionFile: "CLAUDE.md"}
}

func (a *ClaudeAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingHooks
}

func (a *ClaudeAdapter) SignalingArgs(filePath string) []string {
	return []string{"--append-system-prompt-file", filePath}
}
```

**Step 2: Run tests**

Run: `go test ./internal/detect/ -run "TestGetAdapter|TestAdapterInteractiveArgs|TestAdapterResumeArgs|TestAdapterOneshotArgs|TestAdapterStreamingArgs|TestAdapterInstructionConfig|TestAdapterSignalingStrategy" -v`
Expected: Claude-related subtests PASS, others still fail

**Step 3: Commit**

```
feat(detect): implement ClaudeAdapter
```

---

### Task 3: Implement CodexAdapter

**Files:**

- Create: `internal/detect/adapter_codex.go`

**Step 1: Write the CodexAdapter**

Create `internal/detect/adapter_codex.go`:

```go
package detect

import (
	"context"
	"fmt"
)

// CodexAdapter implements ToolAdapter for OpenAI Codex.
type CodexAdapter struct{}

func init() { registerAdapter(&CodexAdapter{}) }

func (a *CodexAdapter) Name() string { return "codex" }

func (a *CodexAdapter) Detect(ctx context.Context) (Tool, bool) {
	return (&codexDetector{}).Detect(ctx)
}

func (a *CodexAdapter) InteractiveArgs(model *Model) []string {
	if model != nil && model.ModelFlag != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *CodexAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"exec", "--json"}
	if model != nil && model.ModelFlag != "" {
		args = append(args, model.ModelFlag, model.ModelValue)
	}
	if jsonSchema != "" {
		args = append(args, "--output-schema", jsonSchema)
	}
	return args, nil
}

func (a *CodexAdapter) StreamingArgs(model *Model) ([]string, error) {
	return nil, fmt.Errorf("tool codex: oneshot-streaming mode is not supported")
}

func (a *CodexAdapter) ResumeArgs() []string {
	return []string{"resume", "--last"}
}

func (a *CodexAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".codex", InstructionFile: "AGENTS.md"}
}

func (a *CodexAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingCLIFlag
}

func (a *CodexAdapter) SignalingArgs(filePath string) []string {
	return []string{"-c", "model_instructions_file=" + filePath}
}
```

**Step 2: Run tests**

Run: `go test ./internal/detect/ -run "TestGetAdapter|TestAdapterResumeArgs|TestAdapterOneshotArgs|TestAdapterStreamingArgs|TestAdapterInstructionConfig|TestAdapterSignalingStrategy" -v`
Expected: Claude + Codex subtests PASS

**Step 3: Commit**

```
feat(detect): implement CodexAdapter
```

---

### Task 4: Implement GeminiAdapter

**Files:**

- Create: `internal/detect/adapter_gemini.go`

**Step 1: Write the GeminiAdapter**

Create `internal/detect/adapter_gemini.go`:

```go
package detect

import (
	"context"
	"fmt"
)

// GeminiAdapter implements ToolAdapter for Google Gemini CLI.
type GeminiAdapter struct{}

func init() { registerAdapter(&GeminiAdapter{}) }

func (a *GeminiAdapter) Name() string { return "gemini" }

func (a *GeminiAdapter) Detect(ctx context.Context) (Tool, bool) {
	return (&geminiDetector{}).Detect(ctx)
}

func (a *GeminiAdapter) InteractiveArgs(model *Model) []string {
	// Gemini uses -i for interactive mode, but that's baked into the detected command
	if model != nil && model.ModelFlag != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *GeminiAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool gemini: oneshot mode with JSON schema is not supported")
}

func (a *GeminiAdapter) StreamingArgs(model *Model) ([]string, error) {
	return nil, fmt.Errorf("tool gemini: streaming oneshot mode is not supported")
}

func (a *GeminiAdapter) ResumeArgs() []string {
	return []string{"-r", "latest"}
}

func (a *GeminiAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".gemini", InstructionFile: "GEMINI.md"}
}

func (a *GeminiAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingInstructionFile
}

func (a *GeminiAdapter) SignalingArgs(filePath string) []string {
	return nil // instruction file strategy doesn't use CLI args
}
```

**Step 2: Run tests**

Run: `go test ./internal/detect/ -run "TestGetAdapter|TestAllAdapters" -v`
Expected: Still fails — opencode adapter missing

**Step 3: Commit**

```
feat(detect): implement GeminiAdapter
```

---

### Task 5: Implement OpencodeAdapter

**Files:**

- Create: `internal/detect/adapter_opencode.go`

**Step 1: Write the OpencodeAdapter**

Create `internal/detect/adapter_opencode.go`:

```go
package detect

import (
	"context"
	"fmt"
	"path/filepath"
)

// OpencodeAdapter implements ToolAdapter for OpenCode.
type OpencodeAdapter struct{}

func init() { registerAdapter(&OpencodeAdapter{}) }

func (a *OpencodeAdapter) Name() string { return "opencode" }

func (a *OpencodeAdapter) Detect(ctx context.Context) (Tool, bool) {
	// Method 1: PATH lookup
	if commandExists("opencode") {
		if tryCommand(ctx, "opencode", "--version") {
			if pkgLogger != nil {
				pkgLogger.Info("opencode found via PATH", "command", "opencode")
			}
			return Tool{Name: "opencode", Command: "opencode", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Native install location (~/.local/bin/opencode)
	if fileExists("~/.local/bin/opencode") {
		cmd := filepath.Join(homeDirOrTilde(), ".local", "bin", "opencode")
		if tryCommand(ctx, cmd, "--version") {
			if pkgLogger != nil {
				pkgLogger.Info("opencode found via native install", "command", cmd)
			}
			return Tool{Name: "opencode", Command: cmd, Source: "native install (~/.local/bin/opencode)", Agentic: true}, true
		}
	}

	// Method 3: Homebrew formula
	if homebrewFormulaInstalled(ctx, "opencode") {
		if pkgLogger != nil {
			pkgLogger.Info("opencode found via Homebrew formula", "command", "opencode")
		}
		return Tool{Name: "opencode", Command: "opencode", Source: "Homebrew formula opencode", Agentic: true}, true
	}

	// Method 4: npm global
	if npmGlobalInstalled(ctx, "opencode-ai") {
		if pkgLogger != nil {
			pkgLogger.Info("opencode found via npm global", "package", "opencode-ai", "command", "opencode")
		}
		return Tool{Name: "opencode", Command: "opencode", Source: "npm global package opencode-ai", Agentic: true}, true
	}

	return Tool{}, false
}

func (a *OpencodeAdapter) InteractiveArgs(model *Model) []string {
	if model != nil && model.ModelFlag != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *OpencodeAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"run"}
	if model != nil && model.ModelFlag != "" {
		args = append(args, model.ModelFlag, model.ModelValue)
	}
	if jsonSchema != "" {
		args = append(args, "--format", "json")
	}
	return args, nil
}

func (a *OpencodeAdapter) StreamingArgs(model *Model) ([]string, error) {
	return nil, fmt.Errorf("tool opencode: streaming oneshot mode is not yet supported")
}

func (a *OpencodeAdapter) ResumeArgs() []string {
	return []string{"--continue"}
}

func (a *OpencodeAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".opencode", InstructionFile: "AGENTS.md"}
}

func (a *OpencodeAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingInstructionFile
}

func (a *OpencodeAdapter) SignalingArgs(filePath string) []string {
	return nil // instruction file strategy doesn't use CLI args
}
```

**Step 2: Run ALL adapter tests**

Run: `go test ./internal/detect/ -run "TestGetAdapter|TestAllAdapters|TestAdapterInteractiveArgs|TestAdapterResumeArgs|TestAdapterOneshotArgs|TestAdapterStreamingArgs|TestAdapterInstructionConfig|TestAdapterSignalingStrategy" -v`
Expected: ALL PASS

**Step 3: Commit**

```
feat(detect): implement OpencodeAdapter with detection and command building
```

---

### Task 6: Add opencode to tools.go registry and models

**Files:**

- Modify: `internal/detect/tools.go:4` (builtinToolNames)
- Modify: `internal/detect/tools.go:32-36` (agentInstructionConfigs)
- Modify: `internal/detect/models.go:40-195` (builtinModels — add opencode model)

**Step 1: Write the failing test**

Add to `internal/detect/tools_test.go`:

```go
func TestOpencodeInBuiltinTools(t *testing.T) {
	t.Parallel()
	if !IsBuiltinToolName("opencode") {
		t.Error("opencode should be a builtin tool name")
	}
}

func TestOpencodeInstructionConfig(t *testing.T) {
	t.Parallel()
	cfg, ok := GetAgentInstructionConfig("opencode")
	if !ok {
		t.Fatal("expected opencode instruction config")
	}
	if cfg.InstructionDir != ".opencode" {
		t.Errorf("InstructionDir = %q, want '.opencode'", cfg.InstructionDir)
	}
	if cfg.InstructionFile != "AGENTS.md" {
		t.Errorf("InstructionFile = %q, want 'AGENTS.md'", cfg.InstructionFile)
	}
}
```

Add to `internal/detect/models_test.go`:

```go
func TestOpencodeModelExists(t *testing.T) {
	t.Parallel()
	model, ok := FindModel("opencode-zen")
	if !ok {
		t.Fatal("expected opencode-zen model to exist")
	}
	if model.BaseTool != "opencode" {
		t.Errorf("BaseTool = %q, want 'opencode'", model.BaseTool)
	}
	if model.Category != "native" {
		t.Errorf("Category = %q, want 'native'", model.Category)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/detect/ -run "TestOpencodeInBuiltinTools|TestOpencodeInstructionConfig|TestOpencodeModelExists" -v`
Expected: FAIL

**Step 3: Add opencode to registries**

In `internal/detect/tools.go`, change line 4:

```go
var builtinToolNames = []string{"claude", "codex", "gemini", "opencode"}
```

In `internal/detect/tools.go`, change lines 32-36:

```go
var agentInstructionConfigs = map[string]AgentInstructionConfig{
	"claude":   {InstructionDir: ".claude", InstructionFile: "CLAUDE.md"},
	"codex":    {InstructionDir: ".codex", InstructionFile: "AGENTS.md"},
	"gemini":   {InstructionDir: ".gemini", InstructionFile: "GEMINI.md"},
	"opencode": {InstructionDir: ".opencode", InstructionFile: "AGENTS.md"},
}
```

In `internal/detect/models.go`, add after the Codex models block (after line 194):

```go
	// OpenCode models
	{
		ID:          "opencode-zen",
		DisplayName: "opencode zen (free)",
		BaseTool:    "opencode",
		Provider:    "opencode-zen",
		ModelValue:  "", // uses opencode's default Zen model
		ModelFlag:   "--model",
		Category:    "native",
	},
```

**Step 4: Run tests**

Run: `go test ./internal/detect/ -run "TestOpencodeInBuiltinTools|TestOpencodeInstructionConfig|TestOpencodeModelExists" -v`
Expected: ALL PASS

**Step 5: Run full detect test suite to check nothing broke**

Run: `go test ./internal/detect/ -v`
Expected: ALL PASS

**Step 6: Commit**

```
feat(detect): register opencode as builtin tool with Zen free-tier model
```

---

### Task 7: Add opencode to detection pipeline

**Files:**

- Modify: `internal/detect/agents.go:69-73` (DetectAvailableTools detectors list)
- Modify: `internal/detect/agents.go:136-140` (DetectAvailableToolsContext detectors list)

**Step 1: Write the failing test**

Add to `internal/detect/agents_test.go`:

```go
func TestOpencodeDetectorRegistered(t *testing.T) {
	t.Parallel()
	// The opencode adapter should be callable as a detector
	a := GetAdapter("opencode")
	if a == nil {
		t.Fatal("opencode adapter not registered")
	}
	if a.Name() != "opencode" {
		t.Errorf("Name() = %q, want 'opencode'", a.Name())
	}
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./internal/detect/ -run TestOpencodeDetectorRegistered -v`
Expected: PASS (adapter was registered in task 5)

**Step 3: Update detection pipeline to use adapter registry**

In `internal/detect/agents.go`, replace the hardcoded detector list in `DetectAvailableTools` (lines 69-73):

Old:

```go
	detectors := []ToolDetector{
		&claudeDetector{},
		&codexDetector{},
		&geminiDetector{},
	}
```

New:

```go
	detectors := allDetectors()
```

Same change in `DetectAvailableToolsContext` (lines 136-140).

Add the helper function:

```go
// allDetectors returns ToolDetectors for all registered adapters.
func allDetectors() []ToolDetector {
	all := AllAdapters()
	detectors := make([]ToolDetector, len(all))
	for i, a := range all {
		detectors[i] = a
	}
	return detectors
}
```

Note: `ToolAdapter` embeds `Detect(ctx) (Tool, bool)` and `Name() string` — it satisfies `ToolDetector` already.

**Step 4: Run full detection test suite**

Run: `go test ./internal/detect/ -run "TestDetect|TestToolDetector" -v`
Expected: ALL PASS — opencode now included in detection sweep

**Step 5: Also update the `TestToolDetectorConfig` test**

In `internal/detect/agents_test.go`, update `TestToolDetectorConfig` (lines 104-142) to include opencode:

Add to the `tests` slice:

```go
		{
			name:     "opencode detector",
			detector: &OpencodeAdapter{},
			wantName: "opencode",
		},
```

And update the `detectors` slice or switch to using `AllAdapters()`.

**Step 6: Run tests**

Run: `go test ./internal/detect/ -v`
Expected: ALL PASS

**Step 7: Commit**

```
refactor(detect): use adapter registry for detection pipeline, add opencode
```

---

### Task 8: Rewrite BuildCommandParts to use adapters

**Files:**

- Modify: `internal/detect/commands.go` (replace switch logic with adapter dispatch)

**Step 1: Write the compatibility test**

The key constraint: the new `BuildCommandParts` must produce identical output to the old one for all existing tool/mode combinations. The existing tests in `commands_test.go` already cover this — they'll serve as our regression tests.

Add one new test for opencode to `internal/detect/commands_test.go`:

```go
func TestBuildCommandParts_OpencodeResume(t *testing.T) {
	got, err := BuildCommandParts("opencode", "opencode", ToolModeResume, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"opencode", "--continue"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildCommandParts_OpencodeOneshot(t *testing.T) {
	got, err := BuildCommandParts("opencode", "opencode", ToolModeOneshot, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain "run" subcommand
	assertContains(t, got, "run")
}

func TestBuildCommandParts_OpencodeInteractive(t *testing.T) {
	model := &Model{ModelFlag: "--model", ModelValue: "anthropic/claude-sonnet-4-5"}
	got, err := BuildCommandParts("opencode", "opencode", ToolModeInteractive, "", model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"opencode", "--model", "anthropic/claude-sonnet-4-5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
```

**Step 2: Run tests to verify new tests fail (old tests pass)**

Run: `go test ./internal/detect/ -run TestBuildCommandParts -v`
Expected: New opencode tests FAIL, existing tests PASS

**Step 3: Rewrite BuildCommandParts**

Replace the entire function body in `internal/detect/commands.go`:

```go
// BuildCommandParts builds command parts for the given detected tool.
// Delegates to the tool's adapter for mode-specific argument construction.
// The jsonSchema parameter is used for oneshot mode (structured output).
// The model parameter is optional; if provided, used for model-specific flags.
func BuildCommandParts(toolName, detectedCommand string, mode ToolMode, jsonSchema string, model *Model) ([]string, error) {
	parts := strings.Fields(detectedCommand)
	if len(parts) == 0 {
		return nil, fmt.Errorf("tool %s: empty command", toolName)
	}

	adapter := GetAdapter(toolName)
	if adapter == nil {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	var modeArgs []string
	var err error

	switch mode {
	case ToolModeInteractive:
		modeArgs = adapter.InteractiveArgs(model)
	case ToolModeOneshot:
		modeArgs, err = adapter.OneshotArgs(model, jsonSchema)
	case ToolModeOneshotStreaming:
		modeArgs, err = adapter.StreamingArgs(model)
	case ToolModeResume:
		modeArgs = adapter.ResumeArgs()
	default:
		return nil, fmt.Errorf("tool %s: unknown mode %q", toolName, mode)
	}

	if err != nil {
		return nil, err
	}

	return append(parts, modeArgs...), nil
}
```

**Step 4: Run ALL command tests**

Run: `go test ./internal/detect/ -run TestBuildCommandParts -v`
Expected: ALL PASS — existing behavior preserved, opencode tests pass

**Important regression check**: The old `BuildCommandParts` for resume mode used to rebuild the command from scratch (e.g., `return []string{"claude", "--continue"}`), ignoring `detectedCommand`. The new version appends `ResumeArgs()` to the detected command parts. This is a behavior change we need to verify — if `detectedCommand` is `/usr/local/bin/claude`, old code returned `["claude", "--continue"]` but new code returns `["/usr/local/bin/claude", "--continue"]`. The new behavior is actually more correct (uses the detected path), but we should verify callers handle this.

Check the resume test cases — if they pass with `detectedCmd: "claude"`, the change is compatible since `strings.Fields("claude")` returns `["claude"]`.

**Step 5: Run full test suite**

Run: `go test ./internal/detect/ -v && go test ./internal/oneshot/ -v && go test ./internal/session/ -v`
Expected: ALL PASS

**Step 6: Commit**

```
refactor(detect): rewrite BuildCommandParts to use adapter dispatch
```

---

### Task 9: Update session manager signaling to use adapters

**Files:**

- Modify: `internal/session/manager.go:950-981` (appendSignalingFlags)
- Modify: `internal/session/manager.go:983-995` (appendPersonaFlags)
- Modify: `internal/session/manager.go:622-632` (spawn signaling setup)

**Step 1: Refactor appendSignalingFlags**

Replace the `appendSignalingFlags` function (lines 950-981) with adapter-based logic:

```go
func appendSignalingFlags(cmd, baseTool string, isRemote bool) string {
	adapter := detect.GetAdapter(baseTool)
	if adapter == nil {
		return cmd
	}

	strategy := adapter.SignalingStrategy()

	if strategy == detect.SignalingHooks {
		// Hooks-based tools handle signaling via workspace config, not CLI flags
		return cmd
	}

	if strategy == detect.SignalingCLIFlag {
		if isRemote {
			// Remote mode: inline content for claude, skip for others
			if baseTool == "claude" {
				return fmt.Sprintf("%s --append-system-prompt %s", cmd, shellutil.Quote(ensure.SignalingInstructions))
			}
			return cmd
		}
		// Local mode: use file-based injection via adapter's signaling args
		sigArgs := adapter.SignalingArgs(ensure.SignalingInstructionsFilePath())
		for _, arg := range sigArgs {
			cmd = fmt.Sprintf("%s %s", cmd, shellutil.Quote(arg))
		}
		return cmd
	}

	// SignalingInstructionFile: no CLI flags needed (handled by ensure package)
	return cmd
}
```

**Step 2: Refactor the spawn signaling setup**

Replace lines 622-632 in `internal/session/manager.go`:

Old:

```go
	if !ensure.SupportsHooks(baseTool) {
		if ensure.SupportsSystemPromptFlag(baseTool) {
			if err := ensure.SignalingInstructionsFile(); err != nil {
				m.logger.Warn("failed to ensure signaling instructions file", "err", err)
			}
		} else {
			if err := ensure.AgentInstructions(w.Path, opts.TargetName); err != nil {
				m.logger.Warn("failed to provision agent instructions", "err", err)
			}
		}
	}
```

New:

```go
	adapter := detect.GetAdapter(baseTool)
	if adapter != nil {
		switch adapter.SignalingStrategy() {
		case detect.SignalingHooks:
			// Handled by ensurer.ForSpawn above
		case detect.SignalingCLIFlag:
			if err := ensure.SignalingInstructionsFile(); err != nil {
				m.logger.Warn("failed to ensure signaling instructions file", "err", err)
			}
		case detect.SignalingInstructionFile:
			if err := ensure.AgentInstructions(w.Path, opts.TargetName); err != nil {
				m.logger.Warn("failed to provision agent instructions", "err", err)
			}
		}
	}
```

**Step 3: Run tests**

Run: `go test ./internal/session/ -v && go test ./internal/detect/ -v`
Expected: ALL PASS

**Step 4: Commit**

```
refactor(session): use adapter signaling strategy instead of hardcoded tool checks
```

---

### Task 10: Update ensure package to use adapters

**Files:**

- Modify: `internal/workspace/ensure/manager.go:359-364` (SupportsHooks)
- Modify: `internal/workspace/ensure/manager.go:306-318` (SupportsSystemPromptFlag)

**Step 1: Refactor SupportsHooks and SupportsSystemPromptFlag**

These functions should delegate to the adapter registry:

```go
func SupportsHooks(baseTool string) bool {
	adapter := detect.GetAdapter(baseTool)
	if adapter == nil {
		return false
	}
	return adapter.SignalingStrategy() == detect.SignalingHooks
}

func SupportsSystemPromptFlag(toolName string) bool {
	adapter := detect.GetAdapter(toolName)
	if adapter == nil {
		return false
	}
	return adapter.SignalingStrategy() == detect.SignalingCLIFlag
}
```

**Step 2: Run tests**

Run: `go test ./internal/workspace/ensure/ -v && go test ./internal/session/ -v`
Expected: ALL PASS

**Step 3: Commit**

```
refactor(ensure): delegate hook/flag support checks to adapter registry
```

---

### Task 11: Full regression test

**Step 1: Run full test suite**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 2: Verify all callers still work**

Key callers to verify (by reading test output):

- `internal/detect/` — adapter tests + legacy BuildCommandParts tests
- `internal/oneshot/` — oneshot execution uses BuildCommandParts
- `internal/session/` — spawn + resume flows
- `internal/floormanager/` — FM resume command
- `internal/workspace/ensure/` — signaling setup
- `internal/config/` — run target validation

**Step 3: Commit if any fixes needed, then final commit**

```
test: verify opencode integration phase 1 regression
```

---

### Task 12: Commit and verify

**Step 1: Run full test suite one more time**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 2: Review the diff**

Run: `git diff main --stat` to see the full scope of changes.

Expected file changes:

- `internal/detect/adapter.go` — NEW (interface + registry)
- `internal/detect/adapter_claude.go` — NEW
- `internal/detect/adapter_codex.go` — NEW
- `internal/detect/adapter_gemini.go` — NEW
- `internal/detect/adapter_opencode.go` — NEW
- `internal/detect/adapter_test.go` — NEW
- `internal/detect/commands.go` — MODIFIED (simplified dispatcher)
- `internal/detect/commands_test.go` — MODIFIED (opencode tests added)
- `internal/detect/tools.go` — MODIFIED (opencode in builtins)
- `internal/detect/tools_test.go` — MODIFIED (opencode tests)
- `internal/detect/models.go` — MODIFIED (opencode-zen model)
- `internal/detect/models_test.go` — MODIFIED (opencode model test)
- `internal/detect/agents.go` — MODIFIED (use adapter registry for detection)
- `internal/detect/agents_test.go` — MODIFIED (opencode detector test)
- `internal/session/manager.go` — MODIFIED (adapter-based signaling)
- `internal/workspace/ensure/manager.go` — MODIFIED (delegate to adapters)

The existing detector implementations (`claudeDetector`, `codexDetector`, `geminiDetector`) remain in `agents.go` — they're now called by the adapters. We don't delete them to minimize risk. They can be inlined into adapters in a follow-up cleanup.
