package escbuf

import (
	"bytes"
	"testing"
)

func TestSplitClean(t *testing.T) {
	tests := []struct {
		name     string
		holdback []byte
		data     []byte
		wantSend []byte
		wantHold []byte
	}{
		{
			name:     "no escape sequences",
			data:     []byte("hello world"),
			wantSend: []byte("hello world"),
		},
		{
			name:     "complete CSI at end",
			data:     []byte("hello\x1b[0m"),
			wantSend: []byte("hello\x1b[0m"),
		},
		{
			name:     "bare ESC at end",
			data:     []byte("hello\x1b"),
			wantSend: []byte("hello"),
			wantHold: []byte("\x1b"),
		},
		{
			name:     "partial CSI - ESC [",
			data:     []byte("hello\x1b["),
			wantSend: []byte("hello"),
			wantHold: []byte("\x1b["),
		},
		{
			name:     "incomplete CSI params",
			data:     []byte("hello\x1b[38;5"),
			wantSend: []byte("hello"),
			wantHold: []byte("\x1b[38;5"),
		},
		{
			name:     "two-byte escape - save cursor",
			data:     []byte("hello\x1b7"),
			wantSend: []byte("hello\x1b7"),
		},
		{
			name:     "two-byte escape - reset",
			data:     []byte("hello\x1bc"),
			wantSend: []byte("hello\x1bc"),
		},
		{
			name:     "holdback + completion",
			holdback: []byte("\x1b["),
			data:     []byte("0mworld"),
			wantSend: []byte("\x1b[0mworld"),
		},
		{
			name:     "holdback still incomplete",
			holdback: []byte("\x1b["),
			data:     []byte("38;5"),
			wantHold: []byte("\x1b[38;5"),
		},
		{
			name:     "empty data with holdback",
			holdback: []byte("\x1b["),
			wantHold: []byte("\x1b["),
		},
		{
			name: "both empty",
		},
		{
			name: "both nil",
		},
		{
			name:     "ESC early in data clean tail",
			data:     []byte("\x1b[0mhello world"),
			wantSend: []byte("\x1b[0mhello world"),
		},
		{
			name:     "multiple ESCs only last matters - last complete",
			data:     []byte("\x1b[31mhello\x1b[0m"),
			wantSend: []byte("\x1b[31mhello\x1b[0m"),
		},
		{
			name:     "multiple ESCs only last matters - last incomplete",
			data:     []byte("\x1b[31mhello\x1b["),
			wantSend: []byte("\x1b[31mhello"),
			wantHold: []byte("\x1b["),
		},
		{
			name:     "data is only ESC",
			data:     []byte("\x1b"),
			wantHold: []byte("\x1b"),
		},
		{
			name:     "OSC incomplete - no terminator",
			data:     []byte("\x1b]0;title"),
			wantHold: []byte("\x1b]0;title"),
		},
		{
			name:     "OSC complete with BEL",
			data:     []byte("\x1b]0;title\x07"),
			wantSend: []byte("\x1b]0;title\x07"),
		},
		{
			name:     "OSC complete with ST",
			data:     []byte("\x1b]0;title\x1b\\"),
			wantSend: []byte("\x1b]0;title\x1b\\"),
		},
		{
			name:     "complete SGR 256 color",
			data:     []byte("text\x1b[38;5;196m"),
			wantSend: []byte("text\x1b[38;5;196m"),
		},
		{
			name:     "incomplete SGR 256 color",
			data:     []byte("text\x1b[38;5;196"),
			wantSend: []byte("text"),
			wantHold: []byte("\x1b[38;5;196"),
		},
		{
			name:     "CSI cursor move complete",
			data:     []byte("text\x1b[10;20H"),
			wantSend: []byte("text\x1b[10;20H"),
		},
		{
			name:     "holdback bare ESC + data completes CSI",
			holdback: []byte("\x1b"),
			data:     []byte("[0mrest"),
			wantSend: []byte("\x1b[0mrest"),
		},
		{
			name:     "holdback bare ESC + data still incomplete",
			holdback: []byte("\x1b"),
			data:     []byte("[38"),
			wantHold: []byte("\x1b[38"),
		},
		{
			name:     "data with only complete sequences",
			data:     []byte("\x1b[1m\x1b[31mBOLD RED\x1b[0m"),
			wantSend: []byte("\x1b[1m\x1b[31mBOLD RED\x1b[0m"),
		},
		{
			name:     "OSC with text before",
			data:     []byte("before\x1b]0;title"),
			wantSend: []byte("before"),
			wantHold: []byte("\x1b]0;title"),
		},
		{
			name:     "OSC with text before complete",
			data:     []byte("before\x1b]0;title\x07after"),
			wantSend: []byte("before\x1b]0;title\x07after"),
		},
		// CSI followed by normal text — the old bug: checking last byte of
		// entire tail instead of finding the CSI final byte properly.
		{
			name:     "CSI followed by newline",
			data:     []byte("\x1b[0m\n"),
			wantSend: []byte("\x1b[0m\n"),
		},
		{
			name:     "CSI followed by text ending with digit",
			data:     []byte("\x1b[0mline1"),
			wantSend: []byte("\x1b[0mline1"),
		},
		{
			name:     "CSI followed by text ending with space",
			data:     []byte("\x1b[0m "),
			wantSend: []byte("\x1b[0m "),
		},
		{
			name:     "CSI reset then newline then text",
			data:     []byte("out\x1b[0m\nmore"),
			wantSend: []byte("out\x1b[0m\nmore"),
		},
		{
			name:     "CSI with intermediate bytes",
			data:     []byte("\x1b[ q"), // DECSCUSR (set cursor style)
			wantSend: []byte("\x1b[ q"),
		},
		{
			name:     "DCS incomplete - no ST terminator",
			data:     []byte("text\x1bP1$r"),
			wantSend: []byte("text"),
			wantHold: []byte("\x1bP1$r"),
		},
		{
			name:     "DCS complete with ST",
			data:     []byte("text\x1bP1$r1 q\x1b\\"),
			wantSend: []byte("text\x1bP1$r1 q\x1b\\"),
		},
		{
			name:     "bare DCS at end",
			data:     []byte("text\x1bP"),
			wantSend: []byte("text"),
			wantHold: []byte("\x1bP"),
		},
		// APC (ESC _) sequences
		{
			name:     "APC incomplete - no ST terminator",
			data:     []byte("text\x1b_app"),
			wantSend: []byte("text"),
			wantHold: []byte("\x1b_app"),
		},
		{
			name:     "APC complete with ST",
			data:     []byte("text\x1b_app\x1b\\"),
			wantSend: []byte("text\x1b_app\x1b\\"),
		},
		{
			name:     "bare APC at end",
			data:     []byte("text\x1b_"),
			wantSend: []byte("text"),
			wantHold: []byte("\x1b_"),
		},
		// PM (ESC ^) sequences
		{
			name:     "PM incomplete - no ST terminator",
			data:     []byte("text\x1b^pmsg"),
			wantSend: []byte("text"),
			wantHold: []byte("\x1b^pmsg"),
		},
		{
			name:     "PM complete with ST",
			data:     []byte("text\x1b^msg\x1b\\"),
			wantSend: []byte("text\x1b^msg\x1b\\"),
		},
		// SOS (ESC X) sequences
		{
			name:     "SOS incomplete - no ST terminator",
			data:     []byte("text\x1bXsos"),
			wantSend: []byte("text"),
			wantHold: []byte("\x1bXsos"),
		},
		{
			name:     "SOS complete with ST",
			data:     []byte("text\x1bXsos\x1b\\"),
			wantSend: []byte("text\x1bXsos\x1b\\"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSend, gotHold, _ := SplitClean(nil, tt.holdback, tt.data)
			if !bytes.Equal(gotSend, tt.wantSend) {
				t.Errorf("send:\n  got  %q\n  want %q", gotSend, tt.wantSend)
			}
			if !bytes.Equal(gotHold, tt.wantHold) {
				t.Errorf("holdback:\n  got  %q\n  want %q", gotHold, tt.wantHold)
			}
		})
	}
}

func TestSplitCleanHoldbackDoesNotAlias(t *testing.T) {
	// Verify that returned holdback doesn't alias the input data
	data := []byte("hello\x1b")
	_, hold, _ := SplitClean(nil, nil, data)

	// Mutate original
	data[0] = 'X'

	if hold[0] == 'X' {
		t.Error("holdback aliases input data")
	}
}

func TestSplitCleanSendMayAlias(t *testing.T) {
	// send is deliberately a sub-slice of data (no copy) for zero-alloc.
	// The caller must consume send before the next SplitClean call.
	data := []byte("hello world")
	send, _, _ := SplitClean(nil, nil, data)

	if !bytes.Equal(send, []byte("hello world")) {
		t.Errorf("send: got %q, want %q", send, "hello world")
	}
}

func TestSplitCleanScratchReuse(t *testing.T) {
	// When holdback + data requires concatenation, the scratch buffer should
	// be reused across calls to avoid repeated allocations.
	var scratch []byte
	holdback := []byte("\x1b[")
	data := []byte("0mworld")

	send, hold, scratch := SplitClean(scratch, holdback, data)
	if string(send) != "\x1b[0mworld" {
		t.Errorf("send: got %q, want %q", send, "\x1b[0mworld")
	}
	if hold != nil {
		t.Errorf("hold: got %q, want nil", hold)
	}

	// Second call with same scratch — should reuse capacity
	capBefore := cap(scratch)
	holdback2 := []byte("\x1b[")
	data2 := []byte("31mred")
	send2, _, scratch := SplitClean(scratch, holdback2, data2)
	if string(send2) != "\x1b[31mred" {
		t.Errorf("send2: got %q, want %q", send2, "\x1b[31mred")
	}
	if cap(scratch) != capBefore {
		t.Error("scratch buffer was reallocated when capacity was sufficient")
	}
}

func TestSplitCleanChainedCalls(t *testing.T) {
	// Simulate a multi-frame stream where a CSI sequence is split across 3 frames
	var hold []byte
	var scratch []byte
	var send []byte

	// Frame 1: text ending with bare ESC
	send, hold, scratch = SplitClean(scratch, hold, []byte("hello\x1b"))
	if string(send) != "hello" {
		t.Errorf("frame 1 send: got %q, want %q", send, "hello")
	}

	// Frame 2: CSI introducer, still incomplete
	send, hold, scratch = SplitClean(scratch, hold, []byte("[38;5"))
	if send != nil {
		t.Errorf("frame 2 send: got %q, want nil", send)
	}

	// Frame 3: rest of sequence + more text
	send, hold, scratch = SplitClean(scratch, hold, []byte(";196mworld"))
	if string(send) != "\x1b[38;5;196mworld" {
		t.Errorf("frame 3 send: got %q, want %q", send, "\x1b[38;5;196mworld")
	}
	if hold != nil {
		t.Errorf("frame 3 hold: got %q, want nil", hold)
	}
}

// TestSplitClean_16ByteScanWindow documents the intentional limitation of the
// 16-byte backward scan window. A DCS/APC/PM/SOS sequence whose opening ESC
// falls more than 16 bytes from the end of the buffer will NOT be held back.
func TestSplitClean_16ByteScanWindow(t *testing.T) {
	// Construct data where ESC starts a DCS sequence but is >16 bytes from the end.
	// The DCS has no ST terminator so it's "incomplete", but escbuf can't see the ESC.
	// Expected: the data passes through as-is (the ESC is outside the scan window).
	prefix := bytes.Repeat([]byte("A"), 10)  // 10 bytes of padding
	dcs := []byte("\x1bP")                   // DCS start
	payload := bytes.Repeat([]byte("x"), 16) // 16 bytes of payload (no ST)
	// Total from ESC to end = 2 + 16 = 18 bytes > 16-byte window
	data := append(prefix, dcs...)
	data = append(data, payload...)

	send, hold, _ := SplitClean(nil, nil, data)
	// Because ESC is at position 10 and end is at 28, the scan window [12,28)
	// doesn't reach position 10. So the ESC is not found — entire buffer is "clean".
	if !bytes.Equal(send, data) {
		t.Errorf("expected entire buffer to pass through (ESC outside scan window)")
	}
	if hold != nil {
		t.Errorf("expected no holdback, got %q", hold)
	}
}
