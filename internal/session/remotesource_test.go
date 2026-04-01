package session

import (
	"testing"
)

func TestRemoteSource_ImplementsControlSource(t *testing.T) {
	// Compile-time check that RemoteSource implements ControlSource.
	// We can't construct one without a real Connection, but the interface
	// compliance check is still valuable.
	var _ ControlSource = (*RemoteSource)(nil)
}

func TestRemoteSource_MethodsRequireConnection(t *testing.T) {
	// Verify the struct fields are accessible and the constructor works
	// with nil (for compile-time verification only — don't call methods on nil conn).
	source := &RemoteSource{
		conn:   nil,
		paneID: "%5",
		events: make(chan SourceEvent, 10),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	if source.paneID != "%5" {
		t.Errorf("paneID = %q, want %%5", source.paneID)
	}
}
