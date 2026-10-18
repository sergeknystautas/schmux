package flights

import (
	"context"
	"errors"
	"time"
)

// renameThreshold is the minimum fraction of rune-level Levenshtein distance
// (relative to the longer of the two strings) at which a flight's name is
// considered stale and needs to be regenerated.
const renameThreshold = 0.05

// namingTick is how often the background namer sweeps every flight.
const namingTick = time.Minute

// maxNamingBackoff caps the per-flight retry wait after consecutive failures.
const maxNamingBackoff = time.Hour

// ErrNamingOff is returned by a Namer when naming is disabled (no target set).
// The manager treats it as "skip this pass" — no retry, no error persisted.
var ErrNamingOff = errors.New("flight naming is off")

// errStaleName is an internal sentinel; when an LLM result is discarded because
// the flight's prompt changed mid-call, editFlight returns it without persisting.
var errStaleName = errors.New("stale flight name")

// Namer generates a flight's display name from its prompt. It must be safe to
// call from any goroutine and never block flight operations.
type Namer interface {
	Name(ctx context.Context, prompt string) (string, error)
}

// namingRetry records the next attempt time and the prompt snapshot it applies
// to. If the prompt at the time of the next pass matches, the retry applies;
// otherwise it is dropped (the prompt has changed and the backoff resets).
type namingRetry struct {
	prompt   string
	failures int
	next     time.Time
}

// promptChange returns the rune-Levenshtein distance between a and b divided
// by the rune length of the longer string. Identical strings return 0;
// disjoint strings return 1. Uses O(min(len(a),len(b))) extra memory.
func promptChange(a, b string) float64 {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 && len(rb) == 0 {
		return 0
	}
	// Ensure ra is the shorter row (so the algorithm is O(len(ra)) memory).
	if len(ra) > len(rb) {
		ra, rb = rb, ra
	}
	prev := make([]int, len(ra)+1)
	curr := make([]int, len(ra)+1)
	for i := 0; i <= len(ra); i++ {
		prev[i] = i
	}
	for j := 1; j <= len(rb); j++ {
		curr[0] = j
		for i := 1; i <= len(ra); i++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[i] + 1
			ins := curr[i-1] + 1
			sub := prev[i-1] + cost
			v := del
			if ins < v {
				v = ins
			}
			if sub < v {
				v = sub
			}
			curr[i] = v
		}
		prev, curr = curr, prev
	}
	distance := prev[len(ra)]
	denom := len(rb)
	if len(ra) > denom {
		denom = len(ra)
	}
	return float64(distance) / float64(denom)
}

// needsName reports whether f's name is missing or stale: a placeholder
// (NamePrompt empty) or drifted at or beyond renameThreshold.
func needsName(f Flight) bool {
	if f.NamePrompt == "" {
		return true
	}
	return promptChange(f.NamePrompt, f.Prompt) >= renameThreshold
}

// nextBackoff returns the wait time after `failures` consecutive failures.
// Doubles up to maxNamingBackoff. failures==0 returns 0.
func nextBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	d := namingTick
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= maxNamingBackoff {
			return maxNamingBackoff
		}
	}
	if d > maxNamingBackoff {
		return maxNamingBackoff
	}
	return d
}

// kickNaming schedules a background pass. Non-blocking: if a kick is already
// pending, the buffer absorbs the burst.
func (m *Manager) kickNaming() {
	if m.deps.Namer == nil {
		return
	}
	select {
	case m.nameKick <- struct{}{}:
	default:
	}
}

// StartNaming runs the background naming worker until ctx is canceled. It is
// safe to call once per daemon lifetime. When Namer is nil, the worker is a
// no-op (every kick is dropped).
func (m *Manager) StartNaming(ctx context.Context) {
	if m.deps.Namer == nil {
		return
	}
	go m.runNamingWorker(ctx)
}

func (m *Manager) runNamingWorker(ctx context.Context) {
	// First pass runs immediately.
	_ = m.runNamingPass(ctx)
	ticker := time.NewTicker(namingTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.nameKick:
		case <-ticker.C:
		}
		_ = m.runNamingPass(ctx)
	}
}

// runNamingPass walks every flight once, naming those that need it.
// Synchronous so tests can drive it directly.
func (m *Manager) runNamingPass(ctx context.Context) error {
	if m.deps.Namer == nil {
		return nil
	}
	flights := m.List()
	now := m.deps.Now()

	// Drop retry records for flights that no longer exist.
	live := make(map[string]bool, len(flights))
	for _, f := range flights {
		live[f.ID] = true
	}
	for id := range m.nameRetry {
		if !live[id] {
			delete(m.nameRetry, id)
		}
	}

	for _, f := range flights {
		if !needsName(f) {
			continue
		}
		r := m.nameRetry[f.ID]
		if r.prompt != f.Prompt {
			// The prompt changed since the last failure: start the backoff over.
			r = namingRetry{prompt: f.Prompt}
		}
		if now.Before(r.next) {
			continue
		}

		start := m.deps.Now()
		name, err := m.deps.Namer.Name(ctx, f.Prompt)
		dur := m.deps.Now().Sub(start)

		switch {
		case errors.Is(err, ErrNamingOff):
			// Same answer for every flight: stop the pass and keep no backoff.
			m.logger.Debug("flight naming off")
			clear(m.nameRetry)
			return nil
		case err != nil:
			m.logger.Error("flight name failed", "flight", f.ID, "duration", dur, "err", err)
			r.failures++
			r.next = now.Add(nextBackoff(r.failures))
			m.nameRetry[f.ID] = r
			msg := err.Error()
			if f.NameError != msg {
				m.applyNaming(f.ID, f.Prompt, func(g *Flight) { g.NameError = msg })
			}
		default:
			m.logger.Info("flight named", "flight", f.ID, "duration", dur)
			delete(m.nameRetry, f.ID)
			m.applyNaming(f.ID, f.Prompt, func(g *Flight) {
				g.ShortName = name
				g.NamePrompt = f.Prompt
				g.NameError = ""
			})
		}
	}
	return nil
}

// applyNaming writes fn to the flight only while its prompt still equals
// prompt. A changed prompt or a deleted flight drops the result.
func (m *Manager) applyNaming(id, prompt string, fn func(f *Flight)) {
	err := m.editFlight(id, func(f *Flight) error {
		if f.Prompt != prompt {
			return errStaleName
		}
		fn(f)
		return nil
	})
	if err != nil && !errors.Is(err, errStaleName) && !errors.Is(err, ErrNotFound) {
		m.logger.Error("flight name not saved", "flight", id, "err", err)
	}
}
