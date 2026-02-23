// Package logging provides structured logging for the schmux daemon.
package logging

import (
	"os"
	"strings"

	"github.com/charmbracelet/log"
)

// New creates a root logger configured from environment.
// Log level defaults to InfoLevel, overridden by SCHMUX_LOG_LEVEL env var.
func New() *log.Logger {
	level := log.InfoLevel
	if env := os.Getenv("SCHMUX_LOG_LEVEL"); env != "" {
		parsed, err := log.ParseLevel(strings.ToLower(env))
		if err == nil {
			level = parsed
		}
	}
	logger := log.NewWithOptions(os.Stderr, log.Options{
		Level:           level,
		ReportTimestamp: true,
	})
	return logger
}

// Sub creates a child logger with the given prefix.
func Sub(parent *log.Logger, prefix string) *log.Logger {
	return parent.WithPrefix(prefix)
}
