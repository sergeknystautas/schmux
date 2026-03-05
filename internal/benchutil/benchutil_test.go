package benchutil

import (
	"runtime"
	"testing"
	"time"
)

func TestComputeBenchResult(t *testing.T) {
	gcStats := &runtime.MemStats{NumGC: 5, PauseTotalNs: 1000}
	gcAfter := &runtime.MemStats{NumGC: 7, PauseTotalNs: 3000}

	durations := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
	}

	result := ComputeBenchResult("test-bench", "variant-a", durations, gcStats, gcAfter)

	if result.Name != "test-bench" {
		t.Errorf("Name = %q, want %q", result.Name, "test-bench")
	}
	if result.Variant != "variant-a" {
		t.Errorf("Variant = %q, want %q", result.Variant, "variant-a")
	}
	if result.Iterations != 5 {
		t.Errorf("Iterations = %d, want 5", result.Iterations)
	}
	if result.MinMs != 10.0 {
		t.Errorf("MinMs = %f, want 10.0", result.MinMs)
	}
	if result.MaxMs != 50.0 {
		t.Errorf("MaxMs = %f, want 50.0", result.MaxMs)
	}
	if result.MeanMs != 30.0 {
		t.Errorf("MeanMs = %f, want 30.0", result.MeanMs)
	}
	// P50 of [10,20,30,40,50] at index 5*50/100=2 → 30ms
	if result.P50Ms != 30.0 {
		t.Errorf("P50Ms = %f, want 30.0", result.P50Ms)
	}
	if result.GCPauses != 2 {
		t.Errorf("GCPauses = %d, want 2", result.GCPauses)
	}
	if result.GCPauseUs != 2.0 { // (3000 - 1000) / 1000 = 2.0
		t.Errorf("GCPauseUs = %f, want 2.0", result.GCPauseUs)
	}
	if result.StddevMs <= 0 {
		t.Errorf("StddevMs = %f, want > 0", result.StddevMs)
	}
	if result.Timestamp == "" {
		t.Error("Timestamp should be non-empty")
	}
}

func TestComputeBenchResult_EmptyDurations(t *testing.T) {
	gc := &runtime.MemStats{}
	result := ComputeBenchResult("ignored", "ignored", nil, gc, gc)

	// Empty durations returns zero-value BenchResult (all fields are zero/empty)
	if result.Iterations != 0 {
		t.Errorf("Iterations = %d, want 0", result.Iterations)
	}
	if result.Name != "" {
		t.Errorf("Name = %q, want empty (zero-value struct returned for empty durations)", result.Name)
	}
	if result.Variant != "" {
		t.Errorf("Variant = %q, want empty (zero-value struct returned for empty durations)", result.Variant)
	}
	if result.MinMs != 0 || result.MaxMs != 0 || result.MeanMs != 0 {
		t.Errorf("statistical fields should be zero, got Min=%f Max=%f Mean=%f", result.MinMs, result.MaxMs, result.MeanMs)
	}
}

func TestComputeBenchResult_SingleDuration(t *testing.T) {
	gc := &runtime.MemStats{}
	durations := []time.Duration{5 * time.Millisecond}

	result := ComputeBenchResult("single", "v", durations, gc, gc)
	if result.Iterations != 1 {
		t.Errorf("Iterations = %d, want 1", result.Iterations)
	}
	if result.MinMs != result.MaxMs {
		t.Errorf("MinMs (%f) should equal MaxMs (%f) for single duration", result.MinMs, result.MaxMs)
	}
	if result.P50Ms != result.MinMs {
		t.Errorf("P50Ms (%f) should equal MinMs (%f) for single duration", result.P50Ms, result.MinMs)
	}
	// Stddev of single element should be 0 (n-1 = 0, guarded by if n > 1)
	if result.StddevMs != 0 {
		t.Errorf("StddevMs = %f, want 0 for single duration", result.StddevMs)
	}
}
