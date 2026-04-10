package events

import (
	"bufio"
	"os"
)

// EventLine holds the raw envelope and the original JSON bytes.
type EventLine struct {
	RawEvent
	Data []byte
}

// ReadEvents reads all events from a JSONL file, applying an optional filter.
// Returns empty slice (not error) for nonexistent files.
func ReadEvents(path string, filter func(RawEvent) bool) ([]EventLine, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []EventLine
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw, err := ParseRawEvent(line)
		if err != nil {
			continue // skip malformed lines
		}
		if filter != nil && !filter(raw) {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		events = append(events, EventLine{RawEvent: raw, Data: cp})
	}
	return events, scanner.Err()
}

