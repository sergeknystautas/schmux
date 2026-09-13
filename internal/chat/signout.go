package chat

import "strings"

// Sign-out statement lists per protocol. A turn error whose text contains
// one of these statements (case-insensitive) is the session reporting that
// its login is gone. Explicit lists only — never fuzzy classification
// (docs/specs/chat-signed-out-recovery.md, State rules / Set).
//
// Seeds are best-effort: acceptance scenarios 1 and 4 observe the real
// harness phrasings and the lists are tightened there. Usage-limit and
// unrelated error texts must never appear here.
var signOutStatements = map[string][]string{
	ProtocolClaude: {
		"please run /login",
		"you are not logged in",
		"session expired",
	},
	ProtocolCodex: {
		"codex is not logged in",
		"login required",
		"log in to chatgpt",
	},
}

// MatchSignOutStatement reports whether a harness turn-error text is a
// sign-out statement for the protocol. Unknown protocols never match.
func MatchSignOutStatement(protocol, text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, stmt := range signOutStatements[protocol] {
		if strings.Contains(lower, stmt) {
			return true
		}
	}
	return false
}
