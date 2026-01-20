// Package version provides the application version.
package version

// Version is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/sergeknystautas/schmux/internal/version.Version=1.2.3" ./cmd/schmux
//
// Defaults to "dev" for local development builds.
var Version = "dev"
