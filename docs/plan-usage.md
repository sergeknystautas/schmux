# Plan Usage

## What it does

Tracks the latest plan quota snapshots reported by Claude and Codex chat harnesses and surfaces them in a sidebar panel that compares reported utilization to the elapsed share of each window.

## Key files

| File                                                      | Purpose                                                                                     |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `internal/usage/manager.go`                               | In-memory + on-disk snapshot store keyed by provider; atomic write, fail-soft on errors     |
| `internal/usage/parse.go`                                 | Claude (`rate_limit_event`) and Codex (`account/rateLimits/updated`) line parsers           |
| `internal/usage/manager_test.go`, `parse_test.go`         | Snapshot persistence and parser tests, using captured harness fixtures                      |
| `internal/api/contracts/usage.go`                         | `UsageSnapshotResponse`, `UsageProviderInfo`, `UsageWindow`, `UsageCredits`                 |
| `internal/dashboard/handlers_usage.go`                    | `GET /api/usage` handler (read-only)                                                        |
| `internal/dashboard/handlers_usage_test.go`               | Routing, attribution, and ordering tests for the endpoint                                   |
| `internal/dashboard/server.go`                            | Wires `usageManager` into the server, registers the callback, owns `usageProviderForTarget` |
| `internal/chat/runtime.go`                                | `SetUsageCallback` — every parsed harness line is forwarded to the registered sink          |
| `internal/session/manager.go`                             | `SetChatUsageCallback` — rewires both new and restored runtimes so usage never has a gap    |
| `internal/session/chat_usage_test.go`                     | Verifies the restored-runtime rewiring                                                      |
| `internal/chat/usage_callback_test.go`                    | Runtime → callback delivery                                                                 |
| `internal/chat/usage_telemetry_test.go`                   | Quota updates must not disturb the headless nudge tracker                                   |
| `assets/dashboard/src/components/PlanUsagePanel.tsx`      | Sidebar panel: provider label, reserve/deficit, time left                                   |
| `assets/dashboard/src/components/PlanUsagePanel.test.tsx` | Fake-timer-driven display tests                                                             |
| `assets/dashboard/src/lib/sidebarPanels.ts`               | Panel id registry; `resolveSidebarPanels` merges config over the all-false defaults         |

## Architecture decisions

- **Provider attribution by session target.** Each chat session's `Target` is resolved against the model catalog (`Server.usageProviderForTarget`). A bare `claude` target is `anthropic`; a bare `codex` target is `openai`. A third-party model like `glm-5.3` running over the Codex protocol stays attributed to `zai`. Unresolvable targets are logged and dropped. No provider field is added to the session model.
- **Replaces snapshots, no history.** Each `Observe(provider, info)` call overwrites the stored snapshot. There is no token accounting, daily bucketing, rolling window, or migration path. Old token-accounting payloads are explicitly rejected by `DisallowUnknownFields`; the next live report writes the current schema.
- **Collection is independent of visibility.** Quota collection runs on every chat harness record, even when the sidebar panel is hidden. `ui.panels.planUsage` controls only `/api/usage` (404 when off) and the sidebar rendering.
- **Live capture, not replay.** The manager never scans chat files or workspace directories. It only receives records forwarded by `SetChatUsageCallback`. Persistence is best-effort: a failed `WriteFile` logs and restores the previous snapshot.
- **Per-feature sidebar panel toggles.** The old global `debug_ui` is gone. Each panel has its own `ui.panels.<id>` bool. Every panel starts disabled; the frontend registry (`sidebarPanels.ts`) lists every known id so unknown config keys are silently dropped.
- **Reserve/deficit, not prediction.** The panel reports `elapsed-window % - used %` rounded to whole points, labeled `reserve` (positive), `deficit` (negative), or `On pace` (zero). It is a pace comparison based on the last reported usage — never a prediction of future consumption.

## Gotchas

- `chat.Record` lines are the only input. Anything that does not look like a Claude `rate_limit_event` or Codex `account/rateLimits/updated` is silently ignored — including token-usage events. The parser must be the authority, not the consumer.
- Codex primary/secondary slots are positional, not durations. Use `WindowDurationMins` to label them. The frontend defaults Codex `primary` to a weekly label only when no duration is present; other durations render as `Nd` / `Nh` / `Nm`.
- Claude `five_hour` and `seven_day` window ids are named with fixed durations in the frontend. New Claude window ids without an explicit `duration_minutes` field will fall through to a fallback of `id` unless the frontend hardcodes them in `windowDurationMinutes`.
- `UsedPercent` is a `*float64` (pointer) for a reason: a missing utilization must not be coerced to 0. The frontend renders `Reserve unavailable` when either the duration, used percentage, or reset timestamp is missing — it never invents a zero.
- An expired window (reset timestamp in the past) renders `Awaiting update` and `0h left`. Do not show a fabricated reserve for the next window; the next live report is what resets the panel.
- `Observe` mutates a clone of the report, not the caller's slice. Tests assert this to catch pointer aliasing (`*got[0].Windows[0].UsedPercent = 99` must not poison the store).
- `GET /api/usage` returns the persisted snapshot only — it never triggers a provider request. The 60s panel refresh is local polling of the daemon, not a rate-limit probe.
- `sidebarPanels.ts` is the source of truth for known panel ids. Adding a new panel means editing both the registry and the backend accessor (`internal/config/config.go`); otherwise the toggle is silently dropped by `resolveSidebarPanels`.

## Common modification patterns

- **To add a new plan-usage parser (e.g., Gemini):** write a `ParseGeminiPlanUsage` in `internal/usage/parse.go` with a table-driven `parse_test.go`, add a new branch in `Server.observeChatUsage` that selects on `session.EffectiveChatProtocol()`, and add a `case` to the protocol's `LiveOnly` if the protocol can also emit quota lines.
- **To add a new provider display name:** extend `PROVIDER_NAMES` in `PlanUsagePanel.tsx`. No backend change needed — the panel receives the resolved provider id.
- **To add a new sidebar panel:** add the id to `SidebarPanelID` and `SIDEBAR_PANEL_META` in `assets/dashboard/src/lib/sidebarPanels.ts`, add a `Get<Name>PanelEnabled()` accessor on `*config.Config`, and add the gated endpoint or broadcast. The Advanced tab Settings UI auto-renders the checkbox from the registry.
- **To change the reserve/deficit math:** edit `WindowBalance` in `PlanUsagePanel.tsx` and update `PlanUsagePanel.test.tsx`. The test file uses `vi.useFakeTimers()` and a frozen `now`; advance the clock with `vi.advanceTimersByTimeAsync` to test pace recomputation without sleeping.
