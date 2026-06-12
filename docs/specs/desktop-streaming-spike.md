# Desktop Streaming Spike

## Status

Spec / spike design. This is not implemented yet.

## Goal

Prove that schmux can show a native application surface inside a workspace dashboard tab with real-time video, audio, and input routing.

This first spike is deliberately scoped to **manual capture of an existing local app/window/display**. It does not solve workspace-aware app launch semantics yet. After the stream/control path works, follow-up work can bind a specific launched GUI process to a specific worktree and workspace tab.

## Non-goals for the spike

- Do not implement full workspace-aware native app launch semantics.
- Do not require app/window selection to be inferred from the worktree.
- Do not implement Windows support yet.
- Do not build a generic remote desktop product.
- Do not support remote-host workspaces in the first spike.
- Do not persist long-lived desktop sessions across daemon restarts unless that falls out naturally from state recovery.

## User story

As a schmux user, I can open a workspace and create a "Desktop" tab that streams a selected native app/window/display from my computer. I can hear audio from the captured source and interact with it using mouse and keyboard events from the browser tab.

## Architectural fit

This feature should attach to the existing workspace accessory tab model. The important analogy to existing web previews is the UI/tab placement: the output of a workspace-related thing appears as a tab within that workspace.

The desktop stream is not a web preview and should not be implemented as a preview subtype. It needs its own lifecycle, state, signaling, permission checks, capture provider, and input path.

Proposed new package:

```text
internal/desktop/
  manager.go
  provider.go
  macos.go
  signaling.go
```

Proposed first helper binary:

```text
schmux-desktop-macos
```

The daemon owns state, authorization, workspace/tab integration, and helper lifecycle. The helper owns macOS capture, audio capture, and native input injection.

## High-level data flow

```text
Dashboard Desktop tab
  <video/audio element + input event capture>
        |
        | WebRTC media + data channel
        |
Schmux daemon
  <signaling websocket + desktop manager>
        |
        | local helper lifecycle/control
        |
schmux-desktop-macos
  <ScreenCaptureKit + native audio + input injection>
        |
        | macOS capture/control APIs
        |
Selected app/window/display
```

The daemon should not be in the hot path for media frames. It should coordinate session creation and signaling, then allow the browser and helper to exchange media/control over WebRTC. If the initial implementation cannot complete direct WebRTC cleanly, a temporary localhost relay is acceptable for the spike, but the desired architecture is WebRTC media tracks plus a data channel.

## Transport model

Use WebRTC for:

- Video track: captured display/window/app surface.
- Audio track: captured source/system/app audio where available.
- Data channel: browser input events routed to the helper.

Use a schmux websocket only for signaling/control:

```text
/ws/desktops/{desktopId}/signal
```

Signaling messages should be JSON envelopes:

```json
{
  "type": "offer|answer|ice|status|error",
  "desktop_id": "desk_abc123",
  "payload": {}
}
```

## Desktop state

Add desktop records to persisted state:

```go
type WorkspaceDesktop struct {
    ID          string            `json:"id"`
    WorkspaceID string            `json:"workspace_id"`
    Provider    string            `json:"provider"`      // "macos" for the spike
    TargetKind  string            `json:"target_kind"`   // "display" | "window" | "app"
    TargetID    string            `json:"target_id"`
    Label       string            `json:"label"`
    Status      string            `json:"status"`        // starting | permission_required | ready | stopped | failed
    HasVideo    bool              `json:"has_video"`
    HasAudio    bool              `json:"has_audio"`
    Interactive bool              `json:"interactive"`
    LastError   string            `json:"last_error,omitempty"`
    Meta        map[string]string `json:"meta,omitempty"`
    CreatedAt   time.Time         `json:"created_at"`
    LastUsedAt  time.Time         `json:"last_used_at"`
}
```

Add to `state.State`:

```go
Desktops map[string]WorkspaceDesktop `json:"desktops,omitempty"`
```

Open a workspace accessory tab for each desktop:

```go
Tab{
    ID:          "sys-desktop-" + desktop.ID,
    WorkspaceID: workspaceID,
    Kind:        "desktop",
    Label:       desktop.Label,
    Route:       "/desktop/" + workspaceID + "/" + desktop.ID,
    Closable:    true,
    Meta: map[string]string{
        "desktop_id": desktop.ID,
    },
}
```

## HTTP API

### Create desktop stream

```text
POST /api/workspaces/{workspaceId}/desktops
```

Request:

```json
{
  "provider": "macos",
  "target_kind": "window",
  "target_id": "optional-provider-specific-id",
  "label": "Desktop",
  "audio": true,
  "interactive": true
}
```

For the spike, `target_id` may be omitted and the helper/dashboard can present a manual source picker.

Response:

```json
{
  "id": "desk_abc123",
  "workspace_id": "ws_abc123",
  "provider": "macos",
  "target_kind": "window",
  "target_id": "...",
  "label": "Desktop",
  "status": "starting",
  "has_video": true,
  "has_audio": true,
  "interactive": true,
  "route": "/desktop/ws_abc123/desk_abc123"
}
```

### List desktop streams for a workspace

```text
GET /api/workspaces/{workspaceId}/desktops
```

### Stop desktop stream

```text
DELETE /api/workspaces/{workspaceId}/desktops/{desktopId}
```

Stopping a desktop stream should stop helper-side capture, close peer connections, remove the state record, and close the corresponding workspace tab.

### Provider capabilities

```text
GET /api/desktop/capabilities
```

Response:

```json
{
  "providers": [
    {
      "name": "macos",
      "available": true,
      "video": true,
      "audio": true,
      "input": true,
      "requires_permissions": ["screen_recording", "accessibility"]
    }
  ]
}
```

### Permission diagnostics

```text
GET /api/desktop/permissions
```

Response:

```json
{
  "provider": "macos",
  "screen_recording": "granted|denied|unknown",
  "accessibility": "granted|denied|unknown",
  "audio": "granted|denied|unknown",
  "restart_required": true
}
```

## Dashboard UI

Add a desktop route:

```text
/desktop/:workspaceId/:desktopId
```

The route renders:

- Connection state.
- Permission warnings and remediation hints.
- A video element for the WebRTC video/audio stream.
- A control overlay that captures mouse, wheel, and keyboard events.
- A focus affordance so users know when keyboard input is being sent to the native app.
- A stop/disconnect action.

Initial creation can be exposed as a workspace action named "Open Desktop Stream". It can create the tab, then let the user choose a source if needed.

## Input routing

Browser input events are sent over the WebRTC data channel. The daemon is not on the hot path once the peer connection is established.

Example pointer message:

```json
{
  "type": "pointer",
  "phase": "down|move|up|wheel",
  "x": 412,
  "y": 280,
  "button": "left",
  "delta_x": 0,
  "delta_y": 0,
  "modifiers": ["meta", "shift"]
}
```

Example keyboard message:

```json
{
  "type": "key",
  "phase": "down|up",
  "code": "KeyS",
  "key": "s",
  "modifiers": ["meta"]
}
```

The helper maps browser coordinates to the captured source coordinates and injects native events.

Input issues the spike must explicitly test:

- Retina scaling.
- Browser CSS scaling vs captured pixel coordinates.
- Window movement and resize while streaming.
- Modifier keys.
- Scroll wheel direction.
- Keyboard layout differences.
- Apps that refuse synthetic input.
- Secure input fields.

## macOS provider

The macOS provider should be implemented as a helper binary because the required APIs are native macOS APIs and are easier to build, permission, and sign from Swift/Objective-C than from pure Go.

Proposed helper commands:

```text
schmux-desktop-macos capabilities
schmux-desktop-macos permissions
schmux-desktop-macos list-sources
schmux-desktop-macos start --desktop-id desk_abc123 --target-kind window --target-id ...
schmux-desktop-macos stop --desktop-id desk_abc123
```

The helper can communicate with the daemon via stdio, a localhost websocket, or a local unix socket. For the spike, choose the simplest implementation that keeps media frames out of the daemon process.

### Capture

Use ScreenCaptureKit for display/window capture on macOS. Prefer window capture for the spike, with display capture as a fallback.

### Audio

The desired model is a WebRTC audio track paired with the captured source. For the spike, try provider-native source/system audio first. If app/window-specific audio is not reliable enough, document the limitation and keep the audio track plumbing intact so a later virtual-device or system-audio implementation can drop in without changing the dashboard or daemon API.

### Input

Use native macOS event injection for pointer and keyboard input. The provider must expose permission state clearly because input routing depends on Accessibility/Input Monitoring-style permissions.

### Permissions

The dashboard should surface permission state before and after a stream starts. The helper should be able to report:

- Screen Recording: required for video capture.
- Accessibility/Input Monitoring: required for interactive control.
- Audio-related permission or capability state: required for audio capture, depending on implementation path.

Permission failures should leave the desktop record in `permission_required` or `failed` with a clear `LastError`.

## Provider abstraction

Define the platform boundary so Windows can be added later without changing the daemon API or dashboard route.

```go
type Provider interface {
    Name() string
    Capabilities(ctx context.Context) (Capabilities, error)
    Permissions(ctx context.Context) (Permissions, error)
    ListSources(ctx context.Context) ([]Source, error)
    Start(ctx context.Context, spec StartSpec) (*Runtime, error)
    Stop(ctx context.Context, desktopID string) error
}
```

`StartSpec` should include:

```go
type StartSpec struct {
    DesktopID   string
    WorkspaceID string
    TargetKind  string
    TargetID    string
    Audio       bool
    Interactive bool
}
```

The first provider is `macos`; later providers may include `windows`.

## Windows follow-up shape

The Windows implementation should plug into the same `Provider` interface and use Windows-native capture/control APIs behind the helper boundary. The dashboard, daemon state, desktop API, signaling websocket, and workspace tab model should not need structural changes.

The spike should avoid macOS-specific fields in persisted state except under provider-specific `Meta`.

## Lifecycle

Create:

1. User opens a desktop stream from a workspace.
2. Daemon creates a `WorkspaceDesktop` record.
3. Daemon opens a `desktop` tab for the workspace.
4. Daemon starts or contacts the provider helper.
5. Dashboard route connects to the signaling websocket.
6. Browser and helper establish WebRTC tracks/data channel.
7. Desktop status becomes `ready`.

Stop:

1. User closes tab or stops desktop.
2. Daemon closes signaling/control state.
3. Provider stops capture and input handling.
4. Peer connection is closed.
5. Desktop state record is removed or marked stopped.
6. Workspace tab is closed.

Workspace dispose:

- Stop all desktops for that workspace.
- Close all desktop tabs for that workspace.
- Do not leave helper capture processes running.

Daemon shutdown:

- Attempt graceful stop of all provider runtimes.
- If helper processes are daemon-owned, kill them on shutdown if they do not exit quickly.

## Security and safety

This feature captures and controls the user's local desktop. Treat it as a high-risk local capability.

Requirements:

- Keep the first implementation local-only.
- Respect existing auth requirements for `/api/*` and `/ws/*` when enabled.
- Do not expose streams to remote clients unless a separate explicit security design is written.
- Do not stream before permissions and source selection are explicit.
- Show persistent UI state when the browser tab is sending input.
- Avoid logging raw input events except under explicit debug mode.
- Avoid storing captured media.

## Error handling

Important statuses:

```text
starting
permission_required
ready
stopped
failed
```

Common errors:

- Provider unavailable.
- Screen Recording permission missing.
- Accessibility/Input permission missing.
- Audio capture unavailable.
- Source no longer exists.
- Peer connection failed.
- Helper exited unexpectedly.

Errors should be visible in the dashboard tab and in daemon logs.

## Open questions

1. Should the helper be bundled with the schmux binary distribution or built separately for macOS?
2. Should source selection be done in the dashboard, in a native picker, or both?
3. Is view-only mode acceptable when input permission is missing, or should interactive streams fail closed?
4. Should the first spike capture a display, a window, or either?
5. How should audio degrade when source-specific audio is unavailable?
6. Should desktop records be persisted across daemon restart, or should the spike treat them as ephemeral runtime state?
7. Should stream creation be available only from workspace detail, or also from session detail?

## Acceptance criteria for the spike

The spike is successful when:

- A user can create a desktop tab inside an existing workspace.
- The tab can display a live captured macOS app/window/display.
- Audio reaches the browser when enabled and available.
- Mouse and keyboard input from the browser tab can control the captured source.
- Permission failures are detected and shown clearly.
- Closing the tab or workspace stops capture and input handling.
- The implementation leaves a clear provider boundary for a later Windows implementation.

## Deferred follow-up: workspace-aware app launch

After the stream/control path works, add launch semantics:

- Start a configured native app command with `cwd` set to the workspace path.
- Track PID, child processes, and app/window identity.
- Bind the launched app's selected window to the workspace desktop tab.
- Clean up the launched app when the desktop tab or workspace is disposed.
- Allow agents or run targets to request/open the native app surface for review.
