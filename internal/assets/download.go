// Package assets handles downloading and managing dashboard assets.
package assets

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/version"
)

const (
	// GitHubReleaseURLTemplate is the URL template for downloading dashboard assets.
	// %s is replaced with the version (without 'v' prefix).
	GitHubReleaseURLTemplate = "https://github.com/sergeknystautas/schmux/releases/download/v%s/dashboard-assets.tar.gz"
)

// GetUserAssetsDir returns the path to the user's cached dashboard assets.
func GetUserAssetsDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".schmux", "dashboard"), nil
}

// GetCachedVersion returns the version of cached assets, or empty string if none.
func GetCachedVersion() string {
	assetsDir, err := GetUserAssetsDir()
	if err != nil {
		return ""
	}

	versionFile := filepath.Join(assetsDir, ".version")
	data, err := os.ReadFile(versionFile)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

// NeedsDownload returns true if dashboard assets need to be downloaded.
// Returns false for dev builds or if correct version is already cached.
func NeedsDownload() bool {
	// Dev builds don't download
	if version.Version == "dev" {
		return false
	}

	// Check if correct version is cached
	return GetCachedVersion() != version.Version
}

// EnsureAssets ensures dashboard assets are available.
// Downloads from GitHub releases if needed.
func EnsureAssets() error {
	if !NeedsDownload() {
		return nil
	}

	assetsDir, err := GetUserAssetsDir()
	if err != nil {
		return err
	}

	url := fmt.Sprintf(GitHubReleaseURLTemplate, version.Version)
	fmt.Printf("Downloading dashboard assets v%s...\n", version.Version)

	if err := DownloadAndExtract(url, assetsDir); err != nil {
		return fmt.Errorf("failed to download dashboard assets: %w", err)
	}

	// Write version marker
	versionFile := filepath.Join(assetsDir, ".version")
	if err := os.WriteFile(versionFile, []byte(version.Version), 0644); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}

	fmt.Printf("Dashboard assets v%s installed.\n", version.Version)
	return nil
}

// DownloadAndExtract downloads a tar.gz file and extracts it to destDir.
func DownloadAndExtract(url, destDir string) error {
	// Download to temp file
	tmpFile, err := os.CreateTemp("", "schmux-assets-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Download
	resp, err := http.Get(url)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	return ExtractTarGzToDir(tmpPath, destDir)
}

// ExtractTarGzToDir extracts a tar.gz file to the destination directory atomically.
// It extracts to a temp directory first, then moves to the final location.
func ExtractTarGzToDir(tarGzPath, destDir string) error {
	// Extract to temp directory first (atomic operation)
	tmpDir, err := os.MkdirTemp("", "schmux-assets-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarGz(tarGzPath, tmpDir); err != nil {
		return fmt.Errorf("failed to extract: %w", err)
	}

	// Remove old assets dir if it exists
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("failed to remove old assets: %w", err)
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create parent dir: %w", err)
	}

	// Move temp dir to final location
	if err := os.Rename(tmpDir, destDir); err != nil {
		// Rename might fail across filesystems, fall back to copy
		if err := copyDir(tmpDir, destDir); err != nil {
			return fmt.Errorf("failed to move assets: %w", err)
		}
	}

	return nil
}

// extractTarGz extracts a tar.gz file to the destination directory.
func extractTarGz(tarGzPath, destDir string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Strip leading "./" or "/" from tar entry names
		name := strings.TrimPrefix(header.Name, "./")
		name = strings.TrimPrefix(name, "/")
		if name == "" || name == "." {
			// Skip root entries
			continue
		}

		// Sanitize path to prevent path traversal
		target := filepath.Join(destDir, name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}
			// Limit copy size to prevent decompression bombs (100MB should be plenty)
			if _, err := io.CopyN(outFile, tr, 100*1024*1024); err != nil && err != io.EOF {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}
