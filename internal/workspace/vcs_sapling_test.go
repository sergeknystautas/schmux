package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestRenderCommandTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     map[string]string
		want     string
		wantErr  bool
	}{
		{
			"simple substitution",
			"sl clone {{.RepoIdentifier}} {{.DestPath}}",
			map[string]string{"RepoIdentifier": "myrepo", "DestPath": "/tmp/ws1"},
			"sl clone myrepo /tmp/ws1",
			false,
		},
		{
			"no variables",
			"echo hello",
			map[string]string{},
			"echo hello",
			false,
		},
		{
			"missing variable errors",
			"sl clone {{.Missing}}",
			map[string]string{},
			"",
			true,
		},
		{
			"special characters in paths",
			"rm -rf {{.WorkspacePath}}",
			map[string]string{"WorkspacePath": "/home/user/my workspace/test"},
			"rm -rf /home/user/my workspace/test",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderCommandTemplate(tt.template, tt.vars)
			if (err != nil) != tt.wantErr {
				t.Fatalf("renderCommandTemplate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSaplingStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   int
		status string
	}{
		{
			"modified file",
			"M path/to/modified.go",
			1, "modified",
		},
		{
			"added file",
			"A path/to/added.go",
			1, "added",
		},
		{
			"removed file",
			"R path/to/removed.go",
			1, "deleted",
		},
		{
			"untracked file",
			"? path/to/untracked.go",
			1, "untracked",
		},
		{
			"empty input",
			"",
			0, "",
		},
		{
			"multiple files",
			"M file1.go\nA file2.go\n? file3.go",
			3, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := parseSaplingStatus(tt.input)
			if len(files) != tt.want {
				t.Fatalf("parseSaplingStatus() returned %d files, want %d", len(files), tt.want)
			}
			if tt.status != "" && len(files) > 0 && files[0].Status != tt.status {
				t.Errorf("files[0].Status = %q, want %q", files[0].Status, tt.status)
			}
		})
	}
}

func TestParseDiffStat(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantAdded   int
		wantRemoved int
	}{
		{
			"typical output",
			" file.go | 10 ++++------\n 1 file changed, 4 insertions(+), 6 deletions(-)",
			4, 6,
		},
		{
			"insertions only",
			" file.go | 5 +++++\n 1 file changed, 5 insertions(+)",
			5, 0,
		},
		{
			"deletions only",
			" file.go | 3 ---\n 1 file changed, 3 deletions(-)",
			0, 3,
		},
		{
			"empty",
			"",
			0, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := parseDiffStat(tt.input)
			if added != tt.wantAdded {
				t.Errorf("added = %d, want %d", added, tt.wantAdded)
			}
			if removed != tt.wantRemoved {
				t.Errorf("removed = %d, want %d", removed, tt.wantRemoved)
			}
		})
	}
}

func TestBackendFor_SelectsSaplingBackend(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{}
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{
		{Name: "git-repo", URL: "git@github.com:user/repo.git"},
		{Name: "sl-repo", URL: "sl-repo-id", VCS: "sapling"},
		{Name: "clone-repo", URL: "git@github.com:user/clone.git", VCS: "git-clone"},
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	gitBackend := m.backendFor("git@github.com:user/repo.git")
	if _, ok := gitBackend.(*GitBackend); !ok {
		t.Errorf("expected GitBackend for git repo, got %T", gitBackend)
	}

	saplingBackend := m.backendFor("sl-repo-id")
	if _, ok := saplingBackend.(*SaplingBackend); !ok {
		t.Errorf("expected SaplingBackend for sapling repo, got %T", saplingBackend)
	}

	cloneBackend := m.backendFor("git@github.com:user/clone.git")
	if _, ok := cloneBackend.(*GitBackend); !ok {
		t.Errorf("expected GitBackend for git-clone repo, got %T", cloneBackend)
	}

	unknownBackend := m.backendFor("unknown-url")
	if _, ok := unknownBackend.(*GitBackend); !ok {
		t.Errorf("expected GitBackend for unknown repo, got %T", unknownBackend)
	}
}

func TestSaplingBackend_EnsureRepoBase(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	reposDir := filepath.Join(tmpDir, "repos")
	cfg := &config.Config{}
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []config.Repo{
		{Name: "myrepo", URL: "myrepo-id", VCS: "sapling", BarePath: "myrepo"},
	}
	cfg.SaplingCommands = config.SaplingCommands{
		CreateRepoBase: "mkdir -p {{.BasePath}}",
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	backend := m.backendFor("myrepo-id")
	ctx := context.Background()

	basePath, err := backend.EnsureRepoBase(ctx, "myrepo-id", "")
	if err != nil {
		t.Fatalf("EnsureRepoBase() failed: %v", err)
	}

	expectedPath := filepath.Join(reposDir, "myrepo")
	if basePath != expectedPath {
		t.Errorf("basePath = %q, want %q", basePath, expectedPath)
	}

	if _, err := os.Stat(basePath); err != nil {
		t.Errorf("basePath directory should exist: %v", err)
	}

	rb, found := st.GetRepoBaseByURL("myrepo-id")
	if !found {
		t.Fatal("expected repo base to be tracked in state")
	}
	if rb.VCS != "sapling" {
		t.Errorf("repo base VCS = %q, want 'sapling'", rb.VCS)
	}
}

func TestSaplingBackend_EnsureRepoBase_ReusesExisting(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	reposDir := filepath.Join(tmpDir, "repos")
	existingPath := filepath.Join(reposDir, "myrepo")
	os.MkdirAll(existingPath, 0755)

	cfg := &config.Config{}
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []config.Repo{
		{Name: "myrepo", URL: "myrepo-id", VCS: "sapling", BarePath: "myrepo"},
	}
	cfg.SaplingCommands = config.SaplingCommands{
		CreateRepoBase: "false",
	}
	st := state.New(statePath, nil)
	st.AddRepoBase(state.RepoBase{RepoURL: "myrepo-id", Path: existingPath, VCS: "sapling"})
	m := New(cfg, st, statePath, testLogger())

	backend := m.backendFor("myrepo-id")
	ctx := context.Background()

	basePath, err := backend.EnsureRepoBase(ctx, "myrepo-id", "")
	if err != nil {
		t.Fatalf("EnsureRepoBase() failed: %v", err)
	}
	if basePath != existingPath {
		t.Errorf("should reuse existing path, got %q want %q", basePath, existingPath)
	}
}

func TestSaplingBackend_CreateAndRemoveWorkspace(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	wsPath := filepath.Join(tmpDir, "workspace-001")

	cfg := &config.Config{}
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.SaplingCommands = config.SaplingCommands{
		CreateWorkspace: "mkdir -p {{.DestPath}}",
		RemoveWorkspace: "rm -rf {{.WorkspacePath}}",
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	sb := NewSaplingBackend(m, cfg.SaplingCommands)
	ctx := context.Background()

	if err := sb.CreateWorkspace(ctx, tmpDir, "main", wsPath); err != nil {
		t.Fatalf("CreateWorkspace() failed: %v", err)
	}
	if _, err := os.Stat(wsPath); err != nil {
		t.Fatalf("workspace directory should exist after create: %v", err)
	}

	if err := sb.RemoveWorkspace(ctx, wsPath); err != nil {
		t.Fatalf("RemoveWorkspace() failed: %v", err)
	}
	if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
		t.Error("workspace directory should be removed")
	}
}

func TestSaplingBackend_CheckRepoBaseDiscoversExisting(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	existingPath := filepath.Join(tmpDir, "existing-checkout")
	os.MkdirAll(existingPath, 0755)

	cfg := &config.Config{}
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{
		{Name: "myrepo", URL: "myrepo-id", VCS: "sapling"},
	}
	cfg.SaplingCommands = config.SaplingCommands{
		CheckRepoBase:  "echo " + existingPath,
		CreateRepoBase: "false",
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	backend := m.backendFor("myrepo-id")
	ctx := context.Background()

	basePath, err := backend.EnsureRepoBase(ctx, "myrepo-id", "")
	if err != nil {
		t.Fatalf("EnsureRepoBase() failed: %v", err)
	}
	if basePath != existingPath {
		t.Errorf("should discover existing checkout, got %q want %q", basePath, existingPath)
	}

	rb, found := st.GetRepoBaseByURL("myrepo-id")
	if !found {
		t.Fatal("expected repo base to be tracked in state after discovery")
	}
	if rb.Path != existingPath {
		t.Errorf("state path = %q, want %q", rb.Path, existingPath)
	}
}

func TestSaplingBackend_ManagerCreate_UsesSaplingBackend(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	reposDir := filepath.Join(tmpDir, "repos")
	wsDir := filepath.Join(tmpDir, "workspaces")

	cfg := &config.Config{}
	cfg.WorkspacePath = wsDir
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []config.Repo{
		{Name: "sl-project", URL: "sl-project-id", VCS: "sapling", BarePath: "sl-project"},
	}
	cfg.SaplingCommands = config.SaplingCommands{
		CreateRepoBase:  "mkdir -p {{.BasePath}}",
		CreateWorkspace: "mkdir -p {{.DestPath}} && echo 'sapling workspace' > {{.DestPath}}/.sl-marker",
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	ctx := context.Background()
	ws, err := m.GetOrCreate(ctx, "sl-project-id", "main")
	if err != nil {
		t.Fatalf("GetOrCreate() failed: %v", err)
	}

	if ws.VCS != "sapling" {
		t.Errorf("workspace VCS = %q, want 'sapling'", ws.VCS)
	}

	markerPath := filepath.Join(ws.Path, ".sl-marker")
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected .sl-marker in workspace (proves sapling template ran): %v", err)
	}

	content, _ := os.ReadFile(markerPath)
	if got := string(content); got != "sapling workspace\n" {
		t.Errorf("marker content = %q, want 'sapling workspace\\n'", got)
	}
}
