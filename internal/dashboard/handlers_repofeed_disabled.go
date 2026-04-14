//go:build norepofeed

package dashboard

import (
	"net/http"

	"github.com/sergeknystautas/schmux/internal/repofeed"
)

func (s *Server) handleRepofeedList(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Repofeed is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRepofeedRepo(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Repofeed is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRepofeedDismiss(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Repofeed is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRepofeedOutgoing(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Repofeed is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRepofeedIncoming(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Repofeed is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) SetRepofeedPublisher(_ *repofeed.Publisher) {}

func (s *Server) SetRepofeedDismissed(_ *repofeed.DismissedStore) {}

func (s *Server) SetRepofeedSummaryCache(_ *repofeed.SummaryCache) {}

func (s *Server) SetRepofeedConsumer(_ *repofeed.Consumer) {}

func (s *Server) BroadcastRepofeed() {}
