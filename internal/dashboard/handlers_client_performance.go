package dashboard

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

//go:embed client_performance_prompt.md
var clientPerformancePrompt string

// clientPerformanceBranch is fixed so the recording browser lands in the same
// workspace every time and never adopts one of the user's own branches.
const clientPerformanceBranch = "client-performance"

// ClientPerformanceHandlers ensures the performance chat exists. The daemon
// stores nothing about it; the browser sends the ids it kept.
type ClientPerformanceHandlers struct {
	config    *config.Config
	state     state.StateStore
	workspace workspace.WorkspaceManager
	session   *session.Manager
	logger    *log.Logger

	// Seams for tests; production wiring points at the managers.
	hasChatMode func(target string) bool
	getOrCreate func(ctx context.Context, repoURL, branch string) (*state.Workspace, error)
	spawn       func(ctx context.Context, opts session.SpawnOptions) (*state.Session, error)
	isRunning   func(ctx context.Context, sessionID string) bool
}

func newClientPerformanceHandlers(s *Server) *ClientPerformanceHandlers {
	h := &ClientPerformanceHandlers{config: s.config, state: s.state, workspace: s.workspace, session: s.session, logger: s.logger}
	m := s.models
	h.hasChatMode = func(target string) bool {
		adapter := detect.GetAdapter(resolveTargetTool(m, target))
		return adapter != nil && adapter.ChatArgs(nil, false, "") != nil
	}
	h.getOrCreate = s.workspace.GetOrCreate
	h.spawn = s.session.Spawn
	h.isRunning = s.session.IsRunning
	return h
}

// handleEnsureSession is POST /api/client-performance/session.
func (h *ClientPerformanceHandlers) handleEnsureSession(w http.ResponseWriter, r *http.Request) {
	var req contracts.ClientPerformanceSessionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	if !h.config.GetClientPerformanceEnabled() {
		writeJSONError(w, "client performance recording is disabled", http.StatusBadRequest)
		return
	}
	repoName := h.config.GetClientPerformanceRepo()
	if repoName == "" {
		writeJSONError(w, "client_performance.repo is not set", http.StatusBadRequest)
		return
	}
	repoURL := ""
	for _, repo := range h.config.GetRepos() {
		if repo.Name == repoName {
			repoURL = repo.URL
			break
		}
	}
	if repoURL == "" {
		writeJSONError(w, fmt.Sprintf("client_performance.repo %s is not a configured repo", repoName), http.StatusBadRequest)
		return
	}
	target := h.config.GetClientPerformanceTarget()
	if target == "" {
		writeJSONError(w, "client_performance.target is not set", http.StatusBadRequest)
		return
	}
	if !h.config.GetChatSessions() {
		writeJSONError(w, "chat sessions are disabled (chat_sessions)", http.StatusBadRequest)
		return
	}
	if !h.hasChatMode(target) {
		writeJSONError(w, fmt.Sprintf("target %s has no chat mode", target), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if req.SessionID != "" && h.isRunning(ctx, req.SessionID) {
		writeJSON(w, contracts.ClientPerformanceSessionResponse(req))
		return
	}

	var ws *state.Workspace
	if req.WorkspaceID != "" {
		if cand, ok := h.workspace.GetByID(req.WorkspaceID); ok &&
			cand.Status != state.WorkspaceStatusDisposing &&
			cand.Repo == repoURL && cand.Branch == clientPerformanceBranch {
			ws = cand
		}
	}
	if ws == nil {
		created, err := h.getOrCreate(ctx, repoURL, clientPerformanceBranch)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("failed to prepare workspace: %v", err), http.StatusInternalServerError)
			return
		}
		ws = created
	}
	if !isSchmuxWorkspace(ws.Path) {
		writeJSONError(w, fmt.Sprintf("workspace %s is not a schmux checkout", ws.ID), http.StatusBadRequest)
		return
	}

	sess, err := h.spawn(ctx, session.SpawnOptions{
		WorkspaceID: ws.ID,
		TargetName:  target,
		Prompt:      clientPerformancePrompt,
		Kind:        state.SessionKindChat,
	})
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to spawn performance chat: %v", err), http.StatusInternalServerError)
		return
	}
	h.logger.Info("client performance chat ready", "workspace_id", ws.ID, "session_id", sess.ID)
	writeJSON(w, contracts.ClientPerformanceSessionResponse{WorkspaceID: ws.ID, SessionID: sess.ID})
}
