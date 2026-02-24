package shellutil

import (
	"fmt"
	"strings"
)

// Split splits a command line string into arguments, respecting quotes.
// Handles single quotes, double quotes, and backslash escaping.
// This prevents breakage when paths contain spaces or special characters.
func Split(input string) ([]string, error) {
	var args []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool
	var escaped bool

	for i := 0; i < len(input); i++ {
		c := input[i]

		if escaped {
			// Previous char was backslash, add this char literally
			current.WriteByte(c)
			escaped = false
			continue
		}

		switch c {
		case '\\':
			if inSingleQuote {
				// Backslash is literal in single quotes
				current.WriteByte(c)
			} else {
				// Set escaped flag for next character
				escaped = true
			}

		case '\'':
			if inDoubleQuote {
				// Single quote is literal inside double quotes
				current.WriteByte(c)
			} else {
				// Toggle single quote mode
				inSingleQuote = !inSingleQuote
			}

		case '"':
			if inSingleQuote {
				// Double quote is literal inside single quotes
				current.WriteByte(c)
			} else {
				// Toggle double quote mode
				inDoubleQuote = !inDoubleQuote
			}

		case ' ', '\t', '\n', '\r':
			if inSingleQuote || inDoubleQuote {
				// Whitespace is literal inside quotes
				current.WriteByte(c)
			} else {
				// Whitespace outside quotes separates arguments
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			}

		default:
			current.WriteByte(c)
		}
	}

	// Handle unterminated quotes
	if inSingleQuote || inDoubleQuote {
		return nil, fmt.Errorf("unterminated quote in command")
	}

	// Add final argument if any
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}
