package signal

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxSignalBufSize = 32768
	FlushTimeout     = 500 * time.Millisecond
)

type SignalDetector struct {
	sessionID        string
	callback         func(Signal)
	nearMissCallback func(string)
	suppressed       atomic.Bool

	mu                    sync.Mutex // protects buf, stripBuf, lastData, lastSignal, lastEmittedNonWorking
	buf                   []byte
	stripBuf              []byte // reusable buffer for StripANSIBytes
	lastData              time.Time
	lastSignal            *Signal // most recent signal parsed (even during suppression)
	lastEmittedNonWorking *Signal // last non-working signal that fired the callback (for dedup)
}

func NewSignalDetector(sessionID string, callback func(Signal)) *SignalDetector {
	return &SignalDetector{
		sessionID: sessionID,
		callback:  callback,
	}
}

func (d *SignalDetector) SetNearMissCallback(cb func(string)) {
	d.nearMissCallback = cb
}

// Suppress enables/disables signal suppression. While suppressed, the detector
// still parses signals (maintaining internal state like partial line buffers)
// but does not invoke the callback. Used during scrollback parsing to avoid
// re-emitting old signals.
func (d *SignalDetector) Suppress(suppress bool) {
	d.suppressed.Store(suppress)
}

func (d *SignalDetector) Feed(data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.lastData = time.Now()
	var combined []byte
	if len(d.buf) > 0 {
		combined = make([]byte, len(d.buf)+len(data))
		copy(combined, d.buf)
		copy(combined[len(d.buf):], data)
		d.buf = nil
	} else {
		combined = data
	}
	lastNL := bytes.LastIndexByte(combined, '\n')
	if lastNL == -1 {
		d.buf = make([]byte, len(combined))
		copy(d.buf, combined)
		d.enforceBufLimit()
		return
	}
	completeLines := combined[:lastNL+1]
	if lastNL+1 < len(combined) {
		remaining := combined[lastNL+1:]
		d.buf = make([]byte, len(remaining))
		copy(d.buf, remaining)
	}
	d.parseLines(completeLines)
}

func (d *SignalDetector) Flush() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.buf) == 0 {
		return
	}
	data := d.buf
	d.buf = nil
	d.parseLines(data)
}

func (d *SignalDetector) ShouldFlush() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return len(d.buf) > 0 && !d.lastData.IsZero() && time.Since(d.lastData) >= FlushTimeout
}

func (d *SignalDetector) parseLines(data []byte) {
	now := time.Now()
	d.stripBuf = StripANSIBytes(d.stripBuf, data)
	cleanData := d.stripBuf
	signals := parseBracketSignals(cleanData, now)
	for _, sig := range signals {
		sigCopy := sig
		d.lastSignal = &sigCopy

		if d.suppressed.Load() {
			continue
		}

		// Deduplicate non-working signals against the last emitted non-working
		// signal. Terminal redraws can re-emit an old signal marker after a
		// transient "working" signal has already cleared it:
		//   needs_input(A) → working → [redraw] needs_input(A)
		// Without this, the "working" would reset a simple lastSignal check,
		// allowing the stale needs_input to re-fire and create a feedback loop.
		// Working signals are idempotent clears, so they always pass through.
		if sig.State != "working" {
			isDuplicate := d.lastEmittedNonWorking != nil &&
				d.lastEmittedNonWorking.State == sig.State &&
				d.lastEmittedNonWorking.Message == sig.Message
			if isDuplicate {
				continue
			}
			d.lastEmittedNonWorking = &sigCopy
		}

		d.callback(sig)
	}
	if d.nearMissCallback != nil && len(signals) == 0 {
		for _, line := range strings.Split(string(cleanData), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.Contains(trimmed, "--<[schmux:") && !bracketPatternLoose.MatchString(trimmed) {
				d.nearMissCallback(trimmed)
			}
		}
	}
}

func (d *SignalDetector) enforceBufLimit() {
	if len(d.buf) > maxSignalBufSize {
		excess := len(d.buf) - maxSignalBufSize
		discarded := d.buf[:excess]

		// Check if the discarded portion contains a signal marker
		if bytes.Contains(discarded, []byte("--<[schmux:")) {
			fmt.Printf("[signal] %s - WARNING: buffer truncation discarded data containing a signal marker\n", ShortID(d.sessionID))
			if d.nearMissCallback != nil {
				d.nearMissCallback("buffer truncation discarded signal-like data")
			}
		}

		copy(d.buf, d.buf[excess:])
		d.buf = d.buf[:maxSignalBufSize]
	}
}

// LastSignal returns the most recent signal parsed by this detector.
// Returns nil if no signal has been parsed yet. This is updated even
// during suppression, so it reflects the latest signal from scrollback.
func (d *SignalDetector) LastSignal() *Signal {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastSignal == nil {
		return nil
	}
	sig := *d.lastSignal
	return &sig
}

// EmitSignal fires the callback for the given signal, provided the detector
// is not suppressed and has a callback. Used by the tracker to re-emit a
// signal recovered from scrollback that differs from stored state.
func (d *SignalDetector) EmitSignal(sig Signal) {
	if d.callback != nil && !d.suppressed.Load() {
		d.callback(sig)
	}
}
