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

// A pilot decision comes only from a completed response on its assigned chat
// stream. The response names the recipient's role; the daemon resolves it to
// the flight's currently assigned session, and the response cannot name a
// session ID.
type pilotDecision struct {
	NotificationID string          `json:"notification_id"`
	To             string          `json:"to"`
	Guidance       json.RawMessage `json:"guidance"`
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
// why it was rejected, and the decision shape that message needs.
func rejectionText(answered FlightMessage, err error) string {
	about, id := "schmux's "+strings.ReplaceAll(answered.Kind, "_", " "), "<Notification ID>"
	if answered.Kind == "notification" {
		about, id = "notification "+answered.ID, answered.ID
	}
	return "Schmux rejected your response to " + about + " and delivered nothing: " + err.Error() + ".\n" +
		`Answer a schmux message with exactly one JSON object and nothing else: {"notification_id":"` + id + `","to":"executor","guidance":"..."} sends guidance ("to" may also be "planner"), and {"guidance":null} sends nothing.`
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

func (m *Manager) recordPilotGuidance(ctx context.Context, flightID string, t MonitoredTurn) error {
	if t.Turn.Outcome != "completed" {
		return fmt.Errorf("pilot turn %s: %s", t.Turn.Outcome, t.Turn.Error)
	}
	if strings.TrimSpace(t.Turn.FinalText) == "" {
		return nil
	}
	var decision pilotDecision
	decoder := json.NewDecoder(strings.NewReader(t.Turn.FinalText))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		if !strings.HasPrefix(strings.TrimSpace(t.Turn.FinalText), "{") {
			return errors.New("the response is prose, not a JSON decision")
		}
		return fmt.Errorf("the response is not a valid JSON decision (%v)", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("pilot decision must contain one JSON object")
	}
	if len(decision.Guidance) == 0 {
		return errors.New("pilot decision requires guidance (a string or null)")
	}
	var text *string
	if err := json.Unmarshal(decision.Guidance, &text); err != nil {
		return fmt.Errorf("pilot guidance: %w", err)
	}
	// Null guidance sends nothing, so "to" has nothing to address.
	if text == nil {
		return nil
	}
	to := SessionRole(decision.To)
	if to != RolePlanner && to != RoleExecutor {
		return errors.New(`pilot decision requires "to": "planner" or "executor"`)
	}
	if strings.TrimSpace(*text) == "" {
		return errors.New("pilot guidance must be nonblank or null")
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
	if f.WorkflowState != StatePlanning && f.WorkflowState != StateExecuting {
		return conflict("flight is no longer planning or executing")
	}
	if err := requireWorkspace(&f); err != nil {
		return err
	}
	pilot := f.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID != t.Turn.SessionID || pilot.Outcome != StatusSucceeded {
		return conflict("response is not from the assigned pilot")
	}
	recipient := f.sessionFor(to)
	if recipient == nil || recipient.SessionID == "" || recipient.Outcome != StatusSucceeded {
		return conflict("flight has no assigned %s", to)
	}
	if m.deps.Chats == nil {
		return errors.New("pilot chat dependency is not wired")
	}
	if !m.deps.Chats.IsRunning(ctx, recipient.SessionID) {
		return conflict("%s session is not running", to)
	}
	if m.deps.Chats.SessionWorkspaceID(pilot.SessionID) != f.Workspace.WorkspaceID || m.deps.Chats.SessionWorkspaceID(recipient.SessionID) != f.Workspace.WorkspaceID {
		return conflict("pilot and %s must belong to the flight workspace", to)
	}
	working := map[string]bool{}
	for _, role := range []SessionRole{RolePlanner, RoleExecutor} {
		if s := f.sessionFor(role); s != nil && s.SessionID != "" && s.Outcome == StatusSucceeded {
			working[s.SessionID] = true
		}
	}
	found := false
	if f.Monitor != nil {
		for _, msg := range f.Monitor.Messages {
			if msg.ID != decision.NotificationID || msg.Kind != "notification" || msg.RecipientSessionID != pilot.SessionID || msg.ChatMessageID == "" || (msg.Status != MessageStatusAccepted && msg.Status != MessageStatusQueued) {
				continue
			}
			for _, source := range f.Monitor.Turns {
				if source.ID == msg.SourceTurnID && working[source.Turn.SessionID] {
					found = true
				}
			}
		}
	}
	if !found {
		return conflict("pilot decision must reference a delivered notification for a current planner or executor turn")
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
	// notifications, execution notices, and rejections go to the pilot.
	roles := []SessionRole{RolePilot}
	if msg.Kind == "pilot_message" {
		roles = []SessionRole{RolePlanner, RoleExecutor}
	}
	var recipientRole SessionRole
	for _, role := range roles {
		if s := f.sessionFor(role); s != nil && s.SessionID == msg.RecipientSessionID {
			recipientRole = role
		}
	}
	var sendErr error
	if (f.WorkflowState != StatePlanning && f.WorkflowState != StateExecuting) || recipientRole == "" {
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
