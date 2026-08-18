// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "schmux-desktop-macos",
    platforms: [.macOS(.v13)],
    targets: [
        // Our own WebRTC.xcframework (built via native/webrtc-build), copied here
        // by cmd/build-desktop-helper from the external-drive build output.
        // Module name is `WebRTC`. Supersedes the LiveKit webrtc-xcframework SPM
        // dependency used in S2 — we own the build so the custom-ADM path is exposed.
        .binaryTarget(name: "WebRTC", path: "Frameworks/WebRTC.xcframework"),
        .executableTarget(
            name: "schmux-desktop-macos",
            dependencies: [
                .target(name: "WebRTC")
            ]
        ),
    ]
)
