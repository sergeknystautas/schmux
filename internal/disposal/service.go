// Package disposal is the one destructive-cleanup path for a workspace and
// everything attached to it. The dashboard's dispose-all endpoint and flight
// termination both call it; neither keeps a private copy of the sequencing.
package disposal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	// sessionDisposeTimeout is deliberately generous and independent of the
	// interactive dispose grace period: bulk disposal under CPU or IO
	// contention needs headroom for tmux subprocess work.
	sessionDisposeTimeout = 2 * time.Minute
	// workspaceDisposeTimeout accommodates large worktrees on slow filesystems
	// (NFS, FUSE).
	workspaceDisposeTimeout = 5 * time.Minute
)

// Request describes one full-workspace disposal.
type Request struct {
	WorkspaceID string
	// DeleteRemoteBranch deletes the workspace's remote branch first, before
	// anything is marked or destroyed. A failure aborts with nothing touched.
	DeleteRemoteBranch bool
	// ForcePurge destroys the workspace without recycling and removes its
	// local branch. It also makes a session disposal failure fatal, so a live
	// session is never left behind a purged workspace.
	ForcePurge bool
	// ChatSessionIDs are the chat session directories under WorkspaceID that
	// must be deleted along with the workspace. When non-empty, sessions
	// enumerated live in the workspace at call time are also deleted. When
	// empty, no chat record is touched. Other sessions stored under the same
	// workspace ID are left in place.
	ChatSessionIDs []string
	// BranchRepo and BranchName retain a flight branch identity after its
	// workspace record disappears, so a retry can finish local branch cleanup.
	BranchRepo string
	BranchName string
}

// Result reports what a disposal did.
type Result struct {
	SessionsDisposed       []string
	ChatDirectoriesDeleted int
	// WorkspacePurged is true when this call disposed the workspace.
	WorkspacePurged bool
	// WorkspaceMissing is true when the workspace was already absent.
	WorkspaceMissing bool
	// AlreadyDisposing is true when a plain (non-ForcePurge) request found the
	// workspace already being disposed and did nothing.
	AlreadyDisposing bool
}

// ResourceOutcome is one resource step's result. Kind is one of session,
// chat_storage, preview, workspace, or branch.
type ResourceOutcome struct {
	Kind string
	ID   string
	Err  error
}

// Sessions disposes sessions.
type Sessions interface {
	MarkSessionDisposing(sessionID string) (previousStatus string, err error)
	Dispose(ctx context.Context, sessionID string) error
}

// Workspaces disposes workspaces.
type Workspaces interface {
	MarkWorkspaceDisposing(workspaceID string) (previousStatus string, err error)
	RevertWorkspaceStatus(workspaceID, previousStatus string)
	DeleteRemoteBranch(ctx context.Context, workspaceID string) error
	DisposeForce(ctx context.Context, workspaceID string) error
	DisposeForcePurge(ctx context.Context, workspaceID string) error
	CleanupFlightBranch(ctx context.Context, repoURL, branch string) error
}

// Previews cleans up workspace previews.
type Previews interface {
	DeleteWorkspace(workspaceID string) error
}

// State is the read side of the state store.
type State interface {
	GetSessions() []state.Session
	GetSession(id string) (state.Session, bool)
	GetWorkspace(id string) (state.Workspace, bool)
}

// Service performs full-workspace disposal.
type Service struct {
	Sessions   Sessions
	Workspaces Workspaces
	Previews   Previews // optional
	State      State
	// Broadcast pushes a dashboard update. Optional.
	Broadcast func()
	// SessionDisposed runs after each successful session disposal, e.g. to
	// drop per-session locks. Optional.
	SessionDisposed func(sessionID string)
	Logger          *log.Logger
}

// DisposeAll disposes every session in the workspace, then the workspace.
//
// Only the remote-branch step runs on ctx. Everything after it runs on an
// independent context with its own timeouts, so a disposal that has started
// completes even if the caller (an HTTP client) goes away.
//
// observe, if non-nil, is called synchronously after each completed or failed
// resource step, so a caller can persist progress as it happens. It must not
// call back into the Service.
func (s *Service) DisposeAll(ctx context.Context, req Request, observe func(ResourceOutcome)) (Result, error) {
	if observe == nil {
		observe = func(ResourceOutcome) {}
	}
	log := logging.Sub(s.Logger, "workspace")
	id := req.WorkspaceID
	var res Result

	if req.DeleteRemoteBranch {
		err := s.Workspaces.DeleteRemoteBranch(ctx, id)
		observe(ResourceOutcome{Kind: "branch", ID: id, Err: err})
		if err != nil {
			log.Error("dispose-all remote branch delete failed", "workspace_id", id, "err", err)
			return res, err
		}
	}

	// Mark the workspace disposing for immediate feedback. A plain request
	// that finds a disposal already under way has nothing to add; a force
	// purge resumes it.
	prevStatus, markErr := s.Workspaces.MarkWorkspaceDisposing(id)
	if markErr != nil {
		log.Warn("mark disposing failed, proceeding with dispose", "workspace_id", id, "err", markErr)
	} else if prevStatus == state.WorkspaceStatusDisposing && !req.ForcePurge {
		res.AlreadyDisposing = true
		return res, nil
	}
	revert := func() {
		if markErr == nil {
			s.Workspaces.RevertWorkspaceStatus(id, prevStatus)
			s.broadcastAsync()
		}
	}

	var sessionIDs []string
	for _, sess := range s.State.GetSessions() {
		if sess.WorkspaceID != id {
			continue
		}
		sessionIDs = append(sessionIDs, sess.ID)
		if _, err := s.Sessions.MarkSessionDisposing(sess.ID); err != nil {
			log.Warn("failed to mark session disposing", "session_id", sess.ID, "err", err)
		}
	}
	// Broadcast immediately — everything grays out at once.
	if s.Broadcast != nil {
		s.Broadcast()
	}

	// Dispose all sessions concurrently.
	type outcome struct {
		id  string
		err error
	}
	outcomes := make(chan outcome, len(sessionIDs))
	for _, sid := range sessionIDs {
		go func(sid string) {
			dctx, cancel := context.WithTimeout(context.Background(), sessionDisposeTimeout)
			defer cancel()
			outcomes <- outcome{id: sid, err: s.Sessions.Dispose(dctx, sid)}
		}(sid)
	}
	var sessionErrs []error
	for range sessionIDs {
		o := <-outcomes
		if o.err != nil {
			// A session that vanished while we were disposing it is cleaned.
			if _, stillThere := s.State.GetSession(o.id); !stillThere {
				o.err = nil
			}
		}
		observe(ResourceOutcome{Kind: "session", ID: o.id, Err: o.err})
		if o.err != nil {
			log.Error("dispose-all session failed", "session_id", o.id, "err", o.err)
			sessionErrs = append(sessionErrs, fmt.Errorf("dispose session %s: %w", o.id, o.err))
			continue
		}
		log.Info("dispose-all session disposed", "session_id", o.id)
		res.SessionsDisposed = append(res.SessionsDisposed, o.id)
		if s.SessionDisposed != nil {
			s.SessionDisposed(o.id)
		}
	}
	if req.ForcePurge && len(sessionErrs) > 0 {
		revert()
		return res, errors.Join(sessionErrs...)
	}

	// Chat records survive an ordinary dispose-all; only a caller that names
	// sessions to delete (flight disposal) removes them, and then the
	// sessions found live in the workspace go with them.
	if len(req.ChatSessionIDs) > 0 {
		seen := make(map[string]struct{}, len(req.ChatSessionIDs)+len(sessionIDs))
		toDelete := make([]string, 0, len(req.ChatSessionIDs)+len(sessionIDs))
		for _, sid := range req.ChatSessionIDs {
			if sid == "" {
				continue
			}
			if _, ok := seen[sid]; ok {
				continue
			}
			seen[sid] = struct{}{}
			toDelete = append(toDelete, sid)
		}
		for _, sid := range sessionIDs {
			if _, ok := seen[sid]; ok {
				continue
			}
			seen[sid] = struct{}{}
			toDelete = append(toDelete, sid)
		}
		n, err := chat.DeleteConversations(id, toDelete)
		observe(ResourceOutcome{Kind: "chat_storage", ID: id, Err: err})
		if err != nil {
			log.Error("dispose-all chat history delete failed", "workspace_id", id, "err", err)
			revert()
			return res, err
		}
		res.ChatDirectoriesDeleted = n
	}

	// Dispose the workspace and any retained flight branch on an independent
	// context: session disposal above may have consumed part of the caller's budget.
	wctx, cancel := context.WithTimeout(context.Background(), workspaceDisposeTimeout)
	defer cancel()
	if _, present := s.State.GetWorkspace(id); !present {
		res.WorkspaceMissing = true
		observe(ResourceOutcome{Kind: "workspace", ID: id})
	} else {
		var err error
		if req.ForcePurge {
			err = s.Workspaces.DisposeForcePurge(wctx, id)
		} else {
			// Skip safety checks: the user explicitly asked to destroy
			// everything, and the sessions were disposed above.
			err = s.Workspaces.DisposeForce(wctx, id)
		}
		observe(ResourceOutcome{Kind: "workspace", ID: id, Err: err})
		if err != nil {
			log.Error("dispose-all workspace failed", "workspace_id", id, "err", err)
			revert()
			return res, err
		}
		res.WorkspacePurged = true
	}

	if req.ForcePurge && req.BranchRepo != "" && req.BranchName != "" {
		err := s.Workspaces.CleanupFlightBranch(wctx, req.BranchRepo, req.BranchName)
		observe(ResourceOutcome{Kind: "branch", ID: req.BranchName, Err: err})
		if err != nil {
			log.Error("dispose-all flight branch cleanup failed", "repo", req.BranchRepo, "branch", req.BranchName, "err", err)
			return res, err
		}
	}

	if s.Previews != nil {
		err := s.Previews.DeleteWorkspace(id)
		observe(ResourceOutcome{Kind: "preview", ID: id, Err: err})
		if err != nil {
			logging.Sub(s.Logger, "preview").Warn("dispose-all cleanup failed", "workspace_id", id, "err", err)
		}
	}
	log.Info("dispose-all success", "workspace_id", id, "sessions_disposed", len(res.SessionsDisposed))
	s.broadcastAsync()
	return res, nil
}

func (s *Service) broadcastAsync() {
	if s.Broadcast != nil {
		go s.Broadcast()
	}
}
