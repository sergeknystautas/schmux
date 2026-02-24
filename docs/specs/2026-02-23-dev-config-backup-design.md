# Dev Config Backup Design

## Overview

Automatically backup critical configuration files when the daemon starts in dev mode. This protects against accidental corruption during development when restarting, rebuilding, or switching workspaces.

## Requirements

- Backup happens on every daemon start when `devMode=true`
- Backs up `config.json`, `secrets.json`, `state.json` (only if they exist)
- Archives are stored in `~/.schmux/backups/`
- Filename format: `config-<timestamp>_<directory>.tar.gz`
- Backups older than 3 days are automatically deleted

## Trigger Point

In `internal/daemon/daemon.go`, `Run()` function:

```go
// After schmuxDir is created, before config/state load
if devMode {
    if err := createDevConfigBackup(schmuxDir); err != nil {
        logger.Warn("failed to create dev config backup", "err", err)
    }
}
```

## Implementation

### Backup Creation

1. Create `~/.schmux/backups/` directory if needed
2. Get current working directory name via `filepath.Base(os.Getwd())`
3. Generate timestamp in UTC: `2006-01-02T15-04-05`
4. Create tar.gz archive containing existing files from the list:
   - `config.json`
   - `secrets.json`
   - `state.json`
5. Store at `~/.schmux/backups/config-<timestamp>_<dirname>.tar.gz`

### Cleanup

1. Scan backups directory for `config-*.tar.gz` files
2. Check file modification time
3. Delete files older than 3 days
4. Log deletions

### Error Handling

- Backup failure: log warning, don't fail daemon startup
- Cleanup failure: log warning, continue
- Missing source files: skip silently (not an error)

## Files Modified

- `internal/daemon/daemon.go` - add `createDevConfigBackup()` function and call site

## Example

Running `./dev.sh` from `/Users/sergeknystautas/schmux-002` creates:

```
~/.schmux/backups/config-2026-02-23T19-33-00_schmux-002.tar.gz
```

Containing:

```
config.json
secrets.json
state.json
```
