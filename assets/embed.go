// Package dashboardassets embeds the built React dashboard for distribution.
package dashboardassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dashboard/dist
var dashboardFS embed.FS

// FS returns the embedded dashboard assets as an fs.FS rooted at the dist directory.
// Returns nil if the embedded assets contain no index.html (placeholder-only build).
func FS() fs.FS {
	sub, err := fs.Sub(dashboardFS, "dashboard/dist")
	if err != nil {
		return nil
	}
	// Check if real assets are embedded (not just .gitkeep)
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
