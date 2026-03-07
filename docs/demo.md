# Interactive Demo

## What it does

The interactive demo lets visitors try schmux without installing anything. It renders the real dashboard React components but swaps the live backend for a mock transport layer that serves scripted data. A spotlight-based tour system overlays the dashboard to guide users step by step.

## Key files

| File                                                           | Purpose                                                                                                          |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/transport.ts`                        | `Transport` interface, `liveTransport` default, `setTransport()` module-level singleton                          |
| `assets/dashboard/website/src/demo/DemoShell.tsx`              | Demo entry point: installs mock transport, wraps `<App />` in `<TourProvider>`, renders scenario switcher banner |
| `assets/dashboard/website/src/demo/main.tsx`                   | Hash-based routing (`#/workspaces`, `#/spawn`), `MemoryRouter` with re-render on `hashchange`                    |
| `assets/dashboard/website/src/demo/transport/mockTransport.ts` | `createDemoTransport()`: mock fetch + mock WebSocket factories                                                   |
| `assets/dashboard/website/src/demo/transport/MockWebSocket.ts` | Mock WebSocket class with binary frame playback                                                                  |
| `assets/dashboard/website/src/demo/transport/mockData.ts`      | Workspace/session fixture data, `createDemoWorkspaces()`, `createPostSpawnWorkspaces()`                          |
| `assets/dashboard/website/src/demo/tour/TourProvider.tsx`      | Tour state machine, step advancement, `beforeStep`/`afterStep` hooks                                             |
| `assets/dashboard/website/src/demo/tour/Spotlight.tsx`         | Dark overlay with CSS `clip-path` cutout around target element                                                   |
| `assets/dashboard/website/src/demo/tour/Tooltip.tsx`           | Step tooltip with title, body, progress indicator                                                                |
| `assets/dashboard/website/src/demo/tour/types.ts`              | `TourScenario` and `TourStep` type definitions                                                                   |
| `assets/dashboard/website/src/demo/scenarios/workspaces.ts`    | "Monitor running agents" scenario: steps + initial mock state                                                    |
| `assets/dashboard/website/src/demo/scenarios/spawn.ts`         | "Launch a new agent" scenario: steps + state transitions                                                         |
| `assets/dashboard/website/src/demo/recordings/workspaces.ts`   | Terminal ANSI recordings for the workspaces scenario (3 sessions)                                                |
| `assets/dashboard/website/src/demo/recordings/spawn.ts`        | Terminal ANSI recording for the spawn scenario (new agent)                                                       |
| `assets/dashboard/website/src/demo/recordings/types.ts`        | `TerminalRecording` and `TerminalFrame` types                                                                    |
| `assets/dashboard/website/src/demo/demo.css`                   | Demo banner and overlay styling                                                                                  |

## Architecture decisions

- **Same components, different transport.** The `Transport` interface (`createWebSocket` + `fetch`) is consumed by `useSessionsWebSocket`, `TerminalStream`, and `api.ts` instead of calling browser APIs directly. The live transport delegates to `new WebSocket()` and `window.fetch()`. The mock transport intercepts and returns scripted data. No dashboard component knows whether it is running in demo or production mode.
- **Module-level singleton, not React context.** `transport.ts` exports a mutable `transport` variable via `setTransport()`. ESM named exports are live bindings, so consumers see updates immediately. This is set synchronously during render in `DemoShell` (before `App`'s effects fire), because `useEffect` would be too late.
- **Hash routing for GitHub Pages compatibility.** Demo URLs use `#/workspaces` and `#/spawn` to avoid 404s on client-side route refresh. The `MemoryRouter` is re-created with a fresh `key` on hash change to reset React state cleanly.
- **Lazy-loaded routes.** The demo code is code-split from the marketing page so the main bundle is unaffected.
- **Spotlight uses `clip-path`, not z-index tricks.** The highlighted element remains interactive (clickable for `advanceOn: 'click'` steps). The overlay dims everything else.
- **Terminal recordings are hand-crafted.** Each recording is a sequence of `{ delay: number, data: string }` frames played back with realistic timing. User keystrokes are silently ignored.

## Gotchas

- `DemoShell` restores the live transport on unmount, but only if its transport is still active. This prevents a race when switching scenarios: the new `DemoShell` installs its transport before the old one unmounts.
- The `createScenarioSetup()` function is called before render (in `main.tsx`) so the transport is available when `App` mounts. Scenario setup includes wiring `afterStep` hooks that need transport access (e.g., updating workspaces after spawn).
- `data-tour="..."` attributes on dashboard components serve as stable selectors for the tour. They are purely additive -- no behavior or visual change in production.
- The spawn scenario dynamically patches steps 4 and 5 to wire transport state changes (`dt.updateWorkspaces()`) and route navigation.
- Mock WebSocket messages for the dashboard use the same JSON format as the real `/ws/dashboard` endpoint. Mock terminal WebSocket messages are binary frames matching the real terminal protocol.

## Common modification patterns

- **To add a new demo scenario:** Create `assets/dashboard/website/src/demo/scenarios/<name>.ts` with a `TourScenario` definition, add terminal recordings in `recordings/<name>.ts`, and register the scenario in the `SCENARIOS` map in `DemoShell.tsx`.
- **To add a tour step:** Append a `TourStep` to the scenario's `steps` array. Use `data-tour` attributes as selectors. Use `beforeStep` to inject mock state before the step renders.
- **To change mock data (workspaces, sessions):** Edit `assets/dashboard/website/src/demo/transport/mockData.ts`.
- **To add a new dashboard component to the tour:** Add a `data-tour="<name>"` attribute to the component in the dashboard source, then reference it in the scenario step's `target` field.
- **To capture a real terminal recording:** Intercept WebSocket frames from a live terminal session and convert to the `TerminalFrame[]` format. No automated capture tool exists yet.
