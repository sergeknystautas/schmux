package tunnel

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// pkgLogger is the package-level logger for tunnel operations.
// Set via SetLogger from the daemon initialization.
var pkgLogger *log.Logger

// SetLogger sets the package-level logger for tunnel operations.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

const cloudflaredBaseURL = "https://github.com/cloudflare/cloudflared/releases/latest/download"

// maxCloudflaredSize is the maximum allowed download size for the cloudflared binary (200MB).
const maxCloudflaredSize = 200 << 20

// cloudflareTeamID is the Apple Developer Team Identifier for Cloudflare Inc.
// Used to verify macOS code signatures on auto-downloaded binaries.
const cloudflareTeamID = "68WVV388M8"

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

	if pkgLogger != nil {
		pkgLogger.Info("cloudflared not found, recommending install", "suggestion", installSuggestion(runtime.GOOS))
	}
	if pkgLogger != nil {
		pkgLogger.Info("falling back to auto-download")
	}
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
		if err := extractTgz(io.LimitReader(resp.Body, maxCloudflaredSize), destPath); err != nil {
			return "", fmt.Errorf("failed to extract cloudflared: %w", err)
		}
	} else {
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create cloudflared binary: %w", err)
		}
		if _, err := io.Copy(f, io.LimitReader(resp.Body, maxCloudflaredSize)); err != nil {
			f.Close()
			os.Remove(destPath)
			return "", fmt.Errorf("failed to write cloudflared binary: %w", err)
		}
		f.Close()
	}

	if pkgLogger != nil {
		pkgLogger.Info("cloudflared downloaded", "path", destPath)
	}

	// Log SHA256 hash for audit trail
	if hash, err := fileSHA256(destPath); err == nil {
		if pkgLogger != nil {
			pkgLogger.Info("cloudflared sha256", "hash", hash)
		}
	}

	// Verify code signature (macOS only; logs warning on other platforms)
	if err := verifyCloudflaredSignature(destPath); err != nil {
		os.Remove(destPath)
		return "", fmt.Errorf("cloudflared signature verification failed: %w", err)
	}

	return destPath, nil
}

// verifyCloudflaredSignature checks the code signature of a downloaded cloudflared binary.
// On macOS, it uses Apple's codesign tool to verify the binary is signed by Cloudflare Inc.
// with the expected team identifier (68WVV388M8) and a valid Apple certificate chain.
// On other platforms, it logs a warning since no signature verification is available.
func verifyCloudflaredSignature(binPath string) error {
	if runtime.GOOS != "darwin" {
		if pkgLogger != nil {
			pkgLogger.Warn("signature verification is only available on macOS; consider installing cloudflared via your package manager for verified binaries")
		}
		return nil
	}

	return verifyCodesign(binPath)
}

// verifyCodesign runs macOS codesign verification and checks the certificate chain.
func verifyCodesign(binPath string) error {
	// Step 1: Verify the signature is valid (checks integrity + certificate chain)
	verifyCmd := exec.Command("codesign", "--verify", "--deep", "--strict", binPath)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("codesign verification failed: %s (%w)", strings.TrimSpace(string(output)), err)
	}

	// Step 2: Extract signing identity and verify it belongs to Cloudflare
	infoCmd := exec.Command("codesign", "-dvv", binPath)
	// codesign -dvv writes to stderr
	output, err := infoCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to read codesign info: %s (%w)", strings.TrimSpace(string(output)), err)
	}

	info := string(output)

	// Verify TeamIdentifier matches Cloudflare's Apple Developer Team ID
	if !strings.Contains(info, "TeamIdentifier="+cloudflareTeamID) {
		return fmt.Errorf("binary is signed but not by Cloudflare (expected TeamIdentifier=%s)", cloudflareTeamID)
	}

	// Verify at least one Authority line mentions Cloudflare
	hasCloudflareAuthority := false
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "Authority=") && strings.Contains(line, "Cloudflare") {
			hasCloudflareAuthority = true
			break
		}
	}
	if !hasCloudflareAuthority {
		return fmt.Errorf("binary is signed but Authority chain does not contain Cloudflare")
	}

	if pkgLogger != nil {
		pkgLogger.Info("cloudflared signature verified", "authority", "Cloudflare Inc.", "team", cloudflareTeamID)
	}
	return nil
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
			if header.Size > maxCloudflaredSize {
				return fmt.Errorf("tar entry %q is %d bytes, exceeds maximum allowed size of %d", header.Name, header.Size, maxCloudflaredSize)
			}
			f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			n, err := io.Copy(f, io.LimitReader(tr, maxCloudflaredSize+1))
			if err != nil {
				f.Close()
				return err
			}
			if n > maxCloudflaredSize {
				f.Close()
				os.Remove(destPath)
				return fmt.Errorf("extracted cloudflared binary exceeds maximum allowed size of %d bytes", maxCloudflaredSize)
			}
			return f.Close()
		}
	}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func installSuggestion(goos string) string {
	switch goos {
	case "darwin":
		return "brew install cloudflared"
	case "linux":
		return "sudo apt install cloudflared  # or: sudo yum install cloudflared"
	default:
		return "install cloudflared from https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"
	}
}
