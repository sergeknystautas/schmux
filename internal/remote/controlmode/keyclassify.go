package controlmode

import (
	"fmt"
	"strings"
	"time"
)

// KeyRun represents a contiguous run of keys that can be sent to tmux in a
// single send-keys command. Literal runs contain printable text sent with -l;
// non-literal runs contain a single tmux key name (e.g., "Enter", "Up").
// Hex runs contain space-separated hex bytes sent with -H (for raw byte
// sequences like mouse events that can't be split across commands).
type KeyRun struct {
	Text    string // The text to send (printable characters, tmux key name, or hex bytes)
	Literal bool   // If true, send with -l (literal mode)
	Hex     bool   // If true, send with -H (hex byte mode)
}

// ctrlKeyNames maps control characters (0x01–0x1a) to pre-computed tmux key
// names, avoiding fmt.Sprintf on the hot path. Index 0 is unused (NUL).
var ctrlKeyNames [27]string

func init() {
	for i := 1; i <= 26; i++ {
		ctrlKeyNames[i] = "C-" + string(rune('a'+i-1))
	}
}

// ClassifyKeyRuns splits a key input string into runs of printable text and
// special keys. This is the classification logic extracted from SendKeys so it
// can be benchmarked and optimized independently of the Execute dispatch.
//
// Each KeyRun is either:
//   - Literal=true: a run of printable ASCII (32-126) to send with send-keys -l
//   - Literal=false: a single tmux key name (Enter, Tab, Up, M-Enter, etc.)
//
// dst is an optional pre-allocated slice for the result. When non-nil, the
// function appends to dst[:0], reusing the backing array to avoid allocation
// for typical single-key input (≤8 runs covers 99%+ of interactive typing).
//
// Unknown CSI sequences are silently skipped (same behavior as SendKeys).
func ClassifyKeyRuns(dst []KeyRun, keys string) []KeyRun {
	runs := dst[:0]
	i := 0
	for i < len(keys) {
		// Find run of printable characters (ASCII 32-126)
		j := i
		for j < len(keys) {
			b := keys[j]
			if b >= 32 && b < 127 {
				j++
			} else if b >= 0xC0 && b <= 0xF7 {
				size := 2
				if b >= 0xE0 && b <= 0xEF {
					size = 3
				} else if b >= 0xF0 {
					size = 4
				}
				if j+size <= len(keys) {
					valid := true
					for k := 1; k < size; k++ {
						if keys[j+k] < 0x80 || keys[j+k] > 0xBF {
							valid = false
							break
						}
					}
					if valid {
						j += size
						continue
					}
				}
				break
			} else {
				break
			}
		}
		if j > i {
			runs = append(runs, KeyRun{Text: keys[i:j], Literal: true})
			i = j
			continue
		}

		// Handle special character at position i
		ch := keys[i]
		var keyName string
		advance := 1

		switch ch {
		case '\r', '\n':
			keyName = "Enter"
		case '\t':
			keyName = "Tab"
		case 127:
			keyName = "BSpace"
		case '\x1b':
			// Meta/Alt-modified Enter: ESC + CR/LF
			if i+1 < len(keys) && (keys[i+1] == '\r' || keys[i+1] == '\n') {
				keyName = "M-Enter"
				advance = 2
				break
			}
			// Meta/Alt-modified Backspace: ESC + DEL/BS
			if i+1 < len(keys) && (keys[i+1] == 127 || keys[i+1] == '\b') {
				keyName = "M-BSpace"
				advance = 2
				break
			}
			// CSI sequence: ESC [ ... <final byte 0x40-0x7E>
			if i+2 < len(keys) && keys[i+1] == '[' {
				end := i + 2
				for end < len(keys) && (keys[end] < 0x40 || keys[end] > 0x7E) {
					end++
				}
				if end < len(keys) {
					seq := keys[i : end+1]
					switch seq {
					case "\x1b[A":
						keyName = "Up"
					case "\x1b[B":
						keyName = "Down"
					case "\x1b[C":
						keyName = "Right"
					case "\x1b[D":
						keyName = "Left"
					case "\x1b[H":
						keyName = "Home"
					case "\x1b[F":
						keyName = "End"
					case "\x1b[2~":
						keyName = "Insert"
					case "\x1b[3~":
						keyName = "DC"
					case "\x1b[5~":
						keyName = "PageUp"
					case "\x1b[6~":
						keyName = "PageDown"
					case "\x1b[Z":
						keyName = "BTab"
					default:
						// Unknown CSI (e.g., SGR mouse events) — send as hex
						// bytes in a single atomic command. Splitting across
						// multiple send-keys commands would introduce timing
						// gaps that cause the TUI to misparse the sequence.
						runs = append(runs, KeyRun{Text: toHexBytes(keys[i : end+1]), Hex: true})
						i = end + 1
						continue
					}
					advance = end + 1 - i
				} else {
					keyName = "Escape"
				}
			} else if i+2 < len(keys) && keys[i+1] == 'O' {
				// SS3 sequence (e.g., ESC O P for F1)
				switch keys[i+2] {
				case 'P':
					keyName = "F1"
				case 'Q':
					keyName = "F2"
				case 'R':
					keyName = "F3"
				case 'S':
					keyName = "F4"
				default:
					keyName = "Escape"
					advance = 1
				}
				if keyName != "Escape" {
					advance = 3
				}
			} else {
				keyName = "Escape"
			}
		default:
			if ch >= 1 && ch <= 26 {
				keyName = ctrlKeyNames[ch]
			}
		}

		if keyName != "" {
			runs = append(runs, KeyRun{Text: keyName, Literal: false})
		}
		i += advance
	}
	return runs
}

// toHexBytes converts a string of raw bytes to tmux send-keys -H format
// (space-separated hex pairs, e.g., "1b 5b 3c 36 34 3b 31 3b 31 4d").
func toHexBytes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%02x", s[i])
	}
	return b.String()
}

// SendKeysTimings records per-keystroke timing breakdown from Client.SendKeys.
// MutexWait and ExecuteNet are non-overlapping and partition the SendKeys duration.
type SendKeysTimings struct {
	MutexWait    time.Duration // time blocked on stdinMu across all Execute() calls
	ExecuteNet   time.Duration // sum of Execute() round-trips, EXCLUDING mutex wait
	ExecuteCount int           // number of Execute() calls (= number of key runs)
}
