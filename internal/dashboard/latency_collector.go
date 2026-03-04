package dashboard

import (
	"sort"
	"time"
)

const latencyRingSize = 200

// LatencySample records the 4 server-side timing segments for one keystroke
// round-trip. All durations are measured in the WebSocket select loop and
// represent non-overlapping phases of the input→output path.
type LatencySample struct {
	Dispatch  time.Duration // controlChan receive → pre-SendKeys
	SendKeys  time.Duration // inside tracker.SendInput (tmux round-trip)
	Echo      time.Duration // SendKeys return → next output event arrives
	FrameSend time.Duration // output event → WriteMessage

	// Context fields: cheap reads at measurement time that help diagnose
	// whether P99 latency correlates with backpressure or correlation errors.
	OutputChDepth int // len(outputCh) when the input case fired
	EchoDataLen   int // len(event.Data) of the matched echo event
}

// LatencyPercentiles holds P50 and P99 values for each segment, in milliseconds.
// Sent to the frontend as part of the periodic stats message.
type LatencyPercentiles struct {
	DispatchP50  float64 `json:"dispatchP50"`
	DispatchP99  float64 `json:"dispatchP99"`
	SendKeysP50  float64 `json:"sendKeysP50"`
	SendKeysP99  float64 `json:"sendKeysP99"`
	EchoP50      float64 `json:"echoP50"`
	EchoP99      float64 `json:"echoP99"`
	FrameSendP50 float64 `json:"frameSendP50"`
	FrameSendP99 float64 `json:"frameSendP99"`
	SampleCount  int     `json:"sampleCount"`

	// Context field percentiles (raw counts, not durations)
	OutputChDepthP50 float64 `json:"outputChDepthP50"`
	OutputChDepthP99 float64 `json:"outputChDepthP99"`
	EchoDataLenP50   float64 `json:"echoDataLenP50"`
	EchoDataLenP99   float64 `json:"echoDataLenP99"`
}

// LatencyCollector is a fixed-size ring buffer that records per-keystroke
// latency samples. It is designed for single-goroutine access from the
// WebSocket select loop — no mutex needed.
type LatencyCollector struct {
	samples [latencyRingSize]LatencySample
	count   int  // total samples added (wraps around the ring)
	head    int  // next write position
	full    bool // ring has wrapped at least once
}

// NewLatencyCollector creates a new empty collector.
func NewLatencyCollector() *LatencyCollector {
	return &LatencyCollector{}
}

// Add records a new latency sample, overwriting the oldest when the ring is full.
func (lc *LatencyCollector) Add(s LatencySample) {
	lc.samples[lc.head] = s
	lc.head = (lc.head + 1) % latencyRingSize
	lc.count++
	if lc.count > latencyRingSize {
		lc.full = true
	}
}

// Percentiles computes P50 and P99 for each segment. Returns nil when no
// samples have been collected.
func (lc *LatencyCollector) Percentiles() *LatencyPercentiles {
	n := lc.Len()
	if n == 0 {
		return nil
	}

	// Extract the active portion of the ring into flat slices
	dispatch := make([]float64, n)
	sendKeys := make([]float64, n)
	echo := make([]float64, n)
	frameSend := make([]float64, n)
	outputChDepth := make([]float64, n)
	echoDataLen := make([]float64, n)

	start := 0
	if lc.full {
		start = lc.head // oldest entry is at head when ring has wrapped
	}
	for i := 0; i < n; i++ {
		idx := (start + i) % latencyRingSize
		s := lc.samples[idx]
		dispatch[i] = float64(s.Dispatch) / float64(time.Millisecond)
		sendKeys[i] = float64(s.SendKeys) / float64(time.Millisecond)
		echo[i] = float64(s.Echo) / float64(time.Millisecond)
		frameSend[i] = float64(s.FrameSend) / float64(time.Millisecond)
		outputChDepth[i] = float64(s.OutputChDepth)
		echoDataLen[i] = float64(s.EchoDataLen)
	}

	return &LatencyPercentiles{
		DispatchP50:      percentile(dispatch, 0.50),
		DispatchP99:      percentile(dispatch, 0.99),
		SendKeysP50:      percentile(sendKeys, 0.50),
		SendKeysP99:      percentile(sendKeys, 0.99),
		EchoP50:          percentile(echo, 0.50),
		EchoP99:          percentile(echo, 0.99),
		FrameSendP50:     percentile(frameSend, 0.50),
		FrameSendP99:     percentile(frameSend, 0.99),
		SampleCount:      n,
		OutputChDepthP50: percentile(outputChDepth, 0.50),
		OutputChDepthP99: percentile(outputChDepth, 0.99),
		EchoDataLenP50:   percentile(echoDataLen, 0.50),
		EchoDataLenP99:   percentile(echoDataLen, 0.99),
	}
}

// Len returns the number of samples currently in the ring.
func (lc *LatencyCollector) Len() int {
	if lc.full {
		return latencyRingSize
	}
	if lc.count > latencyRingSize {
		return latencyRingSize
	}
	return lc.count
}

// percentile computes the p-th percentile (0.0–1.0) of a float64 slice.
// Sorts the input in place.
func percentile(data []float64, p float64) float64 {
	sort.Float64s(data)
	idx := int(float64(len(data)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(data) {
		idx = len(data) - 1
	}
	return data[idx]
}
