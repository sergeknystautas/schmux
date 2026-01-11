package detect

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// commandContext creates a command for direct execution.
// Note: Shell aliases are not detected - add them manually to config.
func commandContext(ctx context.Context, command string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, command, args...)
}

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
// If printProgress is true, prints detection progress to stdout.
func DetectAvailableAgents(printProgress bool) []Agent {
	detectors := []agentDetector{
		{name: "claude", command: "claude", versionArg: "-v"},
		{name: "gemini", command: "gemini", versionArg: "-v"},
		{name: "codex", command: "codex", versionArg: "-V"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	type result struct {
		agent Agent
		found bool
		name  string // detector name for not-found message
	}
	results := make(chan result, len(detectors))

	var wg sync.WaitGroup
	for _, detector := range detectors {
		wg.Add(1)
		go func(d agentDetector) {
			defer wg.Done()
			agent, found := detectAgent(ctx, d)
			results <- result{agent, found, d.name}
		}(detector)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var agents []Agent
	for r := range results {
		if r.found {
			if printProgress {
				fmt.Printf("  Detecting %s... found\n", r.agent.Name)
			}
			agents = append(agents, r.agent)
		} else {
			if printProgress {
				fmt.Printf("  Detecting %s... not found\n", r.name)
			}
		}
	}

	return agents
}

// detectAgent checks if a specific agent tool is available by running its version command.
func detectAgent(ctx context.Context, d agentDetector) (Agent, bool) {
	// Run version command with timeout
	cmd := commandContext(ctx, d.command, d.versionArg)
	if _, err := cmd.CombinedOutput(); err != nil {
		return Agent{}, false
	}

	// Default agentic to true (these are AI agents that take prompts)
	return Agent{
		Name:    d.name,
		Command: d.command,
		Agentic: true,
	}, true
}

// DetectAndPrint runs detection and prints progress messages to stdout.
// Returns the detected agents for use in config.
func DetectAndPrint() []Agent {
	return DetectAvailableAgents(true)
}

var detectTimeout = 2 * time.Second // 2 seconds (var for testability)
