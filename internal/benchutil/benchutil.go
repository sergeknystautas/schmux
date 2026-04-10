// Package benchutil provides shared latency-benchmark helpers (percentile
// computation, JSON reporting) used by both the PTY-level and WebSocket-level
// benchmark suites.
package benchutil

// BenchResult holds latency percentile data in a format shared with the
// Playwright benchmark spec so both can be compared with the same tooling.
type BenchResult struct {
	Name       string  `json:"name"`
	Variant    string  `json:"variant"`
	Iterations int     `json:"iterations"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	P99Ms      float64 `json:"p99_ms"`
	MaxMs      float64 `json:"max_ms"`
	MeanMs     float64 `json:"mean_ms"`
	MinMs      float64 `json:"min_ms"`
	StddevMs   float64 `json:"stddev_ms"`
	GCPauses   uint32  `json:"gc_pauses"`
	GCPauseUs  float64 `json:"gc_pause_total_us"`
	Timestamp  string  `json:"timestamp"`
}
