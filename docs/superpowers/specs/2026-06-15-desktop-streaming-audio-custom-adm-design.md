# Audio — custom AudioDeviceModule on a webrtc build we own

Design spec for the desktop-streaming MVP (`2026-06-12-desktop-streaming-mvp-design.md`, audio / unknown #5). Audio delivered as audio, on a real WebRTC track. No fallbacks. Implemented by `docs/superpowers/plans/2026-06-15-desktop-streaming-audio-custom-adm.md`.

## The mechanism

libwebrtc pulls send-track audio through an `AudioDeviceModule` (ADM). We feed captured non-mic PCM onto a track with a **custom ADM**, installed via webrtc's ObjC custom-audio path: the helper implements the `RTCAudioDevice` protocol (delivering PCM in 10 ms chunks) and `RTCPeerConnectionFactory` installs it through `ObjCAudioDeviceModule`.

That ObjC path is **not** a different mechanism from "a C++ `webrtc::AudioDeviceModule`" — `ObjCAudioDeviceModule` _is_ a `webrtc::AudioDeviceModule`. It is webrtc's own wrapper over the C++ ADM, and it keeps our helper pure Swift/ObjC (no C++ interop in the app; the C++ stays inside the framework). `ObjCAudioDeviceModule` is pure buffer plumbing (`AudioDeviceBuffer`/`FineAudioBuffer`) — it does not use `AVAudioSession`, so porting it to macOS is un-gating, not rewriting. Refs: [ADM design doc](https://chromium.googlesource.com/external/webrtc/+/master/modules/audio_device/g3doc/audio_device_module.md), [custom ADM thread](https://groups.google.com/g/discuss-webrtc/c/1NO0jHalODE), [example impl](https://gist.github.com/mysteryjeans/dfddbf73ab232fd3ef17c51d3b38433d).

**Proven on macOS.** LiveKit's `mixer.capture(appAudio:)` is exactly a custom ADM fed app audio (Path-#1 spike confirmed it's a real, non-mic feed). The only reason we couldn't reuse it was that LiveKit welds its factory and peer inside the Room/Engine (internal, same-factory rule). So the mechanism works; we just need it on a factory **we** own with a peer **we** create, riding our `pion/turn` transport.

## Why owning the build

The `RTCAudioDevice`/`ObjCAudioDeviceModule` custom-audio path is compiled **iOS-only** in stock webrtc, and LiveKit's prebuilt hides it — which is precisely why every prior attempt died:

- Raw `LiveKitWebRTC` (S4): ObjC-only surface, the custom-ADM hook hidden.
- LiveKit SDK (Path #1): has a custom ADM internally, but its factory/peer are module-internal and Room-coupled.

So we build webrtc ourselves with that path **un-gated for macOS** and the factory init exposed, using the same tooling LiveKit ships from: [`webrtc-sdk/webrtc-build`](https://github.com/webrtc-sdk/webrtc-build/blob/main/docs/build.md) (`gn` → `ninja … mac_framework_objc` → `xcodebuild -create-xcframework`). We own the factory and peer, which satisfies the same-factory rule that closed Path #1.

Cost, plainly: we own a webrtc build and toolchain. Real and ongoing. It is the only path that is neither a hack nor a wall.

## The work, in order (this is labor, not a spike)

The mechanism is proven and the build is scripted; there is no yes/no to discover. It is built in order (full plan in the plan doc):

1. **Build webrtc** for macOS arm64 via `webrtc-sdk/webrtc-build`; pin the revision; package + sign the xcframework (reuse the S2 signing recipe), swap it in for LiveKit's via `cmd/build-desktop-helper`, and re-confirm the S2 TCC grant still survives a rebuild against the new binary.
2. **Un-gate the custom-audio path for macOS** — the core engineering: make `RTCAudioDevice`/`ObjCAudioDeviceModule` and the `RTCPeerConnectionFactory` custom-ADM init compile and export for mac. Derive specifics from the iOS source (it's buffer plumbing, no audio session); don't fabricate API.
3. **Custom ADM + tone** — helper implements `RTCAudioDevice` over a PCM ring buffer; factory installs it; raw peer from that factory; direct browser (S4-style). Browser plays the tone on a real track, mic-independent (no Microphone prompt / indicator / Settings entry), no Room.
4. **Real app audio + TCC** — ScreenCaptureKit app/system audio (`capturesAudio`, 48 kHz) → the ring buffer. Browser plays the target app's audio. Records whether SCK audio needs a TCC grant beyond Screen Recording.

## Scope / not in scope

In: the webrtc build + un-gate, the custom ADM, the direct-peer harness, SCK capture. Out: the `pion/turn` relay, `/ws/desktops` signaling, state, dashboard, input — known plumbing that comes after.

## Throwaway vs kept

Throwaway: the direct-peer signaling, tone generator, browser page. Kept: the webrtc build recipe + pinned revision + un-gate patch, the `RTCAudioDevice` ADM, the SCK-audio source — the real helper's audio path, on a binary the rest of the feature (video too) now rides. Result recorded in `2026-06-12-desktop-streaming-mvp-design.md` (Audio section).
