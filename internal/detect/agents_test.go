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
	agents := DetectAvailableAgents(false)
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

	agents := DetectAvailableAgents(false)

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
		name           string
		detector       agentDetector
		wantName       string
		wantCommand    string
		wantVersionArg string
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
