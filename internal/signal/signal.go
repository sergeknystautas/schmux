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

// oscPattern matches OSC 777 sequences with either BEL (\x07) or ST (\x1b\\) terminator.
// Format: ESC ] 777 ; notify ; <state> ; <message> BEL/ST
// Groups: 1=state (BEL), 2=message (BEL), 3=state (ST), 4=message (ST)
var oscPattern = regexp.MustCompile(`\x1b\]777;notify;([^;\x07\x1b]+);([^\x07\x1b]*)\x07|\x1b\]777;notify;([^;\x07\x1b]+);([^\x07\x1b]*)\x1b\\`)

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

// ansiSeq is an optional ANSI escape sequence pattern for use in tolerant matching.
// Matches zero or more ANSI CSI sequences (including DEC Private Mode sequences).
const ansiSeq = `(?:\x1b\[\??[0-9;]*[A-Za-z])*`

// ansiOne matches a single ANSI CSI sequence for use in character classes.
// Includes DEC Private Mode sequences (\x1b[?...).
const ansiOne = `\x1b\[\??[0-9;]*[A-Za-z]`

// bracketPatternTolerant matches bracket-based signal markers on their own line with optional ANSI sequences.
// Format: --<[schmux:state:message]>-- on its own line
// Used for stripping markers from terminal output where ANSI sequences may be embedded.
// Matches signals preceded by: start of string, newline, CR, or ANSI cursor movement sequences.
// Allows common line prefixes like bullets (⏺) used by Claude Code.
// The suffix only matches horizontal whitespace to avoid consuming line endings.
// Neither prefix nor suffix consume \n to preserve line structure.
var bracketPatternTolerant = regexp.MustCompile(
	`(?:^|\n|\r|` + ansiOne + `)` + // Start of string, newline, CR, or ANSI sequence
		`(?:[⏺• \t\r]|` + ansiOne + `)*` + // Bullets/spaces/tabs/CR/ANSI (NOT \n)
		`-` + ansiSeq + `-` + ansiSeq + `<` + ansiSeq + `\[` + ansiSeq +
		`s` + ansiSeq + `c` + ansiSeq + `h` + ansiSeq + `m` + ansiSeq + `u` + ansiSeq + `x` + ansiSeq +
		`:` + ansiSeq + `(\w+)` + ansiSeq + `:` + ansiSeq + `([^\]]*)` + ansiSeq +
		`\]` + ansiSeq + `>` + ansiSeq + `-` + ansiSeq + `-` +
		`[ \t]*`) // Trailing: only horizontal whitespace (spaces, tabs)

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

// parseOSCSignals extracts signals from OSC 777 escape sequences.
func parseOSCSignals(data []byte, now time.Time) []Signal {
	matches := oscPattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil
	}

	var signals []Signal
	for _, match := range matches {
		var state, message string

		// Check which terminator pattern matched
		if len(match[1]) > 0 {
			// BEL terminator
			state = string(match[1])
			message = stripANSI(string(match[2]))
		} else if len(match[3]) > 0 {
			// ST terminator
			state = string(match[3])
			message = stripANSI(string(match[4]))
		} else {
			continue
		}

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
// Recognizes both OSC 777 escape sequences and bracket-based markers.
// Only returns signals where the state matches a valid schmux state.
// Non-schmux OSC 777 notifications are ignored.
func ParseSignals(data []byte) []Signal {
	now := time.Now()

	// Parse both OSC and bracket-based signals
	oscSignals := parseOSCSignals(data, now)
	bracketSignals := parseBracketSignals(data, now)

	// Combine signals (OSC first, then bracket)
	if len(oscSignals) == 0 && len(bracketSignals) == 0 {
		return nil
	}

	signals := make([]Signal, 0, len(oscSignals)+len(bracketSignals))
	signals = append(signals, oscSignals...)
	signals = append(signals, bracketSignals...)

	return signals
}

// ExtractAndStripSignals parses signals and returns both the signals and the data
// with recognized schmux signals removed. Strips both OSC 777 and bracket-based markers.
// Non-schmux OSC 777 notifications are left in the data unchanged.
func ExtractAndStripSignals(data []byte) ([]Signal, []byte) {
	signals := ParseSignals(data)
	if len(signals) == 0 {
		return nil, data
	}

	// Strip OSC sequences with valid schmux states
	cleanData := oscPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		submatches := oscPattern.FindSubmatch(match)
		if submatches == nil {
			return match
		}

		var state string
		if len(submatches[1]) > 0 {
			state = string(submatches[1])
		} else if len(submatches[3]) > 0 {
			state = string(submatches[3])
		}

		// Only strip if it's a valid schmux state
		if IsValidState(state) {
			return nil
		}
		// Leave non-schmux OSC 777 notifications unchanged
		return match
	})

	// Strip bracket-based markers with valid schmux states
	// Use tolerant pattern that handles embedded ANSI sequences
	cleanData = bracketPatternTolerant.ReplaceAllFunc(cleanData, func(match []byte) []byte {
		submatches := bracketPatternTolerant.FindSubmatch(match)
		if submatches == nil {
			return match
		}

		// Strip ANSI from state before checking validity
		state := stripANSI(string(submatches[1]))

		// Only strip if it's a valid schmux state
		if IsValidState(state) {
			return nil
		}
		return match
	})

	return signals, cleanData
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
