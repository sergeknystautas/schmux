// Package flights owns the durable flight records: a unit of work that moves
// manually from a backlog through planning, execution, and completion. The
// Manager is the sole writer of ~/.schmux/flights.json; workspace, session,
// chat, and preview mechanics stay with their existing managers.
package flights

import "time"

// WorkflowState is where a flight is in its manually driven workflow.
type WorkflowState string

const (
	StateBacklog    WorkflowState = "backlog"
	StatePlanning   WorkflowState = "planning"
	StateExecuting  WorkflowState = "executing"
	StateCompleting WorkflowState = "completing"
	StateDone       WorkflowState = "done"
	StateCancelled  WorkflowState = "cancelled"
)

func (s WorkflowState) valid() bool {
	switch s {
	case StateBacklog, StatePlanning, StateExecuting, StateCompleting, StateDone, StateCancelled:
		return true
	}
	return false
}

// CleanupState is the destructive-cleanup axis, independent of WorkflowState.
type CleanupState string

const (
	CleanupIdle        CleanupState = "idle"
	CleanupTerminating CleanupState = "terminating"
)

func (s CleanupState) valid() bool { return s == CleanupIdle || s == CleanupTerminating }

// SessionRole names what a session assignment is for.
type SessionRole string

const (
	RolePlanner  SessionRole = "planner"
	RoleExecutor SessionRole = "executor"
	RoleCommit   SessionRole = "commit"
)

func (r SessionRole) valid() bool {
	return r == RolePlanner || r == RoleExecutor || r == RoleCommit
}

// OperationAction names a recorded workflow transition or terminate attempt.
type OperationAction string

const (
	ActionTakeoff    OperationAction = "takeoff"
	ActionExecution  OperationAction = "execution"
	ActionCompletion OperationAction = "completion"
	ActionDone       OperationAction = "done"
	ActionCancel     OperationAction = "cancel"
	ActionTerminate  OperationAction = "terminate"
)

func (a OperationAction) valid() bool {
	switch a {
	case ActionTakeoff, ActionExecution, ActionCompletion, ActionDone, ActionCancel, ActionTerminate:
		return true
	}
	return false
}

// OperationStatus is the outcome of an operation, assignment, or resource step.
type OperationStatus string

const (
	StatusPending   OperationStatus = "pending"
	StatusSucceeded OperationStatus = "succeeded"
	StatusFailed    OperationStatus = "failed"
)

func (s OperationStatus) valid() bool {
	return s == StatusPending || s == StatusSucceeded || s == StatusFailed
}

// Flight is the durable workflow record. It is independent of workspace and
// session IDs; those are retained as assignments.
type Flight struct {
	ID            string        `json:"id"`
	Repo          string        `json:"repo"`
	Prompt        string        `json:"prompt"`
	ShortName     string        `json:"short_name,omitempty"`
	PlannerModel  string        `json:"planner_model"`
	ExecutorModel string        `json:"executor_model"`
	WorkflowState WorkflowState `json:"workflow_state"`
	CleanupState  CleanupState  `json:"cleanup_state"`
	// Order is the position among backlog flights; it is meaningful only
	// while WorkflowState is backlog.
	Order int `json:"order"`

	Workspace  *WorkspaceAssignment `json:"workspace,omitempty"`
	Sessions   []SessionAssignment  `json:"sessions,omitempty"`
	Operations []Operation          `json:"operations,omitempty"`

	// ArtifactPath is the absolute planning artifact the planner is told to write.
	ArtifactPath string `json:"artifact_path,omitempty"`

	CancelReason string     `json:"cancel_reason,omitempty"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
	DoneAt       *time.Time `json:"done_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// WorkspaceAssignment is the retained relationship from a flight to the
// workspace its takeoff allocated.
type WorkspaceAssignment struct {
	ID              string          `json:"id"`
	RequestedBranch string          `json:"requested_branch"`
	WorkspaceID     string          `json:"workspace_id"`
	Path            string          `json:"path"`
	Branch          string          `json:"branch,omitempty"`
	VCS             string          `json:"vcs,omitempty"`
	Outcome         OperationStatus `json:"outcome"`
	Error           string          `json:"error,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// SessionAssignment is the retained relationship from a flight role to its
// current session and the sessions earlier attempts produced.
type SessionAssignment struct {
	ID              string          `json:"id"`
	Role            SessionRole     `json:"role"`
	Target          string          `json:"target"`
	SessionID       string          `json:"session_id,omitempty"`
	PriorSessionIDs []string        `json:"prior_session_ids,omitempty"`
	Outcome         OperationStatus `json:"outcome"`
	Error           string          `json:"error,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// ResourceOutcome is one resource-level result inside an operation.
type ResourceOutcome struct {
	Kind   string          `json:"kind"`
	ID     string          `json:"id,omitempty"`
	Status OperationStatus `json:"status"`
	Error  string          `json:"error,omitempty"`
}

// Operation is one recorded transition or terminate attempt.
type Operation struct {
	ID            string            `json:"id"`
	Action        OperationAction   `json:"action"`
	CorrelationID string            `json:"correlation_id"`
	Status        OperationStatus   `json:"status"`
	Resource      string            `json:"resource,omitempty"`
	Resources     []ResourceOutcome `json:"resources,omitempty"`
	Error         string            `json:"error,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// snapshotVersion is the only flights.json envelope version this build reads.
const snapshotVersion = 1

type snapshot struct {
	Version int      `json:"version"`
	Flights []Flight `json:"flights"`
}

func (f Flight) clone() Flight {
	c := f
	if f.Workspace != nil {
		ws := *f.Workspace
		c.Workspace = &ws
	}
	if f.Sessions != nil {
		c.Sessions = make([]SessionAssignment, len(f.Sessions))
		for i, s := range f.Sessions {
			s.PriorSessionIDs = append([]string(nil), s.PriorSessionIDs...)
			c.Sessions[i] = s
		}
	}
	if f.Operations != nil {
		c.Operations = make([]Operation, len(f.Operations))
		for i, o := range f.Operations {
			o.Resources = append([]ResourceOutcome(nil), o.Resources...)
			c.Operations[i] = o
		}
	}
	if f.CancelledAt != nil {
		t := *f.CancelledAt
		c.CancelledAt = &t
	}
	if f.DoneAt != nil {
		t := *f.DoneAt
		c.DoneAt = &t
	}
	return c
}
