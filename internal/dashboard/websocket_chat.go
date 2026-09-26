package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// chatWSReadLimit allows a message with up to five inline PNGs.
const chatWSReadLimit = 32 * 1024 * 1024

// chatClientFrame is a client → server frame on /ws/chat/{id}.
type chatClientFrame struct {
	Type         string              `json:"type"` // send | interrupt | permission | answer | abort
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
// history on connect (only records after ?since=N when the client resumes),
// then every appended record. It never touches tmux.
func (s *Server) handleChatWebSocket(w http.ResponseWriter, r *http.Request) {
	requestStarted := time.Now()
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
	var rt *chat.Runtime
	if running {
		rt, err = s.session.GetChatRuntime(sessionID)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("chat runtime: %v", err), http.StatusInternalServerError)
			return
		}
	}

	rawConn, err := s.upgradeWebSocket(w, r, wsReadBufferSize, wsWriteBufferSize)
	if err != nil {
		return
	}
	rawConn.SetReadLimit(chatWSReadLimit)
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	protocol := sess.EffectiveChatProtocol()
	chatLoadProfiling := s.config.GetChatLoadProfilingEnabled()
	loadID := uuid.NewString()
	// since resumes after the client's last durable sequence. A value that
	// does not parse cannot be resumed from, so it resets like a future one.
	var after uint64
	var since *uint64
	invalidSince := false
	if rawSince := r.URL.Query().Get("since"); rawSince != "" {
		if value, parseErr := strconv.ParseUint(rawSince, 10, 64); parseErr != nil {
			invalidSince = true
		} else {
			after = value
			since = &value
		}
	}
	var history []chat.Record
	var live <-chan chat.Record
	var lastSeq uint64
	var reset bool
	readStarted := time.Now()
	if running {
		var sub chat.Subscription
		sub, err = rt.Subscribe(after)
		history, live, lastSeq, reset = sub.History, sub.Live, sub.LastSeq, sub.Reset
		if err != nil {
			_ = conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
		defer rt.Unsubscribe(live)
		protocol = rt.Protocol()
	} else {
		conversationPath := chat.PathsFor(schmuxdir.ChatSessionDir(sess.WorkspaceID, sess.ID)).Conversation
		log, openErr := chat.OpenLog(conversationPath)
		if openErr != nil {
			_ = conn.WriteJSON(map[string]any{"type": "error", "message": openErr.Error()})
			return
		}
		history, err = log.ReadAll()
		if err != nil {
			_ = conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
		history, lastSeq, reset = chat.RecordsAfter(history, after)
	}
	reset = reset || invalidSince
	if history == nil {
		history = []chat.Record{}
	}
	readFinished := time.Now()
	// Compaction rewrites the slice in place. Detailed diagnostics retain only
	// record headers so the source payload can be profiled after the frame write.
	var sourceHistory []chat.Record
	if chatLoadProfiling {
		sourceHistory = append([]chat.Record(nil), history...)
	}
	if protocol == chat.ProtocolCodex {
		history = compactCodexHistory(history)
	}
	filterFinished := time.Now()
	if workspace, ok := s.state.GetWorkspace(sess.WorkspaceID); ok && workspace.Path != "" && !workspace.IsRemoteWorkspace() {
		for i := range history {
			history[i] = chatRecordForBrowser(history[i], sessionID)
		}
	}
	previewFinished := time.Now()
	durableRecords := 0
	for _, rec := range history {
		if rec.Seq > 0 {
			durableRecords++
		}
	}
	historyFrame := map[string]any{"type": "history", "protocol": protocol, "records": history, "load_id": loadID, "last_seq": lastSeq, "reset": reset}
	if since != nil {
		historyFrame["since"] = *since
	}
	frame, err := json.Marshal(historyFrame)
	if err != nil {
		return
	}
	marshalFinished := time.Now()
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		return
	}
	writeFinished := time.Now()
	fields := []any{
		"session", sessionID,
		"load_id", loadID,
		"records", len(history),
		"since", since,
		"last_seq", lastSeq,
		"reset", reset,
		"durable_records", durableRecords,
		"bytes", len(frame),
		"setup_ms", readStarted.Sub(requestStarted).Milliseconds(),
		"read_ms", readFinished.Sub(readStarted).Milliseconds(),
		"filter_ms", filterFinished.Sub(readFinished).Milliseconds(),
		"preview_ms", previewFinished.Sub(filterFinished).Milliseconds(),
		"marshal_ms", marshalFinished.Sub(previewFinished).Milliseconds(),
		"write_ms", writeFinished.Sub(marshalFinished).Milliseconds(),
		"total_ms", writeFinished.Sub(requestStarted).Milliseconds(),
	}
	if chatLoadProfiling {
		breakdownStarted := time.Now()
		sourceBreakdown := chatHistoryBreakdown(sourceHistory)
		sentBreakdown := chatHistoryBreakdown(history)
		fields = append(fields,
			"source_records", len(sourceHistory),
			"source_breakdown", sourceBreakdown,
			"sent_breakdown", sentBreakdown,
			"breakdown_ms", time.Since(breakdownStarted).Milliseconds())
	}
	s.recordChatPerformance("history", fields)
	if !running {
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
			case "abort":
				actErr = rt.Abort(f.RequestID)
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
			if protocol == chat.ProtocolCodex && (isCodexUserMessageEcho(rec) || isCodexUnusedDiffUpdate(rec)) {
				continue
			}
			if workspace, ok := s.state.GetWorkspace(sess.WorkspaceID); ok && workspace.Path != "" && !workspace.IsRemoteWorkspace() {
				rec = chatRecordForBrowser(rec, sessionID)
			}
			if err := conn.WriteJSON(map[string]any{"type": "record", "record": rec}); err != nil {
				return
			}
		case <-clientDone:
			return
		}
	}
}

func chatRecordForBrowser(rec chat.Record, sessionID string) chat.Record {
	if rec.Type != chat.RecordUserMessage || len(rec.Images) == 0 {
		return rec
	}
	rec.Images = append([]chat.Image(nil), rec.Images...)
	for i := range rec.Images {
		width, height, err := chat.PreviewDimensions(rec.Images[i])
		if err != nil {
			continue // preserve inline data when an image cannot be decoded
		}
		rec.Images[i].Data = ""
		rec.Images[i].Path = ""
		rec.Images[i].PreviewURL = fmt.Sprintf("/api/chat/%s/images/%s/%d", url.PathEscape(sessionID), url.PathEscape(rec.ID), i)
		rec.Images[i].PreviewWidth = width
		rec.Images[i].PreviewHeight = height
	}
	return rec
}

func isCodexUserMessageEcho(rec chat.Record) bool {
	if rec.Type != chat.RecordHarness || !bytes.Contains(rec.Line, []byte(`"userMessage"`)) {
		return false
	}
	var line struct {
		Method string `json:"method"`
		Params struct {
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
		} `json:"params"`
	}
	if err := json.Unmarshal(rec.Line, &line); err != nil {
		return false
	}
	return (line.Method == "item/started" || line.Method == "item/completed") &&
		line.Params.Item.Type == "userMessage"
}

// compactCodexHistory removes Codex events that the dashboard does not use.
// A completed fileChange contains the same patch as its start; keep the full
// completed item and the start's identity/path fields for the reducer.
func compactCodexHistory(history []chat.Record) []chat.Record {
	history = slices.DeleteFunc(history, func(rec chat.Record) bool {
		return isCodexUserMessageEcho(rec) || isCodexUnusedDiffUpdate(rec)
	})
	completed := make(map[string]bool)
	for _, rec := range history {
		if id := codexFileChangeID(rec, "item/completed"); id != "" {
			completed[id] = true
		}
	}
	for i, rec := range history {
		if id := codexFileChangeID(rec, "item/started"); id != "" && completed[id] {
			history[i] = stripCodexStartedFileChangeDiff(rec)
		}
	}
	return history
}

type chatHistoryPart struct {
	Category        string `json:"category"`
	Records         int    `json:"records"`
	PayloadBytes    int    `json:"payload_bytes"`
	MaxPayloadBytes int    `json:"max_payload_bytes"`
}

type chatHistoryOutlier struct {
	Index        int    `json:"index"`
	Category     string `json:"category"`
	PayloadBytes int    `json:"payload_bytes"`
}

type chatHistoryProfile struct {
	Categories       []chatHistoryPart    `json:"categories"`
	Largest          []chatHistoryOutlier `json:"largest"`
	Images           int                  `json:"images"`
	InlineImageBytes int                  `json:"inline_image_bytes"`
}

// chatHistoryBreakdown accounts for the large fields without serializing every
// record again. The exact serialized frame size is recorded separately.
func chatHistoryBreakdown(history []chat.Record) chatHistoryProfile {
	parts := make(map[string]*chatHistoryPart)
	profile := chatHistoryProfile{}
	for index, rec := range history {
		category := string(rec.Type)
		if rec.Type == chat.RecordHarness || rec.Type == chat.RecordControl {
			method := chatLineStringField(rec.Line, "method")
			if method == "" {
				method = chatLineStringField(rec.Line, "type")
			}
			if method != "" {
				category += "/" + method
			}
			if itemType := chatLineStringField(rec.Line, "type"); itemType != "" && itemType != method {
				category += "/" + itemType
			}
		}
		part := parts[category]
		if part == nil {
			part = &chatHistoryPart{Category: category}
			parts[category] = part
		}
		part.Records++
		payloadBytes := len(rec.Line) + len(rec.Text)
		for _, img := range rec.Images {
			profile.Images++
			profile.InlineImageBytes += len(img.Data)
			payloadBytes += len(img.Data)
		}
		part.PayloadBytes += payloadBytes
		part.MaxPayloadBytes = max(part.MaxPayloadBytes, payloadBytes)
		profile.Largest = append(profile.Largest, chatHistoryOutlier{Index: index, Category: category, PayloadBytes: payloadBytes})
	}
	profile.Categories = make([]chatHistoryPart, 0, len(parts))
	for _, part := range parts {
		profile.Categories = append(profile.Categories, *part)
	}
	sort.Slice(profile.Categories, func(i, j int) bool {
		if profile.Categories[i].PayloadBytes == profile.Categories[j].PayloadBytes {
			return profile.Categories[i].Category < profile.Categories[j].Category
		}
		return profile.Categories[i].PayloadBytes > profile.Categories[j].PayloadBytes
	})
	sort.Slice(profile.Largest, func(i, j int) bool {
		if profile.Largest[i].PayloadBytes == profile.Largest[j].PayloadBytes {
			return profile.Largest[i].Index < profile.Largest[j].Index
		}
		return profile.Largest[i].PayloadBytes > profile.Largest[j].PayloadBytes
	})
	if len(profile.Largest) > 8 {
		profile.Largest = profile.Largest[:8]
	}
	return profile
}

// The method and item type are short JSON strings. Read only those labels;
// decoding a multi-megabyte patch just to label its telemetry would add load
// time. Escaped keys inside a JSON string do not match the unescaped marker.
func chatLineStringField(line []byte, name string) string {
	marker := []byte(`"` + name + `"`)
	pos := bytes.Index(line, marker)
	if pos < 0 {
		return ""
	}
	rest := bytes.TrimLeft(line[pos+len(marker):], " \t\r\n")
	if len(rest) == 0 || rest[0] != ':' {
		return ""
	}
	rest = bytes.TrimLeft(rest[1:], " \t\r\n")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := bytes.IndexByte(rest, '"')
	if end < 0 || end > 80 {
		return ""
	}
	return string(rest[:end])
}

func isCodexUnusedDiffUpdate(rec chat.Record) bool {
	if rec.Type != chat.RecordHarness || !bytes.Contains(rec.Line, []byte(`"turn/diff/updated"`)) {
		return false
	}
	var line struct {
		Method string `json:"method"`
	}
	return json.Unmarshal(rec.Line, &line) == nil && line.Method == "turn/diff/updated"
}

func codexFileChangeID(rec chat.Record, method string) string {
	if rec.Type != chat.RecordHarness || !bytes.Contains(rec.Line, []byte(`"fileChange"`)) ||
		!bytes.Contains(rec.Line, []byte(`"`+method+`"`)) {
		return ""
	}
	var line struct {
		Method string `json:"method"`
		Params struct {
			Item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
		} `json:"params"`
	}
	if json.Unmarshal(rec.Line, &line) != nil || line.Method != method || line.Params.Item.Type != "fileChange" {
		return ""
	}
	return line.Params.Item.ID
}

func stripCodexStartedFileChangeDiff(rec chat.Record) chat.Record {
	var line map[string]json.RawMessage
	if json.Unmarshal(rec.Line, &line) != nil {
		return rec
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(line["params"], &params) != nil {
		return rec
	}
	var item map[string]json.RawMessage
	if json.Unmarshal(params["item"], &item) != nil {
		return rec
	}
	var changes []map[string]json.RawMessage
	if json.Unmarshal(item["changes"], &changes) != nil {
		return rec
	}
	for _, change := range changes {
		delete(change, "diff")
	}
	var err error
	item["changes"], err = json.Marshal(changes)
	if err != nil {
		return rec
	}
	params["item"], err = json.Marshal(item)
	if err != nil {
		return rec
	}
	line["params"], err = json.Marshal(params)
	if err != nil {
		return rec
	}
	encoded, err := json.Marshal(line)
	if err != nil {
		return rec
	}
	rec.Line = encoded
	return rec
}
