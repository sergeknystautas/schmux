package compound

import (
	"testing"
	"time"
)

func TestSweepExpiredSuppressions_RemovesExpired(t *testing.T) {
	w := &Watcher{
		suppressed: map[string]time.Time{
			"ws-1:config.yaml": time.Now().Add(-10 * time.Second), // expired
			"ws-2:main.go":     time.Now().Add(10 * time.Second),  // still active
			"ws-1:readme.md":   time.Now().Add(-1 * time.Second),  // expired
		},
	}

	w.sweepExpiredSuppressions()

	if len(w.suppressed) != 1 {
		t.Fatalf("expected 1 remaining suppression, got %d", len(w.suppressed))
	}
	if _, ok := w.suppressed["ws-2:main.go"]; !ok {
		t.Error("active suppression ws-2:main.go was incorrectly removed")
	}
}

func TestSweepExpiredSuppressions_EmptyMap(t *testing.T) {
	w := &Watcher{
		suppressed: make(map[string]time.Time),
	}
	// Should not panic
	w.sweepExpiredSuppressions()

	if len(w.suppressed) != 0 {
		t.Errorf("expected empty map, got %d entries", len(w.suppressed))
	}
}

func TestSweepExpiredSuppressions_AllExpired(t *testing.T) {
	w := &Watcher{
		suppressed: map[string]time.Time{
			"ws-1:a.go": time.Now().Add(-5 * time.Second),
			"ws-1:b.go": time.Now().Add(-3 * time.Second),
			"ws-2:c.go": time.Now().Add(-1 * time.Second),
		},
	}

	w.sweepExpiredSuppressions()

	if len(w.suppressed) != 0 {
		t.Errorf("expected all suppressions removed, got %d", len(w.suppressed))
	}
}

func TestSweepExpiredSuppressions_NoneExpired(t *testing.T) {
	w := &Watcher{
		suppressed: map[string]time.Time{
			"ws-1:a.go": time.Now().Add(30 * time.Second),
			"ws-2:b.go": time.Now().Add(60 * time.Second),
		},
	}

	w.sweepExpiredSuppressions()

	if len(w.suppressed) != 2 {
		t.Errorf("expected 2 suppressions preserved, got %d", len(w.suppressed))
	}
}

func TestSuppress_AddsEntryWithTTL(t *testing.T) {
	w := &Watcher{
		suppressed:     make(map[string]time.Time),
		suppressionTTL: 5 * time.Second,
	}

	before := time.Now()
	w.Suppress("ws-1", "config.yaml")
	after := time.Now()

	expiry, ok := w.suppressed["ws-1:config.yaml"]
	if !ok {
		t.Fatal("Suppress() did not add entry to suppressed map")
	}

	// TTL is 5 seconds. Expiry should be between (before + 5s) and (after + 5s).
	expectedLow := before.Add(5 * time.Second)
	expectedHigh := after.Add(5 * time.Second)
	if expiry.Before(expectedLow) || expiry.After(expectedHigh) {
		t.Errorf("expiry %v not in expected range [%v, %v]", expiry, expectedLow, expectedHigh)
	}
}

func TestSuppress_OverwritesPreviousEntry(t *testing.T) {
	w := &Watcher{
		suppressed: map[string]time.Time{
			"ws-1:config.yaml": time.Now().Add(-2 * time.Second), // near expiry
		},
		suppressionTTL: 5 * time.Second,
	}

	w.Suppress("ws-1", "config.yaml")

	expiry := w.suppressed["ws-1:config.yaml"]
	if time.Until(expiry) < 4*time.Second {
		t.Error("Suppress() did not refresh the TTL for an existing entry")
	}
}
