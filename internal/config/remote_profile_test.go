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
	cfg := &Config{
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
	}

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

func TestMigrateRemoteFlavorsToProfiles_Idempotent(t *testing.T) {
	cfg := &Config{
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
	}

	cfg.MigrateRemoteFlavorsToProfiles()

	// Should not add any new profiles since RemoteProfiles already has entries
	if len(cfg.RemoteProfiles) != 1 {
		t.Fatalf("expected 1 profile (unchanged), got %d", len(cfg.RemoteProfiles))
	}
	if cfg.RemoteProfiles[0].ID != "existing_profile" {
		t.Errorf("profile should be unchanged, got ID %q", cfg.RemoteProfiles[0].ID)
	}
}

