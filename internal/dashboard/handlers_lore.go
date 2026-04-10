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

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schema"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
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
	curatorConfigured := s.loreExecutor != nil

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

	// Invalidate any pending merge that includes rules from this proposal
	if s.lorePendingMergeStore != nil {
		for _, rule := range proposal.Rules {
			s.lorePendingMergeStore.InvalidateIfContainsRule(repoName, rule.ID)
		}
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

// handleLoreRuleUpdate updates a specific rule within a proposal (approve/dismiss/edit/reroute).
func (s *Server) handleLoreRuleUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	ruleID := chi.URLParam(r, "ruleID")
	if repoName == "" || proposalID == "" || ruleID == "" {
		http.Error(w, "missing path parameters", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Status      string  `json:"status"`       // "approved", "dismissed", or "pending"
		Text        *string `json:"text"`         // edited text (optional)
		ChosenLayer *string `json:"chosen_layer"` // layer override (optional)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate status
	var status lore.RuleStatus
	switch body.Status {
	case "approved":
		status = lore.RuleApproved
	case "dismissed":
		status = lore.RuleDismissed
	case "pending":
		status = lore.RulePending
	case "":
		// No status change — only updating text or layer
	default:
		http.Error(w, "status must be 'approved', 'dismissed', or 'pending'", http.StatusBadRequest)
		return
	}

	// Validate chosen_layer if provided
	var chosenLayer *lore.Layer
	if body.ChosenLayer != nil {
		l := lore.Layer(*body.ChosenLayer)
		switch l {
		case lore.LayerRepoPublic, lore.LayerRepoPrivate, lore.LayerCrossRepoPrivate:
			chosenLayer = &l
		default:
			http.Error(w, "chosen_layer must be 'repo_public', 'repo_private', or 'cross_repo_private'", http.StatusBadRequest)
			return
		}
	}

	update := lore.RuleUpdate{
		Status:      status,
		Text:        body.Text,
		ChosenLayer: chosenLayer,
	}

	if err := s.loreStore.UpdateRule(repoName, proposalID, ruleID, update); err != nil {
		s.logger.Error("update rule error", "repo", repoName, "proposal", proposalID, "rule", ruleID, "err", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Invalidate any pending merge that includes this rule
	if s.lorePendingMergeStore != nil {
		s.lorePendingMergeStore.InvalidateIfContainsRule(repoName, ruleID)
	}

	// Return the updated proposal
	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err != nil {
		http.Error(w, "failed to reload proposal", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposal); err != nil {
		s.logger.Error("failed to encode response", "handler", "lore-rule-update", "err", err)
	}
}

// mergeApplyRequest represents a per-layer merge to apply.
type mergeApplyRequest struct {
	Layer   string `json:"layer"`
	Content string `json:"content"` // possibly user-edited merged content
}

// handleLoreApplyMerge applies reviewed merge results to their target layers.
func (s *Server) handleLoreApplyMerge(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	repoName := chi.URLParam(r, "repo")
	proposalID := chi.URLParam(r, "proposalID")
	if repoName == "" || proposalID == "" {
		http.Error(w, "missing path parameters", http.StatusBadRequest)
		return
	}

	if s.loreStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Merges     []mergeApplyRequest `json:"merges"`
		AutoCommit bool                `json:"auto_commit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Merges) == 0 {
		http.Error(w, "no merges provided", http.StatusBadRequest)
		return
	}

	var results []map[string]string

	for _, m := range body.Merges {
		layer := lore.Layer(m.Layer)
		switch layer {
		case lore.LayerRepoPublic:
			http.Error(w, "public layer is now handled via /merge and /push endpoints", http.StatusGone)
			return

		case lore.LayerRepoPrivate, lore.LayerCrossRepoPrivate:
			if s.loreInstructionStore == nil {
				http.Error(w, "instruction store not configured", http.StatusServiceUnavailable)
				return
			}
			if err := lore.ApplyToLayer(s.loreInstructionStore, layer, repoName, m.Content); err != nil {
				s.logger.Error("apply to layer failed", "repo", repoName, "layer", layer, "err", err)
				http.Error(w, fmt.Sprintf("apply to %s failed: %v", layer, err), http.StatusInternalServerError)
				return
			}
			results = append(results, map[string]string{"layer": m.Layer, "status": "applied"})

		default:
			http.Error(w, fmt.Sprintf("invalid layer: %s", m.Layer), http.StatusBadRequest)
			return
		}
	}

	// Mark rules as merged and update proposal status
	now := time.Now().UTC()
	proposal, err := s.loreStore.Get(repoName, proposalID)
	if err == nil {
		for i := range proposal.Rules {
			if proposal.Rules[i].Status == lore.RuleApproved {
				proposal.Rules[i].MergedAt = &now
			}
		}
		proposal.Status = lore.ProposalApplied
		s.loreStore.Save(proposal)
	}

	// Mark source entries as applied in state JSONL
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil && proposal != nil {
		entries, _ := s.readLoreEntries(repoName, nil)
		var sourceKeys []string
		for _, rule := range proposal.Rules {
			if rule.Status == lore.RuleApproved {
				for _, se := range rule.SourceEntries {
					switch se.Type {
					case "failure":
						sourceKeys = append(sourceKeys, se.InputSummary)
					default:
						sourceKeys = append(sourceKeys, se.Text)
					}
				}
			}
		}
		if len(sourceKeys) > 0 {
			lore.MarkEntriesByTextFromEntries(entries, statePath, "applied", sourceKeys, proposalID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "applied",
		"results": results,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "lore-apply-merge", "err", err)
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
		pattern := filepath.Join(state.SchmuxDataDir(ws.Path), "events", "*.jsonl")
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

	if s.loreExecutor == nil {
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

	// Prepare curator prompt — v2 extraction (no instruction files needed)
	existingRules := s.loreStore.PendingRuleTexts(repoName)
	dismissedRules := s.loreStore.DismissedRuleTexts(repoName)
	prompt := lore.BuildExtractionPrompt(rawEntries, existingRules, dismissedRules)

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
	go s.runStreamingCuration(repoName, curationID, prompt, rawEntries)
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
func (s *Server) runStreamingCuration(repoName, curationID, prompt string, entries []lore.Entry) {
	ctx, cancel := context.WithTimeout(s.shutdownCtx, 10*time.Minute)
	defer cancel()

	s.logger.Info("starting streaming curation", "repo", repoName, "curation_id", curationID, "entries", len(entries))
	start := time.Now()

	// Create per-run directory
	var runDir string
	var logFile *os.File
	if sd := schmuxdir.Get(); sd != "" {
		runDir = filepath.Join(sd, "lore-curator-runs", repoName, curationID)
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
		s.runWithStreamingExecutor(ctx, repoName, curationID, prompt, entries, runDir, logFile, start)
	} else {
		s.runWithLegacyExecutor(ctx, repoName, curationID, prompt, entries, runDir, logFile, start)
	}
}

// runWithStreamingExecutor runs curation using the streaming executor with event callbacks.
func (s *Server) runWithStreamingExecutor(ctx context.Context, repoName, curationID, prompt string, entries []lore.Entry, runDir string, logFile *os.File, start time.Time) {
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

	rawResponse, err := s.streamingExecutor(ctx, prompt, "", 10*time.Minute, "", onEvent)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		writeDebugFile(runDir, "error.txt", err.Error())
		s.completeCurationWithError(repoName, fmt.Errorf("streaming executor failed: %w", err))
		return
	}

	writeDebugFile(runDir, "output.txt", rawResponse)
	s.finalizeCuration(repoName, curationID, rawResponse, entries, start, logFile)
}

// runWithLegacyExecutor runs curation using the non-streaming executor (fallback).
func (s *Server) runWithLegacyExecutor(ctx context.Context, repoName, curationID, prompt string, entries []lore.Entry, runDir string, logFile *os.File, start time.Time) {
	response, err := s.loreExecutor(ctx, prompt, schema.LabelLoreCurator, 10*time.Minute)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		writeDebugFile(runDir, "error.txt", err.Error())
		s.completeCurationWithError(repoName, fmt.Errorf("curator LLM call failed: %w", err))
		return
	}

	writeDebugFile(runDir, "output.txt", response)
	s.finalizeCuration(repoName, curationID, response, entries, start, logFile)
}

// finalizeCuration parses the extraction response, builds a proposal with per-rule model, saves it, and marks entries.
func (s *Server) finalizeCuration(repoName, curationID, rawResponse string, entries []lore.Entry, start time.Time, logFile *os.File) {
	elapsed := time.Since(start)

	result, err := lore.ParseExtractionResponse(rawResponse)
	if err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		s.completeCurationWithError(repoName, fmt.Errorf("failed to parse extraction response: %w", err))
		return
	}

	// Build per-rule proposal from extraction result
	now := time.Now().UTC()
	proposalID := fmt.Sprintf("prop-%s", now.Format("20060102-150405-")+curationID[len(curationID)-4:])
	proposal := &lore.Proposal{
		ID:        proposalID,
		Repo:      repoName,
		CreatedAt: now,
		Status:    lore.ProposalPending,
		Discarded: result.DiscardedEntries,
	}

	for i, er := range result.Rules {
		proposal.Rules = append(proposal.Rules, lore.Rule{
			ID:             fmt.Sprintf("r%d", i+1),
			Text:           er.Text,
			Category:       er.Category,
			SuggestedLayer: lore.Layer(er.SuggestedLayer),
			Status:         lore.RulePending,
			SourceEntries:  er.SourceEntries,
		})
	}

	// Deduplicate against existing pending and dismissed proposals (safety net for LLM re-extraction)
	existingTexts := s.loreStore.PendingRuleTexts(repoName)
	dismissedTexts := s.loreStore.DismissedRuleTexts(repoName)
	allExcluded := append(existingTexts, dismissedTexts...)
	proposal.Rules, _ = lore.DeduplicateRules(proposal.Rules, allExcluded)

	if len(proposal.Rules) == 0 {
		s.logger.Info("all extracted rules are duplicates of existing proposals", "repo", repoName)
	}

	if err := s.loreStore.Save(proposal); err != nil {
		errRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_error","error":%q}`, err.Error()))
		writeLogEvent(logFile, errRaw)
		s.completeCurationWithError(repoName, fmt.Errorf("failed to save proposal: %w", err))
		return
	}

	// Mark all curated entries as proposed — uses direct timestamp marking
	// instead of matching by LLM source_entries, which is unreliable because
	// the LLM's source_entries format rarely matches EntryKey() exactly.
	// This also covers entries the LLM discarded, preventing them from being
	// re-curated on subsequent runs.
	statePath, err := lore.LoreStatePath(repoName)
	if err == nil {
		if err := lore.MarkEntriesDirect(entries, statePath, "proposed", proposal.ID); err != nil {
			s.logger.Warn("failed to mark entries as proposed", "err", err)
		}
	}

	doneRaw := json.RawMessage(fmt.Sprintf(`{"type":"curator_done","proposal_id":%q,"rule_count":%d}`, proposal.ID, len(proposal.Rules)))
	writeLogEvent(logFile, doneRaw)

	s.curationTracker.Complete(repoName, nil)
	s.BroadcastCuratorEvent(CuratorEvent{
		Repo:      repoName,
		Timestamp: time.Now().UTC(),
		EventType: "curator_done",
		Raw:       doneRaw,
	})

	s.logger.Info("proposal created", "repo", repoName, "proposal", proposal.ID, "rules", len(proposal.Rules), "elapsed", elapsed.Round(time.Millisecond))
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

	runDir := filepath.Join(schmuxdir.Get(), "lore-curator-runs", repoName)

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

	logPath := filepath.Join(schmuxdir.Get(), "lore-curator-runs", repoName, curationID, "events.jsonl")
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

// handleLoreUnifiedMerge triggers a unified merge of approved public rules across
// multiple proposals into the repo's instruction file. It creates a PendingMerge
// in "merging" status, returns 202 immediately, and runs the LLM merge in the
// background.
func (s *Server) handleLoreUnifiedMerge(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	repoName := chi.URLParam(r, "repo")

	if s.loreStore == nil || s.lorePendingMergeStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}
	if s.loreExecutor == nil {
		http.Error(w, "lore curator not configured (no LLM target)", http.StatusServiceUnavailable)
		return
	}

	// Check for existing merging state
	if existing, err := s.lorePendingMergeStore.Get(repoName); err == nil && existing.Status == lore.PendingMergeStatusMerging {
		http.Error(w, "merge already in progress", http.StatusConflict)
		return
	}

	var body struct {
		Proposals []struct {
			ProposalID string   `json:"proposal_id"`
			RuleIDs    []string `json:"rule_ids"`
		} `json:"proposals"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Proposals) == 0 {
		http.Error(w, "no proposals provided", http.StatusBadRequest)
		return
	}

	// Gather approved public rules from all specified proposals
	var allRules []lore.Rule
	var allRuleIDs, allProposalIDs []string
	for _, pg := range body.Proposals {
		proposal, err := s.loreStore.Get(repoName, pg.ProposalID)
		if err != nil {
			http.Error(w, fmt.Sprintf("proposal %s not found", pg.ProposalID), http.StatusNotFound)
			return
		}
		allProposalIDs = append(allProposalIDs, pg.ProposalID)
		for _, rule := range proposal.Rules {
			if rule.Status == lore.RuleApproved && rule.EffectiveLayer() == lore.LayerRepoPublic {
				allRules = append(allRules, rule)
				allRuleIDs = append(allRuleIDs, rule.ID)
			}
		}
	}
	if len(allRules) == 0 {
		http.Error(w, "no approved public rules to merge", http.StatusBadRequest)
		return
	}

	// Find bare repo dir
	var bareDir string
	for _, repoConfig := range s.config.Repos {
		if repoConfig.Name == repoName {
			bareDir = s.config.ResolveBareRepoDir(repoConfig.BarePath)
			break
		}
	}
	if bareDir == "" {
		http.Error(w, "repo bare directory not found", http.StatusNotFound)
		return
	}

	// Create PendingMerge in "merging" state
	pm := &lore.PendingMerge{
		Repo:        repoName,
		Status:      lore.PendingMergeStatusMerging,
		RuleIDs:     allRuleIDs,
		ProposalIDs: allProposalIDs,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.lorePendingMergeStore.Save(pm); err != nil {
		http.Error(w, "failed to create pending merge", http.StatusInternalServerError)
		return
	}

	// Return 202 immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "merging"})

	// Run merge in background
	executor := s.loreExecutor
	pendingStore := s.lorePendingMergeStore
	instrFiles := s.config.GetLoreInstructionFiles()
	logger := s.logger

	go func() {
		ctx, cancel := context.WithTimeout(s.shutdownCtx, 5*time.Minute)
		defer cancel()

		// Fetch latest from remote
		fetchCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "fetch", "--quiet")
		fetchCmd.Run() // best effort

		// Read current instruction file
		targetFile := "CLAUDE.md"
		if len(instrFiles) > 0 {
			targetFile = instrFiles[0]
		}
		currentContent, err := lore.ReadFileFromRepo(ctx, bareDir, targetFile)
		if err != nil {
			logger.Error("failed to read instruction file from repo", "err", err)
			currentContent = "" // empty file is valid — first-time setup
		}

		// Get base SHA
		shaCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "rev-parse", "HEAD")
		shaOut, _ := shaCmd.Output()
		baseSHA := strings.TrimSpace(string(shaOut))

		// Run LLM merge
		prompt := lore.BuildMergePrompt(currentContent, allRules)
		response, err := executor(ctx, prompt, "", 5*time.Minute)
		if err != nil {
			pm.Status = lore.PendingMergeStatusError
			pm.Error = fmt.Sprintf("Merge failed: %v", err)
			pendingStore.Save(pm)
			s.BroadcastCuratorEvent(CuratorEvent{
				Repo: repoName, Timestamp: time.Now().UTC(),
				EventType: "lore_merge_complete",
				Raw:       json.RawMessage(fmt.Sprintf(`{"status":"error","error":%q}`, pm.Error)),
			})
			return
		}

		result, err := lore.ParseMergeResponse(response)
		if err != nil {
			pm.Status = lore.PendingMergeStatusError
			pm.Error = fmt.Sprintf("Failed to parse merge result: %v", err)
			pendingStore.Save(pm)
			s.BroadcastCuratorEvent(CuratorEvent{
				Repo: repoName, Timestamp: time.Now().UTC(),
				EventType: "lore_merge_complete",
				Raw:       json.RawMessage(fmt.Sprintf(`{"status":"error","error":%q}`, pm.Error)),
			})
			return
		}

		// Update PendingMerge to ready
		pm.Status = lore.PendingMergeStatusReady
		pm.BaseSHA = baseSHA
		pm.CurrentContent = currentContent
		pm.MergedContent = result.MergedContent
		pm.Summary = result.Summary
		pm.Error = ""
		pendingStore.Save(pm)

		s.BroadcastCuratorEvent(CuratorEvent{
			Repo: repoName, Timestamp: time.Now().UTC(),
			EventType: "lore_merge_complete",
			Raw:       json.RawMessage(fmt.Sprintf(`{"status":"ready","repo":%q}`, repoName)),
		})
		logger.Info("unified merge complete", "repo", repoName, "rules", len(allRules))
	}()
}

// handleLorePendingMergeGet returns the pending merge for the given repo.
func (s *Server) handleLorePendingMergeGet(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if s.lorePendingMergeStore == nil {
		http.Error(w, "pending merge store not configured", http.StatusServiceUnavailable)
		return
	}
	pm, err := s.lorePendingMergeStore.Get(repoName)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pm)
}

// handleLorePendingMergeDelete removes a pending merge for the given repo.
func (s *Server) handleLorePendingMergeDelete(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")
	if s.lorePendingMergeStore == nil {
		http.Error(w, "pending merge store not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.lorePendingMergeStore.Delete(repoName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// handleLorePendingMergePatch updates the edited content of a pending merge.
func (s *Server) handleLorePendingMergePatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	repoName := chi.URLParam(r, "repo")
	if s.lorePendingMergeStore == nil {
		http.Error(w, "pending merge store not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		EditedContent *string `json:"edited_content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.EditedContent == nil {
		http.Error(w, "edited_content is required", http.StatusBadRequest)
		return
	}
	if err := s.lorePendingMergeStore.UpdateEditedContent(repoName, *body.EditedContent); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleLorePush pushes the pending merge content to the repo's instruction file.
// It validates the PendingMerge, checks for staleness, commits and pushes.
func (s *Server) handleLorePush(w http.ResponseWriter, r *http.Request) {
	repoName := chi.URLParam(r, "repo")

	if s.loreStore == nil || s.lorePendingMergeStore == nil {
		http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
		return
	}

	// 1. Get and validate PendingMerge
	pm, err := s.lorePendingMergeStore.Get(repoName)
	if err != nil {
		http.Error(w, "no pending merge found", http.StatusNotFound)
		return
	}
	if pm.Status != lore.PendingMergeStatusReady {
		http.Error(w, fmt.Sprintf("pending merge is not ready (status: %s)", pm.Status), http.StatusConflict)
		return
	}
	if pm.IsExpired() {
		http.Error(w, "pending merge has expired", http.StatusGone)
		return
	}

	// 2. Server-side rule validation: verify all rules are still approved
	for _, proposalID := range pm.ProposalIDs {
		proposal, err := s.loreStore.Get(repoName, proposalID)
		if err != nil {
			http.Error(w, fmt.Sprintf("proposal %s not found", proposalID), http.StatusNotFound)
			return
		}
		for _, ruleID := range pm.RuleIDs {
			for _, rule := range proposal.Rules {
				if rule.ID == ruleID && rule.Status != lore.RuleApproved {
					http.Error(w, fmt.Sprintf("rule %s is no longer approved", ruleID), http.StatusConflict)
					return
				}
			}
		}
	}

	// 3. Compute instrFiles and targetFile
	instrFiles := s.config.GetLoreInstructionFiles()
	targetFile := "CLAUDE.md"
	if len(instrFiles) > 0 {
		targetFile = instrFiles[0]
	}

	// 4. Find bare repo
	var bareDir string
	for _, repoConfig := range s.config.Repos {
		if repoConfig.Name == repoName {
			bareDir = s.config.ResolveBareRepoDir(repoConfig.BarePath)
			break
		}
	}
	if bareDir == "" {
		http.Error(w, "repo bare directory not found", http.StatusNotFound)
		return
	}

	// 5. Freshness check: compare BaseSHA vs current HEAD
	fetchCmd := exec.CommandContext(r.Context(), "git", "-C", bareDir, "fetch", "--quiet")
	fetchCmd.Run() // best effort

	shaCmd := exec.CommandContext(r.Context(), "git", "-C", bareDir, "rev-parse", "HEAD")
	shaOut, err := shaCmd.Output()
	if err != nil {
		http.Error(w, "failed to read current HEAD from bare repo", http.StatusInternalServerError)
		return
	}
	currentSHA := strings.TrimSpace(string(shaOut))

	if currentSHA != pm.BaseSHA {
		// SHA changed — check if file content is still the same
		currentContent, err := lore.ReadFileFromRepo(r.Context(), bareDir, targetFile)
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
		http.Error(w, "failed to determine default branch", http.StatusInternalServerError)
		return
	}
	defaultBranch := strings.TrimSpace(string(refOut))
	defaultBranch = strings.TrimPrefix(defaultBranch, "refs/heads/")

	// 7. Clone to a temporary directory
	worktreeDir := filepath.Join(os.TempDir(), fmt.Sprintf("lore-push-%s-%d", repoName, time.Now().UnixMilli()))
	defer os.RemoveAll(worktreeDir)

	cloneCmd := exec.CommandContext(r.Context(), "git", "clone", bareDir, worktreeDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("failed to clone: %s: %s", err, out), http.StatusInternalServerError)
		return
	}

	// 8. Write merged content
	fullPath := filepath.Join(worktreeDir, filepath.Clean(targetFile))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		http.Error(w, fmt.Sprintf("failed to create directory: %v", err), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(fullPath, []byte(pm.EffectiveContent()), 0644); err != nil {
		http.Error(w, fmt.Sprintf("failed to write file: %v", err), http.StatusInternalServerError)
		return
	}

	// Stage
	stageCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "add", targetFile)
	if out, err := stageCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("failed to stage: %s: %s", err, out), http.StatusInternalServerError)
		return
	}

	// Count approved rules for commit message
	approvedCount := len(pm.RuleIDs)
	if approvedCount == 0 {
		approvedCount = 1
	}
	commitMsg := fmt.Sprintf("lore: add %d rules from agent learnings", approvedCount)

	// 9. Commit with env vars for author/committer
	commitCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=schmux",
		"GIT_AUTHOR_EMAIL=schmux@localhost",
		"GIT_COMMITTER_NAME=schmux",
		"GIT_COMMITTER_EMAIL=schmux@localhost",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("failed to commit: %s: %s", err, out), http.StatusInternalServerError)
		return
	}

	// Get commit SHA
	commitSHACmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "rev-parse", "HEAD")
	commitSHAOut, err := commitSHACmd.Output()
	if err != nil {
		http.Error(w, "failed to read commit SHA", http.StatusInternalServerError)
		return
	}
	commitSHA := strings.TrimSpace(string(commitSHAOut))

	// 10. Push based on config mode
	mode := "direct_push"
	if s.config != nil && s.config.Lore != nil {
		mode = s.config.Lore.GetPublicRuleMode()
	}
	if mode == "create_pr" {
		branch := fmt.Sprintf("lore/rules-%s", time.Now().Format("2006-01-02"))
		exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "checkout", "-b", branch).Run()
		pushCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "push", "-u", "origin", branch)
		if out, err := pushCmd.CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf("push to branch failed: %s: %s", err, out), http.StatusInternalServerError)
			return
		}
	} else {
		pushCmd := exec.CommandContext(r.Context(), "git", "-C", worktreeDir, "push", "origin", "HEAD:"+defaultBranch)
		if out, err := pushCmd.CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf("push failed: %s: %s", err, out), http.StatusInternalServerError)
			return
		}
	}

	// 11. Delete PendingMerge
	s.lorePendingMergeStore.Delete(repoName)

	// 12. Mark rules as applied in proposals
	now := time.Now().UTC()
	for _, proposalID := range pm.ProposalIDs {
		proposal, err := s.loreStore.Get(repoName, proposalID)
		if err != nil {
			continue
		}
		for i := range proposal.Rules {
			for _, ruleID := range pm.RuleIDs {
				if proposal.Rules[i].ID == ruleID && proposal.Rules[i].Status == lore.RuleApproved {
					proposal.Rules[i].MergedAt = &now
				}
			}
		}
		if proposal.AllRulesResolved() {
			proposal.Status = lore.ProposalApplied
		}
		s.loreStore.Save(proposal)
	}

	// 13. Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":     "pushed",
		"commit_sha": commitSHA,
	})
}

// refreshLoreExecutor updates the lore LLM executor based on the current
// config. Called after config save so the runtime executor stays in sync with
// the persisted lore.llm_target value.
func (s *Server) refreshLoreExecutor(cfg *config.Config) {
	target := cfg.GetLoreTargetRaw()

	if target != "" {
		s.loreExecutor = func(ctx context.Context, prompt, schemaLabel string, timeout time.Duration) (string, error) {
			return oneshot.ExecuteTarget(ctx, cfg, target, prompt, schemaLabel, timeout, "")
		}
	} else {
		s.loreExecutor = nil
	}
}
