# Interactive Demo Design

**Date:** 2026-02-27
**Branch:** feature/website

## Problem

Potential users visiting the schmux marketing site can only see static screenshots. They have no way to experience the product without installing it and having access to AI agents. New users who have installed also lack guided onboarding for common workflows.

## Goals

1. Let visitors try schmux without installing anything or needing AI agents
2. Teach usage workflows through guided, scripted scenarios
3. Host as a static site (GitHub Pages) with no backend
4. Use the real dashboard code to prevent UX drift between the demo and the product

## Solution

An interactive demo built as lazy-loaded routes within the existing marketing website. The demo renders the real dashboard React components but swaps the live backend connection for a mock transport layer that serves scripted data. A spotlight-based tour system overlays the dashboard to guide users step by step.

## Architecture

```
Marketing Website (existing)          Demo Routes (new, lazy-loaded)
┌──────────────────────┐          ┌──────────────────────────────┐
│ /                    │          │ /demo/#/workspaces           │
│  Hero                │──CTA───▶│ /demo/#/spawn                │
│  Sessions            │──CTA───▶│                              │
│  Spawn               │──CTA───▶│  TransportProvider(mock)     │
│  Personas            │          │   ├─ TourProvider(scenario)  │
│  FloorManager        │          │   └─ <App /> ← same as real │
│  Close               │          │       dashboard components   │
└──────────────────────┘          └──────────────────────────────┘
```

### Key principle: same components, different transport

The dashboard already has clean context boundaries (`SessionsProvider`, `ConfigProvider`, `TerminalStream`). All three connect to the backend through WebSocket or fetch. We introduce a thin transport abstraction at this lowest layer so every React component, hook, and provider remains 100% shared.

```
Before (direct):
  useSessionsWebSocket.ts  →  new WebSocket('/ws/dashboard')
  terminalStream.ts        →  new WebSocket('/ws/terminal/{id}')
  api.ts                   →  fetch('/api/...')

After (abstracted):
  useSessionsWebSocket.ts  →  transport.createWebSocket('/ws/dashboard')
  terminalStream.ts        →  transport.createWebSocket('/ws/terminal/{id}')
  api.ts                   →  transport.fetch('/api/...')
```

The transport is provided once at the app root:

```tsx
// Real dashboard (unchanged behavior)
<TransportProvider transport={liveTransport}>
  <App />
</TransportProvider>

// Demo (same App, different transport)
<TransportProvider transport={demoTransport}>
  <TourProvider scenario="spawn">
    <App />
  </TourProvider>
</TransportProvider>
```

## Transport Interface

```typescript
interface Transport {
  // Returns a WebSocket-compatible object (real or mock)
  createWebSocket(url: string, protocols?: string[]): WebSocket;

  // Returns a fetch-compatible function (real or mock)
  fetch(input: RequestInfo, init?: RequestInit): Promise<Response>;
}

// Default: passes through to browser APIs
const liveTransport: Transport = {
  createWebSocket: (url, protocols) => new WebSocket(url, protocols),
  fetch: (input, init) => window.fetch(input, init),
};
```

A `TransportContext` provides the transport to the app. Hooks like `useSessionsWebSocket` and the `TerminalStream` class consume it instead of calling browser APIs directly.

## Mock Transport

The demo transport intercepts all network calls and returns scripted data:

### Mock WebSocket (dashboard state)

When `createWebSocket('/ws/dashboard')` is called, returns a mock that:

1. Immediately emits `onopen`
2. Emits a `{type: "sessions", workspaces: [...]}` message with the scenario's initial state
3. Responds to tour step transitions by emitting updated state (e.g., after "spawn" step completes, emits state with the new session added)

### Mock WebSocket (terminal)

When `createWebSocket('/ws/terminal/{id}')` is called, returns a mock that:

1. Emits `onopen`
2. Plays back a recorded terminal session as binary frames with realistic timing

### Mock fetch (API calls)

When `fetch('/api/...')` is called:

- GET requests return canned JSON responses (config, git graph, etc.)
- POST/PUT/DELETE requests return success and notify the tour system so it can advance state

## Terminal Playback

Terminals show animated playback of scripted ANSI output. The xterm.js instance is real (same addons, theme, fonts) — only the data source changes.

```typescript
interface TerminalRecording {
  sessionId: string;
  frames: TerminalFrame[];
}

interface TerminalFrame {
  delay: number; // ms since previous frame
  data: string; // raw text with ANSI escape codes
}
```

Example recording:

```typescript
const recording: TerminalRecording = {
  sessionId: 'demo-session-1',
  frames: [
    { delay: 0, data: '$ ' },
    { delay: 800, data: 'claude --prompt "Add user auth"\n' },
    { delay: 200, data: '\x1b[36m●\x1b[0m Analyzing codebase...\n' },
    { delay: 1500, data: '\x1b[36m●\x1b[0m Reading src/auth/...\n' },
    { delay: 2000, data: '\x1b[32m✓\x1b[0m Created auth middleware\n' },
  ],
};
```

The mock terminal WebSocket:

1. On connect, clears the terminal and starts playback from frame 0
2. Each frame's `data` is sent as a binary WebSocket message after its `delay`
3. The tour system can `pause()`, `resume()`, or `seekTo(frameIndex)` the playback
4. User keystrokes are silently ignored

For v1, recordings are hand-crafted. A recording capture tool (intercept real terminal WebSocket frames with timestamps) can be built later.

## Tour System

The tour system sits on top of the dashboard as a transparent overlay. It does not modify dashboard components — it highlights them from outside.

### Tour definition

```typescript
interface TourScenario {
  id: string;
  title: string;
  initialRoute: string;
  initialState: WorkspaceResponse[];
  steps: TourStep[];
}

interface TourStep {
  target: string; // CSS selector (prefer data-tour attributes)
  title: string;
  body: string;
  placement: 'top' | 'bottom' | 'left' | 'right';
  advanceOn: 'click' | 'next'; // click = advance when user clicks target
  beforeStep?: () => void; // inject state changes before this step
  afterStep?: () => void; // update mock state after interaction
}
```

### Spotlight overlay

```
┌─────────────────────────────────────────────┐
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │  dark overlay
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░░░┌─────────────┐░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░░░│  [Spawn] btn │░░░░░░░░░░░░░░░░░░░░ │  spotlight cutout
│ ░░░░░░└─────────────┘░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░│░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░░░┌──────▼────────────┐░░░░░░░░░░░░░░ │
│ ░░░░░░│ Start a new session│░░░░░░░░░░░░░░ │  tooltip
│ ░░░░░░│ Click Spawn to     │░░░░░░░░░░░░░░ │
│ ░░░░░░│ launch an agent.   │░░░░░░░░░░░░░░ │
│ ░░░░░░│        [1/6] Next →│░░░░░░░░░░░░░░ │
│ ░░░░░░└───────────────────┘░░░░░░░░░░░░░░ │
└─────────────────────────────────────────────┘
```

- Spotlight cutout uses CSS `clip-path` so the highlighted element remains interactive
- `advanceOn: 'click'` means the tour advances when the user clicks the target — learn by doing
- `beforeStep` / `afterStep` hooks inject state changes into the mock transport
- Step transitions animate smoothly (cutout slides, tooltip fades)

### data-tour attributes

A small number (~10-15) of `data-tour="..."` attributes are added to key dashboard elements as stable selectors. These are purely additive — no behavior change, no visual change in production.

## Scenarios (v1)

### `workspaces` — Monitor running agents

**What the user experiences:** A populated dashboard with 2 workspaces and 3 running agent sessions. Terminals show animated AI output. The tour guides the user through:

1. Sidebar workspace list — understand the layout
2. Session status indicators — what running/stopped/error mean
3. Click into a session — see live terminal output
4. Scrollback — scroll up through history
5. Navigate between sessions — sidebar navigation

**Initial mock state:** 2 workspaces, each with 1-2 sessions in various states (running, completed). Pre-recorded terminal output for each session.

### `spawn` — Launch a new agent

**What the user experiences:** Start from an empty-ish state (1 workspace, no sessions). The tour walks through:

1. Click Spawn in the sidebar
2. Select a repository
3. Choose a persona
4. Write a prompt
5. Hit Spawn — see the session appear in the sidebar
6. Watch the terminal come alive with agent output

**State transitions:** After step 5, the mock transport emits a new session via the dashboard WebSocket. After step 6, the terminal recording begins playback.

## File Structure

```
assets/dashboard/
├── src/                              existing dashboard (shared)
│   ├── lib/
│   │   ├── transport.ts              NEW: Transport interface + context
│   │   ├── api.ts                    MODIFIED: use transport.fetch()
│   │   └── terminalStream.ts         MODIFIED: accept WebSocket factory
│   ├── hooks/
│   │   └── useSessionsWebSocket.ts   MODIFIED: use transport.createWebSocket()
│   ├── components/                   MODIFIED: add data-tour attrs (~10-15 elements)
│   └── main.tsx                      MODIFIED: wrap in TransportProvider(live)
│
├── website/
│   ├── src/
│   │   ├── App.tsx                   MODIFIED: add React Router, lazy demo routes
│   │   ├── sections/                 existing marketing sections
│   │   │   └── *.tsx                 MODIFIED: add CTA links to /demo/#/...
│   │   └── demo/                     NEW
│   │       ├── DemoShell.tsx         demo entry: mock transport + tour + dashboard App
│   │       ├── transport/
│   │       │   ├── mockTransport.ts  mock fetch + mock WebSocket factory
│   │       │   └── mockData.ts       workspace/session fixture data
│   │       ├── tour/
│   │       │   ├── TourProvider.tsx   spotlight overlay + step management
│   │       │   ├── Spotlight.tsx      dark overlay with CSS clip-path cutout
│   │       │   ├── Tooltip.tsx        step tooltip (title, body, progress)
│   │       │   └── types.ts          TourScenario, TourStep types
│   │       ├── recordings/           terminal ANSI recordings
│   │       │   ├── workspaces.ts     terminal frames for workspaces scenario
│   │       │   └── spawn.ts          terminal frames for spawn scenario
│   │       └── scenarios/
│   │           ├── workspaces.ts     scenario definition + steps + mock state
│   │           └── spawn.ts          scenario definition + steps + mock state
│   └── vite.config.ts               no changes needed (already builds website)
```

## URL Structure

```
/schmux/                    marketing website (existing)
/schmux/demo/#/workspaces   workspaces scenario
/schmux/demo/#/spawn        spawn scenario
```

Hash routing (`#/`) avoids GitHub Pages 404 issues with client-side routing. The demo routes are code-split so the marketing page bundle is unaffected.

## Refactor Scope

Changes to existing dashboard code (minimal, purely additive):

| File                      | Change                                                               | Risk                                    |
| ------------------------- | -------------------------------------------------------------------- | --------------------------------------- |
| `transport.ts`            | New file: `Transport` interface, `TransportContext`, `liveTransport` | None (new)                              |
| `useSessionsWebSocket.ts` | Replace `new WebSocket(...)` with `transport.createWebSocket(...)`   | Low — same behavior with live transport |
| `terminalStream.ts`       | Accept WebSocket factory in constructor                              | Low — same behavior with live transport |
| `api.ts`                  | Replace `fetch(...)` with `transport.fetch(...)`                     | Low — same behavior with live transport |
| `main.tsx`                | Wrap `<App />` in `<TransportProvider transport={liveTransport}>`    | None — pass-through                     |
| ~10-15 component files    | Add `data-tour="..."` attributes                                     | None — inert in production              |

All changes are backward-compatible. The live transport delegates directly to browser APIs, producing identical runtime behavior.
