package contracts

// FlightWorkflowState is where a flight is in its manually driven workflow:
// backlog, planning, executing, completing, or done.
type FlightWorkflowState string

// FlightCleanupState is the destructive-cleanup axis, independent of the
// workflow: idle, or disposing while a dispose has not yet finished.
type FlightCleanupState string

// FlightSessionRole is what a session assignment is for: planner, executor,
// commit, or pilot.
type FlightSessionRole string

const (
	FlightStateBacklog    FlightWorkflowState = "backlog"
	FlightStatePlanning   FlightWorkflowState = "planning"
	FlightStateExecuting  FlightWorkflowState = "executing"
	FlightStateCompleting FlightWorkflowState = "completing"
	FlightStateDone       FlightWorkflowState = "done"

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
	Action        string                  `json:"action"` // takeoff, execution, completion, done, or dispose
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
	ShortName     string                     `json:"short_name,omitempty"` // daemon-generated; read-only
	NameError     string                     `json:"name_error,omitempty"`
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

// FlightMonitorAttempt is one Jev classification attempt for a retained
// turn. The Config field is omitted when the YAML was unparseable at
// attempt time.
type FlightMonitorAttempt struct {
	Questions []FlightMonitorQuestion `json:"questions,omitempty"`
	Model     string                  `json:"model,omitempty"`
	Answers   []FlightMonitorAnswer   `json:"answers,omitempty"`
	Error     string                  `json:"error,omitempty"`
	CreatedAt string                  `json:"created_at"`
}

type FlightMonitorQuestion struct {
	ID            string                `json:"id"`
	Question      string                `json:"question"`
	Choices       []FlightMonitorChoice `json:"choices"`
	NotifyOn      []string              `json:"notify_on"`
	MinConfidence float64               `json:"min_confidence"`
}

type FlightMonitorChoice struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// FlightMonitorAnswer is one Jev Choice answer.
type FlightMonitorAnswer struct {
	ID            string             `json:"id"`
	Choice        string             `json:"choice"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// FlightMonitoredTurn is one completed planner turn plus the classification
// work the monitor has done against it.
type FlightMonitoredTurn struct {
	ID        string                 `json:"id"`
	SessionID string                 `json:"session_id"`
	StartSeq  uint64                 `json:"start_seq"`
	EndSeq    uint64                 `json:"end_seq"`
	Text      string                 `json:"text"`
	Outcome   string                 `json:"outcome"`
	Error     string                 `json:"error,omitempty"`
	Status    string                 `json:"status"`
	Attempts  []FlightMonitorAttempt `json:"attempts,omitempty"`
	// Artifacts is the turn's `Spec:`/`Plan:` report and the daemon's check;
	// absent when the turn reported neither.
	Artifacts *FlightArtifactReport `json:"artifacts,omitempty"`
	CreatedAt string                `json:"created_at"`
	UpdatedAt string                `json:"updated_at"`
}

// FlightArtifactReport is a planner turn's reported spec and plan paths.
// Error lists every placement violation; empty means both were verified.
type FlightArtifactReport struct {
	SpecPath string `json:"spec_path,omitempty"`
	PlanPath string `json:"plan_path,omitempty"`
	Error    string `json:"error,omitempty"`
}

// FlightMessage is one notification or pilot-to-planner message. The Text
// field is the exact unprefixed text the pilot or operator wrote; visible
// chat attribution is added by the server when delivering.
type FlightMessage struct {
	ID                   string `json:"id"`
	Kind                 string `json:"kind"`
	SourceTurnID         string `json:"source_turn_id,omitempty"`
	SourceNotificationID string `json:"source_notification_id,omitempty"`
	SenderSessionID      string `json:"sender_session_id,omitempty"`
	RecipientSessionID   string `json:"recipient_session_id"`
	Text                 string `json:"text"`
	ChatMessageID        string `json:"chat_message_id,omitempty"`
	Status               string `json:"status"`
	Error                string `json:"error,omitempty"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

// FlightMonitor is the durable evidence the monitor retains on a flight.
type FlightMonitor struct {
	Turns    []FlightMonitoredTurn `json:"turns,omitempty"`
	Messages []FlightMessage       `json:"messages,omitempty"`
	Error    string                `json:"error,omitempty"`
}

// FlightDetailResponse is a flight with its full prompt, planning artifact,
// completion selection, and operation history.
type FlightDetailResponse struct {
	ID            string                     `json:"id"`
	Repo          string                     `json:"repo"`
	ShortName     string                     `json:"short_name,omitempty"` // daemon-generated; read-only
	NameError     string                     `json:"name_error,omitempty"`
	Prompt        string                     `json:"prompt"`
	PlannerModel  string                     `json:"planner_model"`
	ExecutorModel string                     `json:"executor_model"`
	WorkflowState FlightWorkflowState        `json:"workflow_state"`
	CleanupState  FlightCleanupState         `json:"cleanup_state"`
	Order         int                        `json:"order"`
	Workspace     *FlightWorkspaceAssignment `json:"workspace,omitempty"`
	Sessions      []FlightSessionAssignment  `json:"sessions,omitempty"`
	Operations    []FlightOperation          `json:"operations,omitempty"`
	Monitor       *FlightMonitor             `json:"monitor,omitempty"`
	// SpecPath and PlanPath are the planner's last verified report of where
	// the planning skills placed the spec and plan.
	SpecPath  string `json:"spec_path,omitempty"`
	PlanPath  string `json:"plan_path,omitempty"`
	DoneAt    string `json:"done_at,omitempty"`
	LastError string `json:"last_error,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// FlightsResponse is the body of GET /api/flights.
type FlightsResponse struct {
	Flights []FlightSummaryResponse `json:"flights"`
}

// CreateFlightRequest creates a backlog flight.
type CreateFlightRequest struct {
	Repo          string `json:"repo"`
	Prompt        string `json:"prompt"`
	PlannerModel  string `json:"planner_model"`
	ExecutorModel string `json:"executor_model"`
}

// UpdateFlightRequest edits a flight; omitted fields are left alone.
type UpdateFlightRequest struct {
	WorkflowState *FlightWorkflowState `json:"workflow_state,omitempty"`
	Repo          *string              `json:"repo,omitempty"`
	Prompt        *string              `json:"prompt,omitempty"`
	PlannerModel  *string              `json:"planner_model,omitempty"`
	ExecutorModel *string              `json:"executor_model,omitempty"`
}

// ReorderFlightsRequest carries the complete ordered list of backlog flight IDs.
type ReorderFlightsRequest struct {
	FlightIDs []string `json:"flight_ids"`
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
