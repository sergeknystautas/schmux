package dashboard

import (
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

// handleChatImage serves the capped workspace-cache copy of one attachment.
// Older conversations populate the cache from their persisted record on demand.
func (s *Server) handleChatImage(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	messageID := chi.URLParam(r, "messageID")
	index, indexErr := strconv.Atoi(chi.URLParam(r, "index"))
	if _, err := uuid.Parse(messageID); err != nil || indexErr != nil || index < 0 {
		http.NotFound(w, r)
		return
	}
	sess, err := s.session.GetSession(sessionID)
	if err != nil || !sess.IsChat() {
		http.NotFound(w, r)
		return
	}
	workspace, ok := s.state.GetWorkspace(sess.WorkspaceID)
	if !ok || workspace.Path == "" || workspace.IsRemoteWorkspace() {
		http.NotFound(w, r)
		return
	}
	cacheDir := filepath.Join(state.SchmuxDataDir(workspace.Path), "cache", "chat-images")
	previewPath := chat.PreviewPath(cacheDir, sess.ID, messageID, index)
	cacheHit := chat.EnsurePreview(cacheDir, sess.ID, messageID, index, chat.Image{}) == nil
	if !cacheHit {
		conversationPath := chat.ConversationPath(schmuxdir.ChatSessionDir(sess.WorkspaceID, sess.ID))
		img, err := chat.FindImage(conversationPath, messageID, index)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := chat.EnsurePreview(cacheDir, sess.ID, messageID, index, img); err != nil {
			s.logger.Error("failed to cache chat image preview", "session", sess.ID, "message", messageID, "err", err)
			http.Error(w, "failed to prepare image", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, previewPath)
}
