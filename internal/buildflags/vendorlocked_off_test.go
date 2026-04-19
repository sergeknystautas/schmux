//go:build !vendorlocked

package buildflags

import "testing"

func TestVendorLockedTagOff(t *testing.T) {
	if VendorLocked {
		t.Fatal("VendorLocked must be false without -tags=vendorlocked")
	}
}
