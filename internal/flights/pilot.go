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

// A pilot decision comes only from a completed response on its assigned chat
// stream. It takes exactly one of three forms: guidance for a working
// session, a request for the next reviewer, or a stop. The response names
// the recipient's role; the daemon resolves it to the flight's currently
// assigned session, and the response cannot name a session ID.
//
// Every form but null guidance carries the notification it answers.
type pilotDecision struct {
	NotificationID string          `json:"notification_id"`
	To             json.RawMessage `json:"to"`
	Guidance       json.RawMessage `json:"guidance"`
	NextReviewer   json.RawMessage `json:"next_reviewer"`
	Stop           json.RawMessage `json:"stop"`
}

// decisionForm is which of the decision's forms the pilot answered with.
type decisionForm int

const (
	formNone decisionForm = iota
	formNullGuidance
	formGuidance
	formNextReviewer
	formStop
)

// decodePilotDecision reads the pilot's response as exactly one decision
// object. It rejects unknown fields, more than one decision form, a decision
// with no form at all, and forms that do not fit their fields.
func decodePilotDecision(text string) (pilotDecision, decisionForm, error) {
	var decision pilotDecision
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		if !strings.HasPrefix(strings.TrimSpace(text), "{") {
			return decision, formNone, errors.New("the response is prose, not a JSON decision")
		}
		return decision, formNone, fmt.Errorf("the response is not a valid JSON decision (%v)", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return decision, formNone, errors.New("pilot decision must contain one JSON object")
	}

	present := 0
	for _, field := range []json.RawMessage{decision.Guidance, decision.NextReviewer, decision.Stop} {
		if len(field) > 0 {
			present++
		}
	}
	switch {
	case present > 1:
		return decision, formNone, errors.New("pilot decision must contain exactly one of guidance, next_reviewer, or stop")
	case present == 0:
		return decision, formNone, errors.New("pilot decision requires guidance (a string or null), next_reviewer, or stop")
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

// pilotRecipientRoles returns the roles the pilot may address with guidance
// in a phase. The phases differ because each has a different worker: the
// planner plans, the executor executes, the reviewer reviews, and the
// committer commits.
func pilotRecipientRoles(state WorkflowState) []SessionRole {
	switch state {
	case StatePlanning:
		return []SessionRole{RolePlanner}
	case StateExecuting:
		return []SessionRole{RoleExecutor}
	case StateReviewing:
		return []SessionRole{RoleReviewer}
	case StateCompleting:
		return []SessionRole{RoleCommit, RoleReviewer}
	}
	return nil
}

// pilotSourceRoles returns the roles whose notifications may back a decision
// in a phase. A decision answers a notification about a session the monitor
// currently follows.
func pilotSourceRoles(state WorkflowState) []SessionRole {
	switch state {
	case StatePlanning:
		return []SessionRole{RolePlanner}
	case StateExecuting:
		return []SessionRole{RoleExecutor}
	case StateReviewing:
		return []SessionRole{RoleReviewer}
	case StateCompleting:
		return []SessionRole{RoleCommit, RoleReviewer}
	}
	return nil
}

// recipientAllowed reports whether the pilot may address a role in a phase.
func recipientAllowed(state WorkflowState, role SessionRole) bool {
	for _, allowed := range pilotRecipientRoles(state) {
		if allowed == role {
			return true
		}
	}
	return false
}

// sourceAllowed reports whether a decision may answer a notification about a
// role in a phase.
func sourceAllowed(state WorkflowState, role SessionRole) bool {
	for _, allowed := range pilotSourceRoles(state) {
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

// A pilot response is a decision only when its turn answered a message schmux
// sent the pilot: a notification, execution notice, or rejection. A turn that
// answered only the user's own messages is the pilot talking to the user; it is
// not parsed and sends nothing.
//
// A rejected decision from a completed response goes back to the pilot as a
// rejection message so it can answer again. It goes back once: a rejected
// decision that follows a rejection is not returned, which bounds the exchange,
// and its error says so in monitor history.
func (m *Manager) processPilotTurn(ctx context.Context, flightID string, t MonitoredTurn) {
	answered, err := m.answeredFlightMessage(flightID, t)
	if err == nil && answered != nil {
		err = m.recordPilotGuidance(ctx, flightID, t)
	}
	if ctx.Err() != nil {
		return
	}
	attempt := MonitorAttempt{}
	if err != nil {
		attempt.Error = err.Error()
	}
	if err != nil && answered != nil && t.Turn.Outcome == "completed" {
		if m.followsRejection(flightID, t) {
			attempt.Error += "; not returned to the pilot because its previous decision was also rejected"
		} else if _, rejectErr := m.recordFlightMessage(flightID, FlightMessage{
			Kind:               "rejection",
			SourceTurnID:       t.ID,
			RecipientSessionID: t.Turn.SessionID,
			Text:               rejectionText(*answered, err),
			Status:             MessageStatusPending,
		}); rejectErr != nil {
			m.logger.Warn("pilot: record rejection failed", "err", rejectErr)
		}
	}
	if err := m.recordMonitorAttempt(flightID, t.ID, attempt); err != nil {
		m.logger.Warn("pilot: retain decision failed", "err", err)
	}
}

// answeredFlightMessage returns the last message schmux sent the pilot that
// the turn answered, or nil when the turn answered only the user. A turn with
// no recorded input cannot be attributed, which is an error.
func (m *Manager) answeredFlightMessage(flightID string, t MonitoredTurn) (*FlightMessage, error) {
	if len(t.Turn.InputIDs) == 0 {
		return nil, errors.New("the pilot turn has no recorded input message, so schmux cannot tell whether it answers the flight or the user")
	}
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return nil, nil
	}
	var answered *FlightMessage
	for i, msg := range f.Monitor.Messages {
		if msg.ChatMessageID != "" && slices.Contains(t.Turn.InputIDs, msg.ChatMessageID) {
			answered = &f.Monitor.Messages[i]
		}
	}
	return answered, nil
}

// rejectionText tells the pilot which message its rejected response answered,
// why it was rejected, and the decision shapes that message needs.
func rejectionText(answered FlightMessage, err error) string {
	about, id := "schmux's "+strings.ReplaceAll(answered.Kind, "_", " "), "<Notification ID>"
	if answered.Kind == "notification" {
		about, id = "notification "+answered.ID, answered.ID
	}
	return "Schmux rejected your response to " + about + " and delivered nothing: " + err.Error() + ".\n" +
		`Answer a schmux message with exactly one JSON object and nothing else, carrying exactly one of guidance, next_reviewer, or stop: ` +
		`{"notification_id":"` + id + `","to":"reviewer","guidance":"..."} sends guidance ("to" may be "planner", "executor", "reviewer", or "commit", depending on the phase), ` +
		`{"notification_id":"` + id + `","next_reviewer":"..."} starts a fresh reviewer, ` +
		`{"notification_id":"` + id + `","stop":"..."} records that the flight has gone off the rails, and {"guidance":null} sends nothing.`
}

// followsRejection reports whether the session's response before t was
// returned to it as a rejection.
func (m *Manager) followsRejection(flightID string, t MonitoredTurn) bool {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return false
	}
	var previous *MonitoredTurn
	for i, other := range f.Monitor.Turns {
		if other.Turn.SessionID == t.Turn.SessionID && other.Turn.EndSeq < t.Turn.EndSeq && (previous == nil || other.Turn.EndSeq > previous.Turn.EndSeq) {
			previous = &f.Monitor.Turns[i]
		}
	}
	if previous == nil {
		return false
	}
	for _, msg := range f.Monitor.Messages {
		if msg.Kind == "rejection" && msg.SourceTurnID == previous.ID {
			return true
		}
	}
	return false
}

// recordPilotGuidance applies the pilot's decision. The decision is one of
// three forms: guidance for a working session, a request for the next
// reviewer, or a stop.
func (m *Manager) recordPilotGuidance(ctx context.Context, flightID string, t MonitoredTurn) error {
	if t.Turn.Outcome != "completed" {
		return fmt.Errorf("pilot turn %s: %s", t.Turn.Outcome, t.Turn.Error)
	}
	if strings.TrimSpace(t.Turn.FinalText) == "" {
		return nil
	}
	decision, form, err := decodePilotDecision(t.Turn.FinalText)
	if err != nil {
		return err
	}
	switch form {
	case formNullGuidance:
		// Null guidance sends nothing, so nothing addresses a source.
		return nil
	case formGuidance:
		return m.deliverPilotGuidance(ctx, flightID, t, decision)
	case formNextReviewer:
		return m.startNextReviewer(ctx, flightID, t, decision)
	case formStop:
		return m.recordPilotStop(flightID, t, decision)
	}
	return nil
}

// pilotDecisionSource returns the role that produced the notification the
// decision answered. It is empty unless the notification was delivered to the
// pilot and is about a session the monitor currently follows.
func pilotDecisionSource(f Flight, notificationID, pilotSessionID string) (SessionRole, bool) {
	if f.Monitor == nil {
		return "", false
	}
	_, working, ok := monitoredSessions(f)
	if !ok {
		return "", false
	}
	for _, msg := range f.Monitor.Messages {
		if msg.ID != notificationID || msg.Kind != "notification" || msg.RecipientSessionID != pilotSessionID || msg.ChatMessageID == "" || (msg.Status != MessageStatusAccepted && msg.Status != MessageStatusQueued) {
			continue
		}
		for _, source := range f.Monitor.Turns {
			if source.ID != msg.SourceTurnID {
				continue
			}
			if role, followed := working[source.Turn.SessionID]; followed {
				return role, true
			}
		}
	}
	return "", false
}

// deliverPilotGuidance sends the pilot's guidance to the session it names.
// The recipient must be a role the flight's current phase lets the pilot
// address, and the decision must answer a delivered notification about a
// session the monitor follows.
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
	if !recipientAllowed(f.WorkflowState, recipientRole) {
		return conflict("pilot decision requires \"to\": %s", strings.Join(roleNames(pilotRecipientRoles(f.WorkflowState)), " or "))
	}
	recipient := f.sessionFor(recipientRole)
	if recipient == nil || recipient.SessionID == "" || recipient.Outcome != StatusSucceeded {
		return conflict("flight has no assigned %s", recipientRole)
	}
	if m.deps.Chats == nil {
		return errors.New("pilot chat dependency is not wired")
	}
	if !m.deps.Chats.IsRunning(ctx, recipient.SessionID) {
		return conflict("%s session is not running", recipientRole)
	}
	if m.deps.Chats.SessionWorkspaceID(pilot.SessionID) != f.Workspace.WorkspaceID || m.deps.Chats.SessionWorkspaceID(recipient.SessionID) != f.Workspace.WorkspaceID {
		return conflict("pilot and %s must belong to the flight workspace", recipientRole)
	}
	sourceRole, ok := pilotDecisionSource(f, decision.NotificationID, pilot.SessionID)
	if !ok || !sourceAllowed(f.WorkflowState, sourceRole) {
		return conflict("pilot decision must reference a delivered notification for a current %s turn", f.WorkflowState)
	}
	_, err := m.recordFlightMessage(flightID, FlightMessage{
		Kind:                 "pilot_message",
		SourceTurnID:         t.ID,
		SourceNotificationID: decision.NotificationID,
		SenderSessionID:      pilot.SessionID,
		RecipientSessionID:   recipient.SessionID,
		Text:                 *text,
		Status:               MessageStatusPending,
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
		Paragraph:            paragraph,
		SourceTurnID:         t.ID,
		SourceNotificationID: decision.NotificationID,
		PilotSessionID:       t.Turn.SessionID,
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
	if _, ok := pilotDecisionSource(f, decision.NotificationID, pilot.SessionID); !ok {
		return conflict("pilot decision must reference a delivered notification for a current %s turn", f.WorkflowState)
	}
	now := m.deps.Now()
	if err := m.editFlight(flightID, func(f *Flight) error {
		f.PilotStop = &PilotStop{
			WorkflowState:        f.WorkflowState,
			Reason:               reason,
			SourceTurnID:         t.ID,
			SourceNotificationID: decision.NotificationID,
			CreatedAt:            now,
		}
		return nil
	}); err != nil {
		return err
	}
	_, err = m.recordFlightMessage(flightID, FlightMessage{
		Kind:                 "pilot_stop",
		SourceTurnID:         t.ID,
		SourceNotificationID: decision.NotificationID,
		SenderSessionID:      pilot.SessionID,
		RecipientSessionID:   pilot.SessionID,
		Text:                 reason,
		Status:               MessageStatusAccepted,
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
	switch msg.Kind {
	case "pilot_message":
		text = pilotPrefix + text
	case "notification":
		text = "Notification ID: " + msg.ID + "\n" + text
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
