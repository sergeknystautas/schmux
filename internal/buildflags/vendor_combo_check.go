//go:build vendorlocked && (!nogithub || !notunnel || !nodashboardsx)

package buildflags

// Vendor-locked builds MUST also set nogithub, notunnel, and nodashboardsx.
// This file's build constraint activates only when that requirement is
// violated; the duplicate VendorLocked declaration below collides with the
// one in vendorlocked_on.go, producing a "VendorLocked redeclared in this
// block" compile error that fails the build with a message pointing at the
// missing tags.
//
// See docs/security.md "Vendor-locked builds" for why all four tags must
// ship together.
const VendorLocked = "vendorlocked requires nogithub, notunnel, and nodashboardsx tags"
