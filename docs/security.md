# Security

## What it does

Three structural correctness fixes plus one deliberate non-fix that together establish schmux's security posture for distribution inside vendor environments (and benefit every user):

- **Shell commands are argv arrays, not template strings.** Anywhere schmux executes a configured command, each `{{...}}` template slot becomes one argv element. Shell metacharacters in user-controlled values cannot break out of their slot. (`internal/cmdtemplate`)
- **Local files under `${schmuxdir}` are mode `0600`, directories `0700`.** Enforced on every daemon startup before any listener opens. (`internal/daemon/modes.go`)
- **Two HTTP handlers that previously joined URL params into filesystem paths now validate first.** Timelapse recordings and autolearn curation logs both reject path-traversal characters at the perimeter. (`internal/dashboard/validation.go`)
- **The local HTTP API has no authentication, by design.** A bearer-token mechanism was designed and rejected after analysis. Documented in "Architecture decisions" below.

## Key files

| File                                           | Purpose                                                                                                                   |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `internal/cmdtemplate/cmdtemplate.go`          | `Template`, `Render` — sole renderer for shell-executed configured commands. Enforces literal-script escape hatch.        |
| `internal/dashboard/validation.go`             | `isValidResourceID`, `isValidRepoName`, `isPathWithinDir`. HTTP-perimeter validators only.                                |
| `internal/daemon/modes.go`                     | `MigrateModes` — walks `${schmuxdir}` on startup, tightens permissions, refuses to start on chmod failure (default).      |
| `internal/config/config.go` (`ShellCommand`)   | `[]string` type with `UnmarshalJSON` that rejects legacy string-form values pointing the user at `schmux config migrate`. |
| `internal/config/config.go` (`SecurityConfig`) | `security.allow_insecure_modes` field — escape valve for filesystems without unix permission semantics.                   |
| `cmd/schmux/cmd_config_migrate.go`             | `schmux config migrate` CLI — converts legacy string-form configs to argv arrays. Reads JSON directly, no daemon.         |
| `pkg/shellutil/{quote,split}.go`               | Shell tokenizer + quoting helpers used by the migrate CLI and remote-VCS argv-to-shell conversion.                        |

Use sites of the argv-array schema:

| File                                  | What it executes                                                                  |
| ------------------------------------- | --------------------------------------------------------------------------------- |
| `internal/workspace/vcs_sapling.go`   | `sapling_commands.*` — local sapling lifecycle (clone, remove, check)             |
| `internal/remote/workspace_vcs.go`    | `remote_profiles[].remote_vcs_commands.*` — same, but executed via SSH            |
| `internal/telemetry/command.go`       | `telemetry.command` — telemetry sink (e.g., `<vendor-logger> <vendor_log_table>`) |
| `internal/dashboard/handlers_diff.go` | `external_diff_commands[*].command` — external diff tool launch                   |

## Architecture decisions

### No authentication on the local API

The dashboard server binds `127.0.0.1:44102` (or whatever port is configured) with no per-request authentication. A bearer-token mechanism was designed in detail (token file at `${schmuxdir}/auth-token` mode 0600, CLI bootstrap, SPA cookie handshake gated by `Sec-Fetch-Site`) and then deliberately removed from scope.

The reasoning, in short:

- **Vendor single-tenant hosts**: dev instances are provisioned per engineer; no other engineers can SSH in. The vendor-managed HTTPS gateway that fronts the dashboard for laptop browsers handles SSO + MFA before forwarding to the dev host's loopback. Browser cross-origin / DNS-rebinding attacks are the gateway team's surface, not schmux's.
- **Same-UID processes can't be defended**: A bearer token in a 0600 file is readable by any process running as the engineer's UID. Routing damage through schmux's API adds nothing the same-UID process couldn't do directly (it can already `claude --dangerously-skip-permissions`, `tmux capture-pane`, `git push` with the engineer's credentials).
- **The argv-array schema (§ next decision) is essentially the same code change four times**, with one of the four being a genuine RCE-from-untrusted-input fix. The bearer-token mechanism's cost (CLI plumbing, SPA bootstrap, gateway header verification, pervasive test churn) was substantially larger for similar marginal value.

If a future feature adds a non-loopback listener, or if the existing `corsMiddleware` / `Origin` checks regress, this decision should be revisited.

### Argv-array schema, not validated string templates

The bug class addressed: rendering a `text/template` string and passing it to `sh -c`. Anywhere a template variable is influenced by user input, the variable can break out of its argv position via shell metacharacters. The audit found four families of this bug; the structural fix uniformly converts every site.

Three of the four families (sapling commands, remote VCS commands, telemetry command) only matter if a same-UID writer can change config — and a same-UID writer can already exec arbitrary commands directly. They are bug-class hygiene, included because the marginal cost is small and they remove a surface a future code path could re-open through a flow that doesn't exist today. The fourth family (the external diff tool) is a genuine RCE-from-untrusted-input bug: filenames in git repos can contain `;`, `|`, backticks, and newlines, and the diff tool was substituting those into a `sh -c` command string — a malicious commit author with no access to the engineer's machine could trigger RCE the moment the engineer opened the diff.

The argv-array schema makes the bug class structurally impossible: each `{{...}}` slot becomes exactly one argv element regardless of value contents.

### Literal-script escape hatch for shell-required commands

A few configured commands genuinely need shell features (pipes, redirection, subshells) — for example, an internal `check_repo_base` may pipe `<vcs-list-tool> | jq ...` to find an existing checkout. The renderer permits this via:

```json
"check_repo_base": [
  "sh", "-c",
  "<literal script using positional args $1, $2 — no template syntax in this slot>",
  "_",
  "{{.RepoIdentifier}}",
  "{{.WorktreeBase}}"
]
```

When `basename(argv[0]) ∈ {sh, bash, dash, zsh, ksh}` and `argv[1] == "-c"`, the script slot (argv[2]) is enforced literal — `cmdtemplate.Render` rejects template syntax (`{{`) in it. Positional-arg slots after the script become `$1`, `$2`, … inside the script, where they can be referenced as `"$1"` (quoted) without the shell parsing the value. The literal-only enforcement on the script keeps user-controlled data out of shell-parsed text.

### Hard-fail on legacy string-form config

Loading `~/.schmux/config.json` with a legacy string-form `sapling_commands.create_workspace = "vcs-clone X Y"` causes the daemon to refuse to start, with an error message that explicitly directs the user to `schmux config migrate`. No soft-deprecation period, no silent auto-migration. Silent migration is exactly the bug-class-preserving pattern that the argv-array schema is meant to eliminate.

This is acceptable because schmux had no installed user base when the change shipped. The migrate CLI works against a config that the daemon refuses to load (it parses raw JSON via `os.ReadFile`).

### Settings UI hides shell-command fields

The pre-existing Settings UI rendered each command field (`sapling_commands.create_workspace`, etc.) as a single `<input type="text">`. After the schema flip, these fields are arrays — a proper editor would need chip-style or per-row inputs. Rather than ship that React work in this release, the Settings UI hides the shell-command fields entirely. Users who need to change them edit `~/.schmux/config.json` directly, with an "Advanced — edit config.json" hint pointing at `docs/api.md`. Both `AdvancedTab.tsx` and `RemoteSettingsPage.tsx` lost their shell-command input rows.

### `security.allow_insecure_modes` opt-out for chmod migration

On every daemon start, `MigrateModes` walks `${schmuxdir}` and tightens modes to 0600/0700 _before_ any listener opens. If a `chmod` call fails (NFS without unix permission semantics, immutable bits, SELinux/AppArmor refusal, EPERM on overlayfs, EROFS on SMB, `~/.schmux` symlinked to a network share), the daemon refuses to start.

The opt-out exists because some legitimate environments (WSL2 mounting Windows-formatted drives, certain Docker overlay configurations, SMB-mounted home directories) cannot satisfy the chmod call but are still safe enough to use schmux on. With `security.allow_insecure_modes: true`, the daemon logs a loud warning at every startup and proceeds. The warning is repeated — never silenced — so it stays visible.

The walk tightens the `${schmuxdir}/repos/` directory entry itself but does not descend into it. That subtree holds bare clones and Sapling/EdenFS working copies, including virtual-mount monorepos whose backing store contains millions of files. Recursing would force materialization of every backing file and rewrite permissions on upstream code that schmux does not own. Files keep their owner exec bit (`0600 | (existing & 0100)`) so generated hook scripts under `${schmuxdir}/hooks/` stay runnable; group/other bits are always stripped.

## Gotchas

- **Sapling/remote-VCS/telemetry/diff commands MUST be JSON arrays.** A single string in `~/.schmux/config.json` for any of these fields makes the daemon refuse to start. The error message points at `schmux config migrate`. Old config snippets in blog posts, AGENTS.md examples, etc. need updating.
- **The literal-script escape hatch only applies when `argv[0]` is a recognized shell.** `["python", "-c", "import os; ..."]` is _not_ covered — Python's `-c` looks similar but Python isn't in the shell allowlist (`sh`, `bash`, `dash`, `zsh`, `ksh`). Adding new shell-like binaries requires editing `cmdtemplate.shellBinaries`.
- **`isValidRepoName` allows dots; `isValidResourceID` rejects them.** Repo names legitimately contain `.` (e.g., `owner.repo`); opaque IDs (recordingID, curationID, sessionID) shouldn't. Picking the wrong validator breaks things in either direction.
- **`isValidRepoName` is HTTP-route-only, not a config-schema constraint.** The `Repo.Name` field in `~/.schmux/config.json` accepts arbitrary strings. A repo named `vendor:product` works for everything except autolearn endpoints (which return 400 with a clear error).
- **Filenames in git CAN contain shell metacharacters.** Never substitute filenames into command strings. The diff tool subsystem passes `LOCAL` / `REMOTE` / `MERGED` paths via env vars instead. New code that handles file paths from git output should follow this pattern.
- **The chmod migration runs before listeners open.** Anything that depends on the daemon being reachable (health checks, supervisor probes) sees a longer startup window when there's lots to chmod. Symlinks under `${schmuxdir}` are detected via `Lstat` and skipped — never chased — so an attacker who plants a symlink can't redirect a chmod onto a system file.
- **Same-UID processes have full access to schmux.** The threat model accepts this. If you're tempted to add a defense ("X needs to be locked down"), check whether the attacker would already have `os.exec` as the engineer's UID. If yes, the defense is theatre — don't add it.
- **The auth-token file referenced in some old branches doesn't exist.** A `${schmuxdir}/auth-token` was designed and then dropped. If you find a reference to it, it's stale.

## Common modification patterns

### Adding a new shell-executed configured command

1. Add the field to the relevant config struct (`internal/config/config.go`) with type `ShellCommand` (which is `[]string`).
2. At the call site, render with `cmdtemplate.Template(cfg.YourField).Render(vars)` where `vars` is a `map[string]string` of substitutions.
3. Pass the rendered argv to `exec.CommandContext(ctx, argv[0], argv[1:]...)`. Never `sh -c`.
4. If the command genuinely needs shell features, use the `["sh", "-c", "<literal script>", "_", "{{.Var1}}", ...]` form — variables become `$1`, `$2`, … inside the script, accessed as `"$1"` (quoted).
5. Document the new field in `docs/api.md`.

### Adding a new HTTP handler that takes a path-like URL parameter

1. Decide which validator: `isValidResourceID` for opaque IDs (`recordingID`, `sessionID`, `curationID`); `isValidRepoName` for repo identifiers that may contain dots.
2. Validate at the top of the handler (before any `filepath.Join` call). Return 400 with a clear error.
3. Add a regression test in the same package verifying that `../`, URL-encoded `..`, and NUL-byte injection are all rejected. Note: `chi` does NOT URL-decode path params — encoded `..%2F` reaches the handler verbatim and gets rejected because of the `%` and `.` characters in the deny list.

### Adding a new file or directory under `${schmuxdir}`

1. Write files with mode `0600`, directories with `0700`. The `MigrateModes` walker will fix legacy modes on next startup, but new code should land them right the first time.
2. If you `os.OpenFile`, pass `0600` explicitly. If you `os.WriteFile`, the mode applies only on creation. If you `os.Create`, the file is mode `0666 & ~umask` — usually `0644`. Use `os.OpenFile` with explicit `0600` instead.
3. Symlinks are skipped by the chmod walker. Don't rely on the walker to fix mode on a symlink target.

### Handling user-supplied file paths from external sources

The pattern: pass file paths via environment variables, not via command-line arguments. The diff tool subsystem (`internal/dashboard/handlers_diff.go`) is the model — `LOCAL`, `REMOTE`, `MERGED` env vars carry the paths; the configured command name has no `{old_file}`/`{new_file}` substitutions in the command line. This means a malicious filename can't break out into a separate shell command.

For tools that absolutely require paths on argv (some pre-configured `kdiff3` invocations, etc.), use the argv-array form with `{{.OldFile}}` / `{{.NewFile}}` template slots — each slot is one argv element regardless of contents.

### Re-running the chmod migration manually

There's no CLI for this. The migration runs automatically on every daemon start. If you've manually modified file modes under `${schmuxdir}` and want them tightened immediately, restart the daemon (`schmux stop && schmux start`).

## Risks acknowledged but not addressed in this subsystem

The security audit identified additional risks that are explicitly out of scope. They are NOT silently ignored — listed here so future contributors don't think they were missed:

- **AI agents run with `--dangerously-skip-permissions` and full host access**: no chroot, bwrap, namespaces, AppArmor, seccomp, firewall, or proxy enforcement. A prompt-injected agent can `curl exfil` or `git push` to a personal repo.
- **Persona system prompts are user-editable and free-form**: `POST /api/personas` accepts arbitrary markdown, which becomes the system prompt. A malicious persona effectively bypasses every model safety.
- **Telemetry sends user-controlled branch and intent strings to broad-access internal data sinks**: Privacy / data-classification review territory, not security.
- **Pre-existing `.claude/settings.local.json` hooks survive merge and execute on `SessionStart`**: a malicious workspace can plant code execution that fires on every schmux attach.
- **Same-UID malicious processes are not defended** (architectural reality, not a missed mitigation).
- **Supply-chain hardening of the upstream GitHub repo**: build-pipeline policy decision (pin by SHA, fork to a vendor-controlled org with CODEOWNERS), not a code change in schmux.

Each of these is its own future spec.
