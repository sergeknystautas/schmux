package config

import (
	"testing"
)

func TestResolveProfileFlavor_UsesProfileDefaults(t *testing.T) {
	profile := RemoteProfile{
		ID:                    "test_profile",
		DisplayName:           "Test Profile",
		VCS:                   "git",
		WorkspacePath:         "~/workspace",
		ConnectCommand:        "ssh -tt {{.Flavor}} --",
		ReconnectCommand:      "ssh -tt {{.Hostname}} --",
		ProvisionCommand:      "git clone {{.Repo}} {{.WorkspacePath}}",
		HostnameRegex:         `host-(\S+)`,
		VSCodeCommandTemplate: `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`,
		Flavors: []RemoteProfileFlavor{
			{Flavor: "gpu-large"},
		},
	}

	resolved, err := ResolveProfileFlavor(profile, "gpu-large")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.ProfileID != "test_profile" {
		t.Errorf("ProfileID: got %q, want %q", resolved.ProfileID, "test_profile")
	}
	if resolved.ProfileDisplayName != "Test Profile" {
		t.Errorf("ProfileDisplayName: got %q, want %q", resolved.ProfileDisplayName, "Test Profile")
	}
	if resolved.Flavor != "gpu-large" {
		t.Errorf("Flavor: got %q, want %q", resolved.Flavor, "gpu-large")
	}
	// FlavorDisplayName should default to the flavor string when not set
	if resolved.FlavorDisplayName != "gpu-large" {
		t.Errorf("FlavorDisplayName: got %q, want %q", resolved.FlavorDisplayName, "gpu-large")
	}
	if resolved.VCS != "git" {
		t.Errorf("VCS: got %q, want %q", resolved.VCS, "git")
	}
	if resolved.WorkspacePath != "~/workspace" {
		t.Errorf("WorkspacePath: got %q, want %q", resolved.WorkspacePath, "~/workspace")
	}
	if resolved.ConnectCommand != "ssh -tt {{.Flavor}} --" {
		t.Errorf("ConnectCommand: got %q, want %q", resolved.ConnectCommand, "ssh -tt {{.Flavor}} --")
	}
	if resolved.ReconnectCommand != "ssh -tt {{.Hostname}} --" {
		t.Errorf("ReconnectCommand: got %q, want %q", resolved.ReconnectCommand, "ssh -tt {{.Hostname}} --")
	}
	if resolved.ProvisionCommand != "git clone {{.Repo}} {{.WorkspacePath}}" {
		t.Errorf("ProvisionCommand: got %q, want %q", resolved.ProvisionCommand, "git clone {{.Repo}} {{.WorkspacePath}}")
	}
	if resolved.HostnameRegex != `host-(\S+)` {
		t.Errorf("HostnameRegex: got %q, want %q", resolved.HostnameRegex, `host-(\S+)`)
	}
	if resolved.VSCodeCommandTemplate != `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}` {
		t.Errorf("VSCodeCommandTemplate: got %q, want %q", resolved.VSCodeCommandTemplate, `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`)
	}
}

func TestResolveProfileFlavor_FlavorOverrides(t *testing.T) {
	profile := RemoteProfile{
		ID:               "test_profile",
		DisplayName:      "Test Profile",
		VCS:              "git",
		WorkspacePath:    "~/workspace",
		ProvisionCommand: "git clone {{.Repo}} {{.WorkspacePath}}",
		Flavors: []RemoteProfileFlavor{
			{
				Flavor:           "gpu-large",
				DisplayName:      "GPU Large Instance",
				WorkspacePath:    "~/gpu-workspace",
				ProvisionCommand: "custom-provision {{.Repo}}",
			},
		},
	}

	resolved, err := ResolveProfileFlavor(profile, "gpu-large")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// FlavorDisplayName should use the flavor's DisplayName
	if resolved.FlavorDisplayName != "GPU Large Instance" {
		t.Errorf("FlavorDisplayName: got %q, want %q", resolved.FlavorDisplayName, "GPU Large Instance")
	}
	// WorkspacePath should be overridden by flavor
	if resolved.WorkspacePath != "~/gpu-workspace" {
		t.Errorf("WorkspacePath: got %q, want %q", resolved.WorkspacePath, "~/gpu-workspace")
	}
	// ProvisionCommand should be overridden by flavor
	if resolved.ProvisionCommand != "custom-provision {{.Repo}}" {
		t.Errorf("ProvisionCommand: got %q, want %q", resolved.ProvisionCommand, "custom-provision {{.Repo}}")
	}

	// Profile-level fields should still come from profile
	if resolved.ProfileID != "test_profile" {
		t.Errorf("ProfileID: got %q, want %q", resolved.ProfileID, "test_profile")
	}
	if resolved.VCS != "git" {
		t.Errorf("VCS: got %q, want %q", resolved.VCS, "git")
	}
}

func TestResolveProfileFlavor_NotFound(t *testing.T) {
	profile := RemoteProfile{
		ID:            "test_profile",
		DisplayName:   "Test Profile",
		VCS:           "git",
		WorkspacePath: "~/workspace",
		Flavors: []RemoteProfileFlavor{
			{Flavor: "gpu-large"},
		},
	}

	_, err := ResolveProfileFlavor(profile, "cpu-small")
	if err == nil {
		t.Fatal("expected error for unknown flavor, got nil")
	}
}

func TestConfig_ProfileCRUD(t *testing.T) {
	cfg := &Config{}

	profile := RemoteProfile{
		DisplayName:   "Test Profile",
		VCS:           "git",
		WorkspacePath: "~/workspace",
		Flavors: []RemoteProfileFlavor{
			{Flavor: "gpu-large"},
		},
	}

	// Add
	if err := cfg.AddRemoteProfile(profile); err != nil {
		t.Fatalf("AddRemoteProfile: %v", err)
	}

	profiles := cfg.GetRemoteProfiles()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	// ID should be auto-generated from first flavor
	if profiles[0].ID != "gpu_large" {
		t.Errorf("auto-generated ID: got %q, want %q", profiles[0].ID, "gpu_large")
	}
	// VCS default should be applied
	if profiles[0].VCS != "git" {
		t.Errorf("VCS: got %q, want %q", profiles[0].VCS, "git")
	}

	// Get
	got, ok := cfg.GetRemoteProfile("gpu_large")
	if !ok {
		t.Fatal("GetRemoteProfile: not found")
	}
	if got.DisplayName != "Test Profile" {
		t.Errorf("DisplayName: got %q, want %q", got.DisplayName, "Test Profile")
	}

	// Get non-existent
	_, ok = cfg.GetRemoteProfile("nonexistent")
	if ok {
		t.Error("GetRemoteProfile: expected not found for nonexistent ID")
	}

	// Update
	updated := got
	updated.DisplayName = "Updated Profile"
	if err := cfg.UpdateRemoteProfile(updated); err != nil {
		t.Fatalf("UpdateRemoteProfile: %v", err)
	}
	got, _ = cfg.GetRemoteProfile("gpu_large")
	if got.DisplayName != "Updated Profile" {
		t.Errorf("after update, DisplayName: got %q, want %q", got.DisplayName, "Updated Profile")
	}

	// Update non-existent
	nonExistent := RemoteProfile{
		ID:            "nonexistent",
		DisplayName:   "Nope",
		WorkspacePath: "~/nope",
		Flavors:       []RemoteProfileFlavor{{Flavor: "x"}},
	}
	if err := cfg.UpdateRemoteProfile(nonExistent); err == nil {
		t.Error("UpdateRemoteProfile: expected error for nonexistent ID")
	}

	// Remove
	if err := cfg.RemoveRemoteProfile("gpu_large"); err != nil {
		t.Fatalf("RemoveRemoteProfile: %v", err)
	}
	profiles = cfg.GetRemoteProfiles()
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles after remove, got %d", len(profiles))
	}

	// Remove non-existent
	if err := cfg.RemoveRemoteProfile("gpu_large"); err == nil {
		t.Error("RemoveRemoteProfile: expected error for nonexistent ID")
	}
}

func TestConfig_AddRemoteProfile_DuplicateFlavor(t *testing.T) {
	cfg := &Config{}

	profile := RemoteProfile{
		ID:            "test",
		DisplayName:   "Test",
		WorkspacePath: "~/workspace",
		Flavors: []RemoteProfileFlavor{
			{Flavor: "gpu-large"},
			{Flavor: "gpu-large"}, // duplicate
		},
	}

	err := cfg.AddRemoteProfile(profile)
	if err == nil {
		t.Fatal("expected error for duplicate flavor strings, got nil")
	}
}

func TestMigrateRemoteFlavorsToProfiles(t *testing.T) {
	cfg := &Config{ConfigData: ConfigData{
		RemoteFlavors: []RemoteFlavor{
			{
				ID:                    "my_gpu_flavor",
				Flavor:                "gpu:ml-large",
				DisplayName:           "GPU ML Large",
				VCS:                   "git",
				WorkspacePath:         "~/workspace",
				ConnectCommand:        "ssh -tt {{.Flavor}} --",
				ReconnectCommand:      "ssh -tt {{.Hostname}} --",
				ProvisionCommand:      "git clone {{.Repo}} {{.WorkspacePath}}",
				HostnameRegex:         `host-(\S+)`,
				VSCodeCommandTemplate: `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`,
			},
		},
	}}

	cfg.MigrateRemoteFlavorsToProfiles()

	if len(cfg.RemoteProfiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(cfg.RemoteProfiles))
	}

	p := cfg.RemoteProfiles[0]

	// ID must be preserved from the old flavor (CRITICAL)
	if p.ID != "my_gpu_flavor" {
		t.Errorf("ID: got %q, want %q (must preserve existing ID)", p.ID, "my_gpu_flavor")
	}
	if p.DisplayName != "GPU ML Large" {
		t.Errorf("DisplayName: got %q, want %q", p.DisplayName, "GPU ML Large")
	}
	if p.VCS != "git" {
		t.Errorf("VCS: got %q, want %q", p.VCS, "git")
	}
	if p.WorkspacePath != "~/workspace" {
		t.Errorf("WorkspacePath: got %q, want %q", p.WorkspacePath, "~/workspace")
	}
	if p.ConnectCommand != "ssh -tt {{.Flavor}} --" {
		t.Errorf("ConnectCommand: got %q, want %q", p.ConnectCommand, "ssh -tt {{.Flavor}} --")
	}
	if p.ReconnectCommand != "ssh -tt {{.Hostname}} --" {
		t.Errorf("ReconnectCommand: got %q, want %q", p.ReconnectCommand, "ssh -tt {{.Hostname}} --")
	}
	if p.ProvisionCommand != "git clone {{.Repo}} {{.WorkspacePath}}" {
		t.Errorf("ProvisionCommand: got %q, want %q", p.ProvisionCommand, "git clone {{.Repo}} {{.WorkspacePath}}")
	}
	if p.HostnameRegex != `host-(\S+)` {
		t.Errorf("HostnameRegex: got %q, want %q", p.HostnameRegex, `host-(\S+)`)
	}
	if p.VSCodeCommandTemplate != `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}` {
		t.Errorf("VSCodeCommandTemplate: got %q, want %q", p.VSCodeCommandTemplate, `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`)
	}

	// Check the child flavor
	if len(p.Flavors) != 1 {
		t.Fatalf("expected 1 child flavor, got %d", len(p.Flavors))
	}
	if p.Flavors[0].Flavor != "gpu:ml-large" {
		t.Errorf("child Flavor: got %q, want %q", p.Flavors[0].Flavor, "gpu:ml-large")
	}
	if p.Flavors[0].DisplayName != "GPU ML Large" {
		t.Errorf("child DisplayName: got %q, want %q", p.Flavors[0].DisplayName, "GPU ML Large")
	}
}

func TestResolveProfileFlavor_PersistentNoFlavors(t *testing.T) {
	profile := RemoteProfile{
		ID:                    "devserver",
		DisplayName:           "Dev Server",
		HostType:              HostTypePersistent,
		VCS:                   "git",
		RepoBasePath:          "/home/user/myproject",
		WorkspacePathTemplate: "/home/user/schmux-ws/{{.WorkspaceID}}",
		ConnectCommand:        "ssh user@host --",
		ReconnectCommand:      "ssh user@host --",
		HostnameRegex:         `(host\.example\.com)`,
	}

	resolved, err := ResolveProfileFlavor(profile, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.ProfileID != "devserver" {
		t.Errorf("ProfileID: got %q, want %q", resolved.ProfileID, "devserver")
	}
	if resolved.HostType != HostTypePersistent {
		t.Errorf("HostType: got %q, want %q", resolved.HostType, HostTypePersistent)
	}
	if resolved.RepoBasePath != "/home/user/myproject" {
		t.Errorf("RepoBasePath: got %q, want %q", resolved.RepoBasePath, "/home/user/myproject")
	}
	if resolved.WorkspacePathTemplate != "/home/user/schmux-ws/{{.WorkspaceID}}" {
		t.Errorf("WorkspacePathTemplate: got %q, want %q", resolved.WorkspacePathTemplate, "/home/user/schmux-ws/{{.WorkspaceID}}")
	}
	if resolved.Flavor != "" {
		t.Errorf("Flavor: got %q, want empty", resolved.Flavor)
	}
	if resolved.FlavorDisplayName != "" {
		t.Errorf("FlavorDisplayName: got %q, want empty", resolved.FlavorDisplayName)
	}
	if resolved.ConnectCommand != "ssh user@host --" {
		t.Errorf("ConnectCommand: got %q, want %q", resolved.ConnectCommand, "ssh user@host --")
	}
}

func TestResolveProfileFlavor_PropagatesPersistentFields(t *testing.T) {
	profile := RemoteProfile{
		ID:                    "devserver",
		DisplayName:           "Dev Server",
		HostType:              HostTypePersistent,
		VCS:                   "sapling",
		RepoBasePath:          "/home/user/repo",
		WorkspacePathTemplate: "/home/user/ws/{{.WorkspaceID}}",
		ConnectCommand:        "ssh user@host --",
		ReconnectCommand:      "ssh user@host --",
		RemoteVCSCommands: RemoteVCSCommands{
			CreateWorktree: ShellCommand{"custom-clone", "{{.RepoBasePath}}", "{{.DestPath}}"},
		},
		Flavors: []RemoteProfileFlavor{
			{Flavor: "default", DisplayName: "Default"},
		},
	}

	resolved, err := ResolveProfileFlavor(profile, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.HostType != HostTypePersistent {
		t.Errorf("HostType: got %q, want %q", resolved.HostType, HostTypePersistent)
	}
	if resolved.RepoBasePath != "/home/user/repo" {
		t.Errorf("RepoBasePath: got %q, want %q", resolved.RepoBasePath, "/home/user/repo")
	}
	if resolved.WorkspacePathTemplate != "/home/user/ws/{{.WorkspaceID}}" {
		t.Errorf("WorkspacePathTemplate: got %q, want %q", resolved.WorkspacePathTemplate, "/home/user/ws/{{.WorkspaceID}}")
	}
	wantCreate := ShellCommand{"custom-clone", "{{.RepoBasePath}}", "{{.DestPath}}"}
	if !shellCommandEqual(resolved.RemoteVCSCommands.CreateWorktree, wantCreate) {
		t.Errorf("RemoteVCSCommands.CreateWorktree: got %v, want %v", resolved.RemoteVCSCommands.CreateWorktree, wantCreate)
	}
}

func shellCommandEqual(a, b ShellCommand) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRemoteVCSCommands_Defaults(t *testing.T) {
	empty := RemoteVCSCommands{}

	// Git defaults
	if got, want := empty.GetCreateWorktree("git"), (ShellCommand{"git", "worktree", "add", "{{.DestPath}}", "-b", "schmux-{{.WorkspaceID}}", "origin/main"}); !shellCommandEqual(got, want) {
		t.Errorf("git create default: got %v", got)
	}
	if got, want := empty.GetRemoveWorktree("git"), (ShellCommand{"git", "worktree", "remove", "--force", "{{.WorkspacePath}}"}); !shellCommandEqual(got, want) {
		t.Errorf("git remove default: got %v", got)
	}
	if got, want := empty.GetCheckDirty("git"), (ShellCommand{"git", "-C", "{{.WorkspacePath}}", "status", "--porcelain"}); !shellCommandEqual(got, want) {
		t.Errorf("git dirty default: got %v", got)
	}

	// Sapling defaults
	if got, want := empty.GetCreateWorktree("sapling"), (ShellCommand{"sl", "clone", "{{.RepoBasePath}}", "{{.DestPath}}"}); !shellCommandEqual(got, want) {
		t.Errorf("sapling create default: got %v", got)
	}
	if got, want := empty.GetRemoveWorktree("sapling"), (ShellCommand{"rm", "-rf", "{{.WorkspacePath}}"}); !shellCommandEqual(got, want) {
		t.Errorf("sapling remove default: got %v", got)
	}
	if got, want := empty.GetCheckDirty("sapling"), (ShellCommand{"sl", "status", "--cwd", "{{.WorkspacePath}}"}); !shellCommandEqual(got, want) {
		t.Errorf("sapling dirty default: got %v", got)
	}

	// Custom overrides
	custom := RemoteVCSCommands{
		CreateWorktree: ShellCommand{"my-create", "{{.DestPath}}"},
		RemoveWorktree: ShellCommand{"my-remove", "{{.WorkspacePath}}"},
		CheckDirty:     ShellCommand{"my-dirty", "{{.WorkspacePath}}"},
	}
	if got, want := custom.GetCreateWorktree("git"), (ShellCommand{"my-create", "{{.DestPath}}"}); !shellCommandEqual(got, want) {
		t.Errorf("custom create: got %v", got)
	}
	if got, want := custom.GetRemoveWorktree("sapling"), (ShellCommand{"my-remove", "{{.WorkspacePath}}"}); !shellCommandEqual(got, want) {
		t.Errorf("custom remove: got %v", got)
	}
	if got, want := custom.GetCheckDirty("git"), (ShellCommand{"my-dirty", "{{.WorkspacePath}}"}); !shellCommandEqual(got, want) {
		t.Errorf("custom dirty: got %v", got)
	}
}

func TestValidateRemoteProfile_Persistent(t *testing.T) {
	// Valid persistent profile
	valid := RemoteProfile{
		ID:                    "devserver",
		DisplayName:           "Dev Server",
		HostType:              HostTypePersistent,
		Hostname:              "dev.example.com",
		VCS:                   "git",
		RepoBasePath:          "/home/user/repo",
		WorkspacePathTemplate: "/home/user/ws/{{.WorkspaceID}}",
		ConnectCommand:        "ssh {{.Hostname}} --",
	}
	cfg := &Config{}
	if err := cfg.AddRemoteProfile(valid); err != nil {
		t.Fatalf("valid persistent profile rejected: %v", err)
	}

	// Missing workspace_path_template
	bad := valid
	bad.WorkspacePathTemplate = ""
	bad.ID = "bad1"
	cfg2 := &Config{}
	if err := cfg2.AddRemoteProfile(bad); err == nil {
		t.Error("expected error for persistent without workspace_path_template")
	}

	// Missing repo_base_path
	bad2 := valid
	bad2.RepoBasePath = ""
	bad2.ID = "bad2"
	cfg3 := &Config{}
	if err := cfg3.AddRemoteProfile(bad2); err == nil {
		t.Error("expected error for persistent without repo_base_path")
	}

	// Template missing {{.WorkspaceID}}
	bad3 := valid
	bad3.WorkspacePathTemplate = "/home/user/ws/fixed-path"
	bad3.ID = "bad3"
	cfg4 := &Config{}
	if err := cfg4.AddRemoteProfile(bad3); err == nil {
		t.Error("expected error for template without {{.WorkspaceID}}")
	}

	// Invalid template syntax
	bad4 := valid
	bad4.WorkspacePathTemplate = "/home/user/ws/{{.WorkspaceID"
	bad4.ID = "bad4"
	cfg5 := &Config{}
	if err := cfg5.AddRemoteProfile(bad4); err == nil {
		t.Error("expected error for invalid template syntax")
	}

	// Invalid host_type
	bad5 := valid
	bad5.HostType = "invalid"
	bad5.ID = "bad5"
	cfg6 := &Config{}
	if err := cfg6.AddRemoteProfile(bad5); err == nil {
		t.Error("expected error for invalid host_type")
	}

	// Persistent profiles don't need flavors or workspace_path
	noFlavors := valid
	noFlavors.ID = "noflavors"
	cfg7 := &Config{}
	if err := cfg7.AddRemoteProfile(noFlavors); err != nil {
		t.Fatalf("persistent profile without flavors should be valid: %v", err)
	}
}

func TestValidateRemoteProfile_EphemeralRegression(t *testing.T) {
	// Ephemeral (default) still requires flavors and workspace_path
	ephemeral := RemoteProfile{
		ID:            "eph",
		DisplayName:   "Ephemeral",
		VCS:           "git",
		WorkspacePath: "~/workspace",
		Flavors: []RemoteProfileFlavor{
			{Flavor: "gpu-large"},
		},
	}
	cfg := &Config{}
	if err := cfg.AddRemoteProfile(ephemeral); err != nil {
		t.Fatalf("valid ephemeral profile rejected: %v", err)
	}

	// Ephemeral without flavors should fail
	noFlavor := RemoteProfile{
		ID:            "bad",
		DisplayName:   "Bad",
		VCS:           "git",
		WorkspacePath: "~/workspace",
	}
	cfg2 := &Config{}
	if err := cfg2.AddRemoteProfile(noFlavor); err == nil {
		t.Error("expected error for ephemeral without flavors")
	}
}

// TestValidateRemoteProfile_HostnameRules covers the persistent/ephemeral
// hostname rules: persistent requires hostname; ephemeral rejects hostname;
// persistent rejects hostname_regex and flavors.
func TestValidateRemoteProfile_HostnameRules(t *testing.T) {
	persistentBase := RemoteProfile{
		DisplayName:           "Dev",
		HostType:              HostTypePersistent,
		Hostname:              "dev.example.com",
		VCS:                   "git",
		RepoBasePath:          "/home/user/repo",
		WorkspacePathTemplate: "/home/user/ws/{{.WorkspaceID}}",
	}
	ephemeralBase := RemoteProfile{
		DisplayName:   "Eph",
		VCS:           "git",
		WorkspacePath: "~/workspace",
		Flavors:       []RemoteProfileFlavor{{Flavor: "gpu"}},
	}

	t.Run("persistent_missing_hostname", func(t *testing.T) {
		bad := persistentBase
		bad.ID = "p-no-hn"
		bad.Hostname = ""
		if err := (&Config{}).AddRemoteProfile(bad); err == nil {
			t.Error("expected error: persistent without hostname")
		}
	})

	t.Run("persistent_with_hostname_regex", func(t *testing.T) {
		bad := persistentBase
		bad.ID = "p-with-regex"
		bad.HostnameRegex = `(host\.example\.com)`
		if err := (&Config{}).AddRemoteProfile(bad); err == nil {
			t.Error("expected error: persistent with hostname_regex")
		}
	})

	t.Run("persistent_with_flavors", func(t *testing.T) {
		bad := persistentBase
		bad.ID = "p-with-flavors"
		bad.Flavors = []RemoteProfileFlavor{{Flavor: "x"}}
		if err := (&Config{}).AddRemoteProfile(bad); err == nil {
			t.Error("expected error: persistent with flavors")
		}
	})

	t.Run("ephemeral_with_hostname", func(t *testing.T) {
		bad := ephemeralBase
		bad.ID = "e-with-hn"
		bad.Hostname = "fixed.example.com"
		if err := (&Config{}).AddRemoteProfile(bad); err == nil {
			t.Error("expected error: ephemeral with hostname")
		}
	})
}

// TestValidateRemoteProfile_CommandTemplates ensures connect_command and
// reconnect_command are template-checked at config-load time against the
// {Hostname, Flavor} data set. Regression guard for the original bug where
// {{.Hostname}} in a persistent connect_command failed only at first connect.
func TestValidateRemoteProfile_CommandTemplates(t *testing.T) {
	t.Run("connect_command_unknown_field", func(t *testing.T) {
		bad := RemoteProfile{
			DisplayName:    "Bad",
			VCS:            "git",
			WorkspacePath:  "~/workspace",
			Flavors:        []RemoteProfileFlavor{{Flavor: "gpu"}},
			ConnectCommand: "ssh {{.NotARealField}} --",
		}
		if err := (&Config{}).AddRemoteProfile(bad); err == nil {
			t.Error("expected error: unknown template field in connect_command")
		}
	})
	t.Run("reconnect_command_unknown_field", func(t *testing.T) {
		bad := RemoteProfile{
			DisplayName:      "Bad",
			VCS:              "git",
			WorkspacePath:    "~/workspace",
			Flavors:          []RemoteProfileFlavor{{Flavor: "gpu"}},
			ReconnectCommand: "ssh {{.NotARealField}} --",
		}
		if err := (&Config{}).AddRemoteProfile(bad); err == nil {
			t.Error("expected error: unknown template field in reconnect_command")
		}
	})
	t.Run("hostname_in_connect_command_accepted", func(t *testing.T) {
		ok := RemoteProfile{
			DisplayName:           "Dev",
			HostType:              HostTypePersistent,
			Hostname:              "dev.example.com",
			VCS:                   "git",
			RepoBasePath:          "/home/user/repo",
			WorkspacePathTemplate: "/home/user/ws/{{.WorkspaceID}}",
			ConnectCommand:        "ssh {{.Hostname}} --",
			ReconnectCommand:      "ssh {{.Hostname}} --",
		}
		if err := (&Config{}).AddRemoteProfile(ok); err != nil {
			t.Errorf("expected accept: persistent connect using {{.Hostname}}: %v", err)
		}
	})
	t.Run("flavor_in_connect_command_accepted", func(t *testing.T) {
		ok := RemoteProfile{
			DisplayName:    "Eph",
			VCS:            "git",
			WorkspacePath:  "~/workspace",
			Flavors:        []RemoteProfileFlavor{{Flavor: "gpu"}},
			ConnectCommand: "ssh -tt {{.Flavor}} --",
		}
		if err := (&Config{}).AddRemoteProfile(ok); err != nil {
			t.Errorf("expected accept: ephemeral connect using {{.Flavor}}: %v", err)
		}
	})
}

func TestMigrateRemoteFlavorsToProfiles_Idempotent(t *testing.T) {
	cfg := &Config{ConfigData: ConfigData{
		RemoteFlavors: []RemoteFlavor{
			{
				ID:            "old_flavor",
				Flavor:        "old-host",
				DisplayName:   "Old Host",
				VCS:           "git",
				WorkspacePath: "~/workspace",
			},
		},
		RemoteProfiles: []RemoteProfile{
			{
				ID:            "existing_profile",
				DisplayName:   "Existing",
				VCS:           "git",
				WorkspacePath: "~/workspace",
				Flavors:       []RemoteProfileFlavor{{Flavor: "existing-flavor"}},
			},
		},
	}}

	cfg.MigrateRemoteFlavorsToProfiles()

	// Should not add any new profiles since RemoteProfiles already has entries
	if len(cfg.RemoteProfiles) != 1 {
		t.Fatalf("expected 1 profile (unchanged), got %d", len(cfg.RemoteProfiles))
	}
	if cfg.RemoteProfiles[0].ID != "existing_profile" {
		t.Errorf("profile should be unchanged, got ID %q", cfg.RemoteProfiles[0].ID)
	}
}
