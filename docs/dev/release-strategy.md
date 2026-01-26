# Release Strategy

This document specifies how schmux is distributed to end users, covering versioning, release artifacts, installation, and self-updating.

## Problem Statement

schmux needs to be distributed to Mac and Linux users who may not have Go installed. The previous `go install` approach only works for Go developers and still requires workarounds for dashboard assets.

## Solution Overview

**Pre-built binaries with self-update**: Release platform-specific binaries via GitHub Releases. Users install via a simple curl script, and the binary can update itself.

1. GitHub Actions builds binaries for all platforms on tag push
2. Install script detects OS/arch and downloads the correct binary
3. `schmux update` checks for new versions and replaces itself

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
go build -ldflags "-X github.com/sergeknystautas/schmux/internal/version.Version=1.2.3" ./cmd/schmux
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

## Platform Matrix

Binaries are built for:

| OS | Architecture | Binary Name |
|----|--------------|-------------|
| macOS | Intel | `schmux-darwin-amd64` |
| macOS | Apple Silicon | `schmux-darwin-arm64` |
| Linux | x86_64 | `schmux-linux-amd64` |
| Linux | ARM64 | `schmux-linux-arm64` |

## Release Artifacts

Each GitHub Release (e.g., `v1.2.3`) includes:

| Artifact | Purpose |
|----------|---------|
| `schmux-darwin-amd64` | macOS Intel binary |
| `schmux-darwin-arm64` | macOS Apple Silicon binary |
| `schmux-linux-amd64` | Linux x86_64 binary |
| `schmux-linux-arm64` | Linux ARM64 binary |
| `dashboard-assets.tar.gz` | Pre-built React dashboard |
| `checksums.txt` | SHA256 checksums for all artifacts |

Dashboard assets are distributed separately from the binary. The install script and `schmux update` command download both the binary and assets, extracting assets to `~/.schmux/dashboard/`.

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

      - name: Extract version
        id: version
        run: echo "VERSION=${GITHUB_REF#refs/tags/v}" >> $GITHUB_OUTPUT

      - name: Build binaries
        run: |
          VERSION=${{ steps.version.outputs.VERSION }}
          LDFLAGS="-X github.com/sergeknystautas/schmux/internal/version.Version=${VERSION}"

          GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o schmux-darwin-amd64 ./cmd/schmux
          GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o schmux-darwin-arm64 ./cmd/schmux
          GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o schmux-linux-amd64 ./cmd/schmux
          GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o schmux-linux-arm64 ./cmd/schmux

      - name: Generate checksums
        run: sha256sum schmux-* dashboard-assets.tar.gz > checksums.txt

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: |
            schmux-darwin-amd64
            schmux-darwin-arm64
            schmux-linux-amd64
            schmux-linux-arm64
            dashboard-assets.tar.gz
            checksums.txt
          generate_release_notes: true
```

### Release Process

**Before tagging**, update documentation:

1. **Update CHANGES.md** - Add a new entry at the top with a high-level description of what changed in this release:
   ```markdown
   ## Version 1.2.3 (2025-01-15)

   - Added feature X for doing Y
   - Fixed bug where Z would happen
   - Improved performance of W
   ```

2. **Update README.md** - Ensure the latest version is reflected in the Status section:
   ```markdown
   ## Status

   **v1.2.3** - Stable for daily use
   ```

3. **Add CHANGES.md link to README** - If not already present, add a link to CHANGES.md in the Documentation section or Status section so users can easily find what changed.

**Then create and push the tag**:

4. Create and push tag:
   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```

5. CI automatically:
   - Builds dashboard assets
   - Runs tests
   - Cross-compiles binaries for all platforms
   - Generates checksums
   - Creates GitHub Release with all artifacts

**Release note (v1.0.1 assets repack):** If dashboard assets need to be re-uploaded for an existing release, repackage them without a `./` root entry (e.g., `tar -czf ../../dashboard-assets.tar.gz -C dist $(ls -A)`) and regenerate `checksums.txt` using the release artifacts so the updater can find `dashboard-assets.tar.gz` by filename.

## Installation

### Install Script

Users install via curl one-liner:

```bash
curl -fsSL https://raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh | bash
```

The install script:

1. Detects OS (`darwin` or `linux`)
2. Detects architecture (`amd64` or `arm64`)
3. Fetches latest release version from GitHub API
4. Downloads the correct binary
5. Verifies checksum
6. Installs to `~/.local/bin/schmux` (or `/usr/local/bin` with sudo)
7. Prints success message with next steps

```bash
#!/bin/bash
set -e

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Get latest version
LATEST=$(curl -fsSL https://api.github.com/repos/sergeknystautas/schmux/releases/latest | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')

# Download binary
BINARY="schmux-${OS}-${ARCH}"
URL="https://github.com/sergeknystautas/schmux/releases/download/v${LATEST}/${BINARY}"

echo "Downloading schmux v${LATEST} for ${OS}/${ARCH}..."
curl -fsSL -o /tmp/schmux "$URL"
chmod +x /tmp/schmux

# Install
INSTALL_DIR="${HOME}/.local/bin"
mkdir -p "$INSTALL_DIR"
mv /tmp/schmux "$INSTALL_DIR/schmux"

echo "Installed schmux v${LATEST} to ${INSTALL_DIR}/schmux"
echo ""
echo "Make sure ${INSTALL_DIR} is in your PATH:"
echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
```

### Homebrew (Mac)

For Mac users who prefer Homebrew, a tap can be added later:

```bash
brew tap anthropics/tap
brew install schmux
```

This is not in initial scope but can be added by creating a `homebrew-tap` repository with a formula that points to the GitHub Release binaries.

## Self-Update

### `schmux update` Command

The binary can update itself:

```bash
$ schmux update
Current version: 1.2.3
Checking for updates...
New version available: 1.3.0
Downloading schmux v1.3.0...
Updated successfully. Restart schmux to use the new version.
```

### Implementation

```go
// internal/update/update.go
package update

func Update() error {
    current := version.Version
    if current == "dev" {
        return fmt.Errorf("cannot update dev builds")
    }

    // Get latest release from GitHub API
    latest, err := getLatestVersion()
    if err != nil {
        return fmt.Errorf("failed to check for updates: %w", err)
    }

    if latest == current {
        fmt.Println("Already up to date.")
        return nil
    }

    fmt.Printf("New version available: %s\n", latest)
    fmt.Printf("Downloading schmux v%s...\n", latest)

    // Determine platform
    goos := runtime.GOOS
    goarch := runtime.GOARCH
    binary := fmt.Sprintf("schmux-%s-%s", goos, goarch)

    // Download to temp file
    url := fmt.Sprintf(
        "https://github.com/sergeknystautas/schmux/releases/download/v%s/%s",
        latest, binary,
    )

    tmpFile, err := downloadToTemp(url)
    if err != nil {
        return fmt.Errorf("download failed: %w", err)
    }
    defer os.Remove(tmpFile)

    // Get current executable path
    execPath, err := os.Executable()
    if err != nil {
        return fmt.Errorf("cannot determine executable path: %w", err)
    }
    execPath, err = filepath.EvalSymlinks(execPath)
    if err != nil {
        return fmt.Errorf("cannot resolve executable path: %w", err)
    }

    // Replace current binary
    if err := replaceBinary(tmpFile, execPath); err != nil {
        return fmt.Errorf("failed to replace binary: %w", err)
    }

    fmt.Println("Updated successfully. Restart schmux to use the new version.")
    return nil
}

func getLatestVersion() (string, error) {
    resp, err := http.Get("https://api.github.com/repos/sergeknystautas/schmux/releases/latest")
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var release struct {
        TagName string `json:"tag_name"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
        return "", err
    }

    // Strip "v" prefix
    return strings.TrimPrefix(release.TagName, "v"), nil
}

func replaceBinary(src, dst string) error {
    // Make new binary executable
    if err := os.Chmod(src, 0755); err != nil {
        return err
    }

    // On Unix, we can rename over a running binary
    return os.Rename(src, dst)
}
```

### Update Check on Startup (Optional)

The daemon can optionally check for updates on startup and notify the user:

```
$ schmux start
A new version of schmux is available (1.3.0). Run 'schmux update' to upgrade.
Starting daemon...
```

This is a passive notification only—no automatic updates.

## Code Structure

### New Files

| File | Purpose |
|------|---------|
| `internal/version/version.go` | Version constant (set via ldflags) |
| `internal/update/update.go` | Self-update logic |
| `install.sh` | Installation script |
| `.github/workflows/release.yml` | CI release workflow |

### Modified Files

| File | Change |
|------|--------|
| `cmd/schmux/main.go` | Add `update` command, `--version` flag |

## User Experience

### Fresh Install

```bash
$ curl -fsSL https://raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh | bash
Downloading schmux v1.2.3 for darwin/arm64...
Installed schmux v1.2.3 to /Users/you/.local/bin/schmux

Make sure /Users/you/.local/bin is in your PATH:
  export PATH="$HOME/.local/bin:$PATH"

$ schmux start
Starting daemon...
Dashboard available at http://localhost:7337
```

### Check Version

```bash
$ schmux --version
schmux v1.2.3
```

### Update

```bash
$ schmux update
Current version: 1.2.3
Checking for updates...
New version available: 1.3.0
Downloading schmux v1.3.0...
Updated successfully. Restart schmux to use the new version.
```

### Already Up to Date

```bash
$ schmux update
Current version: 1.3.0
Checking for updates...
Already up to date.
```

## Development Workflow

Local development remains unchanged:

```bash
# Build dashboard locally
go run ./cmd/build-dashboard

# Run daemon
go run ./cmd/schmux start
```

For dev builds (version = "dev"), the `update` command is disabled:

```bash
$ go run ./cmd/schmux update
Error: cannot update dev builds
```

## Security Considerations

### Download Source

Binaries are only downloaded from official GitHub Releases:

```
https://github.com/sergeknystautas/schmux/releases/download/v{VERSION}/schmux-{OS}-{ARCH}
```

### HTTPS Only

All downloads use HTTPS.

### Checksum Verification

The install script and update command should verify SHA256 checksums against `checksums.txt` from the release.

### Binary Replacement

The update command replaces the binary at its current location. This requires write permission to that location. If installed system-wide, `schmux update` may need sudo.

## Summary

| Aspect | Approach |
|--------|----------|
| Distribution | Pre-built binaries via GitHub Releases |
| Installation | `curl \| bash` install script |
| Updates | `schmux update` self-updates |
| Version source | Git tags (`v1.2.3`) |
| Version in binary | ldflags injection |
| Platforms | darwin/linux × amd64/arm64 |
| Dashboard assets | Downloaded separately to ~/.schmux/dashboard/ |
| CI | GitHub Actions on tag push |
