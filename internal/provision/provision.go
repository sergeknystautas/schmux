// Package provision handles automatic provisioning of agent instruction files.
package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/detect"
)

const (
	// Markers used to identify schmux-managed content in instruction files
	schmuxMarkerStart = "<!-- SCHMUX:BEGIN -->"
	schmuxMarkerEnd   = "<!-- SCHMUX:END -->"
)

// SignalingInstructions is the template for agent signaling instructions.
// This is appended to agent instruction files to enable direct signaling.
const SignalingInstructions = `## Schmux Status Signaling

This workspace is managed by schmux. Signal your status to help the user monitor your progress.

### How to Signal

Output this escape sequence to signal your status (uses standard OSC 777 format):
` + "```bash" + `
printf '\x1b]777;notify;<state>;<message>\x07'
` + "```" + `

### Available States

| State | When to Use |
|-------|-------------|
| ` + "`completed`" + ` | Task finished successfully |
| ` + "`needs_input`" + ` | Waiting for user confirmation, approval, or choice |
| ` + "`needs_testing`" + ` | Implementation ready for user to test |
| ` + "`error`" + ` | Something failed that needs user attention |
| ` + "`working`" + ` | Starting new work (clears previous status) |

### Examples

` + "```bash" + `
# After finishing a task
printf '\x1b]777;notify;completed;Implemented the login feature\x07'

# When you need user approval
printf '\x1b]777;notify;needs_input;Should I delete these 5 files?\x07'

# When ready for testing
printf '\x1b]777;notify;needs_testing;Please try the new search functionality\x07'

# When encountering an error
printf '\x1b]777;notify;error;Build failed - missing dependency\x07'

# When starting new work
printf '\x1b]777;notify;working;\x07'
` + "```" + `

### Best Practices

1. **Signal completion** when you finish the user's request
2. **Signal needs_input** when waiting for user decisions (don't just ask in text)
3. **Signal error** for failures that block progress
4. **Signal working** when starting a new task to clear old status
5. Keep messages concise (under 100 characters)
`

// EnsureAgentInstructions ensures the signaling instructions are present
// in the appropriate instruction file for the given target.
// Returns nil if the target doesn't have a known instruction file.
func EnsureAgentInstructions(workspacePath, targetName string) error {
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
			fmt.Printf("[provision] created %s with signaling instructions\n", instructionPath)
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
			fmt.Printf("[provision] updated signaling instructions in %s\n", instructionPath)
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
	fmt.Printf("[provision] appended signaling instructions to %s\n", instructionPath)
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
