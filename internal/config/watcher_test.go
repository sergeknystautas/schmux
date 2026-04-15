package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// writeConfigFile writes a minimal valid config JSON to the given path.
func writeConfigFile(t *testing.T, path string, name string) {
	t.Helper()
	cfg := ConfigData{
		Repos: []Repo{{
			URL:      "https://github.com/test/repo",
			Name:     name,
			BarePath: "repo.git", // Set explicitly to avoid bare_path migration calling Save()
		}},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestConfigWatcher_ReloadsOnExternalChange(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	// Write initial config
	writeConfigFile(t, configPath, "Initial")

	// Load config
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Repos[0].Name != "Initial" {
		t.Fatalf("initial Name: got %q", cfg.Repos[0].Name)
	}

	// Track broadcast calls
	var broadcastCalled atomic.Int32
	broadcast := func() { broadcastCalled.Add(1) }

	// Start watcher
	w := NewConfigWatcher(cfg, nil, broadcast)
	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer w.Stop()

	// Write modified config externally
	writeConfigFile(t, configPath, "Modified")

	// Wait for reload (debounce 500ms + margin)
	deadline := time.After(3 * time.Second)
	for {
		if cfg.Repos[0].Name == "Modified" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("config was not reloaded within timeout; Name=%q", cfg.Repos[0].Name)
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Broadcast should have fired
	if broadcastCalled.Load() == 0 {
		t.Error("broadcast was not called after external change")
	}
}

func TestConfigWatcher_IgnoresSelfWrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	writeConfigFile(t, configPath, "Original")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var broadcastCalled atomic.Int32
	broadcast := func() { broadcastCalled.Add(1) }

	w := NewConfigWatcher(cfg, nil, broadcast)
	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer w.Stop()

	// Simulate a self-write by calling Save() which sets lastSaveCompletedAt
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Wait long enough for the debounce + event delivery
	time.Sleep(1500 * time.Millisecond)

	// Broadcast should NOT have fired (suppressed by grace window)
	if broadcastCalled.Load() > 0 {
		t.Error("broadcast should not fire for self-writes")
	}
}

func TestConfigWatcher_DebouncesRapidWrites(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	writeConfigFile(t, configPath, "v0")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var mu sync.Mutex
	broadcastCount := 0
	broadcast := func() {
		mu.Lock()
		broadcastCount++
		mu.Unlock()
	}

	w := NewConfigWatcher(cfg, nil, broadcast)
	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer w.Stop()

	// Write 5 times rapidly
	for i := 1; i <= 5; i++ {
		writeConfigFile(t, configPath, "rapid")
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for debounce to settle (500ms debounce + margin)
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	count := broadcastCount
	mu.Unlock()

	if count == 0 {
		t.Error("broadcast should have fired at least once")
	}
	if count > 2 {
		// Debounce should collapse rapid writes. Allow up to 2 (some editors
		// produce separate WRITE+CREATE events that may not fully coalesce).
		t.Errorf("expected at most 2 broadcasts from debouncing, got %d", count)
	}
}

func TestConfigWatcher_StopIsSafe(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	writeConfigFile(t, configPath, "test")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	w := NewConfigWatcher(cfg, nil, nil)
	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Stop should be idempotent
	w.Stop()
	w.Stop()

	// Writing after stop should not panic
	writeConfigFile(t, configPath, "after-stop")
	time.Sleep(200 * time.Millisecond)
}

func TestConfig_TimeSinceLastSave_NeverSaved(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	writeConfigFile(t, configPath, "test")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Never saved — should return a large duration
	elapsed := cfg.TimeSinceLastSave()
	if elapsed < time.Minute {
		t.Errorf("TimeSinceLastSave before any save should be large, got %v", elapsed)
	}
}

func TestConfig_TimeSinceLastSave_AfterSave(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	writeConfigFile(t, configPath, "test")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	elapsed := cfg.TimeSinceLastSave()
	if elapsed > time.Second {
		t.Errorf("TimeSinceLastSave immediately after save should be small, got %v", elapsed)
	}
}

func TestConfig_FilePath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	writeConfigFile(t, configPath, "test")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := cfg.FilePath()
	if got != configPath {
		t.Errorf("FilePath: got %q, want %q", got, configPath)
	}
}

func TestConfig_FilePath_EmptyWhenNotLoaded(t *testing.T) {
	cfg := &Config{}
	if got := cfg.FilePath(); got != "" {
		t.Errorf("FilePath on unloaded config should be empty, got %q", got)
	}
}
