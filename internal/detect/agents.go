package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	// envOnce ensures osEnv is initialized exactly once
	envOnce sync.Once

	// osEnv provides access to environment variables.
	// Uses os.Getenv after initialization.
	osEnv = func(key string) string {
		return "" // placeholder, initialized in init()
	}

	// userHomeDir provides access to os.UserHomeDir.
	// Uses os.UserHomeDir after initialization.
	userHomeDir = func() (string, error) {
		return "", nil // placeholder, initialized in init()
	}
)

// Agent represents a detected AI agent tool.
type Agent struct {
	Name    string
	Command string
	Source  string // detection source
	Agentic bool
}

// AgentDetector defines the interface for detecting AI agent tools.
// Each detector knows the specific ways its agent might be installed.
type AgentDetector interface {
	// Detect attempts to find the agent and returns its info if found.
	// Returns (agent, true) if found, (Agent{}, false) otherwise.
	Detect(ctx context.Context) (Agent, bool)

	// Name returns the agent name for logging/reporting.
	Name() string
}

// DetectAvailableAgents runs all registered detectors concurrently and returns available agents.
// All detectors run in parallel with a shared timeout.
// Always logs progress using log.Printf; if printProgress is true, also prints to stdout.
func DetectAvailableAgents(printProgress bool) []Agent {
	log.Printf("[detect] Starting agent detection...")
	detectors := []AgentDetector{
		&claudeDetector{},
		&codexDetector{},
		&geminiDetector{},
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
		go func(d AgentDetector) {
			defer wg.Done()
			agent, found := d.Detect(ctx)
			results <- result{agent, found, d.Name()}
		}(detector)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var agents []Agent
	for r := range results {
		if r.found {
			// Detector already logged the specifics
			if printProgress {
				fmt.Printf("  Detecting %s... found (command: %s)\n", r.agent.Name, r.agent.Command)
			}
			agents = append(agents, r.agent)
		} else {
			log.Printf("[detect] %s not found (tried all detection methods)", r.name)
			if printProgress {
				fmt.Printf("  Detecting %s... not found\n", r.name)
			}
		}
	}

	log.Printf("[detect] Detection complete: found %d agent(s)", len(agents))
	return agents
}

// DetectAndPrint runs detection and prints progress messages to stdout.
// Returns the detected agents for use in config.
func DetectAndPrint() []Agent {
	return DetectAvailableAgents(true)
}

// DetectAvailableAgentsContext runs all registered detectors concurrently with the given context.
// Returns available agents or an error if context is canceled.
func DetectAvailableAgentsContext(ctx context.Context, printProgress bool) ([]Agent, error) {
	log.Printf("[detect] Starting agent detection...")
	detectors := []AgentDetector{
		&claudeDetector{},
		&codexDetector{},
		&geminiDetector{},
	}

	type result struct {
		agent Agent
		found bool
		name  string // detector name for not-found message
	}
	results := make(chan result, len(detectors))

	var wg sync.WaitGroup
	for _, detector := range detectors {
		wg.Add(1)
		go func(d AgentDetector) {
			defer wg.Done()
			agent, found := d.Detect(ctx)
			results <- result{agent, found, d.Name()}
		}(detector)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var agents []Agent
	for r := range results {
		if r.found {
			// Detector already logged the specifics
			if printProgress {
				fmt.Printf("  Detecting %s... found (command: %s)\n", r.agent.Name, r.agent.Command)
			}
			agents = append(agents, r.agent)
		} else {
			log.Printf("[detect] %s not found (tried all detection methods)", r.name)
			if printProgress {
				fmt.Printf("  Detecting %s... not found\n", r.name)
			}
		}
	}

	// Check if context was canceled
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	log.Printf("[detect] Detection complete: found %d agent(s)", len(agents))
	return agents, nil
}

// FindDetectedAgent finds a detected agent by name.
func FindDetectedAgent(ctx context.Context, name string) (Agent, bool, error) {
	agents, err := DetectAvailableAgentsContext(ctx, false)
	if err != nil {
		return Agent{}, false, err
	}
	agent, found := FindAgentInList(agents, name)
	return agent, found, nil
}

// FindAgentInList finds a detected agent by name in a list.
func FindAgentInList(agents []Agent, name string) (Agent, bool) {
	for _, agent := range agents {
		if agent.Name == name {
			return agent, true
		}
	}
	return Agent{}, false
}

var detectTimeout = 3 * time.Second // 3 seconds (increased for multiple detection methods)

// ===== Shared Detection Utilities =====

// tryCommand checks if a command exists and can run with the given version flag.
// Returns true if the command runs successfully (exit code 0).
func tryCommand(ctx context.Context, command, versionFlag string) bool {
	cmd := exec.CommandContext(ctx, command, versionFlag)
	return cmd.Run() == nil
}

// tryCommandArgs checks if a command runs successfully with multiple arguments.
// Returns true if the command runs successfully (exit code 0).
func tryCommandArgs(ctx context.Context, command string, args ...string) bool {
	cmd := exec.CommandContext(ctx, command, args...)
	return cmd.Run() == nil
}

// commandExists checks if a command is available in PATH.
func commandExists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

// fileExists checks if a file exists at the given path.
// Expands ~ to home directory if present.
func fileExists(path string) bool {
	expanded, err := expandHome(path)
	if err != nil {
		return false
	}
	matches, err := expandHomeGlob(expanded)
	if err != nil {
		return false
	}
	return len(matches) > 0
}

// expandHome expands ~ to the user's home directory.
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := homeDir()
	if err != nil {
		return path, err
	}

	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, path[1:]), nil
}

// expandHomeGlob is like filepath.Glob but handles ~ expansion first.
func expandHomeGlob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// homeDir returns the user's home directory.
func homeDir() (string, error) {
	// First check HOME
	if home := osEnv("HOME"); home != "" {
		return home, nil
	}
	// Windows fallback
	if home := osEnv("USERPROFILE"); home != "" {
		return home, nil
	}
	// Try os.UserHomeDir as fallback
	return userHomeDir()
}

// homebrewInstalled checks if Homebrew is available on the system.
func homebrewInstalled() bool {
	return commandExists("brew")
}

// homebrewCaskInstalled checks if a Homebrew cask is installed.
func homebrewCaskInstalled(ctx context.Context, cask string) bool {
	if !homebrewInstalled() {
		return false
	}
	cmd := exec.CommandContext(ctx, "brew", "list", "--cask")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == cask {
			return true
		}
	}
	return false
}

// homebrewFormulaInstalled checks if a Homebrew formula is installed.
func homebrewFormulaInstalled(ctx context.Context, formula string) bool {
	if !homebrewInstalled() {
		return false
	}
	cmd := exec.CommandContext(ctx, "brew", "list", "--formula")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == formula {
			return true
		}
	}
	return false
}

// npmInstalled checks if npm is available on the system.
func npmInstalled() bool {
	return commandExists("npm")
}

// npmGlobalInstalled checks if an npm package is installed globally.
func npmGlobalInstalled(ctx context.Context, pkg string) bool {
	if !npmInstalled() {
		return false
	}
	cmd := exec.CommandContext(ctx, "npm", "list", "-g", "--depth=0", "--json")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// Parse JSON output to check for the package
	// npm list --json returns a top-level "dependencies" object
	type npmList struct {
		Dependencies map[string]struct{} `json:"dependencies"`
	}
	var list npmList
	if err := json.Unmarshal(output, &list); err != nil {
		// Fallback to simple string match if JSON parsing fails
		return strings.Contains(string(output), `"`+pkg+`"`)
	}
	_, found := list.Dependencies[pkg]
	return found
}

// ===== Claude Detector =====

type claudeDetector struct{}

func (d *claudeDetector) Name() string { return "claude" }

func (d *claudeDetector) Detect(ctx context.Context) (Agent, bool) {
	// Method 1: Try claude command in PATH
	if commandExists("claude") {
		if tryCommand(ctx, "claude", "-v") {
			log.Printf("[detect] claude: found via PATH (command: claude)")
			return Agent{Name: "claude", Command: "claude", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Check native install location (standard)
	if fileExists("~/.local/bin/claude") {
		cmd := filepath.Join(homeDirOrTilde(), ".local", "bin", "claude")
		if tryCommand(ctx, cmd, "-v") {
			log.Printf("[detect] claude: found via native install (command: %s)", cmd)
			return Agent{Name: "claude", Command: cmd, Source: "native install (~/.local/bin/claude)", Agentic: true}, true
		}
	}

	// Method 3: Check alternative native install location
	if fileExists("~/.claude/local/claude") {
		cmd := filepath.Join(homeDirOrTilde(), ".claude", "local", "claude")
		if tryCommand(ctx, cmd, "-v") {
			log.Printf("[detect] claude: found via alternative native install (command: %s)", cmd)
			return Agent{Name: "claude", Command: cmd, Source: "native install (~/.claude/local/claude)", Agentic: true}, true
		}
	}

	// Method 4: Check Homebrew cask
	if homebrewCaskInstalled(ctx, "claude-code") {
		log.Printf("[detect] claude: found via Homebrew cask (command: claude)")
		return Agent{Name: "claude", Command: "claude", Source: "Homebrew cask claude-code", Agentic: true}, true
	}

	// Method 5: Check npm global
	if npmGlobalInstalled(ctx, "@anthropic-ai/claude-code") {
		log.Printf("[detect] claude: found via npm global package @anthropic-ai/claude-code (command: claude)")
		return Agent{Name: "claude", Command: "claude", Source: "npm global package @anthropic-ai/claude-code", Agentic: true}, true
	}

	return Agent{}, false
}

// ===== Codex Detector =====

type codexDetector struct{}

func (d *codexDetector) Name() string { return "codex" }

func (d *codexDetector) Detect(ctx context.Context) (Agent, bool) {
	// Method 1: Try codex command in PATH
	if commandExists("codex") {
		if tryCommand(ctx, "codex", "-V") {
			log.Printf("[detect] codex: found via PATH (command: codex)")
			return Agent{Name: "codex", Command: "codex", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Check npm global (primary installation method)
	if npmGlobalInstalled(ctx, "@openai/codex") {
		log.Printf("[detect] codex: found via npm global package @openai/codex (command: codex)")
		return Agent{Name: "codex", Command: "codex", Source: "npm global package @openai/codex", Agentic: true}, true
	}

	// Method 3: Check Homebrew formula (if available)
	if homebrewFormulaInstalled(ctx, "codex") {
		log.Printf("[detect] codex: found via Homebrew formula (command: codex)")
		return Agent{Name: "codex", Command: "codex", Source: "Homebrew formula codex", Agentic: true}, true
	}

	return Agent{}, false
}

// ===== Gemini Detector =====

type geminiDetector struct{}

func (d *geminiDetector) Name() string { return "gemini" }

func (d *geminiDetector) Detect(ctx context.Context) (Agent, bool) {
	// Method 1: Try gemini command in PATH
	if commandExists("gemini") {
		if tryCommand(ctx, "gemini", "-v") {
			log.Printf("[detect] gemini: found via PATH (command: gemini)")
			return Agent{Name: "gemini", Command: "gemini -i", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Check Homebrew formula (common installation method)
	if homebrewFormulaInstalled(ctx, "gemini-cli") {
		log.Printf("[detect] gemini: found via Homebrew formula gemini-cli (command: gemini)")
		return Agent{Name: "gemini", Command: "gemini -i", Source: "Homebrew formula gemini-cli", Agentic: true}, true
	}

	// Method 3: Check npm global
	if npmGlobalInstalled(ctx, "@google/gemini-cli") {
		log.Printf("[detect] gemini: found via npm global package @google/gemini-cli (command: gemini)")
		return Agent{Name: "gemini", Command: "gemini -i", Source: "npm global package @google/gemini-cli", Agentic: true}, true
	}

	return Agent{}, false
}

// ===== Helper for home directory =====

// homeDirOrTilde returns the home directory or "~" as fallback.
// Used when we need a string representation without error handling.
func homeDirOrTilde() string {
	if home := osEnv("HOME"); home != "" {
		return home
	}
	if home := osEnv("USERPROFILE"); home != "" {
		return home
	}
	return "~"
}

func init() {
	// Set up actual os.Getenv and os.UserHomeDir at runtime.
	// Uses sync.Once to ensure thread-safe initialization.
	envOnce.Do(func() {
		osEnv = os.Getenv
		userHomeDir = os.UserHomeDir
	})
}
