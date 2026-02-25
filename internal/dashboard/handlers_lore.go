package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
)

// validateLoreRepo is a chi middleware that validates the {repo} URL parameter.
// Rejects requests with repo names that contain path separators, dots, or null bytes.
func validateLoreRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := chi.URLParam(r, "repo")
		if repo == "" || strings.ContainsAny(repo, "/\\.\x00") || len(repo) > 128 {
			http.Error(w, "invalid repo name", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleLoreStatus returns the lore system configuration status.
func (s *Server) handleLoreStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.config.GetLoreEnabled()
	curateOnDispose := s.config.GetLoreCurateOnDispose()
	llmTarget := s.config.GetLoreTarget()
	curatorConfigured := s.loreCurator != nil && s.loreCurator.Executor != nil

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
		s.logger.Error("failed to encode response", "handler", "lore-status", "err", err)
	}
}

// loreWorkspace holds workspace path and ID for reading lore entries.
type loreWorkspace struct {
	Path string
	ID   string
}

// getLoreWorkspaces returns workspace info for all workspaces belonging to the given repo.
func (s *Server) getLoreWorkspaces(repoName string) []loreWorkspace {
	repo, found := s.config.FindRepo(repoName)
	if !found {
		return nil
	}

	var ws []loreWorkspace
	for _, w := range s.state.GetWorkspaces() {
		if w.Repo == repo.URL {
			ws = append(ws, loreWorkspace{Path: w.Path, ID: w.ID})
		}
	}
	return ws
}

// readLoreEntries reads lore entries from per-session event files across all workspaces
// for the given repo, plus the central state file. Applies the optional filter.
func (s *Server) readLoreEntries(repoName string, filter lore.EntryFilter) ([]lore.Entry, error) {
	var all []lore.Entry

	// Read from per-session event files
	for _, ws := range s.getLoreWorkspaces(repoName) {
		entries, err := lore.ReadEntriesFromEvents(ws.Path, ws.ID, nil)
		if err != nil {
			continue
		}
		all = append(all, entries...)
	}

	// Read from central state file (state-change records)
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		stateEntries, err := lore.ReadEntries(statePath, nil)
		if err == nil {
			all = append(all, stateEntries...)
		}
	}

	if filter != nil {
		all = filter(all)
	}
	return all, nil
}

// handleLoreProposals lists all proposals for a repo.
func (s *Server) handleLoreProposals(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposals, err := s.loreStore.List(repoName)
	if err != nil {
		s.logger.Error("list proposals error", "err", err)
		http.Error(w, "failed to list proposals", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"proposals": proposals,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "lore-proposals", "err", err)
	}
}

// handleLoreProposalGet returns a single proposal by ID.
func (s *Server) handleLoreProposalGet(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	if repoName == "" || proposalID == "" {
		http.Error(w, "missing repo name or proposal id", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposal); err != nil {
		s.logger.Error("failed to encode response", "handler", "lore-proposal-get", "err", err)
	}
}

// handleLoreApply applies a proposal: creates a worktree, commits changes, pushes the branch,
// and optionally creates a PR when auto_pr is enabled.
func (s *Server) handleLoreApply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	if repoName == "" || proposalID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Check that proposal is still pending
	if proposal.Status != "" && proposal.Status != "pending" {
		http.Error(w, fmt.Sprintf("proposal is %s, not pending", proposal.Status), http.StatusConflict)
		return
	}

	// Check for overrides in request body (body may be empty for apply-without-overrides)
	var body struct {
		Overrides map[string]string `json:"overrides"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		for k, v := range body.Overrides {
			if _, exists := proposal.ProposedFiles[k]; !exists {
				http.Error(w, fmt.Sprintf("override key %q not in original proposal", k), http.StatusBadRequest)
				return
			}
			proposal.ProposedFiles[k] = v
		}
	}

	// Find the repo config by name
	var barePath string
	found := false
	for _, repoConfig := range s.config.Repos {
		if repoConfig.Name == repoName {
			barePath = repoConfig.BarePath
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	bareDir := s.config.ResolveBareRepoDir(barePath)
	workDir := filepath.Join(os.TempDir(), "schmux-lore-apply")
	os.MkdirAll(workDir, 0755)

	result, err := lore.ApplyProposal(r.Context(), proposal, bareDir, workDir)
	if err != nil {
		s.logger.Error("apply proposal error", "repo", repoName, "proposal", proposalID, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Always push the branch after a successful commit
	if err := lore.PushBranch(r.Context(), bareDir, result.Branch); err != nil {
		s.logger.Error("push branch error", "repo", repoName, "branch", result.Branch, "err", err)
		http.Error(w, fmt.Sprintf("commit succeeded but push failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Optionally create a PR when auto_pr is enabled
	var prURL string
	if s.config.GetLoreAutoPR() {
		title := "chore: update instruction files with agent lore"
		prBody := proposal.DiffSummary
		if prBody == "" {
			prBody = fmt.Sprintf("Automated lore update — %d file(s) changed.", len(proposal.ProposedFiles))
		}
		url, err := lore.CreatePR(r.Context(), bareDir, result.Branch, title, prBody)
		if err != nil {
			// Log but don't fail — the commit and push already succeeded
			s.logger.Error("auto-PR creation failed", "branch", result.Branch, "err", err)
		} else {
			prURL = url
		}
	}

	// Update proposal status
	if err := s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalApplied); err != nil {
		s.logger.Error("failed to update proposal status", "err", err)
	}

	// Mark source entries as "applied" in the central state JSONL
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		entries, _ := s.readLoreEntries(repoName, nil)
		if err := lore.MarkEntriesByTextFromEntries(entries, statePath, "applied", proposal.EntriesUsed, proposalID); err != nil {
			s.logger.Warn("failed to mark entries as applied", "err", err)
		}
	}

	resp := map[string]interface{}{
		"status": "applied",
		"branch": result.Branch,
	}
	if prURL != "" {
		resp["pr_url"] = prURL
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "lore-apply", "err", err)
	}
}

// handleLoreDismiss marks a proposal as dismissed.
func (s *Server) handleLoreDismiss(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	if repoName == "" || proposalID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	// Load the proposal first to get EntriesUsed for marking
	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}

	if proposal.Status == "applied" {
		http.Error(w, "proposal is already applied", http.StatusConflict)
		return
	}

	if err := s.loreStore.UpdateStatus(repoName, proposalID, lore.ProposalDismissed); err != nil {
		s.logger.Error("update proposal status error", "err", err)
		http.Error(w, "failed to update proposal status", http.StatusInternalServerError)
		return
	}

	// Mark source entries as "dismissed" in the central state JSONL
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		entries, _ := s.readLoreEntries(repoName, nil)
		if err := lore.MarkEntriesByTextFromEntries(entries, statePath, "dismissed", proposal.EntriesUsed, proposalID); err != nil {
			s.logger.Warn("failed to mark entries as dismissed", "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "dismissed"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "lore-dismiss", "err", err)
	}
}

// handleLoreEntries returns the lore JSONL entries for a repo, aggregated from all workspace directories
// and the central state file. Supports query parameters: state, agent, type, limit.
func (s *Server) handleLoreEntries(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	// Parse query params for filtering
	q := r.URL.Query()
	state := q.Get("state")
	agent := q.Get("agent")
	entryType := q.Get("type")
	var limit int
	if limitStr := q.Get("limit"); limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	var filter lore.EntryFilter
	if state != "" || agent != "" || entryType != "" || limit > 0 {
		filter = lore.FilterByParams(state, agent, entryType, limit)
	}

	entries, err := s.readLoreEntries(repoName, filter)
	if err != nil {
		s.logger.Error("read entries error", "err", err)
		http.Error(w, "failed to read lore entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "lore-entries", "err", err)
	}
}

// handleLoreEntriesClear removes per-session event files for the given repo,
// effectively clearing the raw signal queue.
func (s *Server) handleLoreEntriesClear(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	cleared := 0
	for _, ws := range s.getLoreWorkspaces(repoName) {
		pattern := filepath.Join(ws.Path, ".schmux", "events", "*.jsonl")
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, f := range files {
			if err := os.Truncate(f, 0); err != nil {
				if !os.IsNotExist(err) {
					s.logger.Warn("failed to truncate event file", "path", f, "err", err)
				}
				continue
			}
			cleared++
		}
	}

	s.logger.Info("cleared raw signals", "repo", repoName, "files_truncated", cleared)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "cleared",
		"cleared": cleared,
	})
}

// handleLoreCurate handles manual curation requests.
// Returns immediately with a curation ID; events stream via WebSocket.
func (s *Server) handleLoreCurate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	if s.loreCurator == nil || s.loreCurator.Executor == nil {
		http.Error(w, "lore curator not configured (no LLM target)", http.StatusServiceUnavailable)
		return
	}
	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	// Guard: only one curation per repo at a time
	if s.curationTracker.IsRunning(repoName) {
		http.Error(w, "curation already in progress", http.StatusConflict)
		return
	}

	// Find the repo config by name
	var barePath string
	found := false
	for _, repoConfig := range s.config.Repos {
		if repoConfig.Name == repoName {
			barePath = repoConfig.BarePath
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	bareDir := s.config.ResolveBareRepoDir(barePath)

	// Read entries
	rawEntries, err := s.readLoreEntries(repoName, lore.FilterRaw())
	if err != nil {
		s.logger.Error("read entries error", "err", err)
		http.Error(w, fmt.Sprintf("failed to read lore entries: %v", err), http.StatusInternalServerError)
		return
	}
	if len(rawEntries) == 0 {
		s.logger.Info("curate: no raw entries to process", "repo", repoName)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_raw_entries"})
		return
	}

	// Prepare curator prompt
	instrFiles, fileHashes, err := s.loreCurator.ReadInstructionFiles(r.Context(), bareDir)
	if err != nil {
		s.logger.Error("read instruction files error", "repo", repoName, "err", err)
		http.Error(w, fmt.Sprintf("failed to read instruction files: %v", err), http.StatusInternalServerError)
		return
	}
	if len(instrFiles) == 0 {
		s.logger.Error("no instruction files found", "repo", repoName, "bare_dir", bareDir)
		http.Error(w, "no instruction files found", http.StatusInternalServerError)
		return
	}
	prompt := lore.BuildCuratorPrompt(instrFiles, rawEntries)

	// Start curation tracking
	curationID := fmt.Sprintf("cur-%s-%s", repoName, time.Now().UTC().Format("20060102-150405"))
	if _, err := s.curationTracker.Start(repoName, curationID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	// Return immediately with curation ID
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": curationID, "status": "started"})

	// Run curation in background goroutine
	go s.runStreamingCuration(repoName, curationID, prompt, instrFiles, fileHashes, rawEntries, bareDir)
}

// writeLogEvent appends a JSON event to the curation JSONL log file.
func writeLogEvent(logFile *os.File, raw json.RawMessage) {
	if logFile == nil {
		return
	}
	logFile.Write(raw)
	logFile.Write([]byte("\n"))
}

// generateRunScript creates a shell script that reproduces the curator call.
func generateRunScript(cfg *config.Config, targetName, schemaLabel string, streaming bool) string {
	cmdInfo, err := oneshot.ResolveTargetCommand(cfg, targetName, schemaLabel, streaming)
	if err != nil {
		return fmt.Sprintf("#!/bin/sh\n# Could not resolve target command: %s\nexit 1\n", err)
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Reproduce this curator run\n")
	sb.WriteString("# Generated by schmux — edit freely\n\n")

	for k, v := range cmdInfo.Env {
		fmt.Fprintf(&sb, "export %s=%q\n", k, v)
	}
	if len(cmdInfo.Env) > 0 {
		sb.WriteString("\n")
	}

	// Build the command, piping prompt.txt via stdin
	var quotedArgs []string
	for _, a := range cmdInfo.Args {
		if strings.ContainsAny(a, " \t\n\"'\\$`") {
			quotedArgs = append(quotedArgs, fmt.Sprintf("%q", a))
		} else {
			quotedArgs = append(quotedArgs, a)
		}
	}
	fmt.Fprintf(&sb, "cat \"$(dirname \"$0\")/prompt.txt\" | \\\n  %s\n", strings.Join(quotedArgs, " \\\n  "))

	return sb.String()
}

// writeDebugFile writes a file to the per-run debug directory if runDir is non-empty.
func writeDebugFile(runDir, filename, content string) {
	if runDir == "" {
		return
	}
	os.WriteFile(filepath.Join(runDir, filename), []byte(content), 0644)
}

// runStreamingCuration runs the streaming curation in the background,
// broadcasting events via WebSocket and writing debug files to a per-run directory.
func (s *Server) runStreamingCuration(repoName, curationID, prompt string, instrFiles, fileHashes map[string]string, entries []lore.Entry, bareDir string) {
	ctx, cancel := context.WithTimeout(s.shutdownCtx, 10*time.Minute)
	defer cancel()

	s.logger.Info("starting streaming curation", "repo", repoName, "curation_id", curationID, "entries", len(entries))
	start := time.Now()

	// Create per-run directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		s.logger.Error("failed to resolve home dir", "repo", repoName, "err", err)
	}
	var runDir string
	var logFile *os.File
	if homeDir != "" {
		runDir = filepath.Join(homeDir, ".schmux", "lore-curator-runs", repoName, curationID)
		os.MkdirAll(runDir, 0755)

		// Write prompt.txt
		os.WriteFile(filepath.Join(runDir, "prompt.txt"), []byte(prompt), 0644)

		// Write run.sh
		target := s.config.GetLoreTarget()
		streaming := s.streamingExecutor != nil
		runScript := generateRunScript(s.config, target, schema.LabelLoreCurator, streaming)
		os.WriteFile(filepath.Join(runDir, "run.sh"), []byte(runScript), 0755)

		// Create events.jsonl
		logFile, _ = os.Create(filepath.Join(runDir, "events.jsonl"))
		if logFile != nil {
			defer logFile.Close()
		}
	}

	// Choose executor: streaming if available, otherwise fallback to non-streaming
	if s.streamingExecutor != nil {
		s.runWithStreamingExecutor(ctx, repoName, curationID, prompt, instrFiles, fileHashes, entries, runDir, logFile, start)
	} else {
		s.runWithLegacyExecutor(ctx, repoName, curationID, prompt, instrFiles, fileHashes, entries, bareDir, runDir, logFile, start)
	}
}

// runWithStreamingExecutor runs curation using the streaming executor with event callbacks.
func (s *Server) runWithStreamingExecutor(ctx context.Context, repoName, curationID, prompt string, instrFiles, fileHashes map[string]string, entries []lore.Entry, runDir string, logFile *os.File, start time.Time) {
	onEvent := func(ev oneshot.StreamEvent) {
		curatorEvent := CuratorEvent{
			Repo:      repoName,
			Timestamp: time.Now().UTC(),
			EventType: ev.Type,
			Subtype:   ev.Subtype,
			Raw:       ev.Raw,
		}
		s.curationTracker.AddEvent(repoName, curatorEvent)
		s.BroadcastCuratorEvent(curatorEvent)

		if ev.Type == "error" || strings.HasSuffix(ev.Type, "_error") || len(ev.Error) > 0 {
			s.logger.Error("curator stream error", "repo", repoName, "curation_id", curationID, "raw", string(ev.Raw))
		}

		// Append to JSONL file
		if logFile != nil {
			logFile.Write(ev.Raw)
			logFile.Write([]byte("\n"))
		}
	}

	rawResponse, err := s.streamingExecutor(ctx, prompt, schema.LabelLoreCurator, 10*time.Minute, "", onEvent)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		writeDebugFile(runDir, "error.txt", err.Error())
		s.completeCurationWithError(repoName, fmt.Errorf("streaming executor failed: %w", err))
		return
	}

	writeDebugFile(runDir, "output.txt", rawResponse)
	s.finalizeCuration(repoName, curationID, rawResponse, instrFiles, fileHashes, entries, start, logFile)
}

// runWithLegacyExecutor runs curation using the non-streaming executor (fallback).
func (s *Server) runWithLegacyExecutor(ctx context.Context, repoName, curationID, prompt string, instrFiles, fileHashes map[string]string, entries []lore.Entry, bareDir, runDir string, logFile *os.File, start time.Time) {
	response, err := s.loreCurator.Executor(ctx, prompt, 10*time.Minute)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		writeDebugFile(runDir, "error.txt", err.Error())
		s.completeCurationWithError(repoName, fmt.Errorf("curator LLM call failed: %w", err))
		return
	}

	writeDebugFile(runDir, "output.txt", response)
	s.finalizeCuration(repoName, curationID, response, instrFiles, fileHashes, entries, start, logFile)
}

// finalizeCuration parses the response, builds proposal, saves it, and marks entries.
func (s *Server) finalizeCuration(repoName, curationID, rawResponse string, instrFiles, fileHashes map[string]string, entries []lore.Entry, start time.Time, logFile *os.File) {
	elapsed := time.Since(start)

	result, err := lore.ParseCuratorResponse(rawResponse)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		s.completeCurationWithError(repoName, fmt.Errorf("failed to parse curator response: %w", err))
		return
	}

	proposal, err := lore.BuildProposal(repoName, result, instrFiles, fileHashes, entries)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		s.completeCurationWithError(repoName, fmt.Errorf("failed to build proposal: %w", err))
		return
	}

	if err := s.loreStore.Save(proposal); err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		s.completeCurationWithError(repoName, fmt.Errorf("failed to save proposal: %w", err))
		return
	}

	// Mark entries as proposed
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		entries, _ := s.readLoreEntries(repoName, nil)
		if err := lore.MarkEntriesByTextFromEntries(entries, statePath, "proposed", proposal.EntriesUsed, proposal.ID); err != nil {
			s.logger.Warn("failed to mark entries as proposed", "err", err)
		}
	}

	doneRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_done","proposal_id":%q,"file_count":%d}`, proposal.ID, len(proposal.ProposedFiles)))
	writeLogEvent(logFile, doneRaw)

	s.curationTracker.Complete(repoName, nil)
	s.BroadcastCuratorEvent(CuratorEvent{
		Repo:      repoName,
		Timestamp: time.Now().UTC(),
		EventType: "curator_done",
		Raw:       doneRaw,
	})

	s.logger.Info("proposal created", "repo", repoName, "proposal", proposal.ID, "files", len(proposal.ProposedFiles), "entries_used", len(proposal.EntriesUsed), "elapsed", elapsed.Round(time.Millisecond))
}

// completeCurationWithError marks a curation as failed and broadcasts the error.
func (s *Server) completeCurationWithError(repoName string, err error) {
	s.logger.Error("curation error", "repo", repoName, "err", err)
	s.curationTracker.Complete(repoName, err)
	s.BroadcastCuratorEvent(CuratorEvent{
		Repo:      repoName,
		Timestamp: time.Now().UTC(),
		EventType: "curator_error",
		Raw:       json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
	})
}

// handleLoreCurationsActive returns all active curation runs with their buffered events.
func (s *Server) handleLoreCurationsActive(w http.ResponseWriter, r *http.Request) {
	runs := s.curationTracker.Active()
	if runs == nil {
		runs = []*CurationRun{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// handleLoreCurationsList returns past curation run logs for a repo.
func (s *Server) handleLoreCurationsList(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if repoName == "" {
		http.Error(w, "missing repo name", http.StatusBadRequest)
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "failed to resolve home directory", http.StatusInternalServerError)
		return
	}
	runDir := filepath.Join(homeDir, ".schmux", "lore-curator-runs", repoName)

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

// handleLoreCurationLog returns the JSONL log content for a specific curation run.
func (s *Server) handleLoreCurationLog(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	curationID := chi.URLParam(r, "curationID")
	if repoName == "" || curationID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Validate curation ID — no path separators allowed
	if strings.ContainsAny(curationID, "/\\") || curationID == ".." || curationID == "." {
		http.Error(w, "invalid curation ID", http.StatusBadRequest)
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "failed to resolve home directory", http.StatusInternalServerError)
		return
	}

	logPath := filepath.Join(homeDir, ".schmux", "lore-curator-runs", repoName, curationID, "events.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		http.Error(w, "log file not found", http.StatusNotFound)
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

// refreshLoreCurator updates the lore curator's executor based on the current
// config. Called after config save so the runtime curator stays in sync with
// the persisted lore.llm_target value.
func (s *Server) refreshLoreCurator(cfg *config.Config) {
	target := cfg.GetLoreTargetRaw()

	if s.loreCurator == nil {
		// Lore was disabled at startup — create a curator now
		s.loreCurator = &lore.Curator{
			InstructionFiles: cfg.GetLoreInstructionFiles(),
			BareRepo:         true,
		}
	}

	if target != "" {
		s.loreCurator.Executor = func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
			return oneshot.ExecuteTarget(ctx, cfg, target, prompt, schema.LabelLoreCurator, timeout, "")
		}
	} else {
		s.loreCurator.Executor = nil
	}
}
