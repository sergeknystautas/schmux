package dashboard

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

type workspaceFileTargetError struct {
	message string
	status  int
}

func validateWorkspaceFileTarget(
	ctx context.Context,
	store state.StateStore,
	workspaceID string,
	filePath string,
) *workspaceFileTargetError {
	if !isValidResourceID(workspaceID) {
		return &workspaceFileTargetError{message: "invalid workspace ID", status: http.StatusBadRequest}
	}
	if validateGitFilePaths([]string{filePath}) != "" {
		return &workspaceFileTargetError{message: "invalid file path", status: http.StatusBadRequest}
	}

	ws, found := store.GetWorkspace(workspaceID)
	if !found {
		return &workspaceFileTargetError{message: "workspace not found", status: http.StatusNotFound}
	}
	if ws.RemoteHostID != "" {
		return &workspaceFileTargetError{message: "jump links require a local workspace", status: http.StatusBadRequest}
	}

	workspacePath, err := filepath.EvalSymlinks(ws.Path)
	if err != nil {
		return &workspaceFileTargetError{message: "workspace not found", status: http.StatusNotFound}
	}
	requestedPath := filepath.Join(ws.Path, filePath)
	if !isPathWithinDir(requestedPath, ws.Path) {
		return &workspaceFileTargetError{message: "invalid file path", status: http.StatusForbidden}
	}
	currentPath := ws.Path
	for _, component := range strings.Split(filepath.Clean(filePath), string(filepath.Separator)) {
		currentPath = filepath.Join(currentPath, component)
		info, err := os.Lstat(currentPath)
		if err != nil {
			if os.IsNotExist(err) {
				return &workspaceFileTargetError{message: "file not found", status: http.StatusNotFound}
			}
			return &workspaceFileTargetError{message: "cannot access file", status: http.StatusForbidden}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &workspaceFileTargetError{message: "jump target contains a symbolic link", status: http.StatusForbidden}
		}
	}
	realPath, err := filepath.EvalSymlinks(requestedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &workspaceFileTargetError{message: "file not found", status: http.StatusNotFound}
		}
		return &workspaceFileTargetError{message: "cannot access file", status: http.StatusForbidden}
	}
	if !isPathWithinDir(realPath, workspacePath) {
		return &workspaceFileTargetError{message: "file is outside workspace", status: http.StatusForbidden}
	}

	info, err := os.Stat(realPath)
	if err != nil {
		return &workspaceFileTargetError{message: "cannot access file", status: http.StatusForbidden}
	}
	if !info.Mode().IsRegular() {
		return &workspaceFileTargetError{message: "jump target is not a regular file", status: http.StatusForbidden}
	}
	if !caseSensitiveFileExists(filepath.Dir(requestedPath), filepath.Base(requestedPath)) {
		return &workspaceFileTargetError{message: "file not found", status: http.StatusNotFound}
	}

	ignoreCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	vcsType := localWorkspaceVCSType(ws)
	ignored, err := fileMatchesVCSIgnore(ignoreCtx, ws.Path, filePath, vcsType)
	if err != nil {
		return &workspaceFileTargetError{
			message: "failed to check ignore patterns",
			status:  http.StatusInternalServerError,
		}
	}
	if ignored {
		return &workspaceFileTargetError{
			message: "file is ignored by VCS",
			status:  http.StatusForbidden,
		}
	}

	return nil
}

func localWorkspaceVCSType(ws state.Workspace) string {
	if ws.VCS != "" {
		return ws.VCS
	}
	return "git"
}

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

	filePath, err := url.PathUnescape(target[slash+1:])
	if err != nil {
		writeJSONError(w, "invalid file path", http.StatusBadRequest)
		return
	}

	if validationErr := validateWorkspaceFileTarget(r.Context(), s.state, workspaceID, filePath); validationErr != nil {
		writeJSONError(w, validationErr.message, validationErr.status)
		return
	}

	http.Redirect(w, r, workspaceFileViewRoute(workspaceID, filePath), http.StatusFound)
}

func workspaceFileViewRoute(workspaceID, filePath string) string {
	workspace := url.PathEscape(workspaceID)
	file := url.PathEscape(filePath)
	switch workspaceFileViewKind(filePath) {
	case "markdown":
		return "/diff/" + workspace + "/md/" + file
	case "mermaid":
		return "/diff/" + workspace + "/mmd/" + file
	case "image":
		return "/diff/" + workspace + "/img/" + file
	case "html":
		return "/diff/" + workspace + "/html/" + file
	default:
		return "/diff/" + workspace + "?" + url.Values{"file": {filePath}}.Encode()
	}
}

func workspaceFileViewKind(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".md", ".mdx":
		return "markdown"
	case ".mmd":
		return "mermaid"
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return "image"
	case ".html":
		return "html"
	default:
		return "diff"
	}
}
