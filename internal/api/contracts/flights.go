package contracts

// FlightWorkflowState is where a flight is in its manually driven workflow:
// backlog, planning, executing, completing, done, or cancelled.
type FlightWorkflowState string

// FlightCleanupState is the destructive-cleanup axis, independent of the
// workflow: idle, or disposing while a dispose has not yet finished.
type FlightCleanupState string

// FlightSessionRole is what a session assignment is for: planner, executor,
// commit, or a historical pilot assignment.
type FlightSessionRole string

const (
	FlightStateBacklog    FlightWorkflowState = "backlog"
	FlightStatePlanning   FlightWorkflowState = "planning"
	FlightStateExecuting  FlightWorkflowState = "executing"
	FlightStateCompleting FlightWorkflowState = "completing"
	FlightStateDone       FlightWorkflowState = "done"
	FlightStateCancelled  FlightWorkflowState = "cancelled"

	FlightCleanupIdle      FlightCleanupState = "idle"
	FlightCleanupDisposing FlightCleanupState = "disposing"

	FlightRolePlanner  FlightSessionRole = "planner"
	FlightRoleExecutor FlightSessionRole = "executor"
	FlightRoleCommit   FlightSessionRole = "commit"
	FlightRolePilot    FlightSessionRole = "pilot"
)

// FlightWorkspaceAssignment is the retained relationship from a flight to the
// workspace its takeoff allocated. Available is false once the workspace is no
// longer in the state store; the assignment is kept as history.
type FlightWorkspaceAssignment struct {
	ID              string `json:"id"`
	RequestedBranch string `json:"requested_branch"`
	WorkspaceID     string `json:"workspace_id"`
	Path            string `json:"path"`
	Branch          string `json:"branch,omitempty"`
	VCS             string `json:"vcs,omitempty"`
	Outcome         string `json:"outcome"` // pending, succeeded, or failed
	Error           string `json:"error,omitempty"`
	Available       bool   `json:"available"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// FlightSessionAssignment is the retained relationship from a flight role to
// its current session and the sessions earlier attempts produced. Available is
// false once the current session is no longer in the state store.
type FlightSessionAssignment struct {
	ID              string            `json:"id"`
	Role            FlightSessionRole `json:"role"`
	Target          string            `json:"target"`
	SessionID       string            `json:"session_id,omitempty"`
	PriorSessionIDs []string          `json:"prior_session_ids,omitempty"`
	Outcome         string            `json:"outcome"` // pending, succeeded, or failed
	Error           string            `json:"error,omitempty"`
	Available       bool              `json:"available"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
}

// FlightResourceOutcome is one resource-level result inside an operation.
type FlightResourceOutcome struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	Status string `json:"status"` // pending, succeeded, or failed
	Error  string `json:"error,omitempty"`
}

// FlightOperation is one recorded transition or dispose attempt.
type FlightOperation struct {
	ID            string                  `json:"id"`
	Action        string                  `json:"action"` // takeoff, execution, completion, done, cancel, or dispose
	CorrelationID string                  `json:"correlation_id"`
	Status        string                  `json:"status"` // pending, succeeded, or failed
	Resource      string                  `json:"resource,omitempty"`
	Resources     []FlightResourceOutcome `json:"resources,omitempty"`
	Error         string                  `json:"error,omitempty"`
	CreatedAt     string                  `json:"created_at"`
	UpdatedAt     string                  `json:"updated_at"`
}

// FlightSummaryResponse is a flight as listed and as carried in every
// /ws/dashboard sessions payload.
type FlightSummaryResponse struct {
	ID            string                     `json:"id"`
	Repo          string                     `json:"repo"`
	ShortName     string                     `json:"short_name,omitempty"`
	PlannerModel  string                     `json:"planner_model"`
	ExecutorModel string                     `json:"executor_model"`
	WorkflowState FlightWorkflowState        `json:"workflow_state"`
	CleanupState  FlightCleanupState         `json:"cleanup_state"`
	Order         int                        `json:"order"`
	Workspace     *FlightWorkspaceAssignment `json:"workspace,omitempty"`
	Sessions      []FlightSessionAssignment  `json:"sessions,omitempty"`
	// LastError is the error of the most recent failed operation that no later
	// operation of the same action has superseded.
	LastError string `json:"last_error,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// FlightDetailResponse is a flight with its full prompt, planning artifact,
// completion selection, and operation history.
type FlightDetailResponse struct {
	ID            string                     `json:"id"`
	Repo          string                     `json:"repo"`
	ShortName     string                     `json:"short_name,omitempty"`
	Prompt        string                     `json:"prompt"`
	PlannerModel  string                     `json:"planner_model"`
	ExecutorModel string                     `json:"executor_model"`
	WorkflowState FlightWorkflowState        `json:"workflow_state"`
	CleanupState  FlightCleanupState         `json:"cleanup_state"`
	Order         int                        `json:"order"`
	Workspace     *FlightWorkspaceAssignment `json:"workspace,omitempty"`
	Sessions      []FlightSessionAssignment  `json:"sessions,omitempty"`
	Operations    []FlightOperation          `json:"operations,omitempty"`
	ArtifactPath  string                     `json:"artifact_path,omitempty"`
	CancelReason  string                     `json:"cancel_reason,omitempty"`
	CancelledAt   string                     `json:"cancelled_at,omitempty"`
	DoneAt        string                     `json:"done_at,omitempty"`
	LastError     string                     `json:"last_error,omitempty"`
	CreatedAt     string                     `json:"created_at"`
	UpdatedAt     string                     `json:"updated_at"`
}

// FlightsResponse is the body of GET /api/flights.
type FlightsResponse struct {
	Flights []FlightSummaryResponse `json:"flights"`
}

// CreateFlightRequest creates a backlog flight.
type CreateFlightRequest struct {
	Repo          string `json:"repo"`
	Prompt        string `json:"prompt"`
	ShortName     string `json:"short_name,omitempty"`
	PlannerModel  string `json:"planner_model"`
	ExecutorModel string `json:"executor_model"`
}

// UpdateFlightRequest edits a flight; omitted fields are left alone.
type UpdateFlightRequest struct {
	WorkflowState *FlightWorkflowState `json:"workflow_state,omitempty"`
	Repo          *string              `json:"repo,omitempty"`
	Prompt        *string              `json:"prompt,omitempty"`
	ShortName     *string              `json:"short_name,omitempty"`
	PlannerModel  *string              `json:"planner_model,omitempty"`
	ExecutorModel *string              `json:"executor_model,omitempty"`
}

// ReorderFlightsRequest carries the complete ordered list of backlog flight IDs.
type ReorderFlightsRequest struct {
	FlightIDs []string `json:"flight_ids"`
}

// FlightCancelRequest cancels a started flight.
type FlightCancelRequest struct {
	Reason string `json:"reason"`
}

// FlightDisposeRequest confirms a destructive dispose. WorkspaceID must be
// the workspace the flight owns; Confirm must be true. A flight with no
// workspace removes its record at once: the body must carry Confirm with
// WorkspaceID empty.
type FlightDisposeRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Confirm     bool   `json:"confirm"`
}

// FlightTransitionResponse is the body of every workflow transition. After a
// successful dispose, Removed is true and Flight is the record as it was
// removed.
type FlightTransitionResponse struct {
	Flight  FlightDetailResponse `json:"flight"`
	Removed bool                 `json:"removed,omitempty"`
}
