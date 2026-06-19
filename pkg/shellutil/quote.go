package shellutil

import "strings"

// Quote quotes a string for safe use in shell commands using single quotes.
// Single quotes preserve everything literally, including newlines.
// Embedded single quotes are handled with the '\” trick.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// QuoteIfNeeded quotes s only when it contains characters the shell would
// interpret. Tokens made up entirely of safe characters (letters, digits, and
// -_@%+=:,./) are returned unchanged, so simple commands and flags stay
// readable. Use this when serializing a slice of already-tokenized arguments
// into a command string, where blanket Quote would needlessly wrap every flag.
func QuoteIfNeeded(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		if !shellSafeRune(r) {
			return Quote(s)
		}
	}
	return s
}

func shellSafeRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	switch r {
	case '-', '_', '@', '%', '+', '=', ':', ',', '.', '/':
		return true
	}
	return false
}
