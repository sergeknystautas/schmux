package detect

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Agent represents a detected AI agent (matches config.Agent structure).
type Agent struct {
	Name    string
	Command string
	Agentic bool
}

// agentDetector defines a function that detects a specific AI agent tool.
type agentDetector struct {
	name       string
	command    string
	versionArg string
}

// DetectAvailableAgents runs agent detection concurrently and returns available agents.
// All detectors run in parallel with a shared timeout.
func DetectAvailableAgents() []Agent {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	detectors := []agentDetector{
		{name: "claude", command: "claude", versionArg: "-v"},
		{name: "gemini", command: "gemini", versionArg: "-v"},
		{name: "codex", command: "codex", versionArg: "-V"},
	}

	var wg sync.WaitGroup
	results := make(chan Agent, len(detectors))

	for _, detector := range detectors {
		wg.Add(1)
		go func(d agentDetector) {
			defer wg.Done()
			if agent, found := detectAgent(ctx, d); found {
				results <- agent
			}
		}(detector)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var agents []Agent
	for agent := range results {
		agents = append(agents, agent)
	}

	return agents
}

// detectAgent checks if a specific agent tool is available by running its version command.
func detectAgent(ctx context.Context, d agentDetector) (Agent, bool) {
	// First check if command exists in PATH
	if _, err := exec.LookPath(d.command); err != nil {
		return Agent{}, false
	}

	// Run version command with timeout
	cmd := exec.CommandContext(ctx, d.command, d.versionArg)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Agent{}, false
	}

	// Verify output looks like a version string (has common version patterns)
	outputStr := strings.ToLower(string(output))
	if !looksLikeVersion(outputStr) {
		return Agent{}, false
	}

	// Default agentic to true (these are AI agents that take prompts)
	return Agent{
		Name:    d.name,
		Command: d.command,
		Agentic: true,
	}, true
}

// looksLikeVersion checks if output contains version-like patterns.
func looksLikeVersion(output string) bool {
	// Check for common version indicators
	versionPatterns := []string{
		"version",
		"v1",
		"v2",
		"v3",
		"v4",
		"0.",
		"1.",
		"2.",
		"3.",
		"4.",
	}

	lowerOutput := strings.ToLower(output)
	for _, pattern := range versionPatterns {
		if strings.Contains(lowerOutput, pattern) {
			return true
		}
	}

	return false
}

// DetectAndPrint runs detection and prints progress messages to stdout.
// Returns the detected agents for use in config.
func DetectAndPrint() []Agent {
	detectors := []agentDetector{
		{name: "claude", command: "claude", versionArg: "-v"},
		{name: "gemini", command: "gemini", versionArg: "-v"},
		{name: "codex", command: "codex", versionArg: "-V"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	var wg sync.WaitGroup
	type result struct {
		agent Agent
		found bool
		name  string // detector name for not-found message
	}
	results := make(chan result, len(detectors))

	// Run detectors concurrently
	for _, detector := range detectors {
		wg.Add(1)
		go func(d agentDetector) {
			defer wg.Done()
			agent, found := detectAgent(ctx, d)
			results <- result{agent, found, d.name}
		}(detector)
	}

	// Collect results as they complete
	go func() {
		wg.Wait()
		close(results)
	}()

	var agents []Agent
	for r := range results {
		if r.found {
			fmt.Printf("  Detecting %s... found\n", r.agent.Name)
			agents = append(agents, r.agent)
		} else {
			fmt.Printf("  Detecting %s... not found\n", r.name)
		}
	}

	return agents
}

var detectTimeout = 2 * time.Second // 2 seconds (var for testability)
