# Client Performance Recording Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent in a schmux checkout can read a recording of what the user's browser was doing while the dashboard was slow, fix the cause, and compare a second recording against its fix.

**Architecture:** A browser-side recorder (`lib/clientPerf.ts`, a module singleton like `inputLatency`) keeps ring buffers of main-thread activity and builds one JSON file per send. A dev-only endpoint, `POST /api/client-performance/session`, ensures a chat session exists in a schmux checkout on the branch `client-performance`; the browser keeps the returned ids. The chat composer uploads the file as a workspace attachment and appends its path to the message. The daemon analyzes nothing.

**Tech Stack:** Go (chi, go:embed), React 19 + TypeScript (Vite, Vitest, React Testing Library, `fake-indexeddb`), Playwright scenarios.

**Spec:** `docs/superpowers/specs/2026-10-07-client-performance-recording-design.md`

## Spike result

Question: do all sockets created through `transport.createWebSocket` receive messages by assigning `onmessage`, so a wrapper can time handlers by intercepting that property? **Yes.** `assets/dashboard/src/lib/chat/socket.ts:123`, `assets/dashboard/src/lib/terminalStream.ts:1260`, `assets/dashboard/src/hooks/useLogStream.ts:98`, `assets/dashboard/src/hooks/useSessionsWebSocket.ts:298` all assign `ws.onmessage`; none uses `addEventListener('message')`. The only socket outside `transport` is `ConnectionProgressModal.tsx:90`, which the spec excludes.

## Global Constraints

- Dev-only. The endpoint registers under `if s.devMode` in `internal/dashboard/server.go`; the pane renders only when `isDevMode && config.client_performance?.enabled`.
- The daemon stores nothing about the performance chat. No `state.json` field.
- The branch name is the constant `client-performance`.
- Stall rule: a timeline second whose event loop delay or long task total reached 100ms. Used by the file and the pane, defined once in `clientPerf.ts`.
- Caps: timeline 3600; `longTasks` 2000; `interactions` 2000; `commits` 2000; `websocket` 3600 per-second rows and 2000 individual; `fetches` 2000; `terminals` 3600; `chatLoads` 200; `memory` 360; `navigation` 500; `errors` 100.
- Thresholds: interactions 100ms; commits over 16ms; individual WebSocket messages over 5ms handler time or 50KB.
- File name: `client-perf-<timestamp>-<browser id>.json`.
- All dashboard CSS uses design tokens from `docs/dashboard-style-guide.md`; no hardcoded palette colors. Run the `dashboard-style-check` skill before presenting the pane.
- Never edit `assets/dashboard/src/lib/types.generated.ts`; run `go run ./cmd/gen-types`.
- Frontend tests run only through `./test.sh --quick` from the repo root. Definition of done is `./test.sh` (full), `./badcode.sh`, `./format.sh`.
- Commits go through the `/commit` skill, never `git commit`.
- API-related package changes require a `docs/api.md` update in the same commit (`scripts/check-api-docs.sh`).
- Tests follow `docs/testing.md` rules 1 to 12: fake clocks, no retried assertions, lowest gate.

## Review Focus

1. **A browser without `longtask`, `event`, or `performance.memory`** (Safari, Firefox). Expected: the recorder notes the missing observer in `environment.unsupported` and keeps recording everything else. Pinned in Task 4.
2. **A binary WebSocket frame** (terminal output arrives as `ArrayBuffer`). Expected: counted by `byteLength` under type `binary`, never passed to `JSON.parse`. Pinned in Task 5.
3. **A recording longer than an hour.** Expected: the oldest timeline rows drop, but the stall count the pane shows keeps counting every stall since Start. Pinned in Task 3.
4. **Two tabs of the same browser recording at once.** Expected: each tab persists under its own key, so neither tab's reload restores the other's buffers, and Stop in one tab does not empty the other. Pinned in Task 3.
5. **Upload succeeds, send fails** (chat socket closed between upload and send). Expected: the buffers are not emptied, the uploaded path stays as a chip in the composer, and the next send reuses it without a second upload. Pinned in Task 8.

---

### Task 1: Config block in Go

**Files:**

- Modify: `internal/config/config.go:106-108` (field), `:338-342` (struct), `:1089-1091` (legacy check), `:1161-1163` (migration), `:2480-2492` (getters)
- Modify: `internal/api/contracts/config.go:190` (response), `:252-262` (types), `:381` (update)
- Modify: `internal/dashboard/handlers_config.go:267-270` (GET), `:733-748` (PATCH)
- Test: `internal/config/config_client_performance_test.go` (create), `internal/dashboard/api_contract_test.go` (add one test)
- Modify: `docs/api.md:1446` (add entry after `chat_load_profiling_enabled`)
- Regenerate: `assets/dashboard/src/lib/types.generated.ts` via `go run ./cmd/gen-types`

**Interfaces:**

- Produces: `config.ClientPerformanceConfig{Enabled *bool; Repo string; Target string}`, `(*Config).GetClientPerformanceEnabled() bool`, `GetClientPerformanceRepo() string`, `GetClientPerformanceTarget() string`; `contracts.ClientPerformance{Enabled bool; Repo, Target string}` on `ConfigResponse.ClientPerformance` (`json:"client_performance"`); `contracts.ClientPerformanceUpdate{Enabled *bool; Repo, Target *string}` on `ConfigUpdateRequest.ClientPerformance`.

- [ ] **Step 1: Write the failing config tests**

Create `internal/config/config_client_performance_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientPerformanceDefaults(t *testing.T) {
	cfg := CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	if cfg.GetClientPerformanceEnabled() {
		t.Error("enabled should default to false")
	}
	if cfg.GetClientPerformanceRepo() != "" || cfg.GetClientPerformanceTarget() != "" {
		t.Error("repo and target should default to empty")
	}
}

func TestClientPerformanceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := CreateDefault(path)
	enabled := true
	cfg.ClientPerformance = &ClientPerformanceConfig{Enabled: &enabled, Repo: "schmux", Target: " claude-opus-4-6 "}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.GetClientPerformanceEnabled() {
		t.Error("enabled lost on reload")
	}
	if loaded.GetClientPerformanceRepo() != "schmux" {
		t.Errorf("repo = %q", loaded.GetClientPerformanceRepo())
	}
	if loaded.GetClientPerformanceTarget() != "claude-opus-4-6" {
		t.Errorf("target = %q, want trimmed", loaded.GetClientPerformanceTarget())
	}
	raw, _ := os.ReadFile(path)
	if !contains(string(raw), `"client_performance"`) {
		t.Error("client_performance key missing from file")
	}
}

func TestClientPerformanceLegacyTargetMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := CreateDefault(path)
	cfg.ClientPerformance = &ClientPerformanceConfig{Target: "claude-opus"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientPerformance.Target != "claude-opus-4-6" {
		t.Errorf("target = %q, want migrated claude-opus-4-6", loaded.ClientPerformance.Target)
	}
}
```

If `contains` does not exist in the package tests, use `strings.Contains`. Check the migration expectation against `internal/config/config_test.go:2642`, which pins `claude-opus` → `claude-opus-4-6` for `IOWorkspaceTelemetry`; use the same pair.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config -run 'TestClientPerformance' -v`
Expected: build failure, `undefined: ClientPerformanceConfig`.

- [ ] **Step 3: Add the struct, field, getters, and migration hooks**

In `internal/config/config.go`, after line 107 (`IOWorkspaceTelemetry`):

```go
	ClientPerformance          *ClientPerformanceConfig    `json:"client_performance,omitempty"`
```

After `IOWorkspaceTelemetryConfig` (line 342):

```go
// ClientPerformanceConfig gates the browser performance recorder and names
// the repo and target of the chat an agent diagnoses recordings in.
type ClientPerformanceConfig struct {
	Enabled *bool  `json:"enabled,omitempty"` // enable/disable the recorder and sidebar pane
	Repo    string `json:"repo,omitempty"`    // configured repo name; must be a schmux checkout
	Target  string `json:"target,omitempty"`  // chat target for the performance chat
}
```

After the `IOWorkspaceTelemetry` legacy check at line 1089:

```go
	if c.ClientPerformance != nil && isLegacy(c.ClientPerformance.Target) {
		return true
	}
```

After the `IOWorkspaceTelemetry` migration at line 1161:

```go
	if c.ClientPerformance != nil {
		migrateTarget(&c.ClientPerformance.Target)
	}
```

After `GetIOWorkspaceTelemetryTarget` (line 2492):

```go
// GetClientPerformanceEnabled returns whether client performance recording is enabled.
func (c *Config) GetClientPerformanceEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ClientPerformance == nil || c.ClientPerformance.Enabled == nil {
		return false
	}
	return *c.ClientPerformance.Enabled
}

// GetClientPerformanceRepo returns the configured repo name for the performance chat.
func (c *Config) GetClientPerformanceRepo() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ClientPerformance == nil {
		return ""
	}
	return strings.TrimSpace(c.ClientPerformance.Repo)
}

// GetClientPerformanceTarget returns the configured target for the performance chat.
func (c *Config) GetClientPerformanceTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ClientPerformance == nil {
		return ""
	}
	return strings.TrimSpace(c.ClientPerformance.Target)
}
```

- [ ] **Step 4: Run config tests**

Run: `go test ./internal/config -run 'TestClientPerformance' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Add the contracts**

In `internal/api/contracts/config.go`, after line 190 (`IOWorkspaceTelemetry` in `ConfigResponse`):

```go
	ClientPerformance          ClientPerformance      `json:"client_performance"`
```

After `IOWorkspaceTelemetryUpdate` (line 262):

```go
// ClientPerformance represents client performance recording configuration in the API response.
type ClientPerformance struct {
	Enabled bool   `json:"enabled"`
	Repo    string `json:"repo"`
	Target  string `json:"target"`
}

// ClientPerformanceUpdate represents partial client performance config updates.
type ClientPerformanceUpdate struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Repo    *string `json:"repo,omitempty"`
	Target  *string `json:"target,omitempty"`
}
```

After line 381 (`IOWorkspaceTelemetry` in `ConfigUpdateRequest`):

```go
	ClientPerformance          *ClientPerformanceUpdate    `json:"client_performance,omitempty"`
```

- [ ] **Step 6: Write the failing handler test**

Append to `internal/dashboard/api_contract_test.go`, following the POST `/api/config` tests at lines 529-601:

```go
func TestAPIContract_ClientPerformanceConfigRoundTrip(t *testing.T) {
	server, _, _ := newTestServer(t)

	body := []byte(`{"client_performance":{"enabled":true,"repo":"schmux","target":"command"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.configHandlers.handleConfigUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body %s", rr.Code, rr.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	server.configHandlers.handleConfig(getRR, getReq)
	var resp contracts.ConfigResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ClientPerformance.Enabled || resp.ClientPerformance.Repo != "schmux" || resp.ClientPerformance.Target != "command" {
		t.Errorf("client_performance = %+v", resp.ClientPerformance)
	}
}
```

Use the handler names the neighboring tests at lines 504-601 use; if they differ from `handleConfigUpdate`/`handleConfig`, match them.

- [ ] **Step 7: Run to verify failure**

Run: `go test ./internal/dashboard -run TestAPIContract_ClientPerformanceConfigRoundTrip -v`
Expected: FAIL, `resp.ClientPerformance` zero.

- [ ] **Step 8: Wire GET and PATCH**

In `internal/dashboard/handlers_config.go` after the `IOWorkspaceTelemetry` block in the GET response (line 270):

```go
		ClientPerformance: contracts.ClientPerformance{
			Enabled: h.config.GetClientPerformanceEnabled(),
			Repo:    h.config.GetClientPerformanceRepo(),
			Target:  h.config.GetClientPerformanceTarget(),
		},
```

After the `IOWorkspaceTelemetry` PATCH block (line 748):

```go
	if req.ClientPerformance != nil {
		if cfg.ClientPerformance == nil {
			cfg.ClientPerformance = &config.ClientPerformanceConfig{}
		}
		if req.ClientPerformance.Enabled != nil {
			enabled := *req.ClientPerformance.Enabled
			cfg.ClientPerformance.Enabled = &enabled
		}
		if req.ClientPerformance.Repo != nil {
			cfg.ClientPerformance.Repo = strings.TrimSpace(*req.ClientPerformance.Repo)
		}
		if req.ClientPerformance.Target != nil {
			cfg.ClientPerformance.Target = strings.TrimSpace(*req.ClientPerformance.Target)
		}
		// Nil out if everything is at zero value
		if (cfg.ClientPerformance.Enabled == nil || !*cfg.ClientPerformance.Enabled) && cfg.ClientPerformance.Repo == "" && cfg.ClientPerformance.Target == "" {
			cfg.ClientPerformance = nil
		}
	}
```

- [ ] **Step 9: Run handler test, regenerate types**

Run: `go test ./internal/dashboard -run TestAPIContract_ClientPerformanceConfigRoundTrip -v`
Expected: PASS.

Run: `go run ./cmd/gen-types`
Expected: `types.generated.ts` gains `ClientPerformance`, `ClientPerformanceUpdate`, and `client_performance` on `ConfigResponse` and `ConfigUpdateRequest`. Verify with `grep -n 'client_performance' assets/dashboard/src/lib/types.generated.ts`.

- [ ] **Step 10: Document the config key**

In `docs/api.md` after the `chat_load_profiling_enabled` entry (line 1446):

```markdown
**`client_performance`** (object, optional): Dev-only browser performance recorder. `enabled` (boolean, default `false`) turns on the recorder and the Client Performance sidebar pane; `repo` (string) names the configured repo the performance chat is spawned in, which must be a schmux checkout; `target` (string) is the chat target. Hot-reloadable. See `docs/client-performance.md`.
```

- [ ] **Step 11: Commit**

Run `./format.sh`, then `/commit` with message `feat(config): add client_performance config block`.

---

### Task 2: Advanced tab section

**Files:**

- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts:167-169` and `:365-366`
- Modify: `assets/dashboard/src/routes/config/buildConfigUpdate.ts:108-111`
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx:217-218` and `:1279-1280`
- Modify: `assets/dashboard/src/routes/config/AdvancedTab.tsx:9-12`, `:43-46`, `:674-712`
- Modify: `assets/dashboard/src/routes/config/ConfigPanelProps.ts` (if `AdvancedTabProps` lives there)
- Test: `assets/dashboard/src/routes/config/AdvancedTab.test.tsx`, `assets/dashboard/src/routes/config/buildConfigUpdate.test.ts` (add cases)
- Modify: `docs/settings.md:49`, `:58`

**Interfaces:**

- Produces: form fields `clientPerformanceEnabled: boolean`, `clientPerformanceRepo: string`, `clientPerformanceTarget: string`; `buildConfigUpdate` emits `client_performance: { enabled, repo, target }`.

- [ ] **Step 1: Write the failing tests**

In `buildConfigUpdate.test.ts`, add beside the desync case:

```ts
it('emits client_performance from the form fields', () => {
  const state = {
    ...baseState,
    clientPerformanceEnabled: true,
    clientPerformanceRepo: 'schmux',
    clientPerformanceTarget: 'claude-opus-4-6',
  };
  expect(buildConfigUpdate(state).client_performance).toEqual({
    enabled: true,
    repo: 'schmux',
    target: 'claude-opus-4-6',
  });
});
```

In `AdvancedTab.test.tsx`, following the existing desync section test:

```ts
it('renders the Client Performance section in dev mode and saves its fields', async () => {
  const setField = vi.fn();
  render(
    <AdvancedTab
      {...baseProps}
      isDevMode
      repos={[{ name: 'schmux', url: 'https://github.com/x/schmux' }]}
      clientPerformanceEnabled={false}
      clientPerformanceRepo=""
      clientPerformanceTarget=""
      setField={setField}
    />
  );
  await userEvent.click(screen.getByLabelText('Enable client performance recording'));
  expect(setField).toHaveBeenCalledWith('clientPerformanceEnabled', true);
  await userEvent.selectOptions(screen.getByLabelText('Repo'), 'schmux');
  expect(setField).toHaveBeenCalledWith('clientPerformanceRepo', 'schmux');
});

it('hides the Client Performance section outside dev mode', () => {
  render(<AdvancedTab {...baseProps} isDevMode={false} />);
  expect(screen.queryByText('Client Performance')).toBeNull();
});
```

Use the existing `baseProps` fixture in that test file; if `AdvancedTab` does not already receive `repos`, add it to its props in Step 3 and pass `state.repos` from `ConfigPage.tsx` the way the Repositories tab does.

- [ ] **Step 2: Run to verify failure**

Run: `./test.sh --quick`
Expected: the two new tests fail; `client_performance` undefined, section missing.

- [ ] **Step 3: Add the fields and the section**

`useConfigForm.ts` after `desyncTarget: string;` (line 169):

```ts
// Client performance recording
clientPerformanceEnabled: boolean;
clientPerformanceRepo: string;
clientPerformanceTarget: string;
```

and after `desyncTarget: '',` (line 366):

```ts
  clientPerformanceEnabled: false,
  clientPerformanceRepo: '',
  clientPerformanceTarget: '',
```

`buildConfigUpdate.ts` after the `desync` block (line 111):

```ts
    client_performance: {
      enabled: state.clientPerformanceEnabled,
      repo: state.clientPerformanceRepo || '',
      target: state.clientPerformanceTarget || '',
    },
```

`ConfigPage.tsx` after line 218:

```ts
          clientPerformanceEnabled: data.client_performance?.enabled || false,
          clientPerformanceRepo: data.client_performance?.repo || '',
          clientPerformanceTarget: data.client_performance?.target || '',
```

and after line 1280:

```tsx
              clientPerformanceEnabled={state.clientPerformanceEnabled}
              clientPerformanceRepo={state.clientPerformanceRepo}
              clientPerformanceTarget={state.clientPerformanceTarget}
              repos={state.repos}
```

`AdvancedTab.tsx`: add the three props and `repos: Repo[]` to the props type (lines 9-12) and destructuring (43-46). After the IO Workspace Telemetry `settings-section` closes (line 712), inside the `isDevMode` fragment:

```tsx
<div className="settings-section">
  <div className="settings-section__header">
    <h3 className="settings-section__title">Client Performance</h3>
  </div>
  <div className="settings-section__body">
    <div className="form-group">
      <label className="flex-row gap-xs cursor-pointer">
        <input
          type="checkbox"
          checked={clientPerformanceEnabled}
          onChange={(e) => setField('clientPerformanceEnabled', e.target.checked)}
        />
        Enable client performance recording
      </label>
      <p className="form-group__hint">
        Shows a Client Performance pane in the sidebar. Recordings of this browser are attached to
        messages in a chat with an agent working in a schmux checkout.
      </p>
    </div>

    <div className="form-group">
      <label className="form-group__label" htmlFor="client-perf-repo">
        Repo
      </label>
      <select
        id="client-perf-repo"
        className="select"
        value={clientPerformanceRepo}
        onChange={(e) => setField('clientPerformanceRepo', e.target.value)}
        disabled={!clientPerformanceEnabled}
      >
        <option value="">Pick a repo</option>
        {repos.map((r) => (
          <option key={r.name} value={r.name}>
            {r.name}
          </option>
        ))}
      </select>
      <p className="form-group__hint">Must be a checkout of schmux.</p>
    </div>

    <div className="form-group">
      <label className="form-group__label">Target</label>
      <TargetSelect
        value={clientPerformanceTarget}
        onChange={(v) => setField('clientPerformanceTarget', v)}
        disabled={!clientPerformanceEnabled}
        includeDisabledOption={false}
        options={models}
      />
      <p className="form-group__hint">Must have a chat mode.</p>
    </div>
  </div>
</div>
```

- [ ] **Step 4: Run tests**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 5: Docs**

`docs/settings.md:49`: change "Terminal Desync Diagnostics and IO Workspace Telemetry are only useful" to "Terminal Desync Diagnostics, IO Workspace Telemetry, and Client Performance are only useful". Line 58, Dev-only row examples: append `, Client Performance`.

- [ ] **Step 6: Commit**

`./format.sh`, then `/commit` with `feat(settings): add Client Performance section to the Advanced tab`.

---

### Task 3: Recorder core: rings, timeline, stalls, switches, persistence, chat ids

**Files:**

- Create: `assets/dashboard/src/lib/clientPerf.ts`
- Create: `assets/dashboard/src/lib/clientPerfStore.ts`
- Modify: `assets/dashboard/src/setupTests.ts:1` (add `import 'fake-indexeddb/auto';`)
- Modify: `assets/dashboard/package.json` devDependencies (add `"fake-indexeddb": "^6.0.0"`), then `go run ./cmd/build-dashboard` to install
- Test: `assets/dashboard/src/lib/clientPerf.test.ts`

**Interfaces:**

- Produces (all later tasks consume these exact names):

```ts
export const STALL_MS = 100;
export const TIMELINE_CAP = 3600;
export interface PerfRow {
  t: number;
  loop: number;
  longTask: number;
  commit: number;
  wsBytes: number;
  wsHandler: number;
  fetches: number;
  route: string;
  hidden: boolean;
}
export interface ChatIds {
  workspaceId: string;
  sessionId: string;
}
export interface BuildInfo {
  version: string;
  devMode: boolean;
  sourceWorkspace: string;
  viteDev: boolean;
}
export interface Workload {
  workspaces: number;
  sessions: number;
  running: number;
  chats: number;
  terminals: number;
  mountedTerminals: number;
  socketsByPath: Record<string, number>;
  lastDashboardMessageBytes: number;
  panels: Record<string, boolean>;
  flags: Record<string, boolean>;
}
export interface ClientPerfFile {
  version: 1;
  browserId: string;
  startedAt: number;
  builtAt: number;
  build: BuildInfo;
  environment: Environment;
  workload: Workload;
  timeline: PerfRow[];
  stalls: number[];
  longTasks: LongTaskRecord[];
  interactions: InteractionRecord[];
  commits: CommitRecord[];
  websocket: { perSecond: WsSecondRecord[]; individual: WsMessageRecord[] };
  fetches: FetchRecord[];
  terminals: TerminalSecondRecord[];
  chatLoads: ChatLoadSample[];
  memory: MemoryRecord[];
  navigation: NavigationRecord[];
  errors: ErrorRecord[];
}
export class ClientPerfCollector {
  constructor(opts?: { now?: () => number; tabId?: string });
  setConfigEnabled(on: boolean): void;
  start(): void;
  stop(): void;
  isRecording(): boolean;
  startedAt(): number | null;
  stallCount(): number;
  hasUnsent(): boolean;
  subscribe(listener: () => void): () => void;
  setChat(ids: ChatIds): void;
  getChat(): ChatIds | null;
  setBuild(b: BuildInfo): void;
  setWorkload(w: Workload): void;
  setClockOffset(ms: number): void;
  recordLoopDelay(ms: number): void;
  recordLongTask(r: LongTaskRecord): void;
  recordInteraction(r: InteractionRecord): void;
  recordCommit(id: string, phase: string, durationMs: number): void;
  recordWsMessage(path: string, type: string, bytes: number, handlerMs: number): void;
  recordFetch(r: FetchRecord): void;
  recordTerminalSecond(r: TerminalSecondRecord): void;
  recordChatLoad(s: ChatLoadSample): void;
  recordMemory(r: MemoryRecord): void;
  recordNavigation(r: NavigationRecord): void;
  recordError(message: string): void;
  recordRoute(route: string): void;
  recordHidden(hidden: boolean): void;
  tick(): void; // closes the current second into the timeline; called by the 1s timer
  buildFile(): ClientPerfFile;
  markSent(): void;
  restore(): Promise<boolean>;
  persist(): Promise<void>;
}
export const clientPerf: ClientPerfCollector;
```

The record types (`LongTaskRecord`, `InteractionRecord`, `CommitRecord`, `WsSecondRecord`, `WsMessageRecord`, `FetchRecord`, `TerminalSecondRecord`, `MemoryRecord`, `NavigationRecord`, `ErrorRecord`, `Environment`) are defined in Step 3 below.

- [ ] **Step 1: Add the dev dependency and test setup**

Add `"fake-indexeddb": "^6.0.0"` to `devDependencies` in `assets/dashboard/package.json` (alphabetical, after `@vitest/coverage-v8`). Run `go run ./cmd/build-dashboard` from the repo root to install. Add `import 'fake-indexeddb/auto';` as the first line of `assets/dashboard/src/setupTests.ts`.

- [ ] **Step 2: Write the failing tests**

Create `assets/dashboard/src/lib/clientPerf.test.ts`:

```ts
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ClientPerfCollector, STALL_MS, TIMELINE_CAP } from './clientPerf';
import { clearSnapshot } from './clientPerfStore';

function collector(opts: { tabId?: string } = {}) {
  let now = 1_000_000;
  const c = new ClientPerfCollector({ now: () => now, tabId: opts.tabId ?? 'tab-a' });
  c.setConfigEnabled(true);
  return {
    c,
    advance: (ms: number) => {
      now += ms;
    },
  };
}

describe('ClientPerfCollector', () => {
  beforeEach(async () => {
    localStorage.clear();
    await clearSnapshot('tab-a');
    await clearSnapshot('tab-b');
  });

  it('does not record until both switches are on', () => {
    const { c } = collector();
    c.setConfigEnabled(false);
    c.start();
    expect(c.isRecording()).toBe(false);
    c.setConfigEnabled(true);
    expect(c.isRecording()).toBe(false); // start() was a no-op while config was off
    c.start();
    expect(c.isRecording()).toBe(true);
    expect(localStorage.getItem('schmux:client-perf')).toBe('1');
  });

  it('closes one timeline row per tick and marks stalls by the 100ms rule', () => {
    const { c, advance } = collector();
    c.start();
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    c.recordLoopDelay(5);
    c.recordLongTask({ t: 0, duration: STALL_MS, attribution: 'script' });
    advance(1000);
    c.tick();
    c.recordLoopDelay(5);
    advance(1000);
    c.tick();
    const file = c.buildFile();
    expect(file.timeline).toHaveLength(3);
    expect(file.stalls).toEqual([0, 1]);
    expect(c.stallCount()).toBe(2);
  });

  it('drops the oldest timeline rows past the cap but keeps counting stalls', () => {
    const { c, advance } = collector();
    c.start();
    for (let i = 0; i < TIMELINE_CAP + 10; i++) {
      c.recordLoopDelay(STALL_MS);
      advance(1000);
      c.tick();
    }
    const file = c.buildFile();
    expect(file.timeline).toHaveLength(TIMELINE_CAP);
    expect(c.stallCount()).toBe(TIMELINE_CAP + 10);
  });

  it('persists and restores across a reload under the same tab id', async () => {
    const { c, advance } = collector();
    c.start();
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    await c.persist();

    const { c: again } = collector();
    expect(await again.restore()).toBe(true);
    expect(again.isRecording()).toBe(true);
    expect(again.stallCount()).toBe(1);
    expect(again.getChat()).toEqual({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    expect(again.buildFile().navigation.at(-1)?.kind).toBe('reload');
  });

  it('keeps two tabs apart', async () => {
    const a = collector({ tabId: 'tab-a' });
    const b = collector({ tabId: 'tab-b' });
    a.c.start();
    b.c.start();
    a.c.recordLoopDelay(STALL_MS);
    a.advance(1000);
    a.c.tick();
    await a.c.persist();
    await b.c.persist();
    const { c: aAgain } = collector({ tabId: 'tab-a' });
    const { c: bAgain } = collector({ tabId: 'tab-b' });
    await aAgain.restore();
    await bAgain.restore();
    expect(aAgain.stallCount()).toBe(1);
    expect(bAgain.stallCount()).toBe(0);
  });

  it('empties buffers on markSent and keeps the chat ids', () => {
    const { c, advance } = collector();
    c.start();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    expect(c.hasUnsent()).toBe(true);
    c.markSent();
    expect(c.hasUnsent()).toBe(false);
    expect(c.isRecording()).toBe(true);
    expect(c.getChat()).toEqual({ workspaceId: 'ws-1', sessionId: 'sess-1' });
  });

  it('stops, clears the switch, and empties everything when config goes off', async () => {
    const { c, advance } = collector();
    c.start();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    c.recordLoopDelay(5);
    advance(1000);
    c.tick();
    c.setConfigEnabled(false);
    expect(c.isRecording()).toBe(false);
    expect(localStorage.getItem('schmux:client-perf')).toBeNull();
    expect(c.getChat()).toBeNull();
    const { c: again } = collector();
    expect(await again.restore()).toBe(false);
  });

  it('notifies subscribers on start, tick, and stop', () => {
    const { c, advance } = collector();
    const listener = vi.fn();
    c.subscribe(listener);
    c.start();
    advance(1000);
    c.tick();
    c.stop();
    expect(listener).toHaveBeenCalledTimes(3);
  });
});
```

- [ ] **Step 3: Run to verify failure**

Run: `./test.sh --quick`
Expected: `clientPerf.test.ts` fails to import.

- [ ] **Step 4: Write `clientPerfStore.ts`**

```ts
// IndexedDB persistence for the client performance recorder. One record per
// tab so two tabs recording at once never overwrite each other.
const DB_NAME = 'schmux-client-perf';
const STORE = 'snapshots';

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function tx<T>(
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => IDBRequest<T>
): Promise<T> {
  return open().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const t = db.transaction(STORE, mode);
        const req = run(t.objectStore(STORE));
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
        t.oncomplete = () => db.close();
      })
  );
}

export function saveSnapshot(tabId: string, value: unknown): Promise<unknown> {
  return tx('readwrite', (s) => s.put(value, tabId));
}

export function loadSnapshot<T>(tabId: string): Promise<T | undefined> {
  return tx<T | undefined>('readonly', (s) => s.get(tabId) as IDBRequest<T | undefined>);
}

export function clearSnapshot(tabId: string): Promise<unknown> {
  return tx('readwrite', (s) => s.delete(tabId));
}
```

- [ ] **Step 5: Write `clientPerf.ts`**

```ts
// Client performance recorder. A module singleton like inputLatency; every
// method is a no-op while off. See docs/client-performance.md for the file.
import type { ChatLoadSample } from './chat/loadTelemetry';
import { saveSnapshot, loadSnapshot, clearSnapshot } from './clientPerfStore';

export const STALL_MS = 100;
export const COMMIT_MS = 16;
export const INTERACTION_MS = 100;
export const WS_SLOW_MS = 5;
export const WS_LARGE_BYTES = 50 * 1024;
export const TIMELINE_CAP = 3600;
export const BROWSER_SWITCH_KEY = 'schmux:client-perf';
const BROWSER_ID_KEY = 'schmux:client-perf-browser-id';
const TAB_ID_KEY = 'schmux:client-perf-tab-id';

export interface PerfRow {
  t: number;
  loop: number;
  longTask: number;
  commit: number;
  wsBytes: number;
  wsHandler: number;
  fetches: number;
  route: string;
  hidden: boolean;
}
export interface LongTaskRecord {
  t: number;
  duration: number;
  attribution: string;
}
export interface InteractionRecord {
  t: number;
  type: string;
  target: string;
  inputDelay: number;
  processing: number;
  presentation: number;
}
export interface CommitRecord {
  t: number;
  id: string;
  phase: string;
  duration: number;
  route: string;
}
export interface WsSecondRecord {
  t: number;
  path: string;
  type: string;
  count: number;
  bytes: number;
  handlerMs: number;
}
export interface WsMessageRecord {
  t: number;
  path: string;
  type: string;
  bytes: number;
  handlerMs: number;
}
export interface FetchRecord {
  t: number;
  endpoint: string;
  duration: number;
  bytes: number;
}
export interface TerminalSecondRecord {
  t: number;
  id: string;
  frames: number;
  bytes: number;
  handleOutputP50: number;
  handleOutputP99: number;
}
export interface MemoryRecord {
  t: number;
  heapBytes: number | null;
  domNodes: number;
  terminals: number;
  sockets: number;
}
export interface NavigationRecord {
  t: number;
  kind: 'route' | 'visible' | 'hidden' | 'reload';
  route: string;
  firstCommitMs?: number;
  paintMs?: number;
}
export interface ErrorRecord {
  t: number;
  message: string;
}
export interface ChatIds {
  workspaceId: string;
  sessionId: string;
}
export interface BuildInfo {
  version: string;
  devMode: boolean;
  sourceWorkspace: string;
  viteDev: boolean;
}
export interface Environment {
  userAgent: string;
  cpus: number;
  deviceMemoryGb: number | null;
  viewport: { w: number; h: number };
  pixelRatio: number;
  host: string;
  remoteClient: boolean;
  unsupported: string[];
  clockOffsetMs: number;
}
export interface Workload {
  workspaces: number;
  sessions: number;
  running: number;
  chats: number;
  terminals: number;
  mountedTerminals: number;
  socketsByPath: Record<string, number>;
  lastDashboardMessageBytes: number;
  panels: Record<string, boolean>;
  flags: Record<string, boolean>;
}

export interface ClientPerfFile {
  version: 1;
  browserId: string;
  startedAt: number;
  builtAt: number;
  build: BuildInfo;
  environment: Environment;
  workload: Workload;
  timeline: PerfRow[];
  stalls: number[];
  longTasks: LongTaskRecord[];
  interactions: InteractionRecord[];
  commits: CommitRecord[];
  websocket: { perSecond: WsSecondRecord[]; individual: WsMessageRecord[] };
  fetches: FetchRecord[];
  terminals: TerminalSecondRecord[];
  chatLoads: ChatLoadSample[];
  memory: MemoryRecord[];
  navigation: NavigationRecord[];
  errors: ErrorRecord[];
}

export class Ring<T> {
  private items: T[] = [];
  constructor(readonly cap: number) {}
  push(v: T) {
    this.items.push(v);
    if (this.items.length > this.cap) this.items.shift();
  }
  toArray(): T[] {
    return this.items.slice();
  }
  clear() {
    this.items = [];
  }
  get length() {
    return this.items.length;
  }
}

interface Snapshot {
  startedAt: number;
  stalls: number;
  chat: ChatIds | null;
  buffers: Omit<
    ClientPerfFile,
    'version' | 'browserId' | 'startedAt' | 'builtAt' | 'build' | 'environment' | 'workload'
  >;
}

function readOrCreate(storage: Storage, key: string): string {
  let v = storage.getItem(key);
  if (!v) {
    v = Math.random().toString(36).slice(2, 10);
    storage.setItem(key, v);
  }
  return v;
}

export function isStall(row: Pick<PerfRow, 'loop' | 'longTask'>): boolean {
  return row.loop >= STALL_MS || row.longTask >= STALL_MS;
}

export class ClientPerfCollector {
  private now: () => number;
  private tabId: string;
  private configEnabled = false;
  private recording = false;
  private started: number | null = null;
  private stalls = 0;
  private chat: ChatIds | null = null;
  private build: BuildInfo = { version: '', devMode: false, sourceWorkspace: '', viteDev: false };
  private workload: Workload = {
    workspaces: 0,
    sessions: 0,
    running: 0,
    chats: 0,
    terminals: 0,
    mountedTerminals: 0,
    socketsByPath: {},
    lastDashboardMessageBytes: 0,
    panels: {},
    flags: {},
  };
  private clockOffsetMs = 0;
  private unsupported: string[] = [];
  private route = typeof window !== 'undefined' ? window.location.pathname : '';
  private hidden = false;
  private listeners = new Set<() => void>();

  private timeline = new Ring<PerfRow>(TIMELINE_CAP);
  private longTasks = new Ring<LongTaskRecord>(2000);
  private interactions = new Ring<InteractionRecord>(2000);
  private commits = new Ring<CommitRecord>(2000);
  private wsSeconds = new Ring<WsSecondRecord>(3600);
  private wsIndividual = new Ring<WsMessageRecord>(2000);
  private fetches = new Ring<FetchRecord>(2000);
  private terminals = new Ring<TerminalSecondRecord>(3600);
  private chatLoads = new Ring<ChatLoadSample>(200);
  private memory = new Ring<MemoryRecord>(360);
  private navigation = new Ring<NavigationRecord>(500);
  private errors = new Ring<ErrorRecord>(100);

  // The second being accumulated. Closed into `timeline` by tick().
  private cur = this.emptySecond();
  private curWs = new Map<string, WsSecondRecord>();

  constructor(opts: { now?: () => number; tabId?: string } = {}) {
    this.now = opts.now ?? (() => Date.now());
    this.tabId =
      opts.tabId ??
      (typeof sessionStorage !== 'undefined' ? readOrCreate(sessionStorage, TAB_ID_KEY) : 'tab');
  }

  private emptySecond() {
    return { loop: 0, longTask: 0, commit: 0, wsBytes: 0, wsHandler: 0, fetches: 0 };
  }
  private t(): number {
    return this.now() + this.clockOffsetMs;
  }
  private notify() {
    this.listeners.forEach((l) => l());
  }
  private browserSwitch(): boolean {
    return localStorage.getItem(BROWSER_SWITCH_KEY) === '1';
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  setConfigEnabled(on: boolean) {
    this.configEnabled = on;
    if (!on && (this.recording || this.chat)) {
      this.stop();
      this.chat = null;
      void clearSnapshot(this.tabId);
      this.notify();
    }
  }

  start() {
    if (!this.configEnabled || this.recording) return;
    localStorage.setItem(BROWSER_SWITCH_KEY, '1');
    this.recording = true;
    this.started = this.t();
    this.notify();
  }

  stop() {
    if (!this.recording) return;
    localStorage.removeItem(BROWSER_SWITCH_KEY);
    this.recording = false;
    this.started = null;
    this.clearBuffers();
    this.stalls = 0;
    void clearSnapshot(this.tabId);
    this.notify();
  }

  isRecording() {
    return this.recording;
  }
  startedAt() {
    return this.started;
  }
  stallCount() {
    return this.stalls;
  }
  hasUnsent() {
    return this.recording && this.timeline.length > 0;
  }
  setChat(ids: ChatIds) {
    this.chat = ids;
    this.notify();
  }
  getChat() {
    return this.chat;
  }
  setBuild(b: BuildInfo) {
    this.build = b;
  }
  setWorkload(w: Workload) {
    this.workload = w;
  }
  setClockOffset(ms: number) {
    this.clockOffsetMs = ms;
  }
  noteUnsupported(api: string) {
    if (!this.unsupported.includes(api)) this.unsupported.push(api);
  }

  recordLoopDelay(ms: number) {
    if (this.recording) this.cur.loop = Math.max(this.cur.loop, ms);
  }
  recordLongTask(r: LongTaskRecord) {
    if (!this.recording) return;
    this.cur.longTask += r.duration;
    this.longTasks.push(r);
  }
  recordInteraction(r: InteractionRecord) {
    if (this.recording && r.inputDelay + r.processing + r.presentation >= INTERACTION_MS)
      this.interactions.push(r);
  }
  recordCommit(id: string, phase: string, durationMs: number) {
    if (!this.recording) return;
    this.cur.commit += durationMs;
    if (durationMs > COMMIT_MS)
      this.commits.push({
        t: this.t(),
        id,
        phase,
        duration: Math.round(durationMs),
        route: this.route,
      });
  }
  recordWsMessage(path: string, type: string, bytes: number, handlerMs: number) {
    if (!this.recording) return;
    this.cur.wsBytes += bytes;
    this.cur.wsHandler += handlerMs;
    const key = `${path}\n${type}`;
    const rec = this.curWs.get(key) ?? {
      t: this.t(),
      path,
      type,
      count: 0,
      bytes: 0,
      handlerMs: 0,
    };
    rec.count += 1;
    rec.bytes += bytes;
    rec.handlerMs += handlerMs;
    this.curWs.set(key, rec);
    if (handlerMs > WS_SLOW_MS || bytes > WS_LARGE_BYTES)
      this.wsIndividual.push({ t: this.t(), path, type, bytes, handlerMs });
  }
  recordFetch(r: FetchRecord) {
    if (!this.recording) return;
    this.cur.fetches += 1;
    this.fetches.push(r);
  }
  recordTerminalSecond(r: TerminalSecondRecord) {
    if (this.recording) this.terminals.push(r);
  }
  recordChatLoad(s: ChatLoadSample) {
    if (this.recording) this.chatLoads.push(s);
  }
  recordMemory(r: MemoryRecord) {
    if (this.recording) this.memory.push(r);
  }
  recordNavigation(r: NavigationRecord) {
    if (this.recording) this.navigation.push(r);
  }
  recordError(message: string) {
    if (this.recording) this.errors.push({ t: this.t(), message });
  }
  recordRoute(route: string) {
    this.route = route;
  }
  recordHidden(hidden: boolean) {
    this.hidden = hidden;
  }

  tick() {
    if (!this.recording) return;
    const row: PerfRow = { t: this.t(), ...this.cur, route: this.route, hidden: this.hidden };
    this.timeline.push(row);
    if (isStall(row)) this.stalls += 1;
    for (const rec of this.curWs.values()) this.wsSeconds.push(rec);
    this.curWs.clear();
    this.cur = this.emptySecond();
    this.notify();
  }

  private buffers() {
    const timeline = this.timeline.toArray();
    return {
      timeline,
      stalls: timeline.map((r, i) => (isStall(r) ? i : -1)).filter((i) => i >= 0),
      longTasks: this.longTasks.toArray(),
      interactions: this.interactions.toArray(),
      commits: this.commits.toArray(),
      websocket: { perSecond: this.wsSeconds.toArray(), individual: this.wsIndividual.toArray() },
      fetches: this.fetches.toArray(),
      terminals: this.terminals.toArray(),
      chatLoads: this.chatLoads.toArray(),
      memory: this.memory.toArray(),
      navigation: this.navigation.toArray(),
      errors: this.errors.toArray(),
    };
  }

  private clearBuffers() {
    for (const r of [
      this.timeline,
      this.longTasks,
      this.interactions,
      this.commits,
      this.wsSeconds,
      this.wsIndividual,
      this.fetches,
      this.terminals,
      this.chatLoads,
      this.memory,
      this.navigation,
      this.errors,
    ])
      r.clear();
    this.curWs.clear();
    this.cur = this.emptySecond();
  }

  environment(): Environment {
    const nav = typeof navigator !== 'undefined' ? navigator : ({} as Navigator);
    return {
      userAgent: nav.userAgent ?? '',
      cpus: nav.hardwareConcurrency ?? 0,
      deviceMemoryGb: (nav as Navigator & { deviceMemory?: number }).deviceMemory ?? null,
      viewport: {
        w: typeof window !== 'undefined' ? window.innerWidth : 0,
        h: typeof window !== 'undefined' ? window.innerHeight : 0,
      },
      pixelRatio: typeof window !== 'undefined' ? window.devicePixelRatio : 1,
      host: typeof window !== 'undefined' ? window.location.host : '',
      remoteClient: false,
      unsupported: this.unsupported.slice(),
      clockOffsetMs: this.clockOffsetMs,
    };
  }

  buildFile(): ClientPerfFile {
    return {
      version: 1,
      browserId: readOrCreate(localStorage, BROWSER_ID_KEY),
      startedAt: this.started ?? this.t(),
      builtAt: this.t(),
      build: this.build,
      environment: this.environment(),
      workload: this.workload,
      ...this.buffers(),
    };
  }

  markSent() {
    this.clearBuffers();
    void this.persist();
    this.notify();
  }

  async persist() {
    if (!this.recording) return;
    const snap: Snapshot = {
      startedAt: this.started ?? this.t(),
      stalls: this.stalls,
      chat: this.chat,
      buffers: this.buffers(),
    };
    await saveSnapshot(this.tabId, snap);
  }

  async restore(): Promise<boolean> {
    if (!this.configEnabled || !this.browserSwitch()) return false;
    const snap = await loadSnapshot<Snapshot>(this.tabId);
    if (!snap) return false;
    this.recording = true;
    this.started = snap.startedAt;
    this.stalls = snap.stalls;
    this.chat = snap.chat;
    const b = snap.buffers;
    b.timeline.forEach((r) => this.timeline.push(r));
    b.longTasks.forEach((r) => this.longTasks.push(r));
    b.interactions.forEach((r) => this.interactions.push(r));
    b.commits.forEach((r) => this.commits.push(r));
    b.websocket.perSecond.forEach((r) => this.wsSeconds.push(r));
    b.websocket.individual.forEach((r) => this.wsIndividual.push(r));
    b.fetches.forEach((r) => this.fetches.push(r));
    b.terminals.forEach((r) => this.terminals.push(r));
    b.chatLoads.forEach((r) => this.chatLoads.push(r));
    b.memory.forEach((r) => this.memory.push(r));
    b.navigation.forEach((r) => this.navigation.push(r));
    b.errors.forEach((r) => this.errors.push(r));
    this.navigation.push({ t: this.t(), kind: 'reload', route: this.route });
    this.notify();
    return true;
  }
}

export const clientPerf = new ClientPerfCollector();
```

`environment().remoteClient` is filled by Task 6 from `isRemoteClient()`; keep it `false` here so `clientPerf.ts` has no import from `utils` that drags React into the unit test.

- [ ] **Step 6: Run tests**

Run: `./test.sh --quick`
Expected: all 8 `clientPerf` tests PASS. If `Ring` and `isStall` trigger knip "unused export", keep `Ring` unexported and export only `isStall`; the pane uses `isStall` in Task 7? No, the pane uses `stallCount()`. Unexport both unless a test imports them; the tests above import only `ClientPerfCollector`, `STALL_MS`, `TIMELINE_CAP`.

- [ ] **Step 7: Commit**

`./format.sh`, then `/commit` with `feat(dashboard): add client performance recorder core`.

---

### Task 4: Observers: event loop, long tasks, interactions, fetches, memory, navigation, errors

**Files:**

- Create: `assets/dashboard/src/lib/clientPerfObservers.ts`
- Modify: `assets/dashboard/src/lib/clientPerf.ts` (call `startObservers`/`stopObservers` from `start`/`stop`/`restore`)
- Test: `assets/dashboard/src/lib/clientPerfObservers.test.ts`

**Interfaces:**

- Consumes: `ClientPerfCollector` record methods from Task 3.
- Produces: `startObservers(c: ClientPerfCollector, deps?: ObserverDeps): () => void` where `ObserverDeps = { setInterval, clearInterval, PerformanceObserver?, performance, document, window, fetchHealthz }`. Returns a stop function.

- [ ] **Step 1: Write the failing tests**

```ts
import { describe, it, expect, vi } from 'vitest';
import { ClientPerfCollector } from './clientPerf';
import { startObservers, normalizeEndpoint } from './clientPerfObservers';

function fakeObserverClass(entries: Record<string, unknown[]>) {
  const instances: { type: string; cb: (list: { getEntries(): unknown[] }) => void }[] = [];
  class FakePO {
    cb: (list: { getEntries(): unknown[] }) => void;
    constructor(cb: (list: { getEntries(): unknown[] }) => void) {
      this.cb = cb;
    }
    observe(opts: { type: string }) {
      if (!(opts.type in entries)) throw new Error('unsupported');
      instances.push({ type: opts.type, cb: this.cb });
    }
    disconnect() {}
  }
  return {
    FakePO,
    fire: (type: string) =>
      instances
        .filter((i) => i.type === type)
        .forEach((i) => i.cb({ getEntries: () => entries[type] })),
  };
}

describe('clientPerf observers', () => {
  it('notes unsupported observers and keeps the others', () => {
    const c = new ClientPerfCollector({ now: () => 0, tabId: 't' });
    c.setConfigEnabled(true);
    c.start();
    const { FakePO, fire } = fakeObserverClass({
      longtask: [{ startTime: 10, duration: 120, attribution: [{ containerType: 'window' }] }],
    });
    const stop = startObservers(c, {
      PerformanceObserver: FakePO as unknown as typeof PerformanceObserver,
    });
    fire('longtask');
    const file = c.buildFile();
    expect(file.longTasks).toEqual([{ t: 10, duration: 120, attribution: 'window' }]);
    expect(file.environment.unsupported).toEqual(expect.arrayContaining(['event', 'resource']));
    stop();
  });

  it('records same-origin /api/ resources with ids replaced', () => {
    const c = new ClientPerfCollector({ now: () => 0, tabId: 't' });
    c.setConfigEnabled(true);
    c.start();
    const { FakePO, fire } = fakeObserverClass({
      resource: [
        {
          name: 'http://localhost:7337/api/sessions/abc-123/output?x=1',
          initiatorType: 'fetch',
          startTime: 5,
          duration: 40,
          transferSize: 2048,
        },
        {
          name: 'https://cdn.example.com/lib.js',
          initiatorType: 'script',
          startTime: 5,
          duration: 40,
          transferSize: 1,
        },
      ],
    });
    const stop = startObservers(c, {
      PerformanceObserver: FakePO as unknown as typeof PerformanceObserver,
    });
    fire('resource');
    expect(c.buildFile().fetches).toEqual([
      { t: 5, endpoint: '/api/sessions/:id/output', duration: 40, bytes: 2048 },
    ]);
    stop();
  });

  it('normalizes ids in endpoints', () => {
    expect(normalizeEndpoint('/api/workspaces/schmux-004/attachments')).toBe(
      '/api/workspaces/:id/attachments'
    );
    expect(normalizeEndpoint('/api/config')).toBe('/api/config');
  });

  it('records a window error and an unhandled rejection', () => {
    const c = new ClientPerfCollector({ now: () => 0, tabId: 't' });
    c.setConfigEnabled(true);
    c.start();
    const stop = startObservers(c, { PerformanceObserver: undefined });
    window.dispatchEvent(new ErrorEvent('error', { message: 'boom' }));
    expect(c.buildFile().errors).toEqual([{ t: 0, message: 'boom' }]);
    stop();
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `./test.sh --quick`
Expected: import failure for `clientPerfObservers`.

- [ ] **Step 3: Write `clientPerfObservers.ts`**

```ts
import type { ClientPerfCollector } from './clientPerf';

export interface ObserverDeps {
  PerformanceObserver?: typeof PerformanceObserver;
  setInterval?: typeof setInterval;
  clearInterval?: typeof clearInterval;
  fetchHealthz?: () => Promise<Response>;
}

// Replace any path segment containing a digit with :id so the agent sees
// one row per endpoint, not one per workspace or session.
export function normalizeEndpoint(path: string): string {
  return path
    .split('?')[0]
    .split('/')
    .map((seg) => (/\d/.test(seg) ? ':id' : seg))
    .join('/');
}

export function startObservers(c: ClientPerfCollector, deps: ObserverDeps = {}): () => void {
  const PO =
    'PerformanceObserver' in deps ? deps.PerformanceObserver : globalThis.PerformanceObserver;
  const setI = deps.setInterval ?? globalThis.setInterval;
  const clearI = deps.clearInterval ?? globalThis.clearInterval;
  const stops: (() => void)[] = [];

  const observe = (type: string, cb: (entries: PerformanceEntry[]) => void) => {
    if (!PO) {
      c.noteUnsupported(type);
      return;
    }
    try {
      const po = new PO((list) => cb(list.getEntries()));
      po.observe({ type, buffered: false } as PerformanceObserverInit);
      stops.push(() => po.disconnect());
    } catch {
      c.noteUnsupported(type);
    }
  };

  observe('longtask', (entries) => {
    for (const e of entries) {
      const attr =
        (e as PerformanceEntry & { attribution?: { containerType?: string }[] }).attribution?.[0]
          ?.containerType ?? '';
      c.recordLongTask({
        t: Math.round(e.startTime),
        duration: Math.round(e.duration),
        attribution: attr,
      });
    }
  });

  observe('event', (entries) => {
    for (const e of entries) {
      const ev = e as PerformanceEventTiming;
      const target = (ev.target as Element | null)?.tagName?.toLowerCase() ?? '';
      c.recordInteraction({
        t: Math.round(ev.startTime),
        type: ev.name,
        target,
        inputDelay: Math.round(ev.processingStart - ev.startTime),
        processing: Math.round(ev.processingEnd - ev.processingStart),
        presentation: Math.round(ev.startTime + ev.duration - ev.processingEnd),
      });
    }
  });

  observe('resource', (entries) => {
    for (const e of entries) {
      const r = e as PerformanceResourceTiming;
      if (r.initiatorType !== 'fetch' && r.initiatorType !== 'xmlhttprequest') continue;
      let url: URL;
      try {
        url = new URL(r.name);
      } catch {
        continue;
      }
      if (url.origin !== window.location.origin || !url.pathname.startsWith('/api/')) continue;
      c.recordFetch({
        t: Math.round(r.startTime),
        endpoint: normalizeEndpoint(url.pathname),
        duration: Math.round(r.duration),
        bytes: r.transferSize,
      });
    }
  });

  // Event loop delay: post a message each second and measure how late it runs.
  if (typeof MessageChannel !== 'undefined') {
    const ch = new MessageChannel();
    let sentAt = 0;
    ch.port1.onmessage = () => c.recordLoopDelay(Math.round(performance.now() - sentAt));
    const id = setI(() => {
      sentAt = performance.now();
      ch.port2.postMessage(null);
      c.tick();
    }, 1000);
    stops.push(() => {
      clearI(id);
      ch.port1.close();
      ch.port2.close();
    });
  } else {
    c.noteUnsupported('MessageChannel');
    const id = setI(() => c.tick(), 1000);
    stops.push(() => clearI(id));
  }

  const mem = setI(() => {
    const heap =
      (performance as Performance & { memory?: { usedJSHeapSize: number } }).memory
        ?.usedJSHeapSize ?? null;
    if (heap === null) c.noteUnsupported('performance.memory');
    c.recordMemory({
      t: Date.now(),
      heapBytes: heap,
      domNodes: document.getElementsByTagName('*').length,
      terminals: c.terminalCount(),
      sockets: c.socketCount(),
    });
  }, 10_000);
  stops.push(() => clearI(mem));

  const onVisibility = () => {
    const hidden = document.visibilityState === 'hidden';
    c.recordHidden(hidden);
    c.recordNavigation({
      t: Date.now(),
      kind: hidden ? 'hidden' : 'visible',
      route: window.location.pathname,
    });
  };
  document.addEventListener('visibilitychange', onVisibility);
  stops.push(() => document.removeEventListener('visibilitychange', onVisibility));

  const onError = (e: ErrorEvent) => c.recordError(e.message);
  const onRejection = (e: PromiseRejectionEvent) => c.recordError(String(e.reason));
  window.addEventListener('error', onError);
  window.addEventListener('unhandledrejection', onRejection);
  stops.push(() => {
    window.removeEventListener('error', onError);
    window.removeEventListener('unhandledrejection', onRejection);
  });

  const onPageHide = () => {
    void c.persist();
  };
  window.addEventListener('pagehide', onPageHide);
  stops.push(() => window.removeEventListener('pagehide', onPageHide));
  const persistId = setI(() => {
    void c.persist();
  }, 5000);
  stops.push(() => clearI(persistId));

  const healthz = deps.fetchHealthz ?? (() => fetch('/api/healthz'));
  void healthz()
    .then((res) => {
      const date = res.headers.get('Date');
      if (date) c.setClockOffset(Date.parse(date) - Date.now());
    })
    .catch(() => c.noteUnsupported('healthz-date'));

  return () => stops.forEach((s) => s());
}
```

Add to `ClientPerfCollector` in `clientPerf.ts`: `terminalCount(): number` and `socketCount(): number` (backed by counters Task 5 and Task 6 increment: `private terminalsMounted = 0; private socketsOpen = 0;` with `adjustTerminals(delta: number)` and `adjustSockets(delta: number)`), plus a `private stopObservers: (() => void) | null`, and in `start()` and at the end of a successful `restore()`:

```ts
this.stopObservers = startObservers(this);
```

and in `stop()` before `clearBuffers()`:

```ts
this.stopObservers?.();
this.stopObservers = null;
```

`clientPerf.ts` must import `startObservers` lazily to avoid a cycle: put `import { startObservers } from './clientPerfObservers';` at the top of `clientPerf.ts` and have `clientPerfObservers.ts` import only the type (`import type`). Type-only imports do not create a runtime cycle.

The Task 3 tests call `start()` with the global `setInterval`; under Vitest they will create real intervals. In those tests add `vi.useFakeTimers()` in `beforeEach` and `vi.useRealTimers()` in `afterEach`, and call `c.stop()` at the end of each test (or in `afterEach`).

- [ ] **Step 4: Run tests**

Run: `./test.sh --quick`
Expected: PASS for `clientPerfObservers.test.ts` and `clientPerf.test.ts`.

- [ ] **Step 5: Commit**

`./format.sh`, then `/commit` with `feat(dashboard): add client performance observers`.

---

### Task 5: WebSocket wrapper, terminals, chat loads

**Files:**

- Modify: `assets/dashboard/src/lib/transport.ts:6-16`
- Modify: `assets/dashboard/src/lib/clientPerf.ts` (add `wrapSocket`, terminal registry)
- Modify: `assets/dashboard/src/lib/terminalStream.ts:1229` (register on connect), `:1314` (unregister in `disconnect`), `:1761` (count bytes)
- Modify: `assets/dashboard/src/lib/chat/loadTelemetry.ts:60-64`
- Test: `assets/dashboard/src/lib/clientPerfSocket.test.ts`

**Interfaces:**

- Produces: `clientPerf.wrapSocket(ws: WebSocket, url: string): WebSocket`; `clientPerf.registerTerminal(id: string, sample: () => { frames: number; bytes: number }): () => void`.

- [ ] **Step 1: Write the failing tests**

```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { ClientPerfCollector } from './clientPerf';
import { setTransport, transport, liveTransport } from './transport';

class FakeSocket {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  readyState = 1;
  constructor(public url: string) {}
  emit(data: unknown) {
    this.onmessage?.({ data } as MessageEvent);
  }
  close() {
    this.readyState = 3;
    this.onclose?.();
  }
}

describe('clientPerf socket wrapper', () => {
  let c: ClientPerfCollector;
  let now = 0;
  beforeEach(() => {
    vi.useFakeTimers();
    now = 0;
    c = new ClientPerfCollector({ now: () => now, tabId: 't' });
    c.setConfigEnabled(true);
    c.start();
  });
  afterEach(() => {
    c.stop();
    setTransport(liveTransport);
    vi.useRealTimers();
  });

  it('sums messages per second by path and type and keeps large ones individually', () => {
    const sockets: FakeSocket[] = [];
    setTransport({
      ...liveTransport,
      createWebSocket: (url) => {
        const s = new FakeSocket(url);
        sockets.push(s);
        return c.wrapSocket(s as unknown as WebSocket, url);
      },
    });
    const ws = transport.createWebSocket('ws://localhost/ws/terminal/abc123');
    const seen: string[] = [];
    ws.onmessage = (ev) => seen.push(String(ev.data).length.toString());
    sockets[0].emit(JSON.stringify({ type: 'output', data: 'x' }));
    sockets[0].emit(JSON.stringify({ type: 'output', data: 'y'.repeat(60 * 1024) }));
    sockets[0].emit(new ArrayBuffer(16));
    now += 1000;
    c.tick();
    const file = c.buildFile();
    expect(seen).toHaveLength(3);
    expect(file.websocket.perSecond).toEqual([
      expect.objectContaining({ path: '/ws/terminal/:id', type: 'output', count: 2 }),
      expect.objectContaining({ path: '/ws/terminal/:id', type: 'binary', count: 1, bytes: 16 }),
    ]);
    expect(file.websocket.individual).toEqual([
      expect.objectContaining({ type: 'output', bytes: 60 * 1024 + 27 }),
    ]);
  });

  it('counts open sockets and records the last /ws/dashboard message size', () => {
    const sockets: FakeSocket[] = [];
    setTransport({
      ...liveTransport,
      createWebSocket: (url) => {
        const s = new FakeSocket(url);
        sockets.push(s);
        return c.wrapSocket(s as unknown as WebSocket, url);
      },
    });
    const ws = transport.createWebSocket('ws://localhost/ws/dashboard');
    ws.onmessage = () => {};
    sockets[0].emit('{"type":"sessions"}');
    expect(c.socketCount()).toBe(1);
    expect(c.lastDashboardMessageBytes()).toBe(19);
    sockets[0].close();
    expect(c.socketCount()).toBe(0);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `./test.sh --quick`
Expected: `wrapSocket` is not a function.

- [ ] **Step 3: Add `wrapSocket` and the terminal registry to `clientPerf.ts`**

```ts
  private socketsOpen = 0;
  private terminalSamplers = new Map<string, () => { frames: number; bytes: number }>();
  private terminalLast = new Map<string, { frames: number; bytes: number }>();
  private lastDashboardBytes = 0;

  socketCount() { return this.socketsOpen; }
  terminalCount() { return this.terminalSamplers.size; }
  lastDashboardMessageBytes() { return this.lastDashboardBytes; }

  registerTerminal(id: string, sample: () => { frames: number; bytes: number }): () => void {
    this.terminalSamplers.set(id, sample);
    this.terminalLast.set(id, sample());
    return () => { this.terminalSamplers.delete(id); this.terminalLast.delete(id); };
  }

  // Called from tick() before the row closes: one terminal row per mounted terminal.
  private sampleTerminals(p50: number, p99: number) {
    for (const [id, sample] of this.terminalSamplers) {
      const cur = sample();
      const prev = this.terminalLast.get(id) ?? cur;
      this.terminalLast.set(id, cur);
      this.terminals.push({ t: this.t(), id, frames: cur.frames - prev.frames, bytes: cur.bytes - prev.bytes, handleOutputP50: p50, handleOutputP99: p99 });
    }
  }

  wrapSocket(ws: WebSocket, url: string): WebSocket {
    const path = socketPath(url);
    let handler: ((ev: MessageEvent) => void) | null = null;
    let lastEvent: MessageEvent | null = null;
    const wrapped = (ev: MessageEvent) => {
      if (ev === lastEvent) return; // real sockets reach us by addEventListener and by the getter; count once
      lastEvent = ev;
      const t0 = performance.now();
      try { handler?.(ev); } finally {
        const bytes = messageBytes(ev.data);
        if (path === '/ws/dashboard') this.lastDashboardBytes = bytes;
        this.recordWsMessage(path, messageType(ev.data), bytes, performance.now() - t0);
      }
    };
    Object.defineProperty(ws, 'onmessage', { configurable: true, get: () => (handler ? wrapped : null), set: (fn) => { handler = fn; } });
    if (typeof ws.addEventListener === 'function') ws.addEventListener('message', wrapped);
    this.socketsOpen += 1;
    const prevClose = ws.onclose;
    Object.defineProperty(ws, 'onclose', { configurable: true, get: () => prevClose, set: () => {} });
    const onClose = () => { this.socketsOpen = Math.max(0, this.socketsOpen - 1); };
    if (typeof ws.addEventListener === 'function') ws.addEventListener('close', onClose);
    else { const orig = ws.close.bind(ws); ws.close = () => { onClose(); orig(); }; }
    return ws;
  }
```

Replace the `onclose` block above with the simpler version if the socket consumers assign `onclose` (they do: `terminalStream.ts`, `socket.ts`). Use this instead:

```ts
let closeHandler: ((ev: CloseEvent) => void) | null = null;
const wrappedClose = (ev: CloseEvent) => {
  this.socketsOpen = Math.max(0, this.socketsOpen - 1);
  closeHandler?.(ev);
};
Object.defineProperty(ws, 'onclose', {
  configurable: true,
  get: () => (closeHandler ? wrappedClose : wrappedClose),
  set: (fn) => {
    closeHandler = fn;
  },
});
if (typeof ws.addEventListener === 'function') ws.addEventListener('close', wrappedClose);
```

and in the `FakeSocket` test class keep `close()` calling `this.onclose?.()`, which hits the getter. Apply the same `lastEvent` dedupe for close events.

Module-level helpers in `clientPerf.ts`:

```ts
function socketPath(url: string): string {
  let pathname: string;
  try {
    pathname = new URL(url, 'http://x').pathname;
  } catch {
    return url;
  }
  return pathname.replace(/^\/ws\/(terminal|chat|logs\/fence)\/[^/]+/, '/ws/$1/:id');
}
function messageBytes(data: unknown): number {
  if (typeof data === 'string') return data.length;
  if (data instanceof ArrayBuffer) return data.byteLength;
  if (ArrayBuffer.isView(data)) return data.byteLength;
  if (typeof Blob !== 'undefined' && data instanceof Blob) return data.size;
  return 0;
}
const TYPE_RE = /"type"\s*:\s*"([^"]+)"/;
function messageType(data: unknown): string {
  if (typeof data !== 'string') return 'binary';
  const m = TYPE_RE.exec(data.slice(0, 200));
  return m ? m[1] : 'untyped';
}
```

In `tick()`, before building the row: `const render = inputLatency.getRenderStats(); this.sampleTerminals(render?.median ?? 0, render?.p99 ?? 0);` with `import { inputLatency } from './inputLatency';`.

- [ ] **Step 4: Wrap at the transport boundary**

Replace `assets/dashboard/src/lib/transport.ts:6-16` with:

```ts
import { clientPerf } from './clientPerf';

export const liveTransport: Transport = {
  createWebSocket: (url: string) => new WebSocket(url),
  fetch: (input, init) => fetch(input, init),
};

function instrumented(t: Transport): Transport {
  return {
    ...t,
    createWebSocket: (url: string) => clientPerf.wrapSocket(t.createWebSocket(url), url),
  };
}

// Module-level singleton. ESM named exports are live bindings,
// so consumers importing `transport` see the swapped value.
export let transport: Transport = instrumented(liveTransport);

export function setTransport(t: Transport) {
  transport = instrumented(t);
}
```

Keep whatever other members `Transport` already has (line 1-5 shows `fetch` is used at `terminalStream.ts:39`); copy the existing `liveTransport` object and only change `createWebSocket`. If `clientPerf.ts` importing `inputLatency` and `transport.ts` importing `clientPerf` creates a cycle with `terminalStream.ts`, it is import-order safe because `clientPerf` is constructed without touching `inputLatency` until `tick()`.

- [ ] **Step 5: Terminals and chat loads**

`terminalStream.ts`: add a field `private perfBytes = 0;` and `private perfUnregister: (() => void) | null = null;`. At line 1229 after `this.ws = transport.createWebSocket(wsUrl);`:

```ts
this.perfUnregister?.();
this.perfUnregister = clientPerf.registerTerminal(this.sessionId, () => ({
  frames: this.diagnostics?.framesReceived ?? 0,
  bytes: this.perfBytes,
}));
```

At line 1761 next to `this.diagnostics.recordFrame(new Uint8Array(data));` add `this.perfBytes += (data as ArrayBuffer).byteLength;` placed so it runs for every frame even when diagnostics is null. In `disconnect()` (line 1314) add `this.perfUnregister?.(); this.perfUnregister = null;`. The constructor already receives `sessionId` (line 336); if it is not stored on the instance, store it as `private readonly sessionId: string`.

`loadTelemetry.ts:60`:

```ts
export function captureChatLoad(sample: ChatLoadSample): void {
  clientPerf.recordChatLoad(sample);
  void upload([sample], []).then((sent) => {
    if (sent) void migrateLegacySamples();
  });
}
```

with `import { clientPerf } from '../clientPerf';`.

- [ ] **Step 6: Run tests**

Run: `./test.sh --quick`
Expected: PASS, including existing `mockTransport.test.ts`, `socket.test.ts`, `streamDiagnostics.test.ts`. If `mockTransport.test.ts` asserts identity of the socket returned by `createWebSocket`, it still holds: `wrapSocket` returns the same object.

- [ ] **Step 7: Commit**

`./format.sh`, then `/commit` with `feat(dashboard): record websocket, terminal, and chat load activity`.

---

### Task 6: Profilers, config switch, build, workload, route changes

**Files:**

- Modify: `assets/dashboard/src/contexts/ConfigContext.tsx` (apply `config.client_performance?.enabled` on every config response)
- Modify: `assets/dashboard/src/components/AppShell.tsx:634`, `:1117`, `:96` (route effect), `:142-231` (build/workload effects)
- Modify: `assets/dashboard/src/components/chat/ChatView.tsx` (Profiler around `ChatTranscript`)
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx:701-712`
- Test: `assets/dashboard/src/lib/clientPerf.test.ts` (commit threshold), `assets/dashboard/src/contexts/ConfigContext.test.tsx` (switch applied)

- [ ] **Step 1: Write the failing tests**

Add to `clientPerf.test.ts`:

```ts
it('keeps commits over 16ms and sums all commit time into the row', () => {
  const { c, advance } = collector();
  c.start();
  c.recordRoute('/sessions/x');
  c.recordCommit('sidebar', 'update', 5);
  c.recordCommit('main', 'update', 40);
  advance(1000);
  c.tick();
  const file = c.buildFile();
  expect(file.commits).toEqual([
    { t: 1_000_000, id: 'main', phase: 'update', duration: 40, route: '/sessions/x' },
  ]);
  expect(file.timeline[0].commit).toBe(45);
});
```

In `ConfigContext.test.tsx` (create if absent, following the existing context tests), mock `getConfig` to resolve `{ ...DEFAULT, client_performance: { enabled: true, repo: '', target: '' } }` and assert `clientPerf.setConfigEnabled` was called with `true`, using `vi.spyOn(clientPerf, 'setConfigEnabled')`.

- [ ] **Step 2: Run to verify failure**

Run: `./test.sh --quick`
Expected: the ConfigContext test fails (spy not called).

- [ ] **Step 3: Apply the config switch**

In `ConfigContext.tsx`, where the loaded config is stored (around line 150, the `setConfig(data)` call after `getConfig()`), add:

```ts
clientPerf.setConfigEnabled(Boolean(data.client_performance?.enabled));
```

and after the provider mounts, once: `void clientPerf.restore();` inside the same effect after the first successful `setConfigEnabled(true)`. `restore()` is a no-op when either switch is off.

- [ ] **Step 4: Profilers**

`AppShell.tsx`: import `{ Profiler } from 'react'` and `{ clientPerf } from '../lib/clientPerf'`. Wrap the children of `<nav className="app-shell__nav">` (line 634) in `<Profiler id="sidebar" onRender={onPerfRender}>` and the children of `<main className="app-shell__content">` (line 1117) in `<Profiler id="main" onRender={onPerfRender}>`, with:

```ts
const onPerfRender: React.ProfilerOnRenderCallback = (id, phase, actualDuration) =>
  clientPerf.recordCommit(id, phase, actualDuration);
```

at module level. `ChatView.tsx`: wrap `<ChatTranscript ... />` in `<Profiler id="chat-transcript" onRender={onPerfRender}>` with the same module-level callback. `SessionDetailPage.tsx:703`: inside the existing `onRender`, first line `clientPerf.recordCommit(_id, phase, actualDuration);` (rename `_id` to `id`).

- [ ] **Step 5: Build, workload, route, remote client**

In `AppShell.tsx`, after the existing `getDevStatus()` effect (line 228) add:

```ts
useEffect(() => {
  clientPerf.setBuild({
    version: versionInfo?.version ?? '',
    devMode: !!versionInfo?.dev_mode,
    sourceWorkspace: devStatus?.source_workspace ?? '',
    viteDev: import.meta.env.DEV,
  });
}, [versionInfo, devStatus]);

useEffect(() => {
  const sessions = workspaces.flatMap((w) => w.sessions);
  const socketsByPath: Record<string, number> = {};
  clientPerf.setWorkload({
    workspaces: workspaces.length,
    sessions: sessions.length,
    running: sessions.filter((s) => s.running).length,
    chats: sessions.filter((s) => s.kind === 'chat').length,
    terminals: sessions.filter((s) => s.kind !== 'chat').length,
    mountedTerminals: clientPerf.terminalCount(),
    socketsByPath,
    lastDashboardMessageBytes: clientPerf.lastDashboardMessageBytes(),
    panels: config.ui?.panels ?? {},
    flags: {
      chatSessions: !!config.chat_sessions,
      chatLoadProfiling: !!config.chat_load_profiling_enabled,
      desync: !!config.desync?.enabled,
      ioWorkspaceTelemetry: !!config.io_workspace_telemetry?.enabled,
    },
  });
}, [workspaces, config]);

useEffect(() => {
  clientPerf.recordRoute(location.pathname);
  clientPerf.recordNavigation({ t: Date.now(), kind: 'route', route: location.pathname });
}, [location.pathname]);
```

`socketsByPath` is filled from the collector: add `socketsByPath(): Record<string, number>` to `ClientPerfCollector` that counts open sockets per `socketPath` (maintain a `Map<string, number>` in `wrapSocket` open/close instead of the single `socketsOpen` counter, and derive `socketCount()` as the sum). Replace `socketsByPath` above with `clientPerf.socketsByPath()`. Use the config field names exactly as `types.generated.ts` spells them; `chat_sessions` is on `ConfigResponse` per `internal/config/config.go` (`grep -n chat_sessions internal/api/contracts/config.go`).

For `environment().remoteClient`, set it from `AppShell` once: `clientPerf.setRemoteClient(isRemoteClient())` (add `private remote = false; setRemoteClient(v: boolean) { this.remote = v; }` and use `this.remote` in `environment()`).

Time to first commit after a route change: in `recordNavigation` for `kind: 'route'`, store the record; in `recordCommit`, if a pending route record has no `firstCommitMs`, set it to `this.t() - record.t`. Paint: in the route effect, `requestAnimationFrame(() => requestAnimationFrame(() => clientPerf.recordPaint(Date.now())))` with `recordPaint(t)` filling `paintMs` on the same pending record.

- [ ] **Step 6: Run tests**

Run: `./test.sh --quick`
Expected: PASS. `AppShell` tests must still render; `Profiler` is inert in tests.

- [ ] **Step 7: Commit**

`./format.sh`, then `/commit` with `feat(dashboard): wire profilers, build, and workload into the client performance recorder`.

---

### Task 7: The endpoint and the prompt

**Files:**

- Create: `internal/dashboard/handlers_client_performance.go`
- Create: `internal/dashboard/client_performance_prompt.md`
- Create: `internal/api/contracts/client_performance.go`
- Modify: `internal/dashboard/handlers_restart.go:161-169` (extract package func)
- Modify: `internal/dashboard/server.go:887` (construct), `:1183-1190` (route)
- Test: `internal/dashboard/handlers_client_performance_test.go`
- Modify: `docs/api.md` (new endpoint section before `### GET /api/dev/status` at line 5235)

**Interfaces:**

- Consumes: config getters from Task 1; `isSchmuxWorkspace(path string) bool` (`handlers_dev.go:48`); `workspace.WorkspaceManager.GetByID`, `GetOrCreate`; `session.Manager.IsRunning`, `Spawn`.
- Produces: `POST /api/client-performance/session` with `contracts.ClientPerformanceSessionRequest{WorkspaceID, SessionID string}` → `contracts.ClientPerformanceSessionResponse{WorkspaceID, SessionID string}`.

- [ ] **Step 1: Contracts**

Create `internal/api/contracts/client_performance.go`:

```go
package contracts

// ClientPerformanceSessionRequest carries the ids the recording browser kept
// from its last call, both empty the first time.
type ClientPerformanceSessionRequest struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}

// ClientPerformanceSessionResponse is the pair that is valid now.
type ClientPerformanceSessionResponse struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}
```

Run `go run ./cmd/gen-types`.

- [ ] **Step 2: Write the failing tests**

Create `internal/dashboard/handlers_client_performance_test.go`:

```go
package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
)

func schmuxCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+schmuxModulePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type cpFixture struct {
	h       *ClientPerformanceHandlers
	st      *state.State
	spawned []session.SpawnOptions
	created []string
}

func newCPFixture(t *testing.T) *cpFixture {
	t.Helper()
	server, cfg, st := newTestServer(t)
	enabled := true
	cfg.Repos = []config.Repo{{Name: "schmux", URL: "https://example.com/schmux.git"}}
	cfg.ClientPerformance = &config.ClientPerformanceConfig{Enabled: &enabled, Repo: "schmux", Target: "command"}
	cfg.ChatSessions = &enabled
	f := &cpFixture{st: st}
	f.h = newClientPerformanceHandlers(server)
	f.h.hasChatMode = func(string) bool { return true }
	f.h.getOrCreate = func(_ context.Context, repoURL, branch string) (*state.Workspace, error) {
		f.created = append(f.created, repoURL+"@"+branch)
		ws := state.Workspace{ID: "ws-new", Repo: repoURL, Branch: branch, Path: schmuxCheckout(t)}
		if err := st.AddWorkspace(ws); err != nil {
			t.Fatal(err)
		}
		return &ws, nil
	}
	f.h.spawn = func(_ context.Context, opts session.SpawnOptions) (*state.Session, error) {
		f.spawned = append(f.spawned, opts)
		return &state.Session{ID: "sess-new", WorkspaceID: opts.WorkspaceID}, nil
	}
	return f
}

func (f *cpFixture) post(t *testing.T, req contracts.ClientPerformanceSessionRequest) (int, contracts.ClientPerformanceSessionResponse, string) {
	t.Helper()
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/client-performance/session", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	f.h.handleEnsureSession(rr, r)
	var resp contracts.ClientPerformanceSessionResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return rr.Code, resp, rr.Body.String()
}

func TestClientPerformance_ConfigChecks(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*config.Config, *cpFixture)
		want string
	}{
		{"disabled", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Enabled = nil }, "client performance recording is disabled"},
		{"repo unset", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Repo = "" }, "client_performance.repo is not set"},
		{"repo not in config", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Repo = "other" }, "client_performance.repo other is not a configured repo"},
		{"target unset", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Target = "" }, "client_performance.target is not set"},
		{"chat sessions off", func(c *config.Config, _ *cpFixture) { off := false; c.ChatSessions = &off }, "chat sessions are disabled (chat_sessions)"},
		{"target without chat mode", func(_ *config.Config, f *cpFixture) { f.h.hasChatMode = func(string) bool { return false } }, "target command has no chat mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCPFixture(t)
			tc.mut(f.h.config, f)
			code, _, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
			if code != http.StatusBadRequest || !bytes.Contains([]byte(body), []byte(tc.want)) {
				t.Fatalf("code=%d body=%s want 400 containing %q", code, body, tc.want)
			}
		})
	}
}

func TestClientPerformance_EmptyIdsCreatesAndSpawns(t *testing.T) {
	f := newCPFixture(t)
	code, resp, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	if f.created[0] != "https://example.com/schmux.git@client-performance" {
		t.Errorf("created %v", f.created)
	}
	s := f.spawned[0]
	if s.WorkspaceID != "ws-new" || s.TargetName != "command" || s.Kind != state.SessionKindChat || s.Prompt == "" {
		t.Errorf("spawn opts %+v", s)
	}
	if resp != (contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-new", SessionID: "sess-new"}) {
		t.Errorf("resp %+v", resp)
	}
}

func TestClientPerformance_RunningSessionReturnedAsSent(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Path: schmuxCheckout(t)})
	_ = f.st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Target: "command", Kind: state.SessionKindChat, Pid: os.Getpid()})
	code, resp, _ := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1", SessionID: "sess-1"})
	if code != http.StatusOK || resp.SessionID != "sess-1" || len(f.spawned) != 0 {
		t.Fatalf("code=%d resp=%+v spawned=%d", code, resp, len(f.spawned))
	}
}

func TestClientPerformance_EndedSessionRespawnsInLiveWorkspace(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Path: schmuxCheckout(t)})
	_ = f.st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Target: "command", Kind: state.SessionKindChat, Pid: 999999})
	code, resp, _ := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1", SessionID: "sess-1"})
	if code != http.StatusOK || resp.WorkspaceID != "ws-1" || resp.SessionID != "sess-new" || len(f.created) != 0 {
		t.Fatalf("code=%d resp=%+v created=%v", code, resp, f.created)
	}
}

func TestClientPerformance_StaleWorkspaceIdsFallThrough(t *testing.T) {
	cases := []struct {
		name string
		ws   state.Workspace
	}{
		{"disposing", state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Status: state.WorkspaceStatusDisposing}},
		{"different repo", state.Workspace{ID: "ws-1", Repo: "https://example.com/other.git", Branch: clientPerformanceBranch}},
		{"different branch", state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: "main"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCPFixture(t)
			tc.ws.Path = schmuxCheckout(t)
			_ = f.st.AddWorkspace(tc.ws)
			code, resp, _ := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1", SessionID: "gone"})
			if code != http.StatusOK || resp.WorkspaceID != "ws-new" || len(f.created) != 1 {
				t.Fatalf("code=%d resp=%+v created=%v", code, resp, f.created)
			}
		})
	}
}

func TestClientPerformance_RefusesNonSchmuxCheckout(t *testing.T) {
	f := newCPFixture(t)
	f.h.getOrCreate = func(_ context.Context, repoURL, branch string) (*state.Workspace, error) {
		ws := state.Workspace{ID: "ws-plain", Repo: repoURL, Branch: branch, Path: t.TempDir()}
		_ = f.st.AddWorkspace(ws)
		return &ws, nil
	}
	code, _, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
	if code != http.StatusBadRequest || !bytes.Contains([]byte(body), []byte("is not a schmux checkout")) || len(f.spawned) != 0 {
		t.Fatalf("code=%d body=%s spawned=%d", code, body, len(f.spawned))
	}
}
```

If `cfg.ChatSessions` is not a `*bool`, set it the way `GetChatSessions()` in `internal/config/config.go` reads it (`grep -n 'func (c \*Config) GetChatSessions' -A 8`).

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/dashboard -run TestClientPerformance -v`
Expected: build failure, `undefined: ClientPerformanceHandlers`.

- [ ] **Step 4: Extract `resolveTargetTool`**

In `handlers_restart.go:161`, replace the method body with a call to a package function and add the function:

```go
func (h *SpawnHandlers) resolveTargetTool(name string) string {
	return resolveTargetTool(h.models, name)
}

// resolveTargetTool maps a target name to its harness tool: a catalog model
// resolves through the model manager; a bare tool name resolves to itself.
func resolveTargetTool(m *models.Manager, name string) string {
	if model, ok := m.FindModel(name); ok {
		return m.ResolveToolForModel(model)
	}
	if detect.IsToolName(name) {
		return name
	}
	return ""
}
```

- [ ] **Step 5: Write the prompt**

Create `internal/dashboard/client_performance_prompt.md`:

```markdown
You are in this chat to diagnose and fix why the schmux dashboard is slow for the user you are talking to.

This workspace is a checkout of schmux, the dashboard's source. The user's browser attaches a recording to its messages: a JSON file of what the browser's main thread was doing while the dashboard felt slow. The format is documented in docs/client-performance.md in this checkout. Read that file before the first recording.

For each recording:

1. Read `timeline` and find the stalls: seconds where the event loop delay or the long task total reached 100ms. The `stalls` array lists their indexes.
2. For each stall, read the detail buffers for that second: `longTasks`, `commits`, `websocket.perSecond`, `websocket.individual`, `fetches`, `terminals`, `interactions`. Note the route and whether the tab was hidden.
3. Name a cause from what the stalls share, then confirm it in the dashboard source under assets/dashboard/src before you say it. If the code cannot produce the stall, the cause is wrong.
4. Check `build`. If `viteDev` is false, `commits` is empty because React's Profiler only fires in the development build; say so instead of reading it as evidence.

Ask the user one or two questions at a time, in plain language. Prefer a question to a guess: what they were doing, how long it felt, whether a reload clears it, whether it builds over time. When you need another recording, say exactly what the user should do while it runs.

Make the fix on this branch. Then tell the user to switch dev mode to this workspace from the sidebar panel, reload, and record again. The next recording's `build.sourceWorkspace` names this workspace. Compare the new file with the earlier one on the measures that named the cause (stall count, worst event loop delay, handler time per message type, commit time per Profiler id, heap slope) and say whether the cause is gone.

Reply to this message briefly. The user's first message, with the first recording, comes next.
```

- [ ] **Step 6: Write the handler**

Create `internal/dashboard/handlers_client_performance.go`:

```go
package dashboard

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

//go:embed client_performance_prompt.md
var clientPerformancePrompt string

// clientPerformanceBranch is fixed so the recording browser lands in the same
// workspace every time and never adopts one of the user's own branches.
const clientPerformanceBranch = "client-performance"

// ClientPerformanceHandlers ensures the performance chat exists. The daemon
// stores nothing about it; the browser sends the ids it kept.
type ClientPerformanceHandlers struct {
	config    *config.Config
	state     state.StateStore
	workspace workspace.WorkspaceManager
	session   *session.Manager
	logger    *log.Logger

	// Seams for tests; production wiring points at the managers.
	hasChatMode func(target string) bool
	getOrCreate func(ctx context.Context, repoURL, branch string) (*state.Workspace, error)
	spawn       func(ctx context.Context, opts session.SpawnOptions) (*state.Session, error)
	isRunning   func(ctx context.Context, sessionID string) bool
}

func newClientPerformanceHandlers(s *Server) *ClientPerformanceHandlers {
	h := &ClientPerformanceHandlers{config: s.config, state: s.state, workspace: s.workspace, session: s.session, logger: s.logger}
	m := s.models
	h.hasChatMode = func(target string) bool {
		adapter := detect.GetAdapter(resolveTargetTool(m, target))
		return adapter != nil && adapter.ChatArgs(nil, false, "") != nil
	}
	h.getOrCreate = s.workspace.GetOrCreate
	h.spawn = s.session.Spawn
	h.isRunning = s.session.IsRunning
	return h
}

// handleEnsureSession is POST /api/client-performance/session.
func (h *ClientPerformanceHandlers) handleEnsureSession(w http.ResponseWriter, r *http.Request) {
	var req contracts.ClientPerformanceSessionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	if !h.config.GetClientPerformanceEnabled() {
		writeJSONError(w, "client performance recording is disabled", http.StatusBadRequest)
		return
	}
	repoName := h.config.GetClientPerformanceRepo()
	if repoName == "" {
		writeJSONError(w, "client_performance.repo is not set", http.StatusBadRequest)
		return
	}
	repoURL := ""
	for _, repo := range h.config.GetRepos() {
		if repo.Name == repoName {
			repoURL = repo.URL
			break
		}
	}
	if repoURL == "" {
		writeJSONError(w, fmt.Sprintf("client_performance.repo %s is not a configured repo", repoName), http.StatusBadRequest)
		return
	}
	target := h.config.GetClientPerformanceTarget()
	if target == "" {
		writeJSONError(w, "client_performance.target is not set", http.StatusBadRequest)
		return
	}
	if !h.config.GetChatSessions() {
		writeJSONError(w, "chat sessions are disabled (chat_sessions)", http.StatusBadRequest)
		return
	}
	if !h.hasChatMode(target) {
		writeJSONError(w, fmt.Sprintf("target %s has no chat mode", target), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if req.SessionID != "" && h.isRunning(ctx, req.SessionID) {
		writeJSON(w, contracts.ClientPerformanceSessionResponse{WorkspaceID: req.WorkspaceID, SessionID: req.SessionID})
		return
	}

	var ws *state.Workspace
	if req.WorkspaceID != "" {
		if cand, ok := h.workspace.GetByID(req.WorkspaceID); ok &&
			cand.Status != state.WorkspaceStatusDisposing &&
			cand.Repo == repoURL && cand.Branch == clientPerformanceBranch {
			ws = cand
		}
	}
	if ws == nil {
		created, err := h.getOrCreate(ctx, repoURL, clientPerformanceBranch)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("failed to prepare workspace: %v", err), http.StatusInternalServerError)
			return
		}
		ws = created
	}
	if !isSchmuxWorkspace(ws.Path) {
		writeJSONError(w, fmt.Sprintf("workspace %s is not a schmux checkout", ws.ID), http.StatusBadRequest)
		return
	}

	sess, err := h.spawn(ctx, session.SpawnOptions{
		WorkspaceID: ws.ID,
		TargetName:  target,
		Prompt:      clientPerformancePrompt,
		Kind:        state.SessionKindChat,
	})
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to spawn performance chat: %v", err), http.StatusInternalServerError)
		return
	}
	h.logger.Info("client performance chat ready", "workspace_id", ws.ID, "session_id", sess.ID)
	writeJSON(w, contracts.ClientPerformanceSessionResponse{WorkspaceID: ws.ID, SessionID: sess.ID})
}
```

Use the JSON writer the other handlers use; if it is not `writeJSON`, grep `handlers_dev.go` for the pattern (`w.Header().Set("Content-Type", "application/json")` + `json.NewEncoder(w).Encode`) and match it. `h.config.GetChatSessions()` exists (`handlers_spawn.go:244`).

- [ ] **Step 7: Register the route**

In `server.go` next to `spawnH := &SpawnHandlers{` (line 887): `cpH := newClientPerformanceHandlers(s)`. In the dev-mode CSRF group (line 1185-1190), add `r.Post("/client-performance/session", cpH.handleEnsureSession)`. If `s.workspace`/`s.session`/`s.models` are not yet set when line 887 runs, construct `cpH` where `spawnH` is constructed since it uses the same fields.

- [ ] **Step 8: Run tests**

Run: `go test ./internal/dashboard -run 'TestClientPerformance|TestAPIContract' -v`
Expected: PASS. Then `go test ./internal/... ` to confirm nothing else moved.

- [ ] **Step 9: Document the endpoint**

In `docs/api.md`, before `### GET /api/dev/status` (line 5235):

```markdown
### POST /api/client-performance/session

Dev mode only. Ensures a running performance chat exists and returns its ids. See `docs/client-performance.md`.

Request: `{"workspace_id": "", "session_id": ""}`, the ids the browser kept from its last call (both empty the first time).

Response: `{"workspace_id": "...", "session_id": "..."}`, the pair that is valid now. If `session_id` is running, the request pair is returned unchanged. Otherwise the workspace is settled first: the sent `workspace_id` if it exists, is not disposing, and is on the configured repo and the branch `client-performance`; else a workspace is found or created for that repo and branch. A workspace whose `go.mod` is not schmux's module is refused with 400. A new chat session is then spawned in the workspace with the configured target and the embedded prompt.

Errors (400, in order): `client performance recording is disabled`; `client_performance.repo is not set`; `client_performance.repo <name> is not a configured repo`; `client_performance.target is not set`; `chat sessions are disabled (chat_sessions)`; `target <name> has no chat mode`; `workspace <id> is not a schmux checkout`. Requires CSRF.
```

- [ ] **Step 10: Commit**

`./format.sh`, then `/commit` with `feat(dashboard): add client performance chat endpoint and prompt`.

---

### Task 8: The sidebar pane

**Files:**

- Create: `assets/dashboard/src/components/ClientPerformance.tsx`
- Create: `assets/dashboard/src/hooks/useClientPerf.ts`
- Modify: `assets/dashboard/src/lib/api.ts` (add `ensureClientPerformanceSession`)
- Modify: `assets/dashboard/src/components/AppShell.tsx:1110` (render)
- Modify: `assets/dashboard/src/styles/global.css` (after `.typing-perf` block at line 382; use Grep to find its end)
- Test: `assets/dashboard/src/components/ClientPerformance.test.tsx`
- Modify: `docs/web.md` (new subsection under Session Detail's sidebar notes, or after `### Event Monitor` at line 322)

**Interfaces:**

- Produces: `useClientPerf(): { recording: boolean; startedAt: number | null; stalls: number; unsent: boolean; chat: ChatIds | null }`; `ensureClientPerformanceSession(ids: ClientPerformanceSessionRequest): Promise<ClientPerformanceSessionResponse>`.

- [ ] **Step 1: Write the failing tests**

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import ClientPerformance from './ClientPerformance';
import { clientPerf } from '../lib/clientPerf';
import * as api from '../lib/api';

const navigate = vi.fn();
vi.mock('react-router', async (orig) => ({
  ...(await orig<typeof import('react-router')>()),
  useNavigate: () => navigate,
}));
const waitForSession = vi.fn().mockResolvedValue(true);
vi.mock('../contexts/SessionsContext', () => ({ useSessions: () => ({ waitForSession }) }));
let config = { client_performance: { enabled: true, repo: 'schmux', target: 'claude' } };
vi.mock('../contexts/ConfigContext', () => ({ useConfig: () => ({ config }) }));
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}));

describe('ClientPerformance pane', () => {
  beforeEach(() => {
    localStorage.clear();
    clientPerf.setConfigEnabled(true);
    clientPerf.stop();
    navigate.mockClear();
  });

  it('shows Start recording when off and the status line when on', async () => {
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    expect(screen.getByText('Client Performance')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Start recording' }));
    expect(clientPerf.isRecording()).toBe(true);
    expect(screen.getByText(/Recording 0 min · 0 stalls/)).toBeInTheDocument();
    expect(screen.getByText('Client Performance · REC')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open performance chat' })).toBeInTheDocument();
  });

  it('shows the config hint instead of the chat link when repo or target is unset', () => {
    config = { client_performance: { enabled: true, repo: '', target: 'claude' } };
    clientPerf.start();
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    expect(screen.getByRole('link', { name: /Pick a repo and target/ })).toHaveAttribute(
      'href',
      '/config?tab=advanced'
    );
    expect(screen.queryByRole('button', { name: 'Open performance chat' })).toBeNull();
    config = { client_performance: { enabled: true, repo: 'schmux', target: 'claude' } };
  });

  it('opens the chat: posts the kept ids, keeps the returned pair, navigates', async () => {
    const ensure = vi
      .spyOn(api, 'ensureClientPerformanceSession')
      .mockResolvedValue({ workspace_id: 'ws-1', session_id: 'sess-1' });
    clientPerf.start();
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    await userEvent.click(screen.getByRole('button', { name: 'Open performance chat' }));
    expect(ensure).toHaveBeenCalledWith({ workspace_id: '', session_id: '' });
    expect(clientPerf.getChat()).toEqual({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    expect(waitForSession).toHaveBeenCalledWith('sess-1');
    expect(navigate).toHaveBeenCalledWith('/sessions/sess-1');
  });

  it('shows the endpoint error in the pane', async () => {
    vi.spyOn(api, 'ensureClientPerformanceSession').mockRejectedValue(
      new Error('workspace ws-9 is not a schmux checkout')
    );
    clientPerf.start();
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    await userEvent.click(screen.getByRole('button', { name: 'Open performance chat' }));
    expect(await screen.findByText('workspace ws-9 is not a schmux checkout')).toBeInTheDocument();
  });
});
```

Check the Config page's tab query parameter name by reading `assets/dashboard/src/routes/ConfigPage.tsx` for `useSearchParams`/`tab`; use the real one in the hint link.

- [ ] **Step 2: Run to verify failure**

Run: `./test.sh --quick`
Expected: import failure for `ClientPerformance`.

- [ ] **Step 3: API client and hook**

`api.ts`, after `getDevStatus` (line 1292):

```ts
export async function ensureClientPerformanceSession(
  ids: ClientPerformanceSessionRequest
): Promise<ClientPerformanceSessionResponse> {
  const response = await apiFetch('/api/client-performance/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(ids),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to open performance chat');
  return response.json();
}
```

Import the two types from `./types` (re-exported from `types.generated.ts`; add them to the re-export list in `types.ts` beside `ConfigResponse`).

`hooks/useClientPerf.ts`:

```ts
import { useSyncExternalStore } from 'react';
import { clientPerf } from '../lib/clientPerf';

export function useClientPerf() {
  const version = useSyncExternalStore(
    (cb) => clientPerf.subscribe(cb),
    () => clientPerf.version(),
    () => 0
  );
  void version;
  return {
    recording: clientPerf.isRecording(),
    startedAt: clientPerf.startedAt(),
    stalls: clientPerf.stallCount(),
    unsent: clientPerf.hasUnsent(),
    chat: clientPerf.getChat(),
  };
}
```

Add `private ver = 0; version() { return this.ver; }` to `ClientPerfCollector` and increment `this.ver` inside `notify()`.

- [ ] **Step 4: The pane**

```tsx
import { useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useModal } from './ModalProvider';
import { useClientPerf } from '../hooks/useClientPerf';
import { clientPerf } from '../lib/clientPerf';
import { ensureClientPerformanceSession, getErrorMessage } from '../lib/api';

export default function ClientPerformance() {
  const { config } = useConfig();
  const { waitForSession } = useSessions();
  const { confirm } = useModal();
  const navigate = useNavigate();
  const perf = useClientPerf();
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('client-perf-collapsed') === '1'
  );
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const toggleCollapsed = () =>
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem('client-perf-collapsed', next ? '1' : '0');
      return next;
    });

  const configured = Boolean(config.client_performance?.repo && config.client_performance?.target);
  const minutes = perf.startedAt ? Math.floor((Date.now() - perf.startedAt) / 60000) : 0;

  const openChat = async () => {
    setOpening(true);
    setError(null);
    try {
      const kept = clientPerf.getChat();
      const ids = await ensureClientPerformanceSession({
        workspace_id: kept?.workspaceId ?? '',
        session_id: kept?.sessionId ?? '',
      });
      clientPerf.setChat({ workspaceId: ids.workspace_id, sessionId: ids.session_id });
      await waitForSession(ids.session_id);
      navigate(`/sessions/${ids.session_id}`);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to open performance chat'));
    } finally {
      setOpening(false);
    }
  };

  const stop = async () => {
    if (perf.unsent) {
      const ok = await confirm({
        title: 'Stop recording?',
        message: 'The current recording has not been sent and will be discarded.',
        confirmLabel: 'Stop',
      });
      if (!ok) return;
    }
    clientPerf.stop();
  };

  return (
    <div className="client-perf" data-testid="client-perf-pane">
      <div className="client-perf__header">
        <button className="diag-pane__toggle" onClick={toggleCollapsed}>
          <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
            ▶
          </span>
          <span className="nav-section-title">
            {perf.recording ? 'Client Performance · REC' : 'Client Performance'}
          </span>
        </button>
      </div>
      {!collapsed && !perf.recording && (
        <div className="client-perf__body">
          <p className="client-perf__text">
            Records what this browser is doing while the dashboard feels slow. Recordings are sent
            to an agent in a chat.
          </p>
          <button className="btn btn--secondary btn--sm" onClick={() => clientPerf.start()}>
            Start recording
          </button>
        </div>
      )}
      {!collapsed && perf.recording && (
        <div className="client-perf__body">
          <div className="client-perf__status">{`Recording ${minutes} min · ${perf.stalls} stalls`}</div>
          {configured ? (
            <button className="btn btn--secondary btn--sm" onClick={openChat} disabled={opening}>
              Open performance chat
            </button>
          ) : (
            <Link className="client-perf__hint" to="/config?tab=advanced">
              Pick a repo and target on the Config page, Advanced tab.
            </Link>
          )}
          {error && (
            <div className="client-perf__error" role="alert">
              {error}
            </div>
          )}
          <button className="btn btn--ghost btn--sm" onClick={stop}>
            Stop recording
          </button>
        </div>
      )}
    </div>
  );
}
```

Match the button classes and the `confirm` signature to what `SessionDetailPage.tsx` uses (`grep -n 'confirm(' assets/dashboard/src/routes/SessionDetailPage.tsx | head -3`). Render it in `AppShell.tsx` after `TypingPerformance` (line 1108):

```tsx
{
  isDevMode && config.client_performance?.enabled && <ClientPerformance />;
}
```

`AppShell` already has `config` from `useConfig()` (check; if not, add it).

CSS in `global.css`, after the `.typing-perf` rules, using only tokens the style guide defines:

```css
.client-perf {
  padding: var(--space-xs) var(--space-sm);
}
.client-perf__header {
  display: flex;
  align-items: center;
}
.client-perf__body {
  display: flex;
  flex-direction: column;
  gap: var(--space-xs);
  padding-top: var(--space-xs);
}
.client-perf__text,
.client-perf__hint {
  font-size: var(--font-size-xs);
  color: var(--color-text-secondary);
}
.client-perf__status {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
}
.client-perf__error {
  font-size: var(--font-size-xs);
  color: var(--color-danger);
}
```

Confirm each token exists with `grep -n -- '--space-xs:\|--font-size-xs:\|--color-text-secondary:\|--font-mono:\|--color-danger:' assets/dashboard/src/styles/global.css`; substitute the defined names if any differ.

- [ ] **Step 5: Run tests and the style check**

Run: `./test.sh --quick`
Expected: PASS. Then run the `dashboard-style-check` skill against `ClientPerformance.tsx` and the new CSS.

- [ ] **Step 6: Docs**

`docs/web.md`, after `### Event Monitor` (line 322):

```markdown
### Client Performance (sidebar pane, dev mode only)

Shown when `client_performance.enabled` is set and the daemon reports `dev_mode`. **Start recording** turns on a browser-side recorder; the header reads `Client Performance · REC` and the pane shows `Recording N min · N stalls`. **Open performance chat** asks the daemon for a chat session in a schmux checkout on the branch `client-performance` and navigates to it; the browser keeps the returned ids. **Stop recording** asks first when there is unsent data. In the performance chat, the composer shows a `Recording since HH:MM · attach` checkbox; sending with it checked uploads the recording as a workspace attachment and appends its path to the message. See `docs/client-performance.md`.
```

- [ ] **Step 7: Commit**

`./format.sh`, then `/commit` with `feat(dashboard): add Client Performance sidebar pane`.

---

### Task 9: The composer checkbox

**Files:**

- Modify: `assets/dashboard/src/components/chat/Composer.tsx:24-39` (props), `:139-149` (submit)
- Modify: `assets/dashboard/src/components/chat/ChatView.tsx:14-53` (prop), `:154-162` (pass through)
- Modify: `assets/dashboard/src/routes/ChatSessionPage.tsx:398` (compute and pass)
- Test: `assets/dashboard/src/components/chat/Composer.test.tsx`

**Interfaces:**

- Consumes: `clientPerf.buildFile()`, `clientPerf.markSent()`, `useClientPerf()`.
- Produces: `Composer` prop `recordingSince?: number | null`; `ChatView` prop `recordingSince?: number | null`.

- [ ] **Step 1: Write the failing tests**

Add to `Composer.test.tsx`, following its existing upload tests:

```tsx
describe('recording attach checkbox', () => {
  it('is hidden without recordingSince', () => {
    render(<Composer workspaceId="ws-1" disabled={false} ended={false} onSend={vi.fn()} />);
    expect(screen.queryByLabelText(/Recording since/)).toBeNull();
  });

  it('uploads the recording, appends its path, sends, and marks sent', async () => {
    const upload = vi.spyOn(api, 'uploadWorkspaceAttachment').mockResolvedValue({
      name: 'client-perf-1.json',
      path: '/ws/.schmux/attachments/u1/client-perf-1.json',
    });
    const markSent = vi.spyOn(clientPerf, 'markSent');
    vi.spyOn(clientPerf, 'buildFile').mockReturnValue({ version: 1, timeline: [] } as never);
    const onSend = vi.fn();
    render(
      <Composer
        workspaceId="ws-1"
        disabled={false}
        ended={false}
        onSend={onSend}
        recordingSince={Date.UTC(2026, 9, 8, 10, 42)}
      />
    );
    expect(screen.getByLabelText(/Recording since \d\d:\d\d · attach/)).toBeChecked();
    await userEvent.type(screen.getByTestId('chat-input'), 'typing lags');
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(upload).toHaveBeenCalledWith(
      'ws-1',
      expect.objectContaining({ name: expect.stringMatching(/^client-perf-.*\.json$/) })
    );
    expect(onSend).toHaveBeenCalledWith(
      'typing lags\n\nFile attachments:\n/ws/.schmux/attachments/u1/client-perf-1.json',
      []
    );
    expect(markSent).toHaveBeenCalled();
  });

  it('keeps the message and the recording when the upload fails', async () => {
    vi.spyOn(api, 'uploadWorkspaceAttachment').mockRejectedValue(new Error('upload failed'));
    const markSent = vi.spyOn(clientPerf, 'markSent');
    const onSend = vi.fn();
    render(
      <Composer
        workspaceId="ws-1"
        disabled={false}
        ended={false}
        onSend={onSend}
        recordingSince={1}
      />
    );
    await userEvent.type(screen.getByTestId('chat-input'), 'typing lags');
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(onSend).not.toHaveBeenCalled();
    expect(markSent).not.toHaveBeenCalled();
    expect(screen.getByTestId('chat-input')).toHaveValue('typing lags');
    expect(await screen.findByText('upload failed')).toBeInTheDocument();
  });

  it('keeps the uploaded path as a chip when send throws, and does not upload twice', async () => {
    const upload = vi
      .spyOn(api, 'uploadWorkspaceAttachment')
      .mockResolvedValue({ name: 'client-perf-1.json', path: '/p/client-perf-1.json' });
    const markSent = vi.spyOn(clientPerf, 'markSent');
    const onSend = vi.fn().mockImplementationOnce(() => {
      throw new Error('socket closed');
    });
    render(
      <Composer
        workspaceId="ws-1"
        disabled={false}
        ended={false}
        onSend={onSend}
        recordingSince={1}
      />
    );
    await userEvent.type(screen.getByTestId('chat-input'), 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(markSent).not.toHaveBeenCalled();
    expect(screen.getByText('client-perf-1.json')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(upload).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenLastCalledWith('x\n\nFile attachments:\n/p/client-perf-1.json', []);
    expect(markSent).toHaveBeenCalledTimes(1);
  });
});
```

Use the send button's real accessible name from `Composer.tsx` (grep `aria-label` or the button text near the end of the file).

- [ ] **Step 2: Run to verify failure**

Run: `./test.sh --quick`
Expected: new tests fail (no checkbox; `recordingSince` unknown prop).

- [ ] **Step 3: Implement in Composer**

Add to `ComposerProps`:

```ts
  /** When set, the performance recording checkbox is shown with this start time. */
  recordingSince?: number | null;
```

State: `const [attachRecording, setAttachRecording] = useState(true);` and `const [recordingPath, setRecordingPath] = useState<string | null>(null);`. Replace `submit` with:

```ts
const submit = async () => {
  if (disabled || attaching) return;
  let perfPath = recordingPath;
  const wantsRecording = recordingSince != null && attachRecording;
  if (wantsRecording && !perfPath && workspaceId) {
    setAttaching(true);
    setAttachmentError(null);
    try {
      const file = clientPerf.buildFile();
      const name = `client-perf-${file.builtAt}-${file.browserId}.json`;
      const uploaded = await uploadWorkspaceAttachment(
        workspaceId,
        new File([JSON.stringify(file)], name, { type: 'application/json' })
      );
      perfPath = uploaded.path;
      setRecordingPath(perfPath);
    } catch (err) {
      setAttachmentError(getErrorMessage(err, 'Failed to upload recording'));
      return;
    } finally {
      setAttaching(false);
    }
  }
  const paths = files.map((file) => file.path);
  if (perfPath) paths.push(perfPath);
  const text = withFileAttachments(value, paths);
  if (text.trim() === '' && images.length === 0) return;
  try {
    onSend(text, images);
  } catch (err) {
    setAttachmentError(getErrorMessage(err, 'Failed to send'));
    return;
  }
  if (perfPath) clientPerf.markSent();
  setRecordingPath(null);
  setValue('');
  clearAttachments();
  textareaRef.current?.focus();
};
```

Reuse the existing `attaching` and `attachmentError` state the file already has for file uploads (lines 55-70). Show the uploaded recording as a chip: pass `files={[...files.map(...), ...(recordingPath ? [{ name: recordingPath.split('/').pop() ?? 'recording', title: recordingPath }] : [])]}` to `AttachmentChips`, and in `onRemoveFile` handle the recording chip by `setRecordingPath(null)`. Render the checkbox above the composer row when `recordingSince != null`:

```tsx
{
  recordingSince != null && (
    <label className={`${styles.recordingToggle} flex-row gap-xs cursor-pointer`}>
      <input
        type="checkbox"
        checked={attachRecording}
        onChange={(e) => setAttachRecording(e.target.checked)}
      />
      {`Recording since ${new Date(recordingSince).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} · attach`}
    </label>
  );
}
```

Add `.recordingToggle { font-size: var(--font-size-xs); color: var(--color-text-secondary); padding: var(--space-xs) 0; }` to the Composer's CSS module.

- [ ] **Step 4: Thread the prop**

`ChatView.tsx`: add `recordingSince?: number | null;` to `ChatViewProps` and pass `recordingSince={recordingSince}` to both `<Composer>` renders (lines 162 and 199 region). `ChatSessionPage.tsx`: `const perf = useClientPerf();` and pass

```tsx
              recordingSince={perf.recording && perf.chat?.sessionId === sessionId ? perf.startedAt : null}
```

- [ ] **Step 5: Run tests**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 6: Commit**

`./format.sh`, then `/commit` with `feat(chat): attach the client performance recording from the composer`.

---

### Task 10: Documentation and the full gate

**Files:**

- Create: `docs/client-performance.md`
- Modify: `docs/telemetry.md:255` (new section before Chat Load Telemetry)
- Modify: `docs/dev-mode.md:116` (one line under Workspace switching)

- [ ] **Step 1: Write `docs/client-performance.md`**

Headers, each a section the agent reads in order: `# Client Performance Recording`; `## What it is` (two paragraphs: the goal from the spec and how a file reaches the agent); `## Reading a recording` (steps 1 to 6 from the spec's "How the agent works"); `## The file` with one subsection per top-level key: `build`, `environment`, `workload`, `timeline` (every `PerfRow` field, the stall rule, and the `stalls` index array), `longTasks`, `interactions`, `commits`, `websocket` (`perSecond` and `individual`, the 5ms and 50KB thresholds, the `:id` path normalization, the `binary` and `untyped` types), `fetches` (`:id` normalization), `terminals` (per mounted terminal per second, `handleOutputP50/P99` are global samples from `inputLatency`), `chatLoads`, `memory`, `navigation` (`route`, `visible`, `hidden`, `reload`; `firstCommitMs`, `paintMs`), `errors`; `## Caps and thresholds` (the table from the spec); `## Time` (every `t` is daemon time; `environment.clockOffsetMs`); `## The ids the browser keeps` (the POST request/response pair, that the daemon stores nothing, and what happens on dispose or end); `## Key files` (`lib/clientPerf.ts`, `lib/clientPerfObservers.ts`, `lib/clientPerfStore.ts`, `lib/transport.ts`, `components/ClientPerformance.tsx`, `components/chat/Composer.tsx`, `internal/dashboard/handlers_client_performance.go`, `internal/dashboard/client_performance_prompt.md`). Every field name must match the TypeScript interfaces in Task 3 exactly.

- [ ] **Step 2: `docs/telemetry.md`**

Before `## Chat Load Telemetry` (line 257) add `## Client Performance Recording` with the same subsections the IO section uses (lines 157-255: What it does, Key files, Architecture decisions, Data collected, Analysis workflow, Gotchas, Common modification patterns, Configuration). Analysis workflow: "Open the performance chat from the sidebar pane; the agent reads the attached file. See `docs/client-performance.md`." Gotchas: production React build leaves `commits` empty; `--dev-mode` without `--dev-proxy` serves the production bundle; two tabs record separately.

- [ ] **Step 3: `docs/dev-mode.md`**

Under `## Workspace switching` after the "From the dashboard" bullets (line 116): "Client performance recordings taken after switching to an agent's workspace carry that workspace in `build.sourceWorkspace`, which is how the agent compares a recording against its fix (`docs/client-performance.md`)."

- [ ] **Step 4: Full gate**

Run, from the repo root, and paste the output into the done report:

```
./format.sh
./test.sh
./badcode.sh
```

Expected: all pass. `./badcode.sh` includes knip: every new export must have a caller (`Ring` and `isStall` unexported unless imported; `useClientPerf` used by the pane and `ChatSessionPage`; `normalizeEndpoint` used by the test and the observer).

- [ ] **Step 5: Commit**

`/commit` with `docs: client performance recording`.

---

### Task 11: Scenario test

**Files:**

- Create: `test/scenarios/client-performance-recording.md`
- Create: `test/scenarios/generated/client-performance-recording.spec.ts`
- Modify: `test/scenarios/generated/helpers.ts:18-39` (add `extra?: Record<string, unknown>` to `SetupOptions`, spread into `config` before the POST)

- [ ] **Step 1: Write the scenario**

`test/scenarios/client-performance-recording.md`:

```markdown
# Record client performance and send it to the performance chat

A schmux developer in dev mode starts the client performance recorder from the
sidebar, opens the performance chat, and sends a message with the recording
attached. The daemon endpoints and the chat socket are controlled fixtures.

## Preconditions

- The dashboard is running with `client_performance` enabled, a repo named `schmux`, and a chat target.
- `/api/healthz` reports dev mode; `/api/dev/status`, `/api/client-performance/session`, the attachment upload, `/ws/dashboard`, and `/ws/chat/perf-session` are Playwright routes.

## Verifications

- The sidebar shows a Client Performance pane with Start recording.
- After Start recording the header reads `Client Performance · REC` and the status line shows minutes and stalls.
- Open performance chat posts empty ids, receives `perf-workspace`/`perf-session`, and navigates to `/sessions/perf-session`.
- The composer shows a checked `Recording since HH:MM · attach` checkbox.
- Sending uploads one JSON attachment to `perf-workspace` whose body has `build`, `environment`, and `timeline` keys and `build.sourceWorkspace` equal to the dev status source workspace.
- The sent chat message text ends with a `File attachments:` block holding the path the upload route returned.
```

- [ ] **Step 2: Write the spec**

Follow `chat-file-attachments.spec.ts:1-100` for the fixture, `seedConfig`, `routeWebSocket` for `/ws/dashboard` and `/ws/chat/perf-session`, and the upload route. Differences:

```ts
await seedConfig({
  repos: ['https://example.com/schmux.git'],
  agents: [{ name: 'claude', command: 'claude', promptable: true }],
  extra: {
    chat_sessions: true,
    client_performance: { enabled: true, repo: 'schmux.git', target: 'claude' },
  },
});
await page.route('**/api/healthz', (route) =>
  route.fulfill({
    json: { version: 'test', dev_mode: true },
    headers: { Date: new Date().toUTCString() },
  })
);
await page.route('**/api/dev/status', (route) =>
  route.fulfill({
    json: {
      active: true,
      source_workspace: '/tmp/perf-workspace',
      schmux_workspaces: ['perf-workspace'],
    },
  })
);
let ensureBody: unknown = null;
await page.route('**/api/client-performance/session', async (route) => {
  ensureBody = route.request().postDataJSON();
  await route.fulfill({ json: { workspace_id: 'perf-workspace', session_id: 'perf-session' } });
});
let uploadedBody: Record<string, unknown> | null = null;
await page.route('**/api/workspaces/perf-workspace/attachments?*', async (route) => {
  const name = new URL(route.request().url()).searchParams.get('filename') ?? '';
  expect(name).toMatch(/^client-perf-\d+-[a-z0-9]+\.json$/);
  uploadedBody = JSON.parse(route.request().postData() ?? '{}');
  await route.fulfill({
    json: { name, path: `/tmp/perf-workspace/.schmux/attachments/u1/${name}` },
    status: 201,
  });
});
```

The `/ws/dashboard` route sends one workspace `perf-workspace` (`repo: 'https://example.com/schmux.git'`, `branch: 'client-performance'`, `path: '/tmp/perf-workspace'`) with one session `perf-session` (`kind: 'chat'`, `running: true`). The chat socket route echoes `send` messages as `user_message` records into `sentRecords` as the attachments spec does.

Steps and assertions:

```ts
await page.goto('/');
await page.getByRole('button', { name: 'Start recording' }).click();
await expect(page.getByText('Client Performance · REC')).toBeVisible();
await expect(page.getByTestId('client-perf-pane')).toContainText(/Recording \d+ min · \d+ stalls/);
await page.getByRole('button', { name: 'Open performance chat' }).click();
await expect(page).toHaveURL(/\/sessions\/perf-session$/);
expect(ensureBody).toEqual({ workspace_id: '', session_id: '' });
await expect(page.getByLabel(/Recording since \d\d:\d\d · attach/)).toBeChecked();
await page.getByTestId('chat-input').fill('typing lags in the terminal');
await page.getByRole('button', { name: 'Send' }).click();
await expect.poll(() => sentRecords.length).toBe(1);
expect(uploadedBody).toEqual(
  expect.objectContaining({
    build: expect.objectContaining({ sourceWorkspace: '/tmp/perf-workspace' }),
    environment: expect.any(Object),
    timeline: expect.any(Array),
  })
);
const text = String(sentRecords[0].text);
expect(
  text.endsWith(
    `File attachments:\n/tmp/perf-workspace/.schmux/attachments/u1/${new URL(String(uploadRequestUrl)).searchParams.get('filename')}`
  )
).toBe(true);
```

Capture the upload request URL in the route handler for the final assertion. `expect.poll` on `sentRecords.length` is the one eventual-state wait (rule 5); the equality assertions after it run once (rule 7).

- [ ] **Step 3: Run the scenario**

Run: `./test.sh --scenarios`
Expected: the new scenario passes in both themes if you loop over themes as the attachments spec does, or once otherwise. If the daemon in the scenario container does not run with `--dev-mode`, `isDevMode` comes from the routed `/api/healthz`, which the test controls.

- [ ] **Step 4: Commit**

`./format.sh`, then `/commit` with `test(scenarios): client performance recording`.

---

## Self-review

**Spec coverage.** Goal and needs: Tasks 3-6 (need 1), 7-9 (need 2), 7 (need 3), 6 and 10 (need 4). The recording section: Task 3 types, Task 4-6 sources, Task 10 doc. The conversation and prompt: Task 7. Proving the fix: Task 6 `setBuild` from `source_workspace`, Task 7 prompt, Task 10 dev-mode doc. The recorder: Tasks 3-5. What the user does: Tasks 8-9. Configuration: Tasks 1-2. Documentation: Tasks 1, 2, 7, 8, 10. Failure cases: unsupported observer (Task 4 test), production build (`viteDev`, Task 6), endpoint errors in pane (Task 8 test), failed upload or send (Task 9 tests), disposed workspace or ended session (Task 7 tests), config disabled while recording (Task 3 test). Tests: Go cases all in Task 7; Vitest cases across Tasks 3-9; scenario Task 11.

**Type consistency.** `ClientPerfFile` keys in Task 3 match the scenario assertions (`build`, `environment`, `timeline`) and the doc in Task 10. `ChatIds` is `{workspaceId, sessionId}` in the browser; the wire shape is `{workspace_id, session_id}` and the pane maps between them in Task 8. `recordingSince` is the prop name in Composer, ChatView, and ChatSessionPage. `socketCount`, `terminalCount`, `lastDashboardMessageBytes`, `socketsByPath` are defined in Task 5 and read in Tasks 4 and 6.

**Review Focus.** Item 1 pinned by Task 4 test "notes unsupported observers". Item 2 by Task 5 test (ArrayBuffer frame → `binary`, 16 bytes). Item 3 by Task 3 test "drops the oldest timeline rows past the cap but keeps counting stalls". Item 4 by Task 3 test "keeps two tabs apart". Item 5 by Task 9 test "keeps the uploaded path as a chip when send throws".
