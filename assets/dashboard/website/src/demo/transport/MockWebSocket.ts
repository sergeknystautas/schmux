import type { TerminalRecording } from '../recordings/types';

type WSListener = (ev: any) => void;

/**
 * A fake WebSocket that emits scripted data.
 * Implements enough of the WebSocket API for useSessionsWebSocket and TerminalStream.
 */
export class MockDashboardWebSocket {
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;

  readyState = 0; // CONNECTING
  binaryType: BinaryType = 'blob';
  onopen: WSListener | null = null;
  onmessage: WSListener | null = null;
  onclose: WSListener | null = null;
  onerror: WSListener | null = null;
  url: string;

  constructor(url: string) {
    this.url = url;
    // Simulate async open
    setTimeout(() => {
      this.readyState = 1; // OPEN
      this.onopen?.({ type: 'open' });
    }, 50);
  }

  /** Push a JSON message to the consumer (simulates server sending data) */
  pushMessage(data: unknown) {
    if (this.readyState !== 1) return;
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  send(_data: string | ArrayBuffer) {
    // Silently ignore sends (demo doesn't process client messages)
  }

  close() {
    this.readyState = 3; // CLOSED
    this.onclose?.({ code: 1000, reason: 'demo closed', type: 'close' });
  }

  // Stubs for WebSocket API compliance
  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() {
    return false;
  }
}

/**
 * A fake WebSocket for terminal sessions that plays back recorded frames.
 */
export class MockTerminalWebSocket {
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;

  readyState = 0;
  binaryType: BinaryType = 'blob';
  onopen: WSListener | null = null;
  onmessage: WSListener | null = null;
  onclose: WSListener | null = null;
  onerror: WSListener | null = null;
  url: string;

  private timers: ReturnType<typeof setTimeout>[] = [];
  private recording: TerminalRecording | null = null;
  private _paused = false;
  private _currentFrame = 0;

  constructor(url: string) {
    this.url = url;
    setTimeout(() => {
      this.readyState = 1;
      this.onopen?.({ type: 'open' });
    }, 50);
  }

  /** Start playback of a terminal recording */
  startPlayback(recording: TerminalRecording) {
    this.recording = recording;
    this._currentFrame = 0;
    this.playFrom(0);
  }

  private playFrom(frameIndex: number) {
    if (!this.recording || this._paused || this.readyState !== 1) return;

    let cumulativeDelay = 0;
    for (let i = frameIndex; i < this.recording.frames.length; i++) {
      const frame = this.recording.frames[i];
      cumulativeDelay += frame.delay;
      const timer = setTimeout(() => {
        if (this._paused || this.readyState !== 1) return;
        this._currentFrame = i + 1;
        // Send as ArrayBuffer (binary frame) like the real terminal WebSocket
        const encoder = new TextEncoder();
        const buffer = encoder.encode(frame.data).buffer;
        this.onmessage?.({ data: buffer });
      }, cumulativeDelay);
      this.timers.push(timer);
    }
  }

  pause() {
    this._paused = true;
    this.timers.forEach(clearTimeout);
    this.timers = [];
  }

  resume() {
    this._paused = false;
    this.playFrom(this._currentFrame);
  }

  send(_data: string | ArrayBuffer) {
    // Parse resize messages so TerminalStream doesn't error
    // Ignore input messages silently
  }

  close() {
    this.timers.forEach(clearTimeout);
    this.timers = [];
    this.readyState = 3;
    this.onclose?.({ code: 1000, reason: 'demo closed', type: 'close' });
  }

  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() {
    return false;
  }
}
