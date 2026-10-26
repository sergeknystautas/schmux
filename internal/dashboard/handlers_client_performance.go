package dashboard

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/attachment"
	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

//go:embed client_performance_prompt.md
var clientPerformancePrompt string

// A unique suffix gives each new investigation its own workspace, like Build Monitor.
const clientPerformanceBranch = "fix/client-performance-"

// ClientPerformanceHandlers launches workspaces and delivers recordings to their agents.
type ClientPerformanceHandlers struct {
	mu        sync.Mutex
	config    *config.Config
	state     state.StateStore
	workspace workspace.WorkspaceManager
	logger    *log.Logger

	// Seams for tests; production wiring points at the managers.
	targetExists func(target string) bool
	hasChatMode  func(target string) bool
	executeSpawn func(SpawnRequest) ([]SessionResult, int, error)
	send         func(string, string) error
	isRunning    func(context.Context, string) bool
	broadcast    func()
}

func newClientPerformanceHandlers(s *Server) *ClientPerformanceHandlers {
	h := &ClientPerformanceHandlers{config: s.config, state: s.state, workspace: s.workspace, logger: s.logger}
	m := s.models
	// Catalog models, detected tools, and configured run targets all count.
	h.targetExists = func(target string) bool {
		_, found := m.IsModel(target)
		return found
	}
	h.hasChatMode = func(target string) bool {
		adapter := detect.GetAdapter(resolveTargetTool(m, target))
		return adapter != nil && adapter.ChatArgs(nil, false, "") != nil
	}
	h.executeSpawn = s.newSpawnHandlers().executeSpawn
	h.isRunning = s.session.IsRunning
	h.broadcast = s.BroadcastSessions
	h.send = func(id, text string) error {
		rt, err := s.session.GetChatRuntime(id)
		if err != nil {
			return err
		}
		_, err = rt.Send(text, nil)
		return err
	}
	return h
}

// handleEnsureSession is POST /api/client-performance/session.
func (h *ClientPerformanceHandlers) handleEnsureSession(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var req contracts.ClientPerformanceSessionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	if !h.config.GetClientPerformanceEnabled() {
		writeJSONError(w, "client performance recording is disabled", http.StatusBadRequest)
		return
	}
	repoName := h.config.GetClientPerformanceRepo()
	if repoName == "" {
		writeJSONError(w, "client_performance.repo is not set", http.StatusBadRequest)
		return
	}
	repoURL := ""
	for _, repo := range h.config.GetRepos() {
		if repo.Name == repoName {
			repoURL = repo.URL
			break
		}
	}
	if repoURL == "" {
		writeJSONError(w, fmt.Sprintf("client_performance.repo %s is not a configured repo", repoName), http.StatusBadRequest)
		return
	}
	target := h.config.GetClientPerformanceTarget()
	if target == "" {
		writeJSONError(w, "client_performance.target is not set", http.StatusBadRequest)
		return
	}
	if !h.targetExists(target) {
		writeJSONError(w, fmt.Sprintf("client_performance.target %s is not a configured target", target), http.StatusBadRequest)
		return
	}
	if !h.config.GetChatSessions() {
		writeJSONError(w, "chat sessions are disabled (chat_sessions)", http.StatusBadRequest)
		return
	}
	if !h.hasChatMode(target) {
		writeJSONError(w, fmt.Sprintf("target %s has no chat mode", target), http.StatusBadRequest)
		return
	}
	tracked, err := h.readState()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.WorkspaceID == "" {
		req.WorkspaceID = tracked.WorkspaceID
	}

	// Like Build Monitor, launch through the ordinary spawn orchestration.
	// A fresh branch creates a dedicated workspace; subsequent agents can use it.
	spawn := SpawnRequest{Repo: repoURL, Branch: clientPerformanceBranch + uuid.NewString()[:8], Targets: map[string]int{target: 1}, Prompt: clientPerformancePrompt, Nickname: "Client performance", Kind: state.SessionKindChat}
	if req.WorkspaceID != "" {
		ws, ok := h.workspace.GetByID(req.WorkspaceID)
		if !ok || ws.Repo != repoURL || !strings.HasPrefix(ws.Branch, clientPerformanceBranch) || ws.Status == state.WorkspaceStatusDisposing {
			writeJSONError(w, "performance workspace is unavailable", http.StatusConflict)
			return
		}
		spawn.WorkspaceID = ws.ID
		for _, sess := range h.state.GetSessions() {
			if sess.WorkspaceID == ws.ID && sess.Kind == state.SessionKindChat && h.isRunning(r.Context(), sess.ID) {
				tracked = contracts.ClientPerformanceSessionResponse{WorkspaceID: ws.ID, SessionID: sess.ID}
				if err := h.writeState(tracked); err != nil {
					writeJSONError(w, err.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, tracked)
				return
			}
		}
	}
	results, status, err := h.executeSpawn(spawn)
	if err != nil {
		writeJSONError(w, err.Error(), status)
		return
	}
	if len(results) != 1 || results[0].Error != "" {
		message := "workspace launch returned no session"
		if len(results) == 1 {
			message = results[0].Error
		}
		writeJSONError(w, message, http.StatusInternalServerError)
		return
	}
	tracked = contracts.ClientPerformanceSessionResponse{WorkspaceID: results[0].WorkspaceID, SessionID: results[0].SessionID}
	if err := h.writeState(tracked); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, tracked)
}

func (h *ClientPerformanceHandlers) readState() (contracts.ClientPerformanceSessionResponse, error) {
	return readClientPerformanceState(h.config, h.state)
}

func readClientPerformanceState(cfg *config.Config, store state.StateStore) (contracts.ClientPerformanceSessionResponse, error) {
	var tracked contracts.ClientPerformanceSessionResponse
	data, err := os.ReadFile(filepath.Join(schmuxdir.Get(), "client-performance.json"))
	if os.IsNotExist(err) {
		return tracked, nil
	}
	if err != nil {
		return tracked, err
	}
	if err := json.Unmarshal(data, &tracked); err != nil {
		return tracked, err
	}
	ws, ok := store.GetWorkspace(tracked.WorkspaceID)
	if !ok || ws.Status == state.WorkspaceStatusDisposing {
		return contracts.ClientPerformanceSessionResponse{}, nil
	}
	var repoURL string
	for _, repo := range cfg.GetRepos() {
		if repo.Name == cfg.GetClientPerformanceRepo() {
			repoURL = repo.URL
		}
	}
	if ws.Repo != repoURL {
		return contracts.ClientPerformanceSessionResponse{}, nil
	}
	if sess, ok := store.GetSession(tracked.SessionID); !ok || sess.WorkspaceID != ws.ID {
		tracked.SessionID = ""
	}
	return tracked, nil
}

func (h *ClientPerformanceHandlers) writeState(tracked contracts.ClientPerformanceSessionResponse) error {
	data, err := json.Marshal(tracked)
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(schmuxdir.Get(), "client-performance.json"), data, 0o600); err != nil {
		return err
	}
	h.broadcast()
	return nil
}

func (h *ClientPerformanceHandlers) handleGetSession(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tracked, err := h.readState()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, tracked)
}

// handleRecording saves the recording in its agent's workspace and sends an ordinary chat message.
func (h *ClientPerformanceHandlers) handleRecording(w http.ResponseWriter, r *http.Request) {
	if !h.config.GetClientPerformanceEnabled() {
		writeJSONError(w, "client performance recording is disabled", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	tracked, err := h.readState()
	h.mu.Unlock()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tracked.SessionID == "" || tracked.SessionID != r.URL.Query().Get("session_id") {
		writeJSONError(w, "performance agent is unavailable", http.StatusConflict)
		return
	}
	sess, ok := h.state.GetSession(r.URL.Query().Get("session_id"))
	if !ok || sess.Kind != state.SessionKindChat {
		writeJSONError(w, "performance agent not found", http.StatusNotFound)
		return
	}
	ws, ok := h.workspace.GetByID(sess.WorkspaceID)
	if !ok || !strings.HasPrefix(ws.Branch, clientPerformanceBranch) || ws.Status == state.WorkspaceStatusDisposing {
		writeJSONError(w, "performance workspace is unavailable", http.StatusConflict)
		return
	}
	name := r.URL.Query().Get("filename")
	if !attachment.ValidName(name) {
		writeJSONError(w, "invalid filename", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxSize)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, "cannot read recording", http.StatusRequestEntityTooLarge)
		return
	}
	if !json.Valid(data) {
		writeJSONError(w, "invalid recording JSON", http.StatusBadRequest)
		return
	}
	path, err := filepath.EvalSymlinks(ws.Path)
	if err != nil {
		writeJSONError(w, "cannot resolve performance workspace", http.StatusInternalServerError)
		return
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		writeJSONError(w, "cannot open performance workspace", http.StatusInternalServerError)
		return
	}
	defer root.Close()
	rel, err := attachment.Save(root, filepath.Join(state.SchmuxDataDirRelative(ws.VCS), "attachments"), name, bytes.NewReader(data))
	if err != nil {
		writeAttachmentSaveError(w, h.logger, ws.ID, err)
		return
	}
	text := "Investigate this client performance recording. Ask me about what felt slow.\n\nFile attachments:\n" + filepath.Join(path, rel)
	if err := h.send(sess.ID, text); err != nil {
		writeJSONError(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "sent"})
}
