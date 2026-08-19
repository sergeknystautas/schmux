package workspacestatus

import (
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/github"
)

// Status is the dashboard-facing result for one workspace.
type Status struct {
	CIStatus string
	CIURL    string
	PRNumber int
	PRURL    string
}

// Entry is one workspace's cached result plus invalidation bookkeeping.
type Entry struct {
	Status       Status
	Repo         github.RepoInfo // CI query target (fork-aware)
	Branch       string
	HeadSHA      string
	Terminal     bool // Status.CIStatus is success/failure for HeadSHA
	FetchedAt    time.Time
	BackoffUntil time.Time
	Backoff      time.Duration
}

// Cache holds per-workspace CI/PR results. Written only by the build monitor
// check pass (serialized by buildMonitorCheckMu); read by the sessions
// response builder.
type Cache struct {
	mu      sync.Mutex
	entries map[string]Entry
}

func NewCache() *Cache {
	return &Cache{entries: map[string]Entry{}}
}

// Get returns the dashboard-facing status for a workspace.
func (c *Cache) Get(workspaceID string) (Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[workspaceID]
	return e.Status, ok
}

// Lookup returns the full entry for pass-side cache decisions.
func (c *Cache) Lookup(workspaceID string) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[workspaceID]
	return e, ok
}

func (c *Cache) Store(workspaceID string, e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[workspaceID] = e
}

// DropExcept removes entries for workspaces not in live, reporting whether
// anything was removed. Keeps chips from lingering after a workspace is
// disposed or becomes ineligible.
func (c *Cache) DropExcept(live map[string]bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := false
	for id := range c.entries {
		if !live[id] {
			delete(c.entries, id)
			removed = true
		}
	}
	return removed
}

// Clear removes all entries, reporting whether anything was removed. Used
// when the build monitor is disabled so chips vanish instead of going stale.
func (c *Cache) Clear() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := len(c.entries) > 0
	c.entries = map[string]Entry{}
	return removed
}
