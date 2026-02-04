package dashboard

import (
	"encoding/json"
	"sync"
	"time"
)

// LinearSyncResolveConflictStep represents a single step in the conflict resolution process.
type LinearSyncResolveConflictStep struct {
	Action             string   `json:"action"`
	Status             string   `json:"status"` // "in_progress", "done", "failed"
	Message            string   `json:"message"`
	At                 string   `json:"at"`
	LocalCommit        string   `json:"local_commit,omitempty"`
	LocalCommitMessage string   `json:"local_commit_message,omitempty"`
	Files              []string `json:"files,omitempty"`
	Confidence         string   `json:"confidence,omitempty"`
	Summary            string   `json:"summary,omitempty"`
	Created            *bool    `json:"created,omitempty"` // for wip_commit step
}

// LinearSyncResolveConflictResolution is the per-conflict summary included in the final state.
type LinearSyncResolveConflictResolution struct {
	LocalCommit        string   `json:"local_commit"`
	LocalCommitMessage string   `json:"local_commit_message"`
	AllResolved        bool     `json:"all_resolved"`
	Confidence         string   `json:"confidence"`
	Summary            string   `json:"summary"`
	Files              []string `json:"files"`
}

// LinearSyncResolveConflictState is the full operation state, broadcast over the dashboard WebSocket.
type LinearSyncResolveConflictState struct {
	mu          sync.Mutex                            `json:"-"`
	Type        string                                `json:"type"` // always "linear_sync_resolve_conflict"
	WorkspaceID string                                `json:"workspace_id"`
	Status      string                                `json:"status"` // "in_progress", "done", "failed"
	Hash        string                                `json:"hash,omitempty"`
	StartedAt   string                                `json:"started_at"`
	FinishedAt  string                                `json:"finished_at,omitempty"`
	Message     string                                `json:"message,omitempty"`
	Steps       []LinearSyncResolveConflictStep       `json:"steps"`
	Resolutions []LinearSyncResolveConflictResolution `json:"resolutions,omitempty"`
}

// AddStep appends a new step and returns its index.
func (s *LinearSyncResolveConflictState) AddStep(step LinearSyncResolveConflictStep) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if step.At == "" {
		step.At = time.Now().Format(time.RFC3339)
	}
	s.Steps = append(s.Steps, step)
	return len(s.Steps) - 1
}

// UpdateStep updates an existing step by index.
func (s *LinearSyncResolveConflictState) UpdateStep(idx int, fn func(*LinearSyncResolveConflictStep)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx >= 0 && idx < len(s.Steps) {
		fn(&s.Steps[idx])
	}
}

// UpdateLastMatchingStep finds the last in_progress step matching action (and optional localCommit)
// and updates it. Returns true if a step was updated.
func (s *LinearSyncResolveConflictState) UpdateLastMatchingStep(action, localCommit string, fn func(*LinearSyncResolveConflictStep)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.Steps) - 1; i >= 0; i-- {
		step := &s.Steps[i]
		if step.Status != "in_progress" || step.Action != action {
			continue
		}
		if localCommit != "" && step.LocalCommit != localCommit {
			continue
		}
		fn(step)
		return true
	}
	return false
}

// Finish sets the final status and message.
func (s *LinearSyncResolveConflictState) Finish(status, message string, resolutions []LinearSyncResolveConflictResolution) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	s.Message = message
	s.FinishedAt = time.Now().Format(time.RFC3339)
	s.Resolutions = resolutions
}

// SetHash sets the rebased hash if it hasn't been set yet.
func (s *LinearSyncResolveConflictState) SetHash(hash string) {
	if hash == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Hash == "" {
		s.Hash = hash
	}
}

// MarshalJSON produces a thread-safe JSON snapshot.
func (s *LinearSyncResolveConflictState) MarshalJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type Alias LinearSyncResolveConflictState
	return json.Marshal((*Alias)(s))
}

// linearSyncResolveConflictStates manages the in-memory state map on the Server.
// These methods are called from handlers and the broadcast loop.

func (s *Server) getLinearSyncResolveConflictState(workspaceID string) *LinearSyncResolveConflictState {
	s.linearSyncResolveConflictStatesMu.RLock()
	defer s.linearSyncResolveConflictStatesMu.RUnlock()
	return s.linearSyncResolveConflictStates[workspaceID]
}

func (s *Server) setLinearSyncResolveConflictState(workspaceID string, state *LinearSyncResolveConflictState) {
	s.linearSyncResolveConflictStatesMu.Lock()
	defer s.linearSyncResolveConflictStatesMu.Unlock()
	s.linearSyncResolveConflictStates[workspaceID] = state
}

func (s *Server) deleteLinearSyncResolveConflictState(workspaceID string) {
	s.linearSyncResolveConflictStatesMu.Lock()
	defer s.linearSyncResolveConflictStatesMu.Unlock()
	delete(s.linearSyncResolveConflictStates, workspaceID)
}

func (s *Server) getAllLinearSyncResolveConflictStates() []*LinearSyncResolveConflictState {
	s.linearSyncResolveConflictStatesMu.RLock()
	defer s.linearSyncResolveConflictStatesMu.RUnlock()
	result := make([]*LinearSyncResolveConflictState, 0, len(s.linearSyncResolveConflictStates))
	for _, state := range s.linearSyncResolveConflictStates {
		result = append(result, state)
	}
	return result
}
