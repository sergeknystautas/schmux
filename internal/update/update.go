// Package update provides self-update functionality for schmux.
package update

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/sergeknystautas/schmux/internal/assets"
	"github.com/sergeknystautas/schmux/internal/version"
)

const (
	// GitHubAPILatestRelease is the URL for fetching the latest release info.
	GitHubAPILatestRelease = "https://api.github.com/repos/sergeknystautas/schmux/releases/latest"

	// GitHubReleaseBinaryTemplate is the URL template for downloading binaries.
	// %s is the version (without 'v' prefix), %s is OS, %s is arch.
	GitHubReleaseBinaryTemplate = "https://github.com/sergeknystautas/schmux/releases/download/v%s/schmux-%s-%s"

	// GitHubReleaseChecksumsTemplate is the URL template for downloading checksums.
	GitHubReleaseChecksumsTemplate = "https://github.com/sergeknystautas/schmux/releases/download/v%s/checksums.txt"

	// httpTimeout is the timeout for all HTTP operations.
	httpTimeout = 30 * time.Second
)

// httpClient is a shared HTTP client with timeout.
var httpClient = &http.Client{
	Timeout: httpTimeout,
}

// Update checks for and applies updates to the schmux binary and dashboard assets.
func Update() error {
	current := version.Version
	if current == "dev" {
		return fmt.Errorf("cannot update dev builds - build from source instead")
	}

	// Check platform support
	if err := checkPlatformSupport(); err != nil {
		return err
	}

	fmt.Printf("[daemon] current version: %s\n", current)
	fmt.Println("Checking for updates...")

	latest, err := GetLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	// Use semver comparison to check if update is needed
	vLatest, err := semver.NewVersion("v" + latest)
	if err != nil {
		return fmt.Errorf("failed to parse latest version: %w", err)
	}
	vCurrent, err := semver.NewVersion("v" + current)
	if err != nil {
		return fmt.Errorf("failed to parse current version: %w", err)
	}

	if !vLatest.GreaterThan(vCurrent) {
		fmt.Println("Already up to date.")
		return nil
	}

	fmt.Printf("[daemon] new version available: %s\n", latest)

	// Download checksums first
	checksums, err := downloadChecksums(latest)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Download and verify assets FIRST (so failure leaves system in working state)
	if err := downloadAndInstallAssets(latest, checksums); err != nil {
		return fmt.Errorf("failed to update dashboard assets: %w", err)
	}

	// Download and verify binary LAST
	if err := downloadAndInstallBinary(latest, checksums); err != nil {
		return fmt.Errorf("failed to update binary: %w", err)
	}

	fmt.Println("Updated successfully. Restart schmux to use the new version.")
	return nil
}

// checkPlatformSupport returns an error if the current platform is not supported.
func checkPlatformSupport() error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	supported := map[string][]string{
		"darwin": {"amd64", "arm64"},
		"linux":  {"amd64", "arm64"},
	}

	archs, ok := supported[goos]
	if !ok {
		return fmt.Errorf("unsupported operating system: %s (schmux supports macOS and Linux)", goos)
	}

	for _, arch := range archs {
		if arch == goarch {
			return nil
		}
	}

	return fmt.Errorf("unsupported architecture: %s/%s", goos, goarch)
}

// CheckForUpdate checks if a newer version is available without installing it.
// Returns the latest version and whether an update is available.
func CheckForUpdate() (latestVersion string, updateAvailable bool, err error) {
	current := version.Version
	if current == "dev" {
		return "", false, nil
	}

	latest, err := GetLatestVersion()
	if err != nil {
		return "", false, err
	}

	// Parse versions with semver (add "v" prefix for semver package)
	v1, err := semver.NewVersion("v" + latest)
	if err != nil {
		// If parsing fails, assume update not available
		return latest, false, nil
	}
	v2, err := semver.NewVersion("v" + current)
	if err != nil {
		// If parsing fails, assume update not available
		return latest, false, nil
	}

	return latest, v1.GreaterThan(v2), nil
}

// GetLatestVersion fetches the latest release version from GitHub.
func GetLatestVersion() (string, error) {
	resp, err := httpClient.Get(GitHubAPILatestRelease)
	if err != nil {
		return "", fmt.Errorf("failed to fetch release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("GitHub API rate limit exceeded - try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse release info: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no release tag found")
	}

	// Strip "v" prefix
	return strings.TrimPrefix(release.TagName, "v"), nil
}

// downloadChecksums fetches and parses the checksums.txt file for a release.
// Returns a map of filename -> expected SHA256 hash.
func downloadChecksums(ver string) (map[string]string, error) {
	url := fmt.Sprintf(GitHubReleaseChecksumsTemplate, ver)

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "hash  filename" (two spaces between hash and filename)
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hash := parts[0]
			filename := parts[len(parts)-1] // Handle potential extra spaces
			checksums[filename] = hash
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse checksums: %w", err)
	}

	return checksums, nil
}

// downloadAndInstallBinary downloads the binary for the current platform,
// verifies its checksum, and replaces the current executable.
func downloadAndInstallBinary(ver string, checksums map[string]string) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	binaryName := fmt.Sprintf("schmux-%s-%s", goos, goarch)

	expectedHash, ok := checksums[binaryName]
	if !ok {
		return fmt.Errorf("no checksum found for %s", binaryName)
	}

	url := fmt.Sprintf(GitHubReleaseBinaryTemplate, ver, goos, goarch)
	fmt.Printf("[daemon] downloading schmux v%s for %s/%s...\n", ver, goos, goarch)

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "schmux-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	resp, err := httpClient.Get(url)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	// Download and compute hash simultaneously
	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	// Verify checksum
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	fmt.Println("Checksum verified.")

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to make executable: %w", err)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Replace current binary
	// On Unix, we can rename over a running binary
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Rename might fail (cross-filesystem), try copy instead
		if err := copyFile(tmpPath, execPath); err != nil {
			return fmt.Errorf("failed to replace binary: %w", err)
		}
	}

	return nil
}

// downloadAndInstallAssets downloads and installs dashboard assets for the given version.
func downloadAndInstallAssets(ver string, checksums map[string]string) error {
	const assetsFilename = "dashboard-assets.tar.gz"

	expectedHash, ok := checksums[assetsFilename]
	if !ok {
		return fmt.Errorf("no checksum found for %s", assetsFilename)
	}

	assetsDir, err := assets.GetUserAssetsDir()
	if err != nil {
		return err
	}

	url := fmt.Sprintf(assets.GitHubReleaseURLTemplate, ver)
	fmt.Printf("[daemon] downloading dashboard assets v%s...\n", ver)

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "schmux-assets-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	resp, err := httpClient.Get(url)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	// Download and compute hash simultaneously
	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	// Verify checksum
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch for assets: expected %s, got %s", expectedHash, actualHash)
	}
	fmt.Println("Assets checksum verified.")

	// Extract using existing logic
	if err := assets.ExtractTarGzToDir(tmpPath, assetsDir); err != nil {
		return fmt.Errorf("failed to extract assets: %w", err)
	}

	// Write version marker
	versionFile := filepath.Join(assetsDir, ".version")
	if err := os.WriteFile(versionFile, []byte(ver), 0644); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}

	fmt.Printf("[daemon] dashboard assets v%s installed\n", ver)
	return nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Preserve executable permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}
