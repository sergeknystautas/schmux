package session

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/fence"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/repo"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/telemetry"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/tmux/controlmode"
	"github.com/sergeknystautas/schmux/internal/workspace"
	"github.com/sergeknystautas/schmux/internal/workspace/ensure"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

const (
	// maxNicknameAttempts is the maximum number of attempts to find a unique nickname
	// before falling back to a UUID suffix.
	maxNicknameAttempts = 100
)

// Manager manages sessions.
type Manager struct {
	config                  *config.Config
	state                   state.StateStore
	repos                   *repo.Manager
	workspace               workspace.WorkspaceManager
	server                  *tmux.TmuxServer
	logger                  *log.Logger
	ensurer                 *ensure.Ensurer
	models                  *models.Manager // Model catalog, resolution, enablement
	eventHandlers           map[string][]events.EventHandler
	outputCallback          func(sessionID string, chunk []byte)
	trackerCallback         func(tracker *SessionRuntime) // optional, invoked once per tracker creation (spawn or restore)
	trackers                map[string]*SessionRuntime
	chatRuntimes            map[string]*chat.Runtime // chat sessions: conversation bridge runtime in place of a terminal tracker
	mu                      sync.RWMutex
	compoundCallback        func(workspaceID string, isSpawn bool)             // notify compounder on session spawn/dispose
	autolearnCallback       func(repoName, repoURL string, isLastSession bool) // notify autolearn curator on session dispose
	terminalCaptureCallback func(sessionID, workspaceID, output string)        // notify on terminal capture before dispose
	chatNudgeCallback       func(sessionID string, update chat.NudgeUpdate)    // headless chat Nudge updates; nil disables the path
	chatTurnErrorCallback   func(sessionID string, ev chat.TurnErrorEvent)     // live chat turn errors; nil disables the path
	chatActivityCallback    func()                                             // broadcast debounced chat activity; set before runtimes start
	chatUsageCallback       func(sessionID string, record chat.Record)         // centralized usage ingestion; set before runtimes start
	telemetry               telemetry.Telemetry                                // optional, for usage tracking
	recorderFactory         func(sessionID string, outputLog *OutputLog, gapCh <-chan SourceEvent, width, height int) Runnable
	reaper                  *reaper // terminates fenced session process trees
}

// ResolvedTarget is a resolved run target with command and env info.
type ResolvedTarget struct {
	Name       string
	Kind       string
	Command    string
	Promptable bool
	Env        map[string]string
	// Args are precomputed provider-routing CLI args (models.ResolveModel);
	// empty for endpoint-less specs.
	Args     []string
	Model    *detect.Model
	ToolName string // the resolved tool name (e.g., "claude", "opencode")
}

const (
	TargetKindDetected = "detected"
	TargetKindModel    = "model"
	TargetKindUser     = "user"
)

// New creates a new session manager.
func New(cfg *config.Config, st state.StateStore, repos *repo.Manager, statePath string, wm workspace.WorkspaceManager, server *tmux.TmuxServer, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.NewWithOptions(io.Discard, log.Options{})
	}
	return &Manager{
		config:       cfg,
		state:        st,
		repos:        repos,
		workspace:    wm,
		server:       server,
		logger:       logger,
		ensurer:      ensure.New(st),
		trackers:     make(map[string]*SessionRuntime),
		chatRuntimes: make(map[string]*chat.Runtime),
		reaper:       newReaper(logger),
	}
}

// ensureAgentInstructions provisions the schmux-managed instruction block for
// any built-in harness whose descriptor carries an instruction-file
// configuration. Called for every spawn regardless of signaling strategy, so
// hook-based harnesses (Claude Code, Codex) still receive the same managed
// block as `SignalingInstructionFile` harnesses.
func (m *Manager) ensureAgentInstructions(ws state.Workspace, baseTool string) error {
	if baseTool == "" {
		return nil
	}
	if _, ok := detect.GetAgentInstructionConfig(baseTool); !ok {
		return nil
	}
	if err := ensure.AgentInstructions(ws.Path, baseTool, ws.Repo); err != nil {
		return fmt.Errorf("provision agent instructions: %w", err)
	}
	return nil
}

// serverForSocket returns a TmuxServer targeting the given socket.
// TmuxServer is stateless (~56 bytes), so construction is free.
func (m *Manager) serverForSocket(socketName string) *tmux.TmuxServer {
	if m.server == nil {
		return nil
	}
	if socketName == "" {
		socketName = "default"
	}
	return tmux.NewTmuxServer(m.server.Binary(), socketName, m.logger)
}

// ServerForSocket is the public accessor for per-session socket resolution.
// Used by daemon.go for the session restore loop and dashboard for fallback captures.
func (m *Manager) ServerForSocket(socketName string) *tmux.TmuxServer {
	return m.serverForSocket(socketName)
}

// SetRecorderFactory sets a factory for creating timelapse recorders.
// The factory receives the sessionID, OutputLog, gap channel, and initial
// terminal dimensions, and returns a Runnable that will be started in a
// goroutine when the tracker starts.
func (m *Manager) SetRecorderFactory(fn func(sessionID string, outputLog *OutputLog, gapCh <-chan SourceEvent, width, height int) Runnable) {
	m.recorderFactory = fn
}

// SetModelManager sets the model manager for model resolution.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetModelManager(mm *models.Manager) {
	m.models = mm
	m.ensurer.SetResolver(mm)
}

// SetHooksDir sets the centralized hooks directory on the ensurer.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetHooksDir(dir string) {
	m.ensurer.SetHooksDir(dir)
}

// SetEventHandlers sets the event handlers for the unified event system.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetEventHandlers(handlers map[string][]events.EventHandler) {
	m.eventHandlers = handlers
}

// SetOutputCallback sets the callback for terminal output chunks from local trackers.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetOutputCallback(cb func(sessionID string, chunk []byte)) {
	m.outputCallback = cb
}

// SetTrackerCallback registers a function that is invoked exactly once per
// SessionRuntime creation (spawn paths and restore paths). Used by the
// dashboard server to spawn one OSC 52 clipboard subscriber goroutine per
// tracker. Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetTrackerCallback(cb func(tracker *SessionRuntime)) {
	m.trackerCallback = cb
}

// notifyTrackerCallback runs the registered tracker callback if any. Safe to
// call with the manager lock NOT held; the callback is responsible for its
// own concurrency.
func (m *Manager) notifyTrackerCallback(tracker *SessionRuntime) {
	if m.trackerCallback != nil {
		m.trackerCallback(tracker)
	}
}

// SetCompoundCallback sets the callback for notifying the compounder on session lifecycle events.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetCompoundCallback(cb func(workspaceID string, isSpawn bool)) {
	m.compoundCallback = cb
}

// SetAutolearnCallback sets the callback for notifying the autolearn system on session dispose.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetAutolearnCallback(cb func(repoName, repoURL string, isLastSession bool)) {
	m.autolearnCallback = cb
}

// SetTerminalCaptureCallback sets the callback for capturing terminal output before session dispose.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetTerminalCaptureCallback(cb func(sessionID, workspaceID, output string)) {
	m.terminalCaptureCallback = cb
}

// SetChatNudgeCallback registers the sink that receives headless
// NudgeUpdate values from chat runtimes. The callback runs on the
// runtime goroutine; it must not call back into the manager. Wiring
// happens at startup so that restored chat runtimes also receive the
// callback.
func (m *Manager) SetChatNudgeCallback(cb func(sessionID string, update chat.NudgeUpdate)) {
	m.chatNudgeCallback = cb
	// Wire the existing runtimes too: a daemon restart may have
	// restored them via ensureChatRuntime before this callback was
	// registered, and the manager is the single source of truth for
	// which runtimes are live.
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, rt := range m.chatRuntimes {
		rt.SetNudgeCallback(makeChatNudgeForwarder(id, cb))
	}
}

// makeChatNudgeForwarder closes over the session id and the manager's
// stored callback, so the runtime can invoke it through a chat.NudgeCallback.
func makeChatNudgeForwarder(sessionID string, cb func(sessionID string, update chat.NudgeUpdate)) chat.NudgeCallback {
	if cb == nil {
		return nil
	}
	return func(u chat.NudgeUpdate) {
		cb(sessionID, u)
	}
}

// SetChatTurnErrorCallback registers the sink that receives live chat
// turn-ending errors (nil disables). Wiring happens here and in
// ensureChatRuntime so restored runtimes are covered too.
func (m *Manager) SetChatTurnErrorCallback(cb func(sessionID string, ev chat.TurnErrorEvent)) {
	m.chatTurnErrorCallback = cb
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, rt := range m.chatRuntimes {
		rt.SetTurnErrorCallback(makeChatTurnErrorForwarder(id, cb))
	}
}

// makeChatTurnErrorForwarder closes over the session id and the manager's
// stored callback, so the runtime can invoke it through a
// chat.TurnErrorCallback.
func makeChatTurnErrorForwarder(sessionID string, cb func(sessionID string, ev chat.TurnErrorEvent)) chat.TurnErrorCallback {
	if cb == nil {
		return nil
	}
	return func(ev chat.TurnErrorEvent) {
		cb(sessionID, ev)
	}
}

// SetChatActivityCallback registers the startup-wired session broadcast sink.
// Activity updates use the existing in-memory LastOutputAt field, not NudgeSeq.
func (m *Manager) SetChatActivityCallback(cb func()) {
	m.chatActivityCallback = cb
}

// SetChatUsageCallback registers the centralized usage sink for live harness
// records. Existing runtimes are rewired so daemon restoration order cannot
// create a gap in usage collection.
func (m *Manager) SetChatUsageCallback(cb func(sessionID string, record chat.Record)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatUsageCallback = cb
	for id, rt := range m.chatRuntimes {
		rt.SetUsageCallback(makeChatUsageForwarder(id, cb))
	}
}

func makeChatUsageForwarder(sessionID string, cb func(sessionID string, record chat.Record)) func(chat.Record) {
	if cb == nil {
		return nil
	}
	return func(record chat.Record) {
		cb(sessionID, record)
	}
}

// SetTelemetry sets the telemetry client for usage tracking.
func (m *Manager) SetTelemetry(t telemetry.Telemetry) {
	m.telemetry = t
}

// trackSessionCreated sends a telemetry event for session creation.
// Safe to call even if telemetry is nil.
func (m *Manager) trackSessionCreated(sessionID, workspaceID, target string) {
	if m.telemetry == nil {
		return
	}
	m.telemetry.Track("session_created", map[string]any{
		"session_id":   sessionID,
		"workspace_id": workspaceID,
		"target":       target,
	})
}

// SpawnOptions holds parameters for Spawn and SpawnCommand.
type SpawnOptions struct {
	RepoURL         string
	Branch          string
	TargetName      string
	Prompt          string
	Command         string
	Nickname        string
	WorkspaceID     string
	WorkspaceLabel  string // Optional human-friendly label persisted on newly created workspaces (sapling-only today; ignored when WorkspaceID is set or when reusing an existing workspace)
	Resume          bool
	ResumeID        string // when set, restart resumes this specific harness conversation by id
	NewBranch       string
	PersonaID       string
	PersonaPrompt   string // Pre-resolved persona prompt content (set by handler)
	StyleID         string
	Images          []chat.Image // inline images; written into the workspace for terminal spawns
	FileAttachments []string     // absolute staged file paths, copied into the workspace during spawn
	Fence           bool         // OS-level fence sandbox for this spawn (local only)
	FenceCommand    string       // resolved fence command from the dependency report (internal-only; set by the handler)
	Kind            string       // "" (terminal) or state.SessionKindChat
	ChatSeedFrom    string       // chat only: prior session id whose conversation record seeds this one (Restart)
	// SignInProtocol, when set, marks the spawned command session as a local
	// sign-in helper for the named harness protocol. The session starts with
	// SignedOut=true so the helper participates in shared auth state. Empty
	// for ordinary command sessions.
	SignInProtocol string
}

// resolveWorkspace resolves the target workspace from SpawnOptions.
// If WorkspaceID+NewBranch are set, creates a new workspace branching from the source.
// If only WorkspaceID is set, looks up the existing workspace.
// Otherwise, finds or creates a workspace by RepoURL/Branch.
func (m *Manager) resolveWorkspace(ctx context.Context, opts SpawnOptions) (*state.Workspace, error) {
	ws, err := m.lookupWorkspace(ctx, opts)
	if err != nil {
		return nil, err
	}
	path, err := filepath.EvalSymlinks(ws.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace path: %w", err)
	}
	// Harness sandbox roots and attachment paths must use the same physical directory.
	resolved := *ws
	resolved.Path = path
	return &resolved, nil
}

func (m *Manager) lookupWorkspace(ctx context.Context, opts SpawnOptions) (*state.Workspace, error) {
	if opts.WorkspaceID != "" && opts.NewBranch != "" {
		w, err := m.workspace.CreateFromWorkspace(ctx, opts.WorkspaceID, opts.NewBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to create workspace from source: %w", err)
		}
		m.setCreatedAtIfMissing(w)
		return w, nil
	}
	if opts.WorkspaceID != "" {
		ws, found := m.workspace.GetByID(opts.WorkspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", opts.WorkspaceID)
		}
		return ws, nil
	}
	w, err := m.workspace.GetOrCreateWithLabel(ctx, opts.RepoURL, opts.Branch, opts.WorkspaceLabel)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	m.setCreatedAtIfMissing(w)
	return w, nil
}

// setCreatedAtIfMissing stamps the workspace with the current time if CreatedAt is zero.
func (m *Manager) setCreatedAtIfMissing(ws *state.Workspace) {
	if ws.CreatedAt.IsZero() {
		ws.CreatedAt = time.Now()
		m.state.UpdateWorkspace(*ws)
	}
}

// appendImagePathsToPrompt appends image file paths to the prompt text.
func appendImagePathsToPrompt(prompt string, paths []string) string {
	if len(paths) == 0 {
		return prompt
	}
	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n\nImage attachments:")
	for i, p := range paths {
		sb.WriteString(fmt.Sprintf("\nImage #%d: %s", i+1, p))
	}
	return sb.String()
}

// Spawn creates a new session.
// If WorkspaceID is provided, spawn into that specific workspace (Existing Directory Spawn mode).
// If WorkspaceID is provided with NewBranch, create a new workspace branching from the source workspace's branch.
// Otherwise, find or create a workspace by RepoURL/Branch.
// Nickname is an optional human-friendly name for the session.
// Prompt is only used if the target is promptable.
// Resume enables resume mode, which uses the agent's resume command instead of a prompt.
func (m *Manager) Spawn(ctx context.Context, opts SpawnOptions) (*state.Session, error) {
	resolved, err := m.ResolveTarget(ctx, opts.TargetName)
	if err != nil {
		return nil, err
	}

	// Check tmux availability early, before doing workspace resolution and
	// signaling setup work that would be wasted if tmux is missing.
	if m.server == nil {
		return nil, fmt.Errorf("tmux is required to spawn sessions. Install it with: brew install tmux (macOS) or apt install tmux (Linux)")
	}
	if err := m.server.Check(); err != nil {
		return nil, fmt.Errorf("tmux is required to spawn sessions. Install it with: brew install tmux (macOS) or apt install tmux (Linux)")
	}

	w, err := m.resolveWorkspace(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Provision agent signaling mechanism
	baseTool := resolved.ToolName

	isChat := opts.Kind == state.SessionKindChat

	// Ensure workspace has all necessary schmux configuration (hooks, scripts, git exclude)
	if err := m.ensurer.ForSpawn(w.ID, baseTool); err != nil {
		m.logger.Warn("failed to ensure workspace config", "err", err)
	}

	// Session-level signaling setup (not workspace-level)
	if adapter := detect.GetAdapter(baseTool); adapter != nil {
		switch adapter.SignalingStrategy() {
		case detect.SignalingHooks:
			// Handled by ensurer.ForSpawn above
		case detect.SignalingCLIFlag:
			if err := ensure.SignalingInstructionsFile(); err != nil {
				m.logger.Warn("failed to ensure signaling instructions file", "err", err)
			}
		}
	}

	if err := m.ensureAgentInstructions(*w, baseTool); err != nil {
		m.logger.Warn("failed to provision agent instructions", "err", err)
	}

	// Ensure schmux events directory exists for event-based signaling
	eventsDir := filepath.Join(state.SchmuxDataDir(w.Path), "events")
	if err := os.MkdirAll(eventsDir, 0755); err != nil {
		m.logger.Warn("failed to create schmux events directory", "err", err)
	}

	// Deliver attachments into this workspace and point the prompt at them.
	prompt, err := deliverAttachments(w, isChat, opts.Prompt, opts.FileAttachments, opts.Images)
	if err != nil {
		return nil, fmt.Errorf("failed to deliver attachments: %w", err)
	}
	opts.Prompt = prompt

	// Use model from resolution (already populated by ResolveTarget)
	model := resolved.Model

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Inject schmux signaling environment variables. SCHMUX_SESSION_KIND
	// is set explicitly to "chat" for chat spawns and "terminal" for
	// everything else, so the stop-status gate can short-circuit
	// without depending on any other field. Set the terminal value
	// explicitly too: a chat process that respawns a child terminal
	// must not accidentally inherit the chat marker.
	kindMarker := "terminal"
	if isChat {
		kindMarker = "chat"
	}
	resolved.Env = mergeEnvMaps(resolved.Env, map[string]string{
		"SCHMUX_ENABLED":      "1",
		"SCHMUX_SESSION_ID":   sessionID,
		"SCHMUX_WORKSPACE_ID": w.ID,
		"SCHMUX_EVENTS_FILE":  filepath.Join(state.SchmuxDataDir(w.Path), "events", sessionID+".jsonl"),
		"SCHMUX_CONFIG_FILE":  schmuxdir.ConfigPath(),
		"SCHMUX_SESSION_KIND": kindMarker,
	})

	// Write initial spawn event with full prompt
	if opts.Prompt != "" {
		eventsFile := filepath.Join(state.SchmuxDataDir(w.Path), "events", sessionID+".jsonl")
		spawnEvt := events.StatusEvent{
			Type:    "status",
			State:   "working",
			Message: "Session spawned",
			Intent:  opts.Prompt,
		}
		if err := events.AppendEvent(eventsFile, spawnEvt); err != nil {
			m.logger.Warn("failed to write spawn prompt event", "err", err)
		}
	}

	// Determine prompt delivery strategy: tools can set prompt_strategy: send_keys
	// in their descriptor when they ignore positional prompt CLI args in
	// interactive mode. In that case the prompt is typed via tmux send-keys
	// after the tool starts up instead.
	sendKeysPrompt := ""
	commandPrompt := opts.Prompt
	if !isChat && baseTool != "" && strings.TrimSpace(opts.Prompt) != "" && !opts.Resume {
		if adapter := detect.GetAdapter(baseTool); adapter != nil && adapter.PromptDelivery() == detect.PromptSendKeys {
			sendKeysPrompt = opts.Prompt
			commandPrompt = "" // omit from CLI args; will be injected after startup
		}
	}

	var command string
	var chatProto chat.Protocol
	var chatHandshake [][]byte
	chatProtoName := ""
	if isChat {
		command, chatProto, chatHandshake, err = buildChatCommand(resolved, model, opts.Fence, opts.Resume, opts.ResumeID, w.Path)
		if chatProto != nil {
			chatProtoName = chatProto.Name()
		}
	} else {
		command, err = buildCommand(resolved, commandPrompt, model, opts.Resume, opts.Fence, opts.ResumeID)
	}
	if err != nil {
		return nil, err
	}

	// Inject persona prompt if provided
	if opts.PersonaPrompt != "" {
		personaFilePath := filepath.Join(state.SchmuxDataDir(w.Path), fmt.Sprintf("system-prompt-%s.md", sessionID))
		if err := os.WriteFile(personaFilePath, []byte(opts.PersonaPrompt), 0600); err != nil {
			m.logger.Warn("failed to write persona file", "err", err)
		} else {
			command = appendPersonaFlags(command, baseTool, personaFilePath, opts.PersonaPrompt)

			// Inject spawn-time env vars (e.g., OPENCODE_CONFIG_CONTENT for persona)
			if adapter := detect.GetAdapter(baseTool); adapter != nil {
				spawnEnv := adapter.SpawnEnv(detect.SpawnContext{
					WorkspacePath: w.Path,
					SessionID:     sessionID,
					PersonaPath:   personaFilePath,
				})
				if len(spawnEnv) > 0 {
					resolved.Env = mergeEnvMaps(resolved.Env, spawnEnv)
				}
			}
		}
	}

	m.logger.Info("spawn command", "session", sessionID, "target", opts.TargetName, "command_len", len(command))

	// Chat sessions run claude behind a file bridge: the initial prompt and
	// bridge files are recorded before tmux starts the pipeline.
	var chatDir string
	if isChat {
		chatDir = schmuxdir.ChatSessionDir(w.ID, sessionID)
		paths := chat.PathsFor(chatDir)
		seed := ""
		if opts.ChatSeedFrom != "" {
			seed = chat.ConversationPath(schmuxdir.ChatSessionDir(w.ID, opts.ChatSeedFrom))
		}
		if err := prepareChatFiles(paths, seed, chatHandshake); err != nil {
			return nil, err
		}
		command = chat.PipelineCommand(command, paths)
	}

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := opts.Nickname
	if opts.Nickname != "" {
		uniqueNickname = m.generateUniqueNickname(opts.Nickname)
	}

	// Use sanitized unique nickname for tmux session name if provided, otherwise use sessionID
	tmuxSession := sessionID
	if uniqueNickname != "" {
		tmuxSession = sanitizeNickname(uniqueNickname)
	}

	// Create tmux session
	// A chat session's bridge files live outside the workspace, so the
	// sandboxed pipeline needs that one directory writable.
	var extraWritable []string
	if chatDir != "" {
		extraWritable = []string{chatDir}
	}
	command, err = m.wrapForFence(ctx, w.Path, w.ID, sessionID, opts.Fence, opts.FenceCommand, fenceAllowedDomains(resolved), extraWritable, fenceReadablePaths(w.ID, models.CatalogArgPath(resolved.Args)), command)
	if err != nil {
		return nil, err
	}
	// CreateSession reports the pane PID atomically from the creation command,
	// so no follow-up PID query can race the pane's lifecycle.
	pid, err := m.server.CreateSession(ctx, tmuxSession, w.Path, command)
	if err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Inject prompt via send-keys for tools that need it (runs async).
	// Some tools (e.g. Claude Code v2+) ignore positional prompt args in
	// interactive mode. For these, we wait for the tool's input prompt to
	// appear and then type the prompt via tmux send-keys.
	if sendKeysPrompt != "" {
		go m.sendPromptWhenReady(tmuxSession, sendKeysPrompt)
	}

	// Configure status bar: process on left, time on right, clear center
	m.server.ConfigureStatusBar(ctx, tmuxSession)

	// Create session state with cached PID (no Prompt field)
	sess := state.Session{
		ID:           sessionID,
		WorkspaceID:  w.ID,
		Target:       opts.TargetName,
		Nickname:     uniqueNickname,
		PersonaID:    opts.PersonaID,
		StyleID:      opts.StyleID,
		TmuxSession:  tmuxSession,
		TmuxSocket:   m.server.SocketName(),
		CreatedAt:    time.Now(),
		Pid:          pid,
		Fence:        opts.Fence,
		Kind:         opts.Kind,
		ChatProtocol: chatProtoName,
	}

	if err := m.state.AddSession(sess); err != nil {
		return nil, fmt.Errorf("failed to add session to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// Notify compounder about new session
	if m.compoundCallback != nil {
		w, found := m.state.GetWorkspace(sess.WorkspaceID)
		if found {
			m.compoundCallback(w.ID, true)
		}
	}

	m.ensureTrackerFromSession(sess)

	if isChat && (strings.TrimSpace(opts.Prompt) != "" || len(opts.Images) > 0) {
		if rt := m.ensureChatRuntime(sess.ID); rt != nil {
			if _, err := rt.Send(opts.Prompt, opts.Images); err != nil {
				m.logger.Warn("failed to send initial chat message", "session", sess.ID, "err", err)
			}
		}
	}

	// Track session creation
	m.trackSessionCreated(sess.ID, sess.WorkspaceID, sess.Target)

	return &sess, nil
}

// SpawnCommand spawns a session running a raw shell command.
// Used for quick launch presets with a direct command (no target resolution).
func (m *Manager) SpawnCommand(ctx context.Context, opts SpawnOptions) (*state.Session, error) {
	w, err := m.resolveWorkspace(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Ensure schmux events directory exists for event-based signaling
	eventsDirCmd := filepath.Join(state.SchmuxDataDir(w.Path), "events")
	if err := os.MkdirAll(eventsDirCmd, 0755); err != nil {
		m.logger.Warn("failed to create schmux events directory", "err", err)
	}

	// Inject schmux signaling environment variables into the command.
	// SCHMUX_SESSION_KIND is set explicitly so the stop-status gate
	// can short-circuit for chat sessions.
	schmuxEnv := map[string]string{
		"SCHMUX_ENABLED":      "1",
		"SCHMUX_SESSION_ID":   sessionID,
		"SCHMUX_WORKSPACE_ID": w.ID,
		"SCHMUX_EVENTS_FILE":  filepath.Join(state.SchmuxDataDir(w.Path), "events", sessionID+".jsonl"),
		"SCHMUX_CONFIG_FILE":  schmuxdir.ConfigPath(),
		"SCHMUX_SESSION_KIND": "terminal",
	}
	commandWithEnv := fmt.Sprintf("%s %s", buildEnvPrefix(schmuxEnv), opts.Command)

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := opts.Nickname
	if opts.Nickname != "" {
		uniqueNickname = m.generateUniqueNickname(opts.Nickname)
	}

	// Use sanitized unique nickname for tmux session name if provided, otherwise use sessionID
	tmuxSession := sessionID
	if uniqueNickname != "" {
		tmuxSession = sanitizeNickname(uniqueNickname)
	}

	// Create tmux session with the raw command
	commandWithEnv, err = m.wrapForFence(ctx, w.Path, w.ID, sessionID, opts.Fence, opts.FenceCommand, nil, nil, fenceReadablePaths(w.ID, ""), commandWithEnv)
	if err != nil {
		return nil, err
	}
	// CreateSession reports the pane PID atomically from the creation command,
	// so no follow-up PID query can race the pane's lifecycle.
	pid, err := m.server.CreateSession(ctx, tmuxSession, w.Path, commandWithEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Configure status bar: process on left, time on right, clear center
	m.server.ConfigureStatusBar(ctx, tmuxSession)

	// Create session state (Target uses a stable value for command-based sessions)
	sess := state.Session{
		ID:             sessionID,
		WorkspaceID:    w.ID,
		Target:         "command",
		Nickname:       uniqueNickname,
		TmuxSession:    tmuxSession,
		TmuxSocket:     m.server.SocketName(),
		CreatedAt:      time.Now(),
		Pid:            pid,
		Fence:          opts.Fence,
		SignInProtocol: opts.SignInProtocol,
		SignedOut:      opts.SignInProtocol != "",
	}

	if err := m.state.AddSession(sess); err != nil {
		return nil, fmt.Errorf("failed to add session to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	m.ensureTrackerFromSession(sess)

	// Track session creation
	m.trackSessionCreated(sess.ID, sess.WorkspaceID, sess.Target)

	return &sess, nil
}

// ResolveTarget resolves a target name to a command and env.
func (m *Manager) ResolveTarget(_ context.Context, targetName string) (ResolvedTarget, error) {
	// Check if it's a model (handles aliases like "opus", "sonnet", "haiku")
	if m.models != nil && m.models.IsModelID(targetName) {
		resolved, err := m.models.ResolveModel(targetName)
		if err == nil {
			return ResolvedTarget{
				Name:       resolved.Model.ID,
				Kind:       TargetKindModel,
				Command:    resolved.Command,
				Promptable: true,
				Env:        resolved.Env,
				Args:       resolved.Args,
				Model:      &resolved.Model,
				ToolName:   resolved.ToolName,
			}, nil
		}
		// Model exists in catalog but can't be resolved (e.g., no detected tool).
		// Fall through to the builtin tool name check.
	}

	if target, found := m.config.GetRunTarget(targetName); found {
		return ResolvedTarget{
			Name:       target.Name,
			Kind:       TargetKindUser,
			Command:    target.Command,
			Promptable: false,
		}, nil
	}

	// Fallback: registered tool names (e.g., "claude", "codex", or any
	// descriptor-defined tool) can be resolved directly using the tool name
	// as the command, even when the tool binary isn't detected locally.
	if detect.IsToolName(targetName) {
		return ResolvedTarget{
			Name:       targetName,
			Kind:       TargetKindModel,
			Command:    targetName,
			Promptable: true,
			ToolName:   targetName,
		}, nil
	}

	return ResolvedTarget{}, fmt.Errorf("target not found: %s", targetName)
}

func buildCommand(target ResolvedTarget, prompt string, model *detect.Model, resume bool, fence bool, resumeID string) (string, error) {
	trimmedPrompt := strings.TrimSpace(prompt)

	// The resolved harness is authoritative. Do not infer it from target.Name:
	// user-defined command targets are opaque even if their names resemble a
	// built-in harness.
	baseTool := target.ToolName

	// Handle resume mode
	if resume {
		if baseTool == "" {
			return "", fmt.Errorf("resume requires a descriptor-backed target: %s", target.Name)
		}
		var parts []string
		if resumeID != "" {
			// Resume a specific conversation by id. Requires the harness to
			// declare resume_id_args; otherwise we must NOT fall back to the
			// generic resume_args (--continue / resume --last), which could
			// resume the wrong conversation.
			adapter := detect.GetAdapter(baseTool)
			if adapter == nil {
				return "", fmt.Errorf("resume by id requires resume_id_args: %s", baseTool)
			}
			idArgs := adapter.ResumeIDArgs(model, resumeID)
			if idArgs == nil {
				return "", fmt.Errorf("resume by id requires resume_id_args: %s", baseTool)
			}
			parts = append(strings.Fields(target.Command), idArgs...)
		} else {
			var err error
			parts, err = detect.BuildCommandParts(baseTool, target.Command, detect.ToolModeResume, "", model)
			if err != nil {
				return "", err
			}
		}
		// Resume re-applies third-party routing: env alone is not enough for
		// codex (config-routed providers), so the precomputed provider args
		// ride the resume argv too. The model flag itself is already part of
		// resume_args via BuildCommandParts (expandModelPlaceholder appends
		// the descriptor's model_flag when the template has no {model}).
		parts = append(parts, target.Args...)
		// When fenced, append the harness's skip-approvals flags to the
		// resume argv before quoting/joining (descriptor-backed only).
		if fence && baseTool != "" {
			if adapter := detect.GetAdapter(baseTool); adapter != nil {
				parts = append(parts, adapter.AutoApproveArgs()...)
			}
		}
		// Shell-quote each token before joining. Model values can contain spaces
		// and parentheses (e.g. antigravity's "Claude Opus 4.6 (Thinking)"), which
		// the shell would otherwise mangle. QuoteIfNeeded leaves simple commands
		// and flags untouched, so other tools' resume commands are unchanged.
		quoted := make([]string, len(parts))
		for i, p := range parts {
			quoted[i] = shellutil.QuoteIfNeeded(p)
		}
		cmd := strings.Join(quoted, " ")

		// Inject signaling instructions via CLI flag for supported tools
		cmd = appendSignalingFlags(cmd, baseTool)

		// Resume mode still needs model env vars for third-party models
		if len(target.Env) > 0 {
			return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), cmd), nil
		}
		return cmd, nil
	}

	// Build the base command with optional model flag injection
	baseCommand := target.Command
	if model != nil && target.ToolName != "" {
		adapter := detect.GetAdapter(target.ToolName)
		if adapter != nil {
			flag := adapter.ModelFlag()
			if spec, ok := model.RunnerFor(target.ToolName); ok && spec.ModelValue != "" && flag != "" {
				baseCommand = fmt.Sprintf("%s %s %s", baseCommand, flag, shellutil.Quote(spec.ModelValue))
			}
		}
	}

	// Provider routing args for endpoint-routed models, appended after the
	// model flag.
	for _, arg := range target.Args {
		baseCommand = fmt.Sprintf("%s %s", baseCommand, shellutil.QuoteIfNeeded(arg))
	}

	// When fenced, append the harness's skip-approvals flags to the base
	// command before prompt delivery (descriptor-backed only).
	if fence && baseTool != "" {
		if adapter := detect.GetAdapter(baseTool); adapter != nil {
			for _, arg := range adapter.AutoApproveArgs() {
				baseCommand = fmt.Sprintf("%s %s", baseCommand, shellutil.QuoteIfNeeded(arg))
			}
		}
	}

	// Inject signaling instructions via CLI flag for supported tools
	baseCommand = appendSignalingFlags(baseCommand, baseTool)

	if target.Promptable {
		command := baseCommand
		if trimmedPrompt != "" {
			// Use the adapter's prompt flag if available, otherwise positional arg
			promptFlag := ""
			if baseTool != "" {
				if adapter := detect.GetAdapter(baseTool); adapter != nil {
					promptFlag = adapter.PromptFlag()
				}
			}
			if promptFlag != "" {
				command = fmt.Sprintf("%s %s %s", baseCommand, promptFlag, shellutil.Quote(prompt))
			} else {
				command = fmt.Sprintf("%s %s", baseCommand, shellutil.Quote(prompt))
			}
		}
		if len(target.Env) > 0 {
			return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), command), nil
		}
		return command, nil
	}

	if trimmedPrompt != "" {
		return "", fmt.Errorf("prompt is not allowed for command target %s", target.Name)
	}
	if len(target.Env) > 0 {
		return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), baseCommand), nil
	}
	return baseCommand, nil
}

// wrapForFence wraps command in the fence sandbox when enabled. The handler
// has already rejected fence-on requests for which the dependency report says
// fence is unavailable; this guard is local and mechanical and does not
// re-detect dependencies. When disabled, returns today's command untouched.
func (m *Manager) wrapForFence(ctx context.Context, workspacePath, workspaceID, sessionID string, enabled bool, fenceCommand string, allowedDomains []string, extraWritablePaths, extraReadablePaths []string, command string) (string, error) {
	if !enabled {
		return command, nil
	}
	if fenceCommand == "" {
		return "", fmt.Errorf("fence not available")
	}
	var presets, repoDomains []string
	if rc, err := workspace.LoadRepoConfig(workspacePath); err != nil {
		if m.logger != nil {
			m.logger.Warn("fence: failed to load repo config", "err", err)
		}
	} else if rc != nil && rc.Fence != nil {
		presets = rc.Fence.Presets
		repoDomains = rc.Fence.AllowedDomains
		for _, p := range presets {
			if !fence.IsKnownPreset(p) && m.logger != nil {
				m.logger.Warn("fence: unknown preset in .schmux/config.json", "preset", p)
			}
		}
	}
	cfg := fence.Config{
		FenceCommand:       fenceCommand,
		WorkspacePath:      workspacePath,
		ExtraWritablePaths: append(workspace.ExtraWritablePaths(workspacePath), extraWritablePaths...), // git worktree → shared .git; chat → its session dir
		ExtraReadablePaths: extraReadablePaths,                                                         // workspace fence logs + the generated codex catalog, passed structurally by the caller
		AllowedDomains:     append(append([]string{}, repoDomains...), allowedDomains...),
		Presets:            presets,
		DataDir:            schmuxdir.FenceLaunchDir(workspaceID, sessionID),
		SharedCacheDir:     schmuxdir.FenceSharedCacheDir(),
	}
	return fence.Wrap(ctx, cfg, command)
}

// fenceReadablePaths lists extra paths a fenced session must be able to
// read: the workspace's fence monitor logs, plus the generated codex
// catalog when the provider-routing args point the harness at it. The
// catalog path comes from the structured args (models.CatalogArgPath), not
// by scanning the serialized command — a prompt that merely mentions the
// key must not widen the sandbox.
func fenceReadablePaths(workspaceID, catalogPath string) []string {
	paths := []string{schmuxdir.FenceWorkspaceDir(workspaceID)}
	if catalogPath != "" {
		paths = append(paths, catalogPath)
	}
	return paths
}

func fenceAllowedDomains(target ResolvedTarget) []string {
	var domains []string
	if adapter := detect.GetAdapter(target.ToolName); adapter != nil {
		domains = append(domains, adapter.FenceDomains()...) // layer 1: harness control plane
	}
	// layer 2: model / third-party endpoint host
	if target.Model != nil && target.ToolName != "" {
		if spec, ok := target.Model.RunnerFor(target.ToolName); ok && spec.Endpoint != "" {
			if u, err := url.Parse(spec.Endpoint); err == nil {
				if host := strings.ToLower(u.Hostname()); host != "" {
					domains = append(domains, host)
				}
			}
		}
	}
	if len(domains) == 0 {
		return nil
	}
	return domains
}

// appendSignalingFlags appends CLI flags for signaling instruction injection.
// Tools using hooks are skipped because they handle signaling via workspace config.
func appendSignalingFlags(cmd, baseTool string) string {
	adapter := detect.GetAdapter(baseTool)
	if adapter == nil {
		return cmd
	}

	strategy := adapter.SignalingStrategy()

	if strategy == detect.SignalingHooks {
		// Hooks-based tools handle signaling via workspace config, not CLI flags
		return cmd
	}

	if strategy == detect.SignalingCLIFlag {
		sigArgs := adapter.SignalingArgs(ensure.SignalingInstructionsFilePath())
		for _, arg := range sigArgs {
			cmd = fmt.Sprintf("%s %s", cmd, shellutil.Quote(arg))
		}
		return cmd
	}

	// SignalingInstructionFile: no CLI flags needed (handled by ensure package)
	return cmd
}

// appendPersonaFlags injects persona instructions for CLI-flag adapters.
// Config-overlay adapters inject through SpawnEnv instead.
func appendPersonaFlags(cmd, baseTool, personaFilePath, prompt string) string {
	adapter := detect.GetAdapter(baseTool)
	if adapter == nil {
		return cmd
	}

	switch adapter.PersonaInjection() {
	case detect.PersonaCLIFlag:
		args := adapter.PersonaArgs(personaFilePath, prompt)
		for _, arg := range args {
			cmd = fmt.Sprintf("%s %s", cmd, shellutil.Quote(arg))
		}
		return cmd
	default:
		// PersonaInstructionFile delivery remains unimplemented.
		// PersonaConfigOverlay: handled by SpawnEnv (environment variable)
		return cmd
	}
}

// sendPromptWhenReady polls the tmux pane until the tool's input prompt appears,
// then types the user prompt via send-keys. Used for tools that ignore positional
// prompt args in interactive mode (e.g. Claude Code v2+).
func (m *Manager) sendPromptWhenReady(session, prompt string) {
	pollCtx, pollCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pollCancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			m.logger.Warn("send-keys prompt: timed out waiting for input prompt", "session", session)
			// Fall through to send anyway as best-effort
		case <-ticker.C:
			output, err := m.server.CaptureLastLines(pollCtx, session, 5, false)
			if err != nil {
				continue
			}
			// Look for common CLI input indicators (❯ is Claude Code's prompt)
			if !strings.Contains(output, "❯") && !strings.Contains(output, "> ") {
				continue
			}
		}
		break
	}

	// Use a fresh context — pollCtx may already be canceled from timeout.
	sendCtx, sendCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sendCancel()

	if err := m.server.SendText(sendCtx, session, prompt); err != nil {
		m.logger.Warn("send-keys prompt: failed to send", "session", session, "err", err)
	} else {
		m.logger.Info("send-keys prompt: injected", "session", session, "prompt_len", len(prompt))
	}
}

func buildEnvPrefix(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, shellutil.Quote(env[k])))
	}
	return strings.Join(parts, " ")
}

func mergeEnvMaps(base, overrides map[string]string) map[string]string {
	if base == nil && overrides == nil {
		return nil
	}
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

// SessionWorkspaceID returns the recorded workspace for a session.
func (m *Manager) SessionWorkspaceID(sessionID string) string {
	sess, _ := m.state.GetSession(sessionID)
	return sess.WorkspaceID
}

// IsRunning checks whether the agent process is still running, using the cached
// pane PID.
func (m *Manager) IsRunning(ctx context.Context, sessionID string) bool {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return false
	}

	// If we don't have a PID, check if tmux session exists as fallback
	if sess.Pid == 0 {
		server := m.serverForSocket(sess.TmuxSocket)
		if server != nil {
			return server.SessionExists(ctx, sess.TmuxSession)
		}
		return false
	}

	// Check if the process is still running
	process, err := os.FindProcess(sess.Pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}

	return true
}

// MarkSessionDisposing sets a session's status to "disposing" and saves state.
// Called by handlers before the blocking Dispose() call to give immediate visual feedback.
// Returns the previous status (for rollback on failure) and any error.
func (m *Manager) MarkSessionDisposing(sessionID string) (previousStatus string, err error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}
	if sess.Status == state.SessionStatusDisposing {
		return state.SessionStatusDisposing, nil // already disposing — idempotent
	}
	previousStatus = sess.Status
	if !m.state.UpdateSessionFunc(sessionID, func(s *state.Session) {
		s.Status = state.SessionStatusDisposing
	}) {
		return previousStatus, fmt.Errorf("session disappeared during status update: %s", sessionID)
	}
	if err := m.state.Save(); err != nil {
		return previousStatus, fmt.Errorf("failed to save state: %w", err)
	}
	return previousStatus, nil
}

// RevertSessionStatus restores a session's status after a failed disposal.
func (m *Manager) RevertSessionStatus(sessionID, previousStatus string) {
	m.state.UpdateSessionFunc(sessionID, func(s *state.Session) {
		s.Status = previousStatus
	})
	if err := m.state.Save(); err != nil {
		m.logger.Error("failed to save state after status revert", "session_id", sessionID, "err", err)
	}
}

// Dispose disposes of a session.
func (m *Manager) Dispose(ctx context.Context, sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Capture terminal output BEFORE killing the session. Chat sessions have
	// no terminal runtime to capture from — their record is the history.
	if !sess.IsChat() && m.terminalCaptureCallback != nil && ctx.Err() == nil {
		captureCtx, captureCancel := context.WithTimeout(ctx, 5*time.Second)
		var output string
		var err error
		server := m.serverForSocket(sess.TmuxSocket)
		if server != nil {
			output, err = server.CaptureOutput(captureCtx, sess.TmuxSession)
		}
		captureCancel()
		if err != nil {
			m.logger.Warn("failed to capture terminal output", "session", sessionID, "err", err)
		} else if output != "" {
			m.terminalCaptureCallback(sessionID, sess.WorkspaceID, output)
		}
	}

	// Kill tmux session (tmux sends SIGHUP to all processes - handles cleanup)
	// If session exists but kill fails, return error to avoid orphaning processes
	disposeServer := m.serverForSocket(sess.TmuxSocket)
	sessionExists := false
	if disposeServer != nil {
		sessionExists = disposeServer.SessionExists(ctx, sess.TmuxSession)
	}

	// Enumerate the fenced process tree BEFORE killing tmux — the fenced
	// agent lives outside the pane's process group, so tmux's SIGHUP alone
	// can leave it running (see reaper.go).
	// The live pane PID from tmux is the enumeration root; the persisted
	// sess.Pid is written once at spawn and could since have been reused.
	var fencedProcs []procIdent
	if sess.Fence && sessionExists {
		panePID, err := disposeServer.GetPanePID(ctx, sess.TmuxSession)
		if err != nil {
			m.logger.Warn("fenced dispose: cannot resolve pane pid, skipping reap", "session", sessionID, "err", err)
		} else if fencedProcs, err = m.reaper.enumerate(ctx, panePID); err != nil {
			m.logger.Warn("fenced dispose: enumeration failed, skipping reap", "session", sessionID, "pane_pid", panePID, "err", err)
			fencedProcs = nil
		} else {
			var pids []string
			for _, p := range fencedProcs {
				pids = append(pids, strconv.Itoa(p.PID))
			}
			m.logger.Info("fenced dispose: enumerated tree", "session", sessionID, "tmux_session", sess.TmuxSession, "pids", pids)
		}
	}

	// A fenced session's shells can orphan processes: a nohup server, a
	// script the agent left running. Those are no longer under the pane, so
	// the enumeration above misses them. Find them by session ID and dead
	// group leader, and add them to what the reap kills. Do this whether or
	// not tmux is still alive.
	if sess.Fence {
		orphans, err := m.reaper.orphans(ctx, sessionID)
		if err != nil {
			m.logger.Warn("fenced dispose: orphan scan failed", "session", sessionID, "err", err)
		} else if len(orphans) > 0 {
			var pids []string
			for _, p := range orphans {
				pids = append(pids, strconv.Itoa(p.PID))
			}
			m.logger.Info("fenced dispose: found orphans", "session", sessionID, "pids", pids)
			fencedProcs = append(fencedProcs, orphans...)
		}
	}

	if sessionExists {
		if err := disposeServer.KillSession(ctx, sess.TmuxSession); err != nil {
			return fmt.Errorf("failed to kill tmux session %s: %w", sess.TmuxSession, err)
		}
	}

	if len(fencedProcs) > 0 {
		// Killing tmux was the point of no return: the reap phase runs under
		// a manager-owned context so a caller's cancellation (restart passes
		// the request context; startup recovery uses a fixed 30s) cannot
		// abandon a half-signaled tree.
		grace := m.config.DisposeGracePeriod()
		reapCtx, reapCancel := context.WithTimeout(context.Background(), grace+10*time.Second)
		report, reapErr := m.reaper.reap(reapCtx, fencedProcs, grace)
		reapCancel()
		m.logger.Info("fenced dispose: reap finished",
			"session", sessionID,
			"enumerated", len(report.Enumerated),
			"graceful_exit", report.GracefulExit,
			"grace_elapsed", report.GraceElapsed,
			"sigterm_groups", report.SigtermGroups,
			"sigterm_pids", report.SigtermPIDs,
			"sigkill_groups", report.SigkillGroups,
			"sigkill_pids", report.SigkillPIDs,
			"survivors", len(report.Survivors))
		if reapErr != nil {
			return fmt.Errorf("session %s: %w", sessionID, reapErr)
		}
	}

	m.stopTracker(sessionID)

	// Note: workspace is NOT cleaned up on session disposal.
	// Workspaces persist and are only reset when reused for a new spawn.

	// Remove session from state
	if err := m.state.RemoveSession(sessionID); err != nil {
		return fmt.Errorf("failed to remove session from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Chat transport cleanup. The process, reap, and state paths have
	// already succeeded by this point, so a cleanup failure must not fail
	// the dispose — the periodic cleaner will retry on its next pass.
	// The conversation record is intentionally left in place: the retention
	// cleaner owns its lifetime, and Restart still seeds from it within the
	// one-hour grace period.
	if sess.IsChat() {
		paths := chat.PathsFor(schmuxdir.ChatSessionDir(sess.WorkspaceID, sess.ID))
		if err := chat.RemoveBridgeFiles(paths); err != nil {
			m.logger.Warn("failed to clean chat bridge files",
				"session", sessionID, "err", err)
		}
	}

	// Notify compounder if this was the last session for the workspace
	isLastSession := true
	for _, s := range m.state.GetSessions() {
		if s.WorkspaceID == sess.WorkspaceID {
			isLastSession = false
			break
		}
	}
	if m.compoundCallback != nil && isLastSession {
		m.compoundCallback(sess.WorkspaceID, false)
	}

	// Notify autolearn system on session dispose (always fires; daemon decides based on config)
	if m.autolearnCallback != nil {
		w, found := m.state.GetWorkspace(sess.WorkspaceID)
		if found {
			// Find repo name from URL
			repoConfig, repoFound := m.repos.GetByURL(w.Repo)
			if repoFound {
				go m.autolearnCallback(repoConfig.Name, w.Repo, isLastSession)
			}
		}
	}

	m.logger.Info("disposed session", "session", sessionID)

	return nil
}

// GetAttachCommand returns the tmux attach command for a session.
func (m *Manager) GetAttachCommand(sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	server := m.serverForSocket(sess.TmuxSocket)
	if server != nil {
		return server.GetAttachCommand(sess.TmuxSession), nil
	}
	return fmt.Sprintf("tmux attach -t \"=%s\"", sess.TmuxSession), nil
}

// GetAttachArgs returns structured attach command components for safe exec.Command construction.
// Unlike GetAttachCommand (which returns a formatted string), this avoids shell injection.
func (m *Manager) GetAttachArgs(sessionID string) (binary, socketName, sessionName string, err error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", "", "", fmt.Errorf("session not found: %s", sessionID)
	}
	server := m.serverForSocket(sess.TmuxSocket)
	if server == nil {
		return "", "", "", fmt.Errorf("no tmux server available")
	}
	return server.Binary(), server.SocketName(), sess.TmuxSession, nil
}

// GetOutput returns the current terminal output for a session.
func (m *Manager) GetOutput(ctx context.Context, sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	server := m.serverForSocket(sess.TmuxSocket)
	if server != nil {
		return server.CaptureOutput(ctx, sess.TmuxSession)
	}
	return "", fmt.Errorf("no tmux server available")
}

// GetPlainOutput returns the session's full terminal scrollback without ANSI
// formatting. It is intended for diagnostics that consume terminal content as
// text rather than replaying it in a terminal.
func (m *Manager) GetPlainOutput(ctx context.Context, sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	server := m.serverForSocket(sess.TmuxSocket)
	if server != nil {
		return server.CaptureOutputPlain(ctx, sess.TmuxSession)
	}
	return "", fmt.Errorf("no tmux server available")
}

// GetAllSessions returns all sessions.
func (m *Manager) GetAllSessions() []state.Session {
	return m.state.GetSessions()
}

// GetSession returns a session by ID.
func (m *Manager) GetSession(sessionID string) (*state.Session, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return &sess, nil
}

// RenameSession updates a session's nickname and renames the tmux session.
// The nickname is sanitized before use as the tmux session name.
// Returns an error if the new nickname conflicts with an existing session.
func (m *Manager) RenameSession(ctx context.Context, sessionID, newNickname string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Check if new nickname conflicts with an existing session
	if conflictingID := m.nicknameExists(newNickname, sessionID); conflictingID != "" {
		return fmt.Errorf("%w: %q (conflicts with session %s)", ErrNicknameInUse, newNickname, conflictingID)
	}

	oldTmuxName := sess.TmuxSession
	newTmuxName := oldTmuxName
	if newNickname != "" {
		newTmuxName = sanitizeNickname(newNickname)
	}

	// Rename the tmux session
	var renameErr error
	server := m.serverForSocket(sess.TmuxSocket)
	if server != nil {
		renameErr = server.RenameSession(ctx, oldTmuxName, newTmuxName)
	}
	if renameErr != nil {
		return fmt.Errorf("failed to rename tmux session: %w", renameErr)
	}

	// Update session state
	sess.Nickname = newNickname
	sess.TmuxSession = newTmuxName
	if err := m.state.UpdateSession(sess); err != nil {
		return fmt.Errorf("failed to update session in state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	m.updateTrackerSessionName(sessionID, newTmuxName)

	return nil
}

var (
	nicknameDisallowedChars = regexp.MustCompile(`[^a-zA-Z0-9 _\-+*?#%^~@/,<>()\[\]{}|!]`)
)

// sanitizeNickname converts a user-supplied nickname into a string that
// satisfies tmux.ValidateSessionName: alphanumerics, space, and a limited
// punctuation set that is safe inside a double-quoted shell string.
// Disallowed characters are replaced with '-', then leading/trailing
// dashes/spaces are trimmed to keep the result tmux-valid.
func sanitizeNickname(nickname string) string {
	result := nicknameDisallowedChars.ReplaceAllString(nickname, "-")
	result = strings.TrimLeft(result, "- ")
	result = strings.TrimRight(result, "- ")
	return result
}

// nicknameExists checks if a nickname (or its sanitized tmux session name) already exists.
// Returns the conflicting session ID if found, empty string otherwise.
// excludeSessionID is used during rename to skip the session being renamed.
func (m *Manager) nicknameExists(nickname, excludeSessionID string) string {
	if nickname == "" {
		return ""
	}
	tmuxName := sanitizeNickname(nickname)
	if tmuxName == "" {
		return ""
	}
	sessions := m.state.GetSessions()
	excludedTmuxName := ""
	for _, sess := range sessions {
		// Skip the session being edited (for rename operations)
		if sess.ID == excludeSessionID {
			excludedTmuxName = sess.TmuxSession
			continue
		}
		// Check if tmux session name matches (nicknames are sanitized for tmux)
		if sess.TmuxSession == tmuxName {
			return sess.ID
		}
	}
	socketName := ""
	if m.server != nil {
		socketName = m.server.SocketName()
	}
	if server := m.serverForSocket(socketName); server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if tmuxName != excludedTmuxName && server.SessionExists(ctx, tmuxName) {
			return tmuxName
		}
	}
	return ""
}

// generateUniqueNickname generates a unique nickname by trying the base name,
// then "name (1)", "name (2)", etc. until a unique name is found.
func (m *Manager) generateUniqueNickname(baseNickname string) string {
	if baseNickname == "" {
		return ""
	}
	baseNickname = sanitizeNickname(baseNickname)
	if baseNickname == "" {
		return ""
	}
	// Try base name first
	if m.nicknameExists(baseNickname, "") == "" {
		return baseNickname
	}
	// Try numbered suffixes
	for i := 1; i <= maxNicknameAttempts; i++ {
		candidate := fmt.Sprintf("%s (%d)", baseNickname, i)
		if m.nicknameExists(candidate, "") == "" {
			return candidate
		}
	}
	// Fallback: use base nickname with a UUID suffix (should never happen in practice)
	return fmt.Sprintf("%s-%s", baseNickname, uuid.New().String()[:8])
}

// EnsureTracker makes sure a running tracker exists for the session.
func (m *Manager) EnsureTracker(sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	m.ensureTrackerFromSession(sess)
	return nil
}

// GetTracker returns the tracker for a session, creating one if needed.
func (m *Manager) GetTracker(sessionID string) (*SessionRuntime, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if sess.IsChat() {
		return nil, ErrChatSession
	}
	return m.ensureTrackerFromSession(sess), nil
}

// CaptureLastLines captures the last N lines from a session's terminal.
// It tries the session tracker first,
// then falls back to direct tmux CLI for local sessions.
func (m *Manager) CaptureLastLines(ctx context.Context, sessionID string, lines int) (string, error) {
	tracker, err := m.GetTracker(sessionID)
	if err != nil {
		return "", err
	}
	result, trackerErr := tracker.CaptureLastLines(ctx, lines)
	if trackerErr == nil {
		return result, nil
	}
	// Fallback to direct tmux CLI
	sess, ok := m.state.GetSession(sessionID)
	if !ok {
		return "", trackerErr
	}
	server := m.ServerForSocket(sess.TmuxSocket)
	if server == nil {
		return "", trackerErr
	}
	return server.CaptureLastLines(ctx, sess.TmuxSession, lines, true)
}

// GetCursorState returns the cursor position, visibility, and terminal mode state for a session.
// It tries the session tracker first, then falls back to direct tmux CLI.
func (m *Manager) GetCursorState(ctx context.Context, sessionID string) (controlmode.CursorState, error) {
	tracker, trackerErr := m.GetTracker(sessionID)
	if trackerErr != nil {
		return controlmode.CursorState{}, trackerErr
	}
	curState, trackerErr := tracker.GetCursorState(ctx)
	if trackerErr == nil {
		return curState, nil
	}
	// Fallback to direct tmux CLI
	sess, ok := m.state.GetSession(sessionID)
	if !ok {
		return controlmode.CursorState{}, trackerErr
	}
	server := m.ServerForSocket(sess.TmuxSocket)
	if server == nil {
		return controlmode.CursorState{}, trackerErr
	}
	cliState, cliErr := server.GetCursorState(ctx, sess.TmuxSession)
	if cliErr != nil {
		// tmux.CursorState and controlmode.CursorState have the same fields
		return controlmode.CursorState{}, cliErr
	}
	return controlmode.CursorState{
		X:             cliState.X,
		Y:             cliState.Y,
		Visible:       cliState.Visible,
		AlternateOn:   cliState.AlternateOn,
		MouseStandard: cliState.MouseStandard,
		MouseButton:   cliState.MouseButton,
		MouseAny:      cliState.MouseAny,
		MouseSGR:      cliState.MouseSGR,
	}, nil
}

// wireRecorder sets up the timelapse recorder factory on a tracker if recording is enabled.
// Optional width/height override the default 80x24 (e.g. when the local tmux pane size is known).
func (m *Manager) wireRecorder(tracker *SessionRuntime, sessionID string, dims ...int) {
	if m.recorderFactory == nil {
		return
	}
	factory := m.recorderFactory
	paneWidth, paneHeight := 80, 24
	if len(dims) >= 2 {
		paneWidth, paneHeight = dims[0], dims[1]
	}
	tracker.RecorderFactory = func(ol *OutputLog, gapCh <-chan SourceEvent) Runnable {
		return factory(sessionID, ol, gapCh, paneWidth, paneHeight)
	}
}

func (m *Manager) ensureTrackerFromSession(sess state.Session) *SessionRuntime {
	m.mu.Lock()
	if existing := m.trackers[sess.ID]; existing != nil {
		existing.SetTmuxSession(sess.TmuxSession)
		m.mu.Unlock()
		return existing
	}

	// Chat sessions get a conversation bridge runtime, not a terminal
	// runtime. Callers that need the tracker for terminal I/O are gated by
	// GetTracker's ErrChatSession; the restore/EnsureTracker paths only
	// require side effects, which ensureChatRuntime provides.
	if sess.IsChat() {
		m.mu.Unlock()
		m.ensureChatRuntime(sess.ID)
		return nil
	}

	// Build event file path from workspace path
	var eventFilePath string
	if ws, found := m.workspace.GetByID(sess.WorkspaceID); found && ws.Path != "" {
		eventFilePath = filepath.Join(state.SchmuxDataDir(ws.Path), "events", sess.ID+".jsonl")
	}

	var outputCb func([]byte)
	if m.outputCallback != nil {
		sessionID := sess.ID
		handler := m.outputCallback
		outputCb = func(chunk []byte) {
			handler(sessionID, chunk)
		}
	}

	ls := NewLocalSource(sess.ID, sess.TmuxSession, m.serverForSocket(sess.TmuxSocket), m.logger)
	ls.Start()

	tracker := NewSessionRuntime(sess.ID, ls, m.state, eventFilePath, m.eventHandlers, outputCb, m.logger)

	// Query actual tmux pane size for accurate recording dimensions
	if srv := m.serverForSocket(sess.TmuxSocket); srv != nil {
		ctx := context.Background()
		if w, h, err := srv.GetPaneSize(ctx, sess.TmuxSession); err == nil {
			m.wireRecorder(tracker, sess.ID, w, h)
		} else {
			m.wireRecorder(tracker, sess.ID)
		}
	} else {
		m.wireRecorder(tracker, sess.ID)
	}

	m.trackers[sess.ID] = tracker

	m.mu.Unlock()

	tracker.Start()
	m.notifyTrackerCallback(tracker)
	return tracker
}

// ensureChatRuntime returns the chat runtime for a chat session, creating and
// starting it if needed. Returns nil when the session or its workspace is
// unknown.
func (m *Manager) ensureChatRuntime(sessionID string) *chat.Runtime {
	m.mu.Lock()
	if rt := m.chatRuntimes[sessionID]; rt != nil {
		m.mu.Unlock()
		return rt
	}
	m.mu.Unlock()

	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil
	}
	ws, found := m.workspace.GetByID(sess.WorkspaceID)
	if !found || ws.Path == "" {
		return nil
	}
	// Hook events stay in the workspace (the hooks write them there); the
	// conversation and bridge live under ~/.schmux/chat/<workspace>/<session>.
	// Pasted chat images and dashboard previews live in the workspace cache,
	// readable inside the fence and removed with the workspace.
	eventsFile := filepath.Join(state.SchmuxDataDir(ws.Path), "events", sess.ID+".jsonl")
	proto, err := chat.ProtocolFor(sess.EffectiveChatProtocol())
	if err != nil {
		m.logger.Warn("chat protocol", "session", sess.ID, "err", err)
		return nil
	}
	imageCacheDir := filepath.Join(state.SchmuxDataDir(ws.Path), "cache", "chat-images")
	rt, err := chat.NewRuntime(sess.ID, proto, chat.PathsFor(schmuxdir.ChatSessionDir(sess.WorkspaceID, sess.ID)), imageCacheDir, eventsFile, m.eventHandlers, m.logger)
	if err != nil {
		m.logger.Warn("failed to create chat runtime", "session", sess.ID, "err", err)
		return nil
	}
	// Wire the Nudge callback before Start so the rebuilt snapshot
	// emitted on Start reaches the dashboard. The forwarder closes
	// over the session id; the stored callback may be nil if the
	// dashboard has not registered itself yet (in which case the
	// runtime simply does not publish).
	rt.SetNudgeCallback(makeChatNudgeForwarder(sess.ID, m.chatNudgeCallback))
	rt.SetTurnErrorCallback(makeChatTurnErrorForwarder(sess.ID, m.chatTurnErrorCallback))
	rt.SetActivityCallback(sess.CreatedAt, func(at time.Time) {
		m.state.UpdateSessionLastOutput(sess.ID, at)
		if m.chatActivityCallback != nil {
			m.chatActivityCallback()
		}
	})

	m.mu.Lock()
	if existing := m.chatRuntimes[sessionID]; existing != nil {
		m.mu.Unlock()
		rt.Stop()
		return existing
	}
	rt.SetUsageCallback(makeChatUsageForwarder(sess.ID, m.chatUsageCallback))
	m.chatRuntimes[sessionID] = rt
	m.mu.Unlock()

	rt.Start()
	return rt
}

// GetChatRuntime returns the chat runtime for a chat session, creating it if
// needed. Terminal sessions get an error.
func (m *Manager) GetChatRuntime(sessionID string) (*chat.Runtime, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if !sess.IsChat() {
		return nil, fmt.Errorf("session %s is not a chat session", sessionID)
	}
	rt := m.ensureChatRuntime(sessionID)
	if rt == nil {
		return nil, fmt.Errorf("chat runtime unavailable for %s", sessionID)
	}
	return rt, nil
}

// Stop stops all running session trackers, killing their tmux attach-client processes.
func (m *Manager) Stop() {
	m.mu.Lock()
	trackers := m.trackers
	m.trackers = make(map[string]*SessionRuntime)
	chatRuntimes := m.chatRuntimes
	m.chatRuntimes = make(map[string]*chat.Runtime)
	m.mu.Unlock()
	for _, tracker := range trackers {
		tracker.Stop()
	}
	for _, rt := range chatRuntimes {
		rt.Stop()
	}
}

func (m *Manager) stopTracker(sessionID string) {
	m.mu.Lock()
	tracker := m.trackers[sessionID]
	delete(m.trackers, sessionID)
	rt := m.chatRuntimes[sessionID]
	delete(m.chatRuntimes, sessionID)
	m.mu.Unlock()
	if tracker != nil {
		tracker.Stop()
	}
	if rt != nil {
		// Record the cut-off point before tearing the runtime down: a turn that
		// never receives a `result` closes here. Daemon shutdown bypasses this
		// path entirely (Manager.Stop stops runtimes directly).
		if err := rt.End(); err != nil {
			m.logger.Warn("failed to record chat session end", "session", sessionID, "err", err)
		}
		rt.Stop()
	}
}

func (m *Manager) updateTrackerSessionName(sessionID, tmuxSession string) {
	m.mu.RLock()
	tracker := m.trackers[sessionID]
	m.mu.RUnlock()
	if tracker != nil {
		tracker.SetTmuxSession(tmuxSession)
	}
}
