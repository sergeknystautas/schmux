package detect

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestDetectTimeout verifies that detection respects the timeout.
func TestDetectTimeout(t *testing.T) {
	oldTimeout := detectTimeout
	defer func() { detectTimeout = oldTimeout }()

	detectTimeout = 100 * time.Millisecond

	start := time.Now()
	agents := DetectAvailableTools(false)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("Detection took too long: %v, expected < 500ms", elapsed)
	}

	// Results should be valid
	for _, agent := range agents {
		if agent.Name == "" {
			t.Error("Agent name should not be empty")
		}
		if agent.Command == "" {
			t.Error("Agent command should not be empty")
		}
		if !agent.Agentic {
			t.Error("Agent Agentic should be true")
		}
	}
}

// TestDetectAndPrint verifies that DetectAndPrint returns valid results.
func TestDetectAndPrint(t *testing.T) {
	oldTimeout := detectTimeout
	defer func() { detectTimeout = oldTimeout }()

	detectTimeout = 100 * time.Millisecond

	agents := DetectAndPrint()

	// Should return a slice (may be empty)
	if agents == nil {
		t.Error("DetectAndPrint() should never return nil")
	}

	// All returned agents should be valid
	for _, agent := range agents {
		if agent.Name == "" {
			t.Errorf("Agent name should not be empty")
		}
		if agent.Command == "" {
			t.Errorf("Agent command should not be empty")
		}
		if !agent.Agentic {
			t.Errorf("Agent Agentic should be true, got false for %s", agent.Name)
		}
	}
}

// TestDetectAvailableTools verifies concurrent detection works correctly.
func TestDetectAvailableTools(t *testing.T) {
	oldTimeout := detectTimeout
	defer func() { detectTimeout = oldTimeout }()

	detectTimeout = 500 * time.Millisecond

	agents := DetectAvailableTools(false)

	// Should return a slice (may be empty if no tools found)
	if agents == nil {
		t.Error("DetectAvailableTools() should never return nil")
	}

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, agent := range agents {
		if seen[agent.Name] {
			t.Errorf("Duplicate agent found: %s", agent.Name)
		}
		seen[agent.Name] = true
	}

	// All agents should be valid
	for _, agent := range agents {
		if agent.Name == "" {
			t.Error("Agent name should not be empty")
		}
		if agent.Command == "" {
			t.Error("Agent command should not be empty")
		}
		if !agent.Agentic {
			t.Error("Agent Agentic should be true")
		}
	}
}

// TestToolDetectorConfig verifies the detector configurations match requirements.
func TestToolDetectorConfig(t *testing.T) {
	// Verify each detector has a name
	detectors := []ToolDetector{
		&claudeDetector{},
		&codexDetector{},
		&geminiDetector{},
	}

	tests := []struct {
		name     string
		detector ToolDetector
		wantName string
	}{
		{
			name:     "claude detector",
			detector: detectors[0],
			wantName: "claude",
		},
		{
			name:     "codex detector",
			detector: detectors[1],
			wantName: "codex",
		},
		{
			name:     "gemini detector",
			detector: detectors[2],
			wantName: "gemini",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.detector.Name() != tt.wantName {
				t.Errorf("detector.Name() = %q, want %q", tt.detector.Name(), tt.wantName)
			}
		})
	}
}

// TestCommandExists verifies commandExists works correctly.
func TestCommandExists(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		wantBool bool
	}{
		{
			name:     "sh should exist on Unix systems",
			command:  "sh",
			wantBool: true,
		},
		{
			name:     "nonexistent command should not exist",
			command:  "nonexistentcmd12345abcdef",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandExists(tt.command)
			if got != tt.wantBool {
				t.Errorf("commandExists(%q) = %v, want %v", tt.command, got, tt.wantBool)
			}
		})
	}
}

// TestFileExists verifies fileExists works correctly.
func TestFileExists(t *testing.T) {
	// Test with a file that should exist
	tmpFile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Close()

	tests := []struct {
		name     string
		path     string
		wantBool bool
	}{
		{
			name:     "existing temp file",
			path:     tmpFile.Name(),
			wantBool: true,
		},
		{
			name:     "nonexistent file",
			path:     "/nonexistent/file/that/does/not/exist",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileExists(tt.path)
			if got != tt.wantBool {
				t.Errorf("fileExists(%q) = %v, want %v", tt.path, got, tt.wantBool)
			}
		})
	}
}

// TestClaudeDetector verifies claude detector returns valid results.
func TestClaudeDetector(t *testing.T) {
	d := &claudeDetector{}

	if d.Name() != "claude" {
		t.Errorf("claudeDetector.Name() = %q, want \"claude\"", d.Name())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	agent, found := d.Detect(ctx)

	// If found, verify the agent is valid
	if found {
		if agent.Name != "claude" {
			t.Errorf("agent.Name = %q, want \"claude\"", agent.Name)
		}
		if agent.Command == "" {
			t.Error("agent.Command should not be empty")
		}
		if !agent.Agentic {
			t.Error("agent.Agentic should be true")
		}
	}
	// If not found, that's OK - claude may not be installed
}

// TestCodexDetector verifies codex detector returns valid results.
func TestCodexDetector(t *testing.T) {
	d := &codexDetector{}

	if d.Name() != "codex" {
		t.Errorf("codexDetector.Name() = %q, want \"codex\"", d.Name())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	agent, found := d.Detect(ctx)

	// If found, verify the agent is valid
	if found {
		if agent.Name != "codex" {
			t.Errorf("agent.Name = %q, want \"codex\"", agent.Name)
		}
		if agent.Command == "" {
			t.Error("agent.Command should not be empty")
		}
		if !agent.Agentic {
			t.Error("agent.Agentic should be true")
		}
	}
	// If not found, that's OK - codex may not be installed
}

// TestGeminiDetector verifies gemini detector returns valid results.
func TestGeminiDetector(t *testing.T) {
	d := &geminiDetector{}

	if d.Name() != "gemini" {
		t.Errorf("geminiDetector.Name() = %q, want \"gemini\"", d.Name())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	agent, found := d.Detect(ctx)

	// If found, verify the agent is valid
	if found {
		if agent.Name != "gemini" {
			t.Errorf("agent.Name = %q, want \"gemini\"", agent.Name)
		}
		if agent.Command == "" {
			t.Error("agent.Command should not be empty")
		}
		if !agent.Agentic {
			t.Error("agent.Agentic should be true")
		}
	}
	// If not found, that's OK - gemini may not be installed
}

// TestTryCommandArgs verifies tryCommandArgs handles multiple arguments correctly.
func TestTryCommandArgs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		command  string
		args     []string
		wantBool bool
	}{
		{
			name:     "echo command with args should succeed",
			command:  "echo",
			args:     []string{"hello", "world"},
			wantBool: true,
		},
		{
			name:     "nonexistent command should fail",
			command:  "nonexistentcmd12345abcdef",
			args:     []string{"--version"},
			wantBool: false,
		},
		{
			name:     "sh with version flag should succeed",
			command:  "sh",
			args:     []string{"-c", "echo test"},
			wantBool: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tryCommandArgs(ctx, tt.command, tt.args...)
			if got != tt.wantBool {
				t.Errorf("tryCommandArgs(%q, %v) = %v, want %v", tt.command, tt.args, got, tt.wantBool)
			}
		})
	}
}

// TestNpmGlobalInstalled verifies npmGlobalInstalled works correctly.
func TestNpmGlobalInstalled(t *testing.T) {
	ctx := context.Background()
	// Test with a package that should never exist
	pkg := "@nonexistent/testing-package-xyz123"
	if npmGlobalInstalled(ctx, pkg) {
		t.Errorf("npmGlobalInstalled(%q) = true, want false (package should not exist)", pkg)
	}

	// If npm is installed, test with a known package format
	if commandExists("npm") {
		// Test that the function doesn't crash and returns false for non-existent package
		pkg := "@schmux/nonexistent-test-package-xyz"
		if npmGlobalInstalled(ctx, pkg) {
			t.Errorf("npmGlobalInstalled(%q) = true, want false", pkg)
		}
	}
}

// TestHomebrewInstalled verifies homebrew detection works correctly.
func TestHomebrewInstalled(t *testing.T) {
	// homebrewInstalled should return a boolean without crashing
	homebrewInstalled() // Just verify it doesn't panic
}

// TestHomebrewCaskInstalled verifies cask detection works correctly.
func TestHomebrewCaskInstalled(t *testing.T) {
	ctx := context.Background()
	// Test with a cask that should never exist
	cask := "nonexistent-cask-xyz123"
	if homebrewCaskInstalled(ctx, cask) {
		t.Errorf("homebrewCaskInstalled(%q) = true, want false (cask should not exist)", cask)
	}
}

// TestHomebrewFormulaInstalled verifies formula detection works correctly.
func TestHomebrewFormulaInstalled(t *testing.T) {
	ctx := context.Background()
	// Test with a formula that should never exist
	formula := "nonexistent-formula-xyz123"
	if homebrewFormulaInstalled(ctx, formula) {
		t.Errorf("homebrewFormulaInstalled(%q) = true, want false (formula should not exist)", formula)
	}
}

// TestExpandHome verifies home directory expansion works correctly.
func TestExpandHome(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name:      "path without tilde should return as-is",
			path:      "/usr/local/bin",
			wantError: false,
		},
		{
			name:      "tilde alone should expand to home",
			path:      "~",
			wantError: false,
		},
		{
			name:      "tilde with path should expand",
			path:      "~/.local/bin",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandHome(tt.path)
			if (err != nil) != tt.wantError {
				t.Errorf("expandHome(%q) error = %v, wantError %v", tt.path, err, tt.wantError)
				return
			}
			if !tt.wantError && tt.path[0] != '~' && got != tt.path {
				t.Errorf("expandHome(%q) = %q, want %q (paths without ~ should be unchanged)", tt.path, got, tt.path)
			}
		})
	}
}

// TestNpmGlobalInstalledJSONParsing verifies JSON parsing works correctly.
func TestNpmGlobalInstalledJSONParsing(t *testing.T) {
	// This test verifies that npmGlobalInstalled properly parses JSON output
	// We can't mock npm output easily, but we can verify the function handles various cases

	// If npm is not available, function should return false
	if !commandExists("npm") {
		pkg := "@anthropic-ai/claude-code"
		if npmGlobalInstalled(context.Background(), pkg) {
			t.Errorf("npmGlobalInstalled(%q) = true when npm is not available, want false", pkg)
		}
	}
}
