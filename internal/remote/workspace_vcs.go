package remote

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
)

// remoteVCSExecutor abstracts remote VCS operations for testability.
// The real implementation uses controlmode.Client; tests can provide a mock.
type remoteVCSExecutor interface {
	createWorktree(ctx context.Context, profile config.ResolvedFlavor, workspaceID, destPath string) error
	removeWorktree(ctx context.Context, profile config.ResolvedFlavor, workspacePath string) error
	checkDirty(ctx context.Context, profile config.ResolvedFlavor, workspacePath string) (bool, error)
}

// controlModeVCSExecutor implements remoteVCSExecutor using a controlmode.Client.
type controlModeVCSExecutor struct {
	client *controlmode.Client
}

func (e *controlModeVCSExecutor) createWorktree(ctx context.Context, profile config.ResolvedFlavor, workspaceID, destPath string) error {
	return createRemoteWorktree(ctx, e.client, profile, workspaceID, destPath)
}

func (e *controlModeVCSExecutor) removeWorktree(ctx context.Context, profile config.ResolvedFlavor, workspacePath string) error {
	return removeRemoteWorktree(ctx, e.client, profile, workspacePath)
}

func (e *controlModeVCSExecutor) checkDirty(ctx context.Context, profile config.ResolvedFlavor, workspacePath string) (bool, error) {
	return checkRemoteDirty(ctx, e.client, profile, workspacePath)
}

// remoteVCSTemplateData holds the variables available to remote VCS command templates.
type remoteVCSTemplateData struct {
	DestPath      string
	WorkspacePath string
	WorkspaceID   string
	RepoBasePath  string
}

// createRemoteWorktree creates a new worktree on a remote host by executing
// the configured VCS create command via tmux control mode.
// For git, the command runs with cwd = repoBasePath (required by git worktree add).
func createRemoteWorktree(ctx context.Context, client *controlmode.Client, profile config.ResolvedFlavor, workspaceID, destPath string) error {
	tmpl := profile.RemoteVCSCommands.GetCreateWorktree(profile.VCS)

	cmd, err := resolveVCSTemplate(tmpl, remoteVCSTemplateData{
		DestPath:      destPath,
		WorkspaceID:   workspaceID,
		RepoBasePath:  profile.RepoBasePath,
		WorkspacePath: destPath,
	})
	if err != nil {
		return fmt.Errorf("resolve create worktree template: %w", err)
	}

	if _, err := client.RunCommand(ctx, profile.RepoBasePath, cmd); err != nil {
		return fmt.Errorf("create remote worktree: %w", err)
	}
	return nil
}

// removeRemoteWorktree removes a worktree on a remote host by executing
// the configured VCS remove command via tmux control mode.
func removeRemoteWorktree(ctx context.Context, client *controlmode.Client, profile config.ResolvedFlavor, workspacePath string) error {
	tmpl := profile.RemoteVCSCommands.GetRemoveWorktree(profile.VCS)

	cmd, err := resolveVCSTemplate(tmpl, remoteVCSTemplateData{
		WorkspacePath: workspacePath,
		RepoBasePath:  profile.RepoBasePath,
	})
	if err != nil {
		return fmt.Errorf("resolve remove worktree template: %w", err)
	}

	if _, err := client.RunCommand(ctx, profile.RepoBasePath, cmd); err != nil {
		return fmt.Errorf("remove remote worktree: %w", err)
	}
	return nil
}

// checkRemoteDirty checks whether a remote worktree has uncommitted changes
// by executing the configured VCS dirty check command via tmux control mode.
// Returns true if the worktree is dirty (has uncommitted changes).
func checkRemoteDirty(ctx context.Context, client *controlmode.Client, profile config.ResolvedFlavor, workspacePath string) (bool, error) {
	tmpl := profile.RemoteVCSCommands.GetCheckDirty(profile.VCS)

	cmd, err := resolveVCSTemplate(tmpl, remoteVCSTemplateData{
		WorkspacePath: workspacePath,
		RepoBasePath:  profile.RepoBasePath,
	})
	if err != nil {
		return false, fmt.Errorf("resolve check dirty template: %w", err)
	}

	output, err := client.RunCommand(ctx, profile.RepoBasePath, cmd)
	if err != nil {
		return false, fmt.Errorf("check remote dirty: %w", err)
	}

	// Non-empty output means dirty.
	return strings.TrimSpace(output) != "", nil
}

// resolveVCSTemplate parses and executes a Go template with the given data.
func resolveVCSTemplate(tmplStr string, data remoteVCSTemplateData) (string, error) {
	t, err := template.New("vcs").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template %q: %w", tmplStr, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %q: %w", tmplStr, err)
	}
	return buf.String(), nil
}
