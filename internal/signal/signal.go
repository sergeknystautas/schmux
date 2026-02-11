// Package signal provides signal parsing for agent-to-schmux communication.
package signal

import (
	"regexp"
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
var bracketPattern = regexp.MustCompile(`(?m)^[⏺•\-\*\s]*--<\[schmux:(\w+):([^\]]*)\]>--[ \t]*\r*$`)

// ansiPattern matches ANSI escape sequences (CSI sequences like cursor movement, colors, etc.)
// Also matches DEC Private Mode sequences (\x1b[?...) used for terminal mode switching.
// Used to strip terminal escape sequences from signal messages.
var ansiPattern = regexp.MustCompile(`\x1b\[\??[0-9;]*[A-Za-z]`)

// oscSeqPattern matches OSC (Operating System Command) sequences like window title changes.
// Format: ESC ] <params> BEL  or  ESC ] <params> ST
// These are NOT CSI sequences and need separate handling.
var oscSeqPattern = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// cursorForwardPattern matches cursor forward sequences (\x1b[nC) which terminals often use
// instead of spaces. We replace these with actual spaces to preserve word boundaries.
var cursorForwardPattern = regexp.MustCompile(`\x1b\[\d*C`)

// cursorDownPattern matches cursor down sequences (\x1b[nB) which terminals use for
// vertical movement. We replace these with newlines to preserve line boundaries.
var cursorDownPattern = regexp.MustCompile(`\x1b\[\d*B`)

// stripANSI removes ANSI escape sequences from a string.
// Cursor forward sequences (\x1b[nC) are replaced with spaces to preserve word boundaries,
// since terminals often use these instead of actual space characters.
// Cursor down sequences (\x1b[nB) are replaced with newlines to preserve line boundaries.
// Also removes OSC sequences (like window title changes).
func stripANSI(s string) string {
	// First replace cursor forward sequences with spaces
	s = cursorForwardPattern.ReplaceAllString(s, " ")
	// Replace cursor down sequences with newlines
	s = cursorDownPattern.ReplaceAllString(s, "\n")
	// Remove OSC sequences (window titles, etc.)
	s = oscSeqPattern.ReplaceAllString(s, "")
	// Then remove all other ANSI sequences
	return ansiPattern.ReplaceAllString(s, "")
}

// stripANSIBytes removes ANSI escape sequences from a byte slice.
// Cursor forward sequences (\x1b[nC) are replaced with spaces to preserve word boundaries.
// Cursor down sequences (\x1b[nB) are replaced with newlines to preserve line boundaries.
// Also removes OSC sequences (like window title changes).
func stripANSIBytes(data []byte) []byte {
	// First replace cursor forward sequences with spaces
	data = cursorForwardPattern.ReplaceAll(data, []byte(" "))
	// Replace cursor down sequences with newlines
	data = cursorDownPattern.ReplaceAll(data, []byte("\n"))
	// Remove OSC sequences (window titles, etc.)
	data = oscSeqPattern.ReplaceAll(data, nil)
	// Then remove all other ANSI sequences
	return ansiPattern.ReplaceAll(data, nil)
}

// IsValidState checks if a state string is a recognized schmux signal state.
func IsValidState(state string) bool {
	return ValidStates[state]
}

// parseBracketSignals extracts signals from bracket-based markers (--<[schmux:state:message]>--).
// Strips ANSI escape sequences from data before matching to handle terminals that insert
// cursor movement sequences between characters.
func parseBracketSignals(data []byte, now time.Time) []Signal {
	// Strip ANSI sequences before matching to handle embedded cursor movements
	cleanData := stripANSIBytes(data)
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

// ParseSignals extracts all valid schmux signals from the given data.
// Recognizes bracket-based markers (--<[schmux:state:message]>--).
// Only returns signals where the state matches a valid schmux state.
// Signal markers are NOT stripped from the data - they remain visible in terminal output.
func ParseSignals(data []byte) []Signal {
	now := time.Now()
	return parseBracketSignals(data, now)
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
