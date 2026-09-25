package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

type chatPerformanceEvent struct {
	Kind string         `json:"kind"`
	At   string         `json:"at"`
	Data map[string]any `json:"data"`
}

func (s *Server) recordChatPerformance(kind string, fields []any) {
	data := make(map[string]any, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if ok {
			data[key] = fields[i+1]
		}
	}
	event := chatPerformanceEvent{
		Kind: kind,
		At:   time.Now().UTC().Format(time.RFC3339Nano),
		Data: data,
	}
	if err := appendChatPerformance(event); err != nil {
		s.logger.Error("failed to record chat performance", "err", err)
	}
}

// appendChatPerformance persists one complete line. A failed telemetry write
// does not affect chat delivery; callers report the error and continue.
func appendChatPerformance(event chatPerformanceEvent) error {
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	path := filepath.Join(schmuxdir.Get(), "diagnostics", "chat-performance.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
