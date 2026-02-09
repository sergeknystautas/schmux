package detect

import (
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// oscEscapeRe matches OSC escape sequences (e.g., iTerm2 shell integration markers).
// Format: ESC ] ... BEL  or  ESC ] ... ESC \
var oscEscapeRe = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// csiEscapeRe matches CSI escape sequences (e.g., cursor positioning, colors).
// Format: ESC [ ... <final byte>
var csiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// VSCodePath holds the resolved path to VS Code and how it was found.
type VSCodePath struct {
	Path   string // The path or command to use
	Source string // How it was found (for logging/debugging)
}

// ResolveVSCodePath attempts to find the VS Code "code" command using multiple methods.
// It tries in order:
// 1. exec.LookPath (fast path for commands in PATH)
// 2. Shell resolution via 'command -v code' (catches shell aliases and functions)
// 3. Well-known installation locations on macOS
//
// Returns the path and source if found, or empty strings if not found.
func ResolveVSCodePath(ctx context.Context) (VSCodePath, bool) {
	// Method 1: Try exec.LookPath (fast path)
	if path, err := exec.LookPath("code"); err == nil {
		return VSCodePath{Path: path, Source: "PATH"}, true
	}

	// Method 2: Try shell resolution via 'command -v code'
	// This catches shell aliases and functions that exec.LookPath cannot see
	if path, found := resolveViaShell(ctx, "code"); found {
		return VSCodePath{Path: path, Source: "shell alias/function"}, true
	}

	// Method 3: Check well-known installation locations
	if path, source, found := checkKnownLocations(); found {
		return VSCodePath{Path: path, Source: source}, true
	}

	return VSCodePath{}, false
}

// resolveViaShell uses the shell's 'command -v' builtin to resolve a command.
// This can find shell aliases and functions that exec.LookPath cannot.
func resolveViaShell(ctx context.Context, cmd string) (string, bool) {
	// Create a context with timeout if none provided
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	// Try using the user's default shell to resolve the command
	// We use 'command -v' which is POSIX-compliant and returns the path
	// Note: We run with -i (interactive) and -l (login) to ensure aliases are loaded
	// from shell startup files like .bashrc, .zshrc, etc.
	shells := []string{"zsh", "bash", "sh"}
	for _, shell := range shells {
		if !commandExists(shell) {
			continue
		}

		// Use -i -c to run in interactive mode (loads aliases)
		// Some shells need -l (login) too for complete initialization
		var shellCmd *exec.Cmd
		switch shell {
		case "zsh":
			// zsh: -i for interactive, -c for command
			shellCmd = exec.CommandContext(ctx, shell, "-i", "-c", "command -v "+cmd)
		case "bash":
			// bash: -i for interactive, -c for command
			shellCmd = exec.CommandContext(ctx, shell, "-i", "-c", "command -v "+cmd)
		default:
			// sh: basic POSIX shell, may not support aliases well
			shellCmd = exec.CommandContext(ctx, shell, "-c", "command -v "+cmd)
		}

		output, err := shellCmd.Output()
		if err != nil {
			continue
		}

		result := strings.TrimSpace(string(output))
		if result == "" || result == cmd {
			continue
		}

		// Strip terminal escape sequences (OSC, CSI) that interactive shells may emit
		// (e.g., iTerm2 shell integration markers like \x1b]1337;RemoteHost=...\x07)
		result = stripEscapeSequences(result)
		result = strings.TrimSpace(result)
		if result == "" || result == cmd {
			continue
		}

		// Handle alias definitions like "alias code=code-fb" or "code: aliased to code-fb"
		// Extract the target command from the alias
		path := result
		if strings.HasPrefix(result, "alias ") {
			// Format: "alias code=code-fb" or "alias code='code-fb'"
			if idx := strings.Index(result, "="); idx != -1 {
				path = strings.Trim(result[idx+1:], "'\"")
			}
		} else if strings.Contains(result, ": aliased to ") {
			// Format: "code: aliased to code-fb"
			parts := strings.SplitN(result, ": aliased to ", 2)
			if len(parts) == 2 {
				path = strings.TrimSpace(parts[1])
			}
		}

		// If the path is not absolute, try to resolve it via exec.LookPath
		if !filepath.IsAbs(path) {
			if resolved, err := exec.LookPath(path); err == nil {
				return resolved, true
			}
		}

		// If it's an absolute path, check if it exists and is executable
		if filepath.IsAbs(path) && fileExists(path) {
			return path, true
		}
	}

	return "", false
}

// checkKnownLocations checks well-known VS Code installation locations.
func checkKnownLocations() (path string, source string, found bool) {
	if runtime.GOOS == "darwin" {
		// macOS: VS Code installs the CLI helper in the app bundle
		locations := []struct {
			path   string
			source string
		}{
			{
				path:   "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
				source: "VS Code.app bundle",
			},
			{
				path:   "/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/bin/code-insiders",
				source: "VS Code Insiders.app bundle",
			},
		}

		// Also check user's home Applications folder
		if home := homeDirOrTilde(); home != "~" {
			locations = append(locations,
				struct {
					path   string
					source string
				}{
					path:   filepath.Join(home, "Applications", "Visual Studio Code.app", "Contents", "Resources", "app", "bin", "code"),
					source: "VS Code.app bundle (user)",
				},
				struct {
					path   string
					source string
				}{
					path:   filepath.Join(home, "Applications", "Visual Studio Code - Insiders.app", "Contents", "Resources", "app", "bin", "code-insiders"),
					source: "VS Code Insiders.app bundle (user)",
				},
			)
		}

		for _, loc := range locations {
			if fileExists(loc.path) {
				return loc.path, loc.source, true
			}
		}
	}

	if runtime.GOOS == "linux" {
		// Linux: Common installation paths
		locations := []struct {
			path   string
			source string
		}{
			{
				path:   "/usr/bin/code",
				source: "system install",
			},
			{
				path:   "/usr/share/code/bin/code",
				source: "system install (share)",
			},
			{
				path:   "/snap/bin/code",
				source: "snap",
			},
		}

		// Also check user's local bin
		if home := homeDirOrTilde(); home != "~" {
			locations = append(locations,
				struct {
					path   string
					source string
				}{
					path:   filepath.Join(home, ".local", "bin", "code"),
					source: "user local install",
				},
			)
		}

		for _, loc := range locations {
			if fileExists(loc.path) {
				return loc.path, loc.source, true
			}
		}
	}

	return "", "", false
}

// stripEscapeSequences removes terminal escape sequences from shell output.
// Interactive shells may emit OSC sequences (iTerm2 shell integration, etc.)
// and CSI sequences that pollute command output.
func stripEscapeSequences(s string) string {
	s = oscEscapeRe.ReplaceAllString(s, "")
	s = csiEscapeRe.ReplaceAllString(s, "")
	return s
}
