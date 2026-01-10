# React Frontend Improvements - Task Tracker

**Last Updated:** 2025-01-08

Progress tracking document for React frontend improvements. Agents should grab the next available task from the Ready section, work on it, and update status when complete.

---

## Quick Summary

- **Total Tasks:** 52
- **Completed:** 0
- **In Progress:** 0
- **Ready to Start:** 7
- **Blocked:** 0

---

## Ready to Start (Pick One)

### 1.0-TS-CONFIG: Set up TypeScript configuration

**Status:** 🔵 Ready | **Priority:** 🔴 High | **Estimated:** 1-2 hours

**Description:** Initialize TypeScript in the project with basic configuration.

**Steps:**
- [ ] Install TypeScript: `npm install --save-dev typescript @types/react @types/react-dom`
- [ ] Create `tsconfig.json` in `/assets/dashboard/`
- [ ] Create `vite.config.ts` (rename from .js)
- [ ] Add `"type": "module"` to tsconfig
- [ ] Add paths mapping for `@/*` aliases if desired
- [ ] Update build script to use `tsc --noEmit` for type checking

**Files to Create:**
- `assets/dashboard/tsconfig.json`
- `assets/dashboard/vite.config.ts`

**Files to Modify:**
- `assets/dashboard/package.json`

**Dependencies:** None

**Verification:** Run `npm run build` successfully with TypeScript config present

---

### 2.1-ERROR-BOUNDARY-COMP: Create ErrorBoundary component

**Status:** 🔵 Ready | **Priority:** 🔴 High | **Estimated:** 1-2 hours

**Description:** Build a reusable ErrorBoundary component class to catch React errors.

**Steps:**
- [ ] Create `src/components/ErrorBoundary.jsx`
- [ ] Implement componentDidCatch and getDerivedStateFromError
- [ ] Add fallback UI with error message
- [ ] Add "Try Again" button that resets error state
- [ ] Add console.error logging for debugging
- [ ] Export as default

**Files to Create:**
- `assets/dashboard/src/components/ErrorBoundary.jsx`

**Dependencies:** None

**Verification:** Component renders children normally, shows fallback on error

---

### 3.1-MODULE-STATE-FIX: Eliminate module-level state in SessionDetailPage

**Status:** 🔵 Ready | **Priority:** 🔴 High | **Estimated:** 30 minutes

**Description:** Remove the module-level `savedSidebarCollapsed` variable and replace with proper component state + localStorage.

**Steps:**
- [ ] Remove `let savedSidebarCollapsed = false;` from top of file (line 14)
- [ ] Initialize state with localStorage read in useState
- [ ] Add useEffect to persist changes to localStorage
- [ ] Test that state persists across navigation
- [ ] Remove `savedSidebarCollapsed = next` line

**Files to Modify:**
- `assets/dashboard/src/routes/SessionDetailPage.jsx` (line 14, line 126)

**Dependencies:** None

**Verification:** Sidebar collapse state persists across page navigation

---

### 4.1-FIX-MUTATION-SESSIONS: Fix state mutation in SessionsPage

**Status:** 🔵 Ready | **Priority:** 🔴 High | **Estimated:** 30 minutes

**Description:** Replace the forEach mutation pattern with immutable update using Object.fromEntries.

**Steps:**
- [ ] Locate setExpanded call in loadWorkspaces callback (lines 55-63)
- [ ] Replace forEach mutation with Object.fromEntries/map pattern
- [ ] Ensure new workspace IDs default to true if not in current state
- [ ] Test that workspace expansion state works correctly

**Files to Modify:**
- `assets/dashboard/src/routes/SessionsPage.jsx` (lines 55-63)

**Dependencies:** None

**Verification:** Workspace expansion/collapse works, no state mutation

---

### 5.1-AUDIT-NAVIGATION: Audit all navigation patterns

**Status:** 🔵 Ready | **Priority:** 🟡 Medium | **Estimated:** 1 hour

**Description:** Find all instances of direct navigation and categorize them by type.

**Steps:**
- [ ] Grep for `window.location.href` usage
- [ ] Grep for `window.location.assign` usage
- [ ] Grep for `useNavigate()` usage
- [ ] Grep for `<Link>` usage
- [ ] Document findings in a comment or separate doc
- [ ] Identify which files need updates

**Files to Audit:**
- All files in `src/components/`
- All files in `src/routes/`

**Dependencies:** None

**Verification:** List of all navigation patterns and files needing updates

---

### 7.1-CREATE-ASYNC-HOOK: Create useAsyncEffect hook with cancellation

**Status:** 🔵 Ready | **Priority:** 🟡 Medium | **Estimated:** 1-2 hours

**Description:** Build a custom hook that wraps useEffect with AbortController support.

**Steps:**
- [ ] Create `src/hooks/useAsyncEffect.js`
- [ ] Accept async function as first parameter
- [ ] Create AbortController internally
- [ ] Pass signal to async function
- [ ] Cleanup on unmount
- [ ] Handle AbortError silently
- [ ] Export as default

**Files to Create:**
- `assets/dashboard/src/hooks/useAsyncEffect.js`

**Dependencies:** None

**Verification:** Hook can be used, cleans up on unmount, handles abort

---

### 8.1-CREATE-LOADING-COMPONENT: Create LoadingState component

**Status:** 🔵 Ready | **Priority:** 🟡 Medium | **Estimated:** 1 hour

**Description:** Build a reusable loading state component with spinner.

**Steps:**
- [ ] Create `src/components/LoadingState.jsx`
- [ ] Accept optional message prop
- [ ] Use existing spinner CSS classes
- [ ] Match existing empty-state styling
- [ ] Export as default

**Files to Create:**
- `assets/dashboard/src/components/LoadingState.jsx`

**Dependencies:** None

**Verification:** Component renders with spinner and optional message

---

## In Progress

*No tasks currently in progress*

---

## Completed

*No tasks completed yet*

---

## Blocked (Waiting on Dependencies)

### 1.1-TS-TYPES-API: Create API type definitions

**Status:** 🟡 Blocked | **Priority:** 🔴 High | **Estimated:** 2-3 hours

**Description:** Define TypeScript interfaces for all API responses.

**Steps:**
- [ ] Create `src/types/api.ts`
- [ ] Define Session interface
- [ ] Define Workspace interface
- [ ] Define Agent interface
- [ ] Define Config interface
- [ ] Define SpawnRequest interface
- [ ] Define SpawnResponse interface
- [ ] Export all types

**Files to Create:**
- `assets/dashboard/src/types/api.ts`

**Dependencies:** 1.0-TS-CONFIG

**Blocked By:** TypeScript must be configured first

---

### 1.2-TS-MIGRATE-UTILS: Migrate lib/utils.js to TypeScript

**Status:** 🟡 Blocked | **Priority:** 🔴 High | **Estimated:** 1 hour

**Description:** Convert utility functions to TypeScript with proper types.

**Steps:**
- [ ] Rename `lib/utils.js` to `lib/utils.ts`
- [ ] Add types for all function parameters
- [ ] Add return types for all functions
- [ ] Fix any type errors
- [ ] Verify imports still work

**Files to Modify:**
- `assets/dashboard/src/lib/utils.js` → `utils.ts`

**Dependencies:** 1.0-TS-CONFIG, 1.1-TS-TYPES-API

**Blocked By:** TS config and API types must exist first

---

### 1.3-TS-MIGRATE-API: Migrate lib/api.js to TypeScript

**Status:** 🟡 Blocked | **Priority:** 🔴 High | **Estimated:** 1-2 hours

**Description:** Convert API layer to TypeScript with proper request/response types.

**Steps:**
- [ ] Rename `lib/api.js` to `lib/api.ts`
- [ ] Import types from types/api.ts
- [ ] Type all API function parameters
- [ ] Type all return values (Promise<T>)
- [ ] Fix any type errors
- [ ] Update imports in consuming files

**Files to Modify:**
- `assets/dashboard/src/lib/api.js` → `api.ts`

**Dependencies:** 1.0-TS-CONFIG, 1.1-TS-TYPES-API

**Blocked By:** TS config and API types must exist first

---

### 2.2-ERROR-BOUNDARY-INTEGRATE: Integrate ErrorBoundary into App

**Status:** 🟡 Blocked | **Priority:** 🔴 High | **Estimated:** 30 minutes

**Description:** Wrap the Routes component with ErrorBoundary.

**Steps:**
- [ ] Import ErrorBoundary in App.jsx
- [ ] Wrap `<Routes>` with `<ErrorBoundary>`
- [ ] Test by adding intentional error to a component
- [ ] Verify fallback UI appears

**Files to Modify:**
- `assets/dashboard/src/App.jsx`

**Dependencies:** 2.1-ERROR-BOUNDARY-COMP

**Blocked By:** ErrorBoundary component must exist first

---

### 5.2-FIX-NAVIGATION-SESSIONS: Fix navigation in SessionTableRow

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 30 minutes

**Description:** Replace window.location.href with React Router navigation.

**Steps:**
- [ ] Import useNavigate from react-router-dom
- [ ] Replace window.location.href with navigate()
- [ ] Update button to onClick handler (not button with click to href)
- [ ] Test navigation works correctly

**Files to Modify:**
- `assets/dashboard/src/components/SessionTableRow.jsx` (lines 26, 41)

**Dependencies:** 5.1-AUDIT-NAVIGATION

**Blocked By:** Need audit to know all instances to fix

---

### 6.1-SETUP-TESTING: Set up Vitest and React Testing Library

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 1-2 hours

**Description:** Install and configure test framework.

**Steps:**
- [ ] Install dependencies: vitest, @testing-library/react, @testing-library/user-event, jsdom
- [ ] Create `vitest.config.ts`
- [ ] Add test script to package.json
- [ ] Create test setup file if needed
- [ ] Run `npm test` to verify setup

**Files to Create:**
- `assets/dashboard/vitest.config.ts`
- `assets/dashboard/src/test/setup.js` (if needed)

**Files to Modify:**
- `assets/dashboard/package.json`

**Dependencies:** 1.0-TS-CONFIG (recommended)

**Blocked By:** Should wait for TypeScript config

---

### 6.2-TEST-TOAST: Test ToastProvider component

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 1-2 hours

**Description:** Write unit tests for ToastProvider functionality.

**Steps:**
- [ ] Create `components/__tests__/ToastProvider.test.jsx`
- [ ] Test toast appears when show() called
- [ ] Test toast auto-dismisses after duration
- [ ] Test multiple toasts can be shown
- [ ] Test success/error variants work
- [ ] Test toast can be manually removed

**Files to Create:**
- `assets/dashboard/src/components/__tests__/ToastProvider.test.jsx`

**Dependencies:** 6.1-SETUP-TESTING

**Blocked By:** Test framework must be set up first

---

### 7.2-UPDATE-API-FOR-CANCELLATION: Update api.js to accept signal parameter

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 1-2 hours

**Description:** Modify all API functions to accept and use AbortSignal.

**Steps:**
- [ ] Update getSessions to accept optional signal parameter
- [ ] Update getWorkspaces to accept optional signal parameter
- [ ] Update getConfig to accept optional signal parameter
- [ ] Update spawnSessions to accept optional signal parameter
- [ ] Update disposeSession to accept optional signal parameter
- [ ] Update updateNickname to accept optional signal parameter
- [ ] Update disposeWorkspace to accept optional signal parameter
- [ ] Update getDiff to accept optional signal parameter
- [ ] Update scanWorkspaces to accept optional signal parameter
- [ ] Pass signal to fetch() calls

**Files to Modify:**
- `assets/dashboard/src/lib/api.js`

**Dependencies:** 7.1-CREATE-ASYNC-HOOK

**Blocked By:** Hook pattern should be established first

---

### 7.3-ADD-CANCELLATION-CONTEXTS: Add cancellation to ConfigContext

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 30 minutes

**Description:** Update ConfigContext to use cancellation for config loading.

**Steps:**
- [ ] Import useAsyncEffect or use AbortController
- [ ] Add AbortController to loadConfig useEffect
- [ ] Pass signal to getConfig()
- [ ] Abort on cleanup
- [ ] Handle AbortError silently

**Files to Modify:**
- `assets/dashboard/src/contexts/ConfigContext.jsx`

**Dependencies:** 7.1-CREATE-ASYNC-HOOK, 7.2-UPDATE-API-FOR-CANCELLATION

**Blocked By:** Need hook and API updates first

---

### 7.4-ADD-CANCELLATION-SESSIONS: Add cancellation to SessionsPage

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 30 minutes

**Description:** Update SessionsPage data fetching with cancellation.

**Steps:**
- [ ] Add AbortController to loadWorkspaces useEffect
- [ ] Pass signal to getSessions()
- [ ] Abort on cleanup
- [ ] Handle AbortError silently

**Files to Modify:**
- `assets/dashboard/src/routes/SessionsPage.jsx`

**Dependencies:** 7.1-CREATE-ASYNC-HOOK, 7.2-UPDATE-API-FOR-CANCELLATION

**Blocked By:** Need hook and API updates first

---

### 7.5-ADD-CANCELLATION-DETAIL: Add cancellation to SessionDetailPage

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 30 minutes

**Description:** Update SessionDetailPage data fetching with cancellation.

**Steps:**
- [ ] Add AbortController to load session useEffect
- [ ] Pass signal to getSessions()
- [ ] Abort on cleanup
- [ ] Handle AbortError silently

**Files to Modify:**
- `assets/dashboard/src/routes/SessionDetailPage.jsx`

**Dependencies:** 7.1-CREATE-ASYNC-HOOK, 7.2-UPDATE-API-FOR-CANCELLATION

**Blocked By:** Need hook and API updates first

---

### 8.2-CREATE-ERROR-COMPONENT: Create ErrorState component

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 1 hour

**Description:** Build a reusable error state component with retry capability.

**Steps:**
- [ ] Create `src/components/ErrorState.jsx`
- [ ] Accept message, onRetry props
- [ ] Use existing error-state CSS classes
- [ ] Add retry button
- [ ] Export as default

**Files to Create:**
- `assets/dashboard/src/components/ErrorState.jsx`

**Dependencies:** None (but 8.1 should be done first for consistency)

**Blocked By:** Should complete LoadingState first for pattern consistency

---

### 8.3-CREATE-EMPTY-COMPONENT: Create EmptyState component

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 1 hour

**Description:** Build a reusable empty state component.

**Steps:**
- [ ] Create `src/components/EmptyState.jsx`
- [ ] Accept icon, title, description, action props
- [ ] Use existing empty-state CSS classes
- [ ] Export as default

**Files to Create:**
- `assets/dashboard/src/components/EmptyState.jsx`

**Dependencies:** None (but 8.1, 8.2 should be done first)

**Blocked By:** Should complete other state components first for consistency

---

### 8.4-AUDIT-STATES: Audit all components for missing states

**Status:** 🟡 Blocked | **Priority:** 🟡 Medium | **Estimated:** 1-2 hours

**Description:** Review all components and identify where loading/error/empty states are missing.

**Steps:**
- [ ] Review SessionsPage for state completeness
- [ ] Review SessionDetailPage for state completeness
- [ ] Review SpawnPage for state completeness
- [ ] Document gaps and create follow-up tasks

**Files to Audit:**
- All route components

**Dependencies:** 8.1, 8.2, 8.3

**Blocked By:** Should have reusable components created first

---

## Not Started (Dependencies Not Met)

### 1.4-TS-MIGRATE-HOOKS: Migrate hooks to TypeScript

**Status:** ⚪ Not Started | **Priority:** 🔴 High | **Estimated:** 1-2 hours

**Dependencies:** 1.0-TS-CONFIG, 1.1-TS-TYPES-API

---

### 1.5-TS-MIGRATE-CONTEXTS: Migrate contexts to TypeScript

**Status:** ⚪ Not Started | **Priority:** 🔴 High | **Estimated:** 1-2 hours

**Dependencies:** 1.0-TS-CONFIG, 1.1-TS-TYPES-API

---

### 1.6-TS-MIGRATE-COMPONENTS: Migrate components to TypeScript

**Status:** ⚪ Not Started | **Priority:** 🔴 High | **Estimated:** 3-4 hours

**Dependencies:** 1.0-TS-CONFIG, 1.1-TS-TYPES-API

---

### 1.7-TS-MIGRATE-ROUTES: Migrate route components to TypeScript

**Status:** ⚪ Not Started | **Priority:** 🔴 High | **Estimated:** 2-3 hours

**Dependencies:** 1.0-TS-CONFIG, 1.1-TS-TYPES-API, 1.6-TS-MIGRATE-COMPONENTS

---

### 1.8-TS-STRICT-MODE: Enable strict TypeScript mode

**Status:** ⚪ Not Started | **Priority:** 🔴 High | **Estimated:** 2-3 hours

**Dependencies:** 1.0-TS-CONFIG, 1.1-TS-TYPES-API, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7

---

### 1.9-TS-CI-INTEGRATION: Add TypeScript to CI/CD

**Status:** ⚪ Not Started | **Priority:** 🔴 High | **Estimated:** 1 hour

**Dependencies:** 1.0-TS-CONFIG

---

### 6.3-TEST-MODAL: Test ModalProvider component

**Status:** ⚪ Not Started | **Priority:** 🟡 Medium | **Estimated:** 1-2 hours

**Dependencies:** 6.1-SETUP-TESTING

---

### 6.4-TEST-TOOLTIP: Test Tooltip component

**Status:** ⚪ Not Started | **Priority:** 🟡 Medium | **Estimated:** 2-3 hours

**Dependencies:** 6.1-SETUP-TESTING

---

### 6.5-TEST-SPAWN-FLOW: Integration test for spawn flow

**Status:** ⚪ Not Started | **Priority:** 🟡 Medium | **Estimated:** 2-3 hours

**Dependencies:** 6.1-SETUP-TESTING

---

### 6.6-TEST-DISPOSE-FLOW: Integration test for dispose flow

**Status:** ⚪ Not Started | **Priority:** 🟡 Medium | **Estimated:** 1-2 hours

**Dependencies:** 6.1-SETUP-TESTING

---

### 9.1-INSTALL-REACT-QUERY: Install React Query

**Status:** ⚪ Not Started | **Priority:** 🟢 Low | **Estimated:** 30 minutes

**Dependencies:** All Phases 1-3 tasks complete

---

### 9.2-SETUP-QUERY-CLIENT: Set up QueryClientProvider

**Status:** ⚪ Not Started | **Priority:** 🟢 Low | **Estimated:** 1 hour

**Dependencies:** 9.1-INSTALL-REACT-QUERY

---

### 9.3-CREATE-QUERY-HOOKS: Create query hooks

**Status:** ⚪ Not Started | **Priority:** 🟢 Low | **Estimated:** 2-3 hours

**Dependencies:** 9.2-SETUP-QUERY-CLIENT

---

### 9.4-REPLACE-SESSIONS-PAGE: Replace SessionsPage with useQuery

**Status:** ⚪ Not Started | **Priority:** 🟢 Low | **Estimated:** 2-3 hours

**Dependencies:** 9.3-CREATE-QUERY-HOOKS

---

### 9.5-REMOVE-MANUAL-POLLING: Remove manual polling logic

**Status:** ⚪ Not Started | **Priority:** 🟢 Low | **Estimated:** 1-2 hours

**Dependencies:** 9.4-REPLACE-SESSIONS-PAGE

---

### 9.6-REMOVE-CONFIG-CONTEXT: Remove ConfigContext

**Status:** ⚪ Not Started | **Priority:** 🟢 Low | **Estimated:** 1 hour

**Dependencies:** 9.4-REPLACE-SESSIONS-PAGE

---

## Task Status Legend

- 🔵 **Ready** - All dependencies met, safe to start
- 🟡 **Blocked** - Waiting for dependency task to complete
- ⚪ **Not Started** - Dependencies not yet met
- 🟢 **In Progress** - Currently being worked on
- ✅ **Completed** - Finished and verified

---

## How to Use This Document

1. **Pick a task** from the "Ready to Start" section
2. **Update status** to 🟢 In Progress when you begin
3. **Work through steps**, checking off each checkbox
4. **Verify** the completion criteria
5. **Update status** to ✅ Completed when done
6. **Update the summary** at the top of the document
7. **Check for newly unblocked tasks** and move them to "Ready to Start" if appropriate

## Agent Instructions

When starting work:
1. Read the entire task description
2. Check all dependencies are complete
3. Move task from "Ready to Start" to "In Progress"
4. Update the "Last Updated" date
5. Work through all checkboxes
6. When done, move to "Completed" and update counts

**Important:** Always check that a task's dependencies are complete before starting. If a dependency is marked as done but you find issues, note them in the task description.
