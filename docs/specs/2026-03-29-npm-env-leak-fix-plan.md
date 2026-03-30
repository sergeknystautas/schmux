# npm Environment Leak Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent npm environment variables injected by `npx` in `dev.sh` from leaking into the schmux daemon and its child processes.

**Architecture:** `dev.sh` snapshots the pre-npx environment (PATH and any existing npm\_\* vars). A new `cleanEnv()` utility in the dev-runner decodes these snapshots and builds a restored environment. The daemon spawn in `App.tsx` uses this cleaned env. No Go changes needed.

**Tech Stack:** Bash (dev.sh), TypeScript (dev-runner), Node.js Buffer API for base64

**Spec:** `docs/specs/2026-03-29-npm-env-leak-fix-design.md`

---

### Task 1: Add environment snapshots to dev.sh

**Files:**

- Modify: `dev.sh:34-35`

- [ ] **Step 1: Add snapshot exports before the exec npx line**

In `dev.sh`, immediately before the `exec npx` line (line 35), add the two snapshot exports:

```bash
# ── Environment snapshot ─────────────────────────────────────────────────
# npx injects npm_config_*, npm_package_*, npm_lifecycle_*, INIT_CWD, NODE,
# and prepends node_modules/.bin to PATH. These leak into the daemon and
# break npm commands inside spawned sessions. Snapshot the pre-npx state
# so the dev-runner can restore it before spawning the daemon.
# See docs/dev-mode.md "Environment isolation" for details.
export SCHMUX_PRISTINE_NPM_VARS="$(env -0 | grep -z '^npm_' | base64)"
export SCHMUX_PRISTINE_PATH="$PATH"
```

The file should now read (around lines 34-43):

```bash
# ── Environment snapshot ─────────────────────────────────────────────────
# npx injects npm_config_*, npm_package_*, npm_lifecycle_*, INIT_CWD, NODE,
# and prepends node_modules/.bin to PATH. These leak into the daemon and
# break npm commands inside spawned sessions. Snapshot the pre-npx state
# so the dev-runner can restore it before spawning the daemon.
# See docs/dev-mode.md "Environment isolation" for details.
export SCHMUX_PRISTINE_NPM_VARS="$(env -0 | grep -z '^npm_' | base64)"
export SCHMUX_PRISTINE_PATH="$PATH"

# Delegate to TypeScript dev runner
exec npx --prefix "$RUNNER_DIR" tsx "$RUNNER_DIR/src/main.tsx" "$@"
```

- [ ] **Step 2: Verify the snapshot works**

Run from the repo root:

```bash
source dev.sh 2>/dev/null; echo "PATH snapshot length: ${#SCHMUX_PRISTINE_PATH}"
```

This will fail (dev.sh runs exec), so instead verify inline:

```bash
bash -c 'export SCHMUX_PRISTINE_NPM_VARS="$(env -0 | grep -z "^npm_" | base64)"; echo "snapshot length: ${#SCHMUX_PRISTINE_NPM_VARS}"'
```

Expected: prints a snapshot length (0 if no pre-existing npm vars, >0 if running inside an existing schmux dev session).

---

### Task 2: Create cleanEnv utility

**Files:**

- Create: `tools/dev-runner/src/lib/cleanEnv.ts`

- [ ] **Step 1: Create the cleanEnv module**

Create `tools/dev-runner/src/lib/cleanEnv.ts`:

```typescript
/**
 * Restore the pre-npx environment for the daemon process.
 *
 * When dev.sh runs `exec npx`, npm injects npm_config_*, npm_package_*,
 * npm_lifecycle_*, INIT_CWD, NODE, and modifies PATH. These vars break
 * npm commands inside spawned tmux sessions.
 *
 * dev.sh snapshots the pre-npx state into two env vars:
 * - SCHMUX_PRISTINE_PATH: PATH before npx prepended node_modules/.bin
 * - SCHMUX_PRISTINE_NPM_VARS: base64-encoded, NUL-delimited KEY=VALUE
 *   pairs of any npm_* vars that existed before npx
 *
 * This function strips the npx pollution and restores the originals.
 * When the snapshots are absent (not running via dev.sh), returns
 * process.env unchanged.
 */
export function cleanEnv(): Record<string, string> {
  const pristinePath = process.env.SCHMUX_PRISTINE_PATH;
  const pristineNpmB64 = process.env.SCHMUX_PRISTINE_NPM_VARS;

  // Not running via dev.sh — no snapshots, nothing to clean.
  if (pristinePath === undefined && pristineNpmB64 === undefined) {
    return { ...process.env } as Record<string, string>;
  }

  const env = { ...process.env } as Record<string, string>;

  // 1. Strip all npm_* vars (npx injected or overwrote these)
  for (const key of Object.keys(env)) {
    if (key.startsWith('npm_')) {
      delete env[key];
    }
  }

  // 2. Restore npm_* vars that existed before npx
  if (pristineNpmB64) {
    const decoded = Buffer.from(pristineNpmB64, 'base64').toString();
    // NUL-delimited KEY=VALUE pairs from `env -0 | grep -z '^npm_'`
    for (const entry of decoded.split('\0')) {
      if (!entry) continue;
      const eqIdx = entry.indexOf('=');
      if (eqIdx === -1) continue;
      const key = entry.substring(0, eqIdx);
      const value = entry.substring(eqIdx + 1);
      env[key] = value;
    }
  }

  // 3. Strip vars npx adds that never exist in a clean shell
  delete env.INIT_CWD;
  delete env.NODE;

  // 4. Restore PATH from snapshot
  if (pristinePath !== undefined) {
    env.PATH = pristinePath;
  }

  // 5. Remove our own meta-vars
  delete env.SCHMUX_PRISTINE_PATH;
  delete env.SCHMUX_PRISTINE_NPM_VARS;

  return env;
}
```

- [ ] **Step 2: Verify it compiles**

Run from repo root:

```bash
npx --prefix tools/dev-runner tsx --eval "import { cleanEnv } from './tools/dev-runner/src/lib/cleanEnv.js'; console.log(typeof cleanEnv)"
```

Expected: `function`

---

### Task 3: Wire cleanEnv into daemon spawn

**Files:**

- Modify: `tools/dev-runner/src/App.tsx:1-10` (import), `tools/dev-runner/src/App.tsx:156-161` (daemon useProcess)

- [ ] **Step 1: Add the import**

In `tools/dev-runner/src/App.tsx`, add the import alongside the other lib imports (around line 8-11):

```typescript
import { cleanEnv } from './lib/cleanEnv.js';
```

- [ ] **Step 2: Pass cleaned env to daemon useProcess**

Change the daemon `useProcess` call (around line 156-161) from:

```typescript
const backend = useProcess({
  command: binaryPath,
  args: ['daemon-run', '--dev-mode', '--dev-proxy'],
  onLine: addBackendLine,
  onExit: handleDaemonExit,
});
```

to:

```typescript
const backend = useProcess({
  command: binaryPath,
  args: ['daemon-run', '--dev-mode', '--dev-proxy'],
  env: cleanEnv(),
  onLine: addBackendLine,
  onExit: handleDaemonExit,
});
```

- [ ] **Step 3: Verify it builds**

Run from repo root:

```bash
npx --prefix tools/dev-runner tsc --noEmit --project tools/dev-runner/tsconfig.json 2>&1 || echo "No tsconfig, checking with tsx..." && npx --prefix tools/dev-runner tsx --eval "import './tools/dev-runner/src/App.js'"
```

Expected: no type errors.

---

### Task 4: Add Environment isolation section to docs/dev-mode.md

**Files:**

- Modify: `docs/dev-mode.md` (add new section before Troubleshooting)

- [ ] **Step 1: Add the Environment isolation section**

Insert the following section in `docs/dev-mode.md` immediately before the `## Troubleshooting` heading (before line 270):

```markdown
## Environment isolation

### The problem

`dev.sh` launches the dev-runner via `exec npx --prefix tools/dev-runner tsx ...`. When npm runs a script through `npx`, it injects environment variables into the child process:

- **`npm_config_prefix`**, **`npm_config_global_prefix`**, **`npm_config_local_prefix`** — set to `tools/dev-runner/`, redirecting npm's global install location
- **`npm_config_*`** — npm's full resolved configuration dumped as env vars
- **`npm_package_*`** — fields from dev-runner's `package.json`
- **`npm_lifecycle_*`** — script execution metadata
- **`INIT_CWD`**, **`NODE`** — npm execution context
- **`PATH`** — prepended with `node_modules/.bin` directory chains

If these vars leak into the schmux daemon, every child process inherits them — including tmux sessions where agents run. An agent running `npm install` or `npm link` inside a session would use `tools/dev-runner/` as its prefix instead of the system npm location.

### The fix

The fix has two parts:

**1. Snapshot (dev.sh):** Before `exec npx`, `dev.sh` exports two snapshot variables:

- `SCHMUX_PRISTINE_PATH` — the original PATH before npx modifies it
- `SCHMUX_PRISTINE_NPM_VARS` — base64-encoded, NUL-delimited dump of any `npm_*` vars that already existed in the user's shell (e.g., `npm_config_registry` from `.zshrc`). Empty if none existed.

**2. Restore (dev-runner):** `tools/dev-runner/src/lib/cleanEnv.ts` builds a cleaned environment before spawning the daemon:

1. Strip all `npm_*` vars (removes everything npx injected or overwrote)
2. Decode `SCHMUX_PRISTINE_NPM_VARS` and restore any npm vars that existed before npx (preserving user config)
3. Strip `INIT_CWD` and `NODE` (npx-only vars)
4. Restore `PATH` from the snapshot
5. Remove the `SCHMUX_PRISTINE_*` meta-vars themselves

The daemon process starts with a clean environment. All subsystems — tmux sessions, agent detection, workspace git operations, tunnel management — inherit the restored env without any per-subsystem fixes.

The Vite process is **not** cleaned. It runs inside the npm context and needs the npm vars to function.

### When not running via dev.sh

When the daemon runs directly (`./schmux daemon-run`), the `SCHMUX_PRISTINE_*` vars are absent. `cleanEnv()` detects this and passes the environment through unchanged. The fix has zero effect on production usage.
```

- [ ] **Step 2: Also add cleanEnv.ts to the Key files table**

In the `## Key files` table (around line 253), add a row:

```markdown
| `tools/dev-runner/src/lib/cleanEnv.ts` | Strips npx env pollution before daemon spawn (see Environment isolation) |
```

---

### Task 5: Run tests and verify

**Files:** None (verification only)

- [ ] **Step 1: Run the full test suite**

```bash
./test.sh
```

Expected: all tests pass. The changes are in `dev.sh` (bash, not tested by Go/Vitest) and the dev-runner (no test suite). The main risk is a regression in the dashboard frontend tests if the App.tsx import causes issues.

- [ ] **Step 2: Verify the Go binary still builds**

```bash
go build ./cmd/schmux
```

Expected: builds successfully (no Go files changed, but confirm nothing is broken).

- [ ] **Step 3: Verify dev-runner compiles**

```bash
npx --prefix tools/dev-runner tsc --noEmit 2>&1 | head -20
```

Expected: no errors (or no tsconfig — in which case the tsx eval from Task 2 already verified it).
