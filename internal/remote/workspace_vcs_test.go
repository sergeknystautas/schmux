package remote

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestResolveVCSTemplate_Git(t *testing.T) {
	data := remoteVCSTemplateData{
		DestPath:      "/home/user/ws/ws-001",
		WorkspaceID:   "ws-001",
		RepoBasePath:  "/home/user/myproject",
		WorkspacePath: "/home/user/ws/ws-001",
	}

	cmds := config.RemoteVCSCommands{}

	// Git create default
	cmd, err := resolveVCSTemplate(cmds.GetCreateWorktree("git"), data)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := "git worktree add /home/user/ws/ws-001 -b schmux-ws-001 origin/main"
	if cmd != want {
		t.Errorf("git create:\ngot  %q\nwant %q", cmd, want)
	}

	// Git remove default
	cmd, err = resolveVCSTemplate(cmds.GetRemoveWorktree("git"), data)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	want = "git worktree remove --force /home/user/ws/ws-001"
	if cmd != want {
		t.Errorf("git remove:\ngot  %q\nwant %q", cmd, want)
	}

	// Git dirty default
	cmd, err = resolveVCSTemplate(cmds.GetCheckDirty("git"), data)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	want = "git -C /home/user/ws/ws-001 status --porcelain"
	if cmd != want {
		t.Errorf("git dirty:\ngot  %q\nwant %q", cmd, want)
	}
}

func TestResolveVCSTemplate_Sapling(t *testing.T) {
	data := remoteVCSTemplateData{
		DestPath:      "/home/user/ws/ws-002",
		WorkspaceID:   "ws-002",
		RepoBasePath:  "/home/user/repo",
		WorkspacePath: "/home/user/ws/ws-002",
	}

	cmds := config.RemoteVCSCommands{}

	// Sapling create default
	cmd, err := resolveVCSTemplate(cmds.GetCreateWorktree("sapling"), data)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := "sl clone /home/user/repo /home/user/ws/ws-002"
	if cmd != want {
		t.Errorf("sapling create:\ngot  %q\nwant %q", cmd, want)
	}

	// Sapling remove default
	cmd, err = resolveVCSTemplate(cmds.GetRemoveWorktree("sapling"), data)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	want = "rm -rf /home/user/ws/ws-002"
	if cmd != want {
		t.Errorf("sapling remove:\ngot  %q\nwant %q", cmd, want)
	}

	// Sapling dirty default
	cmd, err = resolveVCSTemplate(cmds.GetCheckDirty("sapling"), data)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	want = "sl status --cwd /home/user/ws/ws-002"
	if cmd != want {
		t.Errorf("sapling dirty:\ngot  %q\nwant %q", cmd, want)
	}
}

func TestResolveVCSTemplate_CustomOverrides(t *testing.T) {
	data := remoteVCSTemplateData{
		DestPath:      "/tmp/ws/ws-003",
		WorkspaceID:   "ws-003",
		RepoBasePath:  "/opt/repos/project.git",
		WorkspacePath: "/tmp/ws/ws-003",
	}

	cmds := config.RemoteVCSCommands{
		CreateWorktree: "custom-clone --source {{.RepoBasePath}} --dest {{.DestPath}} --id {{.WorkspaceID}}",
		RemoveWorktree: "custom-rm {{.WorkspacePath}}",
		CheckDirty:     "custom-status --path {{.WorkspacePath}}",
	}

	cmd, err := resolveVCSTemplate(cmds.GetCreateWorktree("git"), data)
	if err != nil {
		t.Fatalf("custom create: %v", err)
	}
	want := "custom-clone --source /opt/repos/project.git --dest /tmp/ws/ws-003 --id ws-003"
	if cmd != want {
		t.Errorf("custom create:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = resolveVCSTemplate(cmds.GetRemoveWorktree("git"), data)
	if err != nil {
		t.Fatalf("custom remove: %v", err)
	}
	want = "custom-rm /tmp/ws/ws-003"
	if cmd != want {
		t.Errorf("custom remove:\ngot  %q\nwant %q", cmd, want)
	}

	cmd, err = resolveVCSTemplate(cmds.GetCheckDirty("git"), data)
	if err != nil {
		t.Fatalf("custom dirty: %v", err)
	}
	want = "custom-status --path /tmp/ws/ws-003"
	if cmd != want {
		t.Errorf("custom dirty:\ngot  %q\nwant %q", cmd, want)
	}
}

func TestResolveVCSTemplate_InvalidTemplate(t *testing.T) {
	_, err := resolveVCSTemplate("{{.Invalid", remoteVCSTemplateData{})
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}
