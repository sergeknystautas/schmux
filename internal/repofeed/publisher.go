//go:build !norepofeed

package repofeed

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/events"
)

// PublisherConfig configures the repofeed publisher.
type PublisherConfig struct {
	DeveloperEmail string
	DisplayName    string
	// RepoResolver maps a session ID to (repoSlug, branch). Optional.
	RepoResolver func(sessionID string) (repoSlug string, branch string)
}

// Publisher listens for session status events, tracks developer activities,
// and builds the current DeveloperFile state.
type Publisher struct {
	config PublisherConfig

	mu                sync.RWMutex
	sessionToActivity map[string]string    // session ID -> activity ID
	activities        map[string]*Activity // activity ID -> activity
	activityRepo      map[string]string    // activity ID -> repo slug
	sessionBranches   map[string]string    // session ID -> branch name

	pushMu       sync.Mutex // guards write+push to prevent concurrent pushes
	lastPushedAt time.Time  // when the user last approved a push
}

// NewPublisher creates a new Publisher.
func NewPublisher(cfg PublisherConfig) *Publisher {
	return &Publisher{
		config:            cfg,
		sessionToActivity: make(map[string]string),
		activities:        make(map[string]*Activity),
		activityRepo:      make(map[string]string),
		sessionBranches:   make(map[string]string),
	}
}

// HandleEvent implements events.EventHandler.
func (p *Publisher) HandleEvent(ctx context.Context, sessionID string, raw events.RawEvent, data []byte) {
	if raw.Type != "status" {
		return
	}

	var evt events.StatusEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	switch evt.State {
	case "working":
		if evt.Intent != "" {
			p.handleSpawn(sessionID, evt)
		}
	case "completed":
		p.handleCompleted(sessionID)
	}
}

// handleSpawn processes a new session spawn with an intent.
func (p *Publisher) handleSpawn(sessionID string, evt events.StatusEvent) {
	// Generate activity ID from intent + timestamp
	activityID := generateActivityID(evt.Intent)

	repoSlug := p.resolveRepo(sessionID)
	branch := ""
	if p.config.RepoResolver != nil {
		_, branch = p.config.RepoResolver(sessionID)
	}

	// Check if this activity already exists (same intent in same repo)
	existingID, exists := p.sessionToActivity[sessionID]
	if exists {
		// Session already tracked — this is a status update, not a new spawn
		if act, ok := p.activities[existingID]; ok {
			act.Status = StatusActive
		}
		return
	}

	// Check if we already have an activity with the same ID (same intent)
	if act, ok := p.activities[activityID]; ok {
		// Same intent already tracked — add this session to it
		act.SessionCount++
		act.Status = StatusActive
		if branch != "" {
			if !containsString(act.Branches, branch) {
				act.Branches = append(act.Branches, branch)
			}
		}
		p.sessionToActivity[sessionID] = activityID
		return
	}

	// New activity
	branches := []string{}
	if branch != "" {
		branches = []string{branch}
	}

	act := &Activity{
		ID:           activityID,
		Intent:       evt.Intent,
		Status:       StatusActive,
		Started:      time.Now().UTC().Format(time.RFC3339),
		Branches:     branches,
		SessionCount: 1,
		Agents:       []string{},
	}
	p.activities[activityID] = act
	p.activityRepo[activityID] = repoSlug
	p.sessionToActivity[sessionID] = activityID
	if branch != "" {
		p.sessionBranches[sessionID] = branch
	}
}

// handleCompleted processes a session completion.
func (p *Publisher) handleCompleted(sessionID string) {
	activityID, ok := p.sessionToActivity[sessionID]
	if !ok {
		return
	}
	delete(p.sessionToActivity, sessionID)
	delete(p.sessionBranches, sessionID)

	act, ok := p.activities[activityID]
	if !ok {
		return
	}

	act.SessionCount--
	if act.SessionCount <= 0 {
		act.SessionCount = 0
		act.Status = StatusCompleted
	}
}

// GetCurrentState builds the current DeveloperFile from tracked activities.
func (p *Publisher) GetCurrentState() *DeveloperFile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	repos := make(map[string]*RepoActivities)
	for actID, act := range p.activities {
		repoSlug := p.activityRepo[actID]
		if _, ok := repos[repoSlug]; !ok {
			repos[repoSlug] = &RepoActivities{}
		}
		repos[repoSlug].Activities = append(repos[repoSlug].Activities, *act)
	}

	return &DeveloperFile{
		Developer:   p.config.DeveloperEmail,
		DisplayName: p.config.DisplayName,
		Updated:     time.Now().UTC().Format(time.RFC3339),
		Repos:       repos,
	}
}

// resolveRepo maps a session ID to a repo slug.
func (p *Publisher) resolveRepo(sessionID string) string {
	if p.config.RepoResolver != nil {
		slug, _ := p.config.RepoResolver(sessionID)
		if slug != "" {
			return slug
		}
	}
	// Fallback: extract workspace ID prefix from session ID
	// Session IDs are typically "ws-<id>-session-<uuid>"
	parts := strings.SplitN(sessionID, "-session-", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}

// generateActivityID creates a deterministic ID from the intent.
func generateActivityID(intent string) string {
	h := sha256.Sum256([]byte(intent))
	return fmt.Sprintf("%x", h[:6])
}

// GetLastPushedAt returns when the user last approved a push.
func (p *Publisher) GetLastPushedAt() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastPushedAt
}

// SetLastPushedAt records the time of a successful push.
func (p *Publisher) SetLastPushedAt(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastPushedAt = t
}

// LockForPush acquires the push mutex to prevent concurrent write+push operations.
// Returns an unlock function on success, or nil if a push is already in flight.
func (p *Publisher) LockForPush() func() {
	if !p.pushMu.TryLock() {
		return nil
	}
	return p.pushMu.Unlock
}

// containsString checks if a slice contains a string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
