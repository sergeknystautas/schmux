# Onboarding & First-Time User Experience

## What it does

Handles the zero-to-first-session experience. When a new user runs `schmux start`, smart defaults, auto-detection, and a lazy-config flow get them to a running AI agent session in under 2 minutes — no wizard, no restart, no upfront configuration.

## Key files

| File                                                     | Purpose                                                        |
| -------------------------------------------------------- | -------------------------------------------------------------- |
| `internal/detect/vcs.go`                                 | VCS (git, sapling) and tmux detection at startup               |
| `internal/detect/agents.go`                              | AI agent detection (claude, codex, gemini) — pre-existing      |
| `internal/config/config.go` (`CreateDefault`)            | Smart defaults: `workspace_path` set to `~/.schmux/workspaces` |
| `internal/workspace/access.go`                           | Repo access probe via `git ls-remote --symref HEAD`            |
| `internal/dashboard/handlers_detection.go`               | `GET /api/detection-summary` endpoint                          |
| `internal/dashboard/handlers_repos.go`                   | `GET /api/repos/scan` + `POST /api/repos/probe` endpoints      |
| `internal/api/contracts/detection.go`                    | `DetectionSummaryResponse` contract type                       |
| `assets/dashboard/src/components/EnvironmentSummary.tsx` | Shows detected tools on the home page                          |
| `assets/dashboard/src/components/AddWorkspaceModal.tsx`  | Smart repo input with local scan + access validation           |
| `assets/dashboard/src/routes/HomePage.tsx`               | FTUE rendering when zero workspaces                            |
| `assets/dashboard/src/routes/SpawnPage.tsx`              | Tmux-missing error + auto-detected agent labels                |

## Architecture decisions

- **No wizard, no gates.** The old setup wizard (`isFirstRun`, `isNotConfigured`, `useRequireConfig()`) was removed entirely. `/config` is always just a settings page. The rationale: wizards feel constraining, and users hate being forced through mandatory sequences before using a product.

- **Smart defaults instead of empty config.** `CreateDefault()` sets `workspace_path` to `~/.schmux/workspaces` (derived from `filepath.Dir(configPath)`). Previously it was empty, which triggered the wizard redirect. The path is derived from `configPath` (not `schmuxdir.Get()`) to keep the function self-contained.

- **Agent detection feeds the model catalog, not `run_targets`.** Detected agents flow through the existing `detect.DetectAvailableToolsContext` → `models.New()` → model catalog pipeline. They are NOT written into `run_targets` (which remains user-defined custom commands). This avoids duplicates and stale entries if an agent is later uninstalled.

- **Tmux check moved from startup to spawn time.** The hard gate in `ValidateReadyToRun()` was removed. The daemon now starts and serves the dashboard without tmux. The check happens in `session.Manager.Spawn()` — right after `ResolveTarget`, before workspace resolution — so no work is wasted. The nil guard on `m.server` prevents panics in remote-only setups.

- **Lazy config: ask when intent is clear.** Repo info is only requested when the user clicks "+ Add Workspace", not upfront. This is the only piece of configuration a new user must provide.

- **Read-modify-write for config updates.** The config update API replaces the full `Repos` slice, not appends. The frontend must GET current config, append the new repo, then POST the full array. Sending only the new repo would delete all existing repos.

- **`default_branch` is not stored in config.** The `contracts.Repo` update type has no `DefaultBranch` field. The `default_branch` on `RepoWithConfig` (GET response) is populated dynamically at read time. After the probe detects the default branch, the frontend passes it directly to the spawn page via `location.state.branch`.

## Gotchas

- **`config.Repos` uses `omitempty`** — a nil slice omits the field (meaning "don't change repos"), but an empty slice `[]` is included (meaning "delete all repos"). The AddWorkspaceModal must always include the full existing repos array when updating.

- **The local repo scan has hard-coded server-side roots** — it scans from `$HOME` with depth 2. The API endpoint accepts NO client-supplied path parameters. This is a security constraint to prevent directory traversal on network-exposed dashboards.

- **Symlinks are only followed at depth 1** — immediate children of `$HOME` (e.g., `~/my-monorepo` → mount point) are followed, but depth 2 symlinks are not, to avoid cycles.

- **The detection summary endpoint has a startup race** — the HTTP server may start before `DetectAvailableToolsContext` completes (up to 10 seconds). The `EnvironmentSummary` component handles `status: "pending"` by retrying every second, with a max of 10 retries before showing a timeout message.

- **`useRequireConfig()` was deleted** — it previously redirected to `/config` from HomePage, SpawnPage, and TipsPage. If you see old code or docs referencing it, it no longer exists. `isNotConfigured` and `isFirstRun` were also removed from `ConfigContextValue`.

## Common modification patterns

- **To add a new detectable tool** (agent, VCS, or other): Add an entry to the candidates list in `internal/detect/vcs.go` (for VCS/tmux) or register a new detector in `internal/detect/agents.go` (for AI agents). The detection summary endpoint picks up new entries automatically.

- **To change what the FTUE home page shows**: Edit the `workspaces.length === 0` branch in `HomePage.tsx`. The `EnvironmentSummary` and `AddWorkspaceModal` components are independent — modify or replace them without touching the home page structure.

- **To add a new repo scan directory or change skip rules**: Edit `scanLocalRepos()` in `handlers_repos.go`. The skip list and depth limit are constants at the top of the function.

- **To change access probe behavior**: Edit `ProbeRepoAccess()` in `internal/workspace/access.go`. Error classification (SSH, auth, network, timeout) is in `classifyGitError()` / `classifyGitErrorType()` in the same file.

- **To add a new requirement to the FTUE flow** (e.g., validating write access): Add it to the probe result or the AddWorkspaceModal flow. Do not add config gates — the design principle is "no gates, no wizards."
