// Package attachment stores user-supplied files under an os.Root, so a
// symlink inside the root can never redirect a write outside it, and formats
// the prompt block that points an agent at them.
package attachment

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// MaxSize is the largest file an attachment endpoint accepts.
const MaxSize = 50 << 20

// ErrReceive marks a failure while reading or copying the source, as opposed
// to preparing storage. The underlying error (for example *http.MaxBytesError)
// stays in the chain.
var ErrReceive = errors.New("attachment: receive failed")

// ValidName reports whether name is usable as a single path element.
func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 &&
		!strings.ContainsAny(name, `/\`) && !strings.ContainsFunc(name, unicode.IsControl)
}

// Save streams src into parent/<uuid>/name under root and returns that path
// relative to root. The file is published (renamed from a temporary name) only
// after src is fully written; on any failure the uuid directory is removed.
func Save(root *os.Root, parent, name string, src io.Reader) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("attachment: invalid filename %q", name)
	}
	if err := root.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("attachment: create %s: %w", parent, err)
	}
	dir := filepath.Join(parent, uuid.NewString())
	if err := root.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("attachment: create %s: %w", dir, err)
	}
	saved := false
	defer func() {
		if !saved {
			_ = root.RemoveAll(dir)
		}
	}()
	sub, err := root.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("attachment: open %s: %w", dir, err)
	}
	defer sub.Close()
	f, err := sub.OpenFile(".upload", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("attachment: create upload: %w", err)
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil {
		return "", fmt.Errorf("%w: %w", ErrReceive, copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("attachment: close upload: %w", closeErr)
	}
	if err := sub.Rename(".upload", name); err != nil {
		return "", fmt.Errorf("attachment: publish %s: %w", name, err)
	}
	saved = true
	return filepath.Join(dir, name), nil
}

// AppendFileList appends the "File attachments:" block the chat composer
// writes (withFileAttachments in assets/dashboard/src/lib/attachments.ts).
func AppendFileList(prompt string, paths []string) string {
	if len(paths) == 0 {
		return prompt
	}
	block := "File attachments:\n" + strings.Join(paths, "\n")
	if prompt == "" {
		return block
	}
	return prompt + "\n\n" + block
}
