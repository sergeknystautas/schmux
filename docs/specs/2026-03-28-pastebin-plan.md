# Pastebin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a pastebin button next to the spawn "+" button in session tabs that opens a dropdown of saved text clips. Clicking a clip pastes it into the active terminal session.

**Architecture:** `[]string` stored in config under `"pastebin"` key. Go config/API layer exposes it. React config tab manages entries. `PastebinDropdown` component renders entries using ActionDropdown's CSS. `onPaste` callback prop threads `sendInput` from SessionDetailPage through SessionTabs to PastebinDropdown.

**Tech Stack:** Go (config, API handlers), React/TypeScript (UI components, config form), CSS Modules (dropdown styling reuses ActionDropdown styles)

---

## File Structure

### New files

- `assets/dashboard/src/components/PastebinDropdown.tsx` — Dropdown component showing pastebin entries
- `assets/dashboard/src/routes/config/PastebinTab.tsx` — Config tab for managing pastebin entries

### Modified files

- `internal/config/config.go` — Add `Pastebin []string` field, getter, Reload/CreateDefault
- `internal/api/contracts/config.go` — Add `Pastebin` to ConfigResponse and ConfigUpdateRequest
- `internal/dashboard/handlers_config.go` — Handle pastebin in GET and UPDATE
- `assets/dashboard/src/lib/types.generated.ts` — Auto-generated from contracts (via `go run ./cmd/gen-types`)
- `assets/dashboard/src/routes/config/useConfigForm.ts` — Add `pastebin` field and actions
- `assets/dashboard/src/routes/ConfigPage.tsx` — Add Pastebin tab
- `assets/dashboard/src/components/SessionTabs.tsx` — Add pastebin button, onPaste prop
- `assets/dashboard/src/routes/SessionDetailPage.tsx` — Pass `onPaste` callback to SessionTabs
- `docs/api.md` — Document new field

---

### Task 1: Go config layer — `Pastebin []string` field

**Files:**

- Modify: `internal/config/config.go` (lines ~72 struct field, ~910 getter pattern, ~1570 Reload, ~1615 CreateDefault)

- [ ] **Step 1: Add `Pastebin` field to Config struct**

In `internal/config/config.go`, find the field declaration block (around line 72, after `ExternalDiffCleanupAfterMs`). Add:

```go
Pastebin []string `json:"pastebin,omitempty"`
```

- [ ] **Step 2: Add getter method**

After the existing getter methods (around line 924, after `GetExternalDiffCleanupAfterMs`), add:

```go
func (c *Config) GetPastebin() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Pastebin
}
```

- [ ] **Step 3: Add field to Reload method**

In the Reload method's field-copy block (around line 1571, after `ExternalDiffCleanupAfterMs`), add:

```go
c.Pastebin = newCfg.Pastebin
```

- [ ] **Step 4: Initialize in CreateDefault**

In CreateDefault (around line 1615, after the `ExternalDiffCommands: []ExternalDiffCommand{}` line), add:

```go
Pastebin: []string{},
```

- [ ] **Step 5: Run Go tests**

Run: `go test ./internal/config/...`
Expected: PASS (no behavior change yet, just a new field with defaults)

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add Pastebin field to config"
```

---

### Task 2: Go API contracts — ConfigResponse and ConfigUpdateRequest

**Files:**

- Modify: `internal/api/contracts/config.go` (lines ~139 ConfigResponse, ~277 ConfigUpdateRequest)

- [ ] **Step 1: Add `Pastebin` to ConfigResponse**

In `ConfigResponse` struct (around line 145, after `ExternalDiffCommands`), add:

```go
Pastebin []string `json:"pastebin,omitempty"`
```

- [ ] **Step 2: Add `Pastebin` to ConfigUpdateRequest**

In `ConfigUpdateRequest` struct (around line 283, after `ExternalDiffCommands`), add:

```go
Pastebin []string `json:"pastebin,omitempty"`
```

Since `Pastebin` is `[]string` (not a struct), it follows the same pattern as `QuickLaunch []QuickLaunch` — a nil slice means "not provided" for partial updates. No separate update type needed.

- [ ] **Step 3: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`

- [ ] **Step 4: Verify generated types**

Check `assets/dashboard/src/lib/types.generated.ts` — should now include:

- In `ConfigResponse`: `pastebin?: string[];`
- In `ConfigUpdateRequest`: `pastebin?: string[];`

- [ ] **Step 5: Commit**

```bash
git add internal/api/contracts/config.go assets/dashboard/src/lib/types.generated.ts
git commit -m "feat(api): add Pastebin to config contracts and generated types"
```

---

### Task 3: Go API handlers — GET and UPDATE

**Files:**

- Modify: `internal/dashboard/handlers_config.go` (lines ~52 GET, ~327 UPDATE)

- [ ] **Step 1: Add pastebin to GET handler**

In `handleConfigGet`, after the ExternalDiffCommands handling (around line 82), add:

```go
pastebin := s.config.GetPastebin()
```

Then in the `ConfigResponse` construction (around line 96, after `ExternalDiffCommands`), add:

```go
Pastebin: pastebin,
```

- [ ] **Step 2: Add pastebin to UPDATE handler**

In `handleConfigUpdate`, after the ExternalDiffCommands handling (around line 339), add:

```go
if req.Pastebin != nil {
	cfg.Pastebin = make([]string, len(req.Pastebin))
	copy(cfg.Pastebin, req.Pastebin)
}
```

- [ ] **Step 3: Run Go tests**

Run: `go test ./internal/dashboard/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/dashboard/handlers_config.go
git commit -m "feat(api): handle Pastebin in config GET and UPDATE"
```

---

### Task 4: API documentation

**Files:**

- Modify: `docs/api.md`

- [ ] **Step 1: Document the pastebin field**

Add `pastebin` field documentation to the config section of `docs/api.md`, following the existing field documentation pattern. The field is `string[]`, optional, defaults to empty array. Each string is a text entry to paste into terminals.

- [ ] **Step 2: Commit**

```bash
git add docs/api.md
git commit -m "docs(api): document pastebin config field"
```

---

### Task 5: Frontend config form — `useConfigForm.ts` reducer

**Files:**

- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts`

- [ ] **Step 1: Add `pastebin` to ConfigSnapshot type**

In the `ConfigSnapshot` type (around line 18, after `externalDiffCommands`), add:

```typescript
pastebin: string[];
```

- [ ] **Step 2: Add `pastebin` to ConfigFormState type**

In `ConfigFormState` (after the `externalDiffCommands` field), add:

```typescript
pastebin: string[];
```

Also add input state for the "add new" form:

```typescript
newPastebinContent: string;
```

- [ ] **Step 3: Add action types**

In `ConfigFormAction` union (after the diff command actions), add:

```typescript
| { type: 'ADD_PASTEBIN'; content: string }
| { type: 'REMOVE_PASTEBIN'; index: number }
```

- [ ] **Step 4: Add reducer cases**

In the reducer, after the diff command cases, add:

```typescript
case 'ADD_PASTEBIN': {
  const updated = [...state.pastebin, action.content].sort((a, b) => a.localeCompare(b));
  return { ...state, pastebin: updated, newPastebinContent: '' };
}
case 'REMOVE_PASTEBIN': {
  const updated = state.pastebin.filter((_, i) => i !== action.index);
  return { ...state, pastebin: updated };
}
```

- [ ] **Step 5: Add `SET_FIELD` handling for `newPastebinContent`**

The existing `SET_FIELD` case already handles generic field updates. No additional code needed — it handles `newPastebinContent` automatically since it uses `action.field` as a key.

- [ ] **Step 6: Add `pastebin` to LOAD_CONFIG case**

In the `LOAD_CONFIG` action handler, add:

```typescript
pastebin: (data.pastebin || []).sort((a, b) => a.localeCompare(b)),
```

- [ ] **Step 7: Initialize `pastebin` in default state**

In the default state object, add:

```typescript
pastebin: [],
newPastebinContent: '',
```

- [ ] **Step 8: Add `pastebin` to hasChanges**

In the `hasChanges` callback, add a comparison:

```typescript
!arraysMatch(state.pastebin, state.originalConfig?.pastebin ?? []);
```

- [ ] **Step 9: Add `pastebin` to snapshotConfig**

In `snapshotConfig`, add:

```typescript
pastebin: state.pastebin,
```

- [ ] **Step 10: Run frontend tests**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add assets/dashboard/src/routes/config/useConfigForm.ts
git commit -m "feat(config): add pastebin to frontend form state"
```

---

### Task 6: Config tab UI — `PastebinTab.tsx`

**Files:**

- Create: `assets/dashboard/src/routes/config/PastebinTab.tsx`
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`

- [ ] **Step 1: Create `PastebinTab.tsx`**

Create `assets/dashboard/src/routes/config/PastebinTab.tsx` following the `CodeReviewTab.tsx` pattern:

```tsx
import React from 'react';

type PastebinTabProps = {
  pastebin: string[];
  newPastebinContent: string;
  dispatch: React.Dispatch<import('../useConfigForm').ConfigFormAction>;
};

export default function PastebinTab({ pastebin, newPastebinContent, dispatch }: PastebinTabProps) {
  return (
    <div className="wizard-step-content" data-step="10">
      <h2 className="wizard-step-content__title">Pastebin</h2>
      <p className="wizard-step-content__description">
        Saved text clips you can paste into any active terminal session.
      </p>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3>Clips</h3>
        </div>
        <div className="settings-section__body">
          {pastebin.length === 0 ? (
            <div className="empty-state-hint">No clips yet. Add one below.</div>
          ) : (
            <div className="item-list">
              {pastebin.map((content, index) => (
                <div className="item-list__item" key={index}>
                  <div className="item-list__item-content">
                    <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: '0.85rem' }}>
                      {content.length > 100 ? content.slice(0, 100) + '...' : content}
                    </pre>
                  </div>
                  <button
                    className="btn btn--sm btn--ghost btn--danger"
                    onClick={() => dispatch({ type: 'REMOVE_PASTEBIN', index })}
                  >
                    Remove
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3>Add clip</h3>
        </div>
        <div className="settings-section__body">
          <textarea
            value={newPastebinContent}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newPastebinContent', value: e.target.value })
            }
            placeholder="Enter text to save as a clip..."
            rows={4}
            style={{ width: '100%', resize: 'vertical' }}
          />
          <button
            className="btn btn--primary"
            disabled={!newPastebinContent.trim()}
            onClick={() => {
              if (newPastebinContent.trim()) {
                dispatch({ type: 'ADD_PASTEBIN', content: newPastebinContent.trim() });
              }
            }}
          >
            Add clip
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Add Pastebin tab to ConfigPage**

In `assets/dashboard/src/routes/ConfigPage.tsx`:

1. Add import at the top (after other tab imports around line 23):

```typescript
import PastebinTab from './config/PastebinTab';
```

2. Update constants (lines 36-58):

```typescript
const TOTAL_STEPS = 10;
```

Add `'Pastebin'` as last entry in `TABS` and `'pastebin'` as last entry in `TAB_SLUGS`.

3. Add `pastebin` to LOAD_CONFIG dispatch (around line 141, after `externalDiffCommands`):

```typescript
pastebin: (data.pastebin || []).sort((a, b) => a.localeCompare(b)),
```

4. Add `newPastebinContent: ''` to the LOAD_CONFIG dispatch.

5. Add `pastebin` to the `SET_ORIGINAL` snapshot (around line 213):

```typescript
pastebin: (data.pastebin || []).sort((a, b) => a.localeCompare(b)),
```

6. Add `pastebin` to the save payload (around line 570, after `external_diff_commands`):

```typescript
pastebin: state.pastebin,
```

7. Add tab render block (after Advanced tab around line 1454):

```tsx
{
  currentTab === 10 && (
    <PastebinTab
      pastebin={state.pastebin}
      newPastebinContent={state.newPastebinContent}
      dispatch={dispatch}
    />
  );
}
```

- [ ] **Step 3: Run frontend tests**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add assets/dashboard/src/routes/config/PastebinTab.tsx assets/dashboard/src/routes/ConfigPage.tsx
git commit -m "feat(config): add Pastebin tab to settings UI"
```

---

### Task 7: `PastebinDropdown` component

**Files:**

- Create: `assets/dashboard/src/components/PastebinDropdown.tsx`

- [ ] **Step 1: Create the component**

Create `assets/dashboard/src/components/PastebinDropdown.tsx`. Uses the same CSS module as ActionDropdown (`ActionDropdown.module.css`):

```tsx
import React from 'react';
import { useNavigate } from 'react-router-dom';
import styles from './ActionDropdown.module.css';

type PastebinDropdownProps = {
  entries: string[];
  onPaste: (content: string) => void;
  onClose: () => void;
  disabled?: boolean;
};

export default function PastebinDropdown({
  entries,
  onPaste,
  onClose,
  disabled,
}: PastebinDropdownProps) {
  const navigate = useNavigate();

  const handleManage = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate('/config?tab=pastebin');
  };

  const handlePaste = (e: React.MouseEvent, content: string) => {
    e.stopPropagation();
    onPaste(content);
    onClose();
  };

  return (
    <div className={styles.menu} role="menu">
      <div className={styles.sectionHeader}>
        <span className={styles.sectionLabel}>Pastebin</span>
        <button className={styles.manageLink} onClick={handleManage}>
          manage
        </button>
      </div>
      {!disabled && entries.length > 0 ? (
        entries.map((content, index) => (
          <button
            key={index}
            className={styles.item}
            onClick={(e) => handlePaste(e, content)}
            role="menuitem"
          >
            <span className={styles.itemLabel}>
              {content.split('\n')[0].length > 40
                ? content.split('\n')[0].slice(0, 40) + '...'
                : content.split('\n')[0]}
            </span>
          </button>
        ))
      ) : (
        <div className={styles.emptyState}>{disabled ? 'No active session' : 'No pastes yet'}</div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add assets/dashboard/src/components/PastebinDropdown.tsx
git commit -m "feat(ui): add PastebinDropdown component"
```

---

### Task 8: Pastebin button in SessionTabs + onPaste prop threading

**Files:**

- Modify: `assets/dashboard/src/components/SessionTabs.tsx`
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx`

- [ ] **Step 1: Add `onPaste` prop to SessionTabs**

In `assets/dashboard/src/components/SessionTabs.tsx`, add to `SessionTabsProps` (around line 124):

```typescript
onPaste?: (content: string) => void;
```

Destructure it from the component props.

- [ ] **Step 2: Add pastebin dropdown state**

After the spawn dropdown state (around line 161), add:

```typescript
const [pastebinOpen, setPastebinOpen] = useState(false);
const pastebinButtonRef = useRef<HTMLButtonElement | null>(null);
const pastebinMenuRef = useRef<HTMLDivElement | null>(null);
```

- [ ] **Step 3: Add pastebin menu position calculation**

After the spawn menu position useEffect (around line 248), add a similar effect for the pastebin dropdown. The `useConfig` hook is already available (imported at line 8). Access pastebin entries via:

```typescript
const { config } = useConfig();
const pastebinEntries = (config as any)?.pastebin || [];
```

Note: Until `ConfigResponse` is fully typed to include `pastebin`, use a cast. After gen-types runs, the proper type will be available.

The position/click-outside effects mirror the spawn dropdown pattern:

- useEffect for position calculation using `pastebinButtonRef` and `pastebinMenuRef`
- useEffect for click-outside detection using `pastebinButtonRef` and `pastebinMenuRef`

- [ ] **Step 4: Add `renderPastebinButton` method**

After `renderAddButton` (around line 697), add:

```tsx
const renderPastebinButton = () => {
  const hasActiveSession = Boolean(currentSessionId);
  const disabled = !hasActiveSession;

  return (
    <>
      <button
        ref={pastebinButtonRef}
        className="session-tab--add"
        onClick={(e) => {
          if (isLocked || disabled) return;
          e.stopPropagation();
          setPastebinOpen(!pastebinOpen);
        }}
        disabled={isLocked || disabled}
        aria-expanded={pastebinOpen}
        aria-haspopup="menu"
        aria-label="Pastebin"
        style={disabled || isLocked ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
      >
        <svg viewBox="0 0 32 32" fill="currentColor" width="20" height="20">
          <path d="M24.5,2H11.4c-0.6,0-1,0.4-1,1v4.5h-3c-0.6,0-1,0.4-1,1v6.8H4c-0.6,0-1,0.4-1,1v12.7 c0,0.6,0.4,1,1,1h13.5c0.6,0,1-0.4,1-1v-2.9h3c0.6,0,1-0.4,1-1v-3.2h2c0.6,0,1-0.4,1-1V3C25.5,2.4,25.1,2,24.5,2z M16.5,26H5V17h11.5V26z M20.5,22h-2v-4.7c0-0.6-0.4-1-1-1H8.4V9.5h12.1V22z M23.5,17.8h-1V8.5c0-0.6-0.4-1-1-1H13.4V4h10.1 V17.8z M7.2,20.4h7.1v1.3H7.2V20.4z M7.2,22.8h7.1v1.3H7.2V22.8z" />
        </svg>
      </button>
      {pastebinOpen &&
        createPortal(
          <div
            ref={pastebinMenuRef}
            style={{
              position: 'fixed',
              top: placementAbove ? 'auto' : `${menuPosition.top}px`,
              bottom: placementAbove ? `${window.innerHeight - menuPosition.top}px` : 'auto',
              left: `${menuPosition.left}px`,
              zIndex: 9999,
            }}
          >
            <PastebinDropdown
              entries={pastebinEntries}
              onPaste={(content) => onPaste?.(content)}
              onClose={() => setPastebinOpen(false)}
              disabled={disabled}
            />
          </div>,
          document.body
        )}
    </>
  );
};
```

The SVG path data is from the Pastebin logo at https://www.svgrepo.com/svg/473743/pastebin (CC0 license).

- [ ] **Step 5: Render pastebin button next to spawn button**

After the `{showAddButton && renderAddButton()}` (around line 739), add:

```tsx
{
  showAddButton && renderPastebinButton();
}
```

- [ ] **Step 6: Add import for PastebinDropdown**

At the top of SessionTabs.tsx, add:

```typescript
import PastebinDropdown from './PastebinDropdown';
```

- [ ] **Step 7: Pass `onPaste` from SessionDetailPage**

In `assets/dashboard/src/routes/SessionDetailPage.tsx`, pass an `onPaste` callback to `SessionTabs`:

```tsx
<SessionTabs
  sessions={currentWorkspace.sessions || []}
  currentSessionId={sessionId}
  workspace={currentWorkspace}
  onPaste={(content: string) => {
    terminalStreamRef.current?.sendInput(content);
  }}
/>
```

There are two `<SessionTabs>` render sites in SessionDetailPage (around lines 566 and 623). Add `onPaste` to both.

- [ ] **Step 8: Run frontend tests**

Run: `./test.sh --quick`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add assets/dashboard/src/components/SessionTabs.tsx assets/dashboard/src/routes/SessionDetailPage.tsx
git commit -m "feat(ui): add pastebin button to session tabs with terminal paste"
```

---

### Task 9: Integration verification

- [ ] **Step 1: Run full test suite**

Run: `./test.sh`
Expected: All tests pass.

- [ ] **Step 2: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds.

- [ ] **Step 3: Verify no lint/typecheck errors**

Run: `./format.sh`
Expected: Exit code 0 or 2 (success).

- [ ] **Step 4: Commit any formatting fixes**

If `format.sh` made changes:

```bash
git add -A
git commit -m "style: format pastebin feature files"
```
