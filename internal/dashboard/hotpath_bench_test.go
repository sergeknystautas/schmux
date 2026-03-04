package dashboard

import (
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// BenchmarkAppendSequencedFrame measures binary frame header construction with
// a 64-byte payload (typical terminal line) using a reusable buffer.
func BenchmarkAppendSequencedFrame(b *testing.B) {
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte('A' + i%26)
	}
	var buf []byte
	b.SetBytes(int64(8 + len(data)))
	b.ResetTimer()
	for b.Loop() {
		buf = appendSequencedFrame(buf, 42, data)
	}
}

// BenchmarkAppendSequencedFrame_Large measures frame construction with a 4KB
// payload (agent dump simulation) using a reusable buffer.
func BenchmarkAppendSequencedFrame_Large(b *testing.B) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte('x')
	}
	var buf []byte
	b.SetBytes(int64(8 + len(data)))
	b.ResetTimer()
	for b.Loop() {
		buf = appendSequencedFrame(buf, 42, data)
	}
}

// BenchmarkLatencyCollectorAdd measures ring buffer insertion (should be
// near-zero — just a struct copy + modular increment).
func BenchmarkLatencyCollectorAdd(b *testing.B) {
	lc := NewLatencyCollector()
	sample := LatencySample{
		Dispatch:      100 * time.Microsecond,
		SendKeys:      5 * time.Millisecond,
		Echo:          2 * time.Millisecond,
		FrameSend:     50 * time.Microsecond,
		OutputChDepth: 3,
		EchoDataLen:   64,
	}
	b.ResetTimer()
	for b.Loop() {
		lc.Add(sample)
	}
}

// BenchmarkLatencyCollectorPercentiles measures percentile computation on a
// full ring (200 samples, 6 sorts). This runs on the stats broadcast path.
func BenchmarkLatencyCollectorPercentiles(b *testing.B) {
	lc := NewLatencyCollector()
	for i := 0; i < latencyRingSize; i++ {
		lc.Add(LatencySample{
			Dispatch:      time.Duration(i) * time.Microsecond,
			SendKeys:      time.Duration(i) * time.Millisecond,
			Echo:          time.Duration(i) * 100 * time.Microsecond,
			FrameSend:     time.Duration(i) * 10 * time.Microsecond,
			OutputChDepth: i % 10,
			EchoDataLen:   i * 10,
		})
	}
	b.ResetTimer()
	for b.Loop() {
		lc.Percentiles()
	}
}

// BenchmarkWSMessageReader_Binary measures binary message routing through
// startWSMessageReader using a mock wsReader (reuses the mockWSReader pattern
// from websocket_helpers_test.go).
func BenchmarkWSMessageReader_Binary(b *testing.B) {
	// Pre-build a large batch of messages
	const batchSize = 1000
	msgs := make([]mockWSMsg, batchSize+1)
	for i := 0; i < batchSize; i++ {
		msgs[i] = mockWSMsg{
			msgType: websocket.BinaryMessage,
			data:    []byte("hello"),
		}
	}
	// Terminal error to close the reader goroutine
	msgs[batchSize] = mockWSMsg{err: errMockClosed}

	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		reader := &benchWSReader{messages: msgs}
		b.StartTimer()

		ch := startWSMessageReader(reader)
		for range ch {
		}
	}
}

// benchWSReader is a resettable mock for benchmarking startWSMessageReader.
type benchWSReader struct {
	messages []mockWSMsg
	idx      int
}

func (r *benchWSReader) ReadMessage() (int, []byte, error) {
	if r.idx >= len(r.messages) {
		return 0, nil, errMockClosed
	}
	msg := r.messages[r.idx]
	r.idx++
	return msg.msgType, msg.data, msg.err
}

var errMockClosed = fmt.Errorf("connection closed")
