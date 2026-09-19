# Plan Usage

## What it does

Tracks the latest plan quota snapshots reported by Claude and Codex chat harnesses and fetched directly from Kimi, z.ai, and MiniMax. The sidebar compares reported utilization to the elapsed share of each window. Each window uses one row: time horizon, reserve/deficit, and time remaining.

## Key files

| File                                                      | Purpose                                                                                     |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `internal/usage/manager.go`                               | In-memory + on-disk snapshot store keyed by provider; atomic write, fail-soft on errors     |
| `internal/usage/parse.go`                                 | Claude (`rate_limit_event`) and Codex (`account/rateLimits/updated`) line parsers           |
| `internal/usage/fetch.go`, `fetch_parse.go`               | API-key quota collector and Kimi, z.ai, MiniMax response parsers                            |
| `internal/usage/fetch_test.go`, `fetch_parse_test.go`     | HTTP, persistence, cancellation, fake-clock cadence, and provider payload tests             |
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
- **Collection is independent of visibility.** Harness quota collection and API-key collection both run when the sidebar is hidden. `ui.panels.planUsage` controls presentation, not collection.
- **Live capture, not replay.** The manager never scans chat files or workspace directories. It receives parsed harness reports and successful API quota responses. Persistence is best-effort: a failed write logs and restores the previous snapshot.
- **Direct provider collection.** The HTTP server starts the collector after binding and cancels it when serving ends. It fetches immediately and every minute, loading each provider's existing `ANTHROPIC_AUTH_TOKEN` through `config.GetProviderSecrets` on each pass. Missing keys skip requests; failed fetches retain the last successful snapshot. Requests have a 15-second timeout, reject redirects, and log structural errors without credentials or response bodies. There are no browser credentials, transcript scans, extra secret fields, or provider-enablement requirements.
- **Per-feature sidebar panel toggles.** The old global `debug_ui` is gone. Each panel has its own `ui.panels.<id>` bool. Every panel starts disabled; the frontend registry (`sidebarPanels.ts`) lists every known id so unknown config keys are silently dropped.
- **Reserve/deficit, not prediction.** The panel reports `elapsed-window % - used %` rounded to whole points, labeled `reserve` (positive), `deficit` (negative), or `On pace` (zero). It is a pace comparison based on the last reported usage — never a prediction of future consumption.

## Gotchas

- Harness input that does not look like a Claude `rate_limit_event` or Codex `account/rateLimits/updated` is silently ignored — including token-usage events. Direct quota API responses are a separate input, not synthetic harness events.
- Codex primary/secondary slots are positional, not durations. Use `WindowDurationMins` to label them. Without a duration, the frontend labels them Primary/Secondary and withholds reserve/deficit.
- Claude `five_hour` and `seven_day` window ids are named with fixed durations in the frontend. New Claude window ids without an explicit `duration_minutes` field will fall through to a fallback of `id` unless the frontend hardcodes them in `windowDurationMinutes`.
- `UsedPercent` is a `*float64` (pointer) for a reason: a missing utilization must not be coerced to 0. The frontend renders `N/A` for reserve/deficit when either the duration, used percentage, or reset timestamp is missing, and `N/A` for time remaining when the reset is missing — it never invents a zero.
- An expired window (reset timestamp in the past) renders `Awaiting update` and `0h left`. Do not show a fabricated reserve for the next window; the next live report is what resets the panel.
- `Observe` mutates a clone of the report, not the caller's slice. Tests assert this to catch pointer aliasing (`*got[0].Windows[0].UsedPercent = 99` must not poison the store).
- `GET /api/usage` returns the persisted snapshot only — it never triggers a provider request. The 60s panel refresh is local polling of the daemon, not a rate-limit probe.
- `sidebarPanels.ts` is the source of truth for known panel ids. Adding a new panel means editing both the registry and the backend accessor (`internal/config/config.go`); otherwise the toggle is silently dropped by `resolveSidebarPanels`.

## API-key providers

These endpoints match the provider regions already configured in `internal/models/profiles.go`. Keys are sent only to their respective HTTPS provider hosts. No endpoint discovery or cross-region fallback occurs.

| Provider ID                                  | Quota endpoint                                   | Mapping                                                                                                                                                                                                       |
| -------------------------------------------- | ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `moonshot` (Kimi Code, not metered Moonshot) | `https://api.kimi.com/coding/v1/usages`          | `limits[].detail` supplies the 5-hour count and `usage` supplies the weekly count. The `usages` ratio pools are used only when their corresponding count is absent or invalid.                                |
| `zai`                                        | `https://api.z.ai/api/monitor/usage/quota/limit` | The coding-plan seven-day quota (`unit: 6`); `nextResetTime` is milliseconds. Counts take precedence over rounded percentage. Other entries, including MCP `TIME_LIMIT`, are not displayed.                   |
| `minimax`                                    | `https://api.minimax.io/v1/token_plan/remains`   | Text/general quota lanes; remaining percentage becomes used percentage. Start/end milliseconds supply duration and reset. HTTP 404 falls back to `/v1/api/openplatform/coding_plan/remains` on the same host. |

Provider-specific caveats:

- Kimi's direct API can return zero-valued `limit_5h` / `limit_7d` ratio pools beside valid nonzero counts. The 2026-09-19 read-only comparison returned `used_ratio: 0` but 13/100 for the 5-hour count and 30/100 for the weekly count. The parser uses valid counts, including an actual zero, and falls back to a ratio only when that count is absent or invalid. It keeps the count's reset time with the count. `limit_month_total`, when reported, retains its percentage and reset but no invented fixed monthly duration; the existing UI therefore cannot calculate its reserve.
- z.ai counts can differ slightly from its integer percentage. Use the larger of reported current usage and total minus remaining, as CodexBar does. Only its explicit seven-day coding-plan quota is displayed.
- MiniMax's `current_interval_usage_count` and `current_weekly_usage_count` mean **remaining**, despite their names. New Token Plan responses can set all counts to zero while providing usable remaining percentages; percentages take precedence. Unavailable/unlimited status-3 lanes and non-coding services are not finite coding-plan windows and are excluded.
- The three endpoint requests succeeded with configured keys during the read-only spike. Automated tests use quota-only captured payloads and injected HTTP transports; they never use real credentials or external APIs.

Reference implementations inspected in [CodexBar](https://github.com/steipete/CodexBar):

- [Kimi API and web-source requests](https://github.com/steipete/CodexBar/blob/main/Sources/CodexBarCore/Providers/Kimi/KimiUsageFetcher.swift), [source selection](https://github.com/steipete/CodexBar/blob/main/Sources/CodexBarCore/Providers/Kimi/KimiProviderDescriptor.swift), and [window mapping](https://github.com/steipete/CodexBar/blob/main/Sources/CodexBarCore/Providers/Kimi/KimiUsageSnapshot.swift). CodexBar's API-key source prioritizes ratio pools, while its web-cookie source uses count fields. Schmux has only the configured coding-plan key; that key received 401 from the web-cookie endpoint, so Schmux selects the valid direct-API counts instead of claiming to use CodexBar's web source.
- [z.ai quota parser](https://github.com/steipete/CodexBar/blob/main/Sources/CodexBarCore/Resources/Plugins/zai.js).
- [MiniMax API requests and quota semantics](https://github.com/steipete/CodexBar/blob/main/Sources/CodexBarCore/Providers/MiniMax/MiniMaxUsageFetcher.swift).

## Common modification patterns

- **To add a new plan-usage parser (e.g., Gemini):** write a `ParseGeminiPlanUsage` in `internal/usage/parse.go` with a table-driven `parse_test.go`, add a new branch in `Server.observeChatUsage` that selects on `session.EffectiveChatProtocol()`, and add a `case` to the protocol's `LiveOnly` if the protocol can also emit quota lines.
- **To add a new provider display name:** extend `PROVIDER_NAMES` in `PlanUsagePanel.tsx`. No backend change needed — the panel receives the resolved provider id.
- **To add a new sidebar panel:** add the id to `SidebarPanelID` and `SIDEBAR_PANEL_META` in `assets/dashboard/src/lib/sidebarPanels.ts`, add a `Get<Name>PanelEnabled()` accessor on `*config.Config`, and add the gated endpoint or broadcast. The Advanced tab Settings UI auto-renders the checkbox from the registry.
- **To change the reserve/deficit math:** edit `WindowBalance` in `PlanUsagePanel.tsx` and update `PlanUsagePanel.test.tsx`. The test file uses `vi.useFakeTimers()` and a frozen `now`; advance the clock with `vi.advanceTimersByTimeAsync` to test pace recomputation without sleeping.
