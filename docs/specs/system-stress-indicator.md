# System Stress Indicator

**Status:** v1 — initial draft.

## Problem

On 2026-04-29 the host kernel-panicked twice (13:50 and 19:12) with userspace
watchdog timeouts on WindowServer. The proximate cause was Finder spinning at
~151 CPU wakes/sec for ~5 minutes, then wedging in an uninterruptible kernel
wait while WindowServer's main loop blocked sending events to it. The
upstream cause was the schmux daemon under load: many concurrent agents
exhausting memory (Jetsam SIGKILL'd helper processes — `git fetch` and
`claude -p` one-shots), tmux liveness probes timing out, and FSEvents from
worktrees flooding Finder.

Crucially, **the daemon already logged the symptoms in real time** before
the panic. `~/.schmux/daemon-startup.log` contained a clear escalation —
`command timeout cmd="display-message -p ok" … queue_depth=1 … 2 … 3 …
4 … 5` — for several minutes before WindowServer hung. The user had no
visibility into this signal until after the crash.

We want a way to see this state coming.

## Goals

- A live time-series graph on the dashboard surfacing the schmux-side
  symptoms that precede a host-level meltdown, modeled on the
  `TmuxDiagnostic` page's process/thread counters.
- Reuse the existing ring-buffer sampler pattern from
  `internal/session/tmux_health.go:1-209` for collection.
- Visual states (calm / warning / critical) so a glance is enough.
- Bookmarkable, real-time-updating, consistent with the other dashboard
  surfaces.

## Non-goals

- **Alerting / notifications.** Visualization only. If the graph isn't on
  screen during a meltdown, it doesn't help — that's a future feature, not
  this one.
- **Persistence across daemon restarts.** Same retention model as
  `TmuxHealthProbe` (in-memory ring buffer, ~1 hour).
- **Cross-platform metrics.** macOS-only host metrics in v1; the
  schmux-internal metrics are platform-agnostic and work everywhere.
- **Auto-mitigation.** No auto-throttling of agent spawns or auto-pausing
  of probes. The user decides what to do when they see red.
- **Persistent kernel-panic / Jetsam history surface.** Out of scope here;
  could be its own page that reads `/Library/Logs/DiagnosticReports/`.

## Vocabulary

- **Stress sample.** A single point-in-time snapshot of all tracked
  metrics, taken on the sampling timer.
- **Stress level.** Per-metric three-state classification —
  `calm` / `warn` / `critical` — derived from thresholds. Each metric has
  its own level; the dashboard tile shows the worst level across metrics
  as the "headline" color.
- **Schmux-internal metric.** A metric derived from data the daemon
  already produces (tmux probe RTT, command queue depth, oneshot kill
  count). No new syscalls.
- **Host metric.** A metric requiring a macOS-specific syscall or external
  command (`sysctl`, `vm_stat`). May be `nil` on non-macOS hosts.

## Design

### Metrics in v1

Picked because (a) the data already exists or is one cheap call away, and
(b) each one moved measurably before today's crashes.

| Metric                     | Source                                                      | warn         | critical     |
| -------------------------- | ----------------------------------------------------------- | ------------ | ------------ |
| `tmux_probe_p95_ms`        | `TmuxHealthProbe` percentile (already collected)            | > 500 ms     | > 2000 ms    |
| `tmux_command_queue_depth` | new counter in `internal/session/manager.go` command path   | ≥ 2          | ≥ 5          |
| `oneshot_sigkill_rate_5m`  | new counter in `internal/nudgenik/` (5-min trailing window) | ≥ 1          | ≥ 5          |
| `mem_pressure_level`       | `sysctl kern.memorystatus_vm_pressure_level` (1/2/4)        | level == 2   | level == 4   |
| `vm_compressor_pages`      | parse `vm_stat` "Pages stored in compressor"                | > 25% of RAM | > 50% of RAM |

The first three are the highest-signal, most actionable, and platform-
neutral; the last two are the direct host-level confirmation that the
schmux symptoms are turning into real OS-level pressure.

Thresholds are starting points and can be tuned in code; they live next to
the metric definitions, not in config.

### Backend collection

New file `internal/health/stress.go` (sibling to existing
`internal/session/tmux_health.go:1-209`, but at the daemon level — not
session-scoped). Mirrors the ring-buffer pattern:

```go
const (
    stressSampleInterval = 5 * time.Second
    stressRingCapacity   = 720 // 1 hour
)

type StressSample struct {
    At                     time.Time
    TmuxProbeP95Ms         float64
    TmuxCommandQueueDepth  int
    OneshotSigkillRate5m   float64
    MemPressureLevel       int    // 0 if unavailable
    VMCompressorPages      uint64 // 0 if unavailable
}

type StressProbe struct {
    samples [stressRingCapacity]StressSample // ring
    head    int
    mu      sync.RWMutex
    // refs to TmuxHealthProbe, session.Manager, nudgenik counter source
}
```

`Sample()` is called every 5s by a goroutine started from
`internal/daemon/daemon.go`. It pulls the current values from existing
sources where possible:

- `tmux_probe_p95_ms` — call into the existing `TmuxHealthProbe` to
  read the most recent computed P95. No new collection.
- `tmux_command_queue_depth` — currently logged inline (e.g.
  `queue_depth=1` in
  `~/.schmux/daemon-startup.log`); add a getter on the session manager's
  command queue and read it here.
- `oneshot_sigkill_rate_5m` — increment a `nudgenik`-owned atomic
  counter on `signal: killed` in the existing error path; expose a
  rate-over-window helper.
- `mem_pressure_level` — `sysctl kern.memorystatus_vm_pressure_level`
  via `golang.org/x/sys/unix.SysctlUint32`. Build-tag for darwin; returns
  0 elsewhere.
- `vm_compressor_pages` — `exec.Command("vm_stat")` and parse the
  "Pages stored in compressor" line. Same darwin-only treatment.

Two new metric sources need a small change at their origin:

1. `internal/session/manager.go` — expose
   `CommandQueueDepth() int` on the manager.
2. `internal/nudgenik/` — track SIGKILL count of agent oneshots in a
   small bucketed counter (5-min window).

### API contract

New typed contract in `internal/api/contracts/`:

```go
// internal/api/contracts/stress.go
package contracts

import "time"

type StressLevel string

const (
    StressLevelCalm     StressLevel = "calm"
    StressLevelWarn     StressLevel = "warn"
    StressLevelCritical StressLevel = "critical"
)

type StressSample struct {
    At                    time.Time `json:"at"`
    TmuxProbeP95Ms        float64   `json:"tmux_probe_p95_ms"`
    TmuxCommandQueueDepth int       `json:"tmux_command_queue_depth"`
    OneshotSigkillRate5m  float64   `json:"oneshot_sigkill_rate_5m"`
    MemPressureLevel      int       `json:"mem_pressure_level,omitempty"`
    VMCompressorPages     uint64    `json:"vm_compressor_pages,omitempty"`
}

type StressSeriesResponse struct {
    SampleIntervalMs int                     `json:"sample_interval_ms"`
    Samples          []StressSample          `json:"samples"` // newest last
    LevelByMetric    map[string]StressLevel  `json:"level_by_metric"`
    Headline         StressLevel             `json:"headline"`
}
```

After the contract is in place, run `go run ./cmd/gen-types` to regenerate
`assets/dashboard/src/lib/types.generated.ts` (per CLAUDE.md: never edit
the generated file by hand).

New handler in `internal/dashboard/`:

- `GET /api/health/stress` — returns the full ring buffer plus current
  per-metric and headline levels. Polled by the frontend; no WebSocket
  push in v1 (matches the simpler shape of `/api/debug/tmux-leak`,
  `internal/dashboard/handlers_debug_tmux.go:14-76`).

Polling cadence: 1s on the frontend (matches `TmuxDiagnostic.tsx:73`).
Each response is small (5 numbers × 720 samples ≈ 30 KB
gzip-compressible), and 1s polling is well under the sample interval —
the same sample is just re-rendered until the next 5s tick produces a
new one.

### Frontend

A new sibling component to `TmuxDiagnostic`:
`assets/dashboard/src/components/SystemStressGraph.tsx`. Same custom-SVG
sparkline approach as `TmuxDiagnostic.tsx:253-338` — no new chart
library — with one row per metric:

```
[ tmux probe p95         ]  ▁▁▁▂▂▃▅▇█  912 ms     [warn]
[ tmux command queue     ]  ▁▁▁▁▂▃▄▅▇  4         [warn]
[ oneshot SIGKILL / 5m   ]  ▁▁▁▁▁▁▂▃▆  3         [warn]
[ memory pressure        ]  ▁▁▁▁▁▁▁▁▂  warn       [warn]
[ vm compressor pages    ]  ▁▁▁▂▃▄▅▆▇  31% of RAM [warn]

Headline: WARN — system pressure climbing.
```

The headline color is the worst per-metric level. Color tokens follow the
existing status-state pattern (`calm` ≈ status-running, `warn` ≈
status-pending, `critical` ≈ status-error) for visual consistency with
the rest of the dashboard.

Placement: a new tile on the home route (`/`) sized like the existing
top-of-home cards, plus a full-page detail view at `/system` that shows
each metric in its own larger panel. The home tile shows just the
headline + the worst-offender metric's sparkline; clicking navigates to
`/system`.

State source: `useEffect` polls `/api/health/stress` every 1s; no
context plumbing needed in v1 (the data isn't shared with other
components). Existing `WebSocket → SessionsContext` traffic is unchanged.

### Tests

- Backend: a `TestStressProbe_Levels` table-driven test verifies threshold
  classification per metric, and a `TestStressProbe_Ring` test verifies
  retention/eviction behavior, modeled on existing tmux health tests.
- Frontend: a `SystemStressGraph.test.tsx` Vitest spec verifies that the
  headline renders the worst per-metric level, and that the sparkline
  uses the latest `samples[]` window.
- Both run under `./test.sh --quick` (per CLAUDE.md: vitest is included
  there; never run vitest directly).

## Open questions

- **Threshold tuning.** The numbers in the table are derived from today's
  crash logs, not from a measured baseline. We should leave the values in
  code and revisit after a week of real data.
- **Headline placement.** Home tile vs. global header bar (always
  visible). Global header would catch problems even when on `/sessions/{id}`,
  at the cost of more chrome. Defer to v2.
- **`oneshot_sigkill_rate_5m` source.** Today's signal lives in the
  daemon stdout log (`signal: killed`). We can either grep the log
  (cheap, fragile) or add a structured counter in `nudgenik` (clean, one
  small change). The spec assumes the latter; flag if the touch surface
  is bigger than expected.
- **Should the spec also cover surfacing kernel panic / Jetsam history?**
  Today this is a separate forensics concern (read
  `/Library/Logs/DiagnosticReports/`). Probably its own spec —
  `system-incident-history.md` — paired with this one.
