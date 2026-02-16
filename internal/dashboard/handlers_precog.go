package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/precog"
)

// precogJobs tracks running precog analysis jobs.
var (
	precogJobs   = make(map[string]bool) // repoName -> running
	precogJobsMu sync.Mutex
)

// POST /api/precog/repo/{repoName}
// Starts a precog analysis job for the specified repository.
func (s *Server) handlePrecogStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract repo name from path: /api/precog/repo/{repoName}
	path := strings.TrimPrefix(r.URL.Path, "/api/precog/repo/")
	repoName := strings.TrimSuffix(path, "/")
	if repoName == "" {
		http.Error(w, "Missing repo name", http.StatusBadRequest)
		return
	}

	// Check if already running
	precogJobsMu.Lock()
	if precogJobs[repoName] {
		precogJobsMu.Unlock()
		http.Error(w, "Analysis already running for this repo", http.StatusConflict)
		return
	}
	precogJobs[repoName] = true
	precogJobsMu.Unlock()

	// Create analyzer
	analyzer, err := precog.NewAnalyzer(s.config, repoName)
	if err != nil {
		precogJobsMu.Lock()
		delete(precogJobs, repoName)
		precogJobsMu.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Generate job ID
	jobID := fmt.Sprintf("rcm-%s-%d", repoName, time.Now().Unix())

	// Start analysis in background
	go func() {
		defer func() {
			precogJobsMu.Lock()
			delete(precogJobs, repoName)
			precogJobsMu.Unlock()
		}()

		updateMeta := func(meta precog.JobMeta) {
			if err := precog.SaveJobMeta(repoName, meta); err != nil {
				fmt.Printf("[precog] failed to save job meta: %v\n", err)
			}
		}

		rcm, err := analyzer.Run(context.Background(), jobID, updateMeta)
		if err != nil {
			fmt.Printf("[precog] analysis failed for %s: %v\n", repoName, err)
			return
		}

		if err := precog.SaveRCM(repoName, rcm); err != nil {
			fmt.Printf("[precog] failed to save RCM: %v\n", err)
		}
	}()

	// Return immediately
	resp := contracts.PrecogStartResponse{
		Status: "started",
		JobID:  jobID,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GET /api/precog/repo/{repoName}/status
// Returns the status of the precog analysis job.
func (s *Server) handlePrecogStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract repo name from path: /api/precog/repo/{repoName}/status
	path := strings.TrimPrefix(r.URL.Path, "/api/precog/repo/")
	path = strings.TrimSuffix(path, "/status")
	repoName := path
	if repoName == "" {
		http.Error(w, "Missing repo name", http.StatusBadRequest)
		return
	}

	meta, err := precog.LoadJobMeta(repoName)
	if err != nil {
		http.Error(w, "No analysis found", http.StatusNotFound)
		return
	}

	resp := contracts.PrecogJobStatus{
		Status:      meta.Status,
		CurrentPass: meta.CurrentPass,
		StartedAt:   meta.StartedAt,
		CompletedAt: meta.CompletedAt,
		Error:       meta.Error,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GET /api/precog/repo/{repoName}
// Returns the RCM analysis result.
func (s *Server) handlePrecogGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract repo name from path: /api/precog/repo/{repoName}
	path := strings.TrimPrefix(r.URL.Path, "/api/precog/repo/")
	repoName := strings.TrimSuffix(path, "/")
	if repoName == "" {
		http.Error(w, "Missing repo name", http.StatusBadRequest)
		return
	}

	rcm, err := precog.LoadRCM(repoName)
	if err != nil {
		http.Error(w, "No analysis found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rcm)
}

// POST /api/precog/repo/{repoName}/{passId}
// Runs a single pass and updates the RCM.
func (s *Server) handlePrecogRunPass(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract repo name and pass ID from path: /api/precog/repo/{repoName}/{passId}
	path := strings.TrimPrefix(r.URL.Path, "/api/precog/repo/")
	path = strings.TrimSuffix(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	repoName := parts[0]
	passID := strings.ToUpper(parts[1])

	if repoName == "" || passID == "" {
		http.Error(w, "Missing repo name or pass ID", http.StatusBadRequest)
		return
	}

	// Check if already running
	jobKey := fmt.Sprintf("%s-pass-%s", repoName, passID)
	precogJobsMu.Lock()
	if precogJobs[jobKey] {
		precogJobsMu.Unlock()
		http.Error(w, fmt.Sprintf("Pass %s already running for this repo", passID), http.StatusConflict)
		return
	}
	precogJobs[jobKey] = true
	precogJobsMu.Unlock()

	// Create analyzer
	analyzer, err := precog.NewAnalyzer(s.config, repoName)
	if err != nil {
		precogJobsMu.Lock()
		delete(precogJobs, jobKey)
		precogJobsMu.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Generate job ID
	jobID := fmt.Sprintf("rcm-%s-pass-%s-%d", repoName, passID, time.Now().Unix())

	fmt.Printf("[precog] Starting pass %s for %s (job: %s)\n", passID, repoName, jobID)

	// Run async
	go func() {
		defer func() {
			precogJobsMu.Lock()
			delete(precogJobs, jobKey)
			precogJobsMu.Unlock()
		}()

		fmt.Printf("[precog] Pass %s running for %s\n", passID, repoName)

		rcm, err := analyzer.RunPass(context.Background(), passID)
		if err != nil {
			fmt.Printf("[precog] Pass %s failed for %s: %v\n", passID, repoName, err)
			return
		}

		fmt.Printf("[precog] Pass %s completed for %s, saving RCM\n", passID, repoName)

		if err := precog.SaveRCM(repoName, rcm); err != nil {
			fmt.Printf("[precog] Failed to save RCM after pass %s: %v\n", passID, err)
			return
		}

		fmt.Printf("[precog] Pass %s saved successfully for %s\n", passID, repoName)
	}()

	// Return immediately
	resp := contracts.PrecogStartResponse{
		Status: "started",
		JobID:  jobID,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePrecogRoutes routes precog API requests.
func (s *Server) handlePrecogRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// POST /api/precog/repo/{repoName} - start full analysis
	// POST /api/precog/repo/{repoName}/{passId} - run single pass
	// GET /api/precog/repo/{repoName} - get result
	// GET /api/precog/repo/{repoName}/status - get status

	if strings.HasSuffix(path, "/status") {
		s.handlePrecogStatus(w, r)
		return
	}

	// Check if this is a single-pass request: /api/precog/repo/{repoName}/{passId}
	// Pass IDs are single letters: a, b, c, d, e, f
	trimmed := strings.TrimPrefix(path, "/api/precog/repo/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	if r.Method == http.MethodPost && len(parts) == 2 && len(parts[1]) == 1 {
		s.handlePrecogRunPass(w, r)
		return
	}

	if r.Method == http.MethodPost {
		s.handlePrecogStart(w, r)
		return
	}

	if r.Method == http.MethodGet {
		s.handlePrecogGet(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
