package events

import (
	"encoding/json"
	"os"
	"sync"
)

var writeMu sync.Mutex

// AppendEvent marshals an event to JSON and appends it as a line to the file.
func AppendEvent(path string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	writeMu.Lock()
	defer writeMu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
