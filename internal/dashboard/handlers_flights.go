package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
	r.Get("/flights", h.handleList)
	r.Get("/flights/{flightID}", h.handleGet)
}

// registerMutatingRoutes mounts the state-changing flight routes; the caller
// puts them behind the CSRF middleware.
func (h *FlightHandlers) registerMutatingRoutes(r chi.Router) {
	r.Post("/flights", h.handleCreate)
	r.Post("/flights/order", h.handleReorder)
	r.Patch("/flights/{flightID}", h.handleUpdate)
	r.Delete("/flights/{flightID}", h.handleDelete)
	r.Post("/flights/{flightID}/takeoff", h.handleTakeoff)
	r.Post("/flights/{flightID}/execution", h.handleExecution)
	r.Post("/flights/{flightID}/completion", h.handleCompletion)
	r.Post("/flights/{flightID}/done", h.handleDone)
	r.Post("/flights/{flightID}/cancel", h.handleCancel)
	r.Post("/flights/{flightID}/terminate", h.handleTerminate)
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
	return contracts.FlightSummaryResponse{
		ID:            f.ID,
		Repo:          f.Repo,
		ShortName:     f.ShortName,
		PlannerModel:  f.PlannerModel,
		ExecutorModel: f.ExecutorModel,
		WorkflowState: contracts.FlightWorkflowState(f.WorkflowState),
		CleanupState:  contracts.FlightCleanupState(f.CleanupState),
		Order:         f.Order,
		Workspace:     flightWorkspace(f.Workspace, st),
		Sessions:      flightSessions(f.Sessions, st),
		LastError:     flightLastError(f),
		CreatedAt:     formatTime(f.CreatedAt),
		UpdatedAt:     formatTime(f.UpdatedAt),
	}
}

func flightDetail(f flights.Flight, st state.StateStore) contracts.FlightDetailResponse {
	d := contracts.FlightDetailResponse{
		ID:            f.ID,
		Repo:          f.Repo,
		ShortName:     f.ShortName,
		Prompt:        f.Prompt,
		PlannerModel:  f.PlannerModel,
		ExecutorModel: f.ExecutorModel,
		WorkflowState: contracts.FlightWorkflowState(f.WorkflowState),
		CleanupState:  contracts.FlightCleanupState(f.CleanupState),
		Order:         f.Order,
		Workspace:     flightWorkspace(f.Workspace, st),
		Sessions:      flightSessions(f.Sessions, st),
		ArtifactPath:  f.ArtifactPath,
		CancelReason:  f.CancelReason,
		LastError:     flightLastError(f),
		CreatedAt:     formatTime(f.CreatedAt),
		UpdatedAt:     formatTime(f.UpdatedAt),
	}
	if f.CancelledAt != nil {
		d.CancelledAt = formatTime(*f.CancelledAt)
	}
	if f.DoneAt != nil {
		d.DoneAt = formatTime(*f.DoneAt)
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

// decodeBody decodes a JSON request body into v. An empty body is allowed when
// optional is true.
func decodeBody(w http.ResponseWriter, r *http.Request, v any, optional bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil || (optional && errors.Is(err, io.EOF)) {
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
	if !decodeBody(w, r, &req, false) {
		return
	}
	f, err := h.manager.Create(r.Context(), flights.CreateRequest{
		Repo: req.Repo, Prompt: req.Prompt, ShortName: req.ShortName,
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
	if !decodeBody(w, r, &req, false) {
		return
	}
	f, err := h.manager.Update(r.Context(), chi.URLParam(r, "flightID"), flights.UpdateRequest{
		WorkflowState: (*flights.WorkflowState)(req.WorkflowState),
		Repo:          req.Repo, Prompt: req.Prompt, ShortName: req.ShortName,
		PlannerModel: req.PlannerModel, ExecutorModel: req.ExecutorModel,
	})
	if err != nil {
		h.writeFlightError(w, "update", err)
		return
	}
	writeJSON(w, flightDetail(f, h.state))
}

func (h *FlightHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.manager.Delete(r.Context(), chi.URLParam(r, "flightID")); err != nil {
		h.writeFlightError(w, "delete", err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *FlightHandlers) handleReorder(w http.ResponseWriter, r *http.Request) {
	var req contracts.ReorderFlightsRequest
	if !decodeBody(w, r, &req, false) {
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

func (h *FlightHandlers) handleCompletion(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "completion", h.manager.EnterCompletion)
}

func (h *FlightHandlers) handleDone(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "done", h.manager.MarkDone)
}

func (h *FlightHandlers) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req contracts.FlightCancelRequest
	if !decodeBody(w, r, &req, false) {
		return
	}
	h.transition(w, r, "cancel", func(ctx context.Context, id string) (flights.Flight, error) {
		return h.manager.Cancel(ctx, id, req.Reason)
	})
}

func (h *FlightHandlers) handleTerminate(w http.ResponseWriter, r *http.Request) {
	var req contracts.FlightTerminateRequest
	if !decodeBody(w, r, &req, false) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	res, err := h.manager.Terminate(ctx, chi.URLParam(r, "flightID"), flights.TerminateConfirmation{
		WorkspaceID: req.WorkspaceID, Confirmed: req.Confirm,
	})
	if err != nil {
		h.writeFlightError(w, "terminate", err)
		return
	}
	writeJSON(w, contracts.FlightTransitionResponse{Flight: flightDetail(res.Flight, h.state), Removed: res.Removed})
}
