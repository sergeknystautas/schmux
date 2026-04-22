package session

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"time"
)

// ClipboardRequest is one OSC 52 write extracted from a session's byte stream.
// Sent over SessionRuntime.clipboardCh; consumed by the dashboard server.
//
// The dashboard server is the single source of truth for the request UUID
// (see internal/dashboard/clipboard_state.go) — it manages the ack-vs-stale
// check, so minting an ID here would just be wasted work and a confusing
// parallel ID.
type ClipboardRequest struct {
	SessionID            string
	Text                 string // post-defang
	ByteCount            int    // pre-defang decoded length
	StrippedControlChars int
	Timestamp            time.Time
}

// pcValidationRe enforces OSC 52 selection-char syntax. Empty Pc is allowed
// and means c+s; non-empty must consist of the documented selectors.
var pcValidationRe = regexp.MustCompile(`^[cpsqb0-7]+$`)

// maxOSC52DecodedSize bounds a single clipboard payload at 64 KiB.
const maxOSC52DecodedSize = 64 * 1024

// maxOSC52CarrySize bounds the carry buffer at the same 64 KiB; if a TUI
// emits an OSC 52 prefix without ever terminating, we eventually flush
// the carry to output to avoid unbounded growth.
const maxOSC52CarrySize = 64 * 1024

// osc52Extractor scans a session's byte stream for OSC 52 ("set clipboard")
// escape sequences, removes them from the forwarded output, validates +
// decodes the payload, and emits ClipboardRequests for the dashboard.
//
// The extractor is stateful only across a narrow OSC 52 prefix table —
// title OSCs, CSI sequences, and other escapes flush through immediately.
// This keeps the carry buffer bounded and avoids holding back unrelated
// terminal output across event boundaries.
type osc52Extractor struct {
	sessionID string
	carry     []byte
}

func newOSC52Extractor(sessionID string) *osc52Extractor {
	return &osc52Extractor{sessionID: sessionID}
}

// process consumes one chunk of session bytes. Returns:
//   - out: the bytes to forward to outputLog/subscribers, with OSC 52 stripped.
//   - reqs: zero or more ClipboardRequests extracted from this chunk (and any
//     that completed across the carry boundary).
func (e *osc52Extractor) process(input []byte) (out []byte, reqs []ClipboardRequest) {
	// Prepend any partial OSC 52 prefix carried over from the previous chunk.
	if len(e.carry) > 0 {
		buf := make([]byte, 0, len(e.carry)+len(input))
		buf = append(buf, e.carry...)
		buf = append(buf, input...)
		input = buf
		e.carry = nil
	}

	out = make([]byte, 0, len(input))
	i := 0
	for i < len(input) {
		if hasOSC52Prefix(input[i:]) {
			end, termLen := findOSC52Terminator(input[i+5:])
			if end >= 0 {
				payload := input[i+5 : i+5+end]
				if req, ok := e.extractRequest(payload); ok {
					reqs = append(reqs, req)
				}
				i = i + 5 + end + termLen
				continue
			}
			// Unterminated OSC 52 — carry the partial sequence to the next
			// chunk so the terminator can complete it. Don't emit these
			// bytes to out (they're part of the sequence we're stripping).
			e.carry = append(e.carry[:0], input[i:]...)
			// Carry-overflow failsafe: if a TUI never closes the sequence
			// we'd buffer forever. Flush it as plain bytes and reset.
			if len(e.carry) > maxOSC52CarrySize {
				out = append(out, e.carry...)
				e.carry = nil
			}
			return out, reqs
		}
		out = append(out, input[i])
		i++
	}

	// After the main loop, the trailing bytes of out may form a partial
	// OSC 52 prefix that just hasn't gathered enough bytes yet to match
	// hasOSC52Prefix. Peel those bytes off into the carry. The prefix
	// table is intentionally narrow — only \x1b], \x1b]5, \x1b]52, \x1b]52;
	// — so other OSCs (title \x1b]0;, etc.) flush immediately.
	for n := 5; n >= 2; n-- {
		if len(out) >= n && osc52PartialPrefix(out[len(out)-n:]) {
			e.carry = append(e.carry[:0], out[len(out)-n:]...)
			out = out[:len(out)-n]
			break
		}
	}
	return out, reqs
}

// hasOSC52Prefix reports whether b starts with "\x1b]52;".
func hasOSC52Prefix(b []byte) bool {
	return len(b) >= 5 && b[0] == 0x1b && b[1] == ']' && b[2] == '5' && b[3] == '2' && b[4] == ';'
}

// findOSC52Terminator looks for BEL (0x07) or ST (ESC \). Returns index of
// terminator in b and its length, or (-1, 0) if not found.
func findOSC52Terminator(b []byte) (int, int) {
	for i := 0; i < len(b); i++ {
		if b[i] == 0x07 {
			return i, 1
		}
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
			return i, 2
		}
	}
	return -1, 0
}

// osc52PartialPrefix returns true if b is one of:
//
//	\x1b]  \x1b]5  \x1b]52  \x1b]52;
//
// (not \x1b alone, not other OSC starts like \x1b]0). This narrow table
// is what makes the extractor safe: only OSC 52 partials are held back
// across event boundaries; everything else (titles, CSI, lone ESC, …)
// flushes through immediately.
func osc52PartialPrefix(b []byte) bool {
	switch len(b) {
	case 2:
		return b[0] == 0x1b && b[1] == ']'
	case 3:
		return b[0] == 0x1b && b[1] == ']' && b[2] == '5'
	case 4:
		return b[0] == 0x1b && b[1] == ']' && b[2] == '5' && b[3] == '2'
	case 5:
		return b[0] == 0x1b && b[1] == ']' && b[2] == '5' && b[3] == '2' && b[4] == ';'
	}
	return false
}

// extractRequest validates Pc + Pd, base64-decodes the payload, applies
// byte-level defang (strip C0 controls except \n/\t, plus DEL 0x7f), and
// builds a ClipboardRequest. Returns (zero, false) on any rejection so
// the malformed sequence is silently dropped.
func (e *osc52Extractor) extractRequest(payload []byte) (ClipboardRequest, bool) {
	semi := bytes.IndexByte(payload, ';')
	if semi < 0 {
		return ClipboardRequest{}, false
	}
	pc := payload[:semi]
	pd := payload[semi+1:]
	if len(pc) > 0 && !pcValidationRe.Match(pc) {
		return ClipboardRequest{}, false
	}
	// Empty Pd or read-query "?" — not a write, ignore.
	if len(pd) == 0 || (len(pd) == 1 && pd[0] == '?') {
		return ClipboardRequest{}, false
	}
	decoded, err := base64.StdEncoding.DecodeString(string(pd))
	if err != nil {
		return ClipboardRequest{}, false
	}
	if len(decoded) > maxOSC52DecodedSize {
		return ClipboardRequest{}, false
	}

	text, byteCount, stripped := defangClipboardBytes(decoded)
	return ClipboardRequest{
		SessionID:            e.sessionID,
		Text:                 text,
		ByteCount:            byteCount,
		StrippedControlChars: stripped,
		Timestamp:            time.Now(),
	}, true
}

// defangClipboardBytes applies the byte-level defang shared between OSC 52
// extraction and the tmux paste-buffer path: strip C0 controls except `\n`
// (0x0a) and `\t` (0x09), plus DEL (0x7f). UTF-8 lead/continuation bytes are
// >= 0x80 and unaffected, so multibyte characters round-trip cleanly.
//
// Returns the post-defang text, the original byte count (pre-defang), and the
// number of stripped control bytes. Both code paths consume these to populate
// ClipboardRequest fields, so security parity is automatic — anything the OSC 52
// path strips, the paste-buffer path strips too.
func defangClipboardBytes(decoded []byte) (text string, byteCount int, stripped int) {
	defanged := make([]byte, 0, len(decoded))
	for _, b := range decoded {
		if (b < 0x20 && b != '\n' && b != '\t') || b == 0x7f {
			stripped++
			continue
		}
		defanged = append(defanged, b)
	}
	return string(defanged), len(decoded), stripped
}
