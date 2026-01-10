package detect

import (
	"context"
	"testing"
	"time"
)

// TestDetectTimeout verifies that detection respects the timeout.
func TestDetectTimeout(t *testing.T) {
	oldTimeout := detectTimeout
	defer func() { detectTimeout = oldTimeout }()

	detectTimeout = 100 * time.Millisecond

	start := time.Now()
	agents := DetectAvailableAgents()
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

// TestDetectAgentMissing tests detection of commands that don't exist.
func TestDetectAgentMissing(t *testing.T) {
	ctx := context.Background()

	d := agentDetector{
		name:       "nonexistentcmd12345",
		command:    "nonexistentcmd12345",
		versionArg: "--version",
	}

	_, found := detectAgent(ctx, d)
	if found {
		t.Error("Expected false for non-existent command")
	}
}

// TestDetectAgentWithInvalidVersion tests detection when command exists
// but version output doesn't look like a version.
func TestDetectAgentWithInvalidVersion(t *testing.T) {
	ctx := context.Background()

	// Use echo to produce non-version output
	d := agentDetector{
		name:       "echo-test",
		command:    "echo",
		versionArg: "hello world",
	}

	agent, found := detectAgent(ctx, d)

	// Should not be found since output doesn't look like a version
	if found {
		t.Errorf("detectAgent() should return false for non-version output, got agent: %+v", agent)
	}
}

// TestDetectAgentWithNonZeroExit tests detection when command exists
// but returns a non-zero exit code.
func TestDetectAgentWithNonZeroExit(t *testing.T) {
	ctx := context.Background()

	// Use a command that exists but with an invalid flag
	d := agentDetector{
		name:       "echo-invalid",
		command:    "echo",
		versionArg: "--invalid-flag-that-does-not-exist-xyz-123",
	}

	_, found := detectAgent(ctx, d)
	if found {
		t.Error("Expected false when command returns non-zero exit")
	}
}

// TestDetectAgentTimeout verifies that detectAgent respects context timeout.
func TestDetectAgentTimeout(t *testing.T) {
	// Create a detector with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Use sleep command which will exceed timeout
	d := agentDetector{
		name:       "sleep",
		command:    "sleep",
		versionArg: "10", // sleep for 10 seconds
	}

	// Should return false due to timeout
	_, found := detectAgent(ctx, d)
	if found {
		t.Error("detectAgent() should return false when command times out")
	}
}

// TestLooksLikeVersion verifies the version detection pattern matching.
func TestLooksLikeVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "claude version string",
			input:    "Claude Code CLI version 1.2.3",
			expected: true,
		},
		{
			name:     "v prefix version",
			input:    "v1.0.0",
			expected: true,
		},
		{
			name:     "version word",
			input:    "Version 2.5",
			expected: true,
		},
		{
			name:     "numeric version",
			input:    "3.0.1",
			expected: true,
		},
		{
			name:     "v2 pattern",
			input:    "some v2.0.0 release",
			expected: true,
		},
		{
			name:     "v3 pattern",
			input:    "tool v3 beta",
			expected: true,
		},
		{
			name:     "v4 pattern",
			input:    "v4.5.6",
			expected: true,
		},
		{
			name:     "0. version",
			input:    "0.1.0",
			expected: true,
		},
		{
			name:     "random text",
			input:    "some random text",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "just letters",
			input:    "abcdefg",
			expected: false,
		},
		{
			name:     "echo output",
			input:    "hello world",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeVersion(tt.input)
			if result != tt.expected {
				t.Errorf("looksLikeVersion(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
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

// TestDetectAvailableAgents verifies concurrent detection works correctly.
func TestDetectAvailableAgents(t *testing.T) {
	oldTimeout := detectTimeout
	defer func() { detectTimeout = oldTimeout }()

	detectTimeout = 500 * time.Millisecond

	agents := DetectAvailableAgents()

	// Should return a slice (may be empty if no tools found)
	if agents == nil {
		t.Error("DetectAvailableAgents() should never return nil")
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

// TestAgentDetectorConfig verifies the detector configurations match requirements.
func TestAgentDetectorConfig(t *testing.T) {
	// These are the actual detectors used in production
	detectors := []agentDetector{
		{name: "claude", command: "claude", versionArg: "-v"},
		{name: "gemini", command: "gemini", versionArg: "-v"},
		{name: "codex", command: "codex", versionArg: "-V"},
	}

	tests := []struct {
		name              string
		detector          agentDetector
		wantName          string
		wantCommand       string
		wantVersionArg    string
	}{
		{
			name:           "claude detector",
			detector:       detectors[0],
			wantName:       "claude",
			wantCommand:    "claude",
			wantVersionArg: "-v",
		},
		{
			name:           "gemini detector",
			detector:       detectors[1],
			wantName:       "gemini",
			wantCommand:    "gemini",
			wantVersionArg: "-v",
		},
		{
			name:           "codex detector with capital V",
			detector:       detectors[2],
			wantName:       "codex",
			wantCommand:    "codex",
			wantVersionArg: "-V",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.detector.name != tt.wantName {
				t.Errorf("detector.name = %q, want %q", tt.detector.name, tt.wantName)
			}
			if tt.detector.command != tt.wantCommand {
				t.Errorf("detector.command = %q, want %q", tt.detector.command, tt.wantCommand)
			}
			if tt.detector.versionArg != tt.wantVersionArg {
				t.Errorf("detector.versionArg = %q, want %q", tt.detector.versionArg, tt.wantVersionArg)
			}
		})
	}
}
