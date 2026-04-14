//go:build !noautolearn

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/autolearn"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

// AutolearnHandlers groups HTTP handlers for autolearn CRUD and curation operations.
type AutolearnHandlers struct {
	config                     *config.Config
	state                      state.WorkspaceStore
	autolearnStore             *autolearn.BatchStore
	autolearnExecutor          func(ctx context.Context, prompt, schemaLabel string, timeout time.Duration) (string, error)
	autolearnPendingMergeStore *autolearn.PendingMergeStore
	curationTracker            *CurationTracker
	logger                     *log.Logger
	shutdownCtx                context.Context
	broadcastCuratorEvent      func(CuratorEvent)
	curationComplete           func(repoName string, err error)
}

// newAutolearnHandlers constructs an AutolearnHandlers from a Server.
func newAutolearnHandlers(s *Server) *AutolearnHandlers {
	return &AutolearnHandlers{
		config:                     s.config,
		state:                      s.state,
		autolearnStore:             s.autolearnStore,
		autolearnExecutor:          s.autolearnExecutor,
		autolearnPendingMergeStore: s.autolearnPendingMergeStore,
		curationTracker:            s.curationTracker,
		logger:                     s.logger,
		shutdownCtx:                s.shutdownCtx,
		broadcastCuratorEvent:      s.BroadcastCuratorEvent,
		curationComplete:           s.curationComplete,
	}
}

// validateAutolearnRepo is a chi middleware that validates the {repo} URL parameter.
// Rejects requests with repo names that contain path separators, dots, or null bytes.
func validateAutolearnRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := chi.URLParam(r, "repo")
		if repo == "" || strings.ContainsAny(repo, "/\\.\x00") || len(repo) > 128 {
			writeJSONError(w, "invalid repo name", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleAutolearnStatus returns the autolearn system configuration status.
func (h *AutolearnHandlers) handleAutolearnStatus(w http.ResponseWriter, r *http.Request) {
	enabled := h.config.GetLoreEnabled()
	curateOnDispose := h.config.GetLoreCurateOnDispose()
	llmTarget := h.config.GetLoreTarget()
	curatorConfigured := h.autolearnExecutor != nil

	var issues []string
	if enabled && !curatorConfigured {
		issues = append(issues, "No LLM target configured — curator cannot run. Set lore.llm_target in config.")
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":            enabled,
		"curator_configured": curatorConfigured,
		"curate_on_dispose":  curateOnDispose,
		"llm_target":         llmTarget,
		"issues":             issues,
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-status", "err", err)
	}
}

// autolearnWorkspace holds workspace path and ID for reading autolearn entries.
type autolearnWorkspace struct {
	Path string
	ID   string
}

// getAutolearnWorkspaces returns workspace info for all workspaces belonging to the given repo.
func (h *AutolearnHandlers) getAutolearnWorkspaces(repoName string) []autolearnWorkspace {
	repo, found := h.config.FindRepo(repoName)
	if !found {
		return nil
	}

	var ws []autolearnWorkspace
	for _, w := range h.state.GetWorkspaces() {
		if w.Repo == repo.URL {
			ws = append(ws, autolearnWorkspace{Path: w.Path, ID: w.ID})
		}
	}
	return ws
}

// readAutolearnEntries reads autolearn entries from per-session event files across all workspaces
// for the given repo, plus the central state file. Applies the optional filter.
func (h *AutolearnHandlers) readAutolearnEntries(repoName string, filter autolearn.EntryFilter) ([]autolearn.Entry, error) {
	var all []autolearn.Entry

	// Read from per-session event files
	for _, ws := range h.getAutolearnWorkspaces(repoName) {
		entries, err := autolearn.ReadEntriesFromEvents(ws.Path, ws.ID, nil)
		if err != nil {
			continue
		}
		all = append(all, entries...)
	}

	// Read from central state file (state-change records)
	statePath, err := autolearn.StatePath(repoName)
	if err == nil {
		stateEntries, err := autolearn.ReadEntries(statePath, nil)
		if err == nil {
			all = append(all, stateEntries...)
		}
	}

	if filter != nil {
		all = filter(all)
	}
	return all, nil
}

// handleAutolearnBatches lists all batches for a repo.
func (h *AutolearnHandlers) handleAutolearnBatches(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		writeJSONError(w, "missing repo name", http.StatusBadRequest)
		return
	}

	if h.autolearnStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}

	batches, err := h.autolearnStore.List(repoName)
	if err != nil {
		h.logger.Error("list batches error", "err", err)
		writeJSONError(w, "failed to list batches", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"batches": batches,
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-batches", "err", err)
	}
}

// handleAutolearnBatchGet returns a single batch by ID.
func (h *AutolearnHandlers) handleAutolearnBatchGet(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	batchID := chi.URLParam(r, "batchID")
	if repoName == "" || batchID == "" {
		writeJSONError(w, "missing repo name or batch id", http.StatusBadRequest)
		return
	}

	if h.autolearnStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}

	batch, err := h.autolearnStore.Get(repoName, batchID)
	if err != nil {
		writeJSONError(w, "batch not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(batch); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-batch-get", "err", err)
	}
}

// handleAutolearnBatchDismiss marks a batch as dismissed.
func (h *AutolearnHandlers) handleAutolearnBatchDismiss(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	batchID := chi.URLParam(r, "batchID")
	if repoName == "" || batchID == "" {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}

	if h.autolearnStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}

	// Load the batch first to get EntriesUsed for marking
	batch, err := h.autolearnStore.Get(repoName, batchID)
	if err != nil {
		writeJSONError(w, "batch not found", http.StatusNotFound)
		return
	}

	if batch.Status == autolearn.BatchApplied {
		writeJSONError(w, "batch is already applied", http.StatusConflict)
		return
	}

	if err := h.autolearnStore.UpdateStatus(repoName, batchID, autolearn.BatchDismissed); err != nil {
		h.logger.Error("update batch status error", "err", err)
		writeJSONError(w, "failed to update batch status", http.StatusInternalServerError)
		return
	}

	// Invalidate any pending merge that includes learnings from this batch
	if h.autolearnPendingMergeStore != nil {
		for _, learning := range batch.Learnings {
			h.autolearnPendingMergeStore.InvalidateIfContainsLearning(repoName, learning.ID)
		}
	}

	// Mark source entries as "dismissed" in the central state JSONL
	statePath, err := autolearn.StatePath(repoName)
	if err == nil {
		entries, _ := h.readAutolearnEntries(repoName, nil)
		if err := autolearn.MarkEntriesByTextFromEntries(entries, statePath, "dismissed", batch.EntriesUsed, batchID); err != nil {
			h.logger.Warn("failed to mark entries as dismissed", "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "dismissed"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-dismiss", "err", err)
	}
}

// handleAutolearnLearningUpdate updates a specific learning within a batch (approve/dismiss/edit/reroute).
func (h *AutolearnHandlers) handleAutolearnLearningUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	repoName := chi.URLParam(r, "repo")
	batchID := chi.URLParam(r, "batchID")
	learningID := chi.URLParam(r, "learningID")
	if repoName == "" || batchID == "" || learningID == "" {
		writeJSONError(w, "missing path parameters", http.StatusBadRequest)
		return
	}

	if h.autolearnStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Status      string  `json:"status"`       // "approved", "dismissed", or "pending"
		Title       *string `json:"title"`        // edited title (optional)
		Description *string `json:"description"`  // edited description (optional)
		ChosenLayer *string `json:"chosen_layer"` // layer override (optional)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Build the LearningUpdate
	update := autolearn.LearningUpdate{}

	// Validate and set status
	if body.Status != "" {
		var status autolearn.LearningStatus
		switch body.Status {
		case "approved":
			status = autolearn.StatusApproved
		case "dismissed":
			status = autolearn.StatusDismissed
		case "pending":
			status = autolearn.StatusPending
		default:
			writeJSONError(w, "status must be 'approved', 'dismissed', or 'pending'", http.StatusBadRequest)
			return
		}
		update.Status = &status
	}

	// Validate chosen_layer if provided
	if body.ChosenLayer != nil {
		l := autolearn.Layer(*body.ChosenLayer)
		switch l {
		case autolearn.LayerRepoPublic, autolearn.LayerRepoPrivate, autolearn.LayerCrossRepoPrivate:
			update.ChosenLayer = &l
		default:
			writeJSONError(w, "chosen_layer must be 'repo_public', 'repo_private', or 'cross_repo_private'", http.StatusBadRequest)
			return
		}
	}

	if body.Title != nil {
		update.Title = body.Title
	}
	if body.Description != nil {
		update.Description = body.Description
	}

	if err := h.autolearnStore.UpdateLearning(repoName, batchID, learningID, update); err != nil {
		h.logger.Error("update learning error", "repo", repoName, "batch", batchID, "learning", learningID, "err", err)
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	// Invalidate any pending merge that includes this learning
	if h.autolearnPendingMergeStore != nil {
		h.autolearnPendingMergeStore.InvalidateIfContainsLearning(repoName, learningID)
	}

	// Return the updated batch
	batch, err := h.autolearnStore.Get(repoName, batchID)
	if err != nil {
		writeJSONError(w, "failed to reload batch", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(batch); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-learning-update", "err", err)
	}
}

// handleAutolearnForget is a stub for the forget endpoint (not yet implemented).
func (h *AutolearnHandlers) handleAutolearnForget(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, "forget is not yet implemented", http.StatusNotImplemented)
}

// handleAutolearnEntries returns the autolearn JSONL entries for a repo, aggregated from
// all workspace directories and the central state file. Supports query parameters: state, agent, type, limit.
func (h *AutolearnHandlers) handleAutolearnEntries(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		writeJSONError(w, "missing repo name", http.StatusBadRequest)
		return
	}

	// Parse query params for filtering
	q := r.URL.Query()
	stateParam := q.Get("state")
	agent := q.Get("agent")
	entryType := q.Get("type")
	var limit int
	if limitStr := q.Get("limit"); limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	var filter autolearn.EntryFilter
	if stateParam != "" || agent != "" || entryType != "" || limit > 0 {
		filter = autolearn.FilterByParams(stateParam, agent, entryType, limit)
	}

	entries, err := h.readAutolearnEntries(repoName, filter)
	if err != nil {
		h.logger.Error("read entries error", "err", err)
		writeJSONError(w, "failed to read autolearn entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-entries", "err", err)
	}
}

// handleAutolearnEntriesClear removes per-session event files for the given repo,
// effectively clearing the raw signal queue.
func (h *AutolearnHandlers) handleAutolearnEntriesClear(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		writeJSONError(w, "missing repo name", http.StatusBadRequest)
		return
	}

	cleared := 0
	for _, ws := range h.getAutolearnWorkspaces(repoName) {
		pattern := filepath.Join(state.SchmuxDataDir(ws.Path), "events", "*.jsonl")
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, f := range files {
			if err := os.Truncate(f, 0); err != nil {
				if !os.IsNotExist(err) {
					h.logger.Warn("failed to truncate event file", "path", f, "err", err)
				}
				continue
			}
			cleared++
		}
	}

	h.logger.Info("cleared raw signals", "repo", repoName, "files_truncated", cleared)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "cleared",
		"cleared": cleared,
	})
}

// handleAutolearnCurate handles manual curation requests.
// Returns immediately with a curation ID; events stream via WebSocket.
func (h *AutolearnHandlers) handleAutolearnCurate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		writeJSONError(w, "missing repo name", http.StatusBadRequest)
		return
	}

	if h.autolearnExecutor == nil {
		writeJSONError(w, "autolearn curator not configured (no LLM target)", http.StatusServiceUnavailable)
		return
	}

	if h.autolearnStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}

	// Guard: only one curation per repo at a time
	if h.curationTracker.IsRunning(repoName) {
		writeJSONError(w, "curation already in progress", http.StatusConflict)
		return
	}

	// Read friction entries
	rawEntries, err := h.readAutolearnEntries(repoName, autolearn.FilterRaw())
	if err != nil {
		h.logger.Error("read entries error", "err", err)
		writeJSONError(w, fmt.Sprintf("failed to read autolearn entries: %v", err), http.StatusInternalServerError)
		return
	}
	if len(rawEntries) == 0 {
		h.logger.Info("curate: no raw entries to process", "repo", repoName)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_raw_entries"})
		return
	}

	// Prepare friction curator prompt
	existingTitles := h.autolearnStore.PendingLearningTitles(repoName)
	dismissedTitles := h.autolearnStore.DismissedLearningTitles(repoName)
	prompt := autolearn.BuildFrictionPrompt(rawEntries, existingTitles, dismissedTitles)

	// Start curation tracking
	curationID := fmt.Sprintf("cur-%s-%s", repoName, time.Now().UTC().Format("20060102-150405"))
	if _, err := h.curationTracker.Start(repoName, curationID); err != nil {
		writeJSONError(w, err.Error(), http.StatusConflict)
		return
	}

	// Return immediately with curation ID
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": curationID, "status": "started"})

	// Run curation in background goroutine
	go h.runAutolearnCuration(repoName, curationID, prompt, rawEntries)
}

// runAutolearnCuration runs the friction curation in the background,
// broadcasting events via WebSocket and writing debug files to a per-run directory.
// TODO: Add intent curator integration — collect intent signals, call intent curator,
// merge both sets of learnings into one batch.
func (h *AutolearnHandlers) runAutolearnCuration(repoName, curationID, prompt string, entries []autolearn.Entry) {
	ctx, cancel := context.WithTimeout(h.shutdownCtx, 10*time.Minute)
	defer cancel()

	h.logger.Info("starting autolearn curation", "repo", repoName, "curation_id", curationID, "entries", len(entries))
	start := time.Now()

	// Create per-run directory
	var runDir string
	var logFile *os.File
	if sd := schmuxdir.Get(); sd != "" {
		runDir = filepath.Join(sd, "autolearn-curator-runs", repoName, curationID)
		os.MkdirAll(runDir, 0755)

		// Write prompt.txt
		os.WriteFile(filepath.Join(runDir, "prompt.txt"), []byte(prompt), 0644)

		// Write run.sh
		target := h.config.GetLoreTarget()
		runScript := curationGenerateRunScript(h.config, target, schema.LabelAutolearnFriction)
		os.WriteFile(filepath.Join(runDir, "run.sh"), []byte(runScript), 0755)

		// Create events.jsonl
		logFile, _ = os.Create(filepath.Join(runDir, "events.jsonl"))
		if logFile != nil {
			defer logFile.Close()
		}
	}

	h.runAutolearnExecutor(ctx, repoName, curationID, prompt, entries, runDir, logFile, start)
}

// runAutolearnExecutor runs curation using the oneshot executor.
func (h *AutolearnHandlers) runAutolearnExecutor(ctx context.Context, repoName, curationID, prompt string, entries []autolearn.Entry, runDir string, logFile *os.File, start time.Time) {
	response, err := h.autolearnExecutor(ctx, prompt, schema.LabelAutolearnFriction, 10*time.Minute)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		curationWriteLogEvent(logFile, errRaw)
		curationWriteDebugFile(runDir, "error.txt", err.Error())
		h.curationComplete(repoName, fmt.Errorf("curator LLM call failed: %w", err))
		return
	}

	curationWriteDebugFile(runDir, "output.txt", response)
	h.finalizeAutolearnCuration(repoName, curationID, response, entries, start, logFile)
}

// finalizeAutolearnCuration parses the friction response, builds a batch with per-learning model, saves it, and marks entries.
func (h *AutolearnHandlers) finalizeAutolearnCuration(repoName, curationID, rawResponse string, entries []autolearn.Entry, start time.Time, logFile *os.File) {
	elapsed := time.Since(start)

	result, err := autolearn.ParseFrictionResponse(rawResponse)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		curationWriteLogEvent(logFile, errRaw)
		h.curationComplete(repoName, fmt.Errorf("failed to parse friction response: %w", err))
		return
	}

	// Build batch from friction result
	now := time.Now().UTC()
	batchID := fmt.Sprintf("batch-%s", now.Format("20060102-150405-")+curationID[len(curationID)-4:])
	batch := &autolearn.Batch{
		ID:        batchID,
		Repo:      repoName,
		CreatedAt: now,
		Status:    autolearn.BatchPending,
		Discarded: result.DiscardedEntries,
	}

	for i, learning := range result.Learnings {
		learning.ID = fmt.Sprintf("l%d", i+1)
		learning.CreatedAt = now
		if learning.Status == "" {
			learning.Status = autolearn.StatusPending
		}
		batch.Learnings = append(batch.Learnings, learning)
	}

	// Deduplicate against existing pending and dismissed batches
	existingTitles := h.autolearnStore.PendingLearningTitles(repoName)
	dismissedTitles := h.autolearnStore.DismissedLearningTitles(repoName)
	allExcluded := append(existingTitles, dismissedTitles...)
	batch.Learnings, _ = autolearn.DeduplicateLearnings(batch.Learnings, allExcluded)

	if len(batch.Learnings) == 0 {
		h.logger.Info("all extracted learnings are duplicates of existing batches", "repo", repoName)
	}

	if err := h.autolearnStore.Save(batch); err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		curationWriteLogEvent(logFile, errRaw)
		h.curationComplete(repoName, fmt.Errorf("failed to save batch: %w", err))
		return
	}

	// Mark all curated entries as proposed — uses direct timestamp marking
	statePath, err := autolearn.StatePath(repoName)
	if err == nil {
		if err := autolearn.MarkEntriesDirect(entries, statePath, "proposed", batch.ID); err != nil {
			h.logger.Warn("failed to mark entries as proposed", "err", err)
		}
	}

	doneRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_done","batch_id":%q,"learning_count":%d}`, batch.ID, len(batch.Learnings)))
	curationWriteLogEvent(logFile, doneRaw)

	h.curationTracker.Complete(repoName, nil)
	h.broadcastCuratorEvent(CuratorEvent{
		Repo:      repoName,
		Timestamp: time.Now().UTC(),
		EventType: "curator_done",
		Raw:       doneRaw,
	})

	h.logger.Info("batch created", "repo", repoName, "batch", batch.ID, "learnings", len(batch.Learnings), "elapsed", elapsed.Round(time.Millisecond))
}

// handleAutolearnCurationsActive returns all active curation runs with their buffered events.
func (h *AutolearnHandlers) handleAutolearnCurationsActive(w http.ResponseWriter, r *http.Request) {
	runs := h.curationTracker.Active()
	if runs == nil {
		runs = []*CurationRun{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// handleAutolearnCurationsList returns past curation run logs for a repo.
func (h *AutolearnHandlers) handleAutolearnCurationsList(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		writeJSONError(w, "missing repo name", http.StatusBadRequest)
		return
	}

	runDir := filepath.Join(schmuxdir.Get(), "autolearn-curator-runs", repoName)

	entries, err := os.ReadDir(runDir)
	if err != nil {
		// Directory doesn't exist — no runs yet
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"runs": []interface{}{}})
		return
	}

	type runInfo struct {
		ID        string `json:"id"`
		SizeBytes int64  `json:"size_bytes"`
		CreatedAt string `json:"created_at"`
	}

	var runs []runInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := entry.Name()

		// Check for events.jsonl to report size; fall back to 0
		var sizeBytes int64
		eventsPath := filepath.Join(runDir, id, "events.jsonl")
		if fi, err := os.Stat(eventsPath); err == nil {
			sizeBytes = fi.Size()
		}

		runs = append(runs, runInfo{
			ID:        id,
			SizeBytes: sizeBytes,
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}

	// Sort newest first (by filename which contains timestamp)
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"runs": runs})
}

// handleAutolearnCurationLog returns the JSONL log content for a specific curation run.
func (h *AutolearnHandlers) handleAutolearnCurationLog(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	curationID := chi.URLParam(r, "curationID")
	if repoName == "" || curationID == "" {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Validate curation ID — no path separators allowed
	if strings.ContainsAny(curationID, "/\\") || curationID == ".." || curationID == "." {
		writeJSONError(w, "invalid curation ID", http.StatusBadRequest)
		return
	}

	logPath := filepath.Join(schmuxdir.Get(), "autolearn-curator-runs", repoName, curationID, "events.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		writeJSONError(w, "log file not found", http.StatusNotFound)
		return
	}

	var events []json.RawMessage
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Validate it's valid JSON
		if json.Valid([]byte(line)) {
			events = append(events, json.RawMessage(line))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"events": events})
}

// handleAutolearnMerge triggers a unified merge of approved public learnings across
// multiple batches into the repo's instruction file. It creates a PendingMerge
// in "merging" status, returns 202 immediately, and runs the LLM merge in the
// background.
func (h *AutolearnHandlers) handleAutolearnMerge(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	repoName := chi.URLParam(r, "repo")

	if h.autolearnStore == nil || h.autolearnPendingMergeStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}
	if h.autolearnExecutor == nil {
		writeJSONError(w, "autolearn curator not configured (no LLM target)", http.StatusServiceUnavailable)
		return
	}

	// Check for existing merging state
	if existing, err := h.autolearnPendingMergeStore.Get(repoName); err == nil && existing.Status == autolearn.PendingMergeStatusMerging {
		writeJSONError(w, "merge already in progress", http.StatusConflict)
		return
	}

	var body struct {
		Batches []struct {
			BatchID     string   `json:"batch_id"`
			LearningIDs []string `json:"learning_ids"`
		} `json:"batches"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Batches) == 0 {
		writeJSONError(w, "no batches provided", http.StatusBadRequest)
		return
	}

	// Gather approved public learnings from all specified batches
	var allLearnings []autolearn.Learning
	var allLearningIDs, allBatchIDs []string
	for _, bg := range body.Batches {
		batch, err := h.autolearnStore.Get(repoName, bg.BatchID)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("batch %s not found", bg.BatchID), http.StatusNotFound)
			return
		}
		allBatchIDs = append(allBatchIDs, bg.BatchID)
		for _, learning := range batch.Learnings {
			if learning.Status == autolearn.StatusApproved && learning.EffectiveLayer() == autolearn.LayerRepoPublic {
				allLearnings = append(allLearnings, learning)
				allLearningIDs = append(allLearningIDs, learning.ID)
			}
		}
	}
	if len(allLearnings) == 0 {
		writeJSONError(w, "no approved public learnings to merge", http.StatusBadRequest)
		return
	}

	// Find bare repo dir
	var bareDir string
	for _, repoConfig := range h.config.Repos {
		if repoConfig.Name == repoName {
			bareDir = h.config.ResolveBareRepoDir(repoConfig.BarePath)
			break
		}
	}
	if bareDir == "" {
		writeJSONError(w, "repo bare directory not found", http.StatusNotFound)
		return
	}

	// Create PendingMerge in "merging" state
	pm := &autolearn.PendingMerge{
		Repo:        repoName,
		Status:      autolearn.PendingMergeStatusMerging,
		LearningIDs: allLearningIDs,
		BatchIDs:    allBatchIDs,
		CreatedAt:   time.Now().UTC(),
	}
	if err := h.autolearnPendingMergeStore.Save(pm); err != nil {
		writeJSONError(w, "failed to create pending merge", http.StatusInternalServerError)
		return
	}

	// Return 202 immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "merging"})

	// Run merge in background
	executor := h.autolearnExecutor
	pendingStore := h.autolearnPendingMergeStore
	instrFiles := h.config.GetLoreInstructionFiles()
	logger := h.logger
	broadcastCuratorEvent := h.broadcastCuratorEvent

	go func() {
		ctx, cancel := context.WithTimeout(h.shutdownCtx, 5*time.Minute)
		defer cancel()

		// Fetch latest from remote
		fetchCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "fetch", "--quiet")
		fetchCmd.Run() // best effort

		// Read current instruction file
		targetFile := "CLAUDE.md"
		if len(instrFiles) > 0 {
			targetFile = instrFiles[0]
		}
		currentContent, err := autolearn.ReadFileFromRepo(ctx, bareDir, targetFile)
		if err != nil {
			logger.Error("failed to read instruction file from repo", "err", err)
			currentContent = "" // empty file is valid — first-time setup
		}

		// Get base SHA
		shaCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "rev-parse", "HEAD")
		shaOut, _ := shaCmd.Output()
		baseSHA := strings.TrimSpace(string(shaOut))

		// Run LLM merge
		prompt := autolearn.BuildMergePrompt(currentContent, allLearnings)
		response, err := executor(ctx, prompt, "", 5*time.Minute)
		if err != nil {
			pm.Status = autolearn.PendingMergeStatusError
			pm.Error = fmt.Sprintf("Merge failed: %v", err)
			pendingStore.Save(pm)
			broadcastCuratorEvent(CuratorEvent{
				Repo: repoName, Timestamp: time.Now().UTC(),
				EventType: "autolearn_merge_complete",
				Raw:       json.RawMessage(fmt.Sprintf(`{"status":"error","error":%q}`, pm.Error)),
			})
			return
		}

		result, err := autolearn.ParseMergeResponse(response)
		if err != nil {
			pm.Status = autolearn.PendingMergeStatusError
			pm.Error = fmt.Sprintf("Failed to parse merge result: %v", err)
			pendingStore.Save(pm)
			broadcastCuratorEvent(CuratorEvent{
				Repo: repoName, Timestamp: time.Now().UTC(),
				EventType: "autolearn_merge_complete",
				Raw:       json.RawMessage(fmt.Sprintf(`{"status":"error","error":%q}`, pm.Error)),
			})
			return
		}

		// Update PendingMerge to ready
		pm.Status = autolearn.PendingMergeStatusReady
		pm.BaseSHA = baseSHA
		pm.CurrentContent = currentContent
		pm.MergedContent = result.MergedContent
		pm.Summary = result.Summary
		pm.Error = ""
		pendingStore.Save(pm)

		broadcastCuratorEvent(CuratorEvent{
			Repo: repoName, Timestamp: time.Now().UTC(),
			EventType: "autolearn_merge_complete",
			Raw:       json.RawMessage(fmt.Sprintf(`{"status":"ready","repo":%q}`, repoName)),
		})
		logger.Info("unified merge complete", "repo", repoName, "learnings", len(allLearnings))
	}()
}

// handleAutolearnPendingMergeGet returns the pending merge for the given repo.
func (h *AutolearnHandlers) handleAutolearnPendingMergeGet(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if h.autolearnPendingMergeStore == nil {
		writeJSONError(w, "pending merge store not configured", http.StatusServiceUnavailable)
		return
	}
	pm, err := h.autolearnPendingMergeStore.Get(repoName)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pm)
}

// handleAutolearnPendingMergeDelete removes a pending merge for the given repo.
func (h *AutolearnHandlers) handleAutolearnPendingMergeDelete(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if h.autolearnPendingMergeStore == nil {
		writeJSONError(w, "pending merge store not configured", http.StatusServiceUnavailable)
		return
	}
	if err := h.autolearnPendingMergeStore.Delete(repoName); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// handleAutolearnPendingMergePatch updates the edited content of a pending merge.
func (h *AutolearnHandlers) handleAutolearnPendingMergePatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	repoName := chi.URLParam(r, "repo")
	if h.autolearnPendingMergeStore == nil {
		writeJSONError(w, "pending merge store not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		EditedContent *string `json:"edited_content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.EditedContent == nil {
		writeJSONError(w, "edited_content is required", http.StatusBadRequest)
		return
	}
	if err := h.autolearnPendingMergeStore.UpdateEditedContent(repoName, *body.EditedContent); err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleAutolearnPush pushes the pending merge content to the repo's instruction file.
// It validates the PendingMerge, checks for staleness, commits and pushes.
// Also handles SkillFiles from the pending merge if present.
func (h *AutolearnHandlers) handleAutolearnPush(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")

	if h.autolearnStore == nil || h.autolearnPendingMergeStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}

	// 1. Get and validate PendingMerge
	pm, err := h.autolearnPendingMergeStore.Get(repoName)
	if err != nil {
		writeJSONError(w, "no pending merge found", http.StatusNotFound)
		return
	}
	if pm.Status != autolearn.PendingMergeStatusReady {
		writeJSONError(w, fmt.Sprintf("pending merge is not ready (status: %s)", pm.Status), http.StatusConflict)
		return
	}
	if pm.IsExpired() {
		writeJSONError(w, "pending merge has expired", http.StatusGone)
		return
	}

	// 2. Server-side learning validation: verify all learnings are still approved
	for _, batchID := range pm.BatchIDs {
		batch, err := h.autolearnStore.Get(repoName, batchID)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("batch %s not found", batchID), http.StatusNotFound)
			return
		}
		for _, learningID := range pm.LearningIDs {
			for _, learning := range batch.Learnings {
				if learning.ID == learningID && learning.Status != autolearn.StatusApproved {
					writeJSONError(w, fmt.Sprintf("learning %s is no longer approved", learningID), http.StatusConflict)
					return
				}
			}
		}
	}

	// 3. Compute instrFiles and targetFile
	instrFiles := h.config.GetLoreInstructionFiles()
	targetFile := "CLAUDE.md"
	if len(instrFiles) > 0 {
		targetFile = instrFiles[0]
	}

	// 4. Find bare repo
	var bareDir string
	for _, repoConfig := range h.config.Repos {
		if repoConfig.Name == repoName {
			bareDir = h.config.ResolveBareRepoDir(repoConfig.BarePath)
			break
		}
	}
	if bareDir == "" {
		writeJSONError(w, "repo bare directory not found", http.StatusNotFound)
		return
	}

	// 5. Freshness check: compare BaseSHA vs current HEAD
	fetchCmd := exec.CommandContext(r.Context(), "git", "-C", bareDir, "fetch", "--quiet")
	fetchCmd.Run() // best effort

	shaCmd := exec.CommandContext(r.Context(), "git", "-C", bareDir, "rev-parse", "HEAD")
	shaOut, err := shaCmd.Output()
	if err != nil {
		writeJSONError(w, "failed to read current HEAD from bare repo", http.StatusInternalServerError)
		return
	}
	currentSHA := strings.TrimSpace(string(shaOut))

	if currentSHA != pm.BaseSHA {
		// SHA changed — check if file content is still the same
		currentContent, err := autolearn.ReadFileFromRepo(r.Context(), bareDir, targetFile)
		if err != nil {
			currentContent = ""
		}
		if currentContent != pm.CurrentContent {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"reason":  "stale",
				"message": "The instruction file has been modified since the merge was created. Please re-merge.",
			})
			return
		}
		// Same content, different SHA — update BaseSHA and proceed
		pm.BaseSHA = currentSHA
	}

	// 6. Determine default branch from bare repo
	refCmd := exec.CommandContext(r.Context(), "git", "-C", bareDir, "symbolic-ref", "HEAD")
	refOut, err := refCmd.Output()
	if err != nil {
		writeJSONError(w, "failed to determine default branch", http.StatusInternalServerError)
		return
	}
	defaultBranch := strings.TrimSpace(string(refOut))
	defaultBranch = strings.TrimPrefix(defaultBranch, "refs/heads/")

	// 7. Clone to a temporary directory
	worktreeDir := filepath.Join(os.TempDir(), fmt.Sprintf("autolearn-push-%s-%d", repoName, time.Now().UnixMilli()))
	defer os.RemoveAll(worktreeDir)

	cloneCmd := exec.CommandContext(r.Context(), "git", "clone", bareDir, worktreeDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to clone: %s: %s", err, out), http.StatusInternalServerError)
		return
	}

	// 8. Write merged content (instruction file)
	fullPath := filepath.Join(worktreeDir, filepath.Clean(targetFile))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to create directory: %v", err), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(fullPath, []byte(pm.EffectiveContent()), 0644); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to write file: %v", err), http.StatusInternalServerError)
		return
	}

	// Stage instruction file
	stageCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "add", targetFile)
	if out, err := stageCmd.CombinedOutput(); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to stage: %s: %s", err, out), http.StatusInternalServerError)
		return
	}

	// 8b. Write skill files if present in the pending merge
	for relPath, content := range pm.SkillFiles {
		skillPath := filepath.Join(worktreeDir, filepath.Clean(relPath))
		if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to create skill dir: %v", err), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(skillPath, []byte(content), 0644); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to write skill file: %v", err), http.StatusInternalServerError)
			return
		}
		skillStageCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "add", relPath)
		if out, err := skillStageCmd.CombinedOutput(); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to stage skill file: %s: %s", err, out), http.StatusInternalServerError)
			return
		}
	}

	// Count approved learnings for commit message
	approvedCount := len(pm.LearningIDs)
	if approvedCount == 0 {
		approvedCount = 1
	}
	commitMsg := fmt.Sprintf("autolearn: add %d learnings from agent sessions", approvedCount)

	// 9. Commit with env vars for author/committer
	commitCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=schmux",
		"GIT_AUTHOR_EMAIL=schmux@localhost",
		"GIT_COMMITTER_NAME=schmux",
		"GIT_COMMITTER_EMAIL=schmux@localhost",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to commit: %s: %s", err, out), http.StatusInternalServerError)
		return
	}

	// Get commit SHA
	commitSHACmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "rev-parse", "HEAD")
	commitSHAOut, err := commitSHACmd.Output()
	if err != nil {
		writeJSONError(w, "failed to read commit SHA", http.StatusInternalServerError)
		return
	}
	commitSHA := strings.TrimSpace(string(commitSHAOut))

	// 10. Push based on config mode
	mode := "direct_push"
	if h.config != nil && h.config.Lore != nil {
		mode = h.config.Lore.GetPublicRuleMode()
	}
	if mode == "create_pr" {
		branch := fmt.Sprintf("autolearn/learnings-%s", time.Now().Format("2006-01-02"))
		exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "checkout", "-b", branch).Run()
		pushCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "push", "-u", "origin", branch)
		if out, err := pushCmd.CombinedOutput(); err != nil {
			writeJSONError(w, fmt.Sprintf("push to branch failed: %s: %s", err, out), http.StatusInternalServerError)
			return
		}
	} else {
		pushCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "push", "origin", "HEAD:"+defaultBranch)
		if out, err := pushCmd.CombinedOutput(); err != nil {
			writeJSONError(w, fmt.Sprintf("push failed: %s: %s", err, out), http.StatusInternalServerError)
			return
		}
	}

	// 11. Delete PendingMerge
	h.autolearnPendingMergeStore.Delete(repoName)

	// 12. Mark learnings as applied in batches
	now := time.Now().UTC()
	for _, batchID := range pm.BatchIDs {
		batch, err := h.autolearnStore.Get(repoName, batchID)
		if err != nil {
			continue
		}
		for i := range batch.Learnings {
			for _, learningID := range pm.LearningIDs {
				if batch.Learnings[i].ID == learningID && batch.Learnings[i].Status == autolearn.StatusApproved {
					if batch.Learnings[i].Rule != nil {
						batch.Learnings[i].Rule.MergedAt = &now
					} else {
						batch.Learnings[i].Rule = &autolearn.RuleDetails{MergedAt: &now}
					}
				}
			}
		}
		if batch.AllResolved() {
			batch.Status = autolearn.BatchApplied
		}
		h.autolearnStore.Save(batch)
	}

	// 13. Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":     "pushed",
		"commit_sha": commitSHA,
	})
}

// handleAutolearnHistory returns filtered learnings from all batches for a repo.
// Supports query parameters: kind, status, layer.
func (h *AutolearnHandlers) handleAutolearnHistory(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		writeJSONError(w, "missing repo name", http.StatusBadRequest)
		return
	}

	if h.autolearnStore == nil {
		writeJSONError(w, "autolearn system not enabled", http.StatusServiceUnavailable)
		return
	}

	batches, err := h.autolearnStore.List(repoName)
	if err != nil {
		h.logger.Error("list batches error", "err", err)
		writeJSONError(w, "failed to list batches", http.StatusInternalServerError)
		return
	}

	// Parse optional filter query params
	q := r.URL.Query()
	var kindFilter *autolearn.LearningKind
	if k := q.Get("kind"); k != "" {
		kind := autolearn.LearningKind(k)
		kindFilter = &kind
	}
	var statusFilter *autolearn.LearningStatus
	if st := q.Get("status"); st != "" {
		status := autolearn.LearningStatus(st)
		statusFilter = &status
	}
	var layerFilter *autolearn.Layer
	if l := q.Get("layer"); l != "" {
		layer := autolearn.Layer(l)
		layerFilter = &layer
	}

	learnings := autolearn.FilterLearnings(batches, kindFilter, statusFilter, layerFilter)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"learnings": learnings,
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-history", "err", err)
	}
}

// handleAutolearnPromptHistory returns intent signal history for a repo.
func (h *AutolearnHandlers) handleAutolearnPromptHistory(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		writeJSONError(w, "missing repo name", http.StatusBadRequest)
		return
	}

	// Collect workspace paths for this repo
	var wsPaths []string
	for _, ws := range h.getAutolearnWorkspaces(repoName) {
		wsPaths = append(wsPaths, ws.Path)
	}

	signals, err := autolearn.CollectIntentSignals(wsPaths)
	if err != nil {
		h.logger.Error("collect intent signals error", "err", err)
		writeJSONError(w, "failed to collect intent signals", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"signals": signals,
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "autolearn-prompt-history", "err", err)
	}
}

// refreshAutolearnExecutor updates the autolearn LLM executor based on the current
// config. Called after config save so the runtime executor stays in sync with
// the persisted lore.llm_target value.
func (s *Server) refreshAutolearnExecutor(cfg *config.Config) {
	target := cfg.GetLoreTargetRaw()

	if target != "" {
		executor := func(ctx context.Context, prompt, schemaLabel string, timeout time.Duration) (string, error) {
			return oneshot.ExecuteTarget(ctx, cfg, target, prompt, schemaLabel, timeout, "")
		}
		s.autolearnExecutor = executor
		if s.autolearnHandlers != nil {
			s.autolearnHandlers.autolearnExecutor = executor
		}
	} else {
		s.autolearnExecutor = nil
		if s.autolearnHandlers != nil {
			s.autolearnHandlers.autolearnExecutor = nil
		}
	}
}
