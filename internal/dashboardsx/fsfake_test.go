//go:build !nodashboardsx

package dashboardsx

import (
	"io/fs"
	"os"
	"testing"
	"time"
)

type memFile struct {
	data []byte
	mode os.FileMode
}

// installMemFS routes the package fs seam through an in-memory map for the test's
// duration and returns the backing store so assertions can inspect writes. The
// seam vars are package globals, so tests using it must not call t.Parallel().
func installMemFS(t *testing.T) map[string]memFile {
	t.Helper()
	store := map[string]memFile{}
	ow, or, ost := writeFile, readFile, statFile
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		store[name] = memFile{data: append([]byte(nil), data...), mode: perm}
		return nil
	}
	readFile = func(name string) ([]byte, error) {
		f, ok := store[name]
		if !ok {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
		return append([]byte(nil), f.data...), nil
	}
	statFile = func(name string) (os.FileInfo, error) {
		f, ok := store[name]
		if !ok {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
		}
		return memFileInfo{name: name, size: int64(len(f.data)), mode: f.mode}, nil
	}
	t.Cleanup(func() { writeFile, readFile, statFile = ow, or, ost })
	return store
}

type memFileInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (i memFileInfo) Name() string       { return i.name }
func (i memFileInfo) Size() int64        { return i.size }
func (i memFileInfo) Mode() os.FileMode  { return i.mode }
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return false }
func (i memFileInfo) Sys() any           { return nil }
