package remote

import (
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestRenderRemoteCommand_Git(t *testing.T) {
	vars := remoteVCSVars(
		"/home/user/ws/ws-001",
		"/home/user/ws/ws-001",
		"ws-001",
		"/home/user/myproject",
	)

	cmds := config.RemoteVCSCommands{}

	// Git create default — argv elements are individually shell-quoted
	// before being joined for transport over SSH/control mode.
	cmd, err := renderRemoteCommand(cmds.GetCreateWorktree("git"), vars)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := "'git' 'worktree' 'add' '/home/user/ws/ws-001' '-b' 'schmux-ws-001' 'origin/main'"
	if cmd != want {
		t.Errorf("git create:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = renderRemoteCommand(cmds.GetRemoveWorktree("git"), vars)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	want = "'git' 'worktree' 'remove' '--force' '/home/user/ws/ws-001'"
	if cmd != want {
		t.Errorf("git remove:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = renderRemoteCommand(cmds.GetCheckDirty("git"), vars)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	want = "'git' '-C' '/home/user/ws/ws-001' 'status' '--porcelain'"
	if cmd != want {
		t.Errorf("git dirty:\ngot  %q\nwant %q", cmd, want)
	}
}

func TestRenderRemoteCommand_Sapling(t *testing.T) {
	vars := remoteVCSVars(
		"/home/user/ws/ws-002",
		"/home/user/ws/ws-002",
		"ws-002",
		"/home/user/repo",
	)

	cmds := config.RemoteVCSCommands{}

	cmd, err := renderRemoteCommand(cmds.GetCreateWorktree("sapling"), vars)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := "'sl' 'clone' '/home/user/repo' '/home/user/ws/ws-002'"
	if cmd != want {
		t.Errorf("sapling create:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = renderRemoteCommand(cmds.GetRemoveWorktree("sapling"), vars)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	want = "'rm' '-rf' '/home/user/ws/ws-002'"
	if cmd != want {
		t.Errorf("sapling remove:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = renderRemoteCommand(cmds.GetCheckDirty("sapling"), vars)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	want = "'sl' 'status' '--cwd' '/home/user/ws/ws-002'"
	if cmd != want {
		t.Errorf("sapling dirty:\ngot  %q\nwant %q", cmd, want)
	}
}

func TestRenderRemoteCommand_CustomOverrides(t *testing.T) {
	vars := remoteVCSVars(
		"/tmp/ws/ws-003",
		"/tmp/ws/ws-003",
		"ws-003",
		"/opt/repos/project.git",
	)

	cmds := config.RemoteVCSCommands{
		CreateWorktree: config.ShellCommand{
			"custom-clone",
			"--source", "{{.RepoBasePath}}",
			"--dest", "{{.DestPath}}",
			"--id", "{{.WorkspaceID}}",
		},
		RemoveWorktree: config.ShellCommand{"custom-rm", "{{.WorkspacePath}}"},
		CheckDirty:     config.ShellCommand{"custom-status", "--path", "{{.WorkspacePath}}"},
	}

	cmd, err := renderRemoteCommand(cmds.GetCreateWorktree("git"), vars)
	if err != nil {
		t.Fatalf("custom create: %v", err)
	}
	want := "'custom-clone' '--source' '/opt/repos/project.git' '--dest' '/tmp/ws/ws-003' '--id' 'ws-003'"
	if cmd != want {
		t.Errorf("custom create:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = renderRemoteCommand(cmds.GetRemoveWorktree("git"), vars)
	if err != nil {
		t.Fatalf("custom remove: %v", err)
	}
	want = "'custom-rm' '/tmp/ws/ws-003'"
	if cmd != want {
		t.Errorf("custom remove:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = renderRemoteCommand(cmds.GetCheckDirty("git"), vars)
	if err != nil {
		t.Fatalf("custom dirty: %v", err)
	}
	want = "'custom-status' '--path' '/tmp/ws/ws-003'"
	if cmd != want {
		t.Errorf("custom dirty:\ngot  %q\nwant %q", cmd, want)
	}
}

func TestRenderRemoteCommand_QuotesShellMetacharsInValues(t *testing.T) {
	// A templated value containing shell metacharacters must be safely quoted
	// so it cannot expand into extra tokens on the remote shell. This proves
	// the bug-class removal end-to-end.
	tmpl := config.ShellCommand{"ls", "{{.Path}}"}
	cmd, err := renderRemoteCommand(tmpl, map[string]string{"Path": "a; rm -rf /; b"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// shellutil.Quote wraps values in single quotes; the metacharacters appear
	// inside the quotes, not as separate tokens.
	if !strings.Contains(cmd, "'a; rm -rf /; b'") {
		t.Errorf("metachar value was NOT properly quoted: %q", cmd)
	}
}

func TestRenderRemoteCommand_InvalidTemplate(t *testing.T) {
	tmpl := config.ShellCommand{"ls", "{{.Invalid"}
	_, err := renderRemoteCommand(tmpl, map[string]string{})
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}
