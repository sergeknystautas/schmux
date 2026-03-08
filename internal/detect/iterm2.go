package detect

import (
	"os"
	"runtime"
)

// ITerm2Available reports whether iTerm2 is installed on this machine.
// Only returns true on macOS where /Applications/iTerm.app exists.
func ITerm2Available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := os.Stat("/Applications/iTerm.app")
	return err == nil
}
