# Desktop Streaming MVP: Helper-Native WebRTC

## Problem

`docs/specs/desktop-streaming-spike.md` defines the feature — stream a native macOS app/window/display into a workspace dashboard tab, with audio and input control, to a remote browser — but leaves the transport unchosen, the scoping questions open, and no engineering plan for the parts nobody has proven yet. This document chooses the architecture and, more importantly, separates the genuine unknowns — the parts no library already guarantees — from the low-uncertainty plumbing, and retires the unknowns with throwaway spikes before building anything on top.

## Principle: no fallbacks

One path per decision, committed to. No "if X fails, fall back to Y," no degradation ladders, no escape hatches. When a path can fail, the spike's job is to find that out and report it — not to silently carry a second design. If a decision turns out wrong, we change the decision. Anything that reads as a fallback gets deleted.

## Approach: prove the unknowns first

This is a spike before it is a build. Most of the work — state records, HTTP endpoints, tabs, the dashboard route — is ordinary schmux plumbing we know how to write. A handful of things we do _not_ know will work, and any one of them failing invalidates the work stacked on top.

So the engineering order is risk-first:

1. **Each genuine unknown is its own throwaway spike impl** that proves exactly one thing with a clear pass/fail.
2. **Spikes use synthetic stand-ins to isolate the unknown.** Audio is proven with generated PCM before real capture exists; input is proven against a synthetic stream. A failure points at one thing, not five.
3. **No integration code is built until the unknowns it depends on are proven.** The manager, state, API, and dashboard come after the spikes pass, not before.
4. **A failed spike is the finding.** We change the decision; we do not build over it.

The spikes are throwaway — their job is to produce a yes/no, not shippable code. The build (the "Target architecture" and "Build" sections below) assembles the real thing once the answers are yes.

## Unknowns, by residual uncertainty

Kill-risk — the blast radius if a thing fails — is _not_ the axis that decides whether to spike. A high-blast-radius item that a mature library handles is still low-uncertainty: running it removes almost nothing you didn't already know, so it does not earn a spike. What earns a spike is the size of the genuine unknown that running it removes. Ranked by that; the "If it fails" column is blast radius, kept only to make the two axes visibly distinct. The `#` is a stable label, not a rank.

| #   | Unknown                                                                                              | Residual uncertainty                                                                                                                                                                              | If it fails (blast radius)                     | Decision                                                                                                                                    |
| --- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| 5   | SCK PCM feeds a libwebrtc audio track via a custom source; whether SCK audio needs its own TCC grant | **HIGH** — no clean per-track PCM push; injecting custom PCM means a custom AudioDeviceModule at the `PeerConnectionFactory`, and whether the pinned xcframework exposes that hook is unconfirmed | No audio                                       | **S4: closed (negative)** — prebuilt framework has no public PCM push; audio route unresolved (pursuing `client-sdk-swift`); TCC unanswered |
| 6   | CGEvent injection across Retina/CSS scaling and the hard input cases                                 | **MEDIUM-HIGH** — CGEvent guarantees only the easy case; secure-input fields, synthetic-input-refusing apps, and mid-stream window moves are empirical                                            | No or partial control                          | **Spike (S5)**                                                                                                                              |
| 3   | macOS TCC grants survive a signed dev rebuild                                                        | **MEDIUM** — undocumented OS behavior; no library answers it                                                                                                                                      | Dev loop unworkable; every later spike painful | **Proven (S2)** — reused self-signed cert; DR keyed on cert leaf, not cdhash                                                                |
| 4   | ScreenCaptureKit frames feed a libwebrtc video track                                                 | **LOW** — documented push path (`CVPixelBuffer` → `RTCVideoSource`)                                                                                                                               | No live picture                                | Build as plumbing                                                                                                                           |
| 2   | A prebuilt libwebrtc embeds in a signed helper and interops through the relay                        | **LOW** — exactly what the LiveKit `webrtc-xcframework` ships to do                                                                                                                               | "Helper owns WebRTC" wrong                     | Build (S2 byproduct)                                                                                                                        |
| 1   | A daemon-embedded `pion/turn` force-relay carries WebRTC UDP to a remote browser                     | **LOW** — `pion/turn`'s core documented function                                                                                                                                                  | Whole transport architecture wrong             | Build as plumbing                                                                                                                           |

So three unknowns get a spike (#5 audio, #6 input, #3 TCC) and three are built as ordinary plumbing (#4 video, #2 interop, #1 transport). Note the inversion: #1 has the largest blast radius and the smallest uncertainty — catastrophic if it failed, but it won't, because it is the library doing its documented job.

Audio (#5) is not an afterthought: it is the highest-uncertainty item and gets an explicit proof. It reuses the capture and peer that the plumbing and S2 stand up — there is no longer a video spike for it to sit behind.

## Spike plan

Only the genuine unknowns get a throwaway spike. The transport relay (`pion/turn` force-relay) and the ScreenCaptureKit→video-track path are graded low-uncertainty — the libraries do their documented jobs — so they are built as ordinary plumbing (see Build) and used as the substrate the spikes run on, not proven by dedicated spikes. S1 (transport) and S3 (video capture) are therefore dropped as spikes; the numbering keeps S2/S4/S5 to match the grading. Each spike below is the smallest program that answers one real unknown.

### S2 — Signed helper + TCC stability

Full spec: `2026-06-13-desktop-streaming-s2-tcc-spike-design.md`.

**Goal:** prove the one genuine unknown — that a Screen Recording (TCC) grant survives a signed dev rebuild — and, because this is the first signed build, bootstrap the build/sign toolchain (a signed `schmux-desktop-macos` that embeds the LiveKit `webrtc-xcframework`). No media: the relay, peer, video, and audio are low-uncertainty plumbing or later spikes, kept out so a failure here can only mean signing/TCC.

**Impl:** minimal signed `schmux-desktop-macos` built via the `go run ./cmd/build-desktop-helper` wrapper, with one-shot `check-permission` / `request-permission` commands over the Screen Recording grant (`CGPreflightScreenCaptureAccess` / `CGRequestScreenCaptureAccess`). It links the xcframework but does not stream.

**TCC-stability check (unknown #3) — the reason this spike exists:** request Screen Recording, grant it, rebuild the helper twice with the stable signing identity, confirm the grant survives without re-prompting.

**Pass:** post-rebuild `check-permission` returns granted on both rebuilds, with a single entry in the Screen Recording list.

**If it fails:** dev signing won't hold TCC grants — fix signing before S4–S5, or the dev loop is unworkable.

### S4 — ScreenCaptureKit audio → audio track

**Goal:** SCK app/system audio PCM → libwebrtc audio track via a custom audio source (not the default mic device module).

**Impl:** SCK audio capture → PCM → custom libwebrtc audio source, on the helper peer from S2.

**Pass:** the browser plays the captured audio; resolves whether SCK audio needs its own TCC grant beyond Screen Recording.

**If it fails:** that is the finding — audio gets reconsidered, not silently dropped to video-only.

### S5 — Input injection + coordinate mapping

**Goal:** browser pointer/key events over the data channel → CGEvent into the captured app, correctly mapped.

**Impl:** data-channel input → coordinate transform (Retina + CSS scaling) → CGEvent; run the spike spec's input checklist: modifiers, wheel direction, window move/resize mid-stream, synthetic-input-refusing apps, secure input fields.

**Pass:** clicks and keys land on the right spot in the captured app; checklist results recorded (some apps legitimately refuse synthetic input — that is data, not failure).

**If it fails:** the "control" promise is at risk; the checklist results define what is controllable.

Cannot run in CI (Docker E2E is Linux-only); S2, S4, S5 are manual, on a Mac.

---

After the spikes (S2, S4, S5) pass, the unknowns are retired and the architecture below is known-buildable. The build assembles it with ordinary schmux patterns and unit tests — including the transport relay and SCK→video-track plumbing that were graded low-uncertainty and built rather than spiked. (Remote UDP reachability to the daemon's TURN port is a deployment fact confirmed with a quick reachability check, not a WebRTC spike.)

## Target architecture

### Transport

The viewer is remote: a local app on the capture machine, streamed to a browser elsewhere. The helper owns WebRTC — it embeds the pinned prebuilt libwebrtc and is one end of the peer connection; the browser is the other. The daemon is the hub both ends reach: it carries signaling and hosts a `pion/turn` relay that forwards the media, but it never reads or re-encodes a media byte.

```text
  Dashboard tab — remote browser
    <video> + input capture
        |  control:      signaling over WS (TCP)
        |  media+input:  UDP to the daemon's TURN relay
        v
  schmux daemon — capture machine
    signaling WS handler + embedded pion/turn relay
    forwards opaque UDP datagrams; never reads media
        |  control:      signaling over stdio
        |  media+input:  loopback UDP to/from the relay
        v
  schmux-desktop-macos — capture machine
    ScreenCaptureKit capture + libwebrtc encode + CGEvent input
        |
        v
    selected app / window / display

  Signaling    browser <-> daemon <-> helper       WS + stdio; SDP/ICE/status + TURN creds
  Media+input  browser <-> daemon-TURN <-> helper  UDP; SRTP video/audio + SCTP input channel
               browser–daemon leg crosses the network; daemon–helper leg is loopback
```

**Why a TURN relay.** The browser is remote; the helper sits on the daemon's machine with no address the browser can reach directly. The standard answer to "two peers that can't reach each other, must stay UDP" is a TURN relay — a reachable host that forwards UDP datagrams between them. The daemon already is that host: it served the dashboard. So it embeds `pion/turn` and both peers connect through it. (Media never rides the TCP signaling socket — that would reintroduce head-of-line blocking and defeat WebRTC.)

**Force-relay.** ICE uses the relay candidate only — no host or server-reflexive candidates (`iceTransportPolicy: "relay"` on both ends). One path, deterministic.

**Opaque forwarding.** The relay moves UDP packets; it does not terminate DTLS, parse SRTP, or touch codecs — out of the media hot path (no decode/encode cost), still the single network endpoint, consistent with how previews and terminals route through the daemon.

**Establishment:** dashboard opens `/ws/desktops/{desktopId}/signal` → daemon allocates a TURN relay for the desktop and mints short-lived credentials, handing them to the browser (over the WS) and the helper (over stdio) → helper builds its peer (video + optional audio track, input data channel) and offers; browser answers; both gather only relay candidates → ICE connects through the relay, DTLS completes end-to-end between browser and helper, media and input flow as opaque UDP through the daemon. Teardown (helper exit, socket close, or `DELETE`) converges: stop helper → free TURN allocation → close peer → remove record → close tab.

### `internal/desktop` package

Mirrors the preview manager (`internal/preview/manager.go`):

```go
// manager.go
func New(st *state.State, logger *slog.Logger) *Manager
func (m *Manager) SetWorkspaceManager(wm workspace.WorkspaceManager)
func (m *Manager) Create(ctx context.Context, ws state.Workspace, spec CreateSpec) (state.WorkspaceDesktop, error)
func (m *Manager) List(workspaceID string) []state.WorkspaceDesktop
func (m *Manager) Delete(workspaceID, desktopID string) error
func (m *Manager) DeleteWorkspace(workspaceID string) error // workspace dispose
func (m *Manager) Reconcile() error                          // startup: drop all records, close tabs
func (m *Manager) Stop()                                     // daemon shutdown: stop all helpers

// provider.go — platform boundary, one implementation ("macos") for the MVP
type Provider interface {
    Name() string
    Permissions(ctx context.Context) (Permissions, error)
    ListSources(ctx context.Context) ([]Source, error)
    Start(ctx context.Context, spec StartSpec) error // spawns one helper process, tracked by DesktopID
    Stop(ctx context.Context, desktopID string) error
}

// CreateSpec is the create request the manager receives: TargetKind, TargetID, Label, Audio.
// StartSpec is CreateSpec plus the manager-assigned DesktopID and WorkspaceID.
// The provider tracks running helpers keyed by DesktopID; Stop looks them up by that key.
```

`signaling.go` owns the websocket handler and the relay loop. `macos.go` implements `Provider` by invoking the helper binary.

### State

`WorkspaceDesktop` as the spike spec defines it, minus `Interactive` and `HasVideo` (every stream controls the app and has video, so both flags are dead): ID, WorkspaceID, Provider, TargetKind `display|window|app`, TargetID, Label, Status, HasAudio, LastError, Meta, CreatedAt, LastUsedAt — plus:

```go
// state.State
Desktops map[string]WorkspaceDesktop `json:"desktops,omitempty"`
```

Records are ephemeral: `Reconcile()` at startup removes every persisted record and closes its tab — peer connections and helpers cannot survive a restart, and reconnection belongs to the deferred launch-semantics work. Records ride the existing `/ws/dashboard` broadcast like preview records — no polling. Statuses: `starting | permission_required | ready | stopped | failed`.

### Tab integration

Tabs go through the workspace manager, not raw `state.Tab` construction (the spike spec's sketch predates right-tabs-mutation and is superseded):

```go
func (m *Manager) OpenDesktopTab(wsID, desktopID, label string) (*state.Tab, error)
```

| Kind      | ID Scheme                 | Route                         | Label                      | Closable | Meta         |
| --------- | ------------------------- | ----------------------------- | -------------------------- | -------- | ------------ |
| `desktop` | `sys-desktop-{desktopID}` | `/desktop/{wsID}/{desktopID}` | user label or window title | true     | `desktop_id` |

The desktop manager registers a `TabCloseHook` for kind `"desktop"` at daemon wiring (like the preview manager's hook): `CanClose` always true; `OnTabClose` stops the helper and removes the record. Closing the tab is the same operation as `DELETE`-ing the desktop.

### HTTP API

```text
POST   /api/workspaces/{workspaceId}/desktops      create (request/response per spike spec, minus interactive and has_video)
GET    /api/workspaces/{workspaceId}/desktops      list
DELETE /api/workspaces/{workspaceId}/desktops/{desktopId}
GET    /api/desktop/sources                        displays + windows from provider list-sources
GET    /api/desktop/permissions                    per spike spec
```

Request/response types live in `internal/api/contracts/desktop.go` and flow to the frontend via `go run ./cmd/gen-types`. `docs/api.md` gains a Desktops section (CI-enforced).

### Signaling

`/ws/desktops/{desktopId}/signal`, JSON envelope (`type: offer|answer|ice|status|error`). The daemon relays `offer|answer|ice` between the browser websocket and the helper's stdio, and originates `status`/`error` from lifecycle events. The same channel delivers ephemeral TURN credentials to the browser (the helper gets its copy over stdio). Signaling is small control data on the existing TCP websocket; never on the media path. Writes go through the `wsConn` mutex wrapper. One browser connection per desktop; a second replaces the first (terminal-socket semantics).

### Helper: `schmux-desktop-macos`

One-shot commands: `permissions`, `list-sources` — JSON to stdout, exit.

Long-running: `start --desktop-id ... --target-kind ... --target-id ... [--audio]` — JSON-lines over stdio:

```text
daemon → helper: {"type":"signal", "payload":{...}}        relayed browser signaling
daemon → helper: {"type":"stop"}
helper → daemon: {"type":"signal", "payload":{...}}        helper's offers/answers/ICE
helper → daemon: {"type":"status", "status":"ready|permission_required|failed", "error":"..."}
```

Stop escalation: stdio `stop` → SIGTERM after 3s → SIGKILL. Helper exit moves the desktop to `failed`, or `stopped` if a stop was requested. One process per desktop: kill-the-PID is the whole crash story. Capture: ScreenCaptureKit, window capture primary; display capture is the same API and a selectable target kind. Input: CGEvent injection mapping browser coordinates to captured-source coordinates.

### Build and signing

Swift package at `native/desktop-macos/`, built by `go run ./cmd/build-desktop-helper` following the `build-dashboard` wrapper pattern. The daemon looks for the helper next to its own binary, with a config override for development.

**Signing — proven in S2.** A reused self-signed identity (`schmux-desktop-dev`) holds the Screen Recording (TCC) grant across rebuilds: the wrapper signs the embedded `LiveKitWebRTC.framework`, then the binary, with that identity, yielding a designated requirement keyed on the **certificate leaf** rather than the per-build `cdhash`. Rebuilds change the cdhash but reuse the identity, so the DR — and the grant keyed on it — stays stable. Verify the identity with `codesign -dvvv`; `security find-identity -v -p codesigning` reports `0 valid identities` for a self-signed dev cert (a false negative). The wrapper is run via `go run`, and its compiled form (`/build-desktop-helper`) is gitignored so a stray build never gets staged. Release bundling is post-MVP.

### Dashboard

Route `/desktop/:workspaceId/:desktopId` renders connection state, permission warnings with remediation, the `<video>` element, an input-capture overlay (pointer/wheel/keyboard), a visible focus affordance while input is being sent, and a stop action. Creation is a workspace-detail action ("Open Desktop Stream"): fetch `GET /api/desktop/sources`, render the picker in the dashboard (no native `SCContentSharingPicker` — newer-macOS-only and bypasses daemon state), `POST` the chosen source, navigate via pending-navigation when the tab appears on the broadcast. Workspace detail is the only entry point.

### Permissions

A desktop stream always routes input, so both Screen Recording (video) and Accessibility (input) are required. If either grant is missing, the stream does not start: status `permission_required`, `LastError` names the missing grant(s), the tab shows remediation. Fail closed. (Whether SCK audio needs a third grant beyond Screen Recording is unresolved — S4 closed before reaching it.)

### Audio

ScreenCaptureKit captures audio alongside video (macOS 13+): app-scoped for window/app targets, system audio for displays. **Delivery into WebRTC is unresolved.** S4 proved the prebuilt `webrtc-xcframework` exposes no clean public API to push captured PCM into a send track — its only public audio-write hook rides the microphone recording path — so the original custom-libwebrtc-source route is closed.

**Path #1 — LiveKit `client-sdk-swift` custom audio: ruled out (FAIL, source read 2026-06-14).** Whether the SDK can publish app-supplied PCM _without opening the mic_ on a helper-owned peer was tested against the pinned `2.15.0` source and failed on all three hooks:

- **Factory not exposed (Room-decoupling fails).** The SDK's `LKRTCPeerConnectionFactory` lives in `actor RTC` (`Core/RTC.swift:27,52`), which is module-internal; `createPeerConnection` / `createAudioSource` / `createAudioTrack` (`Core/RTC.swift:78,95,99`) are all internal too. No public API creates a peer outside the `Room`/`Engine`, so an SDK audio track cannot ride a helper-owned peer.
- **No custom-PCM audio source.** There is no audio `Capturer` (only video has `BufferCapturer`). The sole constructor `LocalAudioTrack.createTrack` (`Track/Local/LocalAudioTrack.swift:57`) builds from the internal factory and hard-codes `source: .microphone`; there is no public init taking an external source. `AudioCustomProcessingDelegate` (`Protocols/AudioCustomProcessingDelegate.swift:24`) only modifies the captured buffer **in place** — it does not replace the source. `AudioDeviceModuleType` (`Audio/Manager/AudioManager+ModuleType.swift:19`) offers only `.audioEngine` / `.platformDefault`, both mic ADMs — no injectable/custom ADM.
- **Mic is the source (mic-independence fails).** `LocalAudioTrack.startCapture()` (`Track/Local/LocalAudioTrack.swift:95`) → `AudioManager.startLocalRecording()` (`Audio/Manager/AudioManager.swift:397`, "Starts mic input") → `audioDeviceModule.initAndStartRecording()`.

So the SDK's audio capture path **is** the microphone, and its factory/peer are `Room`-coupled and internal. Path #1 cannot deliver mic-independent, Room-decoupled PCM — the candidate is closed, not unproven. The remaining routes are the same two named in S4: **path #2** reconstruct the framework's hidden `RTCAudioDevice` for a custom ADM on the raw `LiveKitWebRTC` factory (the SDK is not reused — it adds nothing over the framework for this), or **path #3** move PCM over the data channel + Web Audio. Neither is a silent drop to video-only. Whether SCK audio needs its own TCC grant beyond Screen Recording is also still open.

### Lifecycle

Create: API call → record (`starting`) → `OpenDesktopTab` → spawn helper → dashboard connects to signaling socket → daemon allocates TURN relay and mints creds → SDP/ICE relay → tracks live → `ready`. Stop (tab close and `DELETE` converge via the close hook): signal helper to stop → free TURN allocation → peer closes → record removed → tab closed. Workspace dispose: `DeleteWorkspace` stops every desktop for the workspace. Daemon shutdown: `Stop()` requests graceful helper exit, then kills lingering daemon-owned helpers.

### Security

- **Auth:** the signaling WS and all `/api/*` use the dashboard's existing auth. No desktop is created or viewed without an authenticated session.
- **Media-path auth:** the TURN relay accepts only the short-lived credentials the daemon mints per desktop during the authenticated signaling handshake; the credentials die with the desktop.
- **Encryption:** media is DTLS-SRTP end-to-end between browser and helper. The daemon forwards encrypted datagrams it cannot read.
- **Consent:** no stream starts before explicit source selection and permission checks; a persistent UI indicator shows while input is being sent.
- **No retention:** captured media is never stored; raw input events are not logged outside explicit debug mode.

This drops the spike spec's "local-only" framing. Remote viewing is the point; the protection is auth plus ephemeral relay credentials plus end-to-end DTLS, not network locality.

## Build (after spikes pass)

The known plumbing, assembled once S2, S4, and S5 are green.

**New**

- `internal/desktop/` — manager, provider interface, macOS provider, signaling handler + relay
- `native/desktop-macos/` — Swift helper package (first non-Go binary in the repo)
- `cmd/build-desktop-helper` — Go build wrapper
- `internal/api/contracts/desktop.go` + regenerated `types.generated.ts`
- Dashboard: `/desktop/:workspaceId/:desktopId` route, source-picker flow, desktop tab rendering
- `/ws/desktops/{desktopId}/signal` endpoint
- `docs/api.md` Desktops section

**Modified**

- `state.State` — `Desktops` map
- `internal/workspace/tabs.go` — `OpenDesktopTab`
- Daemon wiring — construct desktop manager, register tab close hook, startup `Reconcile()`, shutdown `Stop()`
- `internal/dashboard/handlers*` — desktop endpoints

**Unchanged**

- Preview manager, existing tab kinds, `/ws/dashboard` broadcast model, terminal websockets
- The spike spec's API payloads, state struct, statuses, input schemas — adopted as written, except `interactive` and `has_video` are dropped

## Testing

- **Go unit tests** (`./test.sh`): manager lifecycle against a fake `Provider` (create/stop/dispose/reconcile, status transitions, helper-exit handling), state CRUD, HTTP handlers, signaling relay framing, `OpenDesktopTab` + close-hook behavior.
- **Frontend (vitest):** desktop route with mocked signaling websocket and `RTCPeerConnection`; picker flow; pending navigation on tab broadcast.
- **Not in CI:** the Swift helper and real capture/input/audio — that coverage is the S2, S4, and S5 spikes plus the manual input checklist, run on a Mac.

## Acceptance

The spike spec's criteria: live video in a workspace tab, audio when enabled and available, browser input controls the source, permission failures visible, teardown leaves no capture running. The Provider boundary exists so a later Windows implementation plugs in without touching the daemon API, state, or dashboard route — demonstrated by the interface, not built in this MVP.
