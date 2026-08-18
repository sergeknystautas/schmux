# Audio — build webrtc with a custom ADM and stream app audio — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Ship the target app's audio on a **real WebRTC audio track**, on a helper-owned peer, mic-free — by building libwebrtc ourselves with a custom `AudioDeviceModule` we feed ScreenCaptureKit PCM into.

**This is not a spike.** The mechanism is proven (LiveKit's `mixer.capture(appAudio:)` is a custom ADM fed app audio on macOS) and the build is scripted (`webrtc-sdk/webrtc-build`, the tooling LiveKit ships from). There is no yes/no to discover — it is a body of known work, built in order.

**Architecture:** Patch `webrtc-sdk/webrtc-build` to expose, on **macOS**, a custom audio-device hook (port the iOS `RTCAudioDevice`/`ObjCAudioDeviceModule` machinery, which webrtc gates to iOS) — an ObjC protocol the app implements to deliver PCM, plus an `RTCPeerConnectionFactory` initializer that installs it. Build a signed xcframework, swap it in for LiveKit's. The helper implements that protocol in Swift, feeds it SCK audio, creates its own factory + peer, streams direct to a browser. The helper stays Swift/ObjC — the C++ lives inside the framework.

**Tech Stack:** `webrtc-sdk/webrtc-build` (depot_tools / gn / ninja), a webrtc ObjC-SDK patch (C++/ObjC++), Swift helper, ScreenCaptureKit, the S2 `cmd/build-desktop-helper` + `schmux-desktop-dev` signing.

**Spec:** `docs/superpowers/specs/2026-06-15-desktop-streaming-audio-custom-adm-design.md`

**Reality this plan accepts:**

- **The build is the bulk of the work and runs for hours** (multi-GB checkout, full compile). It runs on a Mac, outside any chat — a human or long-running agent drives it. Pin the revision.
- **The helper can't compile until the framework exists.** Tasks are ordered so the framework lands first.
- **Verification is manual on a Mac** (listen + mic check). Commits are the user's call (`/commit`); this plan never runs `git commit`.
- **The one genuinely unproven detail** is the patch — porting the iOS custom-audio-device path to macOS. It is _work_, not a wall: the iOS implementation in webrtc's tree is the reference to port, and `webrtc-sdk`/LiveKit already patch this layer for custom audio. Where the exact diff isn't known, the step says to derive it from the iOS source, not to fabricate it.

---

## Task 1: Stand up the webrtc build (baseline)

**Files:** new `native/webrtc-build/` (build scripts + pinned config; not the webrtc source — that's fetched).

- [ ] **Step 1: depot_tools + checkout.** Clone `webrtc-sdk/webrtc-build`; follow its `docs/build.md`. Pin a release tag (match what we can sign; record it). Fetch the webrtc source it references.
- [ ] **Step 2: Baseline macOS arm64 framework.** Build unmodified, to confirm the toolchain works end to end:
      `gn gen out/mac_arm64 --args='target_os="mac" target_cpu="arm64" is_debug=false is_component_build=false rtc_include_tests=false rtc_enable_protobuf=false'`
      then `ninja -C out/mac_arm64 mac_framework_objc`.
      Expected: `out/mac_arm64/WebRTC.framework` builds clean.
- [ ] **Step 3: Package + sign.** `xcodebuild -create-xcframework` → `WebRTC.xcframework`; codesign with `schmux-desktop-dev` (reuse the S2 recipe). Confirm it links into a throwaway Swift binary that calls a trivial symbol (e.g. `RTCPeerConnectionFactory()`), signed, launching.

---

## Task 2: Patch in the custom-ADM hook (macOS)

**Files:** a patch under `webrtc-sdk/webrtc-build`'s patch set; rebuilt `WebRTC.xcframework`.

- [ ] **Step 1: Locate the iOS custom-audio path.** In the webrtc source: `sdk/objc/native/api/audio_device_module.h/.mm`, `sdk/objc/components/audio/RTCAudioDevice.h`, `ObjCAudioDeviceModule`. These are compiled for iOS only. Identify the platform guards (`#if TARGET_OS_IPHONE` / BUILD.gn `ios`-only sources).
- [ ] **Step 2: Un-gate for macOS.** Patch the guards / `BUILD.gn` so `RTCAudioDevice` + `ObjCAudioDeviceModule` compile for mac. If the iOS impl assumes `AVAudioSession` (iOS-only), provide the mac equivalent or stub the session bits — the ADM only needs the buffer plumbing (`AudioDeviceBuffer` / `FineAudioBuffer`), not a session. Derive specifics from the iOS source; do not invent API.
- [ ] **Step 3: Expose factory injection.** Ensure `RTCPeerConnectionFactory` has an initializer that takes the custom ADM (the iOS SDK already wires `audioDevice:` → `CreateAudioDeviceModule`; make that path available on mac). Export the headers in the framework.
- [ ] **Step 4: Rebuild + sign** (Task 1 steps 2–3 with the patch). Confirm `RTCAudioDevice` and the factory init are visible from Swift in the signed framework. **This task is the core engineering — budget for it.**
- [ ] **Step 5: Wire `cmd/build-desktop-helper`** to consume our `WebRTC.xcframework` instead of LiveKit's, and re-confirm the S2 TCC grant still survives a rebuild with the new binary. Commit point.

---

## Task 3: Custom AudioDeviceModule fed by a ring buffer

**Files:** `native/desktop-macos/Sources/.../SchmuxAudioDevice.swift` (implements `RTCAudioDevice`).

- [ ] **Step 1: Implement `RTCAudioDevice`.** Hold a lock-free PCM ring buffer (48 kHz mono Int16). The protocol's render callback pulls 10 ms (480 samples) per tick from the buffer into WebRTC's `deliverRecordedData`/`FineAudioBuffer`; underruns deliver silence. Keep the device's declared format (rate/channels/IO duration) in sync with `AudioDeviceBuffer` (mismatched params corrupt audio — known footgun).
- [ ] **Step 2: A `push(pcm:)` entry** the capture side calls to enqueue samples into the ring.

---

## Task 4: Helper — factory + ADM + peer + tone (real track, mic-free)

**Files:** `native/desktop-macos/Sources/.../AudioStream.swift`, `main.swift` (+ `audio-stream` subcommand), `native/desktop-macos/spike/answerer.html` (direct-peer page, S4-style).

- [ ] **Step 1: Factory with our ADM.** Create `RTCPeerConnectionFactory` initialized with a `SchmuxAudioDevice` instance (Task 3). Create an audio track from it; create a raw `RTCPeerConnection` from the same factory; `addTrack`. No Room, no relay.
- [ ] **Step 2: Direct signaling** (copy-paste SDP over stdio + `answerer.html`, the proven S4 harness).
- [ ] **Step 3: Tone.** Generate a 440 Hz sine and `push(pcm:)` it at 10 ms cadence.
- [ ] **Step 4: Verify (manual, Mac).** Browser plays the tone on a real audio track; **mic-independent** — no Microphone prompt, indicator off, no Microphone entry for the helper. Commit point.

---

## Task 5: ScreenCaptureKit audio → the ADM

**Files:** `native/desktop-macos/Sources/.../AudioStream.swift` (+ `--capture`).

- [ ] **Step 1: SCK audio.** `SCStreamConfiguration.capturesAudio = true`, 48 kHz; filter = window/app (app-scoped) or display (system). `SCStreamOutput` on `.audio`.
- [ ] **Step 2: Bridge.** In `didOutputSampleBuffer` for `.audio`, convert the `CMSampleBuffer` to Int16 PCM and `SchmuxAudioDevice.push(pcm:)`. Replace the tone when `--capture` is set.
- [ ] **Step 3: Verify (manual, Mac).** Browser plays the **target app's** actual audio, still mic-free. Record whether SCK audio required a TCC grant beyond Screen Recording (the open audio-TCC question). Commit point.

---

## Task 6: Land it

- [ ] **Step 1:** Update `docs/superpowers/specs/2026-06-12-desktop-streaming-mvp-design.md` Audio section: audio shipped via our webrtc build + custom ADM; the pinned webrtc revision and build recipe; the audio-TCC answer.
- [ ] **Step 2:** Keep `native/webrtc-build/`, the patch, `SchmuxAudioDevice`, `AudioStream` capture path. Throwaway: the copy-paste signaling + `answerer.html` + tone path. Commit point.

---

## Self-review

- **Coverage:** build → Tasks 1–2; custom ADM → 3; real track + mic-free → 4; real app audio + TCC → 5; landed → 6. The webrtc build/maintenance commitment is explicit (Tasks 1–2, the bulk).
- **Honesty:** the only non-turnkey step is the Task 2 patch (porting the iOS custom-audio path to mac); it's flagged as derive-from-source, not fabricated. Everything else (gn/ninja args, the ADM 10 ms contract, SCK config, the direct-peer harness) is concrete.
- **Not a spike:** no kill gate, no "find out if it works" — it's proven; this builds it.
