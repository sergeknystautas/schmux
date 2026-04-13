package dashboard

import (
	"context"
	"net/http"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// GitHandlers groups HTTP handlers for git/VCS operations including
// commit graph, diff, staging, sync, inspect, and VS Code integration.
type GitHandlers struct {
	config        *config.Config
	state         state.StateStore
	workspace     workspace.WorkspaceManager
	remoteManager *remote.Manager
	tmuxServer    *tmux.TmuxServer
	logger        *log.Logger
	shutdownCtx   context.Context

	// Callbacks into Server methods that cannot be extracted.
	broadcastSessions                        func()
	broadcastWorkspaceUnlockedWithSyncResult func(workspaceID string, result *workspace.LinearSyncResult, err error)
	pauseViteWatch                           func()
	resumeViteWatch                          func()
	requireWorkspace                         func(w http.ResponseWriter, r *http.Request) (state.Workspace, bool)
	vcsTypeForWorkspace                      func(ws state.Workspace) string

	// Linear sync conflict resolution callbacks.
	getLinearSyncResolveConflictState    func(workspaceID string) *LinearSyncResolveConflictState
	setLinearSyncResolveConflictState    func(workspaceID string, state *LinearSyncResolveConflictState)
	deleteLinearSyncResolveConflictState func(workspaceID string)
	setCRTracker                         func(tmuxName string, tracker *session.SessionRuntime)
	getCRTracker                         func(tmuxName string) *session.SessionRuntime
	deleteCRTracker                      func(tmuxName string)
	cleanupCRTrackers                    func(crState *LinearSyncResolveConflictState)
}
