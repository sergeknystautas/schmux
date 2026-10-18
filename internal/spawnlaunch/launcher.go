// Package spawnlaunch holds the local-target spawn path shared by the dashboard's
// POST /api/spawn handler and by backend domains such as flights. It lives
// outside internal/spawn because it drives session.Manager, which already
// depends on internal/spawn through the workspace manager.
package spawnlaunch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/spawnlog"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/style"
)

// Error is a request-level rejection: the request itself is invalid, so no
// target was attempted. Per-target problems (unknown target, prompt on a
// command target, spawn failure) are returned as plain errors instead.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func reject(msg string, args ...any) *Error {
	return &Error{Status: http.StatusBadRequest, Msg: fmt.Sprintf(msg, args...)}
}

// Config is the configuration the launcher reads.
type Config interface {
	GetChatSessions() bool
	GetFenceMode() string
	GetCommStyles() map[string]string
	GetGitCloneTimeoutMs() int
}

// Models resolves spawn target names.
type Models interface {
	IsModel(name string) (promptable, found bool)
	ResolveTargetToTool(name string) string
	FindModel(id string) (detect.Model, bool)
	ResolveToolForModel(model detect.Model) string
}

// Personas looks up personas by ID.
type Personas interface {
	Get(id string) (*persona.Persona, error)
}

// Styles looks up communication styles by ID.
type Styles interface {
	Get(id string) (*style.Style, error)
}

// Sessions spawns local sessions.
type Sessions interface {
	Spawn(ctx context.Context, opts session.SpawnOptions) (*state.Session, error)
}

// LocalTargetRequest describes one local session spawn for one target.
type LocalTargetRequest struct {
	RepoURL        string
	Branch         string
	WorkspaceID    string
	WorkspaceLabel string
	TargetName     string
	Prompt         string
	Nickname       string
	PersonaID      string
	StyleID        string
	Kind           string
	Fence          bool

	// Used by the dashboard's spawn handler; flights leave them zero.
	Resume          bool
	NewBranch       string
	Images          []chat.Image
	FileAttachments []string
}

// LocalTargetResult is the successful outcome of one local target spawn.
type LocalTargetResult struct {
	SessionID   string
	WorkspaceID string
	Target      string
	Prompt      string
	Nickname    string
}

// Launcher validates and spawns local sessions.
type Launcher struct {
	Config   Config
	Models   Models
	Personas Personas
	Styles   Styles
	Sessions Sessions
	// FenceCommand returns the detected fence command and whether fence is installed.
	FenceCommand func() (command string, ok bool)
	// RegisterPrompt records a spawn prompt for clipboard suppression. Optional.
	RegisterPrompt func(prompt string)
	// Broadcast pushes a dashboard update after a successful spawn. Optional.
	Broadcast func()
	Logger    *log.Logger
}

// LocalTarget spawns one session for one target and records the attempt: it
// registers the prompt for clipboard suppression, calls SpawnLocal, appends a
// spawn-log record, and broadcasts after a success. Request-level rejections
// (*Error) are neither logged nor broadcast.
func (l *Launcher) LocalTarget(ctx context.Context, req LocalTargetRequest) (LocalTargetResult, error) {
	if l.RegisterPrompt != nil {
		l.RegisterPrompt(req.Prompt)
	}
	res, err := l.SpawnLocal(ctx, req)
	var rejected *Error
	if errors.As(err, &rejected) {
		return LocalTargetResult{}, err
	}

	sessionLog := logging.Sub(l.Logger, "session")
	logResult := contracts.SpawnLogResult{Target: req.TargetName, SessionID: res.SessionID, WorkspaceID: res.WorkspaceID}
	if err != nil {
		logResult.Error = err.Error()
		sessionLog.Error("spawn failed", "target", req.TargetName, "err", err)
	} else {
		sessionLog.Info("spawn success", "target", req.TargetName, "session_id", res.SessionID, "workspace_id", res.WorkspaceID)
	}
	WriteSpawnLog(l.Logger, contracts.SpawnRequest{
		Repo:        req.RepoURL,
		Branch:      req.Branch,
		WorkspaceID: req.WorkspaceID,
		Targets:     map[string]int{req.TargetName: 1},
		Nickname:    req.Nickname,
		Fence:       req.Fence,
		Resume:      req.Resume,
		Prompt:      req.Prompt,
	}, []contracts.SpawnLogResult{logResult})

	if err != nil {
		return LocalTargetResult{}, err
	}
	if l.Broadcast != nil {
		go l.Broadcast()
	}
	return res, nil
}

// SpawnLocal validates the request, resolves its target, persona, style, and
// fence decision, and spawns one local session. It neither logs nor
// broadcasts; the dashboard's multi-target handler does that once per request.
func (l *Launcher) SpawnLocal(ctx context.Context, req LocalTargetRequest) (LocalTargetResult, error) {
	if req.Kind != "" && req.Kind != state.SessionKindChat {
		return LocalTargetResult{}, reject("unknown session kind: %s", req.Kind)
	}
	if req.Kind == state.SessionKindChat {
		if err := l.CheckChat([]string{req.TargetName}); err != nil {
			return LocalTargetResult{}, err
		}
	}
	var fenceCommand string
	if req.Fence {
		cmd, err := l.ResolveFence()
		if err != nil {
			return LocalTargetResult{}, err
		}
		fenceCommand = cmd
	}
	if err := l.CheckPersonaStyle(req.PersonaID, req.StyleID); err != nil {
		return LocalTargetResult{}, err
	}

	if _, err := l.ResolveTarget(req.TargetName, req.Prompt); err != nil {
		return LocalTargetResult{}, err
	}

	agentPrompt, styleID, err := l.PromptContext(req.PersonaID, req.StyleID, req.TargetName)
	if err != nil {
		return LocalTargetResult{}, err
	}

	// Session spawn needs a longer timeout for git operations.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(l.Config.GetGitCloneTimeoutMs())*time.Millisecond)
	defer cancel()
	sess, err := l.Sessions.Spawn(ctx, session.SpawnOptions{
		RepoURL:         req.RepoURL,
		Branch:          req.Branch,
		TargetName:      req.TargetName,
		Prompt:          req.Prompt,
		Nickname:        req.Nickname,
		WorkspaceID:     req.WorkspaceID,
		WorkspaceLabel:  req.WorkspaceLabel,
		Resume:          req.Resume,
		NewBranch:       req.NewBranch,
		PersonaID:       req.PersonaID,
		PersonaPrompt:   agentPrompt,
		StyleID:         styleID,
		Images:          req.Images,
		FileAttachments: req.FileAttachments,
		Fence:           req.Fence,
		FenceCommand:    fenceCommand,
		Kind:            req.Kind,
	})
	if err != nil {
		return LocalTargetResult{}, err
	}
	return LocalTargetResult{
		SessionID:   sess.ID,
		WorkspaceID: sess.WorkspaceID,
		Target:      req.TargetName,
		Prompt:      req.Prompt,
		Nickname:    sess.Nickname,
	}, nil
}

// ResolveTarget reports whether the named target exists and whether it takes a
// prompt. A missing target, or a prompt sent to a command target, is a plain
// per-target error.
func (l *Launcher) ResolveTarget(name, prompt string) (promptable bool, err error) {
	promptable, found := l.Models.IsModel(name)
	if !found {
		return false, fmt.Errorf("target not found: %s", name)
	}
	if !promptable && strings.TrimSpace(prompt) != "" {
		return false, errors.New("prompt is not allowed for command targets")
	}
	return promptable, nil
}

// CheckChat validates that chat sessions are enabled and that every named
// target has a chat mode.
func (l *Launcher) CheckChat(targetNames []string) error {
	if !l.Config.GetChatSessions() {
		return reject("chat sessions are disabled (chat_sessions)")
	}
	for _, name := range targetNames {
		adapter := detect.GetAdapter(l.resolveTool(name))
		if adapter == nil || adapter.ChatArgs(nil, false, "") == nil {
			return reject("target %s has no chat mode", name)
		}
	}
	return nil
}

func (l *Launcher) resolveTool(name string) string {
	if model, ok := l.Models.FindModel(name); ok {
		return l.Models.ResolveToolForModel(model)
	}
	if detect.IsToolName(name) {
		return name
	}
	return ""
}

// ResolveFence returns the fence command for a fenced spawn, or a rejection
// when fence is not installed or fenced sessions are disabled. A fenced spawn
// that cannot be honored never silently runs unfenced.
func (l *Launcher) ResolveFence() (string, error) {
	cmd, err := l.DetectFence()
	if err != nil {
		return "", err
	}
	if err := l.CheckFenceMode(); err != nil {
		return "", err
	}
	return cmd, nil
}

// DetectFence returns the installed fence command, or a rejection when fence
// is not installed.
func (l *Launcher) DetectFence() (string, error) {
	if l.FenceCommand != nil {
		if cmd, ok := l.FenceCommand(); ok {
			return cmd, nil
		}
	}
	return "", reject("fence not available — install fence to use fenced sessions")
}

// CheckFenceMode returns a rejection when the configured fence mode disables
// fenced sessions.
func (l *Launcher) CheckFenceMode() error {
	if l.Config.GetFenceMode() == config.FenceModeDisabled {
		return reject("fenced sessions are disabled")
	}
	return nil
}

// CheckPersonaStyle validates the explicitly requested persona and style IDs.
func (l *Launcher) CheckPersonaStyle(personaID, styleID string) error {
	_, _, err := l.explicitPersonaStyle(personaID, styleID)
	return err
}

func (l *Launcher) explicitPersonaStyle(personaID, styleID string) (*persona.Persona, *style.Style, error) {
	var p *persona.Persona
	if personaID != "" {
		got, err := l.Personas.Get(personaID)
		if err != nil {
			return nil, nil, reject("persona not found: %s", personaID)
		}
		p = got
	}
	var st *style.Style
	if styleID != "" && styleID != "none" {
		got, err := l.Styles.Get(styleID)
		if err != nil {
			return nil, nil, reject("style not found: %s", styleID)
		}
		st = got
	}
	return p, st, nil
}

// PromptContext resolves the persona and communication style for one target
// and returns the composed agent system prompt and the style ID that applied.
// An explicit style wins; "none" suppresses the configured per-tool default.
func (l *Launcher) PromptContext(personaID, styleID, targetName string) (agentPrompt, resolvedStyleID string, err error) {
	p, st, err := l.explicitPersonaStyle(personaID, styleID)
	if err != nil {
		return "", "", err
	}
	if st == nil && styleID != "none" {
		baseTool := l.Models.ResolveTargetToTool(targetName)
		if baseTool == "" {
			baseTool = targetName // command targets use their name directly
		}
		if defaultID := l.Config.GetCommStyles()[baseTool]; defaultID != "" {
			st, _ = l.Styles.Get(defaultID)
		}
	}
	if st != nil {
		resolvedStyleID = st.ID
	}
	return FormatAgentSystemPrompt(p, st), resolvedStyleID, nil
}

// FormatAgentSystemPrompt composes a system prompt from persona and/or style.
func FormatAgentSystemPrompt(p *persona.Persona, st *style.Style) string {
	var b strings.Builder
	if p != nil {
		fmt.Fprintf(&b, "## Persona: %s\n\n", p.Name)
		if p.Expectations != "" {
			fmt.Fprintf(&b, "### Behavioral Expectations\n%s\n\n", strings.TrimSpace(p.Expectations))
		}
		fmt.Fprintf(&b, "### Instructions\n%s\n", strings.TrimSpace(p.Prompt))
	}
	if st != nil {
		if p != nil {
			b.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&b, "## Communication Style: %s\n\n%s\n", st.Name, strings.TrimSpace(st.Prompt))
	}
	return b.String()
}

// WriteSpawnLog persists one resolved spawn request plus its per-target outcome
// to the spawn log. Best effort — a write failure is logged, never fatal.
func WriteSpawnLog(logger *log.Logger, req contracts.SpawnRequest, results []contracts.SpawnLogResult) {
	rec := contracts.SpawnLogRecord{
		TS:              time.Now().UTC().Format(time.RFC3339),
		Repo:            req.Repo,
		Branch:          req.Branch,
		WorkspaceID:     req.WorkspaceID,
		Targets:         req.Targets,
		Command:         req.Command,
		Nickname:        req.Nickname,
		Fence:           req.Fence,
		Resume:          req.Resume,
		RemoteProfileID: req.RemoteProfileID,
		RemoteFlavor:    req.RemoteFlavor,
		Prompt:          req.Prompt,
		Status:          spawnlog.DeriveStatus(results),
		Results:         results,
	}
	if err := spawnlog.Append(rec); err != nil {
		logging.Sub(logger, "spawnlog").Warn("failed to write spawn log", "err", err)
	}
}
