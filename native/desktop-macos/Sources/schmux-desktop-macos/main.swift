import CoreGraphics
import Foundation
import WebRTC  // our own build (native/webrtc-build); linked to prove it embeds + signs.

func printJSON(_ obj: [String: Any]) {
    let data = try! JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write("\n".data(using: .utf8)!)
}

let command = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : ""

switch command {
case "check-permission":
    let granted = CGPreflightScreenCaptureAccess()
    printJSON(["command": "check-permission", "granted": granted])
    exit(granted ? 0 : 1)
case "request-permission":
    let granted = CGRequestScreenCaptureAccess()
    printJSON(["command": "request-permission", "granted": granted])
    exit(granted ? 0 : 1)
case "audio-stream":
    // Stream a 440 Hz tone on a real WebRTC track via the custom ADM (Task 4).
    // SDP exchanged over stdio against spike/answerer.html. Mic-free.
    AudioStream.run()
default:
    FileHandle.standardError.write("usage: schmux-desktop-macos <check-permission|request-permission|audio-stream>\n".data(using: .utf8)!)
    exit(2)
}
