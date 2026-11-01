package flighthistory

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// Root is the directory holding every durable flight-history file.
func Root() string { return schmuxdir.FlightHistoryDir() }

// EventsPath is the append-only flight snapshot ledger.
func EventsPath() string { return filepath.Join(Root(), "events.jsonl") }

// ReconciliationPath is the append-only resource reconciliation ledger.
func ReconciliationPath() string { return filepath.Join(Root(), "reconciliation.jsonl") }

// ValidateID rejects identifiers that could name anything but a single path
// element: empty, dot elements, separators, and NUL bytes.
func ValidateID(id string) error {
	switch {
	case id == "" || id == "." || id == "..":
		return fmt.Errorf("invalid id %q", id)
	case strings.ContainsAny(id, "/\\\x00"):
		return fmt.Errorf("invalid id %q: contains a path separator or NUL", id)
	}
	return nil
}

// FlightDir returns the per-flight archive directory, confined to Root.
func FlightDir(flightID string) (string, error) {
	if err := ValidateID(flightID); err != nil {
		return "", fmt.Errorf("flight %w", err)
	}
	return filepath.Join(Root(), flightID), nil
}
