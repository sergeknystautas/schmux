# Fix: npm Environment Variable Leak into Child Processes

## Problem

When schmux runs via `./dev.sh`, the launch chain is:

    user's shell (clean env)
    -> dev.sh
    -> exec npx --prefix tools/dev-runner tsx ...  (npm pollutes env)
    -> dev-runner Node process
    -> spawns ./tmp/schmux daemon-run  (inherits polluted env)
    -> daemon creates tmux sessions   (agents inherit polluted env)

`npx` injects environment variables that redirect npm's prefix, global-prefix, and local-prefix to `tools/dev-runner/`. Any npm command run inside a spawned session (e.g., by a Claude Code agent) uses these wrong prefixes, breaking `npm link`, `npm install -g`, and similar operations.

## Empirical Evidence

Diffing env before and after `npx --prefix tools/dev-runner` shows npx:

**Adds** (did not exist before in a clean shell):

- `INIT_CWD`
- `npm_config_call`
- `npm_config_global_prefix`
- `npm_config_local_prefix`
- `npm_config_prefix`
- `npm_lifecycle_script`
- `npm_node_execpath`
- `npm_package_json`
- `npm_package_name`
- `npm_command`
- `npm_config_cache`
- `npm_config_globalconfig`
- `npm_config_init_module`
- `npm_config_node_gyp`
- `npm_config_noproxy`
- `npm_config_npm_version`
- `npm_config_user_agent`
- `npm_config_userconfig`
- `npm_execpath`
- `NODE`

**Modifies** (existed before, value changed):

- `PATH` — prepends `node_modules/.bin` directory chains
- `SHLVL` — incremented
- `COLOR` — changed

**Important caveat:** A user could legitimately have `npm_config_*` vars in their shell profile (e.g., `npm_config_registry` for a private registry). npx overwrites all `npm_config_*` vars with its own resolved values. Blindly deleting all `npm_*` vars would lose the user's original config. The fix must restore pre-npx values, not just delete.

## Design

### Approach: Snapshot pre-npx env, restore at daemon boundary

The pollution originates when `npx` spawns the dev-runner. The fix belongs where the dev-runner spawns the daemon — not downstream in Go. This way the daemon process starts with a clean environment and every subsystem (tmux sessions, agent detection, workspace git ops) benefits without any Go changes.

**`dev.sh`** — Before `exec npx`, snapshot the pre-npx state:

```bash
# Save any npm_* vars the user already has (e.g., npm_config_registry from .zshrc)
# so they can be restored after npx overwrites them. NUL-delimited, base64-encoded.
export SCHMUX_PRISTINE_NPM_VARS="$(env -0 | grep -z '^npm_' | base64)"
export SCHMUX_PRISTINE_PATH="$PATH"
```

Two snapshots:

- `SCHMUX_PRISTINE_PATH` — PATH before npx prepends `node_modules/.bin` chains
- `SCHMUX_PRISTINE_NPM_VARS` — all `npm_*` vars that existed before npx (base64-encoded, NUL-delimited `KEY=VALUE` pairs). Empty string if none existed.

**`tools/dev-runner/src/lib/cleanEnv.ts`** — New utility function `cleanEnv()` that restores the pre-npx environment:

1. Copy `process.env`
2. Delete all keys starting with `npm_` (strip everything npx injected or overwrote)
3. Decode `SCHMUX_PRISTINE_NPM_VARS` and restore any `npm_*` vars that existed before npx (preserving the user's original values)
4. Delete `INIT_CWD` and `NODE` (added by npx, never present in a clean shell)
5. If `SCHMUX_PRISTINE_PATH` exists, set `PATH` to its value
6. Delete `SCHMUX_PRISTINE_PATH` and `SCHMUX_PRISTINE_NPM_VARS` (meta-vars should not leak)
7. Return the cleaned env object

**`tools/dev-runner/src/App.tsx`** — Pass `env: cleanEnv()` to the daemon's `useProcess` call (line 156). The Vite process keeps `process.env` unchanged since it's npm tooling that expects the npm context.

### Why This Is Accurate

- All `npm_*` vars are stripped, then only the ones that existed _before_ npx are restored with their original values. This handles both the common case (user has no npm vars → all get stripped) and the edge case (user has `npm_config_registry` in their profile → it survives).
- `PATH` is restored from a snapshot taken immediately before npx modified it.
- `INIT_CWD` and `NODE` are always stripped — these never exist in a normal shell environment.
- When not running via `dev.sh` (no `SCHMUX_PRISTINE_*` vars present), the env passes through unchanged — zero behavior change for production usage.

### Why this layer

The pollution happens at the npx → dev-runner boundary. The dev-runner is the component that spawns the daemon. Cleaning the env here means the daemon never sees the pollution — every daemon subsystem (tmux sessions, agent detection, workspace commands, tunnel management) gets a clean environment without needing per-subsystem fixes in Go. Fixing downstream in Go would be patching the symptom where it manifests rather than the cause where it originates.

### Files Changed

| File                                   | Change                                                                           |
| -------------------------------------- | -------------------------------------------------------------------------------- |
| `dev.sh`                               | Snapshot `SCHMUX_PRISTINE_PATH` and `SCHMUX_PRISTINE_NPM_VARS` before `exec npx` |
| `tools/dev-runner/src/lib/cleanEnv.ts` | New utility: restore pre-npx env from snapshots                                  |
| `tools/dev-runner/src/App.tsx`         | Pass `env: cleanEnv()` to daemon `useProcess` call                               |
| `docs/dev-mode.md`                     | Add "Environment isolation" section documenting the problem and fix mechanism    |

### Verification

1. Run `./dev.sh`
2. Spawn a session from the dashboard
3. Inside the session, run `npm config list`
4. Confirm `prefix` points to the system npm location (e.g., `/opt/homebrew`), not `tools/dev-runner`
5. Confirm `echo $PATH` does not contain `dev-runner/node_modules/.bin`
6. Run `./test.sh` — all tests pass
