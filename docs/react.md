# Frontend Architecture

This document describes the architecture, patterns, and conventions for the schmux React frontend. It serves as a reference for developers working on the dashboard.

## Overview

The schmux frontend is a single-page application built with React 18 that provides real-time monitoring and management of AI agent tmux sessions. It communicates with a Go daemon via REST API and two WebSocket connections.

**Key characteristics:**

- Real-time session/workspace state via WebSocket (not polling)
- Live terminal streaming via WebSocket + xterm.js
- No component library -- all custom components
- TypeScript throughout
- Tests via Vitest + React Testing Library (run through `./test.sh`)

## Technology stack

### Core

| Technology   | Version | Purpose                   |
| ------------ | ------- | ------------------------- |
| React        | ^18.2.0 | UI framework              |
| React Router | ^6.22.3 | Client-side routing       |
| TypeScript   | ^5.6.3  | Type safety               |
| Vite         | ^7.3.1  | Build tool and dev server |

### Specialized

| Technology                  | Purpose                      |
| --------------------------- | ---------------------------- |
| @xterm/xterm ^6.0.0         | Terminal emulation           |
| @xterm/addon-webgl          | GPU-accelerated rendering    |
| @xterm/addon-unicode11      | Full Unicode support         |
| @dnd-kit/core + sortable    | Drag-and-drop tab reordering |
| react-diff-viewer-continued | Diff visualization           |
| react-markdown + remark-gfm | Markdown rendering           |
| qrcode.react                | QR code generation           |

### Build

The dashboard is built via `go run ./cmd/build-dashboard` (never `npm run build` directly). Vite outputs to `assets/dashboard/dist/` which is embedded in the Go binary. Manual chunks split vendor, xterm, markdown, and dnd-kit into separate bundles.

## Directory structure

```
assets/dashboard/src/
├── main.tsx                  # Entry point: BrowserRouter + ErrorBoundary
├── App.tsx                   # Provider tree + route definitions
│
├── components/               # ~30 reusable UI components
│   ├── AppShell.tsx          # Layout wrapper (sidebar + Outlet)
│   ├── ErrorBoundary.tsx     # React error boundary
│   ├── SessionTabs.tsx       # Draggable session + accessory tab bar
│   ├── WorkspaceHeader.tsx   # Workspace info header
│   ├── ActionDropdown.tsx    # Quick launch + emerged actions dropdown
│   ├── PastebinDropdown.tsx  # Quick-paste text clips
│   ├── GitHistoryDAG.tsx     # SVG commit graph
│   ├── ToolsSection.tsx      # Sidebar tools nav
│   ├── EventMonitor.tsx      # Real-time event stream (dev mode)
│   ├── StreamMetricsPanel.tsx # WebSocket stream health (dev mode)
│   ├── TypingPerformance.tsx # Input latency diagnostics (dev mode)
│   └── ...                   # ModalProvider, ToastProvider, Tooltip, Icons, etc.
│
├── contexts/                 # 10 React Context providers
│   ├── ConfigContext.tsx     # Daemon config (REST, reloadable)
│   ├── FeaturesContext.tsx   # Feature flags from server
│   ├── SessionsContext.tsx   # Sessions + workspaces (WebSocket -- main state)
│   ├── ViewedSessionsContext.tsx  # "New" badge tracking (localStorage)
│   ├── KeyboardContext.tsx   # Vim-style keyboard mode + scoped shortcuts
│   ├── CurationContext.tsx   # Lore curation lifecycle
│   ├── SyncContext.tsx       # Linear sync / workspace lock state
│   ├── OverlayContext.tsx    # Overlay change events
│   ├── RemoteAccessContext.tsx # Remote access status
│   └── MonitorContext.tsx    # Monitor events
│
├── hooks/                    # 15 custom React hooks
│   ├── useSessionsWebSocket.ts  # Dashboard WebSocket + message parsing
│   ├── useTerminalStream.ts  # xterm.js lifecycle management
│   ├── useConnectionMonitor.ts  # Health check polling
│   ├── useTheme.ts           # Dark/light toggle (localStorage)
│   ├── useLocalStorage.ts    # Generic localStorage hook
│   ├── useTabOrder.ts        # Session tab drag reorder
│   ├── useSidebarLayout.ts   # Resizable sidebar + keyboard nav
│   ├── useSync.ts            # Git sync operations
│   ├── useFloorManager.ts    # Floor manager state
│   └── ...                   # useFocusTrap, useDebouncedCallback, etc.
│
├── lib/                      # ~25 utility modules
│   ├── api.ts                # REST API wrappers (all fetch calls)
│   ├── transport.ts          # Pluggable fetch/WebSocket (for testing)
│   ├── csrf.ts               # CSRF token reading + header injection
│   ├── terminalStream.ts     # xterm.js WebSocket class
│   ├── navigation.ts         # Workspace nav + pending navigation
│   ├── types.ts              # Manual TypeScript types
│   ├── types.generated.ts    # Auto-generated from Go (DO NOT EDIT)
│   ├── gitGraphLayout.ts     # Commit DAG layout algorithm
│   ├── inputLatency.ts       # Input latency measurement
│   ├── streamDiagnostics.ts  # WebSocket stream health tracking
│   └── ...                   # tabOrder, quicklaunch, screenDiff, etc.
│
├── routes/                   # ~25 page components (one per route)
│   ├── HomePage.tsx          # Workspace list + session overview
│   ├── SessionDetailPage.tsx # Live terminal view
│   ├── SpawnPage.tsx         # Multi-step spawn wizard
│   ├── DiffPage.tsx          # Git diff viewer
│   ├── GitGraphPage.tsx      # Interactive commit graph
│   ├── ConfigPage.tsx        # Settings editor (tabbed)
│   ├── EnvironmentPage.tsx   # System vs tmux env comparison
│   ├── TimelapsePage.tsx     # Session recording viewer
│   ├── RepofeedPage.tsx      # Cross-developer activity feed
│   └── ...                   # Personas, Lore, Overlays, etc.
│
└── styles/
    ├── global.css            # Design tokens, base styles
    └── *.module.css          # CSS Module files for page-specific styles
```

## State management

### Provider tree

Defined in `App.tsx`:

```
ConfigProvider → FeaturesProvider → ToastProvider → ModalProvider →
  SessionsProvider → ViewedSessionsProvider → KeyboardProvider → CurationProvider
```

`SessionsProvider` also populates four sub-contexts from the same WebSocket: `SyncContext`, `OverlayContext`, `RemoteAccessContext`, `MonitorContext`.

### Real-time state flow

1. `useSessionsWebSocket` connects to `/ws/dashboard` with auto-reconnect
2. Server pushes typed messages: `sessions`, `workspace_locked`, `overlay_change`, `remote_access_status`, `monitor_event`, etc.
3. `SessionsProvider` distributes state across contexts
4. Components subscribe via hooks (`useSessions()`, `useSyncState()`, etc.)

The only polling is `useConnectionMonitor` (health check) and `useFloorManager`.

### Pending navigation

When spawning a session, the UI cannot navigate immediately because the session doesn't exist in WebSocket data yet. The pending navigation system stores a target, watches WebSocket updates, and navigates automatically when the target appears.

### Transport abstraction

All network calls go through `lib/transport.ts`. Production uses native browser APIs. Tests substitute a mock transport via `setTransport()`.

## Routing

All routes are nested under `AppShell` (sidebar + `<Outlet />`).

| Path                                    | Page                          |
| --------------------------------------- | ----------------------------- |
| `/`                                     | HomePage                      |
| `/sessions/:sessionId`                  | SessionDetailPage             |
| `/spawn`                                | SpawnPage                     |
| `/diff/:workspaceId`                    | DiffPage                      |
| `/diff/:workspaceId/md/:filepath`       | MarkdownPreviewPage           |
| `/diff/:workspaceId/img/:filepath`      | ImagePreviewPage              |
| `/commits/:workspaceId`                 | GitGraphPage                  |
| `/commits/:workspaceId/:commitHash`     | GitCommitPage                 |
| `/preview/:workspaceId/:previewId`      | PreviewPage                   |
| `/resolve-conflict/:workspaceId/:tabId` | LinearSyncResolveConflictPage |
| `/config`                               | ConfigPage                    |
| `/settings/remote`                      | RemoteSettingsPage            |
| `/environment`                          | EnvironmentPage               |
| `/overlays`                             | OverlayPage                   |
| `/personas`                             | PersonasListPage              |
| `/personas/create`                      | PersonaCreatePage             |
| `/personas/:personaId`                  | PersonaEditPage               |
| `/lore`                                 | LorePage                      |
| `/events`                               | EventsPage (dev mode)         |
| `/timelapse`                            | TimelapsePage                 |
| `/repofeed`                             | RepofeedPage                  |
| `/tips`                                 | TipsPage                      |

## WebSocket connections

| Endpoint            | Direction       | Purpose                                                               |
| ------------------- | --------------- | --------------------------------------------------------------------- |
| `/ws/dashboard`     | Server → client | Session/workspace state, sync events, overlay changes, curator events |
| `/ws/terminal/{id}` | Bidirectional   | Terminal I/O for a single session                                     |

## Architecture decisions

- **React Context over state management library.** Contexts are split to minimize re-renders. Complexity does not warrant Redux/Zustand.
- **WebSocket, not polling.** Session/workspace state arrives via `/ws/dashboard`. Instant updates, no unnecessary network traffic.
- **Class-based TerminalStream.** xterm.js has an imperative API with complex lifecycle. A class wrapper in `lib/terminalStream.ts` encapsulates this; `useTerminalStream` bridges to React.
- **No UI component library.** Total control over look and feel, smaller bundle, no lock-in.
- **CSS over CSS-in-JS.** Design tokens in CSS custom properties. No runtime CSS generation.
- **Pluggable transport.** `lib/transport.ts` allows tests to substitute mock fetch/WebSocket without mocking globals.

## Gotchas

- **Never edit `types.generated.ts`.** Edit Go structs in `internal/api/contracts/`, then run `go run ./cmd/gen-types`.
- **Never call `window.fetch()` or `new WebSocket()` directly.** Use `transport.fetch()` and `transport.createWebSocket()` so tests can intercept.
- **Never run npm/vite directly.** Build via `go run ./cmd/build-dashboard`. Test via `./test.sh`.
- **Session/workspace state comes from WebSocket, not REST.** Don't add polling for data that's already broadcast.
- **Derived state belongs in render, not useEffect.** Compute derived values during render, not via `useEffect` + `setState`.
