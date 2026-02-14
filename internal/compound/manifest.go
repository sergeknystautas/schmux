package compound

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxBinaryCheckSize = 512

// FileHash computes the SHA-256 hex digest of a file.
func FileHash(path string) (string, error) {
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

// HashBytes computes the SHA-256 hex digest of a byte slice.
func HashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// IsBinary checks if a file appears to be binary by looking for null bytes
// in the first 512 bytes.
func IsBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, maxBinaryCheckSize)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}

// ValidateRelPath checks that a relative path does not escape the base directory
// via ".." traversal. Returns an error if the path is unsafe.
func ValidateRelPath(relPath string) error {
	if relPath == "" {
		return fmt.Errorf("empty relative path")
	}
	// Clean the path and check for traversal
	cleaned := filepath.Clean(relPath)
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("absolute path not allowed: %s", relPath)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path traversal not allowed: %s", relPath)
	}
	return nil
}
