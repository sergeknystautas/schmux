// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "schmux-desktop-macos",
    platforms: [.macOS(.v13)],
    dependencies: [
        // Pinned to the newest stable tag (chosen 2026-06-13 via git ls-remote --tags).
        .package(url: "https://github.com/livekit/webrtc-xcframework.git", exact: "144.7559.09")
    ],
    targets: [
        .executableTarget(
            name: "schmux-desktop-macos",
            dependencies: [
                .product(name: "LiveKitWebRTC", package: "webrtc-xcframework")
            ]
        )
    ]
)
