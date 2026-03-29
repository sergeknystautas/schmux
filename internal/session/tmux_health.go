package session

import (
	"sync"
	"time"
)

const (
	// healthProbeInterval is how often we ping tmux to measure control mode RTT.
	healthProbeInterval = 5 * time.Second
	// healthProbeMaxSamples is the ring buffer capacity (1 hour at 5s interval).
	healthProbeMaxSamples = 720
	// healthProbeCommand is a lightweight no-op tmux command for RTT measurement.
	healthProbeCommand = "display-message -p ok"
	// healthProbeTimeout prevents a single slow probe from blocking the ticker.
	healthProbeTimeout = 2 * time.Second
)

// HealthProbeSample is a single RTT measurement in microseconds.
type HealthProbeSample struct {
	Timestamp time.Time `json:"ts"`
	RTTUs     float64   `json:"rtt_us"`
	Err       bool      `json:"err,omitempty"`
}

// HealthProbeStats is the aggregated view of probe data in microseconds.
type HealthProbeStats struct {
	Count   int     `json:"count"`
	P50Us   float64 `json:"p50_us"`
	P95Us   float64 `json:"p95_us"`
	P99Us   float64 `json:"p99_us"`
	MaxUs   float64 `json:"max_us"`
	AvgUs   float64 `json:"avg_us"`
	LastUs  float64 `json:"last_us"`
	Errors  int     `json:"errors"`
	UptimeS float64 `json:"uptime_s"`
}

// HealthProbeSnapshot is the raw probe data sent to the dashboard widget.
// RTT values are in microseconds. The frontend computes the histogram
// distribution adaptively, matching the TypingPerformance histogram's approach.
type HealthProbeSnapshot struct {
	Samples  []float64 `json:"samples"` // raw RTT values in microseconds (chronological)
	P50Us    float64   `json:"p50_us"`
	P99Us    float64   `json:"p99_us"`
	MaxRTTUs float64   `json:"max_rtt_us"`
	Count    int       `json:"count"`
	Errors   int       `json:"errors"`
	LastUs   float64   `json:"last_us"`
	UptimeS  float64   `json:"uptime_s"`
}

// TmuxHealthProbe measures control mode round-trip time by periodically
// sending a lightweight display-message command through the control mode connection.
type TmuxHealthProbe struct {
	mu        sync.RWMutex
	samples   []HealthProbeSample
	offset    int  // write position in ring buffer
	full      bool // whether the ring buffer has wrapped
	errors    int
	startTime time.Time
}

// NewTmuxHealthProbe creates a new probe with a pre-allocated ring buffer.
func NewTmuxHealthProbe() *TmuxHealthProbe {
	return &TmuxHealthProbe{
		samples:   make([]HealthProbeSample, healthProbeMaxSamples),
		startTime: time.Now(),
	}
}

// Record adds a probe result (in microseconds) to the ring buffer.
func (p *TmuxHealthProbe) Record(rttUs float64, err bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.samples[p.offset] = HealthProbeSample{
		Timestamp: time.Now(),
		RTTUs:     rttUs,
		Err:       err,
	}
	p.offset = (p.offset + 1) % healthProbeMaxSamples
	if p.offset == 0 {
		p.full = true
	}
	if err {
		p.errors++
	}
}

// validSamples returns the valid (non-error) RTT values in chronological order.
func (p *TmuxHealthProbe) validSamples() []float64 {
	var n int
	if p.full {
		n = healthProbeMaxSamples
	} else {
		n = p.offset
	}
	result := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		idx := i
		if p.full {
			idx = (p.offset + i) % healthProbeMaxSamples
		}
		s := p.samples[idx]
		if !s.Err {
			result = append(result, s.RTTUs)
		}
	}
	return result
}

// Stats returns aggregated probe statistics.
func (p *TmuxHealthProbe) Stats() *HealthProbeStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	vals := p.validSamples()
	if len(vals) == 0 {
		return &HealthProbeStats{
			Errors:  p.errors,
			UptimeS: time.Since(p.startTime).Seconds(),
		}
	}

	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sortFloat64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	return &HealthProbeStats{
		Count:   len(sorted),
		P50Us:   sorted[len(sorted)/2],
		P95Us:   sorted[int(float64(len(sorted))*0.95)],
		P99Us:   sorted[int(float64(len(sorted))*0.99)],
		MaxUs:   sorted[len(sorted)-1],
		AvgUs:   sum / float64(len(sorted)),
		LastUs:  vals[len(vals)-1],
		Errors:  p.errors,
		UptimeS: time.Since(p.startTime).Seconds(),
	}
}

// Snapshot returns raw probe data for the dashboard widget.
// RTT values are in microseconds. The frontend computes the histogram.
func (p *TmuxHealthProbe) Snapshot() *HealthProbeSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	vals := p.validSamples()
	if len(vals) == 0 {
		return nil
	}

	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sortFloat64s(sorted)

	return &HealthProbeSnapshot{
		Samples:  vals,
		P50Us:    sorted[len(sorted)/2],
		P99Us:    sorted[int(float64(len(sorted))*0.99)],
		MaxRTTUs: sorted[len(sorted)-1],
		Count:    len(sorted),
		Errors:   p.errors,
		LastUs:   vals[len(vals)-1],
		UptimeS:  time.Since(p.startTime).Seconds(),
	}
}

// AllSamples returns all samples for diagnostic capture.
func (p *TmuxHealthProbe) AllSamples() []HealthProbeSample {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var n int
	if p.full {
		n = healthProbeMaxSamples
	} else {
		n = p.offset
	}

	result := make([]HealthProbeSample, n)
	for i := 0; i < n; i++ {
		idx := i
		if p.full {
			idx = (p.offset + i) % healthProbeMaxSamples
		}
		result[i] = p.samples[idx]
	}
	return result
}

// sortFloat64s sorts a float64 slice in ascending order (insertion sort for small N).
func sortFloat64s(a []float64) {
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}
