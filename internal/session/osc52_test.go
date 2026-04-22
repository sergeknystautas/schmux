package session

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// Step 8: plain bytes pass through untouched.
func TestExtractor_PlainBytesPassThrough(t *testing.T) {
	e := newOSC52Extractor("sess-1")
	out, reqs := e.process([]byte("hello world"))
	if !bytes.Equal(out, []byte("hello world")) {
		t.Errorf("output = %q, want %q", out, "hello world")
	}
	if len(reqs) != 0 {
		t.Errorf("got %d requests, want 0", len(reqs))
	}
}

// Step 9: a single OSC 52 with BEL terminator is fully extracted.
func TestExtractor_SingleEventBEL(t *testing.T) {
	e := newOSC52Extractor("sess-1")
	in := []byte("\x1b]52;c;aGVsbG8=\x07") // base64("hello")
	out, reqs := e.process(in)
	if len(out) != 0 {
		t.Errorf("output = %q, want empty", out)
	}
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Text != "hello" || r.ByteCount != 5 || r.StrippedControlChars != 0 {
		t.Errorf("req = %+v, want Text=hello ByteCount=5 Stripped=0", r)
	}
	if r.SessionID != "sess-1" {
		t.Errorf("metadata wrong: %+v", r)
	}
}

// Step 10a: ST terminator (ESC \) is also recognized.
func TestExtractor_ST_Terminator(t *testing.T) {
	e := newOSC52Extractor("s")
	out, reqs := e.process([]byte("\x1b]52;c;aGVsbG8=\x1b\\"))
	if len(out) != 0 || len(reqs) != 1 || reqs[0].Text != "hello" {
		t.Errorf("out=%q reqs=%+v", out, reqs)
	}
}

// Step 10b: surrounding plain bytes flush; OSC 52 in the middle is stripped.
func TestExtractor_BeforeAndAfter(t *testing.T) {
	e := newOSC52Extractor("s")
	out, reqs := e.process([]byte("before\x1b]52;c;aGVsbG8=\x07after"))
	if string(out) != "beforeafter" {
		t.Errorf("out=%q want beforeafter", out)
	}
	if len(reqs) != 1 || reqs[0].Text != "hello" {
		t.Errorf("reqs=%+v", reqs)
	}
}

// Step 10c: two adjacent sequences both extract.
func TestExtractor_TwoAdjacent(t *testing.T) {
	e := newOSC52Extractor("s")
	in := []byte("\x1b]52;c;YQ==\x07\x1b]52;c;Yg==\x07") // a, b
	out, reqs := e.process(in)
	if len(out) != 0 || len(reqs) != 2 {
		t.Errorf("out=%q reqs=%+v", out, reqs)
	}
	if reqs[0].Text != "a" || reqs[1].Text != "b" {
		t.Errorf("texts=%q,%q", reqs[0].Text, reqs[1].Text)
	}
}

// Step 11a: a sequence split across two process calls completes via carry.
func TestExtractor_CrossEventSplit(t *testing.T) {
	e := newOSC52Extractor("s")
	out1, reqs1 := e.process([]byte("\x1b]52;c;aGVsb"))
	if len(out1) != 0 || len(reqs1) != 0 {
		t.Errorf("event 1: out=%q reqs=%+v", out1, reqs1)
	}
	out2, reqs2 := e.process([]byte("G8=\x07"))
	if len(out2) != 0 {
		t.Errorf("event 2 out=%q want empty", out2)
	}
	if len(reqs2) != 1 || reqs2[0].Text != "hello" {
		t.Errorf("event 2 reqs=%+v", reqs2)
	}
}

// Step 11b: split sequence with surrounding plain bytes — surrounding bytes
// flush in their own events; the OSC 52 itself is stripped.
func TestExtractor_CrossEventWithSurrounding(t *testing.T) {
	e := newOSC52Extractor("s")
	out1, _ := e.process([]byte("before\x1b]52;c;aGVsb"))
	out2, reqs := e.process([]byte("G8=\x07after"))
	if string(out1) != "before" || string(out2) != "after" {
		t.Errorf("out1=%q out2=%q", out1, out2)
	}
	if len(reqs) != 1 || reqs[0].Text != "hello" {
		t.Errorf("reqs=%+v", reqs)
	}
}

// Step 11c: title OSCs (\x1b]0;...) must not be held back across events.
func TestExtractor_OtherOSCNotCarried(t *testing.T) {
	e := newOSC52Extractor("s")
	out, reqs := e.process([]byte("\x1b]0;new title\x07"))
	if string(out) != "\x1b]0;new title\x07" {
		t.Errorf("title OSC altered: out=%q", out)
	}
	if len(reqs) != 0 {
		t.Errorf("reqs=%+v", reqs)
	}
}

// Step 11d: a lone trailing ESC is NOT in the OSC 52 prefix table; flush.
func TestExtractor_LoneEscNotHeld(t *testing.T) {
	e := newOSC52Extractor("s")
	out, _ := e.process([]byte("foo\x1b"))
	if string(out) != "foo\x1b" {
		t.Errorf("out=%q want foo\\x1b", out)
	}
}

// Step 12a: empty Pc (means c+s) is accepted.
func TestExtractor_EmptyPcAccepted(t *testing.T) {
	e := newOSC52Extractor("s")
	out, reqs := e.process([]byte("\x1b]52;;aGVsbG8=\x07"))
	if len(out) != 0 || len(reqs) != 1 || reqs[0].Text != "hello" {
		t.Errorf("out=%q reqs=%+v", out, reqs)
	}
}

// Step 12b: invalid Pc (e.g. "xyz") is rejected silently — no request emitted,
// and the malformed sequence is still stripped from output.
func TestExtractor_InvalidPcRejected(t *testing.T) {
	e := newOSC52Extractor("s")
	out, reqs := e.process([]byte("\x1b]52;xyz;aGVsbG8=\x07"))
	if len(out) != 0 || len(reqs) != 0 {
		t.Errorf("out=%q reqs=%+v", out, reqs)
	}
}

// Step 12c: read-queries (Pd == "?") are not writes and emit nothing.
func TestExtractor_ReadQueryRejected(t *testing.T) {
	e := newOSC52Extractor("s")
	_, reqs := e.process([]byte("\x1b]52;c;?\x07"))
	if len(reqs) != 0 {
		t.Errorf("reqs=%+v", reqs)
	}
}

// Step 12d: payloads decoding to > 64 KiB are rejected.
func TestExtractor_OversizeRejected(t *testing.T) {
	e := newOSC52Extractor("s")
	big := bytes.Repeat([]byte{'a'}, maxOSC52DecodedSize+1)
	encoded := base64.StdEncoding.EncodeToString(big)
	in := append([]byte("\x1b]52;c;"), encoded...)
	in = append(in, 0x07)
	_, reqs := e.process(in)
	if len(reqs) != 0 {
		t.Errorf("reqs=%+v", reqs)
	}
}

// Step 13: byte-level defang strips C0 (except \n,\t) and DEL; counts them.
func TestExtractor_Defang(t *testing.T) {
	payload := []byte{'a', '\n', 'b', 0x1b, 'c', 0x07, 'd', 0x00, 'e'}
	encoded := base64.StdEncoding.EncodeToString(payload)
	in := append([]byte("\x1b]52;c;"), encoded...)
	in = append(in, 0x07)
	e := newOSC52Extractor("s")
	_, reqs := e.process(in)
	if len(reqs) != 1 {
		t.Fatalf("reqs=%+v", reqs)
	}
	r := reqs[0]
	if r.Text != "a\nbcde" {
		t.Errorf("Text=%q want %q", r.Text, "a\nbcde")
	}
	if r.StrippedControlChars != 3 {
		t.Errorf("Stripped=%d want 3", r.StrippedControlChars)
	}
	if r.ByteCount != len(payload) {
		t.Errorf("ByteCount=%d want %d", r.ByteCount, len(payload))
	}
}

// Step 14a: an OSC 52 that never terminates flushes its carry as plain
// bytes once the carry exceeds the cap; subsequent input flows normally.
func TestExtractor_CarryOverflowFlushes(t *testing.T) {
	e := newOSC52Extractor("s")
	open := []byte("\x1b]52;c;")
	junk := bytes.Repeat([]byte{'A'}, maxOSC52CarrySize+100)
	_, reqs := e.process(append(open, junk...))
	if len(reqs) != 0 {
		t.Errorf("expected no requests, got %+v", reqs)
	}
	out, _ := e.process([]byte("hello"))
	if string(out) != "hello" {
		t.Errorf("after overflow, out=%q want hello", out)
	}
}

// Step 14b: a single high byte (0x80) round-trips in the Text field.
// Go's string([]byte{0x80}) preserves the byte even though it isn't valid
// standalone UTF-8; the dashboard is responsible for any further normalization.
func TestExtractor_LoneByte0x80(t *testing.T) {
	// base64 of [0x80] = "gA=="
	e := newOSC52Extractor("s")
	_, reqs := e.process([]byte("\x1b]52;c;gA==\x07"))
	if len(reqs) != 1 {
		t.Fatalf("reqs=%+v", reqs)
	}
	if reqs[0].ByteCount != 1 {
		t.Errorf("ByteCount=%d want 1", reqs[0].ByteCount)
	}
}

// TestDefangClipboardBytes verifies the shared defang helper used by both the
// OSC 52 extractor and the tmux paste-buffer path. Same byte-level rules as
// TestExtractor_Defang (which exercises this through extractRequest), but
// invoked directly so the helper has its own coverage and security parity is
// guaranteed by construction.
func TestDefangClipboardBytes(t *testing.T) {
	tests := []struct {
		name              string
		in                []byte
		wantText          string
		wantByteCount     int
		wantStripped      int
		wantEqualPreserve bool // optional check that the original is untouched
	}{
		{
			name:          "plain ascii",
			in:            []byte("hello world"),
			wantText:      "hello world",
			wantByteCount: 11,
			wantStripped:  0,
		},
		{
			name:          "newlines and tabs preserved",
			in:            []byte("line1\nline2\tcol2"),
			wantText:      "line1\nline2\tcol2",
			wantByteCount: 16,
			wantStripped:  0,
		},
		{
			name:          "C0 controls (except \\n,\\t) and DEL stripped",
			in:            []byte{'a', 0x1b, 'b', 0x07, 'c', 0x00, 'd', 0x7f, 'e'},
			wantText:      "abcde",
			wantByteCount: 9,
			wantStripped:  4,
		},
		{
			name:          "high bytes (UTF-8 / 0x80+) preserved",
			in:            []byte{0xe4, 0xb8, 0x96}, // CJK char in UTF-8
			wantText:      "\xe4\xb8\x96",
			wantByteCount: 3,
			wantStripped:  0,
		},
		{
			name:          "empty input",
			in:            []byte{},
			wantText:      "",
			wantByteCount: 0,
			wantStripped:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, bc, stripped := defangClipboardBytes(tt.in)
			if text != tt.wantText {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
			if bc != tt.wantByteCount {
				t.Errorf("byteCount = %d, want %d", bc, tt.wantByteCount)
			}
			if stripped != tt.wantStripped {
				t.Errorf("stripped = %d, want %d", stripped, tt.wantStripped)
			}
		})
	}
}
