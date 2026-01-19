# Frontend Architecture

**Last Updated:** 2025-01-08

This document describes the architecture, patterns, and conventions for the Schmux React frontend. It serves as a reference for developers and a guide for future architectural decisions.

---

## Table of Contents

1. [Overview](#overview)
2. [Technology Stack](#technology-stack)
3. [Architecture Principles](#architecture-principles)
4. [Directory Structure](#directory-structure)
5. [Component Patterns](#component-patterns)
6. [State Management](#state-management)
7. [Styling Approach](#styling-approach)
8. [API Integration](#api-integration)
9. [Routing](#routing)
10. [Decisions & Rationale](#decisions--rationale)
11. [Anti-Patterns to Avoid](#anti-patterns-to-avoid)
12. [Future Roadmap](#future-roadmap)

---

## Overview

The Schmux frontend is a **single-page application** built with React 18 that provides real-time monitoring and management of AI agent tmux sessions. It runs entirely in the browser, communicating with a Go daemon via REST API and WebSocket connections.

**Key Characteristics:**
- Dashboard-style UI for observability and orchestration
- Real-time terminal streaming via WebSocket
- CLI and web are first-class; web emphasizes observability and orchestration
- Minimal dependencies, maximal control
- No build step in development (Vite HMR)

---

## Technology Stack

### Core

| Technology | Version | Purpose |
|------------|---------|---------|
| React | 18.2.0 | UI framework |
| ReactDOM | 18.2.0 | DOM rendering |
| Vite | 5.0.12 | Build tool & dev server |
| React Router | 6.22.3 | Client-side routing |

### Specialized

| Technology | Purpose |
|------------|---------|
| @xterm/xterm | 5.5.0 | Terminal emulation |
| react-diff-viewer-continued | 3.4.0 | Diff visualization |
| react-tooltip | 5.30.0 | Tooltip library (deprecated - using custom) |

### Build

- **Bundler:** Vite
- **Module System:** ES Modules
- **Language:** TypeScript (TSX)
- **Package Manager:** npm

---

## Architecture Principles

### 1. Pragmatic Simplicity

> **Use the simplest solution that works.** Don't add abstraction until it's clearly needed.

**Examples:**
- React Context instead of Redux for global state
- Manual polling instead of WebSockets for everything
- Custom components instead of UI libraries

### 2. Progressive Enhancement

> **Core functionality should work without JavaScript, but enhance with it.**

**Current State:** Not fully implemented (SPA requires JS), but URLs remain bookmarkable and navigation works with browser back/forward.

### 3. Observable State

> **Server state is the source of truth.** The UI reflects it, doesn't own it.

**Implications:**
- Poll to refresh server state
- Optimistic updates used sparingly
- URL parameters drive view state

### 4. Graceful Degradation

> **When things fail, show something useful.**

**Implementation:**
- Loading states for all async operations
- Error boundaries to catch React errors
- Empty states when no data exists
- Retry mechanisms for failed requests

---

## Directory Structure

```
assets/dashboard/
├── src/
│   ├── main.tsx              # App entry point
│   ├── App.tsx               # Root component with routing
│   │
│   ├── components/           # Reusable UI components
│   │   ├── AppShell.tsx      # Layout wrapper
│   │   ├── ErrorBoundary.tsx # Error catching (planned)
│   │   ├── LoadingState.tsx  # Loading indicator (planned)
│   │   ├── ErrorState.tsx    # Error display (planned)
│   │   ├── EmptyState.tsx    # Empty data display (planned)
│   │   ├── ModalProvider.tsx # Modal dialogs
│   │   ├── ToastProvider.tsx # Toast notifications
│   │   ├── Tooltip.tsx       # Custom tooltip
│   │   ├── SessionTableRow.tsx    # Session list item
│   │   └── WorkspaceTableRow.tsx  # Workspace list item
│   │
│   ├── contexts/             # React Context providers
│   │   ├── ConfigContext.tsx      # Daemon configuration
│   │   └── ViewedSessionsContext.tsx  # Session view tracking
│   │
│   ├── hooks/                # Custom React hooks
│   │   ├── useConnectionMonitor.ts  # Health check polling
│   │   ├── useTheme.ts             # Theme toggle
│   │   └── useAsyncEffect.ts       # AbortController wrapper (planned)
│   │
│   ├── lib/                  # Utilities and libraries
│   │   ├── api.ts            # API layer (fetch wrappers)
│   │   ├── terminalStream.ts # xterm.js WebSocket wrapper
│   │   └── utils.ts          # Helper functions
│   │
│   ├── routes/               # Page components
│   │   ├── SessionsPage.tsx        # Sessions/workspaces list view
│   │   ├── SessionDetailPage.tsx   # Session terminal view
│   │   ├── SpawnPage.tsx           # Multi-step spawn wizard
│   │   ├── DiffPage.tsx            # Git diff viewer
│   │   └── TipsPage.tsx            # Help/tips content
│   │
│   └── styles/               # Global styles
│       ├── global.css        # Design tokens & base styles
│       └── tips.module.css   # CSS Modules (only usage)
│
├── index.html                # HTML entry point
├── vite.config.js            # Vite configuration
└── package.json              # Dependencies
```

---

## Component Patterns

### Component Categories

#### Layout Components

Components that define page structure and navigation.

**Example:** `AppShell.tsx`

```jsx
export default function AppShell() {
  return (
    <div className="app-shell">
      <header>...</header>
      <nav>...</nav>
      <main><Outlet /></main>
    </div>
  );
}
```

**Characteristics:**
- Use `<Outlet />` for nested routes
- Don't contain business logic
- Presentational only

#### Feature Components

Components that implement specific features or views.

**Examples:** `SessionsPage.tsx`, `SessionDetailPage.tsx`

**Characteristics:**
- May contain business logic
- Use hooks for data fetching
- Handle user interactions
- Can be complex

#### Provider Components

Components that provide context or services to children.

**Examples:** `ToastProvider.tsx`, `ModalProvider.tsx`, `ConfigProvider.tsx`

**Characteristics:**
- Wrap application or feature subtree
- Export custom hooks for access
- Minimal rendering (usually render children + portals)

#### UI Components

Reusable presentational components.

**Examples:** `Tooltip.tsx`, `LoadingState.tsx` (planned)

**Characteristics:**
- Highly reusable
- Controlled via props
- No business logic
- Well-documented props

### Component Design Rules

1. **Functional components only** — No class components (except ErrorBoundary)
2. **Props down, events up** — Follow unidirectional data flow
3. **Compound components** — Related components work together (Workspace/Session rows)
4. **Controlled components** — Form inputs controlled by React state
5. **Default exports** — Components use default export for consistency

---

## State Management

### Local Component State

**Use for:** UI-only state that doesn't need to be shared

```jsx
const [isOpen, setIsOpen] = useState(false);
const [items, setItems] = useState([]);
```

**Examples:**
- Modal open/closed
- Form input values
- Accordion expanded/collapsed
- Loading state for single operation

### React Context

**Use for:** Global state that needs to be accessed from multiple components

**Current Contexts:**

#### ConfigContext

Provides daemon configuration loaded at startup.

```jsx
const { config, loading, error } = useConfig();
// config.sessions.dashboard_poll_interval_ms
// config.terminal.width/height
```

#### ViewedSessionsContext

Tracks which sessions user has viewed for "New" badge logic.

```jsx
const { viewedSessions, markAsViewed } = useViewedSessions();
markAsViewed(sessionId);
```

### Server State

**Use for:** Data from the API

**Current Approach:** Manual polling with `useEffect` + `setInterval`

```jsx
useEffect(() => {
  loadWorkspaces();
  const interval = setInterval(() => loadWorkspaces(), 5000);
  return () => clearInterval(interval);
}, []);
```

**Planned:** React Query for automatic caching, refetching, and synchronization

### URL State

**Use for:** View state that should be bookmarkable

**Examples:**
- Filters (`?s=running&r=repo-name`)
- Resource IDs (`/sessions/{id}`)
- Spawn prefill (`?workspace_id=xxx&repo=yyy`)

```jsx
const [searchParams, setSearchParams] = useSearchParams();
const status = searchParams.get('s') || '';
```

### What NOT to Put in State

- **Derived data** — Compute from props/state during render
- **Props** — Don't mirror props in state
- **Module-level variables** — Avoid side effects (see anti-patterns)

---

## Styling Approach

### Design Tokens

CSS custom properties define the design system in `global.css`:

```css
:root {
  --color-surface: #ffffff;
  --color-text: #1a1a1a;
  --color-accent: #0066cc;
  --spacing-md: 12px;
  --radius-md: 6px;
  --font-sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', ...;
}
```

**Benefits:**
- Consistent spacing, colors, typography
- Easy theming (dark mode)
- Design consistency without CSS bloat

### Class Naming

**BEM-inspired** but pragmatic:

```jsx
<div className="session-detail">          {/* Block */}
  <div className="session-detail__main">  {/* Element */}
    <div className={isCollapsed ?        {/* Modifier */}
      "session-detail--sidebar-collapsed" : ""}>
```

**Utility classes** for common patterns:

```jsx
<span className="mono">session-id</span>
<span className="text-muted">optional</span>
<button className="btn btn--primary">Save</button>
```

### Dark Mode

Supported via `[data-theme="dark"]` attribute:

```jsx
document.documentElement.setAttribute('data-theme', 'dark');
```

**Implementation:**
- CSS variables redefine in dark mode media query
- `useTheme` hook manages toggle
- Persists to localStorage

### Component Scoping

Most styles are **global** with specific class names. Only `tips.module.css` uses CSS Modules.

**Rationale:**
- Small codebase doesn't need complex scoping
- Consistent naming prevents conflicts
- Easier to share styles across components

---

## API Integration

### API Layer

All API calls go through `lib/api.ts`:

```js
export async function getSessions() {
  const response = await fetch('/api/sessions');
  if (!response.ok) throw new Error('Failed to fetch sessions');
  return response.json();
}
```

**Patterns:**
- RESTful endpoints
- JSON request/response
- Error throwing with `Error` objects
- Consistent error checking

### Data Fetching in Components

**Current Pattern:**

```jsx
useEffect(() => {
  let active = true;

  const load = async () => {
    try {
      const data = await getSessions();
      if (!active) return;  // Ignore if unmounted
      setSessions(data);
    } catch (err) {
      setError(err.message);
    }
  };

  load();
  return () => { active = false; };  // Cleanup
}, []);
```

**Improvements Planned:**
- AbortController for cancellation
- React Query for caching/refetching

### WebSocket Integration

Terminal streaming uses `lib/terminalStream.ts` class:

```js
class TerminalStream {
  connect() {
    const ws = new WebSocket(`ws://${host}/ws/terminal/${sessionId}`);
    ws.onmessage = (event) => this.handleOutput(event.data);
  }
}
```

**Features:**
- Bi-directional communication
- Auto-reconnect on disconnect
- Terminal scaling
- User input handling

---

## Routing

### Route Structure

```jsx
<Routes>
  <Route element={<AppShell />}>
    <Route path="/" element={<SessionsPage />} />
    <Route path="/sessions" element={<SessionsPage />} />
    <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
    <Route path="/spawn" element={<SpawnPage />} />
    {/* ... */}
  </Route>
</Routes>
```

### Navigation Patterns

**Preferred:** React Router's `Link` component

```jsx
<Link to="/sessions/abc123">View Session</Link>
```

**Programmatic:** `useNavigate` hook

```jsx
const navigate = useNavigate();
navigate('/sessions/abc123');
```

**External only:** Direct assignment

```jsx
// Only for external URLs
<a href="https://github.com/repo">External</a>
```

### URL Design

- **Idempotent:** Reloading shows same content
- **Bookmarkable:** All views accessible via URL
- **Query strings for filters:** `?s=running&r=repo-name`
- **RESTful IDs:** `/sessions/{id}` not `/session?id={id}`

---

## Decisions & Rationale

This section documents significant architectural decisions and the reasoning behind them.

### Decision 1: TypeScript Adoption

**Status:** Implemented — TypeScript migration complete

**Rationale:**
- Type safety prevents bugs and improves code quality
- Better IDE support with autocomplete and refactoring
- Industry standard for React applications
- Self-documenting code with type definitions
- Easier onboarding for new developers

**Implementation:**
- All `.jsx` files migrated to `.tsx`
- All `.js` files migrated to `.ts`
- Type definitions added for API responses and component props
- tsconfig.json configured for Vite compatibility

---

### Decision 2: React Context over State Management Library

**Decision:** Use React Context for global state instead of Redux/Zustand

**Rationale:**
- App complexity doesn't warrant full state management library
- Context is built into React
- Less boilerplate
- Easier to understand for new developers

**Trade-offs:**
- More re-renders (mitigated by splitting contexts)
- No dev tools (less important with simple state)
- Manual optimization with `useMemo`/`useCallback`

**Future:** Consider React Query for server state, which replaces much of what Redux would do.

---

### Decision 3: Manual Polling over Real-time Everything

**Decision:** Use REST API + manual polling for most data, WebSocket only for terminal output

**Rationale:**
- WebSocket connections are expensive
- Most data doesn't need true real-time updates
- Polling is simpler and more reliable
- Terminal is the only truly real-time feature

**Trade-offs:**
- 5-second delay on updates (acceptable for dashboard use case)
- Unnecessary requests when no data changed

**Future:** React Query with smart refetching would optimize this.

---

### Decision 4: No UI Component Library

**Decision:** Build custom components instead of using Material-UI, Ant Design, etc.

**Rationale:**
- Total control over look and feel
- Smaller bundle size
- Learn React deeply by building from scratch
- No lock-in to library decisions
- App-specific UI needs (terminal, workspace trees)

**Trade-offs:**
- More code to maintain
- Reinventing some wheels
- Less polished out-of-the-box

**Decision Made:** Build custom components that match specific needs.

---

### Decision 5: CSS over CSS-in-JS

**Decision:** Use traditional CSS with design tokens, not styled-components or Emotion

**Rationale:**
- Simpler build pipeline
- Better performance (no runtime CSS generation)
- Easier to debug
- Standard CSS is powerful enough
- Dark mode via media query is simple

**Trade-offs:**
- No prop-driven styles
- More global namespace concerns
- No style colocation with components

**Decision Made:** CSS with BEM-inspired naming and design tokens.

---

### Decision 6: SPA over Server-Side Rendering

**Decision:** Build as SPA with Vite, not Next.js or Remix

**Rationale:**
- App is primarily used by single user (admin)
- SEO not a concern (internal tool)
- Simpler deployment (static files)
- Better development experience (Vite HMR)
- Daemon is Go, not Node (so no Next.js integration benefit)

**Trade-offs:**
- Slower initial page load
- No progressive enhancement
- Browser must support JS

**Decision Made:** SPA with Vite is appropriate for this use case.

---

### Decision 7: Class-based TerminalStream over React Component

**Decision:** Use class-based wrapper around xterm.js, not React component

**Rationale:**
- xterm.js has imperative API
- Complex lifecycle (connect, disconnect, resize)
- Easier to encapsulate in class
- React integration via ref

**Trade-offs:**
- Mixes paradigms (classes vs hooks)
- Less "React-idiomatic"

**Future:** Could wrap in React hook for cleaner integration.

---

## Anti-Patterns to Avoid

### 1. Module-Level State

**❌ Don't do this:**

```jsx
let savedState = false;  // Persists unpredictably

export default function Component() {
  const [state, setState] = useState(savedState);
  // ...
}
```

**✅ Do this instead:**

```jsx
export default function Component() {
  const [state, setState] = useState(() => {
    const saved = localStorage.getItem('key');
    return saved ? JSON.parse(saved) : false;
  });

  useEffect(() => {
    localStorage.setItem('key', JSON.stringify(state));
  }, [state]);
}
```

---

### 2. State Mutation

**❌ Don't do this:**

```jsx
setState((current) => {
  const next = { ...current };
  items.forEach((item) => {
    next[item.id] = item;  // Mutation during iteration
  });
  return next;
});
```

**✅ Do this instead:**

```jsx
setState((current) => ({
  ...current,
  ...Object.fromEntries(items.map(item => [item.id, item]))
}));
```

---

### 3. Direct DOM Manipulation

**❌ Don't do this:**

```jsx
document.getElementById('my-element').textContent = 'Hello';
```

**✅ Use React state:**

```jsx
const [text, setText] = useState('Hello');
<div>{text}</div>
```

**Exception:** Third-party libraries like xterm.js that require DOM elements.

---

### 4. useEffect for Derived State

**❌ Don't do this:**

```jsx
const [items, setItems] = useState([]);
const [filtered, setFiltered] = useState([]);

useEffect(() => {
  setFiltered(items.filter(item => item.active));
}, [items]);
```

**✅ Compute during render:**

```jsx
const filtered = items.filter(item => item.active);
```

---

### 5. Mixed Navigation Patterns

**❌ Don't do this:**

```jsx
// Sometimes Link, sometimes navigate, sometimes window.location
<Link to="/">Home</Link>
<button onClick={() => navigate('/about')}>About</button>
<button onClick={() => window.location.href = '/contact'}>Contact</button>
```

**✅ Use consistent pattern:**

```jsx
// Declarative: Link
<Link to="/">Home</Link>

// Programmatic: useNavigate
const navigate = useNavigate();
<button onClick={() => navigate('/about')}>About</button>

// External only: anchor tag
<a href="https://external.com">External</a>
```

---

### 6. Ignoring Cleanup in useEffect

**❌ Don't do this:**

```jsx
useEffect(() => {
  const interval = setInterval(() => load(), 1000);
  // Missing cleanup - will leak memory
}, []);
```

**✅ Always cleanup:**

```jsx
useEffect(() => {
  const interval = setInterval(() => load(), 1000);
  return () => clearInterval(interval);
}, []);
```

---

### 7. Type Safety with TypeScript

**✅ Current state:** TypeScript provides compile-time type checking

All components have properly typed props, preventing entire classes of runtime errors.

---

## Future Roadmap

### Short Term (High Priority)

- [x] TypeScript adoption
- [ ] Error boundaries
- [ ] Fix state mutation patterns
- [ ] Standardize navigation

### Medium Term

- [ ] Request cancellation with AbortController
- [ ] Comprehensive test coverage
- [ ] Consistent loading/error states
- [ ] Accessibility audit and improvements

### Long Term (Optional)

- [ ] React Query for server state management
- [ ] Storybook for component development
- [ ] Performance monitoring
- [ ] Service Worker for offline support

---

## Contributing

When making changes to the frontend:

1. **Read this document** — Understand the patterns and conventions
2. **Follow existing patterns** — Match the code style and architecture
3. **Document new decisions** — Add to the "Decisions & Rationale" section
4. **Update this document** — Keep it in sync with codebase changes

---

## References

- [React Documentation](https://react.dev/)
- [React Router Documentation](https://reactrouter.com/)
- [Vite Documentation](https://vitejs.dev/)
- [xterm.js Documentation](https://xtermjs.org/)
- [docs/web.md](../web.md) — Web dashboard UX and design system
