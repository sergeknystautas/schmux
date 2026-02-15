package tunnel

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const cloudflaredBaseURL = "https://github.com/cloudflare/cloudflared/releases/latest/download"

func cloudflaredDownloadURL(goos, goarch string) string {
	if goos == "darwin" {
		return fmt.Sprintf("%s/cloudflared-%s-%s.tgz", cloudflaredBaseURL, goos, goarch)
	}
	return fmt.Sprintf("%s/cloudflared-%s-%s", cloudflaredBaseURL, goos, goarch)
}

// FindCloudflared looks for the cloudflared binary, first on PATH, then in the
// provided schmux bin directory. Returns the absolute path to the binary or an error.
func FindCloudflared(schmuxBinDir string) (string, error) {
	if path, err := exec.LookPath("cloudflared"); err == nil {
		return path, nil
	}

	if schmuxBinDir != "" {
		candidate := filepath.Join(schmuxBinDir, "cloudflared")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("cloudflared not found (not on PATH, not in %s)", schmuxBinDir)
}

// EnsureCloudflared returns the path to a cloudflared binary, downloading it
// from GitHub releases if not already available on PATH or in the schmux bin dir.
func EnsureCloudflared(schmuxBinDir string) (string, error) {
	path, err := FindCloudflared(schmuxBinDir)
	if err == nil {
		return path, nil
	}

	fmt.Printf("[remote-access] downloading cloudflared...\n")
	if err := os.MkdirAll(schmuxBinDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin dir: %w", err)
	}

	downloadURL := cloudflaredDownloadURL(runtime.GOOS, runtime.GOARCH)
	destPath := filepath.Join(schmuxBinDir, "cloudflared")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download cloudflared: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download cloudflared: HTTP %d", resp.StatusCode)
	}

	if runtime.GOOS == "darwin" {
		if err := extractTgz(resp.Body, destPath); err != nil {
			return "", fmt.Errorf("failed to extract cloudflared: %w", err)
		}
	} else {
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create cloudflared binary: %w", err)
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			os.Remove(destPath)
			return "", fmt.Errorf("failed to write cloudflared binary: %w", err)
		}
		f.Close()
	}

	fmt.Printf("[remote-access] cloudflared downloaded to %s\n", destPath)
	return destPath, nil
}

func extractTgz(r io.Reader, destPath string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("cloudflared binary not found in archive")
		}
		if err != nil {
			return err
		}

		if filepath.Base(header.Name) == "cloudflared" {
			f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			return f.Close()
		}
	}
}
