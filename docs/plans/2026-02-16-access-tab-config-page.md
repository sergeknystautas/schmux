# Access Tab in Config Page — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Add a new "Access" tab to the Config page that consolidates Network Access, Remote Access (Cloudflare tunnel), and GitHub Authentication settings — all currently buried in the Advanced tab.

**Architecture:** Move existing Network/Auth UI sections from Advanced (step 5) into a new step 6 "Access" tab. Add new Remote Access fields (disabled, PIN, timeout, ntfy topic, notify command) to the same tab. The PIN uses a dedicated API endpoint (`POST /api/remote-access/set-pin`) while the other fields flow through the normal `updateConfig` mechanism via `remote_access` in `ConfigUpdateRequest`. The `RemoteAccessPanel` in the sidebar should link to the new Access tab when PIN isn't set, instead of showing a CLI command.

**Tech Stack:** React (TypeScript), existing Go API endpoints (no backend changes needed)

---

### Task 1: Add `setRemoteAccessPin` API function

The frontend has no function to call `POST /api/remote-access/set-pin`. Add one.

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts` (~line 919, after the existing remote access functions)

**Step 1: Add the API function**

Add after the `remoteAccessOff` function (around line 919):

```typescript
export async function setRemoteAccessPin(pin: string): Promise<void> {
  const response = await fetch('/api/remote-access/set-pin', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pin }),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to set PIN');
  }
}
```

**Step 2: Run tests**

```bash
cd assets/dashboard && npx vitest run src/lib/api.test.ts
```

Expected: PASS (no test touches this new function)

**Step 3: Commit**

```bash
git commit -m "Add setRemoteAccessPin API function for web dashboard"
```

---

### Task 2: Add new "Access" tab — update tab constants and step count

Bump `TOTAL_STEPS` from 5 to 6. Add "Access" to the tab list. Insert it as step 5 (before Advanced, which becomes step 6). This preserves the pattern of Advanced being the last tab.

**Files:**

- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`

**Step 1: Update tab constants (lines 29-33)**

Change:

```typescript
const TOTAL_STEPS = 5;
const TABS = ['Workspaces', 'Sessions', 'Quick Launch', 'Code Review', 'Advanced'];
const TAB_SLUGS = ['workspaces', 'sessions', 'quicklaunch', 'codereview', 'advanced'];
```

To:

```typescript
const TOTAL_STEPS = 6;
const TABS = ['Workspaces', 'Sessions', 'Quick Launch', 'Code Review', 'Access', 'Advanced'];
const TAB_SLUGS = ['workspaces', 'sessions', 'quicklaunch', 'codereview', 'access', 'advanced'];
```

**Step 2: Update step validation (around line 1229-1235)**

Change:

```typescript
const stepValid = {
  1: workspacePath.trim().length > 0 && repos.length > 0,
  2: true,
  3: true,
  4: true,
  5: true,
};
```

To:

```typescript
const stepValid = {
  1: workspacePath.trim().length > 0 && repos.length > 0,
  2: true,
  3: true,
  4: true,
  5: true,
  6: true,
};
```

**Step 3: Update stepErrors initial state (around line 334-340)**

Add `6: null` to the initial stepErrors object.

**Step 4: Update validateStep function (around line 570-597)**

The existing `step === 5` validation for terminal settings now needs to apply to step 6 instead (since Advanced moved from 5 to 6). Change the condition:

```typescript
} else if (step === 5) {
```

to:

```typescript
} else if (step === 6) {
```

And add a no-op case for step 5 (Access tab — always valid):

```typescript
} else if (step === 5) {
  // Access settings are always valid
}
```

**Step 5: Update saveCurrentStep (around line 599-691)**

The `saveCurrentStep` function sends ALL config fields at once regardless of which step you're on. No changes needed to the update payload — we'll add the `remote_access` field in Task 4.

**Step 6: Commit**

```bash
git commit -m "Add Access tab slot to config page (step 5, Advanced becomes step 6)"
```

---

### Task 3: Add remote access state variables and wire into load/save/change-detection

Add `useState` hooks for the 4 remote access fields that go through `updateConfig` (disabled, timeout, ntfy topic, notify command). PIN is handled separately via its own API. Wire them into config load, save, and change detection.

**Files:**

- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`

**Step 1: Add state variables (after lore state, around line 210)**

```typescript
// Remote access state
const [remoteAccessDisabled, setRemoteAccessDisabled] = useState(false);
const [remoteAccessTimeoutMinutes, setRemoteAccessTimeoutMinutes] = useState(0);
const [remoteAccessNtfyTopic, setRemoteAccessNtfyTopic] = useState('');
const [remoteAccessNotifyCommand, setRemoteAccessNotifyCommand] = useState('');
const [remoteAccessPinHashSet, setRemoteAccessPinHashSet] = useState(false);
```

**Step 2: Add to ConfigSnapshot type (around line 52-95)**

Add these fields to the `ConfigSnapshot` type:

```typescript
remoteAccessDisabled: boolean;
remoteAccessTimeoutMinutes: number;
remoteAccessNtfyTopic: string;
remoteAccessNotifyCommand: string;
```

(Don't include `pinHashSet` — it's not part of the normal save flow.)

**Step 3: Load from config response (around line 432-435, after lore loading)**

```typescript
setRemoteAccessDisabled(data.remote_access?.disabled || false);
setRemoteAccessTimeoutMinutes(data.remote_access?.timeout_minutes || 0);
setRemoteAccessNtfyTopic(data.remote_access?.notify?.ntfy_topic || '');
setRemoteAccessNotifyCommand(data.remote_access?.notify?.command || '');
setRemoteAccessPinHashSet(data.remote_access?.pin_hash_set || false);
```

**Step 4: Add to originalConfig snapshot (around line 481-484, after loreAutoPR)**

```typescript
remoteAccessDisabled: data.remote_access?.disabled || false,
remoteAccessTimeoutMinutes: data.remote_access?.timeout_minutes || 0,
remoteAccessNtfyTopic: data.remote_access?.notify?.ntfy_topic || '',
remoteAccessNotifyCommand: data.remote_access?.notify?.command || '',
```

**Step 5: Add to hasChanges current snapshot (around line 263-267, after loreAutoPR)**

```typescript
remoteAccessDisabled,
remoteAccessTimeoutMinutes,
remoteAccessNtfyTopic,
remoteAccessNotifyCommand,
```

**Step 6: Add to hasChanges comparison (around line 303-306, after loreAutoPR comparison)**

```typescript
current.remoteAccessDisabled !== originalConfig.remoteAccessDisabled ||
  current.remoteAccessTimeoutMinutes !== originalConfig.remoteAccessTimeoutMinutes ||
  current.remoteAccessNtfyTopic !== originalConfig.remoteAccessNtfyTopic ||
  current.remoteAccessNotifyCommand !== originalConfig.remoteAccessNotifyCommand;
```

**Step 7: Add to updateRequest in saveCurrentStep (around line 689-691, after lore block)**

```typescript
remote_access: {
  disabled: remoteAccessDisabled,
  timeout_minutes: remoteAccessTimeoutMinutes,
  notify: {
    ntfy_topic: remoteAccessNtfyTopic,
    command: remoteAccessNotifyCommand,
  },
},
```

**Step 8: Commit**

```bash
git commit -m "Wire remote access state into config load/save/change-detection"
```

---

### Task 4: Render the Access tab UI

Build the step 5 content with three sections: Network, Remote Access, and Authentication. Move the existing Network and Authentication JSX from step 5 (now step 6) into the new step 5 block. Add the new Remote Access section between them.

**Files:**

- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`
- Modify: `assets/dashboard/src/lib/api.ts` (import `setRemoteAccessPin`)

**Step 1: Add import for `setRemoteAccessPin`**

In `ConfigPage.tsx`, add `setRemoteAccessPin` to the import from `'../lib/api'` (line 6).

**Step 2: Add PIN state for the inline set-pin form**

Near the other remote access state variables:

```typescript
const [pinInput, setPinInput] = useState('');
const [pinConfirm, setPinConfirm] = useState('');
const [pinSaving, setPinSaving] = useState(false);
const [pinError, setPinError] = useState('');
const [pinSuccess, setPinSuccess] = useState('');
```

**Step 3: Add PIN save handler**

Near the other handler functions (e.g., near `openAuthSecretsModal`):

```typescript
const handleSetPin = async () => {
  setPinError('');
  setPinSuccess('');
  if (!pinInput.trim()) {
    setPinError('PIN cannot be empty');
    return;
  }
  if (pinInput.length < 4) {
    setPinError('PIN must be at least 4 characters');
    return;
  }
  if (pinInput !== pinConfirm) {
    setPinError('PINs do not match');
    return;
  }
  setPinSaving(true);
  try {
    await setRemoteAccessPin(pinInput);
    setRemoteAccessPinHashSet(true);
    setPinInput('');
    setPinConfirm('');
    setPinSuccess('PIN updated');
    reloadConfig();
  } catch (err) {
    setPinError(getErrorMessage(err, 'Failed to set PIN'));
  } finally {
    setPinSaving(false);
  }
};
```

**Step 4: Add the Access tab content (step 5)**

Insert a new `{currentStep === 5 && (` block BEFORE the existing `{currentStep === 5 && (` block (which will become `{currentStep === 6 && (`).

The new Access tab step 5 content:

```tsx
{
  currentStep === 5 && (
    <div className="wizard-step-content" data-step="5">
      <h2 className="wizard-step-content__title">Access</h2>
      <p className="wizard-step-content__description">
        Control how the dashboard is accessed — local network, remote tunneling, and authentication.
      </p>

      {/* --- Network section: CUT from old step 5 --- */}
      {/* (Move the existing Network settings-section here verbatim) */}

      {/* --- Remote Access section: NEW --- */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Remote Access</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={!remoteAccessDisabled}
                onChange={(e) => setRemoteAccessDisabled(!e.target.checked)}
              />
              <span>Enable remote access</span>
            </label>
            <p className="form-group__hint">
              Allow accessing the dashboard remotely via a Cloudflare tunnel.
            </p>
          </div>

          {!remoteAccessDisabled && (
            <>
              <div className="form-group">
                <label className="form-group__label">Access PIN</label>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-xs)' }}>
                  {remoteAccessPinHashSet && (
                    <p className="form-group__hint" style={{ color: 'var(--color-success)' }}>
                      PIN is configured
                    </p>
                  )}
                  <input
                    type="password"
                    className="input"
                    style={{ maxWidth: '240px' }}
                    placeholder={
                      remoteAccessPinHashSet ? 'New PIN (leave blank to keep)' : 'Enter PIN'
                    }
                    value={pinInput}
                    onChange={(e) => setPinInput(e.target.value)}
                  />
                  {pinInput && (
                    <input
                      type="password"
                      className="input"
                      style={{ maxWidth: '240px' }}
                      placeholder="Confirm PIN"
                      value={pinConfirm}
                      onChange={(e) => setPinConfirm(e.target.value)}
                    />
                  )}
                  {pinInput && (
                    <button
                      type="button"
                      className="btn btn--primary"
                      style={{ alignSelf: 'flex-start' }}
                      onClick={handleSetPin}
                      disabled={pinSaving}
                    >
                      {pinSaving ? 'Saving...' : remoteAccessPinHashSet ? 'Update PIN' : 'Set PIN'}
                    </button>
                  )}
                  {pinError && <p className="form-group__error">{pinError}</p>}
                  {pinSuccess && (
                    <p className="form-group__hint" style={{ color: 'var(--color-success)' }}>
                      {pinSuccess}
                    </p>
                  )}
                </div>
                <p className="form-group__hint">
                  Required to start a remote tunnel. You&apos;ll enter this PIN when connecting from
                  another device.
                </p>
              </div>

              <div className="form-group">
                <label className="form-group__label">Timeout (minutes)</label>
                <input
                  type="number"
                  className="input input--compact"
                  style={{ maxWidth: '120px' }}
                  min="0"
                  value={remoteAccessTimeoutMinutes}
                  onChange={(e) => setRemoteAccessTimeoutMinutes(parseInt(e.target.value) || 0)}
                />
                <p className="form-group__hint">
                  Auto-stop the tunnel after this many minutes. 0 means no timeout.
                </p>
              </div>

              <div className="form-group">
                <label className="form-group__label">ntfy Topic</label>
                <input
                  type="text"
                  className="input"
                  style={{ maxWidth: '300px' }}
                  placeholder="my-schmux-notifications"
                  value={remoteAccessNtfyTopic}
                  onChange={(e) => setRemoteAccessNtfyTopic(e.target.value)}
                />
                <p className="form-group__hint">
                  Receive a push notification with the connection URL via{' '}
                  <a href="https://ntfy.sh" target="_blank" rel="noopener noreferrer">
                    ntfy.sh
                  </a>
                  . Install the ntfy app on your phone and subscribe to this topic.
                </p>
              </div>

              <div className="form-group">
                <label className="form-group__label">Notify Command</label>
                <input
                  type="text"
                  className="input"
                  placeholder="echo $SCHMUX_REMOTE_URL | pbcopy"
                  value={remoteAccessNotifyCommand}
                  onChange={(e) => setRemoteAccessNotifyCommand(e.target.value)}
                />
                <p className="form-group__hint">
                  Shell command to run when the tunnel connects. The URL is available as{' '}
                  <code>$SCHMUX_REMOTE_URL</code>.
                </p>
              </div>
            </>
          )}
        </div>
      </div>

      {/* --- Authentication section: CUT from old step 5 --- */}
      {/* (Move the existing Authentication settings-section here verbatim) */}
    </div>
  );
}
```

**Step 5: Move Network and Authentication sections from old step 5**

From the existing `data-step="5"` block (Advanced), **cut** these two `settings-section` divs:

1. The "Network" section (Dashboard Access radio buttons, lines ~2517-2574)
2. The "Authentication" section (GitHub auth checkbox + all conditional fields, lines ~2576-2762)

Paste them into the new step 5 block above (replacing the placeholder comments).

The remaining Advanced tab keeps: Lore, NudgeNik, Branch Suggestion, Conflict Resolution, Notifications, Model Versions, Terminal, Sessions, Xterm, Logs.

**Step 6: Update old step 5 to step 6**

Change the Advanced tab's `data-step="5"` to `data-step="6"` and its condition from `currentStep === 5` to `currentStep === 6`.

**Step 7: Commit**

```bash
git commit -m "Add Access tab with remote access, network, and auth settings"
```

---

### Task 5: Update RemoteAccessPanel sidebar to link to Access tab

Instead of showing `schmux remote set-pin`, link to the config page Access tab.

**Files:**

- Modify: `assets/dashboard/src/components/RemoteAccessPanel.tsx`

**Step 1: Add Link import and update the warning**

```tsx
import { Link } from 'react-router-dom';
```

Change:

```tsx
{
  !pinHashSet && remoteAccessStatus.state === 'off' && (
    <div className="remote-access-panel__warning">
      Set a PIN first: <code>schmux remote set-pin</code>
    </div>
  );
}
```

To:

```tsx
{
  !pinHashSet && remoteAccessStatus.state === 'off' && (
    <div className="remote-access-panel__warning">
      <Link to="/config?tab=access">Set a PIN</Link> to enable remote access.
    </div>
  );
}
```

**Step 2: Commit**

```bash
git commit -m "Link RemoteAccessPanel warning to Access tab instead of CLI command"
```

---

### Task 6: Build dashboard and verify

**Step 1: Build the dashboard**

```bash
go run ./cmd/build-dashboard
```

Expected: Build succeeds with no errors.

**Step 2: Run unit tests**

```bash
./test.sh
```

Expected: All tests pass.

**Step 3: Manual smoke test**

```bash
./schmux daemon-run
```

Open http://localhost:7337/config?tab=access and verify:

- "Access" tab appears between "Code Review" and "Advanced"
- Network section (local/network radio) renders
- Remote Access section renders with all 5 fields
- Authentication section renders with GitHub OAuth
- Saving works (changes persist on reload)
- Setting a PIN shows success feedback
- Advanced tab no longer contains Network/Auth sections
- RemoteAccessPanel sidebar links to `/config?tab=access`

**Step 4: Commit any fixes, then final commit**

```bash
git commit -m "Verify Access tab end-to-end"
```

---

### Task 7: Write scenario test

**Files:**

- Create: `test/scenarios/configure-remote-access-settings.md`

**Step 1: Write the scenario**

```markdown
# Configure remote access settings

A user wants to configure remote access from the web dashboard instead
of the CLI. They navigate to the config page, open the Access tab, and
configure the tunnel settings — PIN, ntfy topic, timeout, and notify
command.

## Preconditions

- The daemon is running
- At least one repository is configured

## Verifications

- The config page loads and the "Access" tab is accessible at /config?tab=access
- The Access tab contains three sections: Network, Remote Access, and Authentication
- The Remote Access section has an "Enable remote access" checkbox (checked by default)
- Unchecking "Enable remote access" hides the PIN, timeout, ntfy, and command fields
- Re-checking it shows them again
- The PIN field shows "PIN is configured" or not based on pin_hash_set from GET /api/config
- Typing a PIN reveals the confirm field and a "Set PIN" button
- Entering mismatched PINs shows "PINs do not match" error
- Entering matching PINs (at least 4 chars) and clicking "Set PIN" calls POST /api/remote-access/set-pin
- After setting PIN, the status text changes to "PIN is configured"
- Setting ntfy topic to "test-topic", timeout to 30, and notify command to "echo test" then saving persists via POST /api/config
- GET /api/config after save shows remote_access.notify.ntfy_topic="test-topic", remote_access.timeout_minutes=30, remote_access.notify.command="echo test"
- The Network and Authentication sections are present in the Access tab (moved from Advanced)
- The Advanced tab no longer contains Network or Authentication sections
```

**Step 2: Commit**

```bash
git commit -m "Add scenario test for remote access settings in Access tab"
```

---

### Task 8: Generate scenario test and run

**Step 1: Generate Playwright test from scenario**

Use `generate-scenario-tests` skill to generate the Playwright test from the scenario file.

**Step 2: Run scenario tests**

```bash
./test.sh --scenarios
```

Expected: All scenario tests pass including the new one.

**Step 3: Commit generated test if any changes**

```bash
git commit -m "Generate Playwright test for remote access settings scenario"
```
