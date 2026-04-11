// Package logging provides structured logging for the schmux daemon.
package logging

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// registry tracks all loggers created by New and Sub so that SetLevel
// can update them all atomically. charmbracelet/log's WithPrefix creates
// independent copies, so each must be updated individually.
var (
	registryMu sync.Mutex
	registry   []*log.Logger
	// currentLevel is the active level for all registered loggers.
	currentLevel = log.InfoLevel
)

// New creates a root logger configured from environment.
// Log level defaults to InfoLevel, overridden by SCHMUX_LOG_LEVEL env var.
// If forceColor is true, the entire log line is colored by level even when
// stderr is not a TTY (e.g. when piped into the dev-runner TUI).
func New(forceColor ...bool) *log.Logger {
	level := log.InfoLevel
	// In dev mode, default to debug level for full visibility.
	if len(forceColor) > 0 && forceColor[0] {
		level = log.DebugLevel
	}
	// SCHMUX_LOG_LEVEL always takes precedence if set.
	if env := os.Getenv("SCHMUX_LOG_LEVEL"); env != "" {
		parsed, err := log.ParseLevel(strings.ToLower(env))
		if err == nil {
			level = parsed
		}
	}

	// Allow redirecting log output to a file via SCHMUX_LOG_FILE env var.
	// Used by E2E tests where fd-inherited stderr doesn't reliably capture
	// all output (kernel page cache truncation on SIGKILL).
	var output io.Writer = os.Stderr
	if logPath := os.Getenv("SCHMUX_LOG_FILE"); logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
			output = io.MultiWriter(os.Stderr, f)
		}
	}

	logger := log.NewWithOptions(output, log.Options{
		Level:           level,
		ReportTimestamp: true,
	})
	registryMu.Lock()
	currentLevel = level
	registry = append(registry, logger)
	registryMu.Unlock()

	if len(forceColor) > 0 && forceColor[0] {
		logger.SetTimeFormat("15:04:05")
		// SetOutput auto-detects the writer as non-TTY (Ascii profile),
		// so charmbracelet/log emits plain text. The levelColorWriter
		// then wraps each line in the appropriate ANSI color.
		output := io.Writer(&levelColorWriter{w: os.Stderr})

		// In dev mode, also write to the daemon log file so agents
		// running in tmux sessions can see daemon output.
		{
			logPath := filepath.Join(schmuxdir.Get(), "daemon-startup.log")
			if logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
				output = io.MultiWriter(output, logF)
			}
		}

		logger.SetOutput(output)
	}
	return logger
}

// Sub creates a child logger with the given prefix.
// Brackets are added around the prefix for visual clarity in log output.
// The charmbracelet/log formatter appends a colon after the prefix, so the
// raw output is "[name]:" — the daemon strips the extra colon in dev mode.
func Sub(parent *log.Logger, prefix string) *log.Logger {
	child := parent.WithPrefix("[" + prefix + "]")
	registryMu.Lock()
	registry = append(registry, child)
	registryMu.Unlock()
	return child
}

// SetLevel changes the log level for all registered loggers.
func SetLevel(level log.Level) {
	registryMu.Lock()
	defer registryMu.Unlock()
	currentLevel = level
	for _, l := range registry {
		l.SetLevel(level)
	}
}

// GetLevel returns the current global log level.
func GetLevel() log.Level {
	registryMu.Lock()
	defer registryMu.Unlock()
	return currentLevel
}

// ANSI 256-color codes matching the web dashboard palette.
var levelColors = map[string][]byte{
	"DEBU": []byte("\x1b[38;5;245m"), // gray
	"INFO": []byte("\x1b[38;5;77m"),  // green  (--color-success)
	"WARN": []byte("\x1b[38;5;220m"), // yellow (--color-warning)
	"ERRO": []byte("\x1b[38;5;203m"), // red    (--color-danger)
	"FATA": []byte("\x1b[38;5;196m"), // bright red
}

var ansiReset = []byte("\x1b[0m")

// levelColorWriter wraps an io.Writer and colors the header portion of each
// log line (timestamp, level, prefix) based on its level, leaving the message
// and structured fields unstyled.
// It expects plain-text input (no ANSI codes) from charmbracelet/log with the
// format: "HH:MM:SS LEVL [prefix]: message key=value"
type levelColorWriter struct {
	w io.Writer
}

func (lc *levelColorWriter) Write(p []byte) (n int, err error) {
	// Timestamp is "15:04:05" (8 bytes) + space = level starts at index 9.
	if len(p) >= 13 {
		if color, ok := levelColors[string(p[9:13])]; ok {
			line := bytes.TrimRight(p, "\n")

			// Find where the header ends and the message begins.
			// With prefix:  "15:04:05 INFO [name]: msg" — header ends after "]:"
			// Without prefix: "15:04:05 INFO msg" — header is the first 13 bytes
			headerEnd := 13
			if idx := bytes.Index(line[13:], []byte("]:")); idx >= 0 {
				headerEnd = 13 + idx + 2
			}

			var buf bytes.Buffer
			buf.Grow(len(color) + len(line) + len(ansiReset) + 2)
			buf.Write(color)
			buf.Write(line[:headerEnd])
			buf.Write(ansiReset)
			buf.Write(line[headerEnd:])
			buf.WriteByte('\n')
			_, err = lc.w.Write(buf.Bytes())
			return len(p), err
		}
	}
	return lc.w.Write(p)
}
