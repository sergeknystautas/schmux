package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/buildflags"
)

// skipUnderVendorlocked aborts the calling test if the binary was
// built with -tags=vendorlocked. Use in tests that assert pre-lock
// getter behavior (e.g. GetBindAddress returns the configured value)
// which the vendorlocked build intentionally overrides.
//
// Under non-vendor builds, the if-branch is a typed-const compare and
// gets dead-code-eliminated by the compiler.
func skipUnderVendorlocked(t *testing.T) {
	t.Helper()
	if buildflags.VendorLocked {
		t.Skip("pre-lock test; skipped under -tags=vendorlocked")
	}
}
