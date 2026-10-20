package flights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// pilotPrefix is added to every pilot-to-planner message by the daemon for
// visible chat attribution. The flight record retains the exact unprefixed
// text; the prefix is only ever seen in the planner's conversation log.
const pilotPrefix = "[Pilot] "

// A pilot decision comes only from a completed response on its assigned chat
// stream. The daemon resolves both sessions; the response cannot name a recipient.
type pilotDecision struct {
	NotificationID string          `json:"notification_id"`
	Guidance       json.RawMessage `json:"guidance"`
}

func (m *Manager) processPilotTurn(ctx context.Context, flightID string, t MonitoredTurn) {
	err := m.recordPilotGuidance(ctx, flightID, t)
	attempt := MonitorAttempt{}
	if err != nil {
		attempt.Error = err.Error()
	}
	if ctx.Err() == nil {
		if err := m.recordMonitorAttempt(flightID, t.ID, attempt); err != nil {
			m.logger.Warn("pilot: retain decision failed", "err", err)
		}
	}
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
		return fmt.Errorf("pilot decision: %w", err)
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
	if text == nil {
		return nil
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
	if f.WorkflowState != StatePlanning {
		return conflict("flight is no longer planning")
	}
	if err := requireWorkspace(&f); err != nil {
		return err
	}
	pilot := f.sessionFor(RolePilot)
	if pilot == nil || pilot.SessionID != t.Turn.SessionID || pilot.Outcome != StatusSucceeded {
		return conflict("response is not from the assigned pilot")
	}
	planner := f.sessionFor(RolePlanner)
	if planner == nil || planner.SessionID == "" || planner.Outcome != StatusSucceeded {
		return conflict("flight has no assigned planner")
	}
	if m.deps.Chats == nil {
		return errors.New("pilot chat dependency is not wired")
	}
	if !m.deps.Chats.IsRunning(ctx, planner.SessionID) {
		return conflict("planner session is not running")
	}
	if m.deps.Chats.SessionWorkspaceID(pilot.SessionID) != f.Workspace.WorkspaceID || m.deps.Chats.SessionWorkspaceID(planner.SessionID) != f.Workspace.WorkspaceID {
		return conflict("pilot and planner must belong to the flight workspace")
	}
	found := false
	if f.Monitor != nil {
		for _, msg := range f.Monitor.Messages {
			if msg.ID != decision.NotificationID || msg.Kind != "notification" || msg.RecipientSessionID != pilot.SessionID || msg.ChatMessageID == "" || (msg.Status != MessageStatusAccepted && msg.Status != MessageStatusQueued) {
				continue
			}
			for _, source := range f.Monitor.Turns {
				if source.ID == msg.SourceTurnID && source.Turn.SessionID == planner.SessionID {
					found = true
				}
			}
		}
	}
	if !found {
		return conflict("pilot decision must reference a delivered notification for the current planner")
	}
	_, err := m.recordFlightMessage(flightID, FlightMessage{
		Kind:                 "pilot_message",
		SourceTurnID:         t.ID,
		SourceNotificationID: decision.NotificationID,
		SenderSessionID:      pilot.SessionID,
		RecipientSessionID:   planner.SessionID,
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
	role := RolePilot
	if msg.Kind == "pilot_message" {
		role = RolePlanner
	}
	recipient := f.sessionFor(role)
	var sendErr error
	if f.WorkflowState != StatePlanning || recipient == nil || recipient.SessionID != msg.RecipientSessionID {
		sendErr = conflict("message recipient is no longer assigned to this planning flight")
	}
	if msg.Kind == "pilot_message" {
		if pilot := f.sessionFor(RolePilot); pilot == nil || pilot.SessionID != msg.SenderSessionID {
			sendErr = conflict("message sender is no longer the assigned pilot")
		}
		if sendErr == nil && !m.deps.Chats.IsRunning(ctx, msg.RecipientSessionID) {
			sendErr = conflict("planner session is not running")
		}
		if sendErr == nil && (f.Workspace == nil ||
			m.deps.Chats.SessionWorkspaceID(msg.SenderSessionID) != f.Workspace.WorkspaceID ||
			m.deps.Chats.SessionWorkspaceID(msg.RecipientSessionID) != f.Workspace.WorkspaceID) {
			sendErr = conflict("pilot and planner must belong to the flight workspace")
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
	} else {
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
