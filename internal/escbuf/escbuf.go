// Package escbuf prevents ANSI escape sequence breaks at frame boundaries.
//
// When terminal output is streamed over WebSocket frames, a frame boundary
// can split a multi-byte ANSI escape sequence (e.g. "\x1b[38;5;196m") across
// two frames. The terminal emulator receives a partial sequence at the end of
// one frame and the rest at the start of the next, which can cause rendering
// glitches. SplitClean holds back any trailing partial escape sequence so that
// each frame sent is "clean" — it never ends mid-sequence.
package escbuf

// SplitClean prepends holdback from the previous call to data, then scans
// backward from the end for a trailing partial ANSI escape sequence. It returns
// the clean prefix to send now and any trailing partial to hold for the next call.
//
// It is a pure function with no internal state — the caller owns the holdback bytes.
func SplitClean(holdback, data []byte) (send, newHoldback []byte) {
	// Combine previous holdback with new data
	var buf []byte
	if len(holdback) > 0 && len(data) > 0 {
		buf = make([]byte, len(holdback)+len(data))
		copy(buf, holdback)
		copy(buf[len(holdback):], data)
	} else if len(holdback) > 0 {
		// No new data — keep holding
		return nil, holdback
	} else if len(data) > 0 {
		buf = data
	} else {
		return nil, nil
	}

	// Scan backward up to 16 bytes from the end looking for ESC (0x1b)
	scanStart := len(buf) - 16
	if scanStart < 0 {
		scanStart = 0
	}

	escIdx := -1
	for i := len(buf) - 1; i >= scanStart; i-- {
		if buf[i] == 0x1b {
			escIdx = i
			break
		}
	}

	// No ESC in tail — everything is clean
	if escIdx < 0 {
		return dup(buf), nil
	}

	tail := buf[escIdx:] // from ESC to end

	if isCompleteEscape(tail) {
		return dup(buf), nil
	}

	// Incomplete — hold back from ESC onward
	if escIdx == 0 {
		return nil, dup(tail)
	}
	return dup(buf[:escIdx]), dup(tail)
}

// isCompleteEscape checks whether tail (starting with ESC) is a complete
// escape sequence. Returns false if it appears truncated.
func isCompleteEscape(tail []byte) bool {
	if len(tail) == 0 {
		return true
	}
	if tail[0] != 0x1b {
		return true
	}

	// Bare ESC at end
	if len(tail) == 1 {
		return false
	}

	switch tail[1] {
	case '[': // CSI sequence: ESC [ <params> <terminator>
		// Need at least ESC [ <terminator> (3 bytes)
		if len(tail) < 3 {
			return false
		}
		// Check if last byte is a CSI terminator (0x40-0x7E)
		last := tail[len(tail)-1]
		return last >= 0x40 && last <= 0x7E

	case ']': // OSC sequence: ESC ] ... (terminated by BEL or ST)
		// Look for BEL (0x07) or ST (ESC \)
		for i := 2; i < len(tail); i++ {
			if tail[i] == 0x07 {
				return true // terminated by BEL
			}
			if tail[i] == 0x1b && i+1 < len(tail) && tail[i+1] == '\\' {
				return true // terminated by ST (ESC \)
			}
		}
		return false // no terminator found

	default:
		// Two-byte escape sequences (e.g. ESC 7, ESC 8, ESC c, etc.)
		// These are complete at 2 bytes.
		return true
	}
}

// dup returns a copy of b so returned slices don't alias the input.
func dup(b []byte) []byte {
	if b == nil {
		return nil
	}
	if len(b) == 0 {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
