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
	"sync"

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

const (
	// clientPerformanceWorkspace is the name of the dedicated workspace the
	// client performance agent works in.
	clientPerformanceWorkspace = "client-performance"
	// clientPerformanceBranch is the branch the dedicated workspace is created on.
	clientPerformanceBranch = "fix/client-performance"
)

// clientPerformanceFile is the on-disk record of the tracked agent. It holds
// the session ID only: a session ID can go missing but never matches a
// different session, while a workspace ID could match a reused workspace.
type clientPerformanceFile struct {
	SessionID string `json:"session_id"`
}

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
	ensure       func(context.Context) (*state.Workspace, error)
	register     func(string, workspace.DedicatedRegistration)
	unregister   func(string) error
}

// newClientPerformanceHandlers runs in NewServer, before the server's other
// managers are set, so the seams read them from the server when called.
func newClientPerformanceHandlers(s *Server) *ClientPerformanceHandlers {
	h := &ClientPerformanceHandlers{config: s.config, state: s.state, workspace: s.workspace, logger: s.logger}
	// Catalog models, detected tools, and configured run targets all count.
	h.targetExists = func(target string) bool {
		_, found := s.models.IsModel(target)
		return found
	}
	h.hasChatMode = func(target string) bool {
		adapter := detect.GetAdapter(resolveTargetTool(s.models, target))
		return adapter != nil && adapter.ChatArgs(nil, false, "") != nil
	}
	h.executeSpawn = func(req SpawnRequest) ([]SessionResult, int, error) {
		return s.newSpawnHandlers().executeSpawn(req)
	}
	h.isRunning = func(ctx context.Context, id string) bool { return s.session.IsRunning(ctx, id) }
	h.broadcast = s.BroadcastSessions
	h.send = func(id, text string) error {
		rt, err := s.session.GetChatRuntime(id)
		if err != nil {
			return err
		}
		_, err = rt.Send(text, nil)
		return err
	}
	h.ensure = func(ctx context.Context) (*state.Workspace, error) {
		return s.workspace.EnsureDedicated(ctx, clientPerformanceWorkspace)
	}
	h.register = func(name string, reg workspace.DedicatedRegistration) {
		s.workspace.RegisterDedicated(name, reg)
	}
	h.unregister = func(name string) error { return s.workspace.UnregisterDedicated(name) }
	return h
}

// syncRegistration reconciles the dedicated workspace registration with the
// current config: it registers the client-performance workspace when the
// feature is enabled with a repo and a target, and unregisters it otherwise.
// Disabling the feature removes the name without disposing the workspace.
// Either outcome can end the tracked agent's association, which dashboards
// read from the sessions snapshot.
func (h *ClientPerformanceHandlers) syncRegistration() {
	defer h.broadcast()
	if !h.config.GetClientPerformanceEnabled() {
		h.unregisterDedicated()
		return
	}
	repoName := h.config.GetClientPerformanceRepo()
	if repoName == "" || h.config.GetClientPerformanceTarget() == "" {
		h.unregisterDedicated()
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
		h.unregisterDedicated()
		return
	}
	h.register(clientPerformanceWorkspace, workspace.DedicatedRegistration{
		RepoURL: repoURL,
		Branch:  clientPerformanceBranch,
	})
}

// unregisterDedicated drops the registration, logging the failure so a name
// left behind in state is visible.
func (h *ClientPerformanceHandlers) unregisterDedicated() {
	if err := h.unregister(clientPerformanceWorkspace); err != nil {
		h.logger.Warn("failed to unregister the client performance workspace", "err", err)
	}
}

// handleEnsureSession is POST /api/client-performance/session.
func (h *ClientPerformanceHandlers) handleEnsureSession(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

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

	// The dedicated workspace is schmux's to keep: it is created when there is
	// none and replaced when it no longer matches its registration. It runs on
	// its own context — a browser tab closing mid-launch must not cancel the
	// clone and leave a half-made workspace behind.
	ctx, cancel := context.WithTimeout(context.Background(), h.config.GitCloneTimeout())
	defer cancel()
	ws, err := h.ensure(ctx)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tracked, err := h.readState()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tracked.SessionID != "" && tracked.WorkspaceID == ws.ID {
		if sess, ok := h.state.GetSession(tracked.SessionID); ok && h.isRunning(r.Context(), sess.ID) {
			response := contracts.ClientPerformanceSessionResponse{WorkspaceID: ws.ID, SessionID: sess.ID}
			if err := h.writeState(response); err != nil {
				writeJSONError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, response)
			return
		}
	}

	// Like Build Monitor, launch through the ordinary spawn orchestration,
	// into the dedicated workspace.
	spawn := SpawnRequest{WorkspaceID: ws.ID, Targets: map[string]int{target: 1}, Prompt: clientPerformancePrompt, Nickname: "Client performance", Kind: state.SessionKindChat}
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
	response := contracts.ClientPerformanceSessionResponse{WorkspaceID: ws.ID, SessionID: results[0].SessionID}
	if err := h.writeState(response); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (h *ClientPerformanceHandlers) readState() (contracts.ClientPerformanceSessionResponse, error) {
	return readClientPerformanceState(h.state)
}

// readClientPerformanceState reads the tracked session ID. The workspace ID
// comes from the session's own record, never from the file. The session counts
// only while it lives in the dedicated workspace: a workspace that lost the
// name (a repo change, an unregister) is not the caller's anymore.
func readClientPerformanceState(store state.StateStore) (contracts.ClientPerformanceSessionResponse, error) {
	var file clientPerformanceFile
	data, err := os.ReadFile(filepath.Join(schmuxdir.Get(), "client-performance.json"))
	if os.IsNotExist(err) {
		return contracts.ClientPerformanceSessionResponse{}, nil
	}
	if err != nil {
		return contracts.ClientPerformanceSessionResponse{}, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return contracts.ClientPerformanceSessionResponse{}, err
	}
	if file.SessionID == "" {
		return contracts.ClientPerformanceSessionResponse{}, nil
	}
	sess, ok := store.GetSession(file.SessionID)
	if !ok {
		return contracts.ClientPerformanceSessionResponse{}, nil
	}
	ws, ok := store.GetWorkspace(sess.WorkspaceID)
	if !ok || ws.Dedicated != clientPerformanceWorkspace {
		return contracts.ClientPerformanceSessionResponse{}, nil
	}
	return contracts.ClientPerformanceSessionResponse{WorkspaceID: sess.WorkspaceID, SessionID: sess.ID}, nil
}

func (h *ClientPerformanceHandlers) writeState(tracked contracts.ClientPerformanceSessionResponse) error {
	data, err := json.Marshal(clientPerformanceFile{SessionID: tracked.SessionID})
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
	if !ok {
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
