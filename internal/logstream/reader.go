package logstream

import (
	"bytes"
	"errors"
	"io"
	"os"
)

const (
	readChunkSize = 64 * 1024
	maxLineSize   = 1024 * 1024
)

var errLineTooLong = errors.New("log line exceeds 1 MiB")

// pageRead is the internal result of readPageBefore. Before is the exact byte
// offset at which the oldest returned line begins (zero when history is
// exhausted). It never leaves this package.
type pageRead struct {
	Lines   [][]byte
	Before  int64
	HasMore bool
}

type locatedLine struct {
	data  []byte
	start int64
}

// readPageBefore returns up to limit non-empty lines ending before the byte
// offset before, newest-first. It walks backward in bounded chunks and records
// each line's start offset while parsing, so the next page never has to rescan
// the file or identify a line by its contents.
func readPageBefore(path string, before int64, limit int) (pageRead, error) {
	if limit <= 0 || before <= 0 {
		return pageRead{Before: 0}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return pageRead{Before: 0}, nil
		}
		return pageRead{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return pageRead{}, err
	}
	if before > info.Size() {
		before = info.Size()
	}
	if before <= 0 {
		return pageRead{Before: 0}, nil
	}

	// One extra located line makes HasMore authoritative without another read.
	want := limit + 1
	found := make([]locatedLine, 0, want)
	var carry []byte
	cursor := before

	for cursor > 0 && len(found) < want {
		start := cursor - readChunkSize
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, cursor-start)
		n, err := f.ReadAt(chunk, start)
		if err != nil && !errors.Is(err, io.EOF) {
			return pageRead{}, err
		}
		chunk = chunk[:n]

		// carry is the suffix of a line that began in an older chunk. Prefixing
		// it with this chunk completes that line once its preceding newline is
		// found.
		block := make([]byte, len(chunk)+len(carry))
		copy(block, chunk)
		copy(block[len(chunk):], carry)

		end := len(block)
		for len(found) < want {
			newline := bytes.LastIndexByte(block[:end], '\n')
			if newline < 0 {
				break
			}
			line := block[newline+1 : end]
			if err := checkLineSize(line); err != nil {
				return pageRead{}, err
			}
			if len(line) > 0 {
				found = append(found, locatedLine{
					data:  append([]byte(nil), line...),
					start: start + int64(newline+1),
				})
			}
			end = newline
		}

		if len(found) >= want {
			break
		}

		if start == 0 {
			// With no older chunk remaining, the prefix is the first line.
			line := block[:end]
			if err := checkLineSize(line); err != nil {
				return pageRead{}, err
			}
			if len(line) > 0 {
				found = append(found, locatedLine{data: append([]byte(nil), line...), start: 0})
			}
			break
		}

		carry = append(carry[:0], block[:end]...)
		if err := checkLineSize(carry); err != nil {
			return pageRead{}, err
		}
		cursor = start
	}

	hasMore := len(found) > limit
	if hasMore {
		found = found[:limit]
	}

	lines := make([][]byte, len(found))
	for i := range found {
		lines[i] = found[i].data
	}

	beforeOut := int64(0)
	if hasMore {
		beforeOut = found[len(found)-1].start
	}
	return pageRead{Lines: lines, Before: beforeOut, HasMore: hasMore}, nil
}

func checkLineSize(b []byte) error {
	if len(b) > maxLineSize {
		return errLineTooLong
	}
	return nil
}
