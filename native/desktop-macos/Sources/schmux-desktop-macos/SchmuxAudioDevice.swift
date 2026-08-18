import CoreAudio
import Foundation
import WebRTC

/// A custom `RTCAudioDevice` fed by a PCM ring buffer.
///
/// The capture side (ScreenCaptureKit, or a tone generator for the spike) calls
/// `push(pcm:)` with 48 kHz mono Int16 samples. A 10 ms timer pulls 480 samples
/// per tick and hands them to WebRTC's native ADM via the delegate's
/// `deliverRecordedData`, so the audio rides a real WebRTC send track — without
/// touching the microphone.
///
/// Send-only (recording path): playout is unimplemented because the helper
/// streams app audio *out*; it does not play browser audio back.
final class SchmuxAudioDevice: NSObject, RTCAudioDevice {

    static let sampleRate: Double = 48_000
    static let channels: NSInteger = 1
    static let ioBufferDuration: TimeInterval = 0.01  // 10 ms
    static let framesPerTick: Int = 480               // 48 kHz * 0.01 s
    static let ringCapacity: Int = 48_000 * 2         // 2 s max latency

    // MARK: - Ring buffer (single-producer / single-consumer)
    // NOTE: the plan calls for lock-free. A mutex is correct at the 100 Hz tick
    // rate and far easier to reason about; a lock-free SPSC ring is a drop-in
    // later if profiling ever warrants it.
    private let lock = NSLock()
    private var ring: [Int16]
    private var writePos = 0
    private var readPos = 0
    private var available = 0  // samples available to read

    private var delegate: RTCAudioDeviceDelegate?
    private let tickQueue = DispatchQueue(label: "schmux.audio.adm.tick")
    private var recordTimer: DispatchSourceTimer?
    private var sampleTime: Double = 0

    private var initialized = false
    private var playoutInitialized = false
    private var playing = false
    private var recordingInitialized = false
    private var recording = false

    override init() {
        ring = [Int16](repeating: 0, count: Self.ringCapacity)
        super.init()
    }

    // MARK: - Capture-side entry
    func push(_ pcm: [Int16]) {
        lock.lock()
        defer { lock.unlock() }
        for s in pcm {
            ring[writePos] = s
            writePos = (writePos + 1) % ring.count
            if available < ring.count {
                available += 1
            } else {
                // Overflow: drop the oldest sample to keep latency bounded.
                readPos = (readPos + 1) % ring.count
            }
        }
    }

    private func pull(into out: inout [Int16], count: Int) {
        lock.lock()
        defer { lock.unlock() }
        if available >= count {
            for i in 0..<count {
                out[i] = ring[readPos]
                readPos = (readPos + 1) % ring.count
            }
            available -= count
        } else {
            // Underrun: deliver silence (keeps WebRTC's 10 ms cadence intact).
            for i in 0..<count { out[i] = 0 }
        }
    }

    // MARK: - 10 ms delivery tick
    private func tick() {
        guard let delegate = delegate else { return }
        var samples = [Int16](repeating: 0, count: Self.framesPerTick)
        pull(into: &samples, count: Self.framesPerTick)
        samples.withUnsafeMutableBufferPointer { buf in
            guard let base = buf.baseAddress else { return }
            var flags: AudioUnitRenderActionFlags = []
            var ts = AudioTimeStamp()
            ts.mSampleTime = sampleTime
            ts.mHostTime = mach_absolute_time()
            ts.mFlags = [.sampleTimeValid, .hostTimeValid]
            sampleTime += Double(Self.framesPerTick)

            let audioBuffer = AudioBuffer(
                mNumberChannels: UInt32(Self.channels),
                mDataByteSize: UInt32(Self.framesPerTick * MemoryLayout<Int16>.size),
                mData: UnsafeMutableRawPointer(base))
            var abl = AudioBufferList(mNumberBuffers: 1, mBuffers: (audioBuffer,))
            withUnsafePointer(to: &abl) { ablPtr in
                _ = delegate.deliverRecordedData(
                    &flags, &ts, 0, UInt32(Self.framesPerTick), ablPtr, nil, nil)
            }
        }
    }

    // MARK: - RTCAudioDevice — declared format (kept in sync with AudioDeviceBuffer)
    var deviceInputSampleRate: Double { Self.sampleRate }
    var inputIOBufferDuration: TimeInterval { Self.ioBufferDuration }
    var inputNumberOfChannels: NSInteger { Self.channels }
    var inputLatency: TimeInterval { 0 }
    var deviceOutputSampleRate: Double { Self.sampleRate }
    var outputIOBufferDuration: TimeInterval { Self.ioBufferDuration }
    var outputNumberOfChannels: NSInteger { Self.channels }
    var outputLatency: TimeInterval { 0 }

    var isInitialized: Bool { initialized }
    var isPlayoutInitialized: Bool { playoutInitialized }
    var isPlaying: Bool { playing }
    var isRecordingInitialized: Bool { recordingInitialized }
    var isRecording: Bool { recording }

    func initialize(with delegate: RTCAudioDeviceDelegate) -> Bool {
        self.delegate = delegate
        initialized = true
        // The native ADM sized itself to our declared format (above); nothing
        // else to configure for a buffer-fed device.
        return true
    }

    func terminateDevice() -> Bool {
        delegate = nil
        initialized = false
        return true
    }

    func initializePlayout() -> Bool { playoutInitialized = true; return true }
    func startPlayout() -> Bool { playing = true; return true }
    func stopPlayout() -> Bool { playing = false; return true }

    func initializeRecording() -> Bool { recordingInitialized = true; return true }

    func startRecording() -> Bool {
        guard !recording else { return true }
        recording = true
        sampleTime = 0
        let timer = DispatchSource.makeTimerSource(queue: tickQueue)
        timer.schedule(deadline: .now(), repeating: Self.ioBufferDuration)
        timer.setEventHandler { [weak self] in self?.tick() }
        timer.resume()
        recordTimer = timer
        return true
    }

    func stopRecording() -> Bool {
        recording = false
        recordTimer?.cancel()
        recordTimer = nil
        return true
    }
}
