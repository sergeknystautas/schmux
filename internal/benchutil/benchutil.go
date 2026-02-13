// Package benchutil provides shared latency-benchmark helpers (percentile
// computation, JSON reporting) used by both the PTY-level and WebSocket-level
// benchmark suites.
package benchutil

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

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

// ComputeBenchResult calculates percentiles and GC stats from raw durations.
func ComputeBenchResult(name, variant string, durations []time.Duration, gcBefore, gcAfter *runtime.MemStats) BenchResult {
	n := len(durations)
	ms := make([]float64, n)
	var sum float64
	for i, d := range durations {
		ms[i] = float64(d.Microseconds()) / 1000.0
		sum += ms[i]
	}
	sort.Float64s(ms)

	mean := sum / float64(n)
	var variance float64
	for _, v := range ms {
		diff := v - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(n))

	gcPauses := gcAfter.NumGC - gcBefore.NumGC
	gcPauseTotal := float64(gcAfter.PauseTotalNs-gcBefore.PauseTotalNs) / 1000.0

	return BenchResult{
		Name:       name,
		Variant:    variant,
		Iterations: n,
		P50Ms:      ms[n*50/100],
		P95Ms:      ms[n*95/100],
		P99Ms:      ms[n*99/100],
		MaxMs:      ms[n-1],
		MeanMs:     mean,
		MinMs:      ms[0],
		StddevMs:   stddev,
		GCPauses:   gcPauses,
		GCPauseUs:  gcPauseTotal,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// ReportJSON logs the BenchResult as JSON and optionally writes it to
// BENCH_OUTPUT_DIR if set.
func ReportJSON(t *testing.T, result BenchResult) {
	t.Helper()

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	t.Logf("BENCH_RESULT_JSON: %s", string(data))

	if dir := os.Getenv("BENCH_OUTPUT_DIR"); dir != "" {
		filename := fmt.Sprintf("%s_%s.json", result.Name, result.Variant)
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Errorf("failed to write result to %s: %v", path, err)
		} else {
			t.Logf("Result written to %s", path)
		}
	}
}
