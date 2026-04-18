package remote

import (
	"context"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/cmdtemplate"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
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

// remoteVCSVars builds the template variables map shared by remote VCS commands.
// Keys correspond to the {{.X}} slots in argv-array templates.
func remoteVCSVars(destPath, workspacePath, workspaceID, repoBasePath string) map[string]string {
	vars := map[string]string{}
	if destPath != "" {
		vars["DestPath"] = destPath
	}
	if workspacePath != "" {
		vars["WorkspacePath"] = workspacePath
	}
	if workspaceID != "" {
		vars["WorkspaceID"] = workspaceID
	}
	if repoBasePath != "" {
		vars["RepoBasePath"] = repoBasePath
	}
	return vars
}

// renderRemoteCommand renders an argv-array template via cmdtemplate.Render
// and converts the resulting argv into a properly shell-quoted string for
// transport over SSH/tmux control mode (which inherently requires a shell
// command line on the remote end). Each element is quoted with shellutil.Quote
// so values containing shell metacharacters cannot expand into extra tokens.
func renderRemoteCommand(tmpl config.ShellCommand, vars map[string]string) (string, error) {
	argv, err := cmdtemplate.Template(tmpl).Render(vars)
	if err != nil {
		return "", err
	}
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellutil.Quote(a)
	}
	return strings.Join(parts, " "), nil
}

// createRemoteWorktree creates a new worktree on a remote host by executing
// the configured VCS create command via tmux control mode.
// For git, the command runs with cwd = repoBasePath (required by git worktree add).
func createRemoteWorktree(ctx context.Context, client *controlmode.Client, profile config.ResolvedFlavor, workspaceID, destPath string) error {
	tmpl := profile.RemoteVCSCommands.GetCreateWorktree(profile.VCS)

	cmd, err := renderRemoteCommand(tmpl, remoteVCSVars(destPath, destPath, workspaceID, profile.RepoBasePath))
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

	cmd, err := renderRemoteCommand(tmpl, remoteVCSVars("", workspacePath, "", profile.RepoBasePath))
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

	cmd, err := renderRemoteCommand(tmpl, remoteVCSVars("", workspacePath, "", profile.RepoBasePath))
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
