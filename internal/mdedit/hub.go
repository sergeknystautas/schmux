package mdedit

import (
	"sync"

	"github.com/charmbracelet/log"
)

// Hub maps an absolute file path to the one Document every socket on that
// file shares. Owned by the dashboard server.
type Hub struct {
	mu     sync.Mutex
	docs   map[string]*Document
	logger *log.Logger
	closed bool
}

func NewHub(logger *log.Logger) *Hub {
	return &Hub{docs: map[string]*Document{}, logger: logger}
}

// Subscribe attaches c to the document for path, creating it on first use.
// validate is re-run on every save from any subscriber. The first
// subscriber's closure is the one kept, which is fine because every
// subscriber validates the same path the same way.
func (h *Hub) Subscribe(path string, validate func() error, c Conn) (*Document, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, &CloseError{Reason: ReasonWatcherError}
	}
	doc, ok := h.docs[path]
	if !ok {
		doc = NewDocument(Options{Path: path, Validate: validate, Logger: h.logger})
		h.docs[path] = doc
	}
	h.mu.Unlock()

	if err := doc.Subscribe(c); err != nil {
		h.mu.Lock()
		if doc.Unsubscribe(c) && h.docs[path] == doc {
			delete(h.docs, path)
			doc.Close()
		}
		h.mu.Unlock()
		return nil, err
	}
	return doc, nil
}

// Unsubscribe detaches c; the last subscriber's departure closes the document.
func (h *Hub) Unsubscribe(path string, c Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	doc, ok := h.docs[path]
	if !ok {
		return
	}
	if doc.Unsubscribe(c) {
		delete(h.docs, path)
		doc.Close()
	}
}

// Close stops every document and waits for their goroutines.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	docs := h.docs
	h.docs = map[string]*Document{}
	h.mu.Unlock()
	for _, doc := range docs {
		doc.Close()
	}
}

// Count returns the number of documents the hub is tracking. Exposed for
// tests and operational inspection; production code does not need it.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.docs)
}
