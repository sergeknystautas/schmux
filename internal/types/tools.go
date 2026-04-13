package types

// BuiltinToolNames is the list of built-in detected tool names.
var BuiltinToolNames = []string{"claude", "codex", "gemini", "opencode"}

// IsBuiltinToolName reports whether name matches a built-in detected tool.
func IsBuiltinToolName(name string) bool {
	for _, tool := range BuiltinToolNames {
		if tool == name {
			return true
		}
	}
	return false
}
