// Package flightreview turns the local flight evidence ledgers and archives
// into per-flight summaries and retrospective review reports. It owns legacy
// import, resource reconciliation, derivation, Jev labels, and report
// rendering; nothing it reads or writes leaves the machine.
package flightreview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// VCSInspector reads a workspace's work state without modifying it.
type VCSInspector interface {
	FlightEvidence(ctx context.Context, workspaceID, repoURL, branch string) (workspace.VCSArchiveState, error)
}

// Options are the collaborators of a Service. Workspaces may be nil, in which
// case no git state is collected.
type Options struct {
	State      state.StateStore
	Workspaces VCSInspector
	Logger     *log.Logger
	Now        func() time.Time
	Version    string
	// Classifier labels operator messages. Nil asks Jev's default endpoint.
	Classifier ChatClassifier
}

// Service archives flight evidence and derives review data from it.
type Service struct {
	opts      Options
	reconcile *reconcileState
	// flights is attached after construction (see SetFlights).
	flights *flights.Manager
}

// randomID returns prefix plus 12 random hex digits.
func randomID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("flightreview: crypto/rand: %v", err))
	}
	return prefix + hex.EncodeToString(b)
}

// NewService builds a Service, defaulting the clock and logger.
func NewService(opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = log.New(io.Discard)
	}
	if opts.Classifier == nil {
		opts.Classifier = NewJevClassifier(&http.Client{Timeout: 30 * time.Second}, flights.DefaultJevEndpoint)
	}
	return &Service{opts: opts, reconcile: newReconcileState(opts.Now)}
}
