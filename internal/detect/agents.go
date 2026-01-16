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

// Tool represents a detected AI coding tool.
type Tool struct {
	Name    string
	Command string
	Source  string // detection source
	Agentic bool
}

// ToolDetector defines the interface for detecting AI coding tools.
// Each detector knows the specific ways its tool might be installed.
type ToolDetector interface {
	// Detect attempts to find the tool and returns its info if found.
	// Returns (tool, true) if found, (Tool{}, false) otherwise.
	Detect(ctx context.Context) (Tool, bool)

	// Name returns the tool name for logging/reporting.
	Name() string
}

// DetectAvailableTools runs all registered detectors concurrently and returns available tools.
// All detectors run in parallel with a shared timeout.
// Always logs progress using log.Printf; if printProgress is true, also prints to stdout.
func DetectAvailableTools(printProgress bool) []Tool {
	log.Printf("[detect] Starting tool detection...")
	detectors := []ToolDetector{
		&claudeDetector{},
		&codexDetector{},
		&geminiDetector{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	type result struct {
		tool Tool
		found bool
		name  string // detector name for not-found message
	}
	results := make(chan result, len(detectors))

	var wg sync.WaitGroup
	for _, detector := range detectors {
		wg.Add(1)
		go func(d ToolDetector) {
			defer wg.Done()
			tool, found := d.Detect(ctx)
			results <- result{tool, found, d.Name()}
		}(detector)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var tools []Tool
	for r := range results {
		if r.found {
			// Detector already logged the specifics
			if printProgress {
				fmt.Printf("  Detecting %s... found (command: %s)\n", r.tool.Name, r.tool.Command)
			}
			tools = append(tools, r.tool)
		} else {
			log.Printf("[detect] %s not found (tried all detection methods)", r.name)
			if printProgress {
				fmt.Printf("  Detecting %s... not found\n", r.name)
			}
		}
	}

	log.Printf("[detect] Detection complete: found %d tool(s)", len(tools))
	return tools
}

// DetectAndPrint runs detection and prints progress messages to stdout.
// Returns the detected tools for use in config.
func DetectAndPrint() []Tool {
	return DetectAvailableTools(true)
}

// DetectAvailableToolsContext runs all registered detectors concurrently with the given context.
// Returns available tools or an error if context is canceled.
func DetectAvailableToolsContext(ctx context.Context, printProgress bool) ([]Tool, error) {
	log.Printf("[detect] Starting tool detection...")
	detectors := []ToolDetector{
		&claudeDetector{},
		&codexDetector{},
		&geminiDetector{},
	}

	type result struct {
		tool Tool
		found bool
		name  string // detector name for not-found message
	}
	results := make(chan result, len(detectors))

	var wg sync.WaitGroup
	for _, detector := range detectors {
		wg.Add(1)
		go func(d ToolDetector) {
			defer wg.Done()
			tool, found := d.Detect(ctx)
			results <- result{tool, found, d.Name()}
		}(detector)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var tools []Tool
	for r := range results {
		if r.found {
			// Detector already logged the specifics
			if printProgress {
				fmt.Printf("  Detecting %s... found (command: %s)\n", r.tool.Name, r.tool.Command)
			}
			tools = append(tools, r.tool)
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

	log.Printf("[detect] Detection complete: found %d tool(s)", len(tools))
	return tools, nil
}

// FindDetectedTool finds a detected tool by name.
func FindDetectedTool(ctx context.Context, name string) (Tool, bool, error) {
	tools, err := DetectAvailableToolsContext(ctx, false)
	if err != nil {
		return Tool{}, false, err
	}
	tool, found := FindToolInList(tools, name)
	return tool, found, nil
}

// FindToolInList finds a detected tool by name in a list.
func FindToolInList(tools []Tool, name string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
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

func (d *claudeDetector) Detect(ctx context.Context) (Tool, bool) {
	// Method 1: Try claude command in PATH
	if commandExists("claude") {
		if tryCommand(ctx, "claude", "-v") {
			log.Printf("[detect] claude: found via PATH (command: claude)")
			return Tool{Name: "claude", Command: "claude", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Check native install location (standard)
	if fileExists("~/.local/bin/claude") {
		cmd := filepath.Join(homeDirOrTilde(), ".local", "bin", "claude")
		if tryCommand(ctx, cmd, "-v") {
			log.Printf("[detect] claude: found via native install (command: %s)", cmd)
			return Tool{Name: "claude", Command: cmd, Source: "native install (~/.local/bin/claude)", Agentic: true}, true
		}
	}

	// Method 3: Check alternative native install location
	if fileExists("~/.claude/local/claude") {
		cmd := filepath.Join(homeDirOrTilde(), ".claude", "local", "claude")
		if tryCommand(ctx, cmd, "-v") {
			log.Printf("[detect] claude: found via alternative native install (command: %s)", cmd)
			return Tool{Name: "claude", Command: cmd, Source: "native install (~/.claude/local/claude)", Agentic: true}, true
		}
	}

	// Method 4: Check Homebrew cask
	if homebrewCaskInstalled(ctx, "claude-code") {
		log.Printf("[detect] claude: found via Homebrew cask (command: claude)")
		return Tool{Name: "claude", Command: "claude", Source: "Homebrew cask claude-code", Agentic: true}, true
	}

	// Method 5: Check npm global
	if npmGlobalInstalled(ctx, "@anthropic-ai/claude-code") {
		log.Printf("[detect] claude: found via npm global package @anthropic-ai/claude-code (command: claude)")
		return Tool{Name: "claude", Command: "claude", Source: "npm global package @anthropic-ai/claude-code", Agentic: true}, true
	}

	return Tool{}, false
}

// ===== Codex Detector =====

type codexDetector struct{}

func (d *codexDetector) Name() string { return "codex" }

func (d *codexDetector) Detect(ctx context.Context) (Tool, bool) {
	// Method 1: Try codex command in PATH
	if commandExists("codex") {
		if tryCommand(ctx, "codex", "-V") {
			log.Printf("[detect] codex: found via PATH (command: codex)")
			return Tool{Name: "codex", Command: "codex", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Check npm global (primary installation method)
	if npmGlobalInstalled(ctx, "@openai/codex") {
		log.Printf("[detect] codex: found via npm global package @openai/codex (command: codex)")
		return Tool{Name: "codex", Command: "codex", Source: "npm global package @openai/codex", Agentic: true}, true
	}

	// Method 3: Check Homebrew formula (if available)
	if homebrewFormulaInstalled(ctx, "codex") {
		log.Printf("[detect] codex: found via Homebrew formula (command: codex)")
		return Tool{Name: "codex", Command: "codex", Source: "Homebrew formula codex", Agentic: true}, true
	}

	return Tool{}, false
}

// ===== Gemini Detector =====

type geminiDetector struct{}

func (d *geminiDetector) Name() string { return "gemini" }

func (d *geminiDetector) Detect(ctx context.Context) (Tool, bool) {
	// Method 1: Try gemini command in PATH
	if commandExists("gemini") {
		if tryCommand(ctx, "gemini", "-v") {
			log.Printf("[detect] gemini: found via PATH (command: gemini)")
			return Tool{Name: "gemini", Command: "gemini -i", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Check Homebrew formula (common installation method)
	if homebrewFormulaInstalled(ctx, "gemini-cli") {
		log.Printf("[detect] gemini: found via Homebrew formula gemini-cli (command: gemini)")
		return Tool{Name: "gemini", Command: "gemini -i", Source: "Homebrew formula gemini-cli", Agentic: true}, true
	}

	// Method 3: Check npm global
	if npmGlobalInstalled(ctx, "@google/gemini-cli") {
		log.Printf("[detect] gemini: found via npm global package @google/gemini-cli (command: gemini)")
		return Tool{Name: "gemini", Command: "gemini -i", Source: "npm global package @google/gemini-cli", Agentic: true}, true
	}

	return Tool{}, false
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
