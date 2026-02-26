// Package ensure handles ensuring workspaces have the necessary schmux configuration.
package ensure

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/state"
)

// pkgLogger is the package-level logger, set via SetLogger.
var pkgLogger *log.Logger

// SetLogger configures the package-level logger for the ensure package.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

const (
	// Markers used to identify schmux-managed content in instruction files
	schmuxMarkerStart = "<!-- SCHMUX:BEGIN -->"
	schmuxMarkerEnd   = "<!-- SCHMUX:END -->"
)

// Ensurer ensures workspaces have the necessary schmux configuration.
// It holds a state reference to make decisions about what configuration
// a workspace needs based on active sessions.
type Ensurer struct {
	state    state.StateStore
	hooksDir string // absolute path to centralized hooks directory (~/.schmux/hooks/)
}

// New creates a new Ensurer with access to state for session lookups.
func New(st state.StateStore) *Ensurer {
	return &Ensurer{state: st}
}

// SetHooksDir sets the centralized hooks directory path.
func (e *Ensurer) SetHooksDir(dir string) {
	e.hooksDir = dir
}

// ForSpawn ensures a workspace has all necessary schmux configuration
// when a new session is being spawned. The currentTarget is the agent
// being spawned (needed because the session doesn't exist in state yet).
func (e *Ensurer) ForSpawn(workspaceID, currentTarget string) error {
	w, found := e.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	hookTools := e.workspaceHookTools(workspaceID)
	baseTool := detect.GetBaseToolName(currentTarget)
	if adapter := detect.GetAdapter(baseTool); adapter != nil && adapter.SupportsHooks() {
		found := false
		for _, t := range hookTools {
			if t == baseTool {
				found = true
				break
			}
		}
		if !found {
			hookTools = append(hookTools, baseTool)
		}
	}
	return e.ensureWorkspace(w.Path, hookTools)
}

// ForWorkspace ensures a workspace has all necessary schmux configuration.
// Used for daemon startup and overlay refresh when there's no spawn context.
func (e *Ensurer) ForWorkspace(workspaceID string) error {
	w, found := e.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	return e.ensureWorkspace(w.Path, e.workspaceHookTools(workspaceID))
}

// workspaceHookTools returns the list of tool names that support hooks
// and have active sessions in this workspace.
func (e *Ensurer) workspaceHookTools(workspaceID string) []string {
	seen := map[string]bool{}
	var tools []string
	for _, s := range e.state.GetSessions() {
		if s.WorkspaceID == workspaceID {
			baseTool := detect.GetBaseToolName(s.Target)
			if !seen[baseTool] {
				if adapter := detect.GetAdapter(baseTool); adapter != nil && adapter.SupportsHooks() {
					seen[baseTool] = true
					tools = append(tools, baseTool)
				}
			}
		}
	}
	return tools
}

// ensureWorkspace writes all schmux-managed configuration for a workspace.
func (e *Ensurer) ensureWorkspace(workspacePath string, hookTools []string) error {
	for _, toolName := range hookTools {
		adapter := detect.GetAdapter(toolName)
		if adapter == nil || !adapter.SupportsHooks() {
			continue
		}
		ctx := detect.HookContext{
			WorkspacePath: workspacePath,
			HooksDir:      e.hooksDir,
		}
		if err := adapter.SetupHooks(ctx); err != nil {
			fmt.Printf("[ensure] warning: failed to ensure %s hooks: %v\n", toolName, err)
		}
	}

	// Setup tool-specific commands (e.g., /commit for OpenCode)
	for _, toolName := range hookTools {
		adapter := detect.GetAdapter(toolName)
		if adapter != nil {
			if err := adapter.SetupCommands(workspacePath); err != nil {
				fmt.Printf("[ensure] warning: failed to setup %s commands: %v\n", toolName, err)
			}
		}
	}

	if err := GitExclude(workspacePath); err != nil {
		fmt.Printf("[ensure] warning: failed to ensure git exclude: %v\n", err)
	}
	return nil
}

// SignalingInstructions is the template for agent signaling instructions.
// This is appended to agent instruction files to enable direct signaling.
const SignalingInstructions = `## Schmux Status Signaling

This workspace is managed by schmux. Signal your status to help the user monitor your progress.

### How to Signal

Append structured events to $SCHMUX_EVENTS_FILE (per-session append-only JSONL):

` + "```" + `
echo '{"ts":"<ISO8601>","type":"status","state":"STATE","message":"what you did"}' >> "$SCHMUX_EVENTS_FILE"
` + "```" + `

### Available States

| State | When to Use |
|-------|-------------|
| ` + "`completed`" + ` | Task finished successfully |
| ` + "`needs_input`" + ` | Waiting for user confirmation, approval, or choice |
| ` + "`needs_testing`" + ` | Implementation ready for user to test |
| ` + "`error`" + ` | Something failed that needs user attention |
| ` + "`working`" + ` | Starting new work (include what you're doing) |

### Examples

` + "```" + `
# After finishing a task
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"completed","message":"Implemented the login feature"}' >> "$SCHMUX_EVENTS_FILE"

# When you need user approval
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"needs_input","message":"Should I delete these 5 files?"}' >> "$SCHMUX_EVENTS_FILE"

# When encountering an error
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"error","message":"Build failed - missing dependency"}' >> "$SCHMUX_EVENTS_FILE"

# When starting new work
echo '{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"Refactoring auth module"}' >> "$SCHMUX_EVENTS_FILE"
` + "```" + `

### Best Practices

1. **Signal completion** when you finish the user's request
2. **Signal needs_input** when waiting for user decisions (don't just ask in text)
3. **Signal error** for failures that block progress
4. **Signal working** when starting a new task (describe what you're doing)
5. Keep messages concise (under 100 characters)

## Friction Capture

When you hit a wall — wrong command, missing file, failed build, wrong assumption —
append a reflection event to ` + "`$SCHMUX_EVENTS_FILE`" + `:

` + "```" + `
echo '{"ts":"<ISO8601>","type":"reflection","text":"When <trigger>, do <correction> instead"}' >> "$SCHMUX_EVENTS_FILE"
` + "```" + `

Only write when something tripped you up. Don't write what you built or learned —
write what would have saved you time if you'd known it before starting.
`

// AgentInstructions ensures the signaling instructions are present
// in the appropriate instruction file for the given target.
// Returns nil if the target doesn't have a known instruction file.
func AgentInstructions(workspacePath, targetName string) error {
	config, ok := detect.GetAgentInstructionConfigForTarget(targetName)
	if !ok {
		// Target doesn't have a known instruction file, nothing to do
		return nil
	}

	// Build the full path to the instruction file
	instructionDir := filepath.Join(workspacePath, config.InstructionDir)
	instructionPath := filepath.Join(instructionDir, config.InstructionFile)

	// Ensure the directory exists
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		return fmt.Errorf("failed to create instruction directory %s: %w", instructionDir, err)
	}

	// Build the schmux block with markers
	schmuxBlock := buildSchmuxBlock()

	// Check if file exists
	content, err := os.ReadFile(instructionPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, create it with just the schmux block
			if err := os.WriteFile(instructionPath, []byte(schmuxBlock), 0644); err != nil {
				return fmt.Errorf("failed to create instruction file %s: %w", instructionPath, err)
			}
			if pkgLogger != nil {
				pkgLogger.Info("created instruction file with signaling instructions", "path", instructionPath)
			}
			return nil
		}
		return fmt.Errorf("failed to read instruction file %s: %w", instructionPath, err)
	}

	// File exists, check if schmux block is already present
	contentStr := string(content)
	if strings.Contains(contentStr, schmuxMarkerStart) {
		// Block already exists, update it
		newContent := replaceSchmuxBlock(contentStr, schmuxBlock)
		if newContent != contentStr {
			if err := os.WriteFile(instructionPath, []byte(newContent), 0644); err != nil {
				return fmt.Errorf("failed to update instruction file %s: %w", instructionPath, err)
			}
			if pkgLogger != nil {
				pkgLogger.Info("updated signaling instructions", "path", instructionPath)
			}
		}
		return nil
	}

	// Block doesn't exist, append it
	newContent := contentStr
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += "\n" + schmuxBlock

	if err := os.WriteFile(instructionPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to append to instruction file %s: %w", instructionPath, err)
	}
	if pkgLogger != nil {
		pkgLogger.Info("appended signaling instructions", "path", instructionPath)
	}
	return nil
}

// buildSchmuxBlock builds the full schmux block with markers.
func buildSchmuxBlock() string {
	return schmuxMarkerStart + "\n" + SignalingInstructions + schmuxMarkerEnd + "\n"
}

// replaceSchmuxBlock replaces an existing schmux block with the new one.
func replaceSchmuxBlock(content, newBlock string) string {
	startIdx := strings.Index(content, schmuxMarkerStart)
	endIdx := strings.Index(content, schmuxMarkerEnd)

	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		// Markers not found or malformed, just return original
		return content
	}

	// Include the end marker and any trailing newline
	endIdx += len(schmuxMarkerEnd)
	if endIdx < len(content) && content[endIdx] == '\n' {
		endIdx++
	}

	return content[:startIdx] + newBlock + content[endIdx:]
}

// RemoveAgentInstructions removes the schmux signaling block from an instruction file.
// Used for cleanup if needed.
func RemoveAgentInstructions(workspacePath, targetName string) error {
	config, ok := detect.GetAgentInstructionConfigForTarget(targetName)
	if !ok {
		return nil
	}

	instructionPath := filepath.Join(workspacePath, config.InstructionDir, config.InstructionFile)

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, schmuxMarkerStart) {
		return nil
	}

	startIdx := strings.Index(contentStr, schmuxMarkerStart)
	endIdx := strings.Index(contentStr, schmuxMarkerEnd)

	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return nil
	}

	// Include the end marker and surrounding whitespace
	endIdx += len(schmuxMarkerEnd)
	if endIdx < len(contentStr) && contentStr[endIdx] == '\n' {
		endIdx++
	}
	// Also remove preceding newline if present
	if startIdx > 0 && contentStr[startIdx-1] == '\n' {
		startIdx--
	}

	newContent := contentStr[:startIdx] + contentStr[endIdx:]

	// If file is now empty or just whitespace, remove it
	if strings.TrimSpace(newContent) == "" {
		if err := os.Remove(instructionPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	return os.WriteFile(instructionPath, []byte(newContent), 0644)
}

// SignalingInstructionsFilePath returns the canonical path for system prompt file injection.
func SignalingInstructionsFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".schmux", "signaling.md")
	}
	return filepath.Join(homeDir, ".schmux", "signaling.md")
}

// SignalingInstructionsFile writes signaling instructions to ~/.schmux/signaling.md.
func SignalingInstructionsFile() error {
	path := SignalingInstructionsFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create signaling directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(SignalingInstructions), 0644); err != nil {
		return fmt.Errorf("failed to write signaling instructions file: %w", err)
	}
	return nil
}

// HasSignalingInstructions checks if the instruction file for a target
// already has the schmux signaling block.
func HasSignalingInstructions(workspacePath, targetName string) bool {
	config, ok := detect.GetAgentInstructionConfigForTarget(targetName)
	if !ok {
		return false
	}

	instructionPath := filepath.Join(workspacePath, config.InstructionDir, config.InstructionFile)

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(content), schmuxMarkerStart)
}

// Markers for the schmux-managed block in .git/info/exclude (gitignore comment syntax).
const (
	excludeMarkerStart = "# SCHMUX:BEGIN - managed by schmux, do not edit"
	excludeMarkerEnd   = "# SCHMUX:END"
)

// excludePatterns are the gitignore patterns managed by schmux.
// These cover daemon-written files that should not appear in git status.
var excludePatterns = []string{
	".schmux/hooks/",
	".schmux/events/",
	".opencode/plugins/schmux.ts",
	".opencode/commands/commit.md",
}

// buildExcludeBlock builds the full schmux exclude block with markers.
func buildExcludeBlock() string {
	var b strings.Builder
	b.WriteString(excludeMarkerStart)
	b.WriteByte('\n')
	for _, p := range excludePatterns {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	b.WriteString(excludeMarkerEnd)
	b.WriteByte('\n')
	return b.String()
}

// GitExclude ensures that daemon-managed .schmux/ paths are excluded from
// git status by writing patterns to .git/info/exclude (or the shared git
// dir's info/exclude for worktrees).
func GitExclude(workspacePath string) error {
	// Resolve the shared git directory (handles both full clones and worktrees).
	cmd := exec.Command("git", "-C", workspacePath, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse --git-common-dir failed: %w", err)
	}
	gitCommonDir := strings.TrimSpace(string(out))

	// git returns a relative path for full clones (e.g. ".git"),
	// and an absolute path for worktrees.
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(workspacePath, gitCommonDir)
	}

	excludePath := filepath.Join(gitCommonDir, "info", "exclude")
	return ensureExcludeEntries(excludePath)
}

// ensureExcludeEntries ensures the schmux exclude block is present and
// up-to-date in the given exclude file. It creates the file and parent
// directories if they don't exist, preserves existing user entries, and
// is idempotent (running twice produces an identical file).
func ensureExcludeEntries(excludePath string) error {
	block := buildExcludeBlock()

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return fmt.Errorf("failed to create info directory: %w", err)
	}

	// Read existing file (empty if it doesn't exist yet).
	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read exclude file: %w", err)
	}
	existing := string(content)

	var newContent string
	if strings.Contains(existing, excludeMarkerStart) {
		// Replace existing schmux block.
		newContent = replaceExcludeBlock(existing, block)
	} else {
		// Append block, ensuring a blank line separator.
		newContent = existing
		if len(newContent) > 0 && !strings.HasSuffix(newContent, "\n") {
			newContent += "\n"
		}
		if len(newContent) > 0 {
			newContent += "\n"
		}
		newContent += block
	}

	// Skip write if content is unchanged.
	if newContent == existing {
		return nil
	}

	return os.WriteFile(excludePath, []byte(newContent), 0644)
}

// replaceExcludeBlock replaces the schmux block (between markers, inclusive)
// in the given content with the new block.
func replaceExcludeBlock(content, newBlock string) string {
	startIdx := strings.Index(content, excludeMarkerStart)
	endIdx := strings.Index(content, excludeMarkerEnd)

	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return content
	}

	// Include the end marker and trailing newline.
	endIdx += len(excludeMarkerEnd)
	if endIdx < len(content) && content[endIdx] == '\n' {
		endIdx++
	}

	return content[:startIdx] + newBlock + content[endIdx:]
}

// EnsureGlobalHookScripts delegates to detect.EnsureGlobalHookScripts.
// Called once at daemon startup. Returns the hooks directory path.
func EnsureGlobalHookScripts(homeDir string) (string, error) {
	return detect.EnsureGlobalHookScripts(homeDir)
}
