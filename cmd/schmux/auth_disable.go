package main

import (
	"fmt"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/daemon"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// disableAuth turns access_control.enabled off in the config file without
// validation (recovery path) and, if a restart function is provided, applies it
// so the running daemon picks up the change. Credentials and the session secret
// are left intact.
func disableAuth(configPath string, restart func() error) error {
	if err := config.DisableAuthRaw(configPath); err != nil {
		return err
	}
	if restart != nil {
		if err := restart(); err != nil {
			return fmt.Errorf("config updated but daemon restart failed: %w", err)
		}
	}
	return nil
}

// runAuthDisable is the CLI entry point: it disables auth and restarts the
// daemon only if it is currently running.
func runAuthDisable() error {
	restart := func() error {
		running, _, _, err := daemon.Status()
		if err != nil || !running {
			return nil // not running: next start picks up the change
		}
		if err := daemon.Stop(); err != nil {
			return err
		}
		return daemon.Start()
	}
	if err := disableAuth(schmuxdir.ConfigPath(), restart); err != nil {
		return err
	}
	fmt.Println("GitHub authentication disabled. The dashboard is reachable again.")
	fmt.Println("Fix the OAuth credentials before re-enabling auth in Settings → Access.")
	return nil
}
