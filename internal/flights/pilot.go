package flights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// pilotPrefix is added to every pilot message by the daemon for visible chat
// attribution. The flight record retains the exact unprefixed text; the prefix
// is only ever seen in the recipient's conversation log.
const pilotPrefix = "[Pilot] "

// supervisedPhase reports whether the flight is in a phase the pilot
// supervises. Messages reach the pilot only while it is.
func supervisedPhase(state WorkflowState) bool {
	switch state {
	case StatePlanning, StateExecuting, StateReviewing, StateCompleting:
		return true
	}
	return false
}

// A pilot decision is the completed final response of the pilot's chat
// stream when that response is one JSON object. It takes exactly one of four
// forms: guidance for a working session, a request for the next reviewer, a
// stop, or an advance to the next phase. The response names the recipient's
// role; the daemon resolves it to the flight's currently assigned session,
// and the response cannot name a session ID. The pilot is the flight's
// orchestrator: it decides when to act, and schmux does not tie a decision
// to the message that prompted it.
type pilotDecision struct {
	NotificationID json.RawMessage `json:"notification_id,omitempty"` // accepted for pilots prompted by an earlier build; ignored
	To             json.RawMessage `json:"to"`
	Guidance       json.RawMessage `json:"guidance"`
	NextReviewer   json.RawMessage `json:"next_reviewer"`
	Stop           json.RawMessage `json:"stop"`
	Advance        json.RawMessage `json:"advance"`
}

// decisionForm is which of the decision's forms the pilot chose.
type decisionForm int

const (
	formNone decisionForm = iota
	formNullGuidance
	formGuidance
	formNextReviewer
	formStop
	formAdvance
)

// advanceDestinations are the phases the pilot may advance to. There are no
// others: the pilot cannot ask for the backlog, for completion, or for the
// flight to be disposed.
var advanceDestinations = []string{"executing", "reviewing"}

// isDecision reports whether a pilot response is a JSON decision rather than
// prose for the user: it is an object, optionally wrapped in a code fence.
func isDecision(text string) bool {
	return strings.HasPrefix(unfence(text), "{")
}

// unfence strips a surrounding Markdown code fence from a pilot response, so
// a decision the model fenced despite the prompt is still read.
func unfence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```")
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		text = text[nl+1:]
	} else {
		text = ""
	}
	text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	return strings.TrimSpace(text)
}

// decodePilotDecision reads the pilot's response as exactly one decision
// object. It rejects unknown fields, more than one decision form, a decision
// with no form at all, and forms that do not fit their fields.
func decodePilotDecision(text string) (pilotDecision, decisionForm, error) {
	var decision pilotDecision
	decoder := json.NewDecoder(strings.NewReader(unfence(text)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return decision, formNone, fmt.Errorf("the response is not a valid JSON decision (%v)", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return decision, formNone, errors.New("pilot decision must contain one JSON object")
	}

	present := 0
	for _, field := range []json.RawMessage{decision.Guidance, decision.NextReviewer, decision.Stop, decision.Advance} {
		if len(field) > 0 {
			present++
		}
	}
	switch {
	case present > 1:
		return decision, formNone, errors.New("pilot decision must contain exactly one of guidance, next_reviewer, stop, or advance")
	case present == 0:
		return decision, formNone, errors.New("pilot decision requires guidance (a string or null), next_reviewer, stop, or advance")
	}

	switch {
	case len(decision.Guidance) > 0:
		var text *string
		if err := json.Unmarshal(decision.Guidance, &text); err != nil {
			return decision, formNone, fmt.Errorf("pilot guidance: %w", err)
		}
		if text == nil {
			return decision, formNullGuidance, nil
		}
		if strings.TrimSpace(*text) == "" {
			return decision, formNone, errors.New("pilot guidance must be nonblank or null")
		}
		if len(decision.To) == 0 {
			return decision, formNone, errors.New(`pilot decision requires "to"`)
		}
		return decision, formGuidance, nil
	case len(decision.NextReviewer) > 0:
		if len(decision.To) > 0 {
			return decision, formNone, errors.New(`pilot next_reviewer must not name a "to" recipient`)
		}
		if _, err := decisionText(decision.NextReviewer, "next_reviewer"); err != nil {
			return decision, formNone, err
		}
		return decision, formNextReviewer, nil
	case len(decision.Advance) > 0:
		if len(decision.To) > 0 {
			return decision, formNone, errors.New(`pilot advance must not name a "to" recipient`)
		}
		if _, err := advanceDestination(decision.Advance); err != nil {
			return decision, formNone, err
		}
		return decision, formAdvance, nil
	default:
		if len(decision.To) > 0 {
			return decision, formNone, errors.New(`pilot stop must not name a "to" recipient`)
		}
		if _, err := decisionText(decision.Stop, "stop"); err != nil {
			return decision, formNone, err
		}
		return decision, formStop, nil
	}
}

// decisionText decodes a decision field that must hold a nonblank string.
func decisionText(field json.RawMessage, name string) (string, error) {
	var text string
	if err := json.Unmarshal(field, &text); err != nil {
		return "", fmt.Errorf("pilot %s must be a string", name)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("pilot %s must be nonblank", name)
	}
	return text, nil
}

// advanceDestination reads the phase the pilot asks to advance to. Only the
// two automatic transitions are destinations: the pilot cannot ask for the
// backlog, for completion, or for the flight to be disposed.
func advanceDestination(field json.RawMessage) (string, error) {
	var destination *string
	if err := json.Unmarshal(field, &destination); err != nil {
		return "", errors.New("pilot advance must be a string")
	}
	if destination == nil {
		return "", errors.New("pilot advance must be a string")
	}
	if strings.TrimSpace(*destination) == "" {
		return "", errors.New("pilot advance must be nonblank")
	}
	if !slices.Contains(advanceDestinations, *destination) {
		return "", fmt.Errorf("pilot advance must be %s", strings.Join(advanceDestinations, " or "))
	}
	return *destination, nil
}

// pilotRecipientRoles returns the roles the pilot may address with guidance:
// every working role whose session the flight has started, whatever the
// phase. The pilot orchestrates all of the flight's sessions.
func pilotRecipientRoles(f Flight) []SessionRole {
	var roles []SessionRole
	for _, role := range workingRoles {
		if _, ok := startedSession(f, role); ok {
			roles = append(roles, role)
		}
	}
	return roles
}

// recipientAllowed reports whether the pilot may address a role: the flight
// must have started its session.
func recipientAllowed(f Flight, role SessionRole) bool {
	for _, allowed := range pilotRecipientRoles(f) {
		if allowed == role {
			return true
		}
	}
	return false
}

// roleNames names roles for a message to the pilot.
func roleNames(roles []SessionRole) []string {
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		out = append(out, string(role))
	}
	return out
}

// Every completed pilot turn is read for a decision, whatever prompted it: a
// notification, a launch notice, or the user. A response that is not a JSON
// object is the pilot talking to the user; it is retained, not parsed, and
// sends nothing.
//
// A decision that cannot be applied is recorded on the turn as a visible
// error and nothing is sent anywhere. Schmux does not send the pilot a
// rejection to answer again: the pilot decides when to act, and the user sees
// the error on the flight (the turn can be retried from there once the cause
// is fixed).
func (m *Manager) processPilotTurn(ctx context.Context, flightID string, t MonitoredTurn) {
	err := m.recordPilotGuidance(ctx, flightID, t)
	if ctx.Err() != nil {
		return
	}
	attempt := MonitorAttempt{}
	if err != nil {
		attempt.Error = err.Error()
	}
	if err := m.recordMonitorAttempt(flightID, t.ID, attempt); err != nil {
		m.logger.Warn("pilot: retain decision failed", "err", err)
	}
}

// recordPilotGuidance applies the pilot's decision. The decision is one of
// three forms: guidance for a working session, a request for the next
// reviewer, or a stop. A response that is not a JSON object is prose for the
// user and applies nothing.
func (m *Manager) recordPilotGuidance(ctx context.Context, flightID string, t MonitoredTurn) error {
	if t.Turn.Outcome != "completed" {
		return fmt.Errorf("pilot turn %s: %s", t.Turn.Outcome, t.Turn.Error)
	}
	if !isDecision(t.Turn.FinalText) {
		return nil
	}
	decision, form, err := decodePilotDecision(t.Turn.FinalText)
	if err != nil {
		return err
	}
	switch form {
	case formNullGuidance:
		return nil
	case formGuidance:
		return m.deliverPilotGuidance(ctx, flightID, t, decision)
	case formNextReviewer:
		return m.startNextReviewer(ctx, flightID, t, decision)
	case formStop:
		return m.recordPilotStop(flightID, t, decision)
	case formAdvance:
		return m.advancePilotPhase(ctx, flightID, t, decision)
	}
	return nil
}

// advancePilotPhase applies the pilot's advance decision. The pilot moves the
// flight from planning to executing, or from executing to reviewing; no other
// destination is accepted, and completion stays the user's action.
//
// The decision is applied under the flight's operation lock, so a manual
// action that races it produces one session and not two. A decision that was
// already applied is applied again and starts nothing.
func (m *Manager) advancePilotPhase(ctx context.Context, flightID string, t MonitoredTurn, decision pilotDecision) error {
	destination, err := advanceDestination(decision.Advance)
	if err != nil {
		return err
	}

	unlock := m.lockFlight(flightID)
	defer unlock()

	f, ok := m.Get(flightID)
	if !ok {
		return ErrNotFound
	}
	// What the flight already recorded for this turn comes first: a decision
	// that was consumed is consumed, and an attempt whose outcome is not
	// established is not retried behind the user's back.
	switch f.sourceOperation(t.ID) {
	case sourceConsumed:
		return nil
	case sourceUnresolved:
		return conflict("flight %s has an unresolved operation for pilot turn %s", f.ID, t.ID)
	}
	if err := requireIdle(&f); err != nil {
		return err
	}
	if !supervisedPhase(f.WorkflowState) {
		return conflict("flight %s is %s; it is no longer supervised", f.ID, f.WorkflowState)
	}
	if err := requireWorkspace(&f); err != nil {
		return err
	}
	pilot := f.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID != t.Turn.SessionID || pilot.Outcome != StatusSucceeded {
		return conflict("response is not from the assigned pilot")
	}
	if m.deps.Chats == nil {
		return errors.New("pilot chat dependency is not wired")
	}
	if m.deps.Chats.SessionWorkspaceID(pilot.SessionID) != f.Workspace.WorkspaceID {
		return conflict("the pilot and the flight must belong to the same workspace")
	}
	// The deciding turn must belong to the flight's current phase visit. A
	// turn that began in an earlier visit cannot advance this one, and a
	// turn whose start is unknown cannot advance anything.
	if t.PhaseVisit == 0 {
		return fmt.Errorf("pilot turn %s has no phase visit evidence; its start could not be placed", t.ID)
	}
	if t.PhaseVisit != f.PhaseVisit {
		return conflict("pilot turn %s belongs to phase visit %d; the flight is in visit %d", t.ID, t.PhaseVisit, f.PhaseVisit)
	}
	if f.HasCurrentPilotStop() {
		return conflict("flight %s has a pilot stop in this phase: %s", f.ID, f.PilotStop.Reason)
	}
	switch destination {
	case "executing":
		if f.WorkflowState != StatePlanning {
			return conflict("flight %s is %s; only a planning flight can advance to executing", f.ID, f.WorkflowState)
		}
	case "reviewing":
		if f.WorkflowState != StateExecuting {
			return conflict("flight %s is %s; only an executing flight can advance to reviewing", f.ID, f.WorkflowState)
		}
	}

	source := &advanceRequest{SourceTurnID: t.ID, PilotSessionID: t.Turn.SessionID, PhaseVisit: t.PhaseVisit}
	switch destination {
	case "executing":
		_, err = m.enterExecutionLocked(ctx, flightID, source)
	case "reviewing":
		_, err = m.enterReviewLocked(ctx, flightID, source)
	}
	return err
}

// deliverPilotGuidance sends the pilot's guidance to the session it names.
// The recipient may be any working role whose session the flight has started.
func (m *Manager) deliverPilotGuidance(ctx context.Context, flightID string, t MonitoredTurn, decision pilotDecision) error {
	var text *string
	if err := json.Unmarshal(decision.Guidance, &text); err != nil {
		return fmt.Errorf("pilot guidance: %w", err)
	}
	if text == nil {
		return nil
	}
	var to string
	if err := json.Unmarshal(decision.To, &to); err != nil {
		return fmt.Errorf("pilot decision requires \"to\": a session role")
	}
	unlock := m.lockFlight(flightID)
	defer unlock()
	f, ok := m.Get(flightID)
	if !ok {
		return ErrNotFound
	}
	if err := requireIdle(&f); err != nil {
		return err
	}
	if !supervisedPhase(f.WorkflowState) {
		return conflict("flight %s is %s; it is no longer supervised", f.ID, f.WorkflowState)
	}
	if err := requireWorkspace(&f); err != nil {
		return err
	}
	pilot := f.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID != t.Turn.SessionID || pilot.Outcome != StatusSucceeded {
		return conflict("response is not from the assigned pilot")
	}
	recipientRole := SessionRole(to)
	if !recipientAllowed(f, recipientRole) {
		return conflict("pilot decision requires \"to\": %s", strings.Join(roleNames(pilotRecipientRoles(f)), " or "))
	}
	recipient := f.sessionFor(recipientRole)
	if m.deps.Chats == nil {
		return errors.New("pilot chat dependency is not wired")
	}
	if !m.deps.Chats.IsRunning(ctx, recipient.SessionID) {
		return conflict("%s session is not running", recipientRole)
	}
	if m.deps.Chats.SessionWorkspaceID(pilot.SessionID) != f.Workspace.WorkspaceID || m.deps.Chats.SessionWorkspaceID(recipient.SessionID) != f.Workspace.WorkspaceID {
		return conflict("pilot and %s must belong to the flight workspace", recipientRole)
	}
	_, err := m.recordFlightMessage(flightID, FlightMessage{
		Kind:               "pilot_message",
		SourceTurnID:       t.ID,
		SenderSessionID:    pilot.SessionID,
		RecipientSessionID: recipient.SessionID,
		Text:               *text,
		Status:             MessageStatusPending,
	})
	return err
}

// startNextReviewer starts a fresh reviewer for the flight, as the pilot's
// next_reviewer decision asked. The paragraph tells the new reviewer what the
// previous one changed and what to re-check.
func (m *Manager) startNextReviewer(ctx context.Context, flightID string, t MonitoredTurn, decision pilotDecision) error {
	paragraph, err := decisionText(decision.NextReviewer, "next_reviewer")
	if err != nil {
		return err
	}
	_, err = m.startNextReviewerOperation(ctx, flightID, nextReviewRequest{
		Paragraph:      paragraph,
		SourceTurnID:   t.ID,
		PilotSessionID: t.Turn.SessionID,
	})
	return err
}

// hasNextReviewerMessage reports whether the pilot's turn already started a
// reviewer, so a replayed decision cannot start a second one.
func (m *Manager) hasNextReviewerMessage(flightID, sourceTurnID string) bool {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return false
	}
	for _, msg := range f.Monitor.Messages {
		if msg.Kind == "next_reviewer" && msg.SourceTurnID == sourceTurnID {
			return true
		}
	}
	return false
}

// recordPilotStop records the pilot's reason that the flight has gone off the
// rails. It sends no chat message, cancels nothing, and hides nothing: the
// record is visible until the phase changes.
func (m *Manager) recordPilotStop(flightID string, t MonitoredTurn, decision pilotDecision) error {
	reason, err := decisionText(decision.Stop, "stop")
	if err != nil {
		return err
	}
	unlock := m.lockFlight(flightID)
	defer unlock()
	f, ok := m.Get(flightID)
	if !ok {
		return ErrNotFound
	}
	if err := requireIdle(&f); err != nil {
		return err
	}
	if !supervisedPhase(f.WorkflowState) {
		return conflict("flight %s is %s; it is no longer supervised", f.ID, f.WorkflowState)
	}
	if err := requireWorkspace(&f); err != nil {
		return err
	}
	pilot := f.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID != t.Turn.SessionID || pilot.Outcome != StatusSucceeded {
		return conflict("response is not from the assigned pilot")
	}
	if t.PhaseVisit == 0 || t.PhaseVisit != f.PhaseVisit {
		return conflict("pilot stop turn %s does not belong to the current phase visit", t.ID)
	}
	now := m.deps.Now()
	if err := m.editFlight(flightID, func(f *Flight) error {
		f.PilotStop = &PilotStop{
			WorkflowState: f.WorkflowState,
			Reason:        reason,
			SourceTurnID:  t.ID,
			PhaseVisit:    t.PhaseVisit,
			CreatedAt:     now,
		}
		return nil
	}); err != nil {
		return err
	}
	_, err = m.recordFlightMessage(flightID, FlightMessage{
		Kind:               "pilot_stop",
		SourceTurnID:       t.ID,
		SenderSessionID:    pilot.SessionID,
		RecipientSessionID: pilot.SessionID,
		Text:               reason,
		Status:             MessageStatusAccepted,
	})
	return err
}

func (m *Manager) deliverFlightMessage(ctx context.Context, flightID, messageID string) (FlightMessage, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()
	msg, ok := m.findMonitoredMessage(flightID, messageID)
	if !ok {
		return FlightMessage{}, fmt.Errorf("monitor: message %s not found", messageID)
	}
	if msg.Status != MessageStatusPending {
		return msg, nil
	}
	return m.deliverFlightMessageLocked(ctx, flightID, msg)
}

// The flight operation lock serializes delivery with assignment changes and cleanup.
func (m *Manager) deliverFlightMessageLocked(ctx context.Context, flightID string, msg FlightMessage) (FlightMessage, error) {
	f, ok := m.Get(flightID)
	if !ok {
		return msg, ErrNotFound
	}
	if err := requireIdle(&f); err != nil {
		return msg, err
	}
	if ctx.Err() != nil {
		return msg, ctx.Err()
	}
	// A pilot message goes to whichever working session it was addressed to;
	// notifications, launch notices, and rejections go to the pilot.
	roles := []SessionRole{RolePilot}
	if msg.Kind == "pilot_message" {
		roles = []SessionRole{RolePlanner, RoleExecutor, RoleReviewer, RoleCommit}
	}
	var recipientRole SessionRole
	for _, role := range roles {
		if s := f.sessionFor(role); s != nil && s.SessionID == msg.RecipientSessionID {
			recipientRole = role
		}
	}
	var sendErr error
	if !supervisedPhase(f.WorkflowState) || recipientRole == "" {
		sendErr = conflict("message recipient is no longer assigned to this flight")
	}
	if msg.Kind == "pilot_message" {
		if pilot := f.sessionFor(RolePilot); pilot == nil || pilot.SessionID != msg.SenderSessionID {
			sendErr = conflict("message sender is no longer the assigned pilot")
		}
		if sendErr == nil && !m.deps.Chats.IsRunning(ctx, msg.RecipientSessionID) {
			sendErr = conflict("%s session is not running", recipientRole)
		}
		if sendErr == nil && (f.Workspace == nil ||
			m.deps.Chats.SessionWorkspaceID(msg.SenderSessionID) != f.Workspace.WorkspaceID ||
			m.deps.Chats.SessionWorkspaceID(msg.RecipientSessionID) != f.Workspace.WorkspaceID) {
			sendErr = conflict("pilot and %s must belong to the flight workspace", recipientRole)
		}
	}
	var rt *chat.Runtime
	if sendErr == nil {
		rt, sendErr = m.deps.Chats.GetChatRuntime(msg.RecipientSessionID)
	}
	if sendErr != nil {
		err := m.updateFlightMessage(flightID, msg.ID, func(message *FlightMessage) { message.Status = MessageStatusFailed; message.Error = sendErr.Error() })
		if err != nil {
			return msg, err
		}
		updated, _ := m.findMonitoredMessage(flightID, msg.ID)
		return updated, sendErr
	}
	if err := m.updateFlightMessage(flightID, msg.ID, func(message *FlightMessage) { message.Status = MessageStatusSending }); err != nil {
		return msg, err
	}
	text := msg.Text
	if msg.Kind == "pilot_message" {
		text = pilotPrefix + text
	}
	rec, queued, sendErr := rt.SendWithStatus(text, nil)
	status, detail := MessageStatusAccepted, ""
	switch {
	case sendErr != nil && rec.ID != "":
		status, detail = MessageStatusUnknown, sendErr.Error()
	case sendErr != nil:
		status, detail = MessageStatusFailed, sendErr.Error()
	case queued:
		status = MessageStatusQueued
	}
	if err := m.updateFlightMessage(flightID, msg.ID, func(message *FlightMessage) {
		message.Status, message.ChatMessageID, message.Error = status, rec.ID, detail
	}); err != nil {
		return msg, err
	}
	updated, _ := m.findMonitoredMessage(flightID, msg.ID)
	return updated, sendErr
}

func (m *Manager) observeQueuedMessage(flightID string, msg FlightMessage) {
	rt, err := m.deps.Chats.GetChatRuntime(msg.RecipientSessionID)
	if err != nil {
		return
	}
	sub, err := rt.Subscribe(0)
	if err != nil {
		return
	}
	defer rt.Unsubscribe(sub.Live)
	found, queued := false, false
	for _, rec := range sub.History {
		if rec.ID != msg.ChatMessageID {
			continue
		}
		if rec.Type == chat.RecordUserMessage {
			found = true
		}
		if rec.Type == chat.RecordUserMessageQueue && rec.Queued != nil {
			queued = *rec.Queued
		}
	}
	if found && !queued {
		_ = m.updateFlightMessage(flightID, msg.ID, func(message *FlightMessage) {
			if message.Status == MessageStatusQueued {
				message.Status = MessageStatusAccepted
			}
		})
	}
}
