# Release Strategy

This document specifies how schmux is distributed to end users, covering versioning, release artifacts, and the runtime asset download mechanism.

## Problem Statement

schmux has a React dashboard that must be built via npm before the Go binary can serve it. The built assets (`assets/dashboard/dist/`) are gitignored and not committed to the repository.

This creates a problem for `go install`:

```bash
go install github.com/schmux/schmux/cmd/schmux@latest
```

`go install` fetches source code only—it cannot run npm build steps. Users who install this way get a binary with no dashboard assets.

## Solution Overview

**Runtime asset download**: The binary downloads pre-built dashboard assets from GitHub Releases on first run.

1. Each GitHub Release includes a `dashboard-assets.tar.gz` artifact
2. The binary has its version compiled in (e.g., `1.2.3`)
3. On startup, if assets are missing or outdated, the binary fetches the matching version from GitHub Releases
4. Assets are cached in `~/.schmux/dashboard/`

This allows `go install` to work while keeping built assets out of git.

## Versioning Strategy

### Source of Truth

Git tags are the source of truth for versions:

```bash
git tag v1.2.3
git push origin v1.2.3
```

### Compile-Time Injection

Version is injected at build time via ldflags:

```bash
go build -ldflags "-X github.com/schmux/schmux/internal/version.Version=1.2.3" ./cmd/schmux
```

### Version Package

```go
// internal/version/version.go
package version

// Version is set at build time via ldflags.
// Defaults to "dev" for local development builds.
var Version = "dev"
```

### Version Format

- Release versions: `1.2.3` (semver, no `v` prefix in code)
- Git tags: `v1.2.3` (with `v` prefix, Go convention)
- Dev builds: `dev` (default when not set via ldflags)

## Release Artifacts

Each GitHub Release (e.g., `v1.2.3`) includes:

| Artifact | Contents | Purpose |
|----------|----------|---------|
| Source code (zip/tar.gz) | Repository snapshot | Automatic, provided by GitHub |
| `dashboard-assets.tar.gz` | Built `dist/` contents | Downloaded by binary at runtime |

The `dashboard-assets.tar.gz` contains the Vite build output:

```
dashboard-assets.tar.gz
├── index.html
└── assets/
    ├── index-[hash].js
    ├── index-[hash].css
    └── ...
```

## CI/Release Workflow

### GitHub Actions Workflow

File: `.github/workflows/release.yml`

Triggered by: pushing a version tag (`v*`)

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: assets/dashboard/package-lock.json

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Build dashboard assets
        run: |
          cd assets/dashboard
          npm ci
          npm run build
          tar -czf ../../dashboard-assets.tar.gz -C dist .

      - name: Run tests
        run: go test ./...

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: dashboard-assets.tar.gz
          generate_release_notes: true
```

### Release Process

1. Update version references if needed (CHANGELOG, etc.)
2. Create and push tag:
   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```
3. CI automatically:
   - Builds dashboard assets
   - Runs tests
   - Creates GitHub Release with `dashboard-assets.tar.gz`

## Binary Behavior

### Asset Resolution Order

When the dashboard server needs assets, it checks locations in this order:

1. **User cache**: `~/.schmux/dashboard/` (downloaded assets)
2. **Local dev path**: `./assets/dashboard/dist/` (for development)

```go
func getDashboardDistPath() string {
    // 1. User cache (downloaded assets)
    homeDir, _ := os.UserHomeDir()
    userAssets := filepath.Join(homeDir, ".schmux", "dashboard")
    if fileExists(filepath.Join(userAssets, "index.html")) {
        return userAssets
    }

    // 2. Local dev path
    localDev := "./assets/dashboard/dist"
    if fileExists(filepath.Join(localDev, "index.html")) {
        return localDev
    }

    return "" // Not found, will trigger download
}
```

### Asset Download Logic

On daemon start, before starting the HTTP server:

```go
func (d *Daemon) ensureDashboardAssets() error {
    // Dev builds skip download
    if version.Version == "dev" {
        return nil
    }

    assetsDir := filepath.Join(os.UserHomeDir(), ".schmux", "dashboard")
    versionFile := filepath.Join(assetsDir, ".version")

    // Check if correct version already cached
    if data, err := os.ReadFile(versionFile); err == nil {
        if strings.TrimSpace(string(data)) == version.Version {
            return nil // Already have correct version
        }
    }

    // Download from GitHub Release
    url := fmt.Sprintf(
        "https://github.com/schmux/schmux/releases/download/v%s/dashboard-assets.tar.gz",
        version.Version,
    )

    fmt.Printf("Downloading dashboard assets v%s...\n", version.Version)

    if err := downloadAndExtract(url, assetsDir); err != nil {
        return fmt.Errorf("failed to download dashboard assets: %w", err)
    }

    // Write version marker
    if err := os.WriteFile(versionFile, []byte(version.Version), 0644); err != nil {
        return fmt.Errorf("failed to write version file: %w", err)
    }

    fmt.Printf("Dashboard assets v%s installed.\n", version.Version)
    return nil
}
```

### Caching

- Assets cached in `~/.schmux/dashboard/`
- Version tracked in `~/.schmux/dashboard/.version`
- Cache is per-user, survives binary upgrades
- Cache invalidated when binary version changes

## Code Changes Required

### New Files

| File | Purpose |
|------|---------|
| `internal/version/version.go` | Version constant (set via ldflags) |
| `internal/assets/download.go` | Asset download and extraction logic |
| `.github/workflows/release.yml` | CI release workflow |

### Modified Files

| File | Change |
|------|--------|
| `internal/daemon/daemon.go` | Call `ensureDashboardAssets()` on startup |
| `internal/dashboard/server.go` | Update `getDashboardDistPath()` to check user cache first |
| `cmd/schmux/main.go` | Add `--version` flag support |

### File Structure

```
internal/
├── version/
│   └── version.go          # Version constant
├── assets/
│   └── download.go         # Download/extract logic
├── daemon/
│   └── daemon.go           # Modified: calls ensureDashboardAssets()
└── dashboard/
    └── server.go           # Modified: updated asset resolution

.github/
└── workflows/
    └── release.yml         # New: release automation
```

## User Experience

### Fresh Install

```bash
$ go install github.com/schmux/schmux/cmd/schmux@latest
$ schmux start
Downloading dashboard assets v1.2.3...
Dashboard assets v1.2.3 installed.
Starting daemon...
Dashboard available at http://localhost:7337
```

### Subsequent Runs

```bash
$ schmux start
Starting daemon...
Dashboard available at http://localhost:7337
```

No download—assets already cached.

### Upgrade

```bash
$ go install github.com/schmux/schmux/cmd/schmux@latest  # Gets v1.3.0
$ schmux start
Downloading dashboard assets v1.3.0...
Dashboard assets v1.3.0 installed.
Starting daemon...
Dashboard available at http://localhost:7337
```

Binary version changed, so new assets are fetched.

### Version Check

```bash
$ schmux --version
schmux v1.2.3
```

## Development Workflow

Local development remains unchanged:

```bash
# Build dashboard locally
go run ./cmd/build-dashboard

# Run daemon (uses local ./assets/dashboard/dist/)
go run ./cmd/schmux start
```

The asset resolution checks `./assets/dashboard/dist/` as a fallback, so local dev works without downloading.

For dev builds (version = "dev"), the download step is skipped entirely.

### Testing Release Behavior Locally

To test the download mechanism locally:

```bash
# Build with a specific version
go build -ldflags "-X github.com/schmux/schmux/internal/version.Version=1.2.3" ./cmd/schmux

# Clear cache
rm -rf ~/.schmux/dashboard

# Run (will attempt download)
./schmux start
```

## Error Handling

### Network Failure

```
$ schmux start
Downloading dashboard assets v1.2.3...
Error: failed to download dashboard assets: Get "https://...": dial tcp: no such host

Dashboard assets are required. Please check your network connection and try again.
Alternatively, build from source: go run ./cmd/build-dashboard && go run ./cmd/schmux start
```

### Missing Release Artifact

If the release exists but `dashboard-assets.tar.gz` is missing:

```
$ schmux start
Downloading dashboard assets v1.2.3...
Error: failed to download dashboard assets: 404 Not Found

This version's dashboard assets are not available.
Please report this issue or build from source.
```

### Corrupted Download

If extraction fails, the partial download is cleaned up:

```go
func downloadAndExtract(url, destDir string) error {
    // Download to temp file
    tmpFile, err := os.CreateTemp("", "schmux-assets-*.tar.gz")
    if err != nil {
        return err
    }
    defer os.Remove(tmpFile.Name())

    // ... download ...

    // Extract to temp directory first
    tmpDir, err := os.MkdirTemp("", "schmux-assets-")
    if err != nil {
        return err
    }
    defer os.RemoveAll(tmpDir)

    // ... extract ...

    // Only move to final location if extraction succeeded
    os.RemoveAll(destDir)
    return os.Rename(tmpDir, destDir)
}
```

### Offline Mode (Future Enhancement)

Not in initial scope, but could add:

```bash
$ schmux start --offline
Error: Dashboard assets not found and --offline specified.
Run without --offline to download, or build from source.
```

## Security Considerations

### Download Source

Assets are only downloaded from the official GitHub Releases URL:

```
https://github.com/schmux/schmux/releases/download/v{VERSION}/dashboard-assets.tar.gz
```

The URL is constructed from the compiled-in version, not user input.

### HTTPS Only

All downloads use HTTPS. HTTP is not supported.

### No Code Execution

Downloaded assets are static files (HTML, JS, CSS) served by the Go HTTP server. The binary does not execute downloaded code.

### Checksum Verification (Future Enhancement)

Not in initial scope, but could add SHA256 verification:

1. Publish `dashboard-assets.tar.gz.sha256` alongside the tarball
2. Verify checksum after download, before extraction

## Summary

| Aspect | Approach |
|--------|----------|
| Distribution | `go install` + runtime asset download |
| Version source | Git tags (`v1.2.3`) |
| Version in binary | ldflags injection |
| Asset storage | `~/.schmux/dashboard/` |
| Asset source | GitHub Releases |
| Dev workflow | Unchanged (local `dist/` fallback) |
| CI | GitHub Actions on tag push |
