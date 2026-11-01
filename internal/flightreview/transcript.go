package flightreview

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
)

// ReadTranscript parses an archived conversation log. Valid records are
// sequenced 1..N in log order, as the chat package sequences them; a line that
// does not decode is kept with its file line number and consumes no sequence
// number. The file is never modified. A missing file is an error: the archive
// manifest, not an empty transcript, records an absent source.
func ReadTranscript(path, role, sessionID string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, fmt.Errorf("open transcript %s: %w", path, err)
	}
	defer f.Close()

	t := Transcript{Role: role, SessionID: sessionID}
	r := bufio.NewReader(f)
	for line := 1; ; line++ {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 {
			var rec chat.Record
			if jsonErr := json.Unmarshal(raw, &rec); jsonErr != nil {
				t.Malformed = append(t.Malformed, flighthistory.MalformedLine{Line: line, Bytes: raw, Error: jsonErr.Error()})
			} else {
				rec.Seq = uint64(len(t.Records) + 1)
				t.Records = append(t.Records, TranscriptRecord{Seq: len(t.Records) + 1, Line: line, Record: rec})
			}
		}
		if errors.Is(err, io.EOF) {
			return t, nil
		}
		if err != nil {
			return t, fmt.Errorf("read transcript %s: %w", path, err)
		}
	}
}
