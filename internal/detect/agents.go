package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
)

// allDetectors returns ToolDetectors for all registered adapters.
func allDetectors() []ToolDetector {
	all := AllAdapters()
	detectors := make([]ToolDetector, len(all))
	for i, a := range all {
		detectors[i] = a
	}
	return detectors
}

var (
	// pkgLogger is the package-level logger for detect operations.
	// Set via SetLogger from the daemon initialization.
	pkgLogger *log.Logger

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

// SetLogger sets the package-level logger for detect operations.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

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

// DetectAvailableToolsContext runs all registered detectors concurrently with the given context.
// Returns available tools or an error if context is canceled.
func DetectAvailableToolsContext(ctx context.Context, printProgress bool) ([]Tool, error) {
	if pkgLogger != nil {
		pkgLogger.Info("starting tool detection")
	}
	detectors := allDetectors()

	type result struct {
		tool  Tool
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

	tools := []Tool{}
	for r := range results {
		if r.found {
			// Detector already logged the specifics
			if printProgress {
				fmt.Printf("  Detecting %s... found (command: %s)\n", r.tool.Name, r.tool.Command)
			}
			tools = append(tools, r.tool)
		} else {
			if pkgLogger != nil {
				pkgLogger.Info("tool not found", "tool", r.name)
			}
			if printProgress {
				fmt.Printf("  Detecting %s... not found\n", r.name)
			}
		}
	}

	// Check if context was canceled
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if pkgLogger != nil {
		pkgLogger.Info("detection complete", "tools_found", len(tools))
	}
	return tools, nil
}

// ===== Shared Detection Utilities =====

// cleanEnv returns the current environment with variables removed that prevent
// nested execution (e.g. CLAUDECODE causes "cannot be launched inside another
// Claude Code session" errors).
func cleanEnv() []string {
	skip := map[string]bool{"CLAUDECODE": true}
	var env []string
	for _, e := range os.Environ() {
		if k, _, ok := strings.Cut(e, "="); ok && skip[k] {
			continue
		}
		env = append(env, e)
	}
	return env
}

// tryCommand checks if a command exists and can run with the given version flag.
// Returns true if the command runs successfully (exit code 0).
func tryCommand(ctx context.Context, command, versionFlag string) bool {
	cmd := exec.CommandContext(ctx, command, versionFlag)
	cmd.Env = cleanEnv()
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
