# Experimental Features: Build-Tag Exclusion Guide

Experimental features can be compiled out of the binary using Go build tags. When excluded, the feature disappears from both the backend (handlers return 503) and the dashboard UI (the Experimental tab hides the card).

## How It Works

Each excludable feature follows a three-layer pattern:

```
internal/<feature>/
  *.go              — //go:build !no<feature>    (normal source)
  disabled.go       — //go:build no<feature>     (no-op stubs)

internal/dashboard/
  handlers_<feature>.go          — //go:build !no<feature>
  handlers_<feature>_disabled.go — //go:build no<feature>

cmd/schmux/
  <feature>.go          — //go:build !no<feature>    (if CLI command exists)
  <feature>_disabled.go — //go:build no<feature>
```

**Layer 1: Package stubs** (`internal/<feature>/disabled.go`) — mirrors all exported types as empty structs, all constructors as zero-value returns, all methods as no-ops. Includes `IsAvailable() bool { return false }`.

**Layer 2: Dashboard handler stubs** (`handlers_<feature>_disabled.go`) — all handler methods return `http.StatusServiceUnavailable`. Any `Set*` wiring methods become no-ops.

**Layer 3: CLI command stubs** (`cmd/schmux/<feature>_disabled.go`) — `Run()` returns `fmt.Errorf("<feature> is not available in this build")`.

## The Features Chain

The backend reports which features are compiled in via `GET /api/features`:

```
contracts/features.go  →  Features struct (bool per feature)
handlers_features.go   →  calls <package>.IsAvailable() for each
gen-types              →  generates TypeScript Features type
experimentalRegistry.ts → buildFeatureKey maps to Features field
ExperimentalTab.tsx    →  hides cards where buildFeatureKey is false
```

## Current Exclusion Tags

| Feature        | Build Tag         | Package                    | Has CLI? |
| -------------- | ----------------- | -------------------------- | -------- |
| Telemetry      | `noposthog`       | `internal/telemetry/`      | No       |
| Update         | `noupdate`        | `internal/update/`         | No       |
| Model Registry | `nomodelregistry` | `internal/models/registry` | No       |
| DashboardSX    | `nodashboardsx`   | `internal/dashboardsx/`    | Yes      |
| Repofeed       | `norepofeed`      | `internal/repofeed/`       | Yes      |
| Subreddit      | `nosubreddit`     | `internal/subreddit/`      | No       |
| Tunnel         | `notunnel`        | `internal/tunnel/`         | No       |
| GitHub         | `nogithub`        | `internal/github/`         | No       |
| Timelapse      | `notimelapse`     | `internal/timelapse/`      | Yes      |
| Floor Manager  | `nofloormanager`  | `internal/floormanager/`   | No       |
| Autolearn      | `noautolearn`     | `internal/autolearn/`      | No       |
| Personas       | `nopersonas`      | `internal/personas/`       | No       |
| Comm Styles    | `nocommstyles`    | `internal/commstyles/`     | No       |

## Building Without a Feature

```bash
# Exclude one feature
go build -tags noautolearn ./cmd/schmux

# Exclude multiple features
go build -tags "noautolearn nofloormanager notimelapse" ./cmd/schmux

# Exclude everything optional
go build -tags "noposthog noupdate nomodelregistry nodashboardsx norepofeed nosubreddit notunnel nogithub notimelapse nofloormanager noautolearn nopersonas nocommstyles" ./cmd/schmux
```

## Checklist: Adding a New Experimental Feature

1. Create the package under `internal/<feature>/`
2. Add `//go:build !no<feature>` to every source `.go` file (not test files)
3. Add `func IsAvailable() bool { return true }` to one source file
4. Create `internal/<feature>/disabled.go` with `//go:build no<feature>`:
   - Mirror every exported type as an empty struct
   - Mirror every constructor to return zero values
   - Mirror every method as a no-op
   - Add `func IsAvailable() bool { return false }`
5. If there are dashboard handlers:
   - Add `//go:build !no<feature>` to the handler file
   - Create `handlers_<feature>_disabled.go` returning 503
   - **Check for cross-references**: search for any methods defined in the handler file that are called from OTHER untagged handler files (e.g., `handlers_config.go`, `handlers_spawn_entries.go`). Those methods MUST be stubbed in the disabled file.
6. If there's a CLI command in `cmd/schmux/`:
   - Add `//go:build !no<feature>` to the command file
   - Create `<feature>_disabled.go` returning an error from `Run()`
7. Add a field to `contracts.Features` struct
8. Add `<package>.IsAvailable()` call to `handlers_features.go`
9. Run `go run ./cmd/gen-types` to regenerate TypeScript types
10. Add `buildFeatureKey` to the feature's entry in `experimentalRegistry.ts`
11. Add rows to the exclusion tag table in `.claude/commands/commit.md`
12. Verify: `go build -tags no<feature> ./cmd/schmux`

## Checklist: Adding a Build Tag to an Existing Feature

If a feature already has a backend package but no build-tag exclusion:

1. Follow steps 2-12 from the checklist above
2. Key gotcha: identify ALL external callers of functions defined in your source files. The disabled stubs must provide everything that untagged code references.
3. **server.go fields**: If `server.go` has fields of your package's types, the disabled stub's types must exist (empty structs are fine). `Set*` methods in `server.go` compile unconditionally — they don't need stubs.
4. **daemon.go wiring**: If `daemon.go` calls your constructors unconditionally, the disabled stub constructors must return valid zero values. If calls are inside `cfg.GetXxxEnabled()`, they still need stubs (the code compiles even if the branch is never taken at runtime).

## Common Pitfalls

- **Forgetting cross-file method calls**: A handler method defined in `handlers_autolearn.go` might be called from `handlers_config.go` or `handlers_spawn_entries.go`. If those callers have no build tag, you must stub the method.
- **Test files**: Do NOT add build tags to test files. Tests compile against whichever version (enabled or disabled) is active. Consider adding `//go:build !no<feature>` to test files only if they would produce confusing failures with the disabled stubs.
- **`init()` functions**: If a source file has `init()` that registers something (e.g., schema registration), the disabled stub won't run it. Make sure nothing crashes when the registration is absent.
- **Interface compliance**: If a type implements an interface (e.g., `events.EventHandler`), the disabled stub must satisfy it too. Add `var _ events.EventHandler = (*MyType)(nil)` to verify at compile time.
