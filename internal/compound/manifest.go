package compound

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
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
	if err != nil && err != io.EOF {
		return false
	}
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}
