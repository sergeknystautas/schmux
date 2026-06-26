# Fenced Sessions

## What it does

Fenced sessions let the user check **Fence** in the spawn wizard to run that spawned session under the [`fence`](https://fencesandbox.com/docs/guides/agents) OS sandbox. The feature is per-spawn and opt-in: unchecked spawns behave as they did before.

The intended behavior is narrow:

- Known, descriptor-backed harnesses run inside `fence` and also receive their descriptor-defined unattended/skip-approval args.
- Raw commands and user-defined run targets run inside `fence` only; schmux treats their command strings as opaque and does not add harness flags.
- Oneshot, remote sessions, floor-manager sessions, and launch paths without the visible spawn checkbox are out of scope.

Fence is a guardrail around the process tree, not a rewrite of agent permissions. Schmux delegates OS policy to the Fence `code` template and only adds spawn-specific paths/domains.

## Key files

| File                                        | Purpose                                                                           |
| ------------------------------------------- | --------------------------------------------------------------------------------- |
| `assets/dashboard/src/routes/SpawnPage.tsx` | Renders the Fence checkbox and sends `fence` on spawn requests                    |
| `internal/api/contracts/spawn_request.go`   | `SpawnRequest.Fence` API contract                                                 |
| `internal/dashboard/handlers_spawn.go`      | Server-side fence gate and dependency lookup                                      |
| `internal/session/manager.go`               | Builds final agent command, adds harness unattended args, wraps before tmux spawn |
| `internal/fence/fence.go`                   | Writes per-session Fence settings/script and returns the wrapper command          |
| `internal/workspace/fence_paths.go`         | Adds git worktree shared `.git` paths to Fence writable paths                     |
| `internal/detect/descriptors/*.yaml`        | Harness `auto_approve_args` definitions                                           |
| `internal/detect/dependency_registry.go`    | `fence` dependency entry and install hints                                        |
| `internal/schmuxdir/schmuxdir.go`           | `~/.schmux/fence/<session-id>/` path helper                                       |

## Spawn contract

`POST /api/spawn` accepts:

```json
{
  "fence": true
}
```

`false` or omitted means unchanged behavior.

When `fence:true` is accepted, the backend must either run the session fenced or fail before spawning. It must not silently run the session unfenced.

Hard failures:

- `fence` is not detected in the daemon dependency report.
- The spawn is remote.

## UI behavior

A daemon-level `fence_mode` config flag (Experimental tab) gates the feature in
addition to binary detection. It has three modes:

- **Disabled** — the spawn checkbox is hidden and the backend rejects
  `fence:true` (`"fenced sessions are disabled"`).
- **Optional, default off** (the default) — the checkbox is shown and starts
  unchecked. This is the historical behavior and is the absence of the field in
  `config.json`.
- **Optional, default on** — the checkbox is shown and starts checked; still
  toggleable per spawn.

The spawn page shows the checkbox only when:

- `fence_mode` is not `disabled`, and
- the dependency report says `fence` is available, and
- the selected spawn environment is local.

In the Experimental tab the fence card is grayed out, with an install hint, when
the `fence` binary is not detected. The per-spawn checkbox is not persisted.
Quick-launch shortcuts or other launch paths that do not expose this checkbox
must send `fence:false`.

## Command behavior

### Descriptor-backed harnesses

For known harnesses, fenced spawns add that harness's descriptor-defined `auto_approve_args` before wrapping the command. Examples live in `internal/detect/descriptors/*.yaml`:

| Harness     | Current fenced arg                           |
| ----------- | -------------------------------------------- |
| Claude      | `--dangerously-skip-permissions`             |
| Codex       | `--dangerously-bypass-approvals-and-sandbox` |
| Gemini      | `--yolo`                                     |
| Antigravity | `--dangerously-skip-permissions`             |
| OpenCode    | none                                         |

These args must come from the resolved harness adapter/descriptor. Do not infer them from target names, labels, command strings, or other loose matching.

### Raw and user-defined commands

Raw `command` spawns and user-defined run targets are opaque. When fenced, schmux wraps the final command in Fence but does not add approval args, model args, or resume args. If the command still prompts, that is a harness/command limitation, not a schmux error.

## Fence wrapper

The session manager builds the final command first, including:

- model flags,
- persona/style injection,
- signaling environment variables,
- descriptor auto-approval args when applicable.

Then, immediately before `tmux CreateSession`, it calls the Fence wrapper. The wrapper writes:

```text
~/.schmux/fence/<session-id>/settings.json
~/.schmux/fence/<session-id>/cmd.sh
~/.schmux/fence/<session-id>/monitor.log
```

`cmd.sh` is read by `/bin/sh`. It first exports workspace-local cache environment variables, then appends the final command verbatim. This avoids nested shell quoting through `fence -c` and keeps routine tool caches out of the user's home directory while fenced.

The tmux command has this shape:

```bash
fence -m --fence-log-file ~/.schmux/fence/<session-id>/monitor.log \
  --settings ~/.schmux/fence/<session-id>/settings.json \
  /bin/sh ~/.schmux/fence/<session-id>/cmd.sh
```

Modes:

| Path                            | Mode             |
| ------------------------------- | ---------------- |
| `~/.schmux/fence/<session-id>/` | `0700`           |
| `settings.json`                 | `0600`           |
| `cmd.sh`                        | `0600`           |
| `monitor.log`                   | created by Fence |

Do not store the launch files inside the workspace. The fenced process can write the workspace, so workspace-local launch files would let the agent tamper with future respawns.

## Generated Fence settings

Schmux starts from Fence's `code` template. The Fence guide recommends using the `code` template for coding agents, allowlisting only the network destinations needed, and enabling monitor mode to audit blocked attempts.

Generated settings add only spawn-specific entries. The `allowedDomains` entries and the per-language/socket allowances (Unix sockets, Go/Node/Python caches, Go telemetry) come from the spawning repo's `.schmux/config.json` `fence` block (see [Per-repo fence config](#per-repo-fence-config)) rather than being unconditional; a repo with no `fence` block gets only the universal baseline.

```json
{
  "extends": "code",
  "network": {
    "allowedDomains": ["mcp.posthog.com", "api.z.ai"],
    "allowAllUnixSockets": true
  },
  "filesystem": {
    "allowRead": ["/Users/me/.schmux/fence/<session-id>/cmd.sh"],
    "allowWrite": ["/Users/me/workspaces/project", "/Users/me/.schmux/repos/project.git"]
  }
}
```

### Filesystem policy

The `code` template provides credential read-deny rules and restricted write policy. Schmux appends:

- the workspace path to `filesystem.allowWrite`, and
- any VCS control path that must be writable outside the workspace, and
- Go's telemetry directory under `os.UserConfigDir()/go/telemetry`, when the repo opts into the `golang` preset.

For git worktrees, commits write to the shared git common directory outside the worktree. `internal/workspace/fence_paths.go` finds that path with `git rev-parse --git-common-dir` and adds it to `allowWrite`.

Go telemetry mode is not configurable with an environment variable: `go env GOTELEMETRY` and `GOTELEMETRYDIR` are read-only values. The official Go telemetry docs say local data and configuration live under `os.UserConfigDir()/go/telemetry`, so schmux permits that narrow path instead of trying to redirect it.

The `code` template is not a full default-deny read sandbox. It is intended to protect sensitive credential paths while allowing normal development reads. If schmux needs “can only read this workspace,” that is a different policy and should be designed explicitly rather than assumed from `code`.

### Local tool state

Fenced launch scripts export local cache paths under:

```text
<workspace>/.cache/schmux-fence/
```

This path is git-excluded via `fence.WorkspaceExcludePatterns()`, which the workspace ensurer folds into `.git/info/exclude` — so a workspace first fenced after creation stops leaking these caches into `git status` on its next spawn or daemon restart.

The baseline (always on, any fenced repo) redirects `XDG_CACHE_HOME` and an empty Git template directory (`GIT_TEMPLATE_DIR`) so `git init` does not write default hooks. Everything else is opt-in via the repo's `fence.presets`:

- `golang`: Go build cache (`GOCACHE`), `GOFLAGS=-modcacherw` so any Go module cache remains user-cleanable, Staticcheck cache (`STATICCHECK_CACHE`).
- `node`: npm (`NPM_CONFIG_CACHE`/`npm_config_cache`), Yarn (`YARN_CACHE_FOLDER`), Bun (`BUN_INSTALL_CACHE_DIR`).
- `python`: pip (`PIP_CACHE_DIR`), uv (`UV_CACHE_DIR`).
- `docker`: access to the host Docker daemon so containerized tests (`--e2e`, `--scenarios`) run inside a fenced session. Sets `allowAllUnixSockets` (daemon socket), redirects `DOCKER_CONFIG` to a writable workspace dir, writes a `cliPluginsExtraDirs` config so `docker buildx`/`compose` resolve, and allows the two Docker Hub auth/registry endpoints `docker build` resolves base-image metadata against client-side through buildx. Implies the socket access `tmux` grants, so a repo using `docker` need not also list `tmux`.

Schmux does not redirect `TMPDIR`/`TMP`/`TEMP`: tests often create git repos under temporary directories, and moving those directories into the writable workspace makes Fence block `.git/config` writes. Schmux also does not redirect `GOMODCACHE`: downloaded modules can legitimately contain fixture names such as `cert.pem`, which the Fence credential-write policy blocks inside writable workspaces. These are environment defaults for fenced sessions, not Fence policy exceptions.

### Network policy

Fence blocks outbound network except allowed domains from the template plus generated additions.

Schmux can know model endpoint hosts for resolved model runners. For example, a third-party Anthropic-compatible Claude runner with endpoint `https://api.z.ai/api/anthropic` adds:

```json
"network": { "allowedDomains": ["api.z.ai"] }
```

A repo adds its own service endpoints (for example `mcp.posthog.com`) via `fence.allowed_domains` in its `.schmux/config.json`; schmux no longer hardcodes any app domains.

Do not guess network domains from arbitrary command strings. Unknown blocked destinations should appear in `monitor.log`, then the implementation can add a real source-of-truth if the destination is legitimate.

The `tmux` preset sets `network.allowAllUnixSockets:true`. Local developer tooling commonly creates Unix sockets for IPC (for example test runners and tmux-related tests). Fence's narrower `allowUnixSockets` setting is for connecting to specific socket paths, not creating arbitrary per-run socket files.

## Policy boundaries

The default fenced-session policy is not a promise that every local development workflow can run to completion inside Fence. Do not expand the default policy just because monitor logs show a blocked operation.

These should stay out of the default policy:

- package-manager mutation outside the workspace, such as Homebrew writes under `/opt/homebrew` or `~/Library/Caches/Homebrew`,
- agent self-update/download traffic, such as Claude Code auto-update requests,
- Docker daemon/config access under `~/.docker` or Docker socket access (excluded from the baseline; available via the explicit `docker` preset — see [Security note: docker preset](#security-note-docker-preset) below),
- language toolchain installation or upgrade downloads,
- analytics/telemetry endpoints unrelated to schmux functionality,
- broad home-directory cache writes, and
- temporary directory rewrites that move arbitrary test fixtures into the writable workspace.

When one of these is needed, the answer is not to silently broaden the default fence. The user should run that setup outside the fenced session, or schmux should expose an explicit opt-in configuration.

### Security note: docker preset

Enabling the `docker` preset gives the fenced session full access to the host Docker daemon. The Docker socket is effectively root on the host: a container can mount the host filesystem (`docker run -v /:/host`) and read or write anything. The preset is an explicit per-repo opt-in for exactly this reason; do not enable it casually. It also allows two Docker Hub endpoints (`auth.docker.io`, `registry-1.docker.io`) because `docker build` resolves the base image's auth token and manifest client-side through buildx; layer blobs still pull daemon-side, so the layer CDNs are not allowlisted.

## Per-repo fence config

A repo customizes its fenced sessions through a `fence` block in its own
`.schmux/config.json` (loaded at spawn by `workspace.LoadRepoConfig`):

```json
{
  "fence": {
    "presets": ["golang", "node", "tmux"],
    "allowed_domains": ["mcp.posthog.com"]
  }
}
```

- `presets` opt into core-defined bundles. Available: `golang` (GOCACHE,
  STATICCHECK_CACHE, `GOFLAGS=-modcacherw`, Go telemetry write), `node`
  (npm/yarn/bun caches), `python` (pip/uv caches), `tmux`
  (`allowAllUnixSockets`, for sessions that create local sockets), `docker`
  (Docker daemon socket + `DOCKER_CONFIG` redirect + Docker Hub pull domains,
  for running `--e2e`/`--scenarios` inside a fence — see [Security note](#security-note-docker-preset)).
- `allowed_domains` add network destinations to the baseline allowlist.

The always-on baseline (any fenced repo) is `extends: "code"`, the workspace +
git-worktree writable paths, the `cmd.sh` read, auto model-endpoint domains, and
the generic `GIT_TEMPLATE_DIR`/`XDG_CACHE_HOME` caches. Anything language- or
workload-specific is now a preset; a repo with no `fence` block gets the baseline
only.

## Monitor logs

All fenced sessions run Fence monitor mode (`-m`) and write logs to:

```text
~/.schmux/fence/<session-id>/monitor.log
```

Use this file to debug blocked network and sandbox violations. This is the first place to check when a fenced agent says an API connection, package fetch, or file operation was blocked.

## Lifecycle

Do not eagerly delete Fence launch directories. A tmux pane respawn may re-read `cmd.sh` and `settings.json`, and running processes may still reference the paths. v1 does not include cleanup. Safe cleanup requires checking process liveness and is intentionally out of scope.

## Known limitations

- Remote sessions are not fenced.
- Oneshot commands are not fenced.
- Raw/user-defined commands may still prompt because schmux does not know their harness-specific unattended flags.
- OpenCode currently has no descriptor `auto_approve_args`, so it can be fenced but may not run unattended.
- The `code` template does not imply default-deny reads of all non-workspace paths.
- On macOS, Fence multi-token command deny rules for child processes are limited unless agent hooks are installed; do not rely on command-deny rules as the primary safety property.

## Testing checklist

When changing this feature, cover:

- unchecked spawn leaves command unchanged,
- checked spawn wraps the final tmux command,
- descriptor-backed harness appends `auto_approve_args`,
- raw/user command does not get harness args,
- generated settings include workspace write path and git worktree common dir,
- generated settings include known model endpoint domains,
- wrapper command enables monitor mode and points at `monitor.log`,
- UI does not send stale `fence:true` when the checkbox is hidden,
- remote + fence fails before spawning.

## Writing tests that run inside a fence

schmux is often developed inside its own fenced sessions, so the backend and frontend unit suites are expected to pass with `FENCE_SANDBOX=1`. The `code` template's `denyWrite` blocks creating files named

```
**/.env  **/.env.*  **/*.key  **/*.pem  **/*.p12  **/*.pfx
```

by filename, everywhere the process can write — `t.TempDir()` included. `rename`/`link` into those names is blocked too, and `allowWrite` does not override the deny. A test that writes such a file fails in-fence with `operation not permitted`.

Pick the remediation by what the test actually needs:

| The test needs…                                              | Do this                                                              | Example                                           |
| ------------------------------------------------------------ | -------------------------------------------------------------------- | ------------------------------------------------- |
| a credential-named file to merely _exist_ (stat/read only)   | commit a static fixture under `testdata/` with a non-blocked name    | `internal/config/testdata/tls/{cert,key}`         |
| _some_ file whose name is incidental                         | rename it to a non-blocked name                                      | `overlay_test.go`: `.env` → `app.conf`            |
| to exercise production that writes a _fixed_ credential name | route the package's fs access through a seam, swap an in-memory fake | `internal/dashboardsx` (`fs.go` + `installMemFS`) |

`--e2e` and `--scenarios` need the Docker socket, which the _baseline_ fence blocks. Enable the `docker` fence preset (`.schmux/config.json`) to run them inside a fenced session; otherwise run them on the host or in CI.
