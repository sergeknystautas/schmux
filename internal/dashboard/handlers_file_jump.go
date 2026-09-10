package dashboard

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// handleFileJump validates a workspace-relative local file and redirects to
// the dashboard view responsible for displaying its content type.
func (s *Server) handleFileJump(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimPrefix(r.URL.EscapedPath(), "/jump/")
	slash := strings.IndexByte(target, '/')
	if slash <= 0 || slash == len(target)-1 {
		writeJSONError(w, "invalid jump path", http.StatusBadRequest)
		return
	}

	workspaceID, err := url.PathUnescape(target[:slash])
	if err != nil {
		writeJSONError(w, "invalid workspace ID", http.StatusBadRequest)
		return
	}
	if !isValidResourceID(workspaceID) {
		writeJSONError(w, "invalid workspace ID", http.StatusBadRequest)
		return
	}

	filePath, err := url.PathUnescape(target[slash+1:])
	if err != nil || validateGitFilePaths([]string{filePath}) != "" {
		writeJSONError(w, "invalid file path", http.StatusBadRequest)
		return
	}

	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		writeJSONError(w, "workspace not found", http.StatusNotFound)
		return
	}
	if ws.RemoteHostID != "" {
		writeJSONError(w, "jump links require a local workspace", http.StatusBadRequest)
		return
	}

	workspacePath, err := filepath.EvalSymlinks(ws.Path)
	if err != nil {
		writeJSONError(w, "workspace not found", http.StatusNotFound)
		return
	}
	requestedPath := filepath.Join(ws.Path, filePath)
	if !isPathWithinDir(requestedPath, ws.Path) {
		writeJSONError(w, "invalid file path", http.StatusForbidden)
		return
	}
	currentPath := ws.Path
	for _, component := range strings.Split(filepath.Clean(filePath), string(filepath.Separator)) {
		currentPath = filepath.Join(currentPath, component)
		info, err := os.Lstat(currentPath)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSONError(w, "file not found", http.StatusNotFound)
			} else {
				writeJSONError(w, "cannot access file", http.StatusForbidden)
			}
			return
		}
		if info.Mode()&os.ModeSymlink != 0 {
			writeJSONError(w, "jump target contains a symbolic link", http.StatusForbidden)
			return
		}
	}
	realPath, err := filepath.EvalSymlinks(requestedPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, "file not found", http.StatusNotFound)
		} else {
			writeJSONError(w, "cannot access file", http.StatusForbidden)
		}
		return
	}
	if !isPathWithinDir(realPath, workspacePath) {
		writeJSONError(w, "file is outside workspace", http.StatusForbidden)
		return
	}

	info, err := os.Stat(realPath)
	if err != nil {
		writeJSONError(w, "cannot access file", http.StatusForbidden)
		return
	}
	if !info.Mode().IsRegular() {
		writeJSONError(w, "jump target is not a regular file", http.StatusForbidden)
		return
	}
	if !caseSensitiveFileExists(filepath.Dir(requestedPath), filepath.Base(requestedPath)) {
		writeJSONError(w, "file not found", http.StatusNotFound)
		return
	}

	http.Redirect(w, r, workspaceFileViewRoute(workspaceID, filePath), http.StatusFound)
}

func workspaceFileViewRoute(workspaceID, filePath string) string {
	workspace := url.PathEscape(workspaceID)
	file := url.PathEscape(filePath)
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".md", ".mdx":
		return "/diff/" + workspace + "/md/" + file
	case ".mmd":
		return "/diff/" + workspace + "/mmd/" + file
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return "/diff/" + workspace + "/img/" + file
	case ".html":
		return "/diff/" + workspace + "/html/" + file
	default:
		return "/diff/" + workspace + "?" + url.Values{"file": {filePath}}.Encode()
	}
}
