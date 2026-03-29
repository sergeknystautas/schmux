package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/sergeknystautas/schmux/internal/session"
)

// DiagnosticCapture holds all data for a single diagnostic snapshot.
type DiagnosticCapture struct {
	Timestamp   time.Time
	SessionID   string
	Cols        int
	Rows        int
	Counters    map[string]int64
	TmuxScreen  string
	RingBuffer  []byte
	Findings    []string
	Verdict     string
	DiffSummary string

	CursorTmuxX       int
	CursorTmuxY       int
	CursorTmuxVisible bool
	CursorTmuxErr     string // empty if no error

	TmuxHealthSamples []session.HealthProbeSample `json:"-"`
}

type diagnosticMeta struct {
	Timestamp    string `json:"timestamp"`
	SessionID    string `json:"sessionId"`
	TerminalSize struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	} `json:"terminalSize"`
	Counters    map[string]int64 `json:"counters"`
	Findings    []string         `json:"automatedFindings"`
	Verdict     string           `json:"verdict"`
	DiffSummary string           `json:"diffSummary"`
	CursorTmux  *struct {
		X       int  `json:"x"`
		Y       int  `json:"y"`
		Visible bool `json:"visible"`
	} `json:"cursorTmux,omitempty"`
}

// WriteToDir writes all diagnostic files to the given directory.
func (d *DiagnosticCapture) WriteToDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// meta.json
	meta := diagnosticMeta{
		Timestamp:   d.Timestamp.UTC().Format(time.RFC3339),
		SessionID:   d.SessionID,
		Counters:    d.Counters,
		Findings:    d.Findings,
		Verdict:     d.Verdict,
		DiffSummary: d.DiffSummary,
	}
	meta.TerminalSize.Cols = d.Cols
	meta.TerminalSize.Rows = d.Rows
	if d.CursorTmuxErr == "" {
		meta.CursorTmux = &struct {
			X       int  `json:"x"`
			Y       int  `json:"y"`
			Visible bool `json:"visible"`
		}{
			X:       d.CursorTmuxX,
			Y:       d.CursorTmuxY,
			Visible: d.CursorTmuxVisible,
		}
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaJSON, 0o644); err != nil {
		return err
	}
	// ringbuffer-backend.txt — raw bytes, not base64
	if err := os.WriteFile(filepath.Join(dir, "ringbuffer-backend.txt"), d.RingBuffer, 0o644); err != nil {
		return err
	}
	// screen-tmux.txt
	if err := os.WriteFile(filepath.Join(dir, "screen-tmux.txt"), []byte(d.TmuxScreen), 0o644); err != nil {
		return err
	}
	// tmux-health.json — probe RTT samples for performance trending
	if len(d.TmuxHealthSamples) > 0 {
		healthJSON, err := json.MarshalIndent(d.TmuxHealthSamples, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "tmux-health.json"), healthJSON, 0o644); err != nil {
			return err
		}
	}
	return nil
}
