//go:build !norepofeed

package repofeed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

const summariesFileName = "repofeed-summaries.json"

// SummaryEntry caches an LLM-generated intent summary for a workspace.
type SummaryEntry struct {
	Summary        string    `json:"summary"`
	PromptsHash    string    `json:"prompts_hash"`
	LastSummarized time.Time `json:"last_summarized"`
}

// SummaryCache manages per-workspace LLM intent summaries.
type SummaryCache struct {
	mu      sync.RWMutex
	entries map[string]*SummaryEntry // workspace ID -> summary
	path    string
}

// NewSummaryCache creates a new SummaryCache, loading from disk if available.
func NewSummaryCache() *SummaryCache {
	sc := &SummaryCache{
		entries: make(map[string]*SummaryEntry),
		path:    filepath.Join(schmuxdir.Get(), summariesFileName),
	}
	sc.load()
	return sc
}

// Get returns the cached summary for a workspace, or nil if not cached.
func (sc *SummaryCache) Get(workspaceID string) *SummaryEntry {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.entries[workspaceID]
}

// Set updates the cached summary for a workspace and persists to disk.
func (sc *SummaryCache) Set(workspaceID string, entry *SummaryEntry) {
	sc.mu.Lock()
	sc.entries[workspaceID] = entry
	sc.mu.Unlock()
	sc.save()
}

// AllKeys returns all workspace IDs that have cached summaries.
func (sc *SummaryCache) AllKeys() []string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	keys := make([]string, 0, len(sc.entries))
	for k := range sc.entries {
		keys = append(keys, k)
	}
	return keys
}

// Remove deletes a workspace's summary from the cache and persists.
func (sc *SummaryCache) Remove(workspaceID string) {
	sc.mu.Lock()
	delete(sc.entries, workspaceID)
	sc.mu.Unlock()
	sc.save()
}

func (sc *SummaryCache) load() {
	data, err := os.ReadFile(sc.path)
	if err != nil {
		return
	}
	var entries map[string]*SummaryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}
	sc.entries = entries
}

func (sc *SummaryCache) save() {
	sc.mu.RLock()
	data, err := json.MarshalIndent(sc.entries, "", "  ")
	sc.mu.RUnlock()
	if err != nil {
		return
	}
	os.WriteFile(sc.path, data, 0644)
}
