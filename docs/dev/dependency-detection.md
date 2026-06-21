# Dependency detection

schmux's usefulness scales with what's installed: AI agents, a VCS, `tmux`, the
`fence` sandbox, and editor integrations. This document describes the dependency
registry that knows about all of them in one place.

## Architecture

The registry lives in `internal/detect`:

- `dependency.go` — the model (`Dependency`, `DependencyStatus`, `DependencyReport`,
  `InstallMethod`, `DepGroup`), the ordered `Groups` list, and the install helpers
  (`InstallForOS`, `deriveInstall`).
- `dependency_registry.go` — the native dependency entries, agent-dependency
  registration, and `DetectDependencies`.

There are two kinds of dependency, with different sources of truth:

- **Agents** source _availability_ from `models.Manager` — its detected-tools list is
  frozen at construction and drives model enablement, spawn resolution, and
  antigravity discovery. The report reads `GetDetectedTools()` and **never
  re-detects agents**, so the report can't diverge from what the rest of schmux
  believes. Agent _metadata_ (description, docs URL, unlocks, install methods)
  comes from the descriptor YAML (`internal/detect/descriptors/*.yaml`).
- **Native tools** (git, sapling, tmux, fence, vscode, iterm2) each wrap an existing
  detector function (`DetectVCS`, `DetectTmux`, `ResolveVSCodePath`,
  `ITerm2Available`, or `commandExists` for fence). Those functions stay, so
  current callers are unaffected.

`DetectDependencies(ctx, agents)` folds the manager's agent list into the native
detection (run concurrently) and returns one `DependencyStatus` per dependency,
grouped and ordered by `Groups`.

## Functional groups

`detect.Groups` is an ordered, organizational list:

| ID         | Display name        |
| ---------- | ------------------- |
| `agents`   | AI agents           |
| `vcs`      | Version control     |
| `terminal` | Terminal            |
| `sandbox`  | Sandbox             |
| `editors`  | Editor integrations |

Groups are purely organizational — no severity ranking, no advisories, no
enforcement. The report lists what's present and what's missing per group; install
guidance is the call to action. `iterm2` sets `Platforms: ["macos"]` and is omitted
from the report on Linux (not shown as "missing").

## Detection mechanisms

Agent descriptors (`Descriptor.Detect`) use these entry types:

| Type               | Meaning                                  |
| ------------------ | ---------------------------------------- |
| `path_lookup`      | Binary on PATH (`command`)               |
| `file_exists`      | File present (`path`, optional `verify`) |
| `homebrew_cask`    | Homebrew cask installed (`name`)         |
| `homebrew_formula` | Homebrew formula installed (`name`)      |
| `npm_global`       | npm global package installed (`package`) |

Native tools use the existing detector functions directly (see above).

## Install methods

Install methods are either **derived** or **explicit**:

- **Derived** — `deriveInstall` builds them from a descriptor's detect entries:
  `homebrew_cask` → `brew install --cask <name>`, `homebrew_formula` →
  `brew install <name>`, `npm_global` → `npm i -g <package>`. Detection and install
  stay in sync: a new agent descriptor yields install guidance for free.
- **Explicit** — a descriptor's `install:` block (e.g. antigravity's curl script), or
  the `Install` field on a native `Dependency` entry (e.g. `brew install git`).

Package managers (`homebrew`, `npm`) are detected but **not** reported as
dependencies — their presence only tailors and orders install methods for the real
entries (`InstallForOS` puts methods whose `Requires` manager is present — or empty
— first).

## How to add a dependency

- **An agent** — add or edit a descriptor `.yaml` in `internal/detect/descriptors/`
  with the metadata fields (`description`, `docs_url`, `unlocks`, optional
  `install`). It's registered automatically by `RegisterDescriptorAdapters`; install
  methods are derived from its detect entries.
- **A native tool** — add a `Dependency` entry to `nativeDeps` in
  `dependency_registry.go` in the right group, with a `detect` func (usually
  wrapping an existing detector or `commandExists`), `Unlocks`, and explicit
  `Install` methods.

## The API

`GET /api/dependencies` returns the cached report (computed at daemon startup, so
always populated). `?refresh=1` re-runs the **native** detectors only — agents still
come from the manager — behind a single-flight guard, then returns the fresh report.
The handler filters `Install` to the request's OS and orders it by package-manager
readiness. See `docs/api.md` for the wire shape.

Because agents are sourced from the manager's frozen list, a _newly installed_ agent
needs a daemon restart to appear in the report (same as today).
