package signal

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

const (
	maxSignalBufSize = 4096
	FlushTimeout     = 500 * time.Millisecond
)

type SignalDetector struct {
	sessionID        string
	buf              []byte
	stripBuf         []byte // reusable buffer for stripANSIBytes
	callback         func(Signal)
	nearMissCallback func(string)
	lastData         time.Time
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

func (d *SignalDetector) Feed(data []byte) {
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
	if len(d.buf) == 0 {
		return
	}
	data := d.buf
	d.buf = nil
	d.parseLines(data)
}

func (d *SignalDetector) ShouldFlush() bool {
	return len(d.buf) > 0 && !d.lastData.IsZero() && time.Since(d.lastData) >= FlushTimeout
}

func (d *SignalDetector) parseLines(data []byte) {
	now := time.Now()
	d.stripBuf = stripANSIBytes(d.stripBuf, data)
	cleanData := d.stripBuf
	signals := parseBracketSignals(cleanData, now)
	for _, sig := range signals {
		d.callback(sig)
	}
	if d.nearMissCallback != nil && len(signals) == 0 {
		for _, line := range strings.Split(string(cleanData), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.Contains(trimmed, "--<[schmux:") && !bracketPattern.MatchString(trimmed) {
				d.nearMissCallback(trimmed)
			}
		}
	}
}

func (d *SignalDetector) enforceBufLimit() {
	if len(d.buf) > maxSignalBufSize {
		excess := len(d.buf) - maxSignalBufSize
		copy(d.buf, d.buf[excess:])
		d.buf = d.buf[:maxSignalBufSize]
		fmt.Printf("[signal] %s - line accumulator truncated (%d bytes exceeded limit)\n", ShortID(d.sessionID), excess)
	}
}
