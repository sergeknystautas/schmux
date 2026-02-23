package shellutil

import "strings"

// Quote quotes a string for safe use in shell commands using single quotes.
// Single quotes preserve everything literally, including newlines.
// Embedded single quotes are handled with the '\" trick.
// Null bytes are stripped since they cause C-string truncation in shells.
func Quote(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
