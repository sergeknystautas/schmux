// Package cmdtemplate renders argv-array command templates with text/template,
// preventing shell injection by ensuring each template slot becomes exactly
// one argv element regardless of value contents.
//
// See docs/specs/meta-distribution-hardening-final.md §2.1 for the full
// rationale. The short version: every slot is rendered independently and
// each rendered value occupies exactly one position in the resulting argv.
// A value containing shell metacharacters (`;`, `|`, `$`, backticks,
// newlines, etc.) cannot create additional argv elements because there is
// no shell parsing step — the renderer hands argv directly to
// exec.CommandContext.
//
// For commands that genuinely need shell features (pipes, redirection,
// subshells) the standard escape hatch is `sh -c <script> <argv0>
// <argv1>...`. The renderer enforces that when basename(argv[0]) is a
// recognised POSIX shell and argv[1] == "-c", the script slot (argv[2])
// must contain no template syntax. Positional argv slots after the script
// may still be templated; they become "$0", "$1", ... inside the script
// and the shell does not parse the value.
package cmdtemplate

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

// Template is an argv-array command template. Each element is rendered
// independently with text/template; the rendered values become argv slots
// for exec.CommandContext.
type Template []string

// shellBinaries are the basenames recognised as POSIX-ish shells for the
// literal-script-slot enforcement.
var shellBinaries = map[string]bool{
	"sh":   true,
	"bash": true,
	"dash": true,
	"zsh":  true,
	"ksh":  true,
}

// Render expands every slot of t against vars and returns the resulting argv.
// It returns an error if the template is empty, if any slot fails to render,
// if a slot renders to the empty string, or if the shell-escape-hatch
// invariant is violated (template syntax in the script slot of a
// `<shell> -c <script> ...` invocation).
func (t Template) Render(vars map[string]string) ([]string, error) {
	if len(t) == 0 {
		return nil, errors.New("empty command template")
	}

	// Detect shell escape hatch: argv[0] is a shell + argv[1] == "-c".
	// In that case argv[2] (the script) must contain no template syntax;
	// positional args after the script may still be templated.
	shellEscape := len(t) >= 2 && shellBinaries[filepath.Base(t[0])] && t[1] == "-c"

	argv := make([]string, 0, len(t))
	for i, slot := range t {
		if shellEscape && i == 2 {
			// Script slot — must be literal.
			if strings.Contains(slot, "{{") {
				return nil, fmt.Errorf("slot %d (shell -c script) contains template syntax; only positional args after the script may be templated", i)
			}
			argv = append(argv, slot)
			continue
		}

		rendered, err := renderOne(slot, vars)
		if err != nil {
			return nil, fmt.Errorf("slot %d: %w", i, err)
		}
		if rendered == "" {
			return nil, fmt.Errorf("slot %d rendered to empty string", i)
		}
		argv = append(argv, rendered)
	}
	return argv, nil
}

// renderOne renders a single template slot against vars. Missing variables
// are an error (Option("missingkey=error")) so a typo cannot silently
// produce "<no value>" in argv.
func renderOne(slot string, vars map[string]string) (string, error) {
	tmpl, err := template.New("slot").Option("missingkey=error").Parse(slot)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}
