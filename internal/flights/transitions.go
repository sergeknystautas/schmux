package flights

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/spawnlaunch"
	"github.com/sergeknystautas/schmux/internal/state"
)

// Workflow transitions are explicit user actions. They never read session
// activity or agent signals. Each one records an operation: the pending record
// is persisted before any external call, and the outcome is persisted once the
// call returns, so a crash leaves the attempt visible.

func randomToken(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("flights: crypto/rand: %v", err))
	}
	return prefix + hex.EncodeToString(b)
}

func (f *Flight) operation(id string) *Operation {
	for i := range f.Operations {
		if f.Operations[i].ID == id {
			return &f.Operations[i]
		}
	}
	return nil
}

func (f *Flight) sessionFor(role SessionRole) *SessionAssignment {
	for i := range f.Sessions {
		if f.Sessions[i].Role == role {
			return &f.Sessions[i]
		}
	}
	return nil
}

// upsertSession records the outcome of one spawn attempt for a role, reusing
// the role's assignment on retry and keeping earlier session IDs as history.
func (f *Flight) upsertSession(role SessionRole, target, sessionID string, spawnErr error, now time.Time) {
	s := f.sessionFor(role)
	if s == nil {
		f.Sessions = append(f.Sessions, SessionAssignment{ID: randomToken("sa-"), Role: role, CreatedAt: now})
		s = &f.Sessions[len(f.Sessions)-1]
	}
	s.Target = target
	s.UpdatedAt = now
	if spawnErr != nil {
		s.Outcome = StatusFailed
		s.Error = spawnErr.Error()
		return
	}
	if s.SessionID != "" && s.SessionID != sessionID {
		s.PriorSessionIDs = append(s.PriorSessionIDs, s.SessionID)
	}
	s.SessionID = sessionID
	s.Outcome = StatusSucceeded
	s.Error = ""
}

func (o *Operation) addResource(r ResourceOutcome, now time.Time) {
	o.Resources = append(o.Resources, r)
	o.UpdatedAt = now
}

func resourceOutcome(kind, id string, err error) ResourceOutcome {
	if err != nil {
		return ResourceOutcome{Kind: kind, ID: id, Status: StatusFailed, Error: err.Error()}
	}
	return ResourceOutcome{Kind: kind, ID: id, Status: StatusSucceeded}
}

// requireIdle rejects transitions while destructive cleanup is under way.
func requireIdle(f *Flight) error {
	if f.CleanupState != CleanupIdle {
		return conflict("flight %s is %s", f.ID, f.CleanupState)
	}
	return nil
}

// requireWorkspace rejects operations that depend on a flight's assigned
// workspace when none is recorded. It lets the operation surface a clear
// error instead of dereferencing a nil pointer.
func requireWorkspace(f *Flight) error {
	if f.Workspace == nil {
		return conflict("flight %s has no workspace; take off first", f.ID)
	}
	return nil
}

// begin validates the flight's state and persists a pending operation.
func (m *Manager) begin(id string, action OperationAction, resource string, check func(f *Flight) error) (Flight, string, error) {
	opID := randomToken("op-")
	err := m.editFlight(id, func(f *Flight) error {
		if err := requireIdle(f); err != nil {
			return err
		}
		if err := check(f); err != nil {
			return err
		}
		now := m.deps.Now()
		f.Operations = append(f.Operations, Operation{
			ID: opID, Action: action, CorrelationID: randomToken("corr-"), Status: StatusPending,
			Resource: resource, CreatedAt: now, UpdatedAt: now,
		})
		return nil
	})
	if err != nil {
		return Flight{}, "", err
	}
	f, _ := m.Get(id)
	return f, opID, nil
}

// progress persists an intermediate change while an operation is still pending.
func (m *Manager) progress(id, opID string, mutate func(f *Flight, op *Operation, now time.Time)) (Flight, error) {
	err := m.editFlight(id, func(f *Flight) error {
		mutate(f, f.operation(opID), m.deps.Now())
		return nil
	})
	if err != nil {
		return Flight{}, err
	}
	f, _ := m.Get(id)
	return f, nil
}

// finish persists the operation's outcome together with any final flight change.
// It returns opErr when the operation failed, or the persistence error.
func (m *Manager) finish(id, opID string, opErr error, mutate func(f *Flight, now time.Time)) (Flight, error) {
	err := m.editFlight(id, func(f *Flight) error {
		now := m.deps.Now()
		if mutate != nil {
			mutate(f, now)
		}
		op := f.operation(opID)
		op.UpdatedAt = now
		if opErr != nil {
			op.Status = StatusFailed
			op.Error = opErr.Error()
		} else {
			op.Status = StatusSucceeded
		}
		return nil
	})
	if err != nil {
		return Flight{}, err
	}
	if opErr != nil {
		return Flight{}, opErr
	}
	f, _ := m.Get(id)
	return f, nil
}

// decide applies a transition that needs no external call, recording it as one
// succeeded operation.
func (m *Manager) decide(id string, action OperationAction, check func(f *Flight) error, apply func(f *Flight, now time.Time)) (Flight, error) {
	err := m.editFlight(id, func(f *Flight) error {
		if err := requireIdle(f); err != nil {
			return err
		}
		if err := check(f); err != nil {
			return err
		}
		now := m.deps.Now()
		apply(f, now)
		f.Operations = append(f.Operations, Operation{
			ID: randomToken("op-"), Action: action, CorrelationID: randomToken("corr-"),
			Status: StatusSucceeded, CreatedAt: now, UpdatedAt: now,
		})
		return nil
	})
	if err != nil {
		return Flight{}, err
	}
	f, _ := m.Get(id)
	return f, nil
}

func displayName(f Flight) string {
	if f.ShortName != "" {
		return f.ShortName
	}
	return f.ID
}

// Takeoff allocates the flight's workspace and spawns the planner. It moves
// backlog → planning once the workspace assignment is recorded, and is
// retryable while planning has no successful planner. Every takeoff asks the
// workspace manager for the flight's branch; the manager reuses an existing
// workspace when one matches, so retries are idempotent unless the recorded
// workspace was disposed by hand.
func (m *Manager) Takeoff(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	f, opID, err := m.begin(flightID, ActionTakeoff, "workspace", func(f *Flight) error {
		switch f.WorkflowState {
		case StateBacklog:
			return nil
		case StatePlanning:
			return nil
		}
		return conflict("flight %s is %s; only backlog or planning flights can take off", f.ID, f.WorkflowState)
	})
	if err != nil {
		return Flight{}, err
	}

	repo, found := m.deps.Repos.FindRepoByURL(f.Repo)
	if !found {
		return m.finish(flightID, opID, fmt.Errorf("repository %q is no longer configured", f.Repo), nil)
	}
	branch := "schmux/flight/" + f.ID
	label := ""
	if repo.VCS == "sapling" {
		branch = "" // sapling has no branch concept; its backend picks a label-based one
		label = displayName(f)
	}
	ws, err := m.deps.Workspaces.GetOrCreateWithLabel(ctx, f.Repo, branch, label)
	if err != nil {
		return m.finish(flightID, opID, fmt.Errorf("allocate workspace: %w", err), func(f *Flight, now time.Time) {
			f.Operations[len(f.Operations)-1].addResource(resourceOutcome("workspace", "", err), now)
		})
	}
	artifactPath := filepath.Join(state.SchmuxDataDirForVCS(ws.Path, ws.VCS), "flights", f.ID, "plan.md")
	f, err = m.progress(flightID, opID, func(f *Flight, op *Operation, now time.Time) {
		// Reuse an existing assignment on retry so the flight's planner session
		// remains attached to the same assignment record. A retry that
		// allocates a fresh workspace replaces the workspace-targeted fields.
		if f.Workspace == nil {
			f.Workspace = &WorkspaceAssignment{
				ID: randomToken("wa-"), CreatedAt: now,
			}
		}
		f.Workspace.RequestedBranch = branch
		f.Workspace.WorkspaceID = ws.ID
		f.Workspace.Path = ws.Path
		f.Workspace.Branch = ws.Branch
		f.Workspace.VCS = ws.VCS
		f.Workspace.Outcome = StatusSucceeded
		f.Workspace.UpdatedAt = now
		f.ArtifactPath = artifactPath
		f.WorkflowState = StatePlanning
		op.addResource(resourceOutcome("workspace", ws.ID, nil), now)
	})
	if err != nil {
		return Flight{}, err
	}

	if err := os.MkdirAll(filepath.Dir(f.ArtifactPath), 0o755); err != nil {
		return m.finish(flightID, opID, fmt.Errorf("create planning artifact directory: %w", err), nil)
	}

	res, spawnErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.PlannerModel,
		Prompt:      PlannerPrompt(f.Prompt, f.ArtifactPath),
		Nickname:    "planner",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	var opErr error
	if spawnErr != nil {
		opErr = fmt.Errorf("spawn planner: %w", spawnErr)
	}
	return m.finish(flightID, opID, opErr, func(f *Flight, now time.Time) {
		f.upsertSession(RolePlanner, f.PlannerModel, res.SessionID, spawnErr, now)
		f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", res.SessionID, spawnErr), now)
	})
}

// EnterExecution starts the executor from the planning artifact. It is
// retryable while the flight stays in planning.
func (m *Manager) EnterExecution(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	f, opID, err := m.begin(flightID, ActionExecution, "session", func(f *Flight) error {
		if f.WorkflowState != StatePlanning {
			return conflict("flight %s is %s; only planning flights can enter execution", f.ID, f.WorkflowState)
		}
		if err := requireWorkspace(f); err != nil {
			return err
		}
		if s := f.sessionFor(RolePlanner); s == nil || s.Outcome != StatusSucceeded {
			return conflict("flight %s has no running planner session; take off first", f.ID)
		}
		return nil
	})
	if err != nil {
		return Flight{}, err
	}

	if err := checkArtifact(f.ArtifactPath); err != nil {
		return m.finish(flightID, opID, fmt.Errorf("%w: %v", ErrConflict, err), nil)
	}

	res, spawnErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.ExecutorModel,
		Prompt:      ExecutorPrompt(f.Prompt, f.ArtifactPath),
		Nickname:    "executor",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	var opErr error
	if spawnErr != nil {
		opErr = fmt.Errorf("spawn executor: %w", spawnErr)
	}
	return m.finish(flightID, opID, opErr, func(f *Flight, now time.Time) {
		f.upsertSession(RoleExecutor, f.ExecutorModel, res.SessionID, spawnErr, now)
		f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", res.SessionID, spawnErr), now)
		if spawnErr == nil {
			f.WorkflowState = StateExecuting
		}
	})
}

// checkArtifact verifies the planning artifact is a readable, nonempty file.
// It does not judge the plan's content.
func checkArtifact(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("planning artifact %s is not readable: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("planning artifact %s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("planning artifact %s is not readable: %w", path, err)
	}
	defer file.Close()
	var first [1]byte
	if _, err := file.Read(first[:]); errors.Is(err, io.EOF) {
		return fmt.Errorf("planning artifact %s is empty", path)
	} else if err != nil {
		return fmt.Errorf("planning artifact %s is not readable: %w", path, err)
	}
	return nil
}

// CommitPrompt is what the commit agent is told: run the repository's commit
// skill. The skill owns inspection, staging, pre-commit checks, and the
// message; the flight only starts the agent.
const CommitPrompt = "/commit"

// EnterCompletion starts another agent in the flight's workspace to run the
// repository's commit skill, using the executor's harness. It is retryable
// while the flight stays in executing. Launching the agent is not commit
// success; the user watches it on its session page.
func (m *Manager) EnterCompletion(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	f, opID, err := m.begin(flightID, ActionCompletion, "session", func(f *Flight) error {
		if f.WorkflowState != StateExecuting {
			return conflict("flight %s is %s; only executing flights can enter completion", f.ID, f.WorkflowState)
		}
		return requireWorkspace(f)
	})
	if err != nil {
		return Flight{}, err
	}

	res, spawnErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.ExecutorModel,
		Prompt:      CommitPrompt,
		Nickname:    "commit",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	var opErr error
	if spawnErr != nil {
		opErr = fmt.Errorf("spawn commit agent: %w", spawnErr)
	}
	return m.finish(flightID, opID, opErr, func(f *Flight, now time.Time) {
		f.upsertSession(RoleCommit, f.ExecutorModel, res.SessionID, spawnErr, now)
		f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", res.SessionID, spawnErr), now)
		if spawnErr == nil {
			f.WorkflowState = StateCompleting
		}
	})
}

// MarkDone records the user's decision that a completing flight is finished.
// It does not inspect the commit session.
func (m *Manager) MarkDone(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	return m.decide(flightID, ActionDone, func(f *Flight) error {
		if f.WorkflowState != StateCompleting {
			return conflict("flight %s is %s; only completing flights can be marked done", f.ID, f.WorkflowState)
		}
		return nil
	}, func(f *Flight, now time.Time) {
		f.WorkflowState = StateDone
		f.DoneAt = &now
	})
}

// Cancel records the user's decision to abandon a started flight. Resources are
// left in place; terminate removes them.
func (m *Manager) Cancel(ctx context.Context, flightID, reason string) (Flight, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Flight{}, invalid("a cancellation reason is required")
	}
	unlock := m.lockFlight(flightID)
	defer unlock()

	return m.decide(flightID, ActionCancel, func(f *Flight) error {
		switch f.WorkflowState {
		case StatePlanning, StateExecuting, StateCompleting:
			return nil
		case StateBacklog:
			return conflict("flight %s is in the backlog; delete it instead", f.ID)
		}
		return conflict("flight %s is already %s", f.ID, f.WorkflowState)
	}, func(f *Flight, now time.Time) {
		f.WorkflowState = StateCancelled
		f.CancelReason = reason
		f.CancelledAt = &now
	})
}
