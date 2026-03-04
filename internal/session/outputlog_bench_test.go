package session

import (
	"testing"
)

// BenchmarkOutputLogAppend measures the mutex + ring buffer write path for a
// 64-byte output event (typical terminal line).
func BenchmarkOutputLogAppend(b *testing.B) {
	log := NewOutputLog(50000) // production size
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte('A' + i%26)
	}
	b.SetBytes(64)
	b.ResetTimer()
	for b.Loop() {
		log.Append(data)
	}
}

// BenchmarkOutputLogReplayFrom measures the gap recovery read path — replaying
// the last 100 entries from a full ring buffer.
func BenchmarkOutputLogReplayFrom(b *testing.B) {
	log := NewOutputLog(50000)
	data := make([]byte, 64)
	// Fill the buffer
	for i := 0; i < 50000; i++ {
		log.Append(data)
	}
	// Replay from 100 entries before current
	fromSeq := log.CurrentSeq() - 100
	b.ResetTimer()
	for b.Loop() {
		log.ReplayFrom(fromSeq)
	}
}
