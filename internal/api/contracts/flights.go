package contracts

// FlightWorkflowState is where a flight is in its manually driven workflow:
// backlog, planning, executing, reviewing, completing, or done.
type FlightWorkflowState string

// FlightCleanupState is the destructive-cleanup axis, independent of the
// workflow: idle, or disposing while a dispose has not yet finished.
type FlightCleanupState string

// FlightSessionRole is what a session assignment is for: planner, executor,
// commit, pilot, or reviewer.
type FlightSessionRole string

const (
	FlightStateBacklog    FlightWorkflowState = "backlog"
	FlightStatePlanning   FlightWorkflowState = "planning"
	FlightStateExecuting  FlightWorkflowState = "executing"
	FlightStateReviewing  FlightWorkflowState = "reviewing"
	FlightStateCompleting FlightWorkflowState = "completing"
	FlightStateDone       FlightWorkflowState = "done"

	FlightCleanupIdle      FlightCleanupState = "idle"
	FlightCleanupDisposing FlightCleanupState = "disposing"

	FlightRolePlanner  FlightSessionRole = "planner"
	FlightRoleExecutor FlightSessionRole = "executor"
	FlightRoleCommit   FlightSessionRole = "commit"
	FlightRolePilot    FlightSessionRole = "pilot"
	FlightRoleReviewer FlightSessionRole = "reviewer"
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
	Action        string                  `json:"action"` // takeoff, execution, review, completion, done, or dispose
	CorrelationID string                  `json:"correlation_id"`
	Status        string                  `json:"status"` // pending, succeeded, or failed
	Resource      string                  `json:"resource,omitempty"`
	Resources     []FlightResourceOutcome `json:"resources,omitempty"`
	Error         string                  `json:"error,omitempty"`
	CreatedAt     string                  `json:"created_at"`
	UpdatedAt     string                  `json:"updated_at"`
}

// FlightPilotStop is the pilot's recorded reason that the flight has gone off
// the rails. It is present only while the flight is still in the phase the
// stop was recorded in.
type FlightPilotStop struct {
	WorkflowState        FlightWorkflowState `json:"workflow_state"`
	Reason               string              `json:"reason"`
	SourceTurnID         string              `json:"source_turn_id"`
	SourceNotificationID string              `json:"source_notification_id"`
	CreatedAt            string              `json:"created_at"`
}

// FlightPilotGuidance is the pilot's latest guidance to a working session
// that was delivered (accepted) or is held for the recipient (queued). It is
// derived from the monitor's pilot messages, which remain the full record.
type FlightPilotGuidance struct {
	RecipientRole      FlightSessionRole `json:"recipient_role"`
	RecipientSessionID string            `json:"recipient_session_id"`
	Text               string            `json:"text"`
	Status             string            `json:"status"` // accepted or queued
	CreatedAt          string            `json:"created_at"`
}

// FlightReviewStatus is the latest reviewer classification in words. It is
// derived from the reviewer's last successful classification, not stored
// separately.
type FlightReviewStatus struct {
	ReviewerSessionID string `json:"reviewer_session_id"`
	State             string `json:"state"`
	Tests             string `json:"tests"`
	TestRulesReview   string `json:"test_rules_review"`
	ChangedWork       string `json:"changed_work"`
	ArtifactsNamed    string `json:"artifacts_named"`
	UpdatedAt         string `json:"updated_at"`
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
	// ExecutionStartedAt, ReportTarget, and ReportPath are the execution
	// evidence: when Enter execution last started the executor, the absolute
	// report path fixed then, and that path once the daemon has verified the
	// report.
	ExecutionStartedAt string `json:"execution_started_at,omitempty"`
	ReportTarget       string `json:"report_target,omitempty"`
	ReportPath         string `json:"report_path,omitempty"`
	// ReviewStartedAt is when Enter review last started a reviewer.
	ReviewStartedAt string `json:"review_started_at,omitempty"`
	// PilotStop is the pilot's stop reason, present only while the flight is
	// still in the phase the stop was recorded in.
	PilotStop *FlightPilotStop `json:"pilot_stop,omitempty"`
	// ReviewStatus is the latest reviewer classification in words.
	ReviewStatus *FlightReviewStatus `json:"review_status,omitempty"`
	// PilotGuidance is the pilot's latest delivered or queued guidance.
	PilotGuidance *FlightPilotGuidance `json:"pilot_guidance,omitempty"`
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

// FlightMonitoredTurn is one completed planner, executor, or pilot turn plus
// the classification work the monitor has done against it.
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
	// Execution is the daemon's skill and report check of a completed executor
	// turn; absent on other turns.
	Execution *FlightExecutionCheck `json:"execution,omitempty"`
	// Review is the daemon's skill check of a completed reviewer turn; absent
	// on other turns.
	Review    *FlightReviewCheck `json:"review,omitempty"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
}

// FlightReviewCheck is the daemon's skill evidence for a reviewer turn. Skill
// is "test-rules-review invoked at record N", "test-rules-review skill file
// read at record N", or "test-rules-review not found".
type FlightReviewCheck struct {
	Skill string `json:"skill"`
}

// FlightExecutionCheck is the daemon's check of an executor turn. Skill is
// "executing-plans invoked at record N", "executing-plans skill file read at
// record N", or "executing-plans not found". ReportError is "not written yet"
// or the reason verification failed; it is empty when the report is verified.
type FlightExecutionCheck struct {
	Skill          string `json:"skill"`
	ReportPath     string `json:"report_path"`
	ReportVerified bool   `json:"report_verified"`
	ReportError    string `json:"report_error,omitempty"`
}

// FlightArtifactReport is a planner turn's reported spec and plan paths.
// Error lists every placement violation; empty means both were verified.
type FlightArtifactReport struct {
	SpecPath string `json:"spec_path,omitempty"`
	PlanPath string `json:"plan_path,omitempty"`
	Error    string `json:"error,omitempty"`
}

// FlightMessage is one notification, execution notice, or pilot message to a
// working session. The Text field is the exact unprefixed text the pilot or
// operator wrote; visible chat attribution is added by the server when
// delivering.
type FlightMessage struct {
	ID                   string `json:"id"`
	Kind                 string `json:"kind"` // notification, execution_notice, rejection, or pilot_message
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
	SpecPath string `json:"spec_path,omitempty"`
	PlanPath string `json:"plan_path,omitempty"`
	// ExecutionStartedAt, ReportTarget, and ReportPath are the execution
	// evidence: when Enter execution last started the executor, the absolute
	// report path fixed then, and that path once the daemon has verified the
	// report.
	ExecutionStartedAt string `json:"execution_started_at,omitempty"`
	ReportTarget       string `json:"report_target,omitempty"`
	ReportPath         string `json:"report_path,omitempty"`
	// ReviewStartedAt is when Enter review last started a reviewer.
	ReviewStartedAt string `json:"review_started_at,omitempty"`
	// PilotStop is the pilot's stop reason, present only while the flight is
	// still in the phase the stop was recorded in.
	PilotStop *FlightPilotStop `json:"pilot_stop,omitempty"`
	// ReviewStatus is the latest reviewer classification in words.
	ReviewStatus *FlightReviewStatus `json:"review_status,omitempty"`
	// PilotGuidance is the pilot's latest delivered or queued guidance.
	PilotGuidance *FlightPilotGuidance `json:"pilot_guidance,omitempty"`
	DoneAt        string               `json:"done_at,omitempty"`
	LastError     string               `json:"last_error,omitempty"`
	CreatedAt     string               `json:"created_at"`
	UpdatedAt     string               `json:"updated_at"`
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
