// Package chat owns chat-kind sessions: the append-only conversation record,
// the tmux file bridge, and the per-session runtime that ties them together.
package chat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RecordType identifies who wrote a conversation record.
type RecordType string

const (
	RecordUserMessage RecordType = "user_message" // the user's words, written before the harness sees them
	RecordControl     RecordType = "control"      // a line schmux sent the harness (interrupt, answers)
	RecordHarness     RecordType = "harness"      // a line the harness emitted, verbatim
	RecordSession     RecordType = "session"      // schmux ended the session (dispose or restart)
)

// Image is an inline image attachment.
type Image struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"` // base64
}

// Record is one line of the conversation record.
type Record struct {
	Ts     string          `json:"ts"`
	Type   RecordType      `json:"type"`
	ID     string          `json:"id,omitempty"`     // user_message only
	Text   string          `json:"text,omitempty"`   // user_message only
	Images []Image         `json:"images,omitempty"` // user_message only
	Line   json.RawMessage `json:"line,omitempty"`   // control and harness: the raw JSON object
	Event  string          `json:"event,omitempty"`  // session only: "ended"
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// NewUserMessage builds a user_message record with a fresh id.
func NewUserMessage(text string, images []Image) Record {
	return Record{Ts: now(), Type: RecordUserMessage, ID: uuid.New().String(), Text: text, Images: images}
}

// NewControl wraps a line schmux is about to send to the harness.
func NewControl(line []byte) Record {
	return Record{Ts: now(), Type: RecordControl, Line: json.RawMessage(append([]byte{}, line...))}
}

// NewHarness wraps a line the harness emitted.
func NewHarness(line []byte) Record {
	return Record{Ts: now(), Type: RecordHarness, Line: json.RawMessage(append([]byte{}, line...))}
}

// NewSessionEnded marks the point where schmux disposed the session. The
// harness is killed in the same call: a reader that sees this record knows no
// result is coming.
func NewSessionEnded() Record {
	return Record{Ts: now(), Type: RecordSession, Event: "ended"}
}

// ConversationPath returns the conversation record inside a session directory.
func ConversationPath(sessionDir string) string {
	return filepath.Join(sessionDir, "conversation.jsonl")
}

// UserMessageLine renders the stream-json user message for stdin. Text-only
// messages use a plain string content; images add content blocks.
func UserMessageLine(text string, images []Image) ([]byte, error) {
	var content any = text
	if len(images) > 0 {
		blocks := []map[string]any{{"type": "text", "text": text}}
		for _, img := range images {
			blocks = append(blocks, map[string]any{
				"type":   "image",
				"source": map[string]any{"type": "base64", "media_type": img.MediaType, "data": img.Data},
			})
		}
		content = blocks
	}
	line := struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"message"`
	}{Type: "user"}
	line.Message.Role = "user"
	line.Message.Content = content
	return json.Marshal(line)
}

// Log is the append-only conversation record for one session.
type Log struct {
	path string
	mu   sync.Mutex
}

// OpenLog creates the parent directory and the file if missing.
func OpenLog(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("chat: create record dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("chat: open record: %w", err)
	}
	f.Close()
	return &Log{path: path}, nil
}

// Path returns the record's file path.
func (l *Log) Path() string { return l.path }

// Append writes one record as a JSON line.
func (l *Log) Append(rec Record) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadAll parses every record. Malformed lines are skipped.
func (l *Log) ReadAll() ([]Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024) // image records are large
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err == nil {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

// CountHarness returns how many harness records are stored: the number of
// output-file lines already consumed.
func (l *Log) CountHarness() (int, error) {
	recs, err := l.ReadAll()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range recs {
		if r.Type == RecordHarness {
			n++
		}
	}
	return n, nil
}

// CopyLog copies src to dst byte for byte (restart seeding).
func CopyLog(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
