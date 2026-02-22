const DEFAULT_RING_BUFFER_SIZE = 256 * 1024; // 256KB
const MAX_RECENT_BREAKS = 20;

export type SequenceBreakRecord = {
  frameIndex: number; // which frame (framesReceived at time of break)
  byteOffset: number; // total bytesReceived at time of break
  tail: string; // hex-encoded last ≤16 bytes of the frame (the broken sequence context)
};

export class StreamDiagnostics {
  framesReceived = 0;
  bytesReceived = 0;
  bootstrapCount = 0;
  sequenceBreaks = 0;
  recentBreaks: SequenceBreakRecord[] = [];

  private ringBuffer: Uint8Array;
  private cursor = 0;
  private full = false;

  constructor(ringBufferSize = DEFAULT_RING_BUFFER_SIZE) {
    this.ringBuffer = new Uint8Array(ringBufferSize);
  }

  recordFrame(data: Uint8Array): void {
    this.framesReceived++;
    this.bytesReceived += data.length;
    this.writeToRingBuffer(data);
    this.checkSequenceBreak(data);
  }

  recordBootstrap(): void {
    this.bootstrapCount++;
  }

  ringBufferSnapshot(): Uint8Array {
    if (!this.full) {
      return this.ringBuffer.slice(0, this.cursor);
    }
    const out = new Uint8Array(this.ringBuffer.length);
    const tail = this.ringBuffer.subarray(this.cursor);
    out.set(tail, 0);
    out.set(this.ringBuffer.subarray(0, this.cursor), tail.length);
    return out;
  }

  reset(): void {
    this.framesReceived = 0;
    this.bytesReceived = 0;
    this.bootstrapCount = 0;
    this.sequenceBreaks = 0;
    this.recentBreaks = [];
    this.cursor = 0;
    this.full = false;
  }

  private writeToRingBuffer(data: Uint8Array): void {
    const size = this.ringBuffer.length;
    if (data.length >= size) {
      this.ringBuffer.set(data.subarray(data.length - size));
      this.cursor = 0;
      this.full = true;
      return;
    }
    const end = this.cursor + data.length;
    if (end <= size) {
      this.ringBuffer.set(data, this.cursor);
    } else {
      const first = size - this.cursor;
      this.ringBuffer.set(data.subarray(0, first), this.cursor);
      this.ringBuffer.set(data.subarray(first), 0);
    }
    this.cursor = end % size;
    if (end >= size) {
      this.full = true;
    }
  }

  private checkSequenceBreak(data: Uint8Array): void {
    // Check if frame ends with an incomplete ANSI escape sequence.
    // Look for ESC (\x1b) near the end and parse the CSI structure forward
    // to determine if the sequence is complete.
    const len = data.length;
    if (len === 0) return;

    // Scan backwards from end to find last ESC
    for (let i = len - 1; i >= Math.max(0, len - 16); i--) {
      if (data[i] === 0x1b) {
        // Found ESC — check if sequence is complete
        let pos = i + 1;
        if (pos >= len) {
          // Bare ESC at end of frame
          this.recordBreak(data, i);
          return;
        }
        // CSI: ESC [
        if (data[pos] === 0x5b) {
          pos++;
          // Skip parameter bytes (0x30-0x3F: digits, semicolons, etc.)
          while (pos < len && data[pos] >= 0x30 && data[pos] <= 0x3f) {
            pos++;
          }
          // Skip intermediate bytes (0x20-0x2F: space, !, ", etc.)
          while (pos < len && data[pos] >= 0x20 && data[pos] <= 0x2f) {
            pos++;
          }
          // Check for final byte (0x40-0x7E: @, A-Z, a-z, etc.)
          if (pos >= len || data[pos] < 0x40 || data[pos] > 0x7e) {
            this.recordBreak(data, i);
          }
        }
        // OSC, DCS, or other ESC sequences: not checked (rare in practice)
        return;
      }
    }
  }

  private recordBreak(data: Uint8Array, escPos: number): void {
    this.sequenceBreaks++;
    const tailBytes = data.subarray(escPos);
    const hex = Array.from(tailBytes)
      .map((b) => b.toString(16).padStart(2, '0'))
      .join(' ');
    this.recentBreaks.push({
      frameIndex: this.framesReceived,
      byteOffset: this.bytesReceived,
      tail: hex,
    });
    if (this.recentBreaks.length > MAX_RECENT_BREAKS) {
      this.recentBreaks.shift();
    }
  }
}
