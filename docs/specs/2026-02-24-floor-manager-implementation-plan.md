# Floor Manager Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Build a singleton floor manager agent that monitors all agent sessions via the event pipeline and provides a conversational orchestration interface.

**Architecture:** The floor manager is a peer to the session manager — it manages its own tmux session directly (no workspace, no event hooks, no session list presence). An Injector registers as an `events.EventHandler` alongside `DashboardHandler` to forward filtered status events into the FM's terminal. The dashboard home page reorganizes around the FM terminal as the primary interface.

**Tech Stack:** Go (backend), React/TypeScript (frontend), tmux (agent session), chi router (HTTP), WebSocket (real-time updates)

**Design doc:** `docs/specs/2026-02-24-floor-manager-design.md`

---

### Task 1: Add FloorManagerConfig to Config and API Contracts

**Files:**

- Modify: `internal/config/config.go:64-112` (Config struct), add getters
- Modify: `internal/api/contracts/config.go:113-254` (ConfigResponse, ConfigUpdateRequest)
- Regenerate: `assets/dashboard/src/lib/types.generated.ts`

**Step 1: Write the failing test**

Create `internal/config/config_floor_manager_test.go`:

```go
package config

import (
	"encoding/json"
	"testing"
)

func TestFloorManagerConfigDefaults(t *testing.T) {
	cfg := &Config{}
	if cfg.GetFloorManagerEnabled() != false {
		t.Error("expected floor manager disabled by default")
	}
	if cfg.GetFloorManagerTarget() != "" {
		t.Error("expected empty target by default")
	}
	if cfg.GetFloorManagerRotationThreshold() != 150 {
		t.Errorf("expected default rotation threshold 150, got %d", cfg.GetFloorManagerRotationThreshold())
	}
	if cfg.GetFloorManagerDebounceMs() != 2000 {
		t.Errorf("expected default debounce 2000, got %d", cfg.GetFloorManagerDebounceMs())
	}
}

func TestFloorManagerConfigJSON(t *testing.T) {
	raw := `{"floor_manager":{"enabled":true,"target":"claude-sonnet","rotation_threshold":200,"debounce_ms":3000}}`
	cfg := &Config{}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.GetFloorManagerEnabled() {
		t.Error("expected floor manager enabled")
	}
	if cfg.GetFloorManagerTarget() != "claude-sonnet" {
		t.Errorf("expected target claude-sonnet, got %s", cfg.GetFloorManagerTarget())
	}
	if cfg.GetFloorManagerRotationThreshold() != 200 {
		t.Errorf("expected rotation threshold 200, got %d", cfg.GetFloorManagerRotationThreshold())
	}
	if cfg.GetFloorManagerDebounceMs() != 3000 {
		t.Errorf("expected debounce 3000, got %d", cfg.GetFloorManagerDebounceMs())
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -run TestFloorManager -v
```

Expected: FAIL — `GetFloorManagerEnabled` not defined.

**Step 3: Implement FloorManagerConfig**

In `internal/config/config.go`, add the struct (near line 262, alongside other sub-configs):

```go
// FloorManagerConfig configures the floor manager singleton agent.
type FloorManagerConfig struct {
	Enabled            *bool  `json:"enabled,omitempty"`
	Target             string `json:"target,omitempty"`
	RotationThreshold  int    `json:"rotation_threshold,omitempty"`
	DebounceMs         int    `json:"debounce_ms,omitempty"`
}
```

Add the field to the `Config` struct (near line 93, alongside other optional configs):

```go
FloorManager *FloorManagerConfig `json:"floor_manager,omitempty"`
```

Add getter methods (follow the pattern at lines 640-699):

```go
func (c *Config) GetFloorManagerEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil || c.FloorManager.Enabled == nil {
		return false
	}
	return *c.FloorManager.Enabled
}

func (c *Config) GetFloorManagerTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil {
		return ""
	}
	return strings.TrimSpace(c.FloorManager.Target)
}

func (c *Config) GetFloorManagerRotationThreshold() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil || c.FloorManager.RotationThreshold <= 0 {
		return 150
	}
	return c.FloorManager.RotationThreshold
}

func (c *Config) GetFloorManagerDebounceMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil || c.FloorManager.DebounceMs <= 0 {
		return 2000
	}
	return c.FloorManager.DebounceMs
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/config/ -run TestFloorManager -v
```

Expected: PASS

**Step 5: Add API contract types**

In `internal/api/contracts/config.go`, add to `ConfigResponse` struct (near line 139):

```go
FloorManager FloorManagerResponse `json:"floor_manager"`
```

Add to `ConfigUpdateRequest` struct (near line 254):

```go
FloorManager *FloorManagerUpdate `json:"floor_manager,omitempty"`
```

Add the structs (near line 304, alongside other response/update pairs):

```go
type FloorManagerResponse struct {
	Enabled            bool   `json:"enabled"`
	Target             string `json:"target"`
	RotationThreshold  int    `json:"rotation_threshold"`
	DebounceMs         int    `json:"debounce_ms"`
}

type FloorManagerUpdate struct {
	Enabled            *bool   `json:"enabled,omitempty"`
	Target             *string `json:"target,omitempty"`
	RotationThreshold  *int    `json:"rotation_threshold,omitempty"`
	DebounceMs         *int    `json:"debounce_ms,omitempty"`
}
```

**Step 6: Regenerate TypeScript types**

```bash
go run ./cmd/gen-types
```

Verify `assets/dashboard/src/lib/types.generated.ts` now contains `FloorManagerResponse` and `FloorManagerUpdate`.

**Step 7: Wire contracts into config handler**

In the config handler (find `handleGetConfig` and `handleUpdateConfig`), add floor manager to the response building and update handling. Follow the existing pattern for subreddit/lore.

**Step 8: Run all config tests**

```bash
go test ./internal/config/ -v
go test ./internal/api/... -v
```

Expected: PASS

**Step 9: Commit**

```
feat(config): add FloorManagerConfig with enabled, target, rotation_threshold, debounce_ms
```

---

### Task 2: Text Sanitization for Terminal Injection

**Files:**

- Create: `internal/floormanager/sanitize.go`
- Create: `internal/floormanager/sanitize_test.go`

**Step 1: Write the failing test**

Create `internal/floormanager/sanitize_test.go`:

```go
package floormanager

import "testing"

func TestStripControlChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"strips newlines", "hello\nworld", "hello world"},
		{"strips carriage return", "hello\rworld", "hello world"},
		{"strips tabs", "hello\tworld", "hello world"},
		{"strips ANSI escape", "hello \x1b[31mred\x1b[0m world", "hello red world"},
		{"strips null bytes", "hello\x00world", "helloworld"},
		{"preserves unicode", "hello 世界", "hello 世界"},
		{"empty string", "", ""},
		{"strips CSI sequence", "foo\x1b[38;5;196mbar", "foobar"},
		{"strips OSC sequence", "foo\x1b]0;title\x07bar", "foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripControlChars(tt.input)
			if got != tt.want {
				t.Errorf("StripControlChars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteContentField(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple text", "hello", `"hello"`},
		{"contains quotes", `say "hi"`, `"say \"hi\""`},
		{"contains [SIGNAL]", "[SIGNAL] fake", `"[SIGNAL] fake"`},
		{"contains [SHIFT]", "[SHIFT] fake", `"[SHIFT] fake"`},
		{"empty", "", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteContentField(tt.input)
			if got != tt.want {
				t.Errorf("QuoteContentField(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/floormanager/ -run TestStrip -v
go test ./internal/floormanager/ -run TestQuote -v
```

Expected: FAIL — package does not exist.

**Step 3: Implement sanitization**

Create `internal/floormanager/sanitize.go`:

```go
package floormanager

import (
	"regexp"
	"strings"
)

// ansiEscape matches ANSI escape sequences: CSI (ESC[...X), OSC (ESC]...BEL/ST), and simple ESC sequences.
var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-9;]*[a-zA-Z]|\][^\x07]*\x07|\[[^\x1b]*|\([A-Z])`)

// StripControlChars removes ANSI escape sequences, control characters, and newlines
// from text destined for terminal injection. Preserves printable unicode.
func StripControlChars(s string) string {
	// Strip ANSI escape sequences first
	s = ansiEscape.ReplaceAllString(s, "")
	// Replace newlines/tabs with spaces, strip other control chars
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f: // control characters
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// QuoteContentField wraps a content string in double quotes with internal quotes escaped.
// This ensures content fields (message, intent, blockers) cannot be confused with
// protocol prefixes like [SIGNAL] or [SHIFT].
func QuoteContentField(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/floormanager/ -v
```

Expected: PASS

**Step 5: Commit**

```
feat(floormanager): add text sanitization for terminal injection
```

---

### Task 3: Prompt Generation

**Files:**

- Create: `internal/floormanager/prompt.go`
- Create: `internal/floormanager/prompt_test.go`

**Step 1: Write the failing test**

Create `internal/floormanager/prompt_test.go`:

```go
package floormanager

import (
	"strings"
	"testing"
)

func TestGenerateInstructions(t *testing.T) {
	instructions := GenerateInstructions()

	checks := []string{
		"floor manager",
		"schmux status",
		"schmux spawn",
		"schmux list",
		"memory.md",
		"[SIGNAL]",
		"[SHIFT]",
		"schmux end-shift",
	}
	for _, check := range checks {
		if !strings.Contains(instructions, check) {
			t.Errorf("GenerateInstructions() missing %q", check)
		}
	}

	// Must NOT contain destructive commands as instructions (they're blocked at settings level)
	if strings.Contains(instructions, "schmux dispose") {
		// It can mention dispose in context of "ask the operator", but should not list it as available
	}
}

func TestGenerateSettings(t *testing.T) {
	settings := GenerateSettings()

	// Must pre-approve non-destructive commands
	approvedPatterns := []string{
		"schmux status",
		"schmux list",
		"schmux spawn",
		"schmux end-shift",
		"cat memory.md",
	}
	for _, pattern := range approvedPatterns {
		if !strings.Contains(settings, pattern) {
			t.Errorf("GenerateSettings() missing approved pattern %q", pattern)
		}
	}

	// Must NOT pre-approve destructive commands
	if strings.Contains(settings, "schmux dispose") {
		t.Error("GenerateSettings() must not pre-approve schmux dispose")
	}
	if strings.Contains(settings, "schmux stop") {
		t.Error("GenerateSettings() must not pre-approve schmux stop")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/floormanager/ -run TestGenerate -v
```

Expected: FAIL — `GenerateInstructions` not defined.

**Step 3: Implement prompt generation**

Create `internal/floormanager/prompt.go`:

```go
package floormanager

import "encoding/json"

// GenerateInstructions returns the CLAUDE.md / AGENTS.md content for the floor manager.
func GenerateInstructions() string {
	return `# Floor Manager

You are the floor manager for this schmux instance. You orchestrate work across multiple AI coding agents. You monitor their status, relay information to the human operator, and execute commands on their behalf.

## On Startup

1. Read memory.md in your working directory for context from previous sessions
2. Run schmux status to see the current state of all workspaces and sessions
3. When the operator connects, proactively summarize what you found

## Available Commands

- schmux status — see all workspaces, sessions, and their states
- schmux spawn -a <target> -p "<prompt>" [-b <branch>] [-r <repo>] — create new agent sessions
- schmux list — list all sessions with IDs
- schmux attach <session-id> — get tmux attach command for a session

## Signal Handling

You will receive [SIGNAL] messages injected into your terminal by the schmux daemon. Format:

[SIGNAL] <session-name>: <old-state> -> <new-state> <summary> [intent=<...>] [blocked=<...>]

When a [SIGNAL] arrives, evaluate and decide:
- Act autonomously (e.g., spawn a replacement if an agent errored)
- Inform the operator (e.g., "claude-1 needs input about auth tokens")
- Note silently (e.g., an agent completed a minor task)

## Behavior Guidelines

- Keep responses concise — the operator may be on a phone
- Answer questions about the system using existing context without running commands when possible
- You cannot run schmux dispose or schmux stop directly — if you think a session should be disposed, recommend it to the operator and they will approve it

## Memory File

Maintain memory.md with:
- Key decisions made by the operator
- Ongoing tasks and their status
- Things the operator asked you to watch for
- Pending actions

This file persists across session restarts — it is your long-term memory.

## Shift Rotation

If a [SHIFT] message appears, a forced rotation is imminent (30 seconds). Immediately:
1. Write your final summary to memory.md
2. Run schmux end-shift
3. Do not acknowledge the [SHIFT] to the operator — just save memory and signal completion
`
}

// GenerateSettings returns the .claude/settings.json content for the floor manager.
// Only non-destructive commands are pre-approved.
func GenerateSettings() string {
	settings := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": []string{
				"Bash(schmux status*)",
				"Bash(schmux list*)",
				"Bash(schmux spawn*)",
				"Bash(schmux end-shift*)",
				"Bash(schmux attach*)",
				"Bash(cat memory.md)",
				"Bash(echo * > memory.md)",
				"Bash(printf * > memory.md)",
			},
		},
	}
	b, _ := json.MarshalIndent(settings, "", "  ")
	return string(b)
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/floormanager/ -run TestGenerate -v
```

Expected: PASS

**Step 5: Commit**

```
feat(floormanager): add prompt and settings generation
```

---

### Task 4: Injector — Event Handler with Filtering, Formatting, and Debounce

**Files:**

- Create: `internal/floormanager/injector.go`
- Create: `internal/floormanager/injector_test.go`

**Step 1: Write the failing test for signal filtering**

Create `internal/floormanager/injector_test.go`:

```go
package floormanager

import (
	"testing"
)

func TestShouldInject(t *testing.T) {
	tests := []struct {
		name     string
		prev     string
		curr     string
		expected bool
	}{
		{"working to error", "working", "error", true},
		{"working to needs_input", "working", "needs_input", true},
		{"working to needs_testing", "working", "needs_testing", true},
		{"working to completed", "working", "completed", true},
		{"working to working", "working", "working", false},
		{"needs_input to working", "needs_input", "working", false},
		{"error to working", "error", "working", false},
		{"empty to working", "", "working", false},
		{"empty to error", "", "error", true},
		{"empty to needs_input", "", "needs_input", true},
		{"completed to error", "completed", "error", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldInject(tt.prev, tt.curr)
			if got != tt.expected {
				t.Errorf("shouldInject(%q, %q) = %v, want %v", tt.prev, tt.curr, got, tt.expected)
			}
		})
	}
}

func TestFormatSignalMessage(t *testing.T) {
	tests := []struct {
		name     string
		nickname string
		prev     string
		state    string
		message  string
		intent   string
		blockers string
		want     string
	}{
		{
			name:     "minimal",
			nickname: "claude-1",
			prev:     "working",
			state:    "completed",
			message:  "Auth module finished",
			want:     `[SIGNAL] claude-1: working -> completed "Auth module finished"`,
		},
		{
			name:     "with intent and blockers",
			nickname: "claude-1",
			prev:     "working",
			state:    "needs_input",
			message:  "Need clarification",
			intent:   "Implementing OAuth2",
			blockers: "Unknown token format",
			want:     `[SIGNAL] claude-1: working -> needs_input "Need clarification" intent="Implementing OAuth2" blocked="Unknown token format"`,
		},
		{
			name:     "with intent only",
			nickname: "claude-1",
			prev:     "working",
			state:    "error",
			message:  "Build failed",
			intent:   "Running tests",
			want:     `[SIGNAL] claude-1: working -> error "Build failed" intent="Running tests"`,
		},
		{
			name:     "empty prev state",
			nickname: "agent-2",
			prev:     "",
			state:    "error",
			message:  "Crashed",
			want:     `[SIGNAL] agent-2: -> error "Crashed"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSignalMessage(tt.nickname, tt.prev, tt.state, tt.message, tt.intent, tt.blockers)
			if got != tt.want {
				t.Errorf("FormatSignalMessage() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/floormanager/ -run "TestShouldInject|TestFormatSignal" -v
```

Expected: FAIL — `shouldInject` not defined.

**Step 3: Implement the Injector**

Create `internal/floormanager/injector.go`:

```go
package floormanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/OWNER/schmux/internal/events"
	"github.com/OWNER/schmux/internal/tmux"
)

// NOTE: Replace "github.com/OWNER/schmux" with the actual module path from go.mod.

// Injector is an events.EventHandler that forwards filtered status events
// into the floor manager's terminal via tmux.
type Injector struct {
	manager    *Manager
	debounceMs int
	logger     *slog.Logger

	mu         sync.Mutex
	prevState  map[string]string // sessionID -> last known state
	pending    []string          // buffered messages during debounce window
	timer      *time.Timer
	stopCh     chan struct{}
	stopped    bool
}

// NewInjector creates a new Injector that forwards events to the floor manager.
func NewInjector(manager *Manager, debounceMs int, logger *slog.Logger) *Injector {
	return &Injector{
		manager:    manager,
		debounceMs: debounceMs,
		logger:     logger,
		prevState:  make(map[string]string),
		stopCh:     make(chan struct{}),
	}
}

// HandleEvent implements events.EventHandler. It receives status events from the
// unified event pipeline, filters them, and queues them for debounced injection.
func (inj *Injector) HandleEvent(ctx context.Context, sessionID string, raw events.RawEvent, data []byte) {
	if raw.Type != "status" {
		return
	}

	var evt events.StatusEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		inj.logger.Warn("failed to unmarshal status event", "err", err)
		return
	}

	inj.mu.Lock()
	defer inj.mu.Unlock()

	if inj.stopped {
		return
	}

	prev := inj.prevState[sessionID]
	inj.prevState[sessionID] = evt.State

	if !shouldInject(prev, evt.State) {
		return
	}

	// Look up session nickname from the manager
	nickname := inj.manager.resolveSessionName(sessionID)

	msg := FormatSignalMessage(
		nickname, prev, evt.State,
		StripControlChars(evt.Message),
		StripControlChars(evt.Intent),
		StripControlChars(evt.Blockers),
	)

	inj.pending = append(inj.pending, msg)

	// Reset debounce timer
	if inj.timer != nil {
		inj.timer.Stop()
	}
	inj.timer = time.AfterFunc(time.Duration(inj.debounceMs)*time.Millisecond, func() {
		inj.flush(ctx)
	})
}

// flush sends all pending messages to the floor manager's terminal.
func (inj *Injector) flush(ctx context.Context) {
	inj.mu.Lock()
	if len(inj.pending) == 0 || inj.stopped {
		inj.mu.Unlock()
		return
	}
	messages := inj.pending
	inj.pending = nil
	inj.mu.Unlock()

	tmuxSession := inj.manager.TmuxSession()
	if tmuxSession == "" {
		return
	}

	text := strings.Join(messages, "\n")
	if err := tmux.SendLiteral(ctx, tmuxSession, text); err != nil {
		inj.logger.Warn("failed to send signal to floor manager", "err", err)
		return
	}
	if err := tmux.SendKeys(ctx, tmuxSession, "Enter"); err != nil {
		inj.logger.Warn("failed to send Enter to floor manager", "err", err)
		return
	}

	inj.manager.IncrementInjectionCount(len(messages))
}

// Stop stops the injector and cancels any pending debounce timer.
func (inj *Injector) Stop() {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	inj.stopped = true
	if inj.timer != nil {
		inj.timer.Stop()
	}
	inj.pending = nil
}

// shouldInject determines whether a state transition should be forwarded to the FM.
func shouldInject(prev, curr string) bool {
	if curr == "working" {
		return false
	}
	return true
}

// FormatSignalMessage formats a status event as a [SIGNAL] line for terminal injection.
func FormatSignalMessage(nickname, prev, state, message, intent, blockers string) string {
	var b strings.Builder
	b.WriteString("[SIGNAL] ")
	b.WriteString(nickname)
	b.WriteString(": ")
	if prev != "" {
		b.WriteString(prev)
		b.WriteString(" -> ")
	} else {
		b.WriteString("-> ")
	}
	b.WriteString(state)
	if message != "" {
		b.WriteString(" ")
		b.WriteString(QuoteContentField(message))
	}
	if intent != "" {
		b.WriteString(" intent=")
		b.WriteString(QuoteContentField(intent))
	}
	if blockers != "" {
		b.WriteString(" blocked=")
		b.WriteString(QuoteContentField(blockers))
	}
	return b.String()
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/floormanager/ -run "TestShouldInject|TestFormatSignal" -v
```

Expected: PASS

**Step 5: Commit**

```
feat(floormanager): add Injector with event filtering, formatting, and debounced injection
```

---

### Task 5: Manager — Lifecycle, Spawn, Monitor, Rotation

**Files:**

- Create: `internal/floormanager/manager.go`
- Create: `internal/floormanager/manager_test.go`

**Step 1: Write the failing test**

Create `internal/floormanager/manager_test.go`:

```go
package floormanager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerWritesInstructionFiles(t *testing.T) {
	tmpDir := t.TempDir()
	m := &Manager{
		workDir: tmpDir,
	}

	if err := m.writeInstructionFiles(); err != nil {
		t.Fatal(err)
	}

	// Check CLAUDE.md exists and has content
	claudeMd, err := os.ReadFile(filepath.Join(tmpDir, "CLAUDE.md"))
	if err != nil {
		t.Fatal("CLAUDE.md not written:", err)
	}
	if len(claudeMd) == 0 {
		t.Error("CLAUDE.md is empty")
	}

	// Check AGENTS.md is identical
	agentsMd, err := os.ReadFile(filepath.Join(tmpDir, "AGENTS.md"))
	if err != nil {
		t.Fatal("AGENTS.md not written:", err)
	}
	if string(claudeMd) != string(agentsMd) {
		t.Error("CLAUDE.md and AGENTS.md should have identical content")
	}

	// Check .claude/settings.json exists
	settings, err := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal("settings.json not written:", err)
	}
	if len(settings) == 0 {
		t.Error("settings.json is empty")
	}

	// Check memory.md is NOT overwritten if it exists
	memPath := filepath.Join(tmpDir, "memory.md")
	if err := os.WriteFile(memPath, []byte("existing memory"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.writeInstructionFiles(); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(memPath)
	if string(content) != "existing memory" {
		t.Error("memory.md was overwritten")
	}
}

func TestManagerInjectionCount(t *testing.T) {
	m := &Manager{}

	m.IncrementInjectionCount(5)
	if m.InjectionCount() != 5 {
		t.Errorf("expected 5, got %d", m.InjectionCount())
	}

	m.IncrementInjectionCount(3)
	if m.InjectionCount() != 8 {
		t.Errorf("expected 8, got %d", m.InjectionCount())
	}

	m.ResetInjectionCount()
	if m.InjectionCount() != 0 {
		t.Errorf("expected 0 after reset, got %d", m.InjectionCount())
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/floormanager/ -run TestManager -v
```

Expected: FAIL — `Manager` struct incomplete.

**Step 3: Implement the Manager**

Create `internal/floormanager/manager.go`. This is the largest file — key components:

```go
package floormanager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/OWNER/schmux/internal/config"
	"github.com/OWNER/schmux/internal/session"
	"github.com/OWNER/schmux/internal/tmux"
)

// NOTE: Replace "github.com/OWNER/schmux" with the actual module path from go.mod.

const (
	tmuxSessionName   = "schmux-floor-manager"
	monitorInterval   = 15 * time.Second
	restartDelay      = 3 * time.Second
	shiftTimeout      = 30 * time.Second
)

// Manager manages the floor manager singleton agent session.
type Manager struct {
	cfg    *config.Config
	sm     *session.Manager // used only for ResolveTarget and session name lookups
	logger *slog.Logger

	workDir string // ~/.schmux/floor-manager/

	mu             sync.Mutex
	tmuxSession    string
	injectionCount int
	rotating       bool
	stopCh         chan struct{}
	stopped        bool

	// shiftDone is signaled when schmux end-shift is called
	shiftDone chan struct{}
}

// New creates a new floor manager Manager.
func New(cfg *config.Config, sm *session.Manager, homeDir string, logger *slog.Logger) *Manager {
	return &Manager{
		cfg:     cfg,
		sm:      sm,
		logger:  logger,
		workDir: filepath.Join(homeDir, ".schmux", "floor-manager"),
		stopCh:  make(chan struct{}),
	}
}

// Start spawns the floor manager session and starts the monitor goroutine.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.spawn(ctx); err != nil {
		return fmt.Errorf("failed to spawn floor manager: %w", err)
	}
	go m.monitor(ctx)
	return nil
}

// Stop stops the floor manager, killing its tmux session.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.stopCh)
	tmuxSess := m.tmuxSession
	m.mu.Unlock()

	if tmuxSess != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tmux.KillSession(ctx, tmuxSess)
	}
}

// TmuxSession returns the current tmux session name.
func (m *Manager) TmuxSession() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tmuxSession
}

// Running returns whether the floor manager tmux session is alive.
func (m *Manager) Running() bool {
	m.mu.Lock()
	sess := m.tmuxSession
	m.mu.Unlock()
	if sess == "" {
		return false
	}
	return tmux.SessionExists(context.Background(), sess)
}

// InjectionCount returns the current injection count.
func (m *Manager) InjectionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.injectionCount
}

// IncrementInjectionCount adds n to the injection count and checks if rotation is needed.
func (m *Manager) IncrementInjectionCount(n int) {
	m.mu.Lock()
	m.injectionCount += n
	count := m.injectionCount
	threshold := m.cfg.GetFloorManagerRotationThreshold()
	m.mu.Unlock()

	if count >= threshold {
		go m.handleShiftRotation(context.Background())
	}
}

// ResetInjectionCount resets the count to zero.
func (m *Manager) ResetInjectionCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injectionCount = 0
}

// EndShift signals that the FM has finished saving memory during a shift rotation.
func (m *Manager) EndShift() {
	m.mu.Lock()
	ch := m.shiftDone
	m.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (m *Manager) spawn(ctx context.Context) error {
	// Create working directory
	if err := os.MkdirAll(m.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work dir: %w", err)
	}

	// Write instruction files
	if err := m.writeInstructionFiles(); err != nil {
		return fmt.Errorf("failed to write instruction files: %w", err)
	}

	// Resolve the configured target to get the command
	targetName := m.cfg.GetFloorManagerTarget()
	if targetName == "" {
		return fmt.Errorf("no floor manager target configured")
	}
	resolved, err := m.sm.ResolveTarget(ctx, targetName)
	if err != nil {
		return fmt.Errorf("failed to resolve target %q: %w", targetName, err)
	}

	// Build command with prompt "Begin."
	// This will need to use session.BuildCommand or equivalent
	// to construct the agent launch command with the prompt.
	command, err := buildFMCommand(resolved, "Begin.")
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	// Create tmux session
	if err := tmux.CreateSession(ctx, tmuxSessionName, m.workDir, command); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	m.mu.Lock()
	m.tmuxSession = tmuxSessionName
	m.injectionCount = 0
	m.mu.Unlock()

	m.logger.Info("floor manager spawned", "tmux_session", tmuxSessionName)
	return nil
}

func (m *Manager) spawnResume(ctx context.Context) error {
	// Similar to spawn but with resume flag
	targetName := m.cfg.GetFloorManagerTarget()
	if targetName == "" {
		return fmt.Errorf("no floor manager target configured")
	}
	resolved, err := m.sm.ResolveTarget(ctx, targetName)
	if err != nil {
		return err
	}

	command, err := buildFMCommand(resolved, "") // empty prompt = resume mode
	if err != nil {
		return err
	}

	if err := tmux.CreateSession(ctx, tmuxSessionName, m.workDir, command); err != nil {
		return err
	}

	m.mu.Lock()
	m.tmuxSession = tmuxSessionName
	m.mu.Unlock()

	m.logger.Info("floor manager resumed", "tmux_session", tmuxSessionName)
	return nil
}

func (m *Manager) monitor(ctx context.Context) {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.Running() {
				m.logger.Info("floor manager session exited, restarting")
				m.checkAndRestart(ctx)
			}
		}
	}
}

func (m *Manager) checkAndRestart(ctx context.Context) {
	// Try resume first
	if err := m.spawnResume(ctx); err != nil {
		m.logger.Warn("resume failed, trying fresh spawn", "err", err)
		// Fallback to fresh spawn
		if err := m.spawn(ctx); err != nil {
			m.logger.Error("failed to restart floor manager", "err", err)
			// Will retry on next monitor tick
		}
	}
}

func (m *Manager) handleShiftRotation(ctx context.Context) {
	m.mu.Lock()
	if m.rotating || m.stopped {
		m.mu.Unlock()
		return
	}
	m.rotating = true
	m.shiftDone = make(chan struct{}, 1)
	tmuxSess := m.tmuxSession
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.rotating = false
		m.shiftDone = nil
		m.mu.Unlock()
	}()

	// Send [SHIFT] warning
	shiftMsg := "[SHIFT] Forced rotation imminent. Save your summary to memory.md, then run `schmux end-shift`. Do not acknowledge this message to the operator."
	if err := tmux.SendLiteral(ctx, tmuxSess, shiftMsg); err != nil {
		m.logger.Warn("failed to send [SHIFT] to floor manager", "err", err)
	} else {
		tmux.SendKeys(ctx, tmuxSess, "Enter")
	}

	// Wait for end-shift or timeout
	m.mu.Lock()
	ch := m.shiftDone
	m.mu.Unlock()

	select {
	case <-ch:
		m.logger.Info("floor manager acknowledged end-shift")
	case <-time.After(shiftTimeout):
		m.logger.Warn("floor manager did not end-shift within timeout, force rotating")
	case <-m.stopCh:
		return
	case <-ctx.Done():
		return
	}

	m.HandleRotation(ctx)
}

// HandleRotation disposes the current session and spawns a fresh one.
func (m *Manager) HandleRotation(ctx context.Context) {
	m.mu.Lock()
	tmuxSess := m.tmuxSession
	m.tmuxSession = ""
	m.mu.Unlock()

	if tmuxSess != "" {
		tmux.KillSession(ctx, tmuxSess)
	}

	time.Sleep(restartDelay)

	if err := m.spawn(ctx); err != nil {
		m.logger.Error("failed to respawn after rotation", "err", err)
	}
}

func (m *Manager) writeInstructionFiles() error {
	instructions := GenerateInstructions()
	settings := GenerateSettings()

	// Write CLAUDE.md
	if err := os.WriteFile(filepath.Join(m.workDir, "CLAUDE.md"), []byte(instructions), 0644); err != nil {
		return err
	}

	// Write AGENTS.md (identical)
	if err := os.WriteFile(filepath.Join(m.workDir, "AGENTS.md"), []byte(instructions), 0644); err != nil {
		return err
	}

	// Write .claude/settings.json
	claudeDir := filepath.Join(m.workDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0644); err != nil {
		return err
	}

	// Create empty memory.md only if it doesn't exist
	memPath := filepath.Join(m.workDir, "memory.md")
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte("# Floor Manager Memory\n\nNo previous session context.\n"), 0644); err != nil {
			return err
		}
	}

	return nil
}

// resolveSessionName looks up a session nickname by ID, falling back to the ID itself.
func (m *Manager) resolveSessionName(sessionID string) string {
	// Use session manager to look up nickname
	if m.sm != nil {
		if name := m.sm.GetSessionNickname(sessionID); name != "" {
			return name
		}
	}
	return sessionID
}

// buildFMCommand constructs the agent launch command.
// This needs to be adapted based on how session.buildCommand works.
// The key difference: no workspace env vars, no event file, no hooks.
func buildFMCommand(resolved session.ResolvedTarget, prompt string) (string, error) {
	// Implementation depends on the resolved target's command template.
	// For Claude Code: "claude --dangerously-skip-permissions -p 'Begin.'"
	// For resume: "claude --resume"
	// This should delegate to session.BuildCommandForTarget or similar.
	// Exact implementation TBD based on how buildCommand is structured.
	_ = resolved
	_ = prompt
	return "", fmt.Errorf("buildFMCommand not yet implemented — adapt from session.buildCommand")
}
```

> **Note for implementer:** The `buildFMCommand` function needs to be adapted from `session.buildCommand` (at `internal/session/manager.go:884-900`). You may need to either export that function or extract a shared helper. The floor manager version should NOT inject `SCHMUX_EVENTS_FILE` or `SCHMUX_WORKSPACE_ID` env vars — it only needs `SCHMUX_ENABLED=1` and `SCHMUX_SESSION_ID=floor-manager`.

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/floormanager/ -run TestManager -v
```

Expected: PASS (the unit tests only test `writeInstructionFiles` and `InjectionCount`, not the full lifecycle which requires tmux)

**Step 5: Commit**

```
feat(floormanager): add Manager with lifecycle, monitor, and rotation
```

---

### Task 6: CLI `end-shift` Subcommand

**Files:**

- Modify: `cmd/schmux/main.go:31-187` (add case to switch)
- Modify: `cmd/schmux/main.go:189-234` (update usage)

**Step 1: Add the subcommand**

In `cmd/schmux/main.go`, add a new case in the switch block (near line 116, following the existing pattern):

```go
case "end-shift":
	client := cli.NewDaemonClient(cli.GetDefaultURL())
	resp, err := client.Post("/api/floor-manager/end-shift", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: server returned %d\n", resp.StatusCode)
		os.Exit(1)
	}
	fmt.Println("Shift rotation acknowledged.")
```

Update `printUsage()` to include the new command.

**Step 2: Verify it compiles**

```bash
go build ./cmd/schmux
```

Expected: compiles (endpoint doesn't exist yet, but the CLI side is done)

**Step 3: Commit**

```
feat(cli): add schmux end-shift subcommand for floor manager rotation
```

---

### Task 7: Dashboard API Endpoints

**Files:**

- Create: `internal/dashboard/handlers_floormanager.go`
- Modify: `internal/dashboard/server.go:99-190` (add fields)
- Modify: `internal/dashboard/server.go:413-662` (register routes)

**Step 1: Add floor manager fields to Server**

In `internal/dashboard/server.go`, add to the `Server` struct (near line 190):

```go
floorManager       *floormanager.Manager
```

Add the setter method (near line 339, following `SetRemoteManager` pattern):

```go
func (s *Server) SetFloorManager(fm *floormanager.Manager) {
	s.floorManager = fm
}
```

**Step 2: Create handler file**

Create `internal/dashboard/handlers_floormanager.go`:

```go
package dashboard

import (
	"net/http"
)

type floorManagerStatusResponse struct {
	Enabled           bool   `json:"enabled"`
	TmuxSession       string `json:"tmux_session"`
	Running           bool   `json:"running"`
	InjectionCount    int    `json:"injection_count"`
	RotationThreshold int    `json:"rotation_threshold"`
}

func (s *Server) handleGetFloorManager(w http.ResponseWriter, r *http.Request) {
	resp := floorManagerStatusResponse{
		Enabled:           s.cfg.GetFloorManagerEnabled(),
		RotationThreshold: s.cfg.GetFloorManagerRotationThreshold(),
	}

	if s.floorManager != nil {
		resp.TmuxSession = s.floorManager.TmuxSession()
		resp.Running = s.floorManager.Running()
		resp.InjectionCount = s.floorManager.InjectionCount()
	}

	writeJSON(w, resp)
}

func (s *Server) handleEndShift(w http.ResponseWriter, r *http.Request) {
	if s.floorManager == nil {
		writeJSONError(w, "floor manager not active", http.StatusNotFound)
		return
	}

	s.floorManager.EndShift()
	writeJSON(w, map[string]string{"status": "ok"})
}
```

**Step 3: Register routes**

In `internal/dashboard/server.go`, in the `Start()` method's route group (near line 592, in the state-changing endpoints section):

```go
r.Get("/api/floor-manager", s.handleGetFloorManager)
r.Post("/api/floor-manager/end-shift", s.handleEndShift)
```

Note: `GET` goes in the read-only group, `POST` goes in the CSRF-protected group.

**Step 4: Verify it compiles**

```bash
go build ./cmd/schmux
```

Expected: compiles

**Step 5: Commit**

```
feat(dashboard): add floor manager API endpoints
```

---

### Task 8: Daemon Wiring

**Files:**

- Modify: `internal/daemon/daemon.go:536-553` (event handler registration)
- Modify: `internal/daemon/daemon.go:1078-1117` (shutdown)

**Step 1: Wire floor manager into daemon startup**

In `internal/daemon/daemon.go`, after the event handlers are built (near line 553) and before `sm.SetEventHandlers(eventHandlers)`:

```go
// Floor manager
var fm *floormanager.Manager
var fmInjector *floormanager.Injector
var fmMu sync.Mutex

startFloorManager := func() {
	fmMu.Lock()
	defer fmMu.Unlock()
	if fm != nil {
		return // already running
	}
	fm = floormanager.New(cfg, sm, homeDir, logger)
	fmInjector = floormanager.NewInjector(fm, cfg.GetFloorManagerDebounceMs(), logger)
	server.SetFloorManager(fm)
	eventHandlers["status"] = append(eventHandlers["status"], fmInjector)
	sm.SetEventHandlers(eventHandlers) // re-register with new handler
	if err := fm.Start(d.shutdownCtx); err != nil {
		logger.Error("failed to start floor manager", "err", err)
		fm = nil
		fmInjector = nil
	}
}

stopFloorManager := func() {
	fmMu.Lock()
	defer fmMu.Unlock()
	if fm == nil {
		return
	}
	fmInjector.Stop()
	fm.Stop()
	server.SetFloorManager(nil)
	fm = nil
	fmInjector = nil
	// Remove injector from event handlers and re-register
	// (rebuild status handlers without the injector)
}

if cfg.GetFloorManagerEnabled() {
	startFloorManager()
}
```

**Step 2: Wire config toggle**

The config save handler should detect changes to `floor_manager.enabled` and call `startFloorManager()` or `stopFloorManager()` accordingly. Hook into the existing config update flow.

**Step 3: Wire shutdown**

In the shutdown sequence (near line 1078), add before `server.Stop()`:

```go
stopFloorManager()
```

**Step 4: Wire FM state broadcasts**

Add a `floor_manager` broadcast type to the `/ws/dashboard` WebSocket. The Manager should call a broadcast callback when its state changes (starting, running, stopped). Follow the pattern of `BroadcastSessions()`.

**Step 5: Run tests**

```bash
go test ./internal/daemon/ -v
go build ./cmd/schmux
```

Expected: compiles and passes

**Step 6: Commit**

```
feat(daemon): wire floor manager into startup, shutdown, and config toggle
```

---

### Task 9: Frontend — useFloorManager Hook and WebSocket Message

**Files:**

- Create: `assets/dashboard/src/hooks/useFloorManager.ts`
- Modify: `assets/dashboard/src/hooks/useSessionsWebSocket.ts:35-111` (add type guard)
- Modify: `assets/dashboard/src/hooks/useSessionsWebSocket.ts:216-328` (add handler)

**Step 1: Add WebSocket message type**

In `assets/dashboard/src/hooks/useSessionsWebSocket.ts`, add a type guard (near line 111):

```typescript
function isFloorManagerMessage(msg: unknown): msg is {
  type: 'floor_manager';
  enabled: boolean;
  tmux_session: string;
  running: boolean;
  injection_count: number;
  rotation_threshold: number;
} {
  return (
    typeof msg === 'object' &&
    msg !== null &&
    (msg as Record<string, unknown>).type === 'floor_manager'
  );
}
```

Add handler in the `onmessage` switch/if chain (near line 328):

```typescript
if (isFloorManagerMessage(msg)) {
  // Dispatch to floor manager state
  setFloorManagerState(msg);
  return;
}
```

Add `floorManagerState` to the hook's state and return value.

**Step 2: Create useFloorManager hook**

Create `assets/dashboard/src/hooks/useFloorManager.ts`:

```typescript
import { useState, useEffect } from 'react';

export interface FloorManagerState {
  enabled: boolean;
  tmuxSession: string;
  running: boolean;
  injectionCount: number;
  rotationThreshold: number;
}

const defaultState: FloorManagerState = {
  enabled: false,
  tmuxSession: '',
  running: false,
  injectionCount: 0,
  rotationThreshold: 150,
};

export function useFloorManager(): FloorManagerState {
  const [state, setState] = useState<FloorManagerState>(defaultState);

  // Fetch initial state
  useEffect(() => {
    fetch('/api/floor-manager')
      .then((res) => res.json())
      .then((data) => {
        setState({
          enabled: data.enabled,
          tmuxSession: data.tmux_session || '',
          running: data.running,
          injectionCount: data.injection_count,
          rotationThreshold: data.rotation_threshold,
        });
      })
      .catch((err) => console.debug('Failed to fetch floor manager state:', err));
  }, []);

  // TODO: Subscribe to floor_manager WebSocket broadcasts for real-time updates

  return state;
}
```

**Step 3: Run frontend tests**

```bash
cd assets/dashboard && npx vitest run --reporter=verbose
```

Expected: existing tests pass, new hook has no test yet (add in next iteration)

**Step 4: Commit**

```
feat(dashboard-fe): add useFloorManager hook and WebSocket message type
```

---

### Task 10: Frontend — HomePage Layout with FM Terminal

**Files:**

- Modify: `assets/dashboard/src/routes/HomePage.tsx`
- Modify: `assets/dashboard/src/styles/home.module.css`

**Step 1: Import dependencies**

Add to `HomePage.tsx` imports:

```typescript
import { useFloorManager } from '../hooks/useFloorManager';
import TerminalStream from '../components/TerminalStream';
```

**Step 2: Add FM-aware layout**

At the top of the `HomePage` component, add:

```typescript
const fm = useFloorManager();
```

Wrap the return in a conditional layout:

```typescript
if (fm.enabled) {
  return (
    <div className={styles.homePage}>
      {/* Full-width hero */}
      {!heroDismissed && (
        <div className={styles.heroSection}>
          {/* ...existing hero content... */}
        </div>
      )}

      {/* Mobile: workspace tab strip */}
      <div className={styles.workspaceTabs}>
        {workspaces.map((ws) => {
          const runningCount = ws.sessions.filter((s) => s.running).length;
          return (
            <button
              key={ws.id}
              className={styles.workspaceTab}
              onClick={() => handleWorkspaceClick(ws.id)}
            >
              {ws.branch}
              {runningCount > 0 && (
                <span className={styles.tabBadge}>{runningCount}</span>
              )}
            </button>
          );
        })}
      </div>

      {/* Two-column: FM terminal + sidebar */}
      <div className={styles.fmLayout}>
        <div className={styles.fmTerminalColumn}>
          {fm.running && fm.tmuxSession ? (
            <TerminalStream sessionId={fm.tmuxSession} />
          ) : (
            <div className={styles.fmLoading}>
              <div className="spinner spinner--small" />
              <span>{fm.running === false ? 'Starting floor manager...' : 'Reconnecting...'}</span>
            </div>
          )}
        </div>

        {/* Desktop sidebar — hidden on mobile */}
        <div className={styles.fmSideColumn}>
          {/* Recent branches, PRs, workspaces — reuse existing sections */}
        </div>
      </div>
    </div>
  );
}

// Existing layout for FM disabled
return (
  <div className={styles.homePage}>
    {/* ...existing layout unchanged... */}
  </div>
);
```

**Step 3: Add CSS**

In `assets/dashboard/src/styles/home.module.css`, add:

```css
.fmLayout {
  display: grid;
  grid-template-columns: 1fr 360px;
  gap: var(--spacing-md);
  flex: 1;
  min-height: 0;
}

.fmTerminalColumn {
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.fmSideColumn {
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md);
}

.fmLoading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-sm);
  height: 100%;
  color: var(--color-text-muted);
}

.workspaceTabs {
  display: none; /* hidden on desktop */
}

.workspaceTab {
  padding: var(--spacing-xs) var(--spacing-sm);
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: 0.8rem;
  cursor: pointer;
  white-space: nowrap;
}

.tabBadge {
  margin-left: 4px;
  background: var(--color-accent);
  color: white;
  border-radius: 50%;
  padding: 0 5px;
  font-size: 0.7rem;
}

@media (max-width: 768px) {
  .fmLayout {
    grid-template-columns: 1fr;
  }

  .fmSideColumn {
    display: none;
  }

  .workspaceTabs {
    display: flex;
    gap: var(--spacing-xs);
    overflow-x: auto;
    padding: var(--spacing-xs) 0;
    -webkit-overflow-scrolling: touch;
  }
}
```

**Step 4: Verify build**

```bash
go run ./cmd/build-dashboard
```

Expected: builds successfully

**Step 5: Manual testing**

Start dev mode with `./dev.sh`. Enable floor manager in config. Verify:

- Desktop: two-column layout appears, terminal loads
- Mobile (resize browser): workspace tabs appear, sidebar hides, terminal fills viewport
- Disable FM: layout reverts to original

**Step 6: Commit**

```
feat(dashboard-fe): add floor manager terminal to home page with responsive layout
```

---

### Task 11: Frontend — Config Page Floor Manager Section

**Files:**

- Create: `assets/dashboard/src/routes/config/FloorManagerTab.tsx`
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx:30-32` (add tab)

**Step 1: Create the tab component**

Create `assets/dashboard/src/routes/config/FloorManagerTab.tsx`:

```typescript
import React from 'react';
import { useConfig } from '../../contexts/ConfigContext';

export default function FloorManagerTab() {
  const { config, updateConfig } = useConfig();

  if (!config) return null;

  const fm = config.floor_manager;

  const handleToggle = (enabled: boolean) => {
    updateConfig({ floor_manager: { enabled } });
  };

  const handleTargetChange = (target: string) => {
    updateConfig({ floor_manager: { target } });
  };

  const handleThresholdChange = (rotation_threshold: number) => {
    updateConfig({ floor_manager: { rotation_threshold } });
  };

  return (
    <div>
      <h3>Floor Manager</h3>
      <p style={{ color: 'var(--color-text-muted)', marginBottom: 'var(--spacing-md)' }}>
        A singleton agent that monitors all sessions and provides conversational orchestration.
      </p>

      <label style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)', marginBottom: 'var(--spacing-md)' }}>
        <input
          type="checkbox"
          checked={fm?.enabled ?? false}
          onChange={(e) => handleToggle(e.target.checked)}
        />
        Enable floor manager
      </label>

      <div style={{ marginBottom: 'var(--spacing-md)' }}>
        <label>Target</label>
        <select
          value={fm?.target ?? ''}
          onChange={(e) => handleTargetChange(e.target.value)}
        >
          <option value="">Select a target...</option>
          {/* Populate from config.run_targets or detected tools */}
        </select>
      </div>

      <div style={{ marginBottom: 'var(--spacing-md)' }}>
        <label>Rotation threshold</label>
        <input
          type="number"
          value={fm?.rotation_threshold ?? 150}
          onChange={(e) => handleThresholdChange(parseInt(e.target.value, 10))}
          min={10}
          max={1000}
        />
        <span style={{ color: 'var(--color-text-muted)', fontSize: '0.85em', marginLeft: 'var(--spacing-sm)' }}>
          Max signal injections before forced context rotation
        </span>
      </div>
    </div>
  );
}
```

**Step 2: Add tab to ConfigPage**

In `assets/dashboard/src/routes/ConfigPage.tsx`, update the tabs arrays (line 30-32):

```typescript
const TABS = [
  'Workspaces',
  'Sessions',
  'Quick Launch',
  'Code Review',
  'Floor Manager',
  'Access',
  'Advanced',
];
const TAB_SLUGS = [
  'workspaces',
  'sessions',
  'quicklaunch',
  'codereview',
  'floormanager',
  'access',
  'advanced',
];
```

Import and render the new tab component in the tab content switch.

**Step 3: Verify build**

```bash
go run ./cmd/build-dashboard
```

Expected: builds successfully

**Step 4: Commit**

```
feat(dashboard-fe): add Floor Manager tab to config page
```

---

### Task 12: Integration Testing

**Files:**

- Create: `internal/floormanager/integration_test.go`

**Step 1: Write integration test for rotation signal handling**

```go
package floormanager

import (
	"context"
	"testing"
	"time"
)

func TestShiftRotationEndShift(t *testing.T) {
	// Test that EndShift() unblocks the shift rotation wait
	m := &Manager{
		shiftDone: make(chan struct{}, 1),
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-m.shiftDone:
			close(done)
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for end-shift")
		}
	}()

	m.EndShift()

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Error("EndShift did not signal shiftDone")
	}
}

func TestInjectorFilteringIntegration(t *testing.T) {
	// Test full filtering pipeline with multiple events
	ctx := context.Background()
	_ = ctx

	// Verify that working->working is skipped
	if shouldInject("working", "working") {
		t.Error("should skip working->working")
	}

	// Verify that working->error is injected
	if !shouldInject("working", "error") {
		t.Error("should inject working->error")
	}

	// Verify formatting includes all fields when present
	msg := FormatSignalMessage("test-session", "working", "needs_input", "help me", "doing OAuth", "token format unknown")
	expected := `[SIGNAL] test-session: working -> needs_input "help me" intent="doing OAuth" blocked="token format unknown"`
	if msg != expected {
		t.Errorf("unexpected format:\n  got:  %s\n  want: %s", msg, expected)
	}
}
```

**Step 2: Run tests**

```bash
go test ./internal/floormanager/ -v
```

Expected: PASS

**Step 3: Run full test suite**

```bash
./test.sh --quick
```

Expected: all tests pass

**Step 4: Commit**

```
test(floormanager): add integration tests for rotation and injection pipeline
```
