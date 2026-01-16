package detect

// Built-in detected tool names.
var builtinToolNames = []string{"claude", "codex", "gemini"}

// GetBuiltinToolNames returns the built-in tool names.
func GetBuiltinToolNames() []string {
	out := make([]string, len(builtinToolNames))
	copy(out, builtinToolNames)
	return out
}

// IsBuiltinToolName reports whether name matches a built-in detected tool.
func IsBuiltinToolName(name string) bool {
	for _, tool := range builtinToolNames {
		if tool == name {
			return true
		}
	}
	return false
}
