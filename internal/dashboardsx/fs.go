//go:build !nodashboardsx

package dashboardsx

import "os"

// Filesystem seam. Production uses the real os functions; tests swap these for an
// in-memory fake so credential-named files (cert.pem, *.key) are never written to
// disk — the fence "code" template denies those writes everywhere it can write.
var (
	writeFile = os.WriteFile
	readFile  = os.ReadFile
	statFile  = os.Stat
)
