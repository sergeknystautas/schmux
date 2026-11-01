package flightreview

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// EvidenceCoverage names how much evidence stands behind a flight's summary.
type EvidenceCoverage string

const (
	CoverageLiveCapture   EvidenceCoverage = "live_capture"
	CoverageLinkedLegacy  EvidenceCoverage = "linked_legacy"
	CoveragePartialLegacy EvidenceCoverage = "partial_legacy"
	CoverageMetadataOnly  EvidenceCoverage = "metadata_only"
	CoverageUnlinked      EvidenceCoverage = "unlinked_evidence"
)

// Outcome is what became of a flight's work.
type Outcome string

const (
	OutcomeFlightCompleted      Outcome = "flight_completed"
	OutcomeFinishedOutside      Outcome = "finished_outside_flight"
	OutcomeManualUndoRecovery   Outcome = "manual_undo_recovery"
	OutcomeWorkspaceLostUnknown Outcome = "workspace_lost_unknown"
	OutcomeDisposedEarly        Outcome = "disposed_early"
	OutcomeBacklogRemoved       Outcome = "backlog_removed"
	OutcomeActive               Outcome = "active"
)

// outcomePrecedence orders the outcomes that can apply at once; the first
// that applies is the primary outcome.
var outcomePrecedence = []Outcome{
	OutcomeFlightCompleted, OutcomeFinishedOutside, OutcomeManualUndoRecovery,
	OutcomeWorkspaceLostUnknown, OutcomeDisposedEarly, OutcomeBacklogRemoved, OutcomeActive,
}

// SourceReference points at the evidence a derived value came from.
type SourceReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Line int    `json:"line,omitempty"`
}

// SourceReferences maps each derived summary field to its evidence.
type SourceReferences map[string][]SourceReference

// Summary field names used as SourceReferences keys.
const (
	FieldCoverage       = "coverage"
	FieldOutcome        = "primary_outcome"
	FieldOrphaned       = "orphaned_active"
	FieldPhasePath      = "phase_path"
	FieldInterventions  = "interventions"
	FieldCounts         = "counts"
	FieldDataQuality    = "data_quality"
	FieldWaitingSeconds = "waiting_seconds"
)

// SourceFieldKeys lists every field that must carry a source reference.
var SourceFieldKeys = []string{
	FieldCoverage, FieldOutcome, FieldOrphaned, FieldPhasePath,
	FieldInterventions, FieldCounts, FieldDataQuality, FieldWaitingSeconds,
}

// PhaseVisit is one observed visit to a workflow state. Durations are present
// only when both boundaries were observed.
type PhaseVisit struct {
	Phase           flights.WorkflowState `json:"phase"`
	Visit           uint64                `json:"visit"`
	EnteredAt       *time.Time            `json:"entered_at,omitempty"`
	LeftAt          *time.Time            `json:"left_at,omitempty"`
	DurationSeconds *int64                `json:"duration_seconds,omitempty"`
	EnteredBy       flighthistory.Actor   `json:"entered_by,omitempty"`
	Sources         []SourceReference     `json:"sources"`
}

// InterventionKind says what the user did.
type InterventionKind string

const (
	KindManualRoute      InterventionKind = "manual_route"
	KindWorkflowEdit     InterventionKind = "workflow_edit"
	KindModelEdit        InterventionKind = "model_edit"
	KindFlightEdit       InterventionKind = "flight_edit"
	KindManualUndo       InterventionKind = "manual_undo"
	KindSessionKill      InterventionKind = "session_kill"
	KindExternalTakeover InterventionKind = "external_takeover"
	KindOperatorMessage  InterventionKind = "operator_message"
	KindApproval         InterventionKind = "approval"
	KindAnswer           InterventionKind = "answer"
	KindInterrupt        InterventionKind = "interrupt"
	KindQueueChange      InterventionKind = "queue_change"
	KindDisposal         InterventionKind = "disposal"
)

// Intervention is one counted act of user work after takeoff.
type Intervention struct {
	ID                   string                `json:"id"`
	Kind                 InterventionKind      `json:"kind"`
	At                   *time.Time            `json:"at,omitempty"`
	ElapsedSinceTakeoff  *int64                `json:"elapsed_since_takeoff_seconds,omitempty"`
	ElapsedSincePrevious *int64                `json:"elapsed_since_previous_seconds,omitempty"`
	Phase                flights.WorkflowState `json:"phase,omitempty"`
	PhaseVisit           uint64                `json:"phase_visit,omitempty"`
	SessionRole          string                `json:"session_role,omitempty"`
	SessionID            string                `json:"session_id,omitempty"`
	// Excerpt is verbatim operator text, local to the report.
	Excerpt string `json:"excerpt,omitempty"`
	// Classification is set only when the mechanism proves it: permission,
	// question, correction, retry, workflow_override. Otherwise "unclassified".
	Classification string `json:"classification"`
	// ChatLabel is the stored Jev label for an operator message, when one
	// succeeded. It groups evidence; it does not prove why the user wrote.
	ChatLabel ChatLabel `json:"chat_label,omitempty"`
	// ChatRecordID is the conversation record the intervention came from.
	ChatRecordID string `json:"chat_record_id,omitempty"`
	// Source is where a session disappearance came from ("unknown" when
	// unattributed).
	Source string `json:"source,omitempty"`
	// MissingSessionIDs links a manual undo to the sessions that vanished in
	// the window before it.
	MissingSessionIDs []string `json:"missing_session_ids,omitempty"`
	// Unconfirmed marks corroboration that is not proof of user action.
	Unconfirmed bool              `json:"unconfirmed,omitempty"`
	Sources     []SourceReference `json:"sources"`
}

// SummaryCounts are the aggregates of one flight.
type SummaryCounts struct {
	Interventions         int                      `json:"interventions"`
	ByKind                map[InterventionKind]int `json:"by_kind"`
	Unclassified          int                      `json:"unclassified"`
	ByLabel               map[string]int           `json:"by_label,omitempty"`
	Unlabeled             int                      `json:"unlabeled"`
	LowConfidence         int                      `json:"low_confidence"`
	PilotMessagesExcluded int                      `json:"pilot_messages_excluded"`
	LaunchPromptsExcluded int                      `json:"launch_prompts_excluded"`
	ManualUndos           int                      `json:"manual_undos"`
	SessionsKilled        int                      `json:"sessions_killed"`
	Reviewers             int                      `json:"reviewers"`
	SessionReplacements   int                      `json:"session_replacements"`
	PhaseVisits           int                      `json:"phase_visits"`
	Failures              map[string]int           `json:"failures"`
	FailureTotal          int                      `json:"failure_total"`
}

// DataQuality lists what is missing or doubtful about a flight's evidence.
type DataQuality struct {
	BaselineImported         bool     `json:"baseline_imported"`
	RecoveredEvents          int      `json:"recovered_events"`
	MissingTranscripts       []string `json:"missing_transcripts,omitempty"`
	MalformedTranscriptLines int      `json:"malformed_transcript_lines"`
	GitStateError            string   `json:"git_state_error,omitempty"`
	GitStateMissing          bool     `json:"git_state_missing"`
	MissingBoundaries        []string `json:"missing_boundaries,omitempty"`
}

// Summary is the derived, rebuildable view of one flight. It carries no
// wall-clock field: identical evidence produces identical bytes.
type Summary struct {
	SchemaVersion      int              `json:"schema_version"`
	FlightID           string           `json:"flight_id"`
	Coverage           EvidenceCoverage `json:"coverage"`
	PrimaryOutcome     Outcome          `json:"primary_outcome"`
	OutcomePendingUser bool             `json:"outcome_pending_user"`
	// ConfirmedByUser is set only by the decision overlay, never by derivation.
	ConfirmedByUser bool             `json:"confirmed_by_user,omitempty"`
	OrphanedActive  bool             `json:"orphaned_active"`
	CleanupFriction bool             `json:"cleanup_friction,omitempty"`
	PhasePath       []PhaseVisit     `json:"phase_path"`
	Interventions   []Intervention   `json:"interventions"`
	WaitingSeconds  *int64           `json:"waiting_seconds,omitempty"`
	Counts          SummaryCounts    `json:"counts"`
	DataQuality     DataQuality      `json:"data_quality"`
	Source          SourceReferences `json:"source"`
}

// GitState is the workspace work state archived for a flight.
type GitState = workspace.VCSArchiveState

// Transcript is one archived conversation log.
type Transcript struct {
	Role      string
	SessionID string
	Records   []TranscriptRecord
	Malformed []flighthistory.MalformedLine
}

// TranscriptRecord is a valid conversation record with the sequence it holds
// among valid records and the file line it came from.
type TranscriptRecord struct {
	Seq    int
	Line   int
	Record chat.Record
}

// Evidence is everything DeriveSummary reads for one flight. It reads no clock
// and no filesystem.
type Evidence struct {
	Events         []flighthistory.Event
	Reconciliation []flighthistory.ReconciliationEvent
	Flight         flights.Flight
	Transcripts    []Transcript
	GitState       *GitState
	// Manifest is the archive manifest, when one exists.
	Manifest *flighthistory.Manifest
	// Classifications are the stored Jev attempts for operator messages.
	Classifications []ClassificationAttempt
}

// UnlinkedCandidate is a conversation that may belong to a flight. It is never
// attached automatically.
type UnlinkedCandidate struct {
	SessionID       string    `json:"session_id"`
	WorkspaceID     string    `json:"workspace_id"`
	FlightID        string    `json:"flight_id"`
	MatchKinds      []string  `json:"match_kinds"`
	TranscriptLine  int       `json:"transcript_line"`
	Excerpt         string    `json:"excerpt"`
	FirstObservedAt time.Time `json:"first_observed_at"`
	Confirmed       bool      `json:"confirmed"`
}

// DecisionContext carries the user's confirmed decisions into a report. It is
// an overlay: summaries never record it.
type DecisionContext struct {
	// ConfirmedExternalCompletion lists flights whose work the user confirmed
	// was finished outside the flight workflow.
	ConfirmedExternalCompletion map[string]bool
}
