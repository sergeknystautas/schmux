//go:build vendorlocked

package buildflags

import "testing"

func TestVendorLockedTagOn(t *testing.T) {
	if !VendorLocked {
		t.Fatal("VendorLocked must be true under -tags=vendorlocked")
	}
}
