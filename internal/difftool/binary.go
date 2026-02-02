package difftool

import "os"

// IsBinaryFile checks if a file is binary by looking for null bytes in the first 8KB.
// This is the same heuristic git uses.
func IsBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}
