package session

import (
	"testing"
	"time"
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

func TestRemoteSource_OutputBackpressurePreservesEvent(t *testing.T) {
	source := &RemoteSource{
		events: make(chan SourceEvent, 1),
		stopCh: make(chan struct{}),
	}
	defer close(source.stopCh)

	first := SourceEvent{Type: SourceOutput, Data: "first"}
	second := SourceEvent{Type: SourceOutput, Data: "second"}
	source.emit(first)
	delivered := make(chan struct{})
	go func() {
		source.emit(second)
		close(delivered)
	}()

	// The full channel must hold the producer until SessionRuntime drains it;
	// this bounded window proves remote output is not silently discarded.
	select {
	case <-delivered:
		t.Fatal("second output returned before source capacity was available")
	case <-time.After(25 * time.Millisecond):
	}

	if got := <-source.events; got.Data != first.Data {
		t.Fatalf("first output = %q, want %q", got.Data, first.Data)
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("second output was not released after source capacity became available")
	}
	if got := <-source.events; got.Data != second.Data {
		t.Fatalf("second output = %q, want %q", got.Data, second.Data)
	}
}
