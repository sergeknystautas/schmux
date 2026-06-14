import CoreGraphics
import Foundation
import LiveKitWebRTC  // linked to prove the framework embeds + signs; not called into.

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
default:
    FileHandle.standardError.write("usage: schmux-desktop-macos <check-permission|request-permission>\n".data(using: .utf8)!)
    exit(2)
}
