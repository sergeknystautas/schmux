# Interactive Demo Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Build a static interactive demo hosted on GitHub Pages that lets users try schmux without installing it, using the real dashboard components with mock data and guided spotlight tours.

**Architecture:** A thin transport abstraction replaces direct WebSocket/fetch calls in the dashboard. The marketing website gains a `/demo/` route that mounts the real dashboard with a mock transport and spotlight tour overlay. Two scenarios (workspaces, spawn) guide users through core workflows.

**Tech Stack:** React 18, React Router 6, xterm.js 5, Vite (multi-page), TypeScript, CSS clip-path for spotlight

**Design doc:** `docs/specs/2026-02-27-interactive-demo-design.md`

---

## Phase 1: Transport Abstraction

### Task 1: Create transport module

**Files:**

- Create: `assets/dashboard/src/lib/transport.ts`
- Test: `assets/dashboard/src/lib/transport.test.ts`

**Step 1: Write the test**

```typescript
// assets/dashboard/src/lib/transport.test.ts
import { describe, it, expect } from 'vitest';
import { transport, setTransport, liveTransport, type Transport } from './transport';

describe('transport', () => {
  it('defaults to liveTransport', () => {
    expect(transport).toBe(liveTransport);
  });

  it('setTransport swaps the active transport', () => {
    const mock: Transport = {
      createWebSocket: () => ({}) as WebSocket,
      fetch: () => Promise.resolve(new Response()),
    };
    setTransport(mock);
    expect(transport).toBe(mock);
    // Restore
    setTransport(liveTransport);
    expect(transport).toBe(liveTransport);
  });

  it('liveTransport.fetch delegates to window.fetch', async () => {
    // Just verify the shape — actual fetch is tested elsewhere
    expect(typeof liveTransport.fetch).toBe('function');
    expect(typeof liveTransport.createWebSocket).toBe('function');
  });
});
```

**Step 2: Run the test, verify it fails**

```bash
cd assets/dashboard && npx vitest run src/lib/transport.test.ts
```

Expected: FAIL — `Cannot find module './transport'`

**Step 3: Implement the transport module**

```typescript
// assets/dashboard/src/lib/transport.ts

export interface Transport {
  createWebSocket(url: string): WebSocket;
  fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response>;
}

export const liveTransport: Transport = {
  createWebSocket: (url: string) => new WebSocket(url),
  fetch: (input: RequestInfo | URL, init?: RequestInit) => window.fetch(input, init),
};

// Module-level singleton. ESM named exports are live bindings,
// so consumers importing `transport` see updates after setTransport().
export let transport: Transport = liveTransport;

export function setTransport(t: Transport) {
  transport = t;
}
```

**Step 4: Run the test, verify it passes**

```bash
cd assets/dashboard && npx vitest run src/lib/transport.test.ts
```

Expected: PASS

**Step 5: Run full test suite to verify no regressions**

```bash
./test.sh --quick
```

Expected: All existing tests pass

**Step 6: Commit**

Use `/commit`

---

### Task 2: Refactor useSessionsWebSocket to use transport

**Files:**

- Modify: `assets/dashboard/src/hooks/useSessionsWebSocket.ts` (line 205)

**Step 1: Add transport import and replace WebSocket construction**

At the top of `useSessionsWebSocket.ts`, add:

```typescript
import { transport } from '../lib/transport';
```

At line 205, replace:

```typescript
// Before
const ws = new WebSocket(`${protocol}//${window.location.host}/ws/dashboard`);

// After
const ws = transport.createWebSocket(`${protocol}//${window.location.host}/ws/dashboard`);
```

**Step 2: Run the useSessionsWebSocket tests**

```bash
cd assets/dashboard && npx vitest run src/hooks/useSessionsWebSocket.test.ts
```

Expected: PASS — tests already stub `WebSocket` globally via `vi.stubGlobal('WebSocket', MockWebSocket)`, and `liveTransport.createWebSocket` delegates to the global `WebSocket` constructor, so the mock still intercepts.

**Step 3: Run full test suite**

```bash
./test.sh --quick
```

Expected: All tests pass

**Step 4: Commit**

Use `/commit`

---

### Task 3: Refactor terminalStream to use transport

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.ts` (lines 25, 429, 558)

**Step 1: Add transport import and replace WebSocket + fetch calls**

At the top of `terminalStream.ts`, add:

```typescript
import { transport } from './transport';
```

Replace the WebSocket construction at line 429:

```typescript
// Before
this.ws = new WebSocket(wsUrl);

// After
this.ws = transport.createWebSocket(wsUrl);
```

Replace the fetch call in `pasteImageToSession` (line 25):

```typescript
// Before
const resp = await fetch('/api/clipboard-paste', {

// After
const resp = await transport.fetch('/api/clipboard-paste', {
```

Replace the fetch call in `enableDiagnostics` (~line 558):

```typescript
// Before
fetch('/api/dev/diagnostic-append', {

// After
transport.fetch('/api/dev/diagnostic-append', {
```

**Step 2: Run the terminalStream tests**

```bash
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

Expected: PASS — xterm is mocked at module level, and WebSocket is not directly tested in these unit tests.

**Step 3: Run full test suite**

```bash
./test.sh --quick
```

Expected: All tests pass

**Step 4: Commit**

Use `/commit`

---

### Task 4: Refactor api.ts to use transport

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts` (~56 fetch calls)

**Step 1: Add transport import and create local wrapper**

At the top of `api.ts`, add:

```typescript
import { transport } from './transport';
```

Add a local wrapper function near the top (after imports, before the first exported function):

```typescript
// All fetch calls in this module route through the active transport.
// This enables the demo shell to intercept API calls with mock responses.
function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return transport.fetch(input, init);
}
```

**Step 2: Replace all `fetch(` calls with `apiFetch(`**

Search and replace within `api.ts` only:

- Replace `await fetch(` → `await apiFetch(`
- Replace `return fetch(` → `return apiFetch(`
- Replace standalone `fetch(` (e.g., fire-and-forget in `enableDiagnostics`) → `apiFetch(`

There are ~56 occurrences. Do NOT replace `fetchFn` or any other variable containing "fetch" — only bare `fetch(` calls.

Verify the replacement count matches (should be ~56 replacements).

**Step 3: Run full test suite**

```bash
./test.sh --quick
```

Expected: All tests pass. API tests mock at the `fetch` level via `vi.stubGlobal` or `vi.mock`, and `liveTransport.fetch` delegates to `window.fetch`, so existing mocks still work.

**Step 4: Commit**

Use `/commit`

---

### Task 5: Add data-tour attributes to dashboard components

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx` (4 attributes)
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx` (3 attributes)
- Modify: `assets/dashboard/src/routes/SpawnPage.tsx` (4 attributes)
- Modify: `assets/dashboard/src/components/SessionTabs.tsx` (2 attributes)

These are purely additive — no behavior change.

**Step 1: Add attributes to AppShell.tsx**

Add `data-tour` attributes to these elements:

| Line | Element                                                            | Attribute                                                            |
| ---- | ------------------------------------------------------------------ | -------------------------------------------------------------------- |
| ~672 | `<button className="btn nav-spawn-btn"`                            | `data-tour="sidebar-add-workspace"`                                  |
| ~688 | `<div className="nav-workspaces">`                                 | `data-tour="sidebar-workspace-list"`                                 |
| ~908 | First `<div className={`nav-session...`}` in the sessions `.map()` | Add `data-tour={index === 0 ? 'sidebar-session' : undefined}`        |
| ~958 | First `<span className="nav-session__activity">`                   | Add `data-tour={index === 0 ? 'sidebar-session-status' : undefined}` |

**Step 2: Add attributes to SessionDetailPage.tsx**

| Line | Element                                             | Attribute                            |
| ---- | --------------------------------------------------- | ------------------------------------ |
| ~667 | `<div className="log-viewer">`                      | `data-tour="terminal-log-viewer"`    |
| ~816 | `<div id="terminal" className="log-viewer__output"` | `data-tour="terminal-viewport"`      |
| ~840 | `<aside className="session-detail__sidebar"`        | `data-tour="session-detail-sidebar"` |

**Step 3: Add attributes to SpawnPage.tsx**

| Line  | Element                                  | Attribute                          |
| ----- | ---------------------------------------- | ---------------------------------- |
| ~939  | `<div className="spawn-content">`        | `data-tour="spawn-form"`           |
| ~1097 | `<select id="repo"`                      | `data-tour="spawn-repo-select"`    |
| ~1081 | `<select id="persona-select"`            | `data-tour="spawn-persona-select"` |
| ~1475 | `<button ... data-testid="spawn-submit"` | `data-tour="spawn-submit"`         |

**Step 4: Add attributes to SessionTabs.tsx**

| Line | Element                                      | Attribute                     |
| ---- | -------------------------------------------- | ----------------------------- |
| ~545 | `<button ... aria-label="Spawn new session"` | `data-tour="session-tab-add"` |
| ~626 | `<div className="session-tabs">`             | `data-tour="session-tabs"`    |

**Step 5: Run full test suite**

```bash
./test.sh --quick
```

Expected: All tests pass — `data-tour` attributes are inert.

**Step 6: Commit**

Use `/commit`

---

## Phase 2: Tour System

### Task 6: Create tour types and TourProvider

**Files:**

- Create: `assets/dashboard/website/src/demo/tour/types.ts`
- Create: `assets/dashboard/website/src/demo/tour/TourProvider.tsx`
- Create: `assets/dashboard/website/src/demo/tour/Spotlight.tsx`
- Create: `assets/dashboard/website/src/demo/tour/Tooltip.tsx`
- Create: `assets/dashboard/website/src/demo/tour/tour.css`

**Step 1: Create tour types**

```typescript
// assets/dashboard/website/src/demo/tour/types.ts

export interface TourStep {
  /** CSS selector for the target element (prefer data-tour attributes) */
  target: string;
  /** Step title shown in tooltip */
  title: string;
  /** Step description shown in tooltip */
  body: string;
  /** Tooltip placement relative to target */
  placement: 'top' | 'bottom' | 'left' | 'right';
  /** How to advance: 'click' = user clicks target, 'next' = user clicks Next button */
  advanceOn: 'click' | 'next';
  /** Inject state changes before this step renders */
  beforeStep?: () => void;
  /** Update state after this step completes */
  afterStep?: () => void;
  /** Route to navigate to before showing this step */
  route?: string;
}

export interface TourScenario {
  id: string;
  title: string;
  description: string;
  /** Initial route for the dashboard */
  initialRoute: string;
  /** Tour steps in order */
  steps: TourStep[];
}

export interface TourContextValue {
  /** Current scenario being played, or null */
  scenario: TourScenario | null;
  /** Current step index (0-based) */
  currentStep: number;
  /** Total steps */
  totalSteps: number;
  /** Whether the tour is active */
  active: boolean;
  /** Advance to the next step */
  next: () => void;
  /** Go back to the previous step */
  prev: () => void;
  /** End the tour */
  end: () => void;
}
```

**Step 2: Create the Spotlight component**

The Spotlight renders a dark overlay with a rectangular cutout around the target element using CSS `clip-path: path()`. It observes the target's position with `ResizeObserver` and `MutationObserver`.

```tsx
// assets/dashboard/website/src/demo/tour/Spotlight.tsx
import { useEffect, useState, useCallback } from 'react';

interface SpotlightProps {
  target: string; // CSS selector
  active: boolean;
  padding?: number;
}

export default function Spotlight({ target, active, padding = 8 }: SpotlightProps) {
  const [rect, setRect] = useState<DOMRect | null>(null);

  const updateRect = useCallback(() => {
    const el = document.querySelector(target);
    if (el) setRect(el.getBoundingClientRect());
  }, [target]);

  useEffect(() => {
    if (!active) return;
    updateRect();
    // Re-measure on scroll, resize, and DOM mutations
    window.addEventListener('scroll', updateRect, true);
    window.addEventListener('resize', updateRect);
    const observer = new MutationObserver(updateRect);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true });
    return () => {
      window.removeEventListener('scroll', updateRect, true);
      window.removeEventListener('resize', updateRect);
      observer.disconnect();
    };
  }, [active, updateRect]);

  if (!active || !rect) return null;

  const x = rect.x - padding;
  const y = rect.y - padding;
  const w = rect.width + padding * 2;
  const h = rect.height + padding * 2;
  const r = 6; // border radius

  // SVG clip-path: full viewport minus rounded rect cutout
  const vw = window.innerWidth;
  const vh = window.innerHeight;

  return (
    <div className="tour-spotlight" aria-hidden="true">
      <svg width={vw} height={vh}>
        <defs>
          <mask id="tour-mask">
            <rect width={vw} height={vh} fill="white" />
            <rect x={x} y={y} width={w} height={h} rx={r} ry={r} fill="black" />
          </mask>
        </defs>
        <rect width={vw} height={vh} fill="rgba(0,0,0,0.6)" mask="url(#tour-mask)" />
      </svg>
    </div>
  );
}
```

**Step 3: Create the Tooltip component**

```tsx
// assets/dashboard/website/src/demo/tour/Tooltip.tsx
import type { TourStep } from './types';

interface TooltipProps {
  step: TourStep;
  currentStep: number;
  totalSteps: number;
  onNext: () => void;
  onPrev: () => void;
  onEnd: () => void;
}

export default function Tooltip({
  step,
  currentStep,
  totalSteps,
  onNext,
  onPrev,
  onEnd,
}: TooltipProps) {
  const targetEl = document.querySelector(step.target);
  if (!targetEl) return null;

  const rect = targetEl.getBoundingClientRect();
  const style = computePosition(rect, step.placement);

  return (
    <div className="tour-tooltip" style={style} role="dialog" aria-label={step.title}>
      <div className="tour-tooltip__header">
        <span className="tour-tooltip__title">{step.title}</span>
        <button className="tour-tooltip__close" onClick={onEnd} aria-label="Close tour">
          &times;
        </button>
      </div>
      <p className="tour-tooltip__body">{step.body}</p>
      <div className="tour-tooltip__footer">
        <span className="tour-tooltip__progress">
          {currentStep + 1} / {totalSteps}
        </span>
        <div className="tour-tooltip__actions">
          {currentStep > 0 && (
            <button className="tour-tooltip__btn tour-tooltip__btn--back" onClick={onPrev}>
              Back
            </button>
          )}
          {step.advanceOn === 'next' && (
            <button className="tour-tooltip__btn tour-tooltip__btn--next" onClick={onNext}>
              {currentStep === totalSteps - 1 ? 'Done' : 'Next'}
            </button>
          )}
          {step.advanceOn === 'click' && (
            <span className="tour-tooltip__hint">Click the highlighted element</span>
          )}
        </div>
      </div>
    </div>
  );
}

function computePosition(rect: DOMRect, placement: TourStep['placement']): React.CSSProperties {
  const gap = 12;
  switch (placement) {
    case 'bottom':
      return { position: 'fixed', top: rect.bottom + gap, left: rect.left, maxWidth: 320 };
    case 'top':
      return {
        position: 'fixed',
        bottom: window.innerHeight - rect.top + gap,
        left: rect.left,
        maxWidth: 320,
      };
    case 'right':
      return { position: 'fixed', top: rect.top, left: rect.right + gap, maxWidth: 320 };
    case 'left':
      return {
        position: 'fixed',
        top: rect.top,
        right: window.innerWidth - rect.left + gap,
        maxWidth: 320,
      };
  }
}
```

**Step 4: Create the TourProvider**

```tsx
// assets/dashboard/website/src/demo/tour/TourProvider.tsx
import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import Spotlight from './Spotlight';
import Tooltip from './Tooltip';
import type { TourScenario, TourContextValue } from './types';
import './tour.css';

const TourContext = createContext<TourContextValue | null>(null);

export function useTour(): TourContextValue {
  const ctx = useContext(TourContext);
  if (!ctx) throw new Error('useTour must be used within TourProvider');
  return ctx;
}

interface TourProviderProps {
  scenario: TourScenario;
  children: React.ReactNode;
}

export default function TourProvider({ scenario, children }: TourProviderProps) {
  const [currentStep, setCurrentStep] = useState(0);
  const [active, setActive] = useState(true);
  const navigate = useNavigate();

  const step = scenario.steps[currentStep];

  // Run beforeStep hook when step changes
  useEffect(() => {
    if (!active || !step) return;
    step.beforeStep?.();
    if (step.route) navigate(step.route);
  }, [currentStep, active]); // eslint-disable-line react-hooks/exhaustive-deps

  // For 'click' advance mode, listen for clicks on the target
  useEffect(() => {
    if (!active || !step || step.advanceOn !== 'click') return;
    const handler = () => {
      step.afterStep?.();
      if (currentStep < scenario.steps.length - 1) {
        setCurrentStep((s) => s + 1);
      } else {
        setActive(false);
      }
    };
    const el = document.querySelector(step.target);
    if (el) el.addEventListener('click', handler, { once: true });
    return () => {
      el?.removeEventListener('click', handler);
    };
  }, [active, step, currentStep, scenario.steps.length]);

  const next = useCallback(() => {
    step?.afterStep?.();
    if (currentStep < scenario.steps.length - 1) {
      setCurrentStep((s) => s + 1);
    } else {
      setActive(false);
    }
  }, [currentStep, scenario.steps.length, step]);

  const prev = useCallback(() => {
    if (currentStep > 0) setCurrentStep((s) => s - 1);
  }, [currentStep]);

  const end = useCallback(() => setActive(false), []);

  const value: TourContextValue = {
    scenario,
    currentStep,
    totalSteps: scenario.steps.length,
    active,
    next,
    prev,
    end,
  };

  return (
    <TourContext.Provider value={value}>
      {children}
      {active && step && (
        <>
          <Spotlight target={step.target} active={active} />
          <Tooltip
            step={step}
            currentStep={currentStep}
            totalSteps={scenario.steps.length}
            onNext={next}
            onPrev={prev}
            onEnd={end}
          />
        </>
      )}
    </TourContext.Provider>
  );
}
```

**Step 5: Create tour styles**

```css
/* assets/dashboard/website/src/demo/tour/tour.css */

.tour-spotlight {
  position: fixed;
  inset: 0;
  z-index: 9998;
  pointer-events: none;
  transition: opacity 0.2s ease;
}

.tour-spotlight svg {
  display: block;
}

.tour-tooltip {
  position: fixed;
  z-index: 9999;
  background: var(--bg-primary, #1a1a2e);
  border: 1px solid var(--border-color, #333);
  border-radius: 8px;
  padding: 16px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
  animation: tour-tooltip-enter 0.2s ease;
  font-family: 'DM Sans', system-ui, sans-serif;
}

@keyframes tour-tooltip-enter {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.tour-tooltip__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.tour-tooltip__title {
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #e0e0e0);
}

.tour-tooltip__close {
  background: none;
  border: none;
  color: var(--text-secondary, #888);
  cursor: pointer;
  font-size: 18px;
  padding: 0 4px;
}

.tour-tooltip__body {
  font-size: 13px;
  color: var(--text-secondary, #b0b0b0);
  line-height: 1.5;
  margin: 0 0 12px;
}

.tour-tooltip__footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.tour-tooltip__progress {
  font-size: 12px;
  color: var(--text-tertiary, #666);
  font-family: 'IBM Plex Mono', monospace;
}

.tour-tooltip__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tour-tooltip__btn {
  padding: 6px 14px;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  border: none;
  font-weight: 500;
}

.tour-tooltip__btn--back {
  background: transparent;
  color: var(--text-secondary, #888);
}

.tour-tooltip__btn--next {
  background: var(--accent-color, #6366f1);
  color: white;
}

.tour-tooltip__hint {
  font-size: 12px;
  color: var(--accent-color, #6366f1);
  font-style: italic;
}
```

**Step 6: Verify the website still builds**

```bash
cd assets/dashboard/website && npx vite build
```

Expected: Build succeeds (new files exist but aren't imported from the main website entry point yet).

**Step 7: Commit**

Use `/commit`

---

## Phase 3: Mock Transport + Terminal Playback

### Task 7: Create mock data fixtures

**Files:**

- Create: `assets/dashboard/website/src/demo/transport/mockData.ts`

**Step 1: Create workspace and session fixtures**

```typescript
// assets/dashboard/website/src/demo/transport/mockData.ts
import type { WorkspaceResponse, SessionResponse } from '@dashboard/lib/types';
import type { ConfigResponse } from '@dashboard/lib/types.generated';

export function createDemoSessions(): SessionResponse[] {
  return [
    {
      id: 'demo-sess-1',
      target: 'claude',
      branch: 'feature/user-auth',
      created_at: new Date(Date.now() - 15 * 60 * 1000).toISOString(),
      running: true,
      attach_cmd: 'schmux attach demo-sess-1',
      nickname: 'auth-impl',
      persona_name: 'Backend Engineer',
      persona_icon: '⚙️',
      persona_color: '#6366f1',
    },
    {
      id: 'demo-sess-2',
      target: 'claude',
      branch: 'feature/user-auth',
      created_at: new Date(Date.now() - 45 * 60 * 1000).toISOString(),
      last_output_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
      running: true,
      attach_cmd: 'schmux attach demo-sess-2',
      nickname: 'test-writer',
      persona_name: 'QA Engineer',
      persona_icon: '🧪',
      persona_color: '#10b981',
    },
  ];
}

export function createDemoWorkspaces(): WorkspaceResponse[] {
  const sessions = createDemoSessions();
  return [
    {
      id: 'demo-ws-1',
      repo: 'https://github.com/acme/webapp.git',
      repo_name: 'acme/webapp',
      branch: 'feature/user-auth',
      path: '/home/dev/workspaces/webapp-001',
      session_count: sessions.length,
      sessions,
      git_ahead: 3,
      git_behind: 0,
      git_lines_added: 147,
      git_lines_removed: 23,
      git_files_changed: 8,
      default_branch: 'main',
    },
    {
      id: 'demo-ws-2',
      repo: 'https://github.com/acme/api-server.git',
      repo_name: 'acme/api-server',
      branch: 'fix/rate-limiting',
      path: '/home/dev/workspaces/api-server-001',
      session_count: 1,
      sessions: [
        {
          id: 'demo-sess-3',
          target: 'codex',
          branch: 'fix/rate-limiting',
          created_at: new Date(Date.now() - 30 * 60 * 1000).toISOString(),
          running: false,
          attach_cmd: 'schmux attach demo-sess-3',
          nickname: 'rate-limiter',
        },
      ],
      git_ahead: 1,
      git_behind: 2,
      git_lines_added: 42,
      git_lines_removed: 15,
      git_files_changed: 3,
      default_branch: 'main',
    },
  ];
}

export function createDemoConfig(): ConfigResponse {
  return {
    version: 2,
    repos: [
      { url: 'https://github.com/acme/webapp.git' },
      { url: 'https://github.com/acme/api-server.git' },
    ],
    agents: [
      { name: 'claude', command: 'claude --dangerously-skip-permissions' },
      { name: 'codex', command: 'codex --full-auto' },
    ],
    workspace: {
      base_dir: '/home/dev/workspaces',
    },
    floor_manager: { enabled: false },
    notifications: {},
    remote_access: { enabled: false },
  } as ConfigResponse;
}

/** Workspace state after a spawn completes (for the spawn scenario) */
export function createPostSpawnWorkspaces(): WorkspaceResponse[] {
  const base = createDemoWorkspaces();
  // Add new session to first workspace
  base[0].sessions = [
    ...base[0].sessions,
    {
      id: 'demo-sess-new',
      target: 'claude',
      branch: 'feature/user-auth',
      created_at: new Date().toISOString(),
      running: true,
      attach_cmd: 'schmux attach demo-sess-new',
      nickname: 'new-agent',
      persona_name: 'Frontend Developer',
      persona_icon: '🎨',
      persona_color: '#f59e0b',
    },
  ];
  base[0].session_count = base[0].sessions.length;
  return base;
}
```

**Step 2: Commit**

Use `/commit`

---

### Task 8: Create terminal recordings

**Files:**

- Create: `assets/dashboard/website/src/demo/recordings/workspaces.ts`
- Create: `assets/dashboard/website/src/demo/recordings/spawn.ts`

**Step 1: Create the recording type and playback engine**

```typescript
// assets/dashboard/website/src/demo/recordings/types.ts

export interface TerminalFrame {
  /** Milliseconds to wait before displaying this frame (relative to previous frame) */
  delay: number;
  /** Raw text to write to xterm.js, may contain ANSI escape codes */
  data: string;
}

export interface TerminalRecording {
  sessionId: string;
  frames: TerminalFrame[];
}
```

**Step 2: Create workspaces terminal recording**

```typescript
// assets/dashboard/website/src/demo/recordings/workspaces.ts
import type { TerminalRecording } from './types';

const CSI = '\x1b[';
const CYAN = `${CSI}36m`;
const GREEN = `${CSI}32m`;
const DIM = `${CSI}2m`;
const BOLD = `${CSI}1m`;
const RESET = `${CSI}0m`;
const YELLOW = `${CSI}33m`;

export const authImplRecording: TerminalRecording = {
  sessionId: 'demo-sess-1',
  frames: [
    {
      delay: 0,
      data: `${DIM}$ claude --prompt "Implement user authentication with JWT"${RESET}\n`,
    },
    { delay: 600, data: `\n${CYAN}●${RESET} Analyzing codebase structure...\n` },
    { delay: 1200, data: `${DIM}  Reading src/middleware/...${RESET}\n` },
    { delay: 800, data: `${DIM}  Reading src/models/user.ts...${RESET}\n` },
    { delay: 1000, data: `${DIM}  Reading src/routes/...${RESET}\n` },
    { delay: 1500, data: `\n${CYAN}●${RESET} Creating auth middleware...\n` },
    { delay: 2000, data: `${GREEN}✓${RESET} Created ${BOLD}src/middleware/auth.ts${RESET}\n` },
    { delay: 800, data: `${GREEN}✓${RESET} Created ${BOLD}src/lib/jwt.ts${RESET}\n` },
    { delay: 1200, data: `\n${CYAN}●${RESET} Adding login and register routes...\n` },
    { delay: 2500, data: `${GREEN}✓${RESET} Created ${BOLD}src/routes/auth.ts${RESET}\n` },
    { delay: 1000, data: `${GREEN}✓${RESET} Updated ${BOLD}src/routes/index.ts${RESET}\n` },
    { delay: 1500, data: `\n${CYAN}●${RESET} Writing tests...\n` },
    {
      delay: 3000,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/middleware/auth.test.ts${RESET} ${DIM}(12 tests)${RESET}\n`,
    },
    {
      delay: 2000,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/routes/auth.test.ts${RESET} ${DIM}(8 tests)${RESET}\n`,
    },
    { delay: 1000, data: `\n${YELLOW}Running tests...${RESET}\n` },
    { delay: 2000, data: `${GREEN}✓${RESET} 20/20 tests passed\n` },
    {
      delay: 500,
      data: `\n${GREEN}${BOLD}Done.${RESET} Authentication system implemented with JWT.\n`,
    },
  ],
};

export const testWriterRecording: TerminalRecording = {
  sessionId: 'demo-sess-2',
  frames: [
    {
      delay: 0,
      data: `${DIM}$ claude --prompt "Write integration tests for the auth endpoints"${RESET}\n`,
    },
    { delay: 500, data: `\n${CYAN}●${RESET} Reading existing test patterns...\n` },
    { delay: 1200, data: `${DIM}  Found vitest config, supertest setup${RESET}\n` },
    { delay: 1000, data: `\n${CYAN}●${RESET} Writing integration tests...\n` },
    {
      delay: 2500,
      data: `${GREEN}✓${RESET} Created ${BOLD}test/integration/auth.test.ts${RESET}\n`,
    },
    { delay: 1500, data: `${DIM}  • POST /auth/register - creates user${RESET}\n` },
    { delay: 300, data: `${DIM}  • POST /auth/login - returns JWT${RESET}\n` },
    { delay: 300, data: `${DIM}  • GET /auth/me - requires valid token${RESET}\n` },
    { delay: 300, data: `${DIM}  • POST /auth/refresh - extends session${RESET}\n` },
    { delay: 1000, data: `\n${YELLOW}Running integration tests...${RESET}\n` },
    { delay: 3000, data: `${GREEN}✓${RESET} 4/4 integration tests passed\n` },
    { delay: 500, data: `\n${GREEN}${BOLD}Done.${RESET}\n` },
  ],
};
```

**Step 3: Create spawn terminal recording**

```typescript
// assets/dashboard/website/src/demo/recordings/spawn.ts
import type { TerminalRecording } from './types';

const CSI = '\x1b[';
const CYAN = `${CSI}36m`;
const GREEN = `${CSI}32m`;
const DIM = `${CSI}2m`;
const BOLD = `${CSI}1m`;
const RESET = `${CSI}0m`;

export const newAgentRecording: TerminalRecording = {
  sessionId: 'demo-sess-new',
  frames: [
    {
      delay: 0,
      data: `${DIM}$ claude --prompt "Build a responsive dashboard component"${RESET}\n`,
    },
    { delay: 800, data: `\n${CYAN}●${RESET} Scanning project structure...\n` },
    { delay: 1200, data: `${DIM}  Found React 18, Tailwind CSS, TypeScript${RESET}\n` },
    { delay: 900, data: `${DIM}  Reading existing component patterns...${RESET}\n` },
    { delay: 1500, data: `\n${CYAN}●${RESET} Creating dashboard component...\n` },
    {
      delay: 2000,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/components/Dashboard.tsx${RESET}\n`,
    },
    { delay: 1200, data: `${GREEN}✓${RESET} Created ${BOLD}src/components/StatCard.tsx${RESET}\n` },
    { delay: 1000, data: `${GREEN}✓${RESET} Created ${BOLD}src/components/Chart.tsx${RESET}\n` },
    { delay: 800, data: `\n${CYAN}●${RESET} Adding responsive breakpoints...\n` },
    { delay: 1500, data: `${GREEN}✓${RESET} Updated ${BOLD}src/styles/dashboard.css${RESET}\n` },
    { delay: 1000, data: `\n${CYAN}●${RESET} Writing tests...\n` },
    {
      delay: 2500,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/components/Dashboard.test.tsx${RESET} ${DIM}(6 tests)${RESET}\n`,
    },
    { delay: 1500, data: `${GREEN}✓${RESET} 6/6 tests passed\n` },
    {
      delay: 500,
      data: `\n${GREEN}${BOLD}Done.${RESET} Dashboard component built with responsive layout.\n`,
    },
  ],
};
```

**Step 4: Commit**

Use `/commit`

---

### Task 9: Create mock transport

**Files:**

- Create: `assets/dashboard/website/src/demo/transport/mockTransport.ts`
- Create: `assets/dashboard/website/src/demo/transport/MockWebSocket.ts`
- Create: `assets/dashboard/website/src/demo/transport/TerminalPlayback.ts`

**Step 1: Create MockWebSocket class**

This implements the WebSocket interface enough for the dashboard hooks to work.

```typescript
// assets/dashboard/website/src/demo/transport/MockWebSocket.ts
import type { TerminalRecording } from '../recordings/types';

type WSListener = (ev: any) => void;

/**
 * A fake WebSocket that emits scripted data.
 * Implements enough of the WebSocket API for useSessionsWebSocket and TerminalStream.
 */
export class MockDashboardWebSocket {
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;

  readyState = 0; // CONNECTING
  binaryType: BinaryType = 'blob';
  onopen: WSListener | null = null;
  onmessage: WSListener | null = null;
  onclose: WSListener | null = null;
  onerror: WSListener | null = null;
  url: string;

  constructor(url: string) {
    this.url = url;
    // Simulate async open
    setTimeout(() => {
      this.readyState = 1; // OPEN
      this.onopen?.({ type: 'open' });
    }, 50);
  }

  /** Push a JSON message to the consumer (simulates server sending data) */
  pushMessage(data: unknown) {
    if (this.readyState !== 1) return;
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  send(_data: string | ArrayBuffer) {
    // Silently ignore sends (demo doesn't process client messages)
  }

  close() {
    this.readyState = 3; // CLOSED
    this.onclose?.({ code: 1000, reason: 'demo closed', type: 'close' });
  }

  // Stubs for WebSocket API compliance
  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() {
    return false;
  }
}

/**
 * A fake WebSocket for terminal sessions that plays back recorded frames.
 */
export class MockTerminalWebSocket {
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;

  readyState = 0;
  binaryType: BinaryType = 'blob';
  onopen: WSListener | null = null;
  onmessage: WSListener | null = null;
  onclose: WSListener | null = null;
  onerror: WSListener | null = null;
  url: string;

  private timers: ReturnType<typeof setTimeout>[] = [];
  private recording: TerminalRecording | null = null;
  private _paused = false;
  private _currentFrame = 0;

  constructor(url: string) {
    this.url = url;
    setTimeout(() => {
      this.readyState = 1;
      this.onopen?.({ type: 'open' });
    }, 50);
  }

  /** Start playback of a terminal recording */
  startPlayback(recording: TerminalRecording) {
    this.recording = recording;
    this._currentFrame = 0;
    this.playFrom(0);
  }

  private playFrom(frameIndex: number) {
    if (!this.recording || this._paused || this.readyState !== 1) return;

    let cumulativeDelay = 0;
    for (let i = frameIndex; i < this.recording.frames.length; i++) {
      const frame = this.recording.frames[i];
      cumulativeDelay += frame.delay;
      const timer = setTimeout(() => {
        if (this._paused || this.readyState !== 1) return;
        this._currentFrame = i + 1;
        // Send as ArrayBuffer (binary frame) like the real terminal WebSocket
        const encoder = new TextEncoder();
        const buffer = encoder.encode(frame.data).buffer;
        this.onmessage?.({ data: buffer });
      }, cumulativeDelay);
      this.timers.push(timer);
    }
  }

  pause() {
    this._paused = true;
    this.timers.forEach(clearTimeout);
    this.timers = [];
  }

  resume() {
    this._paused = false;
    this.playFrom(this._currentFrame);
  }

  send(data: string | ArrayBuffer) {
    // Parse resize messages so TerminalStream doesn't error
    // Ignore input messages silently
  }

  close() {
    this.timers.forEach(clearTimeout);
    this.timers = [];
    this.readyState = 3;
    this.onclose?.({ code: 1000, reason: 'demo closed', type: 'close' });
  }

  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() {
    return false;
  }
}
```

**Step 2: Create the mock transport**

```typescript
// assets/dashboard/website/src/demo/transport/mockTransport.ts
import type { Transport } from '@dashboard/lib/transport';
import type { WorkspaceResponse } from '@dashboard/lib/types';
import type { TerminalRecording } from '../recordings/types';
import { MockDashboardWebSocket, MockTerminalWebSocket } from './MockWebSocket';
import { createDemoConfig } from './mockData';

export interface DemoTransportOptions {
  /** Initial workspace state */
  workspaces: WorkspaceResponse[];
  /** Terminal recordings keyed by session ID */
  recordings: Record<string, TerminalRecording>;
}

export function createDemoTransport(options: DemoTransportOptions): Transport & {
  /** Update the workspace state (triggers re-broadcast to dashboard WS) */
  updateWorkspaces: (workspaces: WorkspaceResponse[]) => void;
  /** Get the terminal playback for a session */
  getTerminalWS: (sessionId: string) => MockTerminalWebSocket | undefined;
} {
  let currentWorkspaces = options.workspaces;
  let dashboardWS: MockDashboardWebSocket | null = null;
  const terminalSockets = new Map<string, MockTerminalWebSocket>();

  const transport: Transport & {
    updateWorkspaces: (ws: WorkspaceResponse[]) => void;
    getTerminalWS: (sid: string) => MockTerminalWebSocket | undefined;
  } = {
    createWebSocket(url: string): WebSocket {
      if (url.includes('/ws/dashboard')) {
        dashboardWS = new MockDashboardWebSocket(url);
        // Send initial state after connection opens
        setTimeout(() => {
          dashboardWS?.pushMessage({ type: 'sessions', workspaces: currentWorkspaces });
        }, 100);
        return dashboardWS as unknown as WebSocket;
      }

      if (url.includes('/ws/terminal/')) {
        const sessionId = url.split('/ws/terminal/')[1];
        const mockWS = new MockTerminalWebSocket(url);
        terminalSockets.set(sessionId, mockWS);
        // Start playback after connection opens
        const recording = options.recordings[sessionId];
        if (recording) {
          setTimeout(() => mockWS.startPlayback(recording), 150);
        }
        return mockWS as unknown as WebSocket;
      }

      // Fallback: return a no-op WebSocket
      return new MockDashboardWebSocket(url) as unknown as WebSocket;
    },

    fetch(input: RequestInfo | URL, _init?: RequestInit): Promise<Response> {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

      // Config endpoint
      if (url.includes('/api/config')) {
        return Promise.resolve(
          new Response(JSON.stringify(createDemoConfig()), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Sessions endpoint
      if (url.includes('/api/sessions')) {
        return Promise.resolve(
          new Response(JSON.stringify(currentWorkspaces), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Spawn endpoint (return success)
      if (url.includes('/api/spawn')) {
        return Promise.resolve(
          new Response(JSON.stringify({ sessions: [{ id: 'demo-sess-new' }] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Detect tools
      if (url.includes('/api/detect-tools')) {
        return Promise.resolve(
          new Response(JSON.stringify({ agents: ['claude', 'codex'] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Default: return 200 empty
      return Promise.resolve(
        new Response('{}', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );
    },

    updateWorkspaces(workspaces: WorkspaceResponse[]) {
      currentWorkspaces = workspaces;
      dashboardWS?.pushMessage({ type: 'sessions', workspaces });
    },

    getTerminalWS(sessionId: string) {
      return terminalSockets.get(sessionId);
    },
  };

  return transport;
}
```

**Step 3: Commit**

Use `/commit`

---

## Phase 4: Scenarios

### Task 10: Create workspaces scenario

**Files:**

- Create: `assets/dashboard/website/src/demo/scenarios/workspaces.ts`

**Step 1: Define the workspaces scenario**

```typescript
// assets/dashboard/website/src/demo/scenarios/workspaces.ts
import type { TourScenario } from '../tour/types';

export const workspacesScenario: TourScenario = {
  id: 'workspaces',
  title: 'Monitor Running Agents',
  description: 'See how schmux lets you watch multiple AI agents work simultaneously.',
  initialRoute: '/',
  steps: [
    {
      target: '[data-tour="sidebar-workspace-list"]',
      title: 'Your workspaces',
      body: 'The sidebar shows all your active workspaces. Each workspace is a git branch where agents are working.',
      placement: 'right',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="sidebar-session"]',
      title: 'Running sessions',
      body: "Each session is an AI agent working in a workspace. The green dot means it's actively running.",
      placement: 'right',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="sidebar-session-status"]',
      title: 'Live activity',
      body: 'You can see what each agent is doing at a glance — reading files, writing code, running tests.',
      placement: 'right',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="sidebar-session"]',
      title: 'Open a session',
      body: 'Click on a session to see its live terminal output.',
      placement: 'right',
      advanceOn: 'click',
      afterStep: () => {
        // Navigation happens via React Router click handler on the session
      },
    },
    {
      target: '[data-tour="terminal-viewport"]',
      title: 'Live terminal',
      body: "Watch the agent work in real time. This is the same terminal output you'd see in tmux.",
      placement: 'left',
      advanceOn: 'next',
      route: '/sessions/demo-sess-1',
    },
    {
      target: '[data-tour="session-detail-sidebar"]',
      title: 'Session details',
      body: 'See the agent type, persona, branch, and attach command. You can always jump into tmux directly.',
      placement: 'left',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="session-tabs"]',
      title: 'Switch between agents',
      body: 'Tabs let you quickly flip between agents working in the same workspace. All running in parallel.',
      placement: 'bottom',
      advanceOn: 'next',
    },
  ],
};
```

**Step 2: Commit**

Use `/commit`

---

### Task 11: Create spawn scenario

**Files:**

- Create: `assets/dashboard/website/src/demo/scenarios/spawn.ts`

**Step 1: Define the spawn scenario**

This scenario needs `beforeStep`/`afterStep` hooks that interact with the demo transport to update state. Those hooks will be wired in the DemoShell (Task 12) since they need a reference to the transport instance.

```typescript
// assets/dashboard/website/src/demo/scenarios/spawn.ts
import type { TourScenario } from '../tour/types';

/** Base scenario definition. beforeStep/afterStep hooks are wired in DemoShell. */
export const spawnScenario: TourScenario = {
  id: 'spawn',
  title: 'Spawn Your First Agent',
  description: 'Launch an AI coding agent and watch it start working.',
  initialRoute: '/',
  steps: [
    {
      target: '[data-tour="sidebar-add-workspace"]',
      title: 'Add a workspace',
      body: 'Click "Add Workspace" to spawn a new AI agent. This opens the spawn wizard.',
      placement: 'right',
      advanceOn: 'click',
    },
    {
      target: '[data-tour="spawn-form"]',
      title: 'The spawn wizard',
      body: 'Configure your agent here — pick a repo, choose a persona, and describe what you want built.',
      placement: 'left',
      advanceOn: 'next',
      route: '/spawn',
    },
    {
      target: '[data-tour="spawn-repo-select"]',
      title: 'Pick a repository',
      body: 'Select which codebase the agent will work in. schmux creates an isolated git worktree for each workspace.',
      placement: 'bottom',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="spawn-persona-select"]',
      title: 'Choose a persona',
      body: 'Personas shape how the agent approaches work — a "Backend Engineer" focuses on APIs, a "QA Engineer" on tests.',
      placement: 'bottom',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="spawn-submit"]',
      title: 'Launch the agent',
      body: 'Hit Engage to spawn the agent. It starts working immediately in its own tmux session.',
      placement: 'top',
      advanceOn: 'click',
      // afterStep: wired in DemoShell to update workspaces with new session
    },
    {
      target: '[data-tour="terminal-viewport"]',
      title: 'Watch it work',
      body: 'Your agent is now running! Watch it analyze the codebase, write code, and run tests — all autonomously.',
      placement: 'left',
      advanceOn: 'next',
      // route + beforeStep: wired in DemoShell to navigate to new session
    },
  ],
};
```

**Step 2: Commit**

Use `/commit`

---

## Phase 5: Website Integration

### Task 12: Create the DemoShell and integrate with website

**Files:**

- Create: `assets/dashboard/website/src/demo/DemoShell.tsx`
- Create: `assets/dashboard/website/demo/index.html`
- Create: `assets/dashboard/website/src/demo/main.tsx`
- Modify: `assets/dashboard/website/vite.config.ts` (add multi-page input)
- Modify: `assets/dashboard/website/src/sections/Sessions.tsx` (add CTA)
- Modify: `assets/dashboard/website/src/sections/Spawn.tsx` (add CTA)

**Step 1: Create demo/index.html**

```bash
mkdir -p assets/dashboard/website/demo
```

```html
<!-- assets/dashboard/website/demo/index.html -->
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>schmux — Interactive Demo</title>
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
    <link
      href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=DM+Sans:wght@400;500;600;700&display=swap"
      rel="stylesheet"
    />
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="../src/demo/main.tsx"></script>
  </body>
</html>
```

**Step 2: Create demo entry point**

```tsx
// assets/dashboard/website/src/demo/main.tsx
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import '@dashboard/styles/global.css';
import DemoShell from './DemoShell';

createRoot(document.getElementById('root')!).render(
  <HashRouter>
    <DemoShell />
  </HashRouter>
);
```

**Step 3: Create DemoShell**

```tsx
// assets/dashboard/website/src/demo/DemoShell.tsx
import { useEffect, useMemo } from 'react';
import { Routes, Route, Navigate, useParams } from 'react-router-dom';
import { setTransport, liveTransport } from '@dashboard/lib/transport';
import App from '@dashboard/App';
import TourProvider from './tour/TourProvider';
import { createDemoTransport } from './transport/mockTransport';
import { createDemoWorkspaces, createPostSpawnWorkspaces } from './transport/mockData';
import { authImplRecording, testWriterRecording } from './recordings/workspaces';
import { newAgentRecording } from './recordings/spawn';
import { workspacesScenario } from './scenarios/workspaces';
import { spawnScenario } from './scenarios/spawn';
import type { TourScenario } from './tour/types';

const SCENARIOS: Record<
  string,
  () => { scenario: TourScenario; transport: ReturnType<typeof createDemoTransport> }
> = {
  workspaces: () => {
    const dt = createDemoTransport({
      workspaces: createDemoWorkspaces(),
      recordings: {
        'demo-sess-1': authImplRecording,
        'demo-sess-2': testWriterRecording,
      },
    });
    return { scenario: workspacesScenario, transport: dt };
  },
  spawn: () => {
    const dt = createDemoTransport({
      workspaces: createDemoWorkspaces(),
      recordings: {
        'demo-sess-1': authImplRecording,
        'demo-sess-2': testWriterRecording,
        'demo-sess-new': newAgentRecording,
      },
    });

    // Wire up spawn scenario hooks that need transport access
    const scenario = { ...spawnScenario };
    scenario.steps = scenario.steps.map((step, i) => {
      if (i === 4) {
        // "Launch the agent" step — after clicking Engage, update state
        return {
          ...step,
          afterStep: () => {
            dt.updateWorkspaces(createPostSpawnWorkspaces());
          },
        };
      }
      if (i === 5) {
        // "Watch it work" step — navigate to new session
        return {
          ...step,
          route: '/sessions/demo-sess-new',
        };
      }
      return step;
    });

    return { scenario, transport: dt };
  },
};

function ScenarioRoute() {
  const { scenarioId } = useParams<{ scenarioId: string }>();

  const setup = useMemo(() => {
    const factory = SCENARIOS[scenarioId || ''];
    if (!factory) return null;
    return factory();
  }, [scenarioId]);

  useEffect(() => {
    if (!setup) return;
    setTransport(setup.transport);
    return () => setTransport(liveTransport);
  }, [setup]);

  if (!setup) return <Navigate to="/workspaces" replace />;

  return (
    <TourProvider scenario={setup.scenario}>
      <App />
    </TourProvider>
  );
}

export default function DemoShell() {
  return (
    <Routes>
      <Route path="/:scenarioId" element={<ScenarioRoute />} />
      <Route path="*" element={<Navigate to="/workspaces" replace />} />
    </Routes>
  );
}
```

**Step 4: Update website Vite config for multi-page build**

In `assets/dashboard/website/vite.config.ts`, add the demo entry to `build.rollupOptions.input`:

```typescript
// Add to the top of the file
import { resolve } from 'path';

// In the defineConfig, add/modify the build section:
build: {
  outDir: resolve(__dirname, '../../../dist/website'),
  emptyOutDir: true,
  rollupOptions: {
    input: {
      main: resolve(__dirname, 'index.html'),
      demo: resolve(__dirname, 'demo/index.html'),
    },
  },
},
```

**Step 5: Add CTA links to marketing sections**

In `assets/dashboard/website/src/sections/Sessions.tsx`, add a "Try it" link after the existing content. Find the section's closing content area and add:

```tsx
<a href="demo/#/workspaces" className="website-cta-link">
  Try it →
</a>
```

In `assets/dashboard/website/src/sections/Spawn.tsx`, add similarly:

```tsx
<a href="demo/#/spawn" className="website-cta-link">
  Try it →
</a>
```

Add the CTA link style to `assets/dashboard/website/src/styles/website.css`:

```css
.website-cta-link {
  display: inline-block;
  margin-top: var(--spacing-md);
  color: var(--accent-color, #6366f1);
  font-weight: 500;
  font-size: 0.95rem;
  text-decoration: none;
  transition: opacity 0.15s ease;
}

.website-cta-link:hover {
  opacity: 0.8;
}
```

**Step 6: Verify the build**

```bash
cd assets/dashboard/website && npx vite build
```

Expected: Build succeeds, output in `dist/website/` with both `index.html` and `demo/index.html`.

**Step 7: Test locally**

```bash
cd assets/dashboard/website && npx vite preview
```

Navigate to `http://localhost:4173/` (marketing site) and `http://localhost:4173/demo/#/workspaces` (demo). Verify:

- Marketing site renders as before
- Demo loads the dashboard UI with mock data in the sidebar
- Spotlight tour appears and steps advance
- Terminal shows animated playback

**Step 8: Commit**

Use `/commit`

---

## Phase 6: Polish and Verify

### Task 13: Handle edge cases in DemoShell

**Files:**

- Modify: `assets/dashboard/website/src/demo/DemoShell.tsx`
- Create: `assets/dashboard/website/src/demo/demo.css`

**Step 1: Add a "Demo Mode" banner and scenario picker**

When a scenario ends (tour becomes inactive), show a banner offering to restart or try another scenario.

```css
/* assets/dashboard/website/src/demo/demo.css */

.demo-banner {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 10000;
  background: var(--bg-primary, #1a1a2e);
  border-top: 1px solid var(--border-color, #333);
  padding: 12px 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-family: 'DM Sans', system-ui, sans-serif;
  font-size: 13px;
  color: var(--text-secondary, #b0b0b0);
}

.demo-banner__label {
  display: flex;
  align-items: center;
  gap: 8px;
}

.demo-banner__badge {
  background: var(--accent-color, #6366f1);
  color: white;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.demo-banner__actions {
  display: flex;
  gap: 12px;
  align-items: center;
}

.demo-banner__link {
  color: var(--accent-color, #6366f1);
  text-decoration: none;
  font-weight: 500;
}
```

**Step 2: Add the banner to DemoShell**

In `ScenarioRoute`, after the `<TourProvider>`, conditionally render a persistent bottom banner:

```tsx
import './demo.css';

// Inside ScenarioRoute return:
return (
  <TourProvider scenario={setup.scenario}>
    <App />
    <div className="demo-banner">
      <div className="demo-banner__label">
        <span className="demo-banner__badge">Demo</span>
        <span>You're exploring schmux with sample data</span>
      </div>
      <div className="demo-banner__actions">
        <a className="demo-banner__link" href="#/workspaces">
          Workspaces tour
        </a>
        <a className="demo-banner__link" href="#/spawn">
          Spawn tour
        </a>
        <a
          className="demo-banner__link"
          href="https://github.com/sergeknystautas/schmux"
          target="_blank"
          rel="noopener"
        >
          Install schmux →
        </a>
      </div>
    </div>
  </TourProvider>
);
```

**Step 3: Verify end-to-end**

```bash
cd assets/dashboard/website && npx vite build && npx vite preview
```

Test both scenarios manually:

1. `/demo/#/workspaces` — walk through all 7 steps
2. `/demo/#/spawn` — walk through all 6 steps
3. Click between scenarios via the banner
4. Verify marketing site CTA links work

**Step 4: Run full test suite**

```bash
./test.sh --quick
```

Expected: All existing dashboard tests still pass.

**Step 5: Commit**

Use `/commit`

---

## Summary

| Phase                  | Tasks        | Files created | Files modified |
| ---------------------- | ------------ | ------------- | -------------- |
| 1: Transport           | Tasks 1-4    | 2             | 3              |
| 2: Data-tour           | Task 5       | 0             | 4              |
| 3: Tour system         | Task 6       | 5             | 0              |
| 4: Mock transport      | Tasks 7-9    | 5             | 0              |
| 5: Scenarios           | Tasks 10-11  | 2             | 0              |
| 6: Website integration | Tasks 12-13  | 5             | 3              |
| **Total**              | **13 tasks** | **19 files**  | **10 files**   |
