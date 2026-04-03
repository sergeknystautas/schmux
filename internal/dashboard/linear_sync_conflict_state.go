package dashboard

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

type LinearSyncResolveConflictStep = state.ResolveConflictStep
type LinearSyncResolveConflictResolution = state.ResolveConflictResolution

// LinearSyncResolveConflictState is the full operation state, broadcast over the dashboard WebSocket.
type LinearSyncResolveConflictState struct {
	mu sync.Mutex `json:"-"`
	state.ResolveConflict
}

// AddStep appends a new step and returns its index.
func (s *LinearSyncResolveConflictState) AddStep(step LinearSyncResolveConflictStep) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if step.At == "" {
		step.At = time.Now().Format(time.RFC3339)
	}
	s.ResolveConflict.Steps = append(s.ResolveConflict.Steps, step)
	return len(s.ResolveConflict.Steps) - 1
}

// UpdateStep updates an existing step by index.
func (s *LinearSyncResolveConflictState) UpdateStep(idx int, fn func(*LinearSyncResolveConflictStep)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx >= 0 && idx < len(s.ResolveConflict.Steps) {
		fn(&s.ResolveConflict.Steps[idx])
	}
}

// UpdateLastMatchingStep finds the last in_progress step matching action (and optional localCommit)
// and updates it. Returns true if a step was updated.
func (s *LinearSyncResolveConflictState) UpdateLastMatchingStep(action, localCommit string, fn func(*LinearSyncResolveConflictStep)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.ResolveConflict.Steps) - 1; i >= 0; i-- {
		step := &s.ResolveConflict.Steps[i]
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
	s.ResolveConflict.Status = status
	s.ResolveConflict.Message = message
	s.ResolveConflict.FinishedAt = time.Now().Format(time.RFC3339)
	s.ResolveConflict.Resolutions = resolutions
}

// SetHash sets the rebased hash and its commit message if not already set.
func (s *LinearSyncResolveConflictState) SetHash(hash, hashMessage string) {
	if hash == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ResolveConflict.Hash == "" {
		s.ResolveConflict.Hash = shortHash(hash)
		s.ResolveConflict.HashMessage = hashMessage
	}
}

func (s *LinearSyncResolveConflictState) SetTmuxSession(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ResolveConflict.TmuxSession = name
}

func (s *LinearSyncResolveConflictState) ClearTmuxSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ResolveConflict.TmuxSession = ""
}

// MarshalJSON produces a thread-safe JSON snapshot.
func (s *LinearSyncResolveConflictState) MarshalJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type Alias state.ResolveConflict
	alias := Alias(s.ResolveConflict)
	return json.Marshal(alias)
}

func (s *LinearSyncResolveConflictState) Snapshot() state.ResolveConflict {
	s.mu.Lock()
	defer s.mu.Unlock()
	return state.CopyResolveConflicts([]state.ResolveConflict{s.ResolveConflict})[0]
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
