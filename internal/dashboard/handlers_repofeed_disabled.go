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

func (s *Server) handleRepofeedPublishPreview(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Repofeed is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) handleRepofeedPublishPush(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, "Repofeed is not available in this build", http.StatusServiceUnavailable)
}

func (s *Server) SetRepofeedPublisher(_ *repofeed.Publisher) {}

func (s *Server) SetRepofeedConsumer(_ *repofeed.Consumer) {}

func (s *Server) BroadcastRepofeed() {}
