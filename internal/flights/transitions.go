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

// requireNoPendingTransition rejects a transition while another one is
// unresolved. A pending operation has not settled: its session may or may not
// exist, so a second transition must not start. The operation's identity is
// part of the error, so the user can reconcile it. Disposal keeps its own
// cleanup and retry path and is not covered here.
func requireNoPendingTransition(f *Flight) error {
	for i := range f.Operations {
		op := &f.Operations[i]
		if op.Status != StatusPending {
			continue
		}
		switch op.Action {
		case ActionTakeoff, ActionExecution, ActionReview, ActionNextReview, ActionCompletion:
			return conflict("flight %s has an unresolved %s operation %s", f.ID, op.Action, op.ID)
		}
	}
	return nil
}

// advanceRequest is the pilot's request to advance the flight's phase: the
// turn that decided it, the pilot session the turn came from, and the phase
// visit the decision belongs to. A request with no source is the user's own
// action.
type advanceRequest struct {
	SourceTurnID   string
	PilotSessionID string
	PhaseVisit     uint64
}

// sourceOutcome is what the flight already recorded for a pilot turn's
// advance request.
type sourceOutcome int

const (
	// sourceUnseen means the flight recorded nothing for the turn.
	sourceUnseen sourceOutcome = iota
	// sourceConsumed means the decision was already applied.
	sourceConsumed
	// sourceUnresolved means the attempt's outcome is not established.
	sourceUnresolved
	// sourceFailed means the attempt failed definitely and may be retried.
	sourceFailed
)

// sourceOperation reports what the flight already recorded for a pilot turn.
// A pending operation outranks a succeeded one: an unresolved attempt is
// never treated as consumed.
func (f *Flight) sourceOperation(sourceTurnID string) sourceOutcome {
	outcome := sourceUnseen
	for i := range f.Operations {
		op := &f.Operations[i]
		if op.SourceTurnID != sourceTurnID {
			continue
		}
		switch op.Status {
		case StatusPending:
			return sourceUnresolved
		case StatusSucceeded:
			outcome = sourceConsumed
		case StatusFailed:
			if outcome == sourceUnseen {
				outcome = sourceFailed
			}
		}
	}
	return outcome
}

// operationForTurn returns the operation a pilot turn decided, if the flight
// recorded one.
func (f *Flight) operationForTurn(sourceTurnID string) *Operation {
	for i := range f.Operations {
		if f.Operations[i].SourceTurnID == sourceTurnID {
			return &f.Operations[i]
		}
	}
	return nil
}

// begin validates the flight's state and persists a pending operation. A
// source request is recorded in the operation before any external call, so
// the deciding pilot turn is part of the operation's durable identity.
func (m *Manager) begin(id string, action OperationAction, resource string, source *advanceRequest, check func(f *Flight) error) (Flight, string, error) {
	opID := randomToken("op-")
	err := m.editFlight(id, func(f *Flight) error {
		if err := requireIdle(f); err != nil {
			return err
		}
		if err := requireNoPendingTransition(f); err != nil {
			return err
		}
		if err := check(f); err != nil {
			return err
		}
		now := m.deps.Now()
		op := Operation{
			ID: opID, Action: action, CorrelationID: randomToken("corr-"), Status: StatusPending,
			Resource: resource, CreatedAt: now, UpdatedAt: now,
		}
		if source != nil {
			op.SourceTurnID = source.SourceTurnID
			op.PhaseVisit = source.PhaseVisit
		}
		f.Operations = append(f.Operations, op)
		return nil
	})
	if err != nil {
		return Flight{}, "", err
	}
	f, _ := m.Get(id)
	return f, opID, nil
}

// retainUncertainSpawn records what is known about an attempt whose outcome
// cannot be established and leaves its operation pending: a retry must not
// start a second session. The operation's identity is reported so the user
// can reconcile it. When the record cannot be persisted either, the known
// identifiers are logged rather than claimed as saved.
func (m *Manager) retainUncertainSpawn(flightID, opID string, action OperationAction, sessionID string, spawnErr error) error {
	_, err := m.progress(flightID, opID, func(f *Flight, op *Operation, now time.Time) {
		op.addResource(resourceOutcome("session", sessionID, spawnErr), now)
	})
	if err != nil {
		m.logger.Error("flights: retain uncertain spawn failed",
			"flight", flightID, "operation", opID, "action", string(action), "session", sessionID, "err", err)
	}
	return conflict("flight %s has an unresolved %s operation %s; reconcile it before retrying", flightID, action, opID)
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

func displayName(f Flight) string {
	if f.ShortName != "" {
		return f.ShortName
	}
	return f.ID
}

// Takeoff allocates the flight's workspace and spawns the pilot then the
// planner. It moves backlog → planning once the workspace assignment is
// recorded. A planning flight cannot take off again, including after a spawn
// failure or the loss of a session or workspace.
func (m *Manager) Takeoff(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	// The pilot model is flight-manager configuration, read fresh per takeoff.
	// Without a usable one, nothing is allocated or spawned.
	pilotTarget := m.deps.Pilot.GetFlightPilotTarget()
	if pilotTarget == "" {
		return Flight{}, conflict("no pilot model is configured; choose one in Settings > Flights > Pilot Supervision")
	}
	if err := m.deps.Targets.CheckChat([]string{pilotTarget}); err != nil {
		return Flight{}, conflict("pilot model %q: %v", pilotTarget, err)
	}

	f, opID, err := m.begin(flightID, ActionTakeoff, "workspace", nil, func(f *Flight) error {
		if f.WorkflowState == StateBacklog {
			return nil
		}
		return conflict("flight %s is %s; only backlog flights can take off", f.ID, f.WorkflowState)
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
	f, err = m.progress(flightID, opID, func(f *Flight, op *Operation, now time.Time) {
		// A manually returned backlog flight can retain an assignment.
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
		setWorkflowState(f, StatePlanning, now)
		op.addResource(resourceOutcome("workspace", ws.ID, nil), now)
	})
	if err != nil {
		return Flight{}, err
	}

	// Persist the pilot before starting its planner.
	pilotRes, pilotErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  pilotTarget,
		Prompt:      PilotPrompt(f),
		Nickname:    "pilot",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	if pilotErr != nil {
		return m.finish(flightID, opID, fmt.Errorf("spawn pilot: %w", pilotErr), func(f *Flight, now time.Time) {
			f.upsertSession(RolePilot, pilotTarget, pilotRes.SessionID, pilotErr, now)
			f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", pilotRes.SessionID, pilotErr), now)
		})
	}

	f, err = m.progress(flightID, opID, func(f *Flight, op *Operation, now time.Time) {
		f.upsertSession(RolePilot, pilotTarget, pilotRes.SessionID, nil, now)
		op.addResource(resourceOutcome("session", pilotRes.SessionID, nil), now)
	})
	if err != nil {
		return Flight{}, err
	}

	plannerRes, plannerErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.PlannerModel,
		Prompt:      PlannerPrompt(f.Prompt),
		Nickname:    "planner",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	var opErr error
	if plannerErr != nil {
		opErr = fmt.Errorf("spawn planner: %w", plannerErr)
	}
	return m.finish(flightID, opID, opErr, func(f *Flight, now time.Time) {
		f.upsertSession(RolePlanner, f.PlannerModel, plannerRes.SessionID, plannerErr, now)
		f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", plannerRes.SessionID, plannerErr), now)
	})
}

// EnterExecution starts the executor from the planning artifact. It is
// retryable while the flight stays in planning.
func (m *Manager) EnterExecution(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()
	return m.enterExecutionLocked(ctx, flightID, nil)
}

// enterExecutionLocked starts the executor. The caller holds the flight's
// operation lock. A source request is the pilot's advance decision: it is
// checked against what the flight already recorded for that turn before the
// phase is looked at, so a decision that was already consumed never starts a
// second executor.
func (m *Manager) enterExecutionLocked(ctx context.Context, flightID string, source *advanceRequest) (Flight, error) {
	if source != nil {
		f, ok := m.Get(flightID)
		if !ok {
			return Flight{}, ErrNotFound
		}
		switch f.sourceOperation(source.SourceTurnID) {
		case sourceConsumed:
			return f, nil
		case sourceUnresolved:
			return f, conflict("flight %s has an unresolved operation for pilot turn %s", f.ID, source.SourceTurnID)
		}
	}

	f, opID, err := m.begin(flightID, ActionExecution, "session", source, func(f *Flight) error {
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

	// The spec and plan are wherever the planning skills put them, as the
	// planner last reported and the daemon verified. The report is
	// information for the executor's prompt, not a gate: the user decides
	// when to execute, and an executor started without a verified report is
	// told so and finds the documents itself.
	//
	// The report goes beside the plan when one is known, else under review/.
	// Fixing it here means a later planner report cannot move it, and the
	// start time lets the daemon reject a file left over from before execution.
	started := m.deps.Now()
	reportTarget := executionReportPath(f.PlanPath)
	if f.PlanPath == "" {
		reportTarget = filepath.Join(f.Workspace.Path, "review", f.ID+"-execution.md")
	}
	res, spawnErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.ExecutorModel,
		Prompt:      ExecutorPrompt(f.Prompt, f.SpecPath, f.PlanPath, reportTarget),
		Nickname:    "executor",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	var opErr error
	if spawnErr != nil {
		opErr = fmt.Errorf("spawn executor: %w", spawnErr)
	}
	if spawnErr != nil && ctx.Err() != nil {
		// The daemon cannot prove that no session was created: the attempt's
		// outcome is unknown, so the operation stays pending and a retry must
		// not start a second executor.
		return Flight{}, m.retainUncertainSpawn(flightID, opID, ActionExecution, res.SessionID, spawnErr)
	}
	executing, err := m.finish(flightID, opID, opErr, func(f *Flight, now time.Time) {
		f.upsertSession(RoleExecutor, f.ExecutorModel, res.SessionID, spawnErr, now)
		f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", res.SessionID, spawnErr), now)
		if spawnErr == nil {
			setWorkflowState(f, StateExecuting, now)
			f.ExecutionStartedAt = &started
			f.ReportTarget = reportTarget
			f.ReportPath = ""
		}
	})
	if err != nil {
		if opErr != nil && errors.Is(err, opErr) {
			// The spawn failed and the failure was recorded: the flight stays
			// in its source phase and the attempt may be retried explicitly.
			return Flight{}, err
		}
		// The result could not be persisted: the attempt's outcome is
		// unknown, so the operation stays pending.
		return Flight{}, fmt.Errorf("flight %s has an unresolved %s operation %s: %w", flightID, ActionExecution, opID, err)
	}
	pilot := executing.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID == "" || pilot.Outcome != StatusSucceeded {
		return executing, errors.New("record execution notice: flight has no assigned pilot")
	}
	if _, err := m.recordFlightMessage(flightID, FlightMessage{
		Kind:               "execution_notice",
		RecipientSessionID: pilot.SessionID,
		Text:               ExecutionNotice(executing, res.SessionID),
		Status:             MessageStatusPending,
	}); err != nil {
		return executing, fmt.Errorf("record execution notice: %w", err)
	}
	m.kickMonitor()
	recorded, _ := m.Get(flightID)
	return recorded, nil
}

// executionReportPath is the plan path with its extension replaced by
// "-execution.md": the report sits beside the plan it implements.
func executionReportPath(planPath string) string {
	return strings.TrimSuffix(planPath, filepath.Ext(planPath)) + "-execution.md"
}

// checkArtifact verifies a planning document is a readable, nonempty file.
// It does not judge the plan's content.
func checkArtifact(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s is not readable: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s is not readable: %w", path, err)
	}
	defer file.Close()
	var first [1]byte
	if _, err := file.Read(first[:]); errors.Is(err, io.EOF) {
		return fmt.Errorf("%s is empty", path)
	} else if err != nil {
		return fmt.Errorf("%s is not readable: %w", path, err)
	}
	return nil
}

// EnterReview starts a fresh reviewer in the flight's workspace. The reviewer
// uses the planner's model, reviews and fixes the work, and reports back. It
// is retryable while the flight stays in executing; a spawn failure leaves
// the flight where it was.
func (m *Manager) EnterReview(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()
	return m.enterReviewLocked(ctx, flightID, nil)
}

// enterReviewLocked starts a fresh reviewer. The caller holds the flight's
// operation lock. A source request is the pilot's advance decision: it is
// checked against what the flight already recorded for that turn before the
// phase is looked at, so a decision that was already consumed never starts a
// second reviewer.
func (m *Manager) enterReviewLocked(ctx context.Context, flightID string, source *advanceRequest) (Flight, error) {
	if source != nil {
		f, ok := m.Get(flightID)
		if !ok {
			return Flight{}, ErrNotFound
		}
		switch f.sourceOperation(source.SourceTurnID) {
		case sourceConsumed:
			return f, nil
		case sourceUnresolved:
			return f, conflict("flight %s has an unresolved operation for pilot turn %s", f.ID, source.SourceTurnID)
		}
	}

	f, opID, err := m.begin(flightID, ActionReview, "session", source, func(f *Flight) error {
		if f.WorkflowState != StateExecuting {
			return conflict("flight %s is %s; only executing flights can enter review", f.ID, f.WorkflowState)
		}
		return requireWorkspace(f)
	})
	if err != nil {
		return Flight{}, err
	}

	res, spawnErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.PlannerModel,
		Prompt:      ReviewerPrompt(f, ""),
		Nickname:    "reviewer",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	var opErr error
	if spawnErr != nil {
		opErr = fmt.Errorf("spawn reviewer: %w", spawnErr)
	}
	if spawnErr != nil && ctx.Err() != nil {
		return Flight{}, m.retainUncertainSpawn(flightID, opID, ActionReview, res.SessionID, spawnErr)
	}
	reviewing, err := m.finish(flightID, opID, opErr, func(f *Flight, now time.Time) {
		f.upsertSession(RoleReviewer, f.PlannerModel, res.SessionID, spawnErr, now)
		f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", res.SessionID, spawnErr), now)
		if spawnErr == nil {
			setWorkflowState(f, StateReviewing, now)
			f.ReviewStartedAt = &now
		}
	})
	if err != nil {
		if opErr != nil && errors.Is(err, opErr) {
			return Flight{}, err
		}
		return Flight{}, fmt.Errorf("flight %s has an unresolved %s operation %s: %w", flightID, ActionReview, opID, err)
	}
	pilot := reviewing.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID == "" || pilot.Outcome != StatusSucceeded {
		return reviewing, errors.New("record review notice: flight has no assigned pilot")
	}
	if _, err := m.recordFlightMessage(flightID, FlightMessage{
		Kind:               "review_notice",
		RecipientSessionID: pilot.SessionID,
		Text:               ReviewNotice(reviewing, res.SessionID),
		Status:             MessageStatusPending,
	}); err != nil {
		return reviewing, fmt.Errorf("record review notice: %w", err)
	}
	m.kickMonitor()
	recorded, _ := m.Get(flightID)
	return recorded, nil
}

// EnterCompletion starts another agent in the flight's workspace to run the
// repository's commit skill, using the executor's harness. It is retryable
// while the flight stays in reviewing. Launching the agent is not commit
// success; the user watches it on its session page.
func (m *Manager) EnterCompletion(ctx context.Context, flightID string) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	f, opID, err := m.begin(flightID, ActionCompletion, "session", nil, func(f *Flight) error {
		if f.WorkflowState != StateReviewing {
			return conflict("flight %s is %s; only reviewing flights can enter completion", f.ID, f.WorkflowState)
		}
		return requireWorkspace(f)
	})
	if err != nil {
		return Flight{}, err
	}

	res, spawnErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.ExecutorModel,
		Prompt:      CommitterPrompt(f),
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
			setWorkflowState(f, StateCompleting, now)
			// The reviewer current at this moment is the one that reviewed
			// the work; a reviewer spawned later is a gate-failure reviewer.
			if reviewer := f.sessionFor(RoleReviewer); reviewer != nil {
				f.Operations[len(f.Operations)-1].addResource(ResourceOutcome{Kind: "reviewer", ID: reviewer.SessionID, Status: StatusSucceeded}, now)
			}
		}
	})
}

// nextReviewRequest is the pilot's request for a fresh reviewer: the
// paragraph the reviewer receives and the pilot turn that decided it.
type nextReviewRequest struct {
	Paragraph      string
	SourceTurnID   string
	PilotSessionID string
}

// startNextReviewerOperation starts a fresh reviewer for the flight, as the
// pilot's next_reviewer decision asked. The superseded reviewer keeps running:
// it is never disposed, messaged, or interrupted.
func (m *Manager) startNextReviewerOperation(ctx context.Context, flightID string, req nextReviewRequest) (Flight, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	if m.hasNextReviewerMessage(flightID, req.SourceTurnID) {
		// The decision was already applied; a replay must not start a second
		// reviewer.
		f, _ := m.Get(flightID)
		return f, nil
	}

	f, opID, err := m.begin(flightID, ActionNextReview, "session", nil, func(f *Flight) error {
		if f.WorkflowState != StateReviewing && f.WorkflowState != StateCompleting {
			return conflict("flight %s is %s; only reviewing or completing flights can start another reviewer", f.ID, f.WorkflowState)
		}
		if err := requireWorkspace(f); err != nil {
			return err
		}
		if s := f.sessionFor(RoleReviewer); s == nil || s.SessionID == "" || s.Outcome != StatusSucceeded {
			return conflict("flight %s has no running reviewer", f.ID)
		}
		pilot := f.sessionFor(RolePilot)
		if pilot == nil || pilot.SessionID != req.PilotSessionID || pilot.Outcome != StatusSucceeded {
			return conflict("response is not from the assigned pilot")
		}
		return nil
	})
	if err != nil {
		return Flight{}, err
	}

	res, spawnErr := m.deps.Spawner.LocalTarget(ctx, spawnlaunch.LocalTargetRequest{
		WorkspaceID: f.Workspace.WorkspaceID,
		TargetName:  f.PlannerModel,
		Prompt:      ReviewerPrompt(f, req.Paragraph),
		Nickname:    "reviewer",
		Kind:        state.SessionKindChat,
		Fence:       m.deps.Settings.FenceMode() == config.FenceModeOptionalOn && m.deps.Settings.FenceInstalled(),
	})
	var opErr error
	if spawnErr != nil {
		opErr = fmt.Errorf("spawn reviewer: %w", spawnErr)
	}
	reviewing, err := m.finish(flightID, opID, opErr, func(f *Flight, now time.Time) {
		if spawnErr == nil {
			// The superseded reviewer becomes history; its session keeps running.
			f.upsertSession(RoleReviewer, f.PlannerModel, res.SessionID, nil, now)
		}
		f.Operations[len(f.Operations)-1].addResource(resourceOutcome("session", res.SessionID, spawnErr), now)
	})
	if err != nil {
		return Flight{}, err
	}
	if spawnErr != nil {
		return reviewing, opErr
	}
	pilot := reviewing.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID == "" || pilot.Outcome != StatusSucceeded {
		return reviewing, errors.New("record next reviewer: flight has no assigned pilot")
	}
	if _, err := m.recordFlightMessage(flightID, FlightMessage{
		Kind:               "next_reviewer",
		SourceTurnID:       req.SourceTurnID,
		SenderSessionID:    pilot.SessionID,
		RecipientSessionID: res.SessionID,
		Text:               req.Paragraph,
		Status:             MessageStatusAccepted,
	}); err != nil {
		return reviewing, fmt.Errorf("record next reviewer: %w", err)
	}
	if _, err := m.recordFlightMessage(flightID, FlightMessage{
		Kind:               "review_notice",
		RecipientSessionID: pilot.SessionID,
		Text:               ReviewNotice(reviewing, res.SessionID),
		Status:             MessageStatusPending,
	}); err != nil {
		return reviewing, fmt.Errorf("record review notice: %w", err)
	}
	m.kickMonitor()
	recorded, _ := m.Get(flightID)
	return recorded, nil
}
