package dashboard

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CurationRun tracks a single active curation with its streamed events.
type CurationRun struct {
	ID          string         `json:"id"`
	Repo        string         `json:"repo"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
	Events      []CuratorEvent `json:"events"`
	Done        bool           `json:"done"`
	Error       string         `json:"error,omitempty"`
}

// CuratorEvent is a stream-json event enriched with curation metadata.
type CuratorEvent struct {
	Repo      string          `json:"repo"`
	Timestamp time.Time       `json:"timestamp"`
	EventType string          `json:"event_type"` // "system", "assistant", "user", "result", "curator_done", "curator_error"
	Subtype   string          `json:"subtype,omitempty"`
	Raw       json.RawMessage `json:"raw"`
}

// CurationTracker manages active curation runs.
type CurationTracker struct {
	mu   sync.RWMutex
	runs map[string]*CurationRun // keyed by repo name (one curation per repo at a time)
}

// NewCurationTracker creates a new CurationTracker.
func NewCurationTracker() *CurationTracker {
	return &CurationTracker{runs: make(map[string]*CurationRun)}
}

// Start begins tracking a new curation run for a repo. Returns error if one is already running.
func (ct *CurationTracker) Start(repo, id string) (*CurationRun, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Opportunistic cleanup of old completed runs
	cutoff := time.Now().Add(-5 * time.Minute)
	for r, run := range ct.runs {
		if run.Done && !run.CompletedAt.IsZero() && run.CompletedAt.Before(cutoff) {
			delete(ct.runs, r)
		}
	}

	if existing, ok := ct.runs[repo]; ok && !existing.Done {
		return nil, fmt.Errorf("curation already in progress for repo %s", repo)
	}

	run := &CurationRun{
		ID:        id,
		Repo:      repo,
		StartedAt: time.Now().UTC(),
		Events:    make([]CuratorEvent, 0),
	}
	ct.runs[repo] = run
	return run, nil
}

// AddEvent appends an event to the run for the given repo.
func (ct *CurationTracker) AddEvent(repo string, event CuratorEvent) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if run, ok := ct.runs[repo]; ok {
		run.Events = append(run.Events, event)
	}
}

// Complete marks the curation run for a repo as done, optionally with an error.
func (ct *CurationTracker) Complete(repo string, err error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if run, ok := ct.runs[repo]; ok {
		run.Done = true
		run.CompletedAt = time.Now().UTC()
		if err != nil {
			run.Error = err.Error()
		}
	}
}

// Active returns all curation runs that are not yet done.
func (ct *CurationTracker) Active() []*CurationRun {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	var active []*CurationRun
	for _, run := range ct.runs {
		if !run.Done {
			active = append(active, run)
		}
	}
	return active
}

// Recent returns curation runs that completed within the given duration.
func (ct *CurationTracker) Recent(within time.Duration) []*CurationRun {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	cutoff := time.Now().Add(-within)
	var recent []*CurationRun
	for _, run := range ct.runs {
		if run.Done && !run.CompletedAt.IsZero() && run.CompletedAt.After(cutoff) {
			recent = append(recent, run)
		}
	}
	return recent
}

// IsRunning returns true if there is an active (not done) curation for the given repo.
func (ct *CurationTracker) IsRunning(repo string) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if run, ok := ct.runs[repo]; ok {
		return !run.Done
	}
	return false
}
