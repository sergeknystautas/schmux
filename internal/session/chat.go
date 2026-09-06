package session

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// ErrChatSession is returned where a terminal runtime is requested for a
// chat session. Terminal socket, capture, tell, and clipboard injection all
// go through GetTracker, so this one error gates all of them.
var ErrChatSession = errors.New("chat sessions have no terminal runtime")

// buildChatClaudeCommand builds the claude half of the bridge pipeline:
// env prefix, binary, chat-mode args, model flag, resume-by-id, and the
// fence's skip-approvals args. Persona flags are appended by Spawn exactly as
// for terminal commands.
func buildChatClaudeCommand(target ResolvedTarget, model *detect.Model, fence bool, resumeID string) (string, error) {
	adapter := detect.GetAdapter(target.ToolName)
	if adapter == nil {
		return "", fmt.Errorf("chat requires a descriptor-backed target: %s", target.Name)
	}
	args := adapter.ChatArgs(model, resumeID)
	if args == nil {
		return "", fmt.Errorf("harness %s has no chat mode", target.ToolName)
	}
	parts := append(strings.Fields(target.Command), args...)
	if model != nil {
		if spec, ok := model.RunnerFor(target.ToolName); ok && spec.ModelValue != "" && adapter.ModelFlag() != "" {
			parts = append(parts, adapter.ModelFlag(), spec.ModelValue)
		}
	}
	if fence {
		parts = append(parts, adapter.AutoApproveArgs()...)
	}
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellutil.QuoteIfNeeded(p)
	}
	cmd := strings.Join(quoted, " ")
	if len(target.Env) > 0 {
		cmd = buildEnvPrefix(target.Env) + " " + cmd
	}
	return cmd, nil
}

// prepareChatFiles creates the bridge files and the conversation record,
// seeds the record from a prior session when seedFromPath is set (Restart),
// and records the initial prompt before the harness can see it.
func prepareChatFiles(p chat.Paths, seedFromPath, prompt string, images []chat.Image) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	if seedFromPath != "" {
		if err := chat.CopyLog(seedFromPath, p.Conversation); err != nil {
			return fmt.Errorf("chat: seed conversation: %w", err)
		}
	}
	l, err := chat.OpenLog(p.Conversation)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" && len(images) == 0 {
		return nil
	}
	line, err := chat.UserMessageLine(prompt, images)
	if err != nil {
		return err
	}
	if err := l.Append(chat.NewUserMessage(prompt, images)); err != nil {
		return err
	}
	return chat.AppendInput(p, line)
}
