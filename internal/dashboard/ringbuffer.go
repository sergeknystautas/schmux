package dashboard

// RingBuffer is a fixed-size circular byte buffer.
// It is not thread-safe — callers must ensure single-writer access.
type RingBuffer struct {
	buf    []byte
	cursor int
	full   bool
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{buf: make([]byte, size)}
}

func (rb *RingBuffer) Write(data []byte) {
	n := len(data)
	size := len(rb.buf)
	if n >= size {
		// data larger than buffer — keep only the last `size` bytes
		copy(rb.buf, data[n-size:])
		rb.cursor = 0
		rb.full = true
		return
	}
	end := rb.cursor + n
	if end <= size {
		copy(rb.buf[rb.cursor:], data)
	} else {
		first := size - rb.cursor
		copy(rb.buf[rb.cursor:], data[:first])
		copy(rb.buf, data[first:])
	}
	rb.cursor = end % size
	if end >= size {
		rb.full = true
	}
}

func (rb *RingBuffer) Snapshot() []byte {
	if !rb.full {
		return append([]byte(nil), rb.buf[:rb.cursor]...)
	}
	out := make([]byte, len(rb.buf))
	n := copy(out, rb.buf[rb.cursor:])
	copy(out[n:], rb.buf[:rb.cursor])
	return out
}
