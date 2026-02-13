// Package signal provides signal parsing for agent-to-schmux communication.
package signal

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ValidStates are the recognized schmux signal states.
var ValidStates = map[string]bool{
	"needs_input":   true,
	"needs_testing": true,
	"completed":     true,
	"error":         true,
	"working":       true,
}

// Signal represents a parsed signal from an agent.
type Signal struct {
	State     string    // needs_input, needs_testing, completed, error, working
	Message   string    // Optional message from the agent
	Timestamp time.Time // When the signal was detected
}

// bracketPattern matches bracket-based signal markers on their own line: --<[schmux:state:message]>--
// Format: --<[schmux:<state>:<message>]>--
// Groups: 1=state, 2=message
// Requires signals to be on their own line (with optional leading/trailing whitespace).
// Also allows common line prefixes like bullets (⏺) used by Claude Code.
// This prevents matching signals in code blocks or documentation examples.
var bracketPattern = regexp.MustCompile(`(?m)^[\x00-\x1f⏺•\-\*\s]*--<\[schmux:(\w+):([^\]]*)\]>--[ \t]*\r*$`)

// StripANSIBytes removes ANSI escape sequences from a byte slice using a state machine.
// Cursor forward sequences (\x1b[nC) are replaced with n spaces to preserve word boundaries.
// Cursor down sequences (\x1b[nB) are replaced with n newlines to preserve line boundaries.
// All other escape sequences (CSI, OSC, DCS, APC) are consumed entirely.
// This follows ECMA-48 terminal protocol structure for complete coverage.
func StripANSIBytes(dst, data []byte) []byte {
	const (
		stNormal = iota
		stEsc
		stCSI
		stOSC
		stDCS // also handles APC
	)

	var out []byte
	if dst != nil {
		out = dst[:0]
	} else {
		out = make([]byte, 0, len(data))
	}
	st := stNormal
	escSeen := false    // for OSC/DCS ST terminator detection (\x1b\\)
	var csiParam []byte // accumulate CSI parameter bytes to parse count

	for _, b := range data {
		switch st {
		case stNormal:
			if b == 0x1b {
				st = stEsc
			} else {
				out = append(out, b)
			}

		case stEsc:
			switch b {
			case '[':
				st = stCSI
				csiParam = csiParam[:0]
			case ']':
				st = stOSC
				escSeen = false
			case 'P', '_': // DCS or APC
				st = stDCS
				escSeen = false
			default:
				// Unknown ESC sequence (e.g., ESC c for reset) — consume just the ESC
				st = stNormal
			}

		case stCSI:
			if b >= 0x30 && b <= 0x3F {
				// Parameter bytes (0-9, :, ;, <, =, >, ?)
				csiParam = append(csiParam, b)
			} else if b >= 0x20 && b <= 0x2F {
				// Intermediate bytes — ignore
			} else if b >= 0x40 && b <= 0x7E {
				// Final byte — determines the command
				switch b {
				case 'C': // Cursor Forward — replace with spaces
					n := parseCSICount(csiParam)
					for i := 0; i < n; i++ {
						out = append(out, ' ')
					}
				case 'B': // Cursor Down — replace with newlines
					n := parseCSICount(csiParam)
					for i := 0; i < n; i++ {
						out = append(out, '\n')
					}
				}
				// All other CSI commands: consume (emit nothing)
				st = stNormal
			}
			// Else: still accumulating CSI sequence

		case stOSC:
			if escSeen {
				if b == '\\' {
					st = stNormal
				}
				escSeen = false
				continue
			}
			if b == 0x07 { // BEL terminates OSC
				st = stNormal
				continue
			}
			escSeen = b == 0x1b

		case stDCS:
			if escSeen {
				if b == '\\' {
					st = stNormal
				}
				escSeen = false
				continue
			}
			escSeen = b == 0x1b
		}
	}

	return out
}

// parseCSICount extracts the numeric parameter from CSI parameter bytes.
// Returns 1 if no parameter is present (default for cursor movement commands).
// Handles DEC Private Mode prefix '?' by skipping it.
func parseCSICount(params []byte) int {
	if len(params) == 0 {
		return 1
	}
	// Skip DEC private mode prefix
	s := string(params)
	if len(s) > 0 && s[0] == '?' {
		return 1 // DEC private mode sequences don't have meaningful counts for our purposes
	}
	// Take first parameter before any ';'
	if idx := strings.IndexByte(s, ';'); idx >= 0 {
		s = s[:idx]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return string(StripANSIBytes(nil, []byte(s)))
}

// IsValidState checks if a state string is a recognized schmux signal state.
func IsValidState(state string) bool {
	return ValidStates[state]
}

// parseBracketSignals extracts signals from clean (ANSI-stripped) data using
// bracket-based markers (--<[schmux:state:message]>--).
func parseBracketSignals(cleanData []byte, now time.Time) []Signal {
	matches := bracketPattern.FindAllSubmatch(cleanData, -1)
	if len(matches) == 0 {
		return nil
	}

	var signals []Signal
	for _, match := range matches {
		state := string(match[1])
		message := string(match[2])

		// Only include signals with valid schmux states
		if !IsValidState(state) {
			continue
		}

		signals = append(signals, Signal{
			State:     state,
			Message:   message,
			Timestamp: now,
		})
	}

	return signals
}

// parseSignals extracts all valid schmux signals from the given data.
// Recognizes bracket-based markers (--<[schmux:state:message]>--).
// Only returns signals where the state matches a valid schmux state.
// This is an internal helper used by tests to validate the core parsing logic.
func parseSignals(data []byte) []Signal {
	now := time.Now()
	cleanData := StripANSIBytes(nil, data)
	return parseBracketSignals(cleanData, now)
}

// MapStateToNudge maps a signal state to the corresponding nudge display state.
// The nudge states are used by the frontend for consistent display.
func MapStateToNudge(state string) string {
	switch state {
	case "needs_input":
		return "Needs Authorization"
	case "needs_testing":
		return "Needs User Testing"
	case "completed":
		return "Completed"
	case "error":
		return "Error"
	case "working":
		return "Working"
	default:
		return state
	}
}

// ShortID returns the first 8 characters of an ID for log output,
// or the full ID if it's shorter than 8 characters.
func ShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
