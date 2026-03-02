package actions

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// Registry manages actions for a single repo, backed by a JSON file.
type Registry struct {
	mu             sync.RWMutex
	actions        []contracts.Action
	baseDir        string // e.g. ~/.schmux/actions
	repo           string
	lastDecayCheck time.Time
}

// NewRegistry creates a new Registry for the given repo.
func NewRegistry(baseDir, repo string) *Registry {
	return &Registry{
		baseDir: baseDir,
		repo:    repo,
	}
}

// filePath returns the path to the registry JSON file.
func (r *Registry) filePath() string {
	return filepath.Join(r.baseDir, r.repo, "registry.json")
}

// Load reads the registry from disk. If the file doesn't exist, starts empty.
// Applies decay to stale actions on load.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			r.actions = nil
			r.lastDecayCheck = time.Now()
			return nil
		}
		return fmt.Errorf("read registry: %w", err)
	}

	var actions []contracts.Action
	if err := json.Unmarshal(data, &actions); err != nil {
		return fmt.Errorf("parse registry: %w", err)
	}
	r.actions = actions
	r.applyDecay(time.Now())
	r.lastDecayCheck = time.Now()
	return nil
}

// List returns actions filtered by state.
func (r *Registry) List(state contracts.ActionState) []contracts.Action {
	r.maybeApplyDecay()

	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []contracts.Action
	for _, a := range r.actions {
		if a.State == state {
			result = append(result, a)
		}
	}
	return result
}

// Get returns a single action by ID.
func (r *Registry) Get(id string) (contracts.Action, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, a := range r.actions {
		if a.ID == id {
			return a, true
		}
	}
	return contracts.Action{}, false
}

// Create adds a new manual action (pinned immediately).
func (r *Registry) Create(req contracts.CreateActionRequest) (contracts.Action, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	action := contracts.Action{
		ID:         r.generateID(),
		Name:       req.Name,
		Type:       req.Type,
		Scope:      "repo",
		Template:   req.Template,
		Parameters: req.Parameters,
		Target:     req.Target,
		Persona:    req.Persona,
		Command:    req.Command,
		Source:     contracts.ActionSourceManual,
		Confidence: 1.0,
		State:      contracts.ActionStatePinned,
		FirstSeen:  now,
		PinnedAt:   &now,
	}
	r.actions = append(r.actions, action)
	if err := r.save(); err != nil {
		// Roll back on save failure.
		r.actions = r.actions[:len(r.actions)-1]
		return contracts.Action{}, err
	}
	return action, nil
}

// Pin transitions an action from proposed to pinned.
func (r *Registry) Pin(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.actions {
		if r.actions[i].ID == id {
			if r.actions[i].State != contracts.ActionStateProposed {
				return fmt.Errorf("action %s is %s, not proposed", id, r.actions[i].State)
			}
			now := time.Now()
			r.actions[i].State = contracts.ActionStatePinned
			r.actions[i].PinnedAt = &now
			return r.save()
		}
	}
	return fmt.Errorf("action %s not found", id)
}

// Dismiss transitions an action to dismissed.
func (r *Registry) Dismiss(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.actions {
		if r.actions[i].ID == id {
			r.actions[i].State = contracts.ActionStateDismissed
			return r.save()
		}
	}
	return fmt.Errorf("action %s not found", id)
}

// Update modifies an action's fields.
func (r *Registry) Update(id string, req contracts.UpdateActionRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.actions {
		if r.actions[i].ID == id {
			if req.Name != nil {
				r.actions[i].Name = *req.Name
			}
			if req.Template != nil {
				r.actions[i].Template = *req.Template
			}
			if req.Parameters != nil {
				r.actions[i].Parameters = *req.Parameters
			}
			if req.Target != nil {
				r.actions[i].Target = *req.Target
			}
			if req.Persona != nil {
				r.actions[i].Persona = *req.Persona
			}
			if req.Command != nil {
				r.actions[i].Command = *req.Command
			}
			return r.save()
		}
	}
	return fmt.Errorf("action %s not found", id)
}

// Delete removes an action from the registry.
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.actions {
		if r.actions[i].ID == id {
			r.actions = append(r.actions[:i], r.actions[i+1:]...)
			return r.save()
		}
	}
	return fmt.Errorf("action %s not found", id)
}

// RecordUse increments use_count (and edit_count if edited), updates last_used,
// and boosts confidence.
func (r *Registry) RecordUse(id string, edited bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.actions {
		if r.actions[i].ID == id {
			now := time.Now()
			r.actions[i].UseCount++
			r.actions[i].LastUsed = &now
			if edited {
				r.actions[i].EditCount++
			}
			// Boost confidence on use, capped at 1.0.
			newConf := r.actions[i].Confidence + 0.1
			if newConf > 1.0 {
				newConf = 1.0
			}
			r.actions[i].Confidence = newConf
			return r.save()
		}
	}
	return fmt.Errorf("action %s not found", id)
}

// AddProposed inserts curator-proposed actions into the registry.
func (r *Registry) AddProposed(actions []contracts.Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for i := range actions {
		actions[i].ID = r.generateID()
		actions[i].State = contracts.ActionStateProposed
		actions[i].Source = contracts.ActionSourceEmerged
		actions[i].ProposedAt = &now
		if actions[i].FirstSeen.IsZero() {
			actions[i].FirstSeen = now
		}
	}
	r.actions = append(r.actions, actions...)
	return r.save()
}

// MigrateQuickLaunch converts QuickLaunch presets to pinned actions.
// This is idempotent — if any migrated-source action exists, it's a no-op.
func (r *Registry) MigrateQuickLaunch(presets []contracts.QuickLaunch) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Skip if already migrated.
	for _, a := range r.actions {
		if a.Source == contracts.ActionSourceMigrated {
			return 0, nil
		}
	}

	now := time.Now()
	count := 0
	for _, p := range presets {
		action := contracts.Action{
			ID:         r.generateID(),
			Name:       p.Name,
			Scope:      "repo",
			Source:     contracts.ActionSourceMigrated,
			Confidence: 1.0,
			State:      contracts.ActionStatePinned,
			FirstSeen:  now,
			PinnedAt:   &now,
		}
		if p.Command != "" {
			action.Type = contracts.ActionTypeCommand
			action.Command = p.Command
		} else {
			action.Type = contracts.ActionTypeAgent
			action.Target = p.Target
			if p.Prompt != nil {
				action.Template = *p.Prompt
			}
		}
		r.actions = append(r.actions, action)
		count++
	}
	return count, r.save()
}

// save writes the registry to disk atomically (temp-file + rename).
// Caller must hold the write lock.
func (r *Registry) save() error {
	dir := filepath.Dir(r.filePath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}

	data, err := json.MarshalIndent(r.actions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	return os.Rename(tmpPath, r.filePath())
}

// maybeApplyDecay checks if enough time has passed since the last decay check
// and, if so, applies decay. Safe to call without holding any lock.
func (r *Registry) maybeApplyDecay() {
	r.mu.RLock()
	needsDecay := time.Since(r.lastDecayCheck) > 1*time.Hour
	r.mu.RUnlock()

	if !needsDecay {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock.
	if time.Since(r.lastDecayCheck) <= 1*time.Hour {
		return
	}
	r.applyDecay(time.Now())
	r.lastDecayCheck = time.Now()
}

// applyDecay reduces confidence for stale pinned actions.
// For each pinned action, if last_used (or pinned_at if never used) is older
// than 30 days, reduce confidence by 0.1 per 30-day period. Auto-dismiss below 0.3.
// Caller must hold the write lock.
func (r *Registry) applyDecay(now time.Time) {
	changed := false
	for i := range r.actions {
		a := &r.actions[i]
		if a.State != contracts.ActionStatePinned {
			continue
		}

		// Determine the reference time for staleness.
		var ref time.Time
		if a.LastUsed != nil {
			ref = *a.LastUsed
		} else if a.PinnedAt != nil {
			ref = *a.PinnedAt
		} else {
			ref = a.FirstSeen
		}

		elapsed := now.Sub(ref)
		periods := int(elapsed.Hours() / (24 * 30))
		if periods <= 0 {
			continue
		}

		decay := float64(periods) * 0.1
		newConf := a.Confidence - decay
		if newConf < 0 {
			newConf = 0
		}
		if newConf != a.Confidence {
			a.Confidence = newConf
			changed = true
		}
		if a.Confidence < 0.3 {
			a.State = contracts.ActionStateDismissed
			changed = true
		}
	}

	if changed {
		// Best-effort save; decay is re-applied on next load anyway.
		r.save()
	}
}

// generateID returns a unique action ID like "act-a1b2c3d4".
func (r *Registry) generateID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use timestamp-based ID.
		return fmt.Sprintf("act-%x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return "act-" + hex.EncodeToString(b)
}

// validateRepoName rejects repo names with path traversal.
func validateRepoName(repo string) error {
	if strings.Contains(repo, "..") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "\\") {
		return fmt.Errorf("invalid repo name: %s", repo)
	}
	return nil
}
