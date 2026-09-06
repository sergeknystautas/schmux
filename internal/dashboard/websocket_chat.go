package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// chatWSReadLimit allows a message with up to five inline PNGs.
const chatWSReadLimit = 32 * 1024 * 1024

// chatClientFrame is a client → server frame on /ws/chat/{id}.
type chatClientFrame struct {
	Type         string              `json:"type"` // send | interrupt | permission | answer
	Text         string              `json:"text,omitempty"`
	Images       []chat.Image        `json:"images,omitempty"`
	RequestID    string              `json:"request_id,omitempty"`
	Allow        bool                `json:"allow,omitempty"`
	UpdatedInput json.RawMessage     `json:"updated_input,omitempty"`
	Message      string              `json:"message,omitempty"`
	Answers      map[string][]string `json:"answers,omitempty"`
	Input        json.RawMessage     `json:"input,omitempty"`
}

// handleChatWebSocket streams the conversation record of a chat session:
// history on connect, then every appended record. It never touches tmux.
func (s *Server) handleChatWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if s.requiresAuth() {
		if s.authEnabled() || !s.isTrustedRequest(r) {
			if _, err := s.authenticateRequest(r); err != nil {
				writeJSONError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}
	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}
	if !sess.IsChat() {
		writeJSONError(w, "not a chat session", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermQueryTimeoutMs())*time.Millisecond)
	running := s.session.IsRunning(ctx, sessionID)
	cancel()
	if !running {
		writeJSONError(w, "session not running", http.StatusGone)
		return
	}
	rt, err := s.session.GetChatRuntime(sessionID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("chat runtime: %v", err), http.StatusInternalServerError)
		return
	}

	rawConn, err := s.upgradeWebSocket(w, r, wsReadBufferSize, wsWriteBufferSize)
	if err != nil {
		return
	}
	rawConn.SetReadLimit(chatWSReadLimit)
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	history, live, err := rt.Subscribe()
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	defer rt.Unsubscribe(live)
	if history == nil {
		history = []chat.Record{}
	}
	if err := conn.WriteJSON(map[string]any{"type": "history", "protocol": rt.Protocol(), "records": history}); err != nil {
		return
	}

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		for {
			_, data, err := rawConn.ReadMessage()
			if err != nil {
				return
			}
			var f chatClientFrame
			if err := json.Unmarshal(data, &f); err != nil {
				_ = conn.WriteJSON(map[string]any{"type": "error", "message": "invalid frame"})
				continue
			}
			var actErr error
			switch f.Type {
			case "send":
				_, actErr = rt.Send(f.Text, f.Images)
			case "interrupt":
				actErr = rt.Interrupt()
			case "permission":
				actErr = rt.AnswerPermission(f.RequestID, f.Allow, f.UpdatedInput, f.Message)
			case "answer":
				actErr = rt.AnswerQuestion(f.RequestID, f.Answers, f.Input)
			default:
				actErr = fmt.Errorf("unknown frame type %q", f.Type)
			}
			if actErr != nil {
				_ = conn.WriteJSON(map[string]any{"type": "error", "message": actErr.Error()})
			}
		}
	}()

	for {
		select {
		case rec, ok := <-live:
			if !ok {
				return // runtime stopped or dropped us; client reconnects
			}
			if err := conn.WriteJSON(map[string]any{"type": "record", "record": rec}); err != nil {
				return
			}
		case <-clientDone:
			return
		}
	}
}
