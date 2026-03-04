package escbuf

import (
	"bytes"
	"testing"
)

// BenchmarkSplitClean_Clean measures the common case: 64 bytes of printable
// text with no ESC in the trailing 16-byte scan window.
func BenchmarkSplitClean_Clean(b *testing.B) {
	data := bytes.Repeat([]byte("ABCDabcd"), 8) // 64 bytes, no ESC
	var scratch []byte
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		_, _, scratch = SplitClean(scratch, nil, data)
	}
}

// BenchmarkSplitClean_WithHoldback measures prepending an 8-byte holdback
// to a 64-byte clean data chunk (holdback completion path).
func BenchmarkSplitClean_WithHoldback(b *testing.B) {
	holdback := []byte("\x1b[38;5;1")                             // 8 bytes — incomplete CSI
	data := []byte("96m" + string(bytes.Repeat([]byte("X"), 61))) // completes CSI + 61 bytes
	var scratch []byte
	b.SetBytes(int64(len(holdback) + len(data)))
	b.ResetTimer()
	for b.Loop() {
		_, _, scratch = SplitClean(scratch, holdback, data)
	}
}

// BenchmarkSplitClean_TrailingCSI measures data ending mid-escape, triggering
// the holdback split path.
func BenchmarkSplitClean_TrailingCSI(b *testing.B) {
	data := append(bytes.Repeat([]byte("Z"), 56), []byte("\x1b[38;5;1")...) // 56 clean + 8 partial CSI
	var scratch []byte
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		_, _, scratch = SplitClean(scratch, nil, data)
	}
}

// BenchmarkSplitClean_LargeChunk measures a 4KB output chunk simulating agent
// dump output. No ESC in the tail — exercises the scan path at scale.
func BenchmarkSplitClean_LargeChunk(b *testing.B) {
	data := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog.\n"), 90) // ~4050 bytes
	var scratch []byte
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		_, _, scratch = SplitClean(scratch, nil, data)
	}
}
