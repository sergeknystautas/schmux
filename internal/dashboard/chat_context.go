package dashboard

import "sync"

// chatContextUsage holds each chat session's latest context-window usage as its
// harness reported it. Memory only: a daemon restart starts empty, and the
// sidebar shows the maximum alone until the session's next model call.
type chatContextUsage struct {
	mu        sync.Mutex
	bySession map[string]int
}

// set records a session's context tokens and reports whether the value changed.
func (c *chatContextUsage) set(sessionID string, tokens int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bySession[sessionID] == tokens {
		return false
	}
	if c.bySession == nil {
		c.bySession = make(map[string]int)
	}
	c.bySession[sessionID] = tokens
	return true
}

// get returns a session's context tokens, or 0 when none were reported.
func (c *chatContextUsage) get(sessionID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bySession[sessionID]
}
