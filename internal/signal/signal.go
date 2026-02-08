// Package signal provides OSC 777 escape sequence parsing for agent-to-schmux signaling.
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

// Signal represents a parsed OSC 777 signal from an agent.
type Signal struct {
	State     string    // needs_input, needs_testing, completed, error, working
	Message   string    // Optional message from the agent
	Timestamp time.Time // When the signal was detected
}

// oscPattern matches OSC 777 sequences with either BEL (\x07) or ST (\x1b\\) terminator.
// Format: ESC ] 777 ; notify ; <state> ; <message> BEL/ST
// Groups: 1=state (BEL), 2=message (BEL), 3=state (ST), 4=message (ST)
var oscPattern = regexp.MustCompile(`\x1b\]777;notify;([^;\x07\x1b]+);([^\x07\x1b]*)\x07|\x1b\]777;notify;([^;\x07\x1b]+);([^\x07\x1b]*)\x1b\\`)

// IsValidState checks if a state string is a recognized schmux signal state.
func IsValidState(state string) bool {
	return ValidStates[state]
}

// ParseSignals extracts all valid schmux signals from the given data.
// Only returns signals where the state matches a valid schmux state.
// Non-schmux OSC 777 notifications are ignored.
func ParseSignals(data []byte) []Signal {
	matches := oscPattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil
	}

	now := time.Now()
	var signals []Signal

	for _, match := range matches {
		var state, message string

		// Check which terminator pattern matched
		if len(match[1]) > 0 {
			// BEL terminator
			state = string(match[1])
			message = string(match[2])
		} else if len(match[3]) > 0 {
			// ST terminator
			state = string(match[3])
			message = string(match[4])
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

// ExtractAndStripSignals parses signals and returns both the signals and the data
// with recognized schmux signals removed. Non-schmux OSC 777 notifications are
// left in the data unchanged.
func ExtractAndStripSignals(data []byte) ([]Signal, []byte) {
	signals := ParseSignals(data)
	if len(signals) == 0 {
		return nil, data
	}

	// Build a new regex that only matches valid schmux states
	// We need to strip only the OSC sequences that have valid states
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
