package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

// flightOperationTimeout bounds one workflow transition. Takeoff can clone a
// repository and spawn a session, so it needs the same headroom as a spawn.
const flightOperationTimeout = 10 * time.Minute

// FlightHandlers groups the HTTP handlers for flight records. They translate
// requests into flights.Manager calls; the manager owns every workflow decision.
type FlightHandlers struct {
	manager *flights.Manager
	state   state.StateStore
	logger  *log.Logger
}

// flightCommitSettings gives flights the global commit configuration and the
// live fence detection result, so flight completion makes the same fence
// decision the commit launcher does.
type flightCommitSettings struct {
	config *config.Config
	report func() detect.DependencyReport
}

func (s flightCommitSettings) FenceMode() string { return s.config.GetFenceMode() }
func (s flightCommitSettings) FenceInstalled() bool {
	st, ok := s.report().Status("fence")
	return ok && st.Detected
}

// registerReadRoutes mounts the read-only flight routes (no CSRF).
func (h *FlightHandlers) registerReadRoutes(r chi.Router) {
	r = r.With(h.requireAvailable)
	r.Get("/flights", h.handleList)
	r.Get("/flights/{flightID}", h.handleGet)
}

// registerMutatingRoutes mounts the state-changing flight routes; the caller
// puts them behind the CSRF middleware.
func (h *FlightHandlers) registerMutatingRoutes(r chi.Router) {
	r = r.With(h.requireAvailable)
	r.Post("/flights", h.handleCreate)
	r.Post("/flights/order", h.handleReorder)
	r.Patch("/flights/{flightID}", h.handleUpdate)
	r.Post("/flights/{flightID}/takeoff", h.handleTakeoff)
	r.Post("/flights/{flightID}/execution", h.handleExecution)
	r.Post("/flights/{flightID}/review", h.handleReview)
	r.Post("/flights/{flightID}/completion", h.handleCompletion)
	r.Post("/flights/{flightID}/dispose", h.handleDispose)
	r.Post("/flights/{flightID}/monitor/{itemID}/retry", h.handleMonitorRetry)
}

func (h *FlightHandlers) requireAvailable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.manager == nil {
			writeJSONError(w, "flights unavailable; see daemon logs", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// flightSummaries returns every flight for the /ws/dashboard sessions payload.
// It is never nil, so clients always replace their array rather than merge.
func (s *Server) flightSummaries() []contracts.FlightSummaryResponse {
	return flightSummaries(s.flights.List(), s.state)
}

func flightSummaries(list []flights.Flight, st state.StateStore) []contracts.FlightSummaryResponse {
	out := make([]contracts.FlightSummaryResponse, 0, len(list))
	for _, f := range list {
		out = append(out, flightSummary(f, st))
	}
	return out
}

func flightSummary(f flights.Flight, st state.StateStore) contracts.FlightSummaryResponse {
	startedAt, reportTarget, reportPath := flightExecutionEvidence(f)
	return contracts.FlightSummaryResponse{
		ID:            f.ID,
		Repo:          f.Repo,
		ShortName:     f.ShortName,
		NameError:     f.NameError,
		PlannerModel:  f.PlannerModel,
		ExecutorModel: f.ExecutorModel,
		WorkflowState: contracts.FlightWorkflowState(f.WorkflowState),
		CleanupState:  contracts.FlightCleanupState(f.CleanupState),
		Order:         f.Order,
		Workspace:     flightWorkspace(f.Workspace, st),
		Sessions:      flightSessions(f.Sessions, st),

		ExecutionStartedAt: startedAt,
		ReportTarget:       reportTarget,
		ReportPath:         reportPath,
		ReviewStartedAt:    flightReviewStartedAt(f),
		PilotStop:          flightPilotStop(f),
		ReviewStatus:       flightReviewStatus(f),
		PilotGuidance:      flightPilotGuidance(f),
		LastError:          flightLastError(f),
		CreatedAt:          formatTime(f.CreatedAt),
		UpdatedAt:          formatTime(f.UpdatedAt),
	}
}

func flightDetail(f flights.Flight, st state.StateStore) contracts.FlightDetailResponse {
	startedAt, reportTarget, reportPath := flightExecutionEvidence(f)
	d := contracts.FlightDetailResponse{
		ID:            f.ID,
		Repo:          f.Repo,
		ShortName:     f.ShortName,
		NameError:     f.NameError,
		Prompt:        f.Prompt,
		PlannerModel:  f.PlannerModel,
		ExecutorModel: f.ExecutorModel,
		WorkflowState: contracts.FlightWorkflowState(f.WorkflowState),
		CleanupState:  contracts.FlightCleanupState(f.CleanupState),
		Order:         f.Order,
		Workspace:     flightWorkspace(f.Workspace, st),
		Sessions:      flightSessions(f.Sessions, st),
		SpecPath:      f.SpecPath,
		PlanPath:      f.PlanPath,
		Monitor:       flightMonitor(f),

		ExecutionStartedAt: startedAt,
		ReportTarget:       reportTarget,
		ReportPath:         reportPath,
		ReviewStartedAt:    flightReviewStartedAt(f),
		PilotStop:          flightPilotStop(f),
		ReviewStatus:       flightReviewStatus(f),
		PilotGuidance:      flightPilotGuidance(f),
		LastError:          flightLastError(f),
		CreatedAt:          formatTime(f.CreatedAt),
		UpdatedAt:          formatTime(f.UpdatedAt),
	}
	for _, op := range f.Operations {
		out := contracts.FlightOperation{
			ID: op.ID, Action: string(op.Action), CorrelationID: op.CorrelationID, Status: string(op.Status),
			Resource: op.Resource, Error: op.Error, CreatedAt: formatTime(op.CreatedAt), UpdatedAt: formatTime(op.UpdatedAt),
		}
		for _, r := range op.Resources {
			out.Resources = append(out.Resources, contracts.FlightResourceOutcome{Kind: r.Kind, ID: r.ID, Status: string(r.Status), Error: r.Error})
		}
		d.Operations = append(d.Operations, out)
	}
	return d
}

func flightMonitor(f flights.Flight) *contracts.FlightMonitor {
	if f.Monitor == nil {
		return nil
	}
	out := &contracts.FlightMonitor{Error: f.Monitor.Error}
	for _, t := range f.Monitor.Turns {
		turn := contracts.FlightMonitoredTurn{
			ID: t.ID, SessionID: t.Turn.SessionID, StartSeq: t.Turn.StartSeq,
			EndSeq: t.Turn.EndSeq, Text: t.Turn.Text, Outcome: t.Turn.Outcome,
			Error: t.Turn.Error, Status: string(t.Status),
			CreatedAt: formatTime(t.CreatedAt), UpdatedAt: formatTime(t.UpdatedAt),
		}
		if t.Artifacts != nil {
			turn.Artifacts = &contracts.FlightArtifactReport{SpecPath: t.Artifacts.SpecPath, PlanPath: t.Artifacts.PlanPath, Error: t.Artifacts.Error}
		}
		if x := t.Execution; x != nil {
			turn.Execution = &contracts.FlightExecutionCheck{Skill: x.Skill, ReportPath: x.ReportPath, ReportVerified: x.ReportVerified, ReportError: x.ReportError}
		}
		if r := t.Review; r != nil {
			turn.Review = &contracts.FlightReviewCheck{Skill: r.Skill}
		}
		role := flightTurnRole(f, t.Turn.SessionID)
		for _, a := range t.Attempts {
			att := contracts.FlightMonitorAttempt{
				Error: a.Error, CreatedAt: formatTime(a.CreatedAt),
			}
			if a.Config != nil {
				att.Model = a.Config.Model
				for _, q := range a.Config.Roles[string(role)].Classifiers {
					question := contracts.FlightMonitorQuestion{ID: q.ID, Question: q.Question, NotifyOn: append([]string{}, q.NotifyOn...), MinConfidence: q.MinConfidence}
					for _, choice := range q.Choices {
						question.Choices = append(question.Choices, contracts.FlightMonitorChoice{ID: choice.ID, Description: choice.Description})
					}
					att.Questions = append(att.Questions, question)
				}
			}
			if a.Classification != nil {
				att.Model = a.Classification.Model
				for _, ans := range a.Classification.Answers {
					att.Answers = append(att.Answers, contracts.FlightMonitorAnswer{
						ID: ans.ID, Choice: ans.Choice, Confidence: ans.Confidence, Probabilities: ans.Probabilities,
					})
				}
			}
			turn.Attempts = append(turn.Attempts, att)
		}
		out.Turns = append(out.Turns, turn)
	}
	for _, m := range f.Monitor.Messages {
		out.Messages = append(out.Messages, contracts.FlightMessage{
			ID: m.ID, Kind: m.Kind, SourceTurnID: m.SourceTurnID, SenderSessionID: m.SenderSessionID,
			RecipientSessionID: m.RecipientSessionID, Text: m.Text,
			ChatMessageID: m.ChatMessageID, Status: string(m.Status), Error: m.Error,
			CreatedAt: formatTime(m.CreatedAt), UpdatedAt: formatTime(m.UpdatedAt),
		})
	}
	return out
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// flightWorkspace joins the recorded workspace assignment to the state store.
// A workspace that is gone stays visible as unavailable history.
func flightWorkspace(ws *flights.WorkspaceAssignment, st state.StateStore) *contracts.FlightWorkspaceAssignment {
	if ws == nil {
		return nil
	}
	_, available := st.GetWorkspace(ws.WorkspaceID)
	return &contracts.FlightWorkspaceAssignment{
		ID: ws.ID, RequestedBranch: ws.RequestedBranch, WorkspaceID: ws.WorkspaceID, Path: ws.Path,
		Branch: ws.Branch, VCS: ws.VCS, Outcome: string(ws.Outcome), Error: ws.Error, Available: available,
		CreatedAt: formatTime(ws.CreatedAt), UpdatedAt: formatTime(ws.UpdatedAt),
	}
}

func flightSessions(list []flights.SessionAssignment, st state.StateStore) []contracts.FlightSessionAssignment {
	var out []contracts.FlightSessionAssignment
	for _, s := range list {
		_, available := st.GetSession(s.SessionID)
		out = append(out, contracts.FlightSessionAssignment{
			ID: s.ID, Role: contracts.FlightSessionRole(s.Role), Target: s.Target, SessionID: s.SessionID,
			PriorSessionIDs: s.PriorSessionIDs, Outcome: string(s.Outcome), Error: s.Error,
			Available: s.SessionID != "" && available,
			CreatedAt: formatTime(s.CreatedAt), UpdatedAt: formatTime(s.UpdatedAt),
		})
	}
	return out
}

// flightLastError is the error of the most recent operation when it failed.
// A later successful operation clears it.
func flightLastError(f flights.Flight) string {
	if n := len(f.Operations); n > 0 && f.Operations[n-1].Status == flights.StatusFailed {
		return f.Operations[n-1].Error
	}
	return ""
}

// writeFlightError maps a flights error to its HTTP status.
func (h *FlightHandlers) writeFlightError(w http.ResponseWriter, action string, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, flights.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, flights.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, flights.ErrConflict):
		status = http.StatusConflict
	}
	if status == http.StatusInternalServerError {
		logging.Sub(h.logger, "flights").Error(action+" failed", "err", err)
	}
	writeJSONError(w, err.Error(), status)
}

// opContext detaches a transition from the request so a client that goes away
// cannot abort a half-finished workspace allocation or cleanup.
func opContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), flightOperationTimeout)
}

// decodeBody decodes a JSON request body into v. An empty body is invalid.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(v); err == nil {
		return true
	}
	writeJSONError(w, "invalid request body", http.StatusBadRequest)
	return false
}

func (h *FlightHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, contracts.FlightsResponse{Flights: flightSummaries(h.manager.List(), h.state)})
}

func (h *FlightHandlers) handleGet(w http.ResponseWriter, r *http.Request) {
	f, ok := h.manager.Get(chi.URLParam(r, "flightID"))
	if !ok {
		writeJSONError(w, "flight not found", http.StatusNotFound)
		return
	}
	writeJSON(w, flightDetail(f, h.state))
}

func (h *FlightHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req contracts.CreateFlightRequest
	if !decodeBody(w, r, &req) {
		return
	}
	f, err := h.manager.Create(r.Context(), flights.CreateRequest{
		Repo: req.Repo, Prompt: req.Prompt,
		PlannerModel: req.PlannerModel, ExecutorModel: req.ExecutorModel,
	})
	if err != nil {
		h.writeFlightError(w, "create", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(flightDetail(f, h.state)); err != nil {
		h.logger.Error("failed to encode response", "handler", "flight-create", "err", err)
	}
}

func (h *FlightHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req contracts.UpdateFlightRequest
	if !decodeBody(w, r, &req) {
		return
	}
	f, err := h.manager.Update(r.Context(), chi.URLParam(r, "flightID"), flights.UpdateRequest{
		WorkflowState: (*flights.WorkflowState)(req.WorkflowState),
		Repo:          req.Repo, Prompt: req.Prompt,
		PlannerModel: req.PlannerModel, ExecutorModel: req.ExecutorModel,
	})
	if err != nil {
		h.writeFlightError(w, "update", err)
		return
	}
	writeJSON(w, flightDetail(f, h.state))
}

func (h *FlightHandlers) handleReorder(w http.ResponseWriter, r *http.Request) {
	var req contracts.ReorderFlightsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.manager.ReorderBacklog(r.Context(), req.FlightIDs); err != nil {
		h.writeFlightError(w, "reorder", err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// transition runs one workflow transition and writes its result.
func (h *FlightHandlers) transition(w http.ResponseWriter, r *http.Request, action string, run func(ctx context.Context, id string) (flights.Flight, error)) {
	ctx, cancel := opContext(r)
	defer cancel()
	f, err := run(ctx, chi.URLParam(r, "flightID"))
	if err != nil {
		h.writeFlightError(w, action, err)
		return
	}
	writeJSON(w, contracts.FlightTransitionResponse{Flight: flightDetail(f, h.state)})
}

func (h *FlightHandlers) handleTakeoff(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "takeoff", h.manager.Takeoff)
}

func (h *FlightHandlers) handleExecution(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "execution", h.manager.EnterExecution)
}

func (h *FlightHandlers) handleReview(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "review", h.manager.EnterReview)
}

func (h *FlightHandlers) handleCompletion(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "completion", h.manager.EnterCompletion)
}

func (h *FlightHandlers) handleDispose(w http.ResponseWriter, r *http.Request) {
	var req contracts.FlightDisposeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	res, err := h.manager.Dispose(ctx, chi.URLParam(r, "flightID"), flights.DisposeConfirmation{
		WorkspaceID: req.WorkspaceID, Confirmed: req.Confirm,
	})
	if err != nil {
		h.writeFlightError(w, "dispose", err)
		return
	}
	writeJSON(w, contracts.FlightTransitionResponse{Flight: flightDetail(res.Flight, h.state), Removed: res.Removed})
}

func (h *FlightHandlers) handleMonitorRetry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := opContext(r)
	defer cancel()
	flightID := chi.URLParam(r, "flightID")
	if err := h.manager.RetryMonitor(ctx, flightID, chi.URLParam(r, "itemID")); err != nil {
		h.writeFlightError(w, "monitor-retry", err)
		return
	}
	f, ok := h.manager.Get(flightID)
	if !ok {
		writeJSONError(w, "flight not found", http.StatusNotFound)
		return
	}
	writeJSON(w, contracts.FlightTransitionResponse{Flight: flightDetail(f, h.state)})
}

// flightExecutionEvidence returns the flight's execution start time, report
// target, and verified report path in the contract's string form.
func flightExecutionEvidence(f flights.Flight) (startedAt, target, path string) {
	if f.ExecutionStartedAt != nil {
		startedAt = formatTime(*f.ExecutionStartedAt)
	}
	return startedAt, f.ReportTarget, f.ReportPath
}

// flightReviewStartedAt returns the review start time in the contract's
// string form.
func flightReviewStartedAt(f flights.Flight) string {
	if f.ReviewStartedAt == nil {
		return ""
	}
	return formatTime(*f.ReviewStartedAt)
}

// flightPilotStop returns the pilot's stop record while the flight is still
// in the phase the stop was recorded in. The record itself is retained after
// the phase changes; only the display stops.
func flightPilotStop(f flights.Flight) *contracts.FlightPilotStop {
	if f.PilotStop == nil || f.PilotStop.WorkflowState != f.WorkflowState {
		return nil
	}
	return &contracts.FlightPilotStop{
		WorkflowState: contracts.FlightWorkflowState(f.PilotStop.WorkflowState),
		Reason:        f.PilotStop.Reason,
		SourceTurnID:  f.PilotStop.SourceTurnID,
		CreatedAt:     formatTime(f.PilotStop.CreatedAt),
	}
}

// flightReviewStatus derives the latest reviewer classification in words from
// the flight's retained monitor turns. It reads the daemon's own record, not
// the reviewer's claims. Superseded reviewers count too: when the pilot starts
// the next reviewer, the reading that explains why stays until the new
// reviewer has a classified turn of its own.
func flightReviewStatus(f flights.Flight) *contracts.FlightReviewStatus {
	if f.Monitor == nil {
		return nil
	}
	reviewers := map[string]bool{}
	for _, s := range f.Sessions {
		if s.Role != flights.RoleReviewer {
			continue
		}
		if s.SessionID != "" {
			reviewers[s.SessionID] = true
		}
		for _, id := range s.PriorSessionIDs {
			reviewers[id] = true
		}
	}
	if len(reviewers) == 0 {
		return nil
	}
	var latest *contracts.FlightReviewStatus
	for _, turn := range f.Monitor.Turns {
		if !reviewers[turn.Turn.SessionID] {
			continue
		}
		var answers []flights.MonitorAnswer
		for _, attempt := range turn.Attempts {
			if attempt.Classification == nil || attempt.Error != "" {
				continue
			}
			answers = attempt.Classification.Answers
		}
		if len(answers) == 0 {
			continue
		}
		status := &contracts.FlightReviewStatus{
			ReviewerSessionID: turn.Turn.SessionID,
			UpdatedAt:         formatTime(turn.UpdatedAt),
		}
		for _, answer := range answers {
			switch answer.ID {
			case "review_state":
				status.State = answer.Choice
			case "tests":
				status.Tests = answer.Choice
			case "test_rules_review":
				status.TestRulesReview = answer.Choice
			case "changed_work":
				status.ChangedWork = answer.Choice
			case "artifacts_named":
				status.ArtifactsNamed = answer.Choice
			}
		}
		latest = status
	}
	return latest
}

// flightPilotGuidance returns the pilot's latest guidance that was delivered
// to its recipient or is held for it. Monitor history keeps every message;
// this is only the one the list and detail lead with.
func flightPilotGuidance(f flights.Flight) *contracts.FlightPilotGuidance {
	if f.Monitor == nil {
		return nil
	}
	for i := len(f.Monitor.Messages) - 1; i >= 0; i-- {
		msg := f.Monitor.Messages[i]
		if msg.Kind != "pilot_message" || (msg.Status != flights.MessageStatusAccepted && msg.Status != flights.MessageStatusQueued) {
			continue
		}
		return &contracts.FlightPilotGuidance{
			RecipientRole:      contracts.FlightSessionRole(flightTurnRole(f, msg.RecipientSessionID)),
			RecipientSessionID: msg.RecipientSessionID,
			Text:               msg.Text,
			Status:             string(msg.Status),
			CreatedAt:          formatTime(msg.CreatedAt),
		}
	}
	return nil
}

// flightTurnRole names the role of the session that produced a retained turn,
// including sessions a restart or retry replaced. It is empty for a session the
// flight does not assign.
func flightTurnRole(f flights.Flight, sessionID string) flights.SessionRole {
	for _, s := range f.Sessions {
		if s.SessionID == sessionID || slices.Contains(s.PriorSessionIDs, sessionID) {
			return s.Role
		}
	}
	return ""
}
