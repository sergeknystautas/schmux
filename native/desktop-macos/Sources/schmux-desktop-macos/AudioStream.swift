import Foundation
import WebRTC

/// Task 4 spike: stream a 440 Hz tone on a real WebRTC audio track, mic-free.
///
/// Creates an `RTCPeerConnectionFactory` with our `SchmuxAudioDevice`, builds an
/// audio track + raw peer from that factory, and signals directly with a browser
/// via SDP-over-stdio (copy-paste, S4-style). A 440 Hz sine is fed to the ADM at
/// 10 ms cadence; the browser should play it on a real track with no Microphone
/// prompt/indicator. No Room, no relay — host candidates only.
final class AudioStream: NSObject, RTCPeerConnectionDelegate {

    private let audioDevice = SchmuxAudioDevice()
    private var factory: RTCPeerConnectionFactory!
    private var pc: RTCPeerConnection!
    private var track: RTCAudioTrack!

    private let iceGathered = DispatchSemaphore(value: 0)
    private var iceGatheringComplete = false

    static func run() {
        AudioStream().start()
    }

    func start() {
        // Factory that owns our ADM (same-factory rule satisfied: factory,
        // track, and peer all come from here).
        factory = RTCPeerConnectionFactory(
            encoderFactory: nil, decoderFactory: nil, audioDevice: audioDevice)

        let source = factory.audioSource(with: nil)
        track = factory.audioTrack(with: source, trackId: "schmux-audio0")

        let config = RTCConfiguration()
        config.iceServers = []  // host candidates only — helper + browser are local
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        pc = factory.peerConnection(with: config, constraints: constraints, delegate: self)
        pc.add(track, streamIds: ["schmux"])

        startTone()

        // Offer → gather ICE → print.
        pc.offer(for: constraints) { [weak self] sdp, _ in
            guard let self, let sdp else { return }
            self.pc.setLocalDescription(sdp) { _ in }
        }
        iceGathered.wait()

        let offer = pc.localDescription!
        let offerJSON = SDP.encode(offer)
        out("==== PASTE THIS OFFER INTO answerer.html ====")
        out(offerJSON)
        out("==== (end offer) ====")

        err("==== PASTE THE ANSWER JSON FROM answerer.html BELOW, THEN ENTER ====")
        guard let line = readLine()?.trimmingCharacters(in: .whitespacesAndNewlines),
              let answer = SDP.decode(line) else {
            err("could not parse answer JSON"); exit(1)
        }
        pc.setRemoteDescription(answer) { _ in }

        err("waiting for connection… Ctrl+C to stop")
        dispatchMain()  // stay alive; tone keeps flowing
    }

    // MARK: - Tone (440 Hz sine → ADM ring buffer)
    private var phase: Double = 0
    private var toneTimer: DispatchSourceTimer?
    private func startTone() {
        pushTone(frames: 48_000 / 5)  // pre-fill ~200 ms so the ADM never underruns at start
        let t = DispatchSource.makeTimerSource(queue: .global())
        t.schedule(deadline: .now(), repeating: 0.01)  // 10 ms
        t.setEventHandler { [weak self] in self?.pushTone(frames: 480) }
        t.resume()
        toneTimer = t
    }
    private func pushTone(frames: Int) {
        let amplitude: Double = 0.3
        let freq: Double = 440.0
        var samples = [Int16](repeating: 0, count: frames)
        for i in 0..<frames {
            let t = phase / 48_000.0
            samples[i] = Int16(amplitude * Double(Int16.max) * sin(2 * .pi * freq * t))
            phase += 1
        }
        audioDevice.push(samples)
    }

    // MARK: - RTCPeerConnectionDelegate
    func peerConnection(_ pc: RTCPeerConnection, didChange newState: RTCIceGatheringState) {
        err("ice gathering: \(newState.rawValue)")
        if newState == .complete, !iceGatheringComplete {
            iceGatheringComplete = true
            iceGathered.signal()
        }
    }
    func peerConnection(_ pc: RTCPeerConnection, didChange newState: RTCIceConnectionState) {
        err("ice connection: \(newState.rawValue)")
    }

    // Required delegate stubs we don't need for the spike.
    func peerConnection(_ pc: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    func peerConnection(_ pc: RTCPeerConnection, didAdd stream: RTCMediaStream) {}
    func peerConnection(_ pc: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    func peerConnection(_ pc: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {}
    func peerConnection(_ pc: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    func peerConnection(_ pc: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}
    func peerConnection(_ pc: RTCPeerConnection, didAdd receiver: RTCRtpReceiver, streams: [RTCMediaStream]) {}
    func peerConnectionShouldNegotiate(_ pc: RTCPeerConnection) {}
}

/// Encode/decode RTCSessionDescription as `{type, sdp}` JSON for browser exchange.
enum SDP {
    static func encode(_ sdp: RTCSessionDescription) -> String {
        let type: String
        switch sdp.type {
        case .offer: type = "offer"
        case .answer: type = "answer"
        default: type = "pranswer"
        }
        let data = try! JSONSerialization.data(
            withJSONObject: ["type": type, "sdp": sdp.sdp], options: [.sortedKeys])
        return String(data: data, encoding: .utf8) ?? "{}"
    }
    static func decode(_ json: String) -> RTCSessionDescription? {
        guard let data = json.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: String],
              let typeStr = obj["type"], let sdp = obj["sdp"] else { return nil }
        let type: RTCSdpType = (typeStr == "answer") ? .answer : .offer
        return RTCSessionDescription(type: type, sdp: sdp)
    }
}

private func out(_ s: String) {
    FileHandle.standardOutput.write((s + "\n").data(using: .utf8)!)
}
private func err(_ s: String) {
    FileHandle.standardError.write((s + "\n").data(using: .utf8)!)
}
