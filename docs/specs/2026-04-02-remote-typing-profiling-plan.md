# Plan: Remote Typing Performance Profiling

**Goal**: Instrument the remote typing path to break down `sendKeys` latency into mutex contention, execute round-trips, and classify overhead — surfaced in the existing TypingPerformance dashboard widget.
**Architecture**: `Execute()` returns mutex wait as stack-local value (no shared state). `SendKeysTimings` flows through `ControlSource` interface → `SessionTracker` → WebSocket handler → frontend. Health probes enabled for remote sessions with jitter.
**Tech Stack**: Go 1.22, TypeScript, React, Vitest
**Design**: `docs/specs/2026-04-02-remote-typing-profiling.md`

---

## Task Dependencies

| Group | Steps       | Can Parallelize         | Verify                                               |
| ----- | ----------- | ----------------------- | ---------------------------------------------------- |
| 1     | Step 1      | Single step             | `go build ./internal/remote/controlmode/...`         |
| 2     | Steps 2–5   | No (cascade)            | `go build ./internal/remote/...`                     |
| 3     | Steps 6–9   | No (cascade)            | `go build ./...`                                     |
| 4     | Steps 10–11 | Yes (independent files) | `go test ./internal/dashboard/ -run Latency`         |
| 5     | Steps 12–13 | No (12 then 13)         | `go build ./... && go test ./internal/dashboard/...` |
| 6     | Steps 14–16 | No (cascade)            | `./test.sh --quick`                                  |
| 7     | Steps 17–18 | Yes (independent)       | `./test.sh --quick`                                  |
| 8     | Step 19     | Single step             | `./test.sh`                                          |

---

## Step 1: Add `SendKeysTimings` type

**File**: `internal/remote/controlmode/keyclassify.go`

Add at end of file:

```go
// SendKeysTimings records per-keystroke timing breakdown from Client.SendKeys.
// MutexWait and ExecuteNet are non-overlapping and partition the SendKeys duration.
type SendKeysTimings struct {
	MutexWait    time.Duration // time blocked on stdinMu across all Execute() calls
	ExecuteNet   time.Duration // sum of Execute() round-trips, EXCLUDING mutex wait
	ExecuteCount int           // number of Execute() calls (= number of key runs)
}
```

Add `"time"` to imports.

### Verify

```bash
go build ./internal/remote/controlmode/...
```

---

## Step 2: Instrument `Execute` to return mutex wait

**File**: `internal/remote/controlmode/client.go`

### 2a. Change `Execute` signature

At line 151, change:

```go
func (c *Client) Execute(ctx context.Context, cmd string) (string, error) {
```

to:

```go
func (c *Client) Execute(ctx context.Context, cmd string) (string, time.Duration, error) {
```

### 2b. Add mutex timing inside `Execute`

At line 180 (the `stdinMu.Lock()` call), wrap with timing:

```go
mutexStart := time.Now()
c.stdinMu.Lock()
mutexWait := time.Since(mutexStart)
```

### 2c. Update all return paths in `Execute`

- Error after stdin write (line ~186): `return "", mutexWait, fmt.Errorf(...)`
- Success response (line ~198): `return resp.Content, mutexWait, nil`
- Error response (line ~196): `return "", mutexWait, fmt.Errorf(...)`
- Context timeout (line ~207): `return "", mutexWait, ctx.Err()`
- Client closed (line ~210): `return "", mutexWait, fmt.Errorf("client closed")`

### 2d. Update all 19 `Execute` callers within `client.go`

Each call site adds `_` for the new `time.Duration` return. Pattern:

- `_, err := c.Execute(...)` → `_, _, err := c.Execute(...)`
- `output, err := c.Execute(...)` → `output, _, err := c.Execute(...)`
- `if _, err := c.Execute(...)` → `if _, _, err := c.Execute(...)`

**Special cases (lines 488, 495)**: `CapturePaneVisible` and `CapturePaneLines` currently `return c.Execute(ctx, cmd)`. Change to:

```go
output, _, err := c.Execute(ctx, cmd)
return output, err
```

**Call sites** (line numbers): 344, 350, 376, 392, 409, 418, 424, 449, 472, 478, 488, 495, 507, 543, 566, 607, 654, 659, 678.

### 2e. Update `SendKeys` to accumulate timings

At line 401, change from returning `error` to `(SendKeysTimings, error)`:

```go
func (c *Client) SendKeys(ctx context.Context, paneID, keys string) (SendKeysTimings, error) {
	var timings SendKeysTimings
	runs := ClassifyKeyRuns(nil, keys)
	timings.ExecuteCount = len(runs)
	for _, run := range runs {
		var cmd string
		if run.Literal {
			cmd = fmt.Sprintf("send-keys -t %s -l %s", paneID, shellutil.Quote(run.Text))
		} else {
			cmd = fmt.Sprintf("send-keys -t %s %s", paneID, run.Text)
		}
		execStart := time.Now()
		_, mutexWait, err := c.Execute(ctx, cmd)
		if err != nil {
			return timings, err
		}
		execDur := time.Since(execStart)
		timings.MutexWait += mutexWait
		timings.ExecuteNet += max(0, execDur-mutexWait)
	}
	return timings, nil
}
```

### 2f. Update `SendEnter` (line 417)

`SendEnter` calls `Execute` and returns error. Update:

```go
func (c *Client) SendEnter(ctx context.Context, paneID string) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("send-keys -t %s Enter", paneID))
	return err
}
```

**Do not verify yet** — `connection.go` and `localsource.go` call `Execute` and will break.

---

## Step 3: Update `Execute` callers in tests

**File**: `internal/remote/controlmode/client_test.go`

Update all `Execute` calls (lines 208, 280, 441, 664, 745, 753) from:

- `_, err := client.Execute(...)` → `_, _, err := client.Execute(...)`
- `result, err := client.Execute(...)` → `result, _, err := client.Execute(...)`

**File**: `internal/remote/controlmode/protocol_bench_test.go`

Line 145: `client.Execute(ctx, "list-sessions")` → `_, _, _ = client.Execute(ctx, "list-sessions")`

**Do not verify yet** — `connection.go` callers still break.

---

## Step 4: Update `connection.go`

**File**: `internal/remote/connection.go`

### 4a. Update `SendKeys` return type (line 919)

```go
func (c *Connection) SendKeys(ctx context.Context, paneID, keys string) (controlmode.SendKeysTimings, error) {
	if !c.IsConnected() {
		return controlmode.SendKeysTimings{}, fmt.Errorf("not connected")
	}
	return c.client.SendKeys(ctx, paneID, keys)
}
```

### 4b. Update direct `client.Execute` callers

Line 663: `if resp, err := c.client.Execute(...)` → `if resp, _, err := c.client.Execute(...)`
Line 686: `if _, err := c.client.Execute(...)` → `if _, _, err := c.client.Execute(...)`

### 4c. Add `ExecuteHealthProbe` method

```go
// ExecuteHealthProbe runs a lightweight no-op command for RTT measurement.
func (c *Connection) ExecuteHealthProbe(ctx context.Context) (string, time.Duration, error) {
	if !c.IsConnected() {
		return "", 0, fmt.Errorf("not connected")
	}
	return c.client.Execute(ctx, "display-message -p ok")
}
```

### Verify

```bash
go build ./internal/remote/...
```

---

## Step 5: Update `localsource.go` `Execute` callers

**File**: `internal/session/localsource.go`

### 5a. Update `SendKeys` return type (line 72)

```go
func (s *LocalSource) SendKeys(keys string) (controlmode.SendKeysTimings, error) {
	s.mu.RLock()
	client := s.cmClient
	paneID := s.paneID
	s.mu.RUnlock()
	if client == nil {
		return controlmode.SendKeysTimings{}, fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.SendKeys(ctx, paneID, keys)
}
```

### 5b. Update direct `Execute` callers

Line 226: `output, err := client.Execute(...)` → `output, _, err := client.Execute(...)`
Line 292: `_, err := client.Execute(...)` → `_, _, err := client.Execute(...)`
Line 400: `output, err := client.Execute(...)` → `output, _, err := client.Execute(...)`

**Do not verify yet** — `ControlSource` interface mismatch.

---

## Step 6: Update `ControlSource` interface + mock + `RemoteSource.SendKeys`

**File**: `internal/session/controlsource.go`

Change line 34:

```go
SendKeys(keys string) (controlmode.SendKeysTimings, error)
```

Add import for `controlmode` if not present:

```go
"github.com/sergeknystautas/schmux/internal/remote/controlmode"
```

**File**: `internal/session/controlsource_test.go`

Update `MockControlSource.SendKeys` (line 21):

```go
func (m *MockControlSource) SendKeys(keys string) (controlmode.SendKeysTimings, error) {
	return controlmode.SendKeysTimings{}, nil
}
```

Add `controlmode` import.

**File**: `internal/session/remotesource.go`

Update `SendKeys` (line 35):

```go
func (s *RemoteSource) SendKeys(keys string) (controlmode.SendKeysTimings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.conn.SendKeys(ctx, s.paneID, keys)
}
```

**Do not verify yet** — `SessionTracker.SendInput` mismatch.

---

## Step 7: Update `SessionTracker.SendInput` + probe extraction

**File**: `internal/session/tracker.go`

### 7a. Update `SendInput` (line 253)

```go
func (t *SessionTracker) SendInput(data string) (controlmode.SendKeysTimings, error) {
	return t.source.SendKeys(data)
}
```

Add `controlmode` import if not present.

### 7b. Update probe extraction in `NewSessionTracker` (line 131)

Replace:

```go
var healthProbe *TmuxHealthProbe
if ls, ok := source.(*LocalSource); ok {
    healthProbe = ls.HealthProbe
} else {
    healthProbe = NewTmuxHealthProbe()
}
```

With:

```go
var healthProbe *TmuxHealthProbe
switch s := source.(type) {
case *LocalSource:
    healthProbe = s.HealthProbe
case *RemoteSource:
    healthProbe = s.healthProbe
default:
    healthProbe = NewTmuxHealthProbe()
}
```

**Do not verify yet** — `websocket.go` callers of `SendInput` mismatch.

---

## Step 8: Add health probe to `RemoteSource`

**File**: `internal/session/remotesource.go`

### 8a. Add `healthProbe` field and update constructor

```go
type RemoteSource struct {
	conn        *remote.Connection
	paneID      string
	events      chan SourceEvent
	stopCh      chan struct{}
	doneCh      chan struct{}
	healthProbe *TmuxHealthProbe
}

func NewRemoteSource(conn *remote.Connection, paneID string) *RemoteSource {
	return &RemoteSource{
		conn:        conn,
		paneID:      paneID,
		events:      make(chan SourceEvent, 1000),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		healthProbe: NewTmuxHealthProbe(),
	}
}
```

### 8b. Add health probe goroutine to `run()`

Replace the existing `run()` method with the version from the design spec — subscribe first, then launch probe goroutine with internal jitter, then output event loop.

Add imports: `"math/rand"`, `"time"`.

---

## Step 9: Update callers outside session package

**File**: `internal/dashboard/handlers_tell.go`

Line 51: `if err := conn.SendKeys(...)` → `if _, err := conn.SendKeys(...)`

**File**: `internal/dashboard/clipboard.go`

Line 202: `if err := conn.SendKeys(...)` → `if _, err := conn.SendKeys(...)`
Line 214: `if err := conn.SendKeys(...)` → `if _, err := conn.SendKeys(...)`

**File**: `internal/session/manager.go`

Line 278: find the `conn.SendKeys(...)` call and update to `_, err := conn.SendKeys(...)` (or `_, _ = conn.SendKeys(...)` if error is currently ignored).

**File**: `internal/dashboard/websocket.go`

Line 597: Update `tracker.SendInput` caller:

```go
err := tracker.SendInput(batch.data)
```

→

```go
timings, err := tracker.SendInput(batch.data)
```

And propagate `timings` fields into `inputResult`.

Also line 1119 (FM handler): `tracker.SendInput(msg.Data)` → `_, _ = tracker.SendInput(msg.Data)`
Line 284 (pre-bootstrap): `tracker.SendInput(msg.Data)` → `_, _ = tracker.SendInput(msg.Data)`

### Verify

```bash
go build ./...
go test ./internal/remote/controlmode/... ./internal/session/... ./internal/dashboard/...
```

---

## Step 10: Extend `LatencySample` and `LatencyPercentiles`

**File**: `internal/dashboard/latency_collector.go`

### 10a. Add fields to `LatencySample` (line 13)

```go
MutexWait    time.Duration
ExecuteNet   time.Duration
ExecuteCount int
```

### 10b. Add fields to `LatencyPercentiles` (line 27)

```go
MutexWaitP50    float64 `json:"mutexWaitP50"`
MutexWaitP99    float64 `json:"mutexWaitP99"`
ExecuteNetP50   float64 `json:"executeNetP50"`
ExecuteNetP99   float64 `json:"executeNetP99"`
ExecuteCountP50 float64 `json:"executeCountP50"`
ExecuteCountP99 float64 `json:"executeCountP99"`
```

### 10c. Update `Percentiles()` method

Add 3 new slices (`mutexWait`, `executeNet`, `executeCount`), populate in the loop, add percentile computations to the return struct.

---

## Step 11: Extend `latency_collector_test.go`

**File**: `internal/dashboard/latency_collector_test.go`

### 11a. Update `TestLatencyCollector_SingleSample`

Add `MutexWait`, `ExecuteNet`, `ExecuteCount` to the sample, assert the new percentile fields are non-zero.

### 11b. Update `TestLatencyCollector_KnownDistribution`

Add new fields to all samples in the distribution, verify P50/P99 computation.

### Verify

```bash
go test ./internal/dashboard/ -run Latency -v
```

---

## Step 12: WebSocket handler — `inputResult` and `inputEcho`

**File**: `internal/dashboard/websocket.go`

### 12a. Add fields to `inputResult` struct (~line 580)

```go
mutexWait    time.Duration
executeNet   time.Duration
executeCount int
```

### 12b. Update async input sender goroutine (~line 593)

Capture timings from `tracker.SendInput` and populate the new `inputResult` fields.

### 12c. Update `LatencySample` construction (~line 654)

Add `MutexWait`, `ExecuteNet`, `ExecuteCount` from `pending`.

### 12d. Update `inputEcho` sideband JSON (~line 666)

Add `"mutexWaitMs"`, `"executeNetMs"`, `"executeCount"` to the JSON map.

---

## Step 13: Add `sessionType` to stats and inputEcho

**File**: `internal/dashboard/websocket.go`

### 13a. Add field to `WSStatsMessage` (~line 111)

```go
SessionType string `json:"sessionType"` // "local" or "remote"
```

### 13b. Set `sessionType` in the WebSocket handler

Near the top of `handleTerminalWebSocket`, after `sess` is available (~line 208), determine session type:

```go
sessionType := "local"
if sess.IsRemoteSession() {
    sessionType = "remote"
}
```

### 13c. Add to stats ticker (~line 692)

Set `SessionType: sessionType` on `WSStatsMessage`.

### 13d. Add to `inputEcho` sideband (~line 666)

Add `"sessionType": sessionType` to the JSON map.

### Verify

```bash
go build ./... && go test ./internal/dashboard/...
```

---

## Step 14: Update `websocket_test.go`

**File**: `internal/dashboard/websocket_test.go`

### 14a. Update `TestInputEchoSidebandFormat` (~line 890)

Add `"mutexWaitMs"`, `"executeNetMs"`, `"executeCount"`, `"sessionType"` to the test JSON and verify they serialize correctly.

### 14b. Add `TestLatencyPercentiles_NewFields`

Construct `LatencyCollector`, add samples with non-zero `MutexWait`/`ExecuteNet`/`ExecuteCount`, call `Percentiles()`, assert new fields are populated.

### Verify

```bash
go test ./internal/dashboard/ -v -run "InputEcho|Latency"
```

---

## Step 15: Frontend types — `inputLatency.ts`

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 15a. Add fields to `ServerLatencySegments` (line 28)

```typescript
mutexWaitP50?: number;
mutexWaitP99?: number;
executeNetP50?: number;
executeNetP99?: number;
executeCountP50?: number;
executeCountP99?: number;
```

### 15b. Add fields to `ServerSegmentTuple` (line 46)

```typescript
mutexWait?: number;
executeNet?: number;
executeCount?: number;
sessionType?: 'local' | 'remote';
```

### 15c. Add fields to `LatencyBreakdown` (line 55)

```typescript
mutexWait?: number;
executeNet?: number;
```

### 15d. Update `getBreakdown()` (~line 251)

In the tuple construction loop, propagate `mutexWait` and `executeNet` from `seg`:

```typescript
mutexWait: seg.mutexWait,
executeNet: seg.executeNet,
```

In the `picked` return, include the new fields.

---

## Step 16: Frontend plumbing — `terminalStream.ts`

**File**: `assets/dashboard/src/lib/terminalStream.ts`

### 16a. Update `inputEcho` handler (~line 1302)

Add new fields to `recordServerSegments` call:

```typescript
case 'inputEcho':
  inputLatency.recordServerSegments({
    dispatch: (msg.dispatchMs as number) ?? 0,
    sendKeys: (msg.sendKeysMs as number) ?? 0,
    echo: (msg.echoMs as number) ?? 0,
    frameSend: (msg.frameSendMs as number) ?? 0,
    total: (msg.serverMs as number) ?? 0,
    mutexWait: (msg.mutexWaitMs as number) ?? undefined,
    executeNet: (msg.executeNetMs as number) ?? undefined,
    executeCount: (msg.executeCount as number) ?? undefined,
    sessionType: (msg.sessionType as string) ?? undefined,
  });
```

### Verify

```bash
./test.sh --quick
```

---

## Step 17: Frontend UI — `TypingPerformance.tsx`

**File**: `assets/dashboard/src/components/TypingPerformance.tsx`

### 17a. Add sub-segment colors and labels (~line 203)

```typescript
const SEGMENT_COLORS: Record<string, string> = {
  // ... existing ...
  mutexWait: 'rgba(220, 80, 80, 0.7)',
  executeNet: 'rgba(80, 130, 200, 0.7)',
};

const SEGMENT_LABELS: Record<string, string> = {
  // ... existing ...
  mutexWait: 'mutex',
  executeNet: 'execNet',
};
```

### 17b. Update `SEGMENTS` array (~line 223)

Add `'mutexWait'` and `'executeNet'` after `'sendKeys'`.

### 17c. Conditional sub-segment rendering in `BreakdownRow`

When `breakdown.mutexWait` and `breakdown.executeNet` are defined, hide the `sendKeys` segment and show `mutexWait` + `executeNet` instead. When undefined, render `sendKeys` as before.

### 17d. Session type badge

In the `TypingPerformance` component header, show session type when available from the latest `inputEcho` data.

### Verify

```bash
./test.sh --quick
```

---

## Step 18: Update API docs

**File**: `docs/api.md`

Update the WebSocket message documentation:

- `inputEcho` message: add `mutexWaitMs`, `executeNetMs`, `executeCount`, `sessionType` fields
- `stats` message: add `sessionType` field, add new `inputLatency` percentile fields
- Health probe section: note that remote sessions now have active health probes

---

## Step 19: End-to-end verification

### 19a. Run full test suite

```bash
./test.sh
```

### 19b. Verify race detector

```bash
go test -race ./internal/remote/controlmode/... ./internal/session/... ./internal/dashboard/...
```

### 19c. Build dashboard

```bash
go run ./cmd/build-dashboard
```

### 19d. Build binary

```bash
go build ./cmd/schmux
```
