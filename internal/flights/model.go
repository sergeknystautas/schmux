// Package flights owns the durable flight records: a unit of work that moves
// manually from a backlog through planning, execution, and completion. The
// Manager is the sole writer of ~/.schmux/flights.json; workspace, session,
// chat, and preview mechanics stay with their existing managers.
package flights

import (
	"encoding/json"
	"time"

	"github.com/sergeknystautas/schmux/internal/jsoncompat"
)

// WorkflowState is where a flight is in its manually driven workflow.
type WorkflowState string

const (
	StateBacklog    WorkflowState = "backlog"
	StatePlanning   WorkflowState = "planning"
	StateExecuting  WorkflowState = "executing"
	StateCompleting WorkflowState = "completing"
	StateDone       WorkflowState = "done"
)

func (s WorkflowState) valid() bool {
	switch s {
	case StateBacklog, StatePlanning, StateExecuting, StateCompleting, StateDone:
		return true
	}
	return false
}

// CleanupState is the destructive-cleanup axis, independent of WorkflowState.
type CleanupState string

const (
	CleanupIdle      CleanupState = "idle"
	CleanupDisposing CleanupState = "disposing"
)

func (s CleanupState) valid() bool { return s == CleanupIdle || s == CleanupDisposing }

// SessionRole names what a session assignment is for.
type SessionRole string

const (
	RolePlanner  SessionRole = "planner"
	RoleExecutor SessionRole = "executor"
	RoleCommit   SessionRole = "commit"
	RolePilot    SessionRole = "pilot"
)

func (r SessionRole) valid() bool {
	return r == RolePlanner || r == RoleExecutor || r == RoleCommit || r == RolePilot
}

// OperationAction names a recorded workflow transition or dispose attempt.
type OperationAction string

const (
	ActionTakeoff    OperationAction = "takeoff"
	ActionExecution  OperationAction = "execution"
	ActionCompletion OperationAction = "completion"
	ActionDone       OperationAction = "done"
	ActionDispose    OperationAction = "dispose"
	ActionReview     OperationAction = "review"
)

func (a OperationAction) valid() bool {
	switch a {
	case ActionTakeoff, ActionExecution, ActionCompletion, ActionDone, ActionDispose, ActionReview:
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
	NamePrompt    string        `json:"name_prompt,omitempty"`
	NameError     string        `json:"name_error,omitempty"`
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

	// Monitor is the durable evidence of planner monitoring: completed
	// turns, classification attempts, and the messages the monitor sent to
	// the pilot. Absent on flights that predate the pilot increment.
	Monitor *FlightMonitor `json:"monitor,omitempty"`

	// SpecPath and PlanPath are the absolute spec and plan the planner last
	// reported and the daemon verified. The planning skills choose where these
	// go; schmux never dictates a location. Empty until a report verifies.
	SpecPath string `json:"spec_path,omitempty"`
	PlanPath string `json:"plan_path,omitempty"`

	DoneAt    *time.Time `json:"done_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// extraFields preserves fields written by a newer schmux so a save from
	// this version cannot strip them.
	extraFields jsoncompat.Fields
}

func (f *Flight) UnmarshalJSON(data []byte) error {
	type plain Flight
	extras, err := jsoncompat.UnmarshalUnknown(data, (*plain)(f), Flight{})
	if err != nil {
		return err
	}
	f.extraFields = extras
	return nil
}

func (f Flight) MarshalJSON() ([]byte, error) {
	type plain Flight
	return jsoncompat.MarshalWithFields(plain(f), f.extraFields)
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

	extraFields jsoncompat.Fields
}

func (w *WorkspaceAssignment) UnmarshalJSON(data []byte) error {
	type plain WorkspaceAssignment
	extras, err := jsoncompat.UnmarshalUnknown(data, (*plain)(w), WorkspaceAssignment{})
	w.extraFields = extras
	return err
}

func (w WorkspaceAssignment) MarshalJSON() ([]byte, error) {
	type plain WorkspaceAssignment
	return jsoncompat.MarshalWithFields(plain(w), w.extraFields)
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

	extraFields jsoncompat.Fields
}

func (s *SessionAssignment) UnmarshalJSON(data []byte) error {
	type plain SessionAssignment
	extras, err := jsoncompat.UnmarshalUnknown(data, (*plain)(s), SessionAssignment{})
	s.extraFields = extras
	return err
}

func (s SessionAssignment) MarshalJSON() ([]byte, error) {
	type plain SessionAssignment
	return jsoncompat.MarshalWithFields(plain(s), s.extraFields)
}

// ResourceOutcome is one resource-level result inside an operation.
type ResourceOutcome struct {
	Kind   string          `json:"kind"`
	ID     string          `json:"id,omitempty"`
	Status OperationStatus `json:"status"`
	Error  string          `json:"error,omitempty"`

	extraFields jsoncompat.Fields
}

func (r *ResourceOutcome) UnmarshalJSON(data []byte) error {
	type plain ResourceOutcome
	extras, err := jsoncompat.UnmarshalUnknown(data, (*plain)(r), ResourceOutcome{})
	r.extraFields = extras
	return err
}

func (r ResourceOutcome) MarshalJSON() ([]byte, error) {
	type plain ResourceOutcome
	return jsoncompat.MarshalWithFields(plain(r), r.extraFields)
}

// Operation is one recorded transition or dispose attempt.
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

	extraFields jsoncompat.Fields
}

func (o *Operation) UnmarshalJSON(data []byte) error {
	type plain Operation
	extras, err := jsoncompat.UnmarshalUnknown(data, (*plain)(o), Operation{})
	o.extraFields = extras
	return err
}

func (o Operation) MarshalJSON() ([]byte, error) {
	type plain Operation
	return jsoncompat.MarshalWithFields(plain(o), o.extraFields)
}

// MonitorAttemptStatus is the outcome of one Jev classification attempt.
type MonitorAttemptStatus string

const (
	MonitorAttemptPending MonitorAttemptStatus = "pending"
	MonitorAttemptOK      MonitorAttemptStatus = "classified"
	MonitorAttemptFailed  MonitorAttemptStatus = "failed"
	MonitorAttemptSkipped MonitorAttemptStatus = "skipped"
)

// MonitorAttempt is one Jev call against a completed turn. The definition
// snapshot is retained so the operator can see which questions were asked,
// even after the YAML has been edited. Empty config means the YAML was
// unparseable at attempt time; the Error field carries the parse failure.
type MonitorAttempt struct {
	Config         *MonitorConfig  `json:"config,omitempty"`
	Classification *Classification `json:"classification,omitempty"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// MonitoredTurnStatus is the state of a retained turn.
type MonitoredTurnStatus string

const (
	MonitoredTurnPending    MonitoredTurnStatus = "pending"
	MonitoredTurnClassified MonitoredTurnStatus = "classified"
	MonitoredTurnSkipped    MonitoredTurnStatus = "skipped"
	MonitoredTurnFailed     MonitoredTurnStatus = "failed"
)

// MonitoredTurn is one completed planner turn plus the classification work
// the monitor has done against it.
type MonitoredTurn struct {
	ID       string           `json:"id"`
	Turn     PlannerTurn      `json:"turn"`
	Attempts []MonitorAttempt `json:"attempts,omitempty"`
	// Artifacts is the spec/plan report found in a planner turn and the
	// daemon's check of it. Nil when the turn reported neither.
	Artifacts *ArtifactReport     `json:"artifacts,omitempty"`
	Status    MonitoredTurnStatus `json:"status"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// ArtifactReport is what a planner turn reported in its `Spec:` and `Plan:`
// lines. Error lists every way the report violates the placement contract;
// empty means both files were verified.
type ArtifactReport struct {
	SpecPath string `json:"spec_path,omitempty"`
	PlanPath string `json:"plan_path,omitempty"`
	Error    string `json:"error,omitempty"`
}

// FlightMessageStatus is the delivery state of a notification or pilot
// message. unknown means the daemon cannot prove delivery after a crash.
type FlightMessageStatus string

const (
	MessageStatusPending  FlightMessageStatus = "pending"
	MessageStatusSending  FlightMessageStatus = "sending"
	MessageStatusQueued   FlightMessageStatus = "queued"
	MessageStatusAccepted FlightMessageStatus = "accepted"
	MessageStatusFailed   FlightMessageStatus = "failed"
	MessageStatusUnknown  FlightMessageStatus = "unknown"
)

func (s FlightMessageStatus) valid() bool {
	switch s {
	case MessageStatusPending, MessageStatusSending, MessageStatusQueued,
		MessageStatusAccepted, MessageStatusFailed, MessageStatusUnknown:
		return true
	}
	return false
}

// FlightMessage is one notification or pilot-to-planner message. The Text
// field is the exact unprefixed text the pilot or operator wrote; visible
// chat attribution is added by the server when delivering, not stored here.
type FlightMessage struct {
	ID                   string              `json:"id"`
	Kind                 string              `json:"kind"` // notification or pilot_message
	SourceTurnID         string              `json:"source_turn_id,omitempty"`
	SourceNotificationID string              `json:"source_notification_id,omitempty"`
	SenderSessionID      string              `json:"sender_session_id,omitempty"`
	RecipientSessionID   string              `json:"recipient_session_id"`
	Text                 string              `json:"text"`
	ChatMessageID        string              `json:"chat_message_id,omitempty"`
	Status               FlightMessageStatus `json:"status"`
	Error                string              `json:"error,omitempty"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

// FlightMonitor is the durable monitor evidence retained on a flight. Turns
// and messages are kept in the order the monitor observed them; Error is set
// when subscription, extraction, or configuration startup failed.
type FlightMonitor struct {
	Turns    []MonitoredTurn `json:"turns,omitempty"`
	Messages []FlightMessage `json:"messages,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type snapshot struct {
	Flights        []Flight
	InvalidFlights []json.RawMessage
	ExtraFields    jsoncompat.Fields
}

func (f Flight) clone() Flight {
	c := f
	c.extraFields = jsoncompat.Clone(f.extraFields)
	if f.Workspace != nil {
		ws := *f.Workspace
		ws.extraFields = jsoncompat.Clone(ws.extraFields)
		c.Workspace = &ws
	}
	if f.Sessions != nil {
		c.Sessions = make([]SessionAssignment, len(f.Sessions))
		for i, s := range f.Sessions {
			s.PriorSessionIDs = append([]string(nil), s.PriorSessionIDs...)
			s.extraFields = jsoncompat.Clone(s.extraFields)
			c.Sessions[i] = s
		}
	}
	if f.Operations != nil {
		c.Operations = make([]Operation, len(f.Operations))
		for i, o := range f.Operations {
			o.Resources = append([]ResourceOutcome(nil), o.Resources...)
			o.extraFields = jsoncompat.Clone(o.extraFields)
			for j := range o.Resources {
				o.Resources[j].extraFields = jsoncompat.Clone(o.Resources[j].extraFields)
			}
			c.Operations[i] = o
		}
	}
	if f.Monitor != nil {
		m := *f.Monitor
		if f.Monitor.Turns != nil {
			m.Turns = make([]MonitoredTurn, len(f.Monitor.Turns))
			for i, t := range f.Monitor.Turns {
				if t.Attempts != nil {
					t.Attempts = append([]MonitorAttempt(nil), t.Attempts...)
					for j := range t.Attempts {
						t.Attempts[j] = t.Attempts[j].clone()
					}
				}
				if t.Artifacts != nil {
					a := *t.Artifacts
					t.Artifacts = &a
				}
				m.Turns[i] = t
			}
		}
		if f.Monitor.Messages != nil {
			m.Messages = make([]FlightMessage, len(f.Monitor.Messages))
			copy(m.Messages, f.Monitor.Messages)
		}
		c.Monitor = &m
	}
	if f.DoneAt != nil {
		t := *f.DoneAt
		c.DoneAt = &t
	}
	return c
}

func (a MonitorAttempt) clone() MonitorAttempt {
	if a.Config != nil {
		cfg := *a.Config
		cfg.Roles = make(map[string]MonitorRole, len(a.Config.Roles))
		for name, role := range a.Config.Roles {
			role.Classifiers = append([]MonitorClassifier(nil), role.Classifiers...)
			for i := range role.Classifiers {
				c := &role.Classifiers[i]
				c.Choices = append([]MonitorChoice(nil), c.Choices...)
				c.NotifyOn = append([]string(nil), c.NotifyOn...)
			}
			cfg.Roles[name] = role
		}
		a.Config = &cfg
	}
	if a.Classification != nil {
		c := *a.Classification
		c.Answers = append([]MonitorAnswer(nil), c.Answers...)
		for i := range c.Answers {
			ans := &c.Answers[i]
			if ans.Confidence != nil {
				value := *ans.Confidence
				ans.Confidence = &value
			}
			if ans.Probabilities != nil {
				values := make(map[string]float64, len(ans.Probabilities))
				for k, v := range ans.Probabilities {
					values[k] = v
				}
				ans.Probabilities = values
			}
		}
		a.Classification = &c
	}
	return a
}
