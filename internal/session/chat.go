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

// buildChatCommand builds the harness half of the bridge pipeline: env
// prefix, binary, the descriptor's chat base args, the protocol's own argv
// (model, resume, and fenced flags for Claude; nothing for Codex, whose
// equivalents are request parameters), and the signaling flags exactly as
// buildCommand applies them (Codex's status comes from the signaling
// instruction file). Persona flags are appended by Spawn as for terminal
// commands. The handshake lines, when any, are written to the input file by
// prepareChatFiles before tmux starts.
func buildChatCommand(target ResolvedTarget, model *detect.Model, fence bool, resumeID, cwd string) (string, chat.Protocol, [][]byte, error) {
	adapter := detect.GetAdapter(target.ToolName)
	if adapter == nil {
		return "", nil, nil, fmt.Errorf("chat requires a descriptor-backed target: %s", target.Name)
	}
	args := adapter.ChatArgs(model, resumeID)
	if args == nil {
		return "", nil, nil, fmt.Errorf("harness %s has no chat mode", target.ToolName)
	}
	proto, err := chat.ProtocolFor(adapter.ChatProtocol())
	if err != nil {
		return "", nil, nil, err
	}
	modelValue := ""
	if model != nil {
		if spec, ok := model.RunnerFor(target.ToolName); ok {
			modelValue = spec.ModelValue
		}
	}
	argv, handshake := proto.Launch(chat.LaunchOpts{Adapter: adapter, ModelValue: modelValue, ResumeID: resumeID, Fenced: fence, Cwd: cwd})
	parts := append(strings.Fields(target.Command), args...)
	parts = append(parts, argv...)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellutil.QuoteIfNeeded(p)
	}
	cmd := appendSignalingFlags(strings.Join(quoted, " "), target.ToolName, false)
	if len(target.Env) > 0 {
		cmd = buildEnvPrefix(target.Env) + " " + cmd
	}
	return cmd, proto, handshake, nil
}

// prepareChatFiles creates the bridge files and the conversation record,
// seeds the record from a prior session when seedFromPath is set (Restart),
// and writes the protocol's handshake lines before the harness can see them.
// The first user message is sent through the runtime like every later one,
// after the session is persisted.
func prepareChatFiles(p chat.Paths, seedFromPath string, handshake [][]byte) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	if seedFromPath != "" {
		if err := chat.CopyLog(seedFromPath, p.Conversation); err != nil {
			return fmt.Errorf("chat: seed conversation: %w", err)
		}
	}
	if _, err := chat.OpenLog(p.Conversation); err != nil {
		return err
	}
	for _, line := range handshake {
		if err := chat.AppendInput(p, line); err != nil {
			return err
		}
	}
	return nil
}
