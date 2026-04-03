package dashboard

import (
	"testing"
	"time"
)

func TestLatencyCollector_EmptyReturnsNil(t *testing.T) {
	lc := NewLatencyCollector()
	if lc.Percentiles() != nil {
		t.Fatal("expected nil percentiles for empty collector")
	}
	if lc.Len() != 0 {
		t.Fatalf("expected Len()=0, got %d", lc.Len())
	}
}

func TestLatencyCollector_SingleSample(t *testing.T) {
	lc := NewLatencyCollector()
	lc.Add(LatencySample{
		Dispatch:     1 * time.Millisecond,
		SendKeys:     2 * time.Millisecond,
		Echo:         3 * time.Millisecond,
		FrameSend:    4 * time.Millisecond,
		MutexWait:    5 * time.Millisecond,
		ExecuteNet:   6 * time.Millisecond,
		ExecuteCount: 7,
	})

	p := lc.Percentiles()
	if p == nil {
		t.Fatal("expected non-nil percentiles")
	}
	if p.SampleCount != 1 {
		t.Errorf("SampleCount = %d, want 1", p.SampleCount)
	}
	// With one sample, P50 and P99 should be the same value
	if p.DispatchP50 != 1.0 || p.DispatchP99 != 1.0 {
		t.Errorf("dispatch P50=%.2f P99=%.2f, want 1.0/1.0", p.DispatchP50, p.DispatchP99)
	}
	if p.SendKeysP50 != 2.0 || p.SendKeysP99 != 2.0 {
		t.Errorf("sendKeys P50=%.2f P99=%.2f, want 2.0/2.0", p.SendKeysP50, p.SendKeysP99)
	}
	if p.EchoP50 != 3.0 || p.EchoP99 != 3.0 {
		t.Errorf("echo P50=%.2f P99=%.2f, want 3.0/3.0", p.EchoP50, p.EchoP99)
	}
	if p.FrameSendP50 != 4.0 || p.FrameSendP99 != 4.0 {
		t.Errorf("frameSend P50=%.2f P99=%.2f, want 4.0/4.0", p.FrameSendP50, p.FrameSendP99)
	}
	if p.MutexWaitP50 != 5.0 || p.MutexWaitP99 != 5.0 {
		t.Errorf("mutexWait P50=%.2f P99=%.2f, want 5.0/5.0", p.MutexWaitP50, p.MutexWaitP99)
	}
	if p.ExecuteNetP50 != 6.0 || p.ExecuteNetP99 != 6.0 {
		t.Errorf("executeNet P50=%.2f P99=%.2f, want 6.0/6.0", p.ExecuteNetP50, p.ExecuteNetP99)
	}
	if p.ExecuteCountP50 != 7.0 || p.ExecuteCountP99 != 7.0 {
		t.Errorf("executeCount P50=%.2f P99=%.2f, want 7.0/7.0", p.ExecuteCountP50, p.ExecuteCountP99)
	}
}

func TestLatencyCollector_KnownDistribution(t *testing.T) {
	lc := NewLatencyCollector()

	// Add 100 samples: dispatch = 1ms..100ms
	for i := 1; i <= 100; i++ {
		lc.Add(LatencySample{
			Dispatch:     time.Duration(i) * time.Millisecond,
			SendKeys:     time.Duration(i*2) * time.Millisecond,
			Echo:         time.Duration(i*3) * time.Millisecond,
			FrameSend:    time.Duration(i*4) * time.Millisecond,
			MutexWait:    time.Duration(i*5) * time.Millisecond,
			ExecuteNet:   time.Duration(i*6) * time.Millisecond,
			ExecuteCount: i * 7,
		})
	}

	p := lc.Percentiles()
	if p == nil {
		t.Fatal("expected non-nil percentiles")
	}
	if p.SampleCount != 100 {
		t.Errorf("SampleCount = %d, want 100", p.SampleCount)
	}

	// For 100 samples sorted 1..100:
	// P50 = index floor(99*0.50)=49 → value 50
	// P99 = index floor(99*0.99)=98 → value 99
	if p.DispatchP50 != 50.0 {
		t.Errorf("DispatchP50 = %.2f, want 50.0", p.DispatchP50)
	}
	if p.DispatchP99 != 99.0 {
		t.Errorf("DispatchP99 = %.2f, want 99.0", p.DispatchP99)
	}
	if p.SendKeysP50 != 100.0 {
		t.Errorf("SendKeysP50 = %.2f, want 100.0", p.SendKeysP50)
	}
	// MutexWait: i*5 → 5..500, P50 = 250, P99 = 495
	if p.MutexWaitP50 != 250.0 {
		t.Errorf("MutexWaitP50 = %.2f, want 250.0", p.MutexWaitP50)
	}
	if p.MutexWaitP99 != 495.0 {
		t.Errorf("MutexWaitP99 = %.2f, want 495.0", p.MutexWaitP99)
	}
	// ExecuteNet: i*6 → 6..600, P50 = 300, P99 = 594
	if p.ExecuteNetP50 != 300.0 {
		t.Errorf("ExecuteNetP50 = %.2f, want 300.0", p.ExecuteNetP50)
	}
	if p.ExecuteNetP99 != 594.0 {
		t.Errorf("ExecuteNetP99 = %.2f, want 594.0", p.ExecuteNetP99)
	}
	// ExecuteCount: i*7 → 7..700 (raw counts), P50 = 350, P99 = 693
	if p.ExecuteCountP50 != 350.0 {
		t.Errorf("ExecuteCountP50 = %.2f, want 350.0", p.ExecuteCountP50)
	}
	if p.ExecuteCountP99 != 693.0 {
		t.Errorf("ExecuteCountP99 = %.2f, want 693.0", p.ExecuteCountP99)
	}
}

func TestLatencyCollector_RingOverflow(t *testing.T) {
	lc := NewLatencyCollector()

	// Add 300 samples (overflows 200-element ring)
	for i := 0; i < 300; i++ {
		lc.Add(LatencySample{
			Dispatch: time.Duration(i) * time.Millisecond,
		})
	}

	if lc.Len() != latencyRingSize {
		t.Errorf("Len() = %d, want %d", lc.Len(), latencyRingSize)
	}

	p := lc.Percentiles()
	if p == nil {
		t.Fatal("expected non-nil percentiles")
	}
	if p.SampleCount != latencyRingSize {
		t.Errorf("SampleCount = %d, want %d", p.SampleCount, latencyRingSize)
	}

	// After overflow, the ring should contain samples 100..299
	// P50 of 100..299 → value at index floor(199*0.50)=99 → 100+99=199
	if p.DispatchP50 != 199.0 {
		t.Errorf("DispatchP50 after overflow = %.2f, want 199.0", p.DispatchP50)
	}
}

func TestLatencyCollector_ContextFields(t *testing.T) {
	lc := NewLatencyCollector()

	// Add 100 samples with varying context fields
	for i := 1; i <= 100; i++ {
		lc.Add(LatencySample{
			Dispatch:      time.Duration(i) * time.Millisecond,
			SendKeys:      time.Millisecond,
			Echo:          time.Millisecond,
			FrameSend:     time.Millisecond,
			MutexWait:     time.Duration(i*2) * time.Millisecond,
			ExecuteNet:    time.Duration(i*3) * time.Millisecond,
			ExecuteCount:  i * 4,
			OutputChDepth: i,      // 1..100
			EchoDataLen:   i * 10, // 10..1000
		})
	}

	p := lc.Percentiles()
	if p == nil {
		t.Fatal("expected non-nil percentiles")
	}

	// For 100 samples sorted 1..100:
	// P50 = index floor(99*0.50)=49 → value 50
	// P99 = index floor(99*0.99)=98 → value 99
	if p.OutputChDepthP50 != 50.0 {
		t.Errorf("OutputChDepthP50 = %.2f, want 50.0", p.OutputChDepthP50)
	}
	if p.OutputChDepthP99 != 99.0 {
		t.Errorf("OutputChDepthP99 = %.2f, want 99.0", p.OutputChDepthP99)
	}
	// EchoDataLen: 10..1000, P50 = 500, P99 = 990
	if p.EchoDataLenP50 != 500.0 {
		t.Errorf("EchoDataLenP50 = %.2f, want 500.0", p.EchoDataLenP50)
	}
	if p.EchoDataLenP99 != 990.0 {
		t.Errorf("EchoDataLenP99 = %.2f, want 990.0", p.EchoDataLenP99)
	}
}

func TestLatencyCollector_Len(t *testing.T) {
	lc := NewLatencyCollector()

	for i := 0; i < 50; i++ {
		lc.Add(LatencySample{Dispatch: time.Millisecond})
	}
	if lc.Len() != 50 {
		t.Errorf("Len() = %d after 50 adds, want 50", lc.Len())
	}

	// Fill to exactly ring size
	for i := 50; i < latencyRingSize; i++ {
		lc.Add(LatencySample{Dispatch: time.Millisecond})
	}
	if lc.Len() != latencyRingSize {
		t.Errorf("Len() = %d after %d adds, want %d", lc.Len(), latencyRingSize, latencyRingSize)
	}

	// One more
	lc.Add(LatencySample{Dispatch: time.Millisecond})
	if lc.Len() != latencyRingSize {
		t.Errorf("Len() = %d after overflow, want %d", lc.Len(), latencyRingSize)
	}
}
