package floormanager

import (
	"regexp"
	"strings"
)

// ansiEscape matches ANSI escape sequences: CSI (ESC[...X), OSC (ESC]...BEL/ST), and simple ESC sequences.
var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-9;]*[a-zA-Z]|\][^\x07]*\x07|\([A-Z])`)

// StripControlChars removes ANSI escape sequences, control characters, and newlines
// from text destined for terminal injection. Preserves printable unicode.
func StripControlChars(s string) string {
	// Strip ANSI escape sequences first
	s = ansiEscape.ReplaceAllString(s, "")
	// Replace newlines/tabs with spaces, strip other control chars
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f: // control characters
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// QuoteContentField wraps a content string in double quotes with internal quotes escaped.
// This ensures content fields (message, intent, blockers) cannot be confused with
// protocol prefixes like [SIGNAL] or [SHIFT].
func QuoteContentField(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
