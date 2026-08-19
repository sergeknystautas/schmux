// Package sysstat reads host-level system statistics for the debug UI.
package sysstat

// LoadAvg holds the host's 1, 5, and 15 minute load averages.
type LoadAvg struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

// LoadProbe reads the host's load average. It is passive: no goroutine,
// no state — the caller owns the sampling ticker (same ownership pattern
// as session.TmuxHealthProbe).
type LoadProbe struct{}

// NewLoadProbe returns a ready-to-use probe.
func NewLoadProbe() *LoadProbe { return &LoadProbe{} }

// Read returns the current load average.
func (p *LoadProbe) Read() (LoadAvg, error) { return readLoadAvg() }
