VERDICT: NEEDS_REVISION

## Summary Assessment

The approach is sound -- making `~/.schmux/` configurable is the right lever to pull, and the resolution priority (flag > env > default) is correct. However, the design significantly underestimates the scope of hardcoded `~/.schmux` paths and omits several packages that contain their own `os.UserHomeDir()` + `.schmux` joins, which would silently ignore the configured directory.

## Critical Issues (must fix)

### 1. The inventory of packages needing changes is incomplete

The design lists `internal/config/`, `internal/daemon/`, `pkg/cli/`, `internal/models/`, `internal/dashboardsx/`, `internal/oneshot/`, `cmd/schmux/timelapse.go`, and `cmd/schmux/auth_github.go`. But a codebase search reveals **at least 10 additional locations** with hardcoded `os.UserHomeDir() + ".schmux"` paths that the design does not mention:

- `internal/config/secrets.go` -- `secretsPath()` hardcodes `~/.schmux/secrets.json`. Every secrets operation (load, save, auth, session secret) bypasses the configured dir. This is critical because two instances would share the same secrets file.
- `internal/config/config.go` -- `ConfigExists()` and `EnsureExists()` hardcode `~/.schmux/config.json`. The design says `EnsureExists(configDir)` but does not mention `ConfigExists()`, which is called by `EnsureExists()`.
- `internal/config/config.go` -- `GetWorktreeBasePath()` defaults to `~/.schmux/repos` and `GetQueryRepoPath()` defaults to `~/.schmux/query`. Two instances would clobber each other's bare repos.
- `internal/workspace/overlay.go` -- `OverlayDir()` hardcodes `~/.schmux/overlays/<repoName>`.
- `internal/lore/scratchpad.go` -- `LoreStateDir()` and `LoreStatePath()` hardcode `~/.schmux/lore/<repoName>`.
- `internal/logging/logging.go` -- log path hardcodes `~/.schmux/daemon-startup.log`. Two instances would interleave log output in the same file.
- `internal/floormanager/manager.go` -- work directory hardcodes `~/.schmux/floor-manager`.
- `internal/assets/download.go` -- dashboard asset cache hardcodes `~/.schmux/dashboard`.
- `internal/detect/adapter_claude_hooks.go` -- `EnsureGlobalHookScripts()` takes `homeDir` but then hardcodes `.schmux/hooks` relative to it. This would need to accept the schmux dir directly instead.
- `internal/dashboard/` -- at least 6 handler files (`handlers_dev.go`, `handlers_subreddit.go`, `handlers_timelapse.go`, `handlers_usermodels.go`, `handlers_lore.go`) each independently call `os.UserHomeDir()` and join `.schmux`.

Without addressing these, instance 2's daemon would silently read/write files in instance 1's `~/.schmux/` directory for secrets, overlays, lore state, log files, floor manager data, and dashboard handler paths. That is the exact cross-talk the design intends to prevent.

### 2. `config.EnsureSessionSecret()` and all secrets functions ignore the config dir

`secretsPath()` in `internal/config/secrets.go` is a package-level function that calls `os.UserHomeDir()` directly. It is used by `LoadSecretsFile()`, `SaveSecretsFile()`, `EnsureSessionSecret()`, and every secrets accessor. The design does not address this at all. Since both instances would share the same `secrets.json`, one instance modifying secrets would corrupt the other's state.

This needs a mechanism to thread the schmux dir into secrets operations -- either by making `secretsPath` accept a parameter, or by adding a package-level setter that is called during daemon initialization.

### 3. `schmux stop` does not accept or resolve a config dir

The design lists changes for `Start`, `Status`, and `ValidateReadyToRun`, but does not mention `Stop()`. Looking at `daemon.Stop()`, it hardcodes `filepath.Join(homeDir, ".schmux", pidFileName)`. Without the config dir, `schmux --config-dir ~/.schmux-work stop` would try to stop the default instance, not instance 2.

### 4. CLI commands that use `cli.ResolveURL()` also need the config dir

The current `ResolveURL()` in `pkg/cli/daemon_client.go` reads from `~/.schmux/daemon.url`. The design proposes `DaemonURLFromDir(configDir)` but does not explain how CLI subcommands like `spawn`, `list`, `attach`, `dispose`, `tell`, `events`, `capture`, `inspect`, `branches`, `repofeed`, `end-shift`, and `remote` would receive the resolved config dir. In `main.go`, each of these calls `cli.ResolveURL()` directly. The design says the `--config-dir` flag is "stripped from os.Args so subcommands never see it" but never describes how the resolved dir reaches these call sites.

### 5. The `dev.sh` restart mechanism is not addressed

When the daemon does a dev restart (exit code 42), `dev.sh` re-launches `schmux daemon-run`. The design says `Start()` propagates via `SCHMUX_HOME` env var, but the dev.sh restart loop is a separate codepath. If `dev.sh` does not propagate `SCHMUX_HOME` (it currently does not reference it at all), dev mode restarts would lose the config dir and revert to `~/.schmux/`.

## Suggestions (nice to have)

### 1. Consider a package-level `SetSchmuxDir()` pattern instead of threading through every constructor

Given 50+ call sites across 25 files, threading the config dir as a parameter through every function is a large and error-prone refactor. An alternative is a package-level `SetSchmuxDir()` that is called once at startup, with a `GetSchmuxDir()` that returns it (defaulting to `~/.schmux/`). This is the same pattern already used for loggers (`lore.SetLogger()`, `config.SetLogger()`, etc.). It would reduce the diff size significantly and make it harder to miss a call site.

### 2. Add a startup log line showing the resolved config dir

When a non-default config dir is in use, log it prominently at startup. This helps users debug which instance they are interacting with.

### 3. Validate that two instances are not pointing at the same config dir

If `schmux --config-dir ~/.schmux start` is run while the default instance is already running (both resolve to `~/.schmux/`), the PID check in `ValidateReadyToRun` would catch it. But if the user sets `SCHMUX_HOME=~/.schmux` explicitly, there is an implicit expectation that this is a separate instance. The design should note that the PID check is the safety net here and that this is by design.

### 4. Document what happens if `tmux_socket` is not set in the second config

The design says "the user must configure `tmux_socket`" but does not describe what happens if they forget. Both instances would use the default `schmux` socket, and sessions from instance 2 would appear in instance 1's tmux namespace. A startup warning when two instances share a socket name would be helpful.

### 5. The `--config-dir` flag parsing should handle `=` syntax

The design says `--config-dir <path>` but users may also write `--config-dir=~/.schmux-work`. The parsing logic should handle both forms.

## Verified Claims (things you confirmed are correct)

- **tmux isolation already exists**: Confirmed. `TmuxServer` uses `-L socketName` for all commands, and `GetTmuxSocketName()` reads from config. Setting `tmux_socket` in a separate config would fully isolate tmux sessions.
- **Port binding already configurable**: Confirmed. `GetPort()` reads from `config.json`.
- **`models.New()` already takes `schmuxDir`**: Confirmed. The `Manager` struct has a `schmuxDir` field, and `CachePath`, `SaveCache`, `LoadCache` all use it. This subsystem is already properly parameterized.
- **`config.Load()` already accepts a path**: Confirmed. `Load(configPath string)` takes the full path to `config.json`.
- **`state.Load()` already accepts a path**: Confirmed. `Load(statePath string, ...)` takes the full path.
- **`workspace.New()` and `session.New()` do not hardcode `~/.schmux`**: Confirmed. They receive config/state objects and paths from the daemon. The hardcoded paths are in `daemon.go` where these are constructed.
- **`SCHMUX_URL` env var for CLI URL resolution already exists**: Confirmed. `ResolveURL()` checks `SCHMUX_URL` first.
- **Browser cookie isolation by port**: Confirmed. Cookies are scoped to host:port by browsers, so different ports mean different cookie jars.
- **Propagation via `SCHMUX_HOME` env on the child process is viable**: Confirmed. `Start()` creates the child via `exec.Command` and `cmd.Env` can be set before `cmd.Start()`. The child's `daemon-run` would then pick up `SCHMUX_HOME` during resolution.
