# native/webrtc-build — our own WebRTC.xcframework for macOS

We build libwebrtc ourselves so the ObjC custom-audio path (`RTCAudioDevice` /
`ObjCAudioDeviceModule`) is reachable from the desktop helper — letting us feed
captured (non-mic) PCM onto a real WebRTC audio track on a peer we own. The
prebuilt LiveKit `webrtc-xcframework` hides this path; owning the build exposes
it. See `docs/superpowers/specs/2026-06-15-desktop-streaming-audio-custom-adm-design.md`.

## Where things live

- **This dir** (`native/webrtc-build/`): build recipe + pinned config only. The
  multi-GB webrtc source is NOT committed — it's fetched into the build checkout.
- **Build checkout (external drive, not committed):** `~/dev-ext/webrtc-build/`
  (→ `/Volumes/UnionSine/dev/webrtc-build`). Contains the cloned
  `webrtc-sdk/webrtc-build` repo, depot_tools, the fetched webrtc source, and
  build output. Put it on the external drive (`~/dev-ext/`), not `~/dev/` — the
  source + build is ~15–20 GB.
- **Output xcframework:** `~/dev-ext/webrtc-build/build/_build/macos_arm64/release/webrtc/WebRTC.xcframework`
  (copied into `native/desktop-macos/Frameworks/` at helper-build time).

## Pinned revision

Tag **`m144.7559.09`** of `webrtc-sdk/webrtc-build` — matches the LiveKit
`webrtc-xcframework` tag `144.7559.09` that S2 used.

- `WEBRTC_VERSION = 144.7559.09`
- `WEBRTC_COMMIT = b1800a61db8320af5c14456c13622d8b85b1ed39`

## Prerequisites (one-time, on the build Mac)

1. Xcode + `xcodebuild` (we build on Xcode 26 / Apple clang 21 / LLVM 22). Run
   `sudo xcodebuild -runFirstLaunch` so `xcodebuild -create-xcframework` can
   load its required plugins.
2. `ninja` on PATH **after** depot_tools. depot_tools' `autoninja` looks for a
   real `ninja` binary and the webrtc source's CIPD copy isn't on PATH. Symlink
   the tree's pinned ninja onto PATH (depot_tools is prepended by run.py, so it
   stays ahead): `ln -sf <checkout>/build/_source/macos_arm64/webrtc/src/third_party/ninja/ninja ~/bin/ninja`
   (ensure `~/bin` is on PATH).

## Local patches (applied on top of tag `m144.7559.09`)

These make M144 build on the current Xcode 26 toolchain and expose the custom-ADM
path. All three are in the build checkout (`~/dev-ext/webrtc-build/build/`):

### 1. `build/run.py` — mac gn args (build*webrtc, `macos*\*` branch)

```
'mac_deployment_target="11.0"',          # was "10.11" — aligned-`new` needs >=10.13 under LLVM 22
'rtc_enable_objc_symbol_export=true',    # was false — else ObjC classes compile hidden and aren't linkable
```

Pass these as extra gn args (the override mechanism is unreliable for the
objc-export flag because run.py forces it in base args and GN's duplicate-arg
handling doesn't let the override win — so edit base):

```
--webrtc-extra-gn-args 'use_clang_modules=false use_libcxx_modules=false'
```

`use_clang_modules=false` / `use_libcxx_modules=false` work as extra args — they
disable clang C++20 modules, whose libc++-vs-macOS-26.5-SDK precompile collides
under the new toolchain. (Modules are a compile optimization; the framework is
identical without them.)

### 2. `webrtc/src/sdk/BUILD.gn` — expose the custom-ADM protocol header on mac

In the `mac_framework_objc` target (`if (is_mac)`,
`apple_framework_bundle_with_umbrella_header`), add `RTCAudioDevice.h` to the
public `sources` list so the umbrella imports it and Swift can see the protocol:

```
sources = [
  "objc/components/audio/RTCAudioDevice.h",     # added
  "objc/api/peerconnection/RTCAudioDeviceModule.h",
  ...
```

The custom-ADM _implementation_ (`objc_audio_device`, `objc_audio_device_module`,
`RTCObjCAudioDeviceDelegate`) is already compiled and linked for mac in M144 —
only the public-header exposure was missing. Verified: `RTCObjCAudioDeviceDelegate`
symbols present in the framework dSYM; `RTCAudioDevice` type visible from Swift.

## Reproduce the build

```bash
# one-time
cd ~/dev-ext && git clone --branch m144.7559.09 https://github.com/webrtc-sdk/webrtc-build.git
# apply the run.py + sdk/BUILD.gn patches above to ~/dev-ext/webrtc-build/build/
ln -sf ~/dev-ext/webrtc-build/build/_source/macos_arm64/webrtc/src/third_party/ninja/ninja ~/bin/ninja

# build (first run fetches depot_tools + syncs ~15 GB of webrtc source; hours)
cd ~/dev-ext/webrtc-build/build
python3 run.py build macos_arm64 --webrtc-gen-force \
  --webrtc-extra-gn-args 'use_clang_modules=false use_libcxx_modules=false'
```

`run.py package macos_arm64` fails on `generate_licenses.py` (shells to
`gn desc :default` and errors) — that step only bundles license text and is
irrelevant to the framework. The xcframework is produced by the `build` step;
we use it directly.

## Consume it from the helper

`cmd/build-desktop-helper` copies the xcframework into
`native/desktop-macos/Frameworks/` (gitignored) and the helper's `Package.swift`
references it as a local binary target (module `WebRTC`). Build with:

```bash
go run ./cmd/build-desktop-helper   # signs with the schmux-desktop-dev identity (S2 recipe)
```
