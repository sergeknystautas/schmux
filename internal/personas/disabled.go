//go:build nopersonas

package personas

// IsAvailable reports whether the personas feature is compiled in.
func IsAvailable() bool { return false }
