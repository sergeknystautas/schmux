import { Terminal } from '@xterm/xterm';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { transport } from './transport';
import { inputLatency } from './inputLatency';
import { StreamDiagnostics } from './streamDiagnostics';
import { extractViewportText } from './screenCapture';
import { computeScreenDiff } from './screenDiff';
import { csrfHeaders } from './csrf';
import { stripAnsi } from './ansiStrip';
import { compareScreens } from './syncCompare';
import { buildSurgicalCorrection, buildCursorCorrection } from './surgicalCorrection';

/**
 * Send a clipboard image to the server, which writes it to the system
 * clipboard and triggers Ctrl+V in the tmux session.
 */
async function pasteImageToSession(sessionId: string, imageBlob: Blob): Promise<void> {
  const buf = await imageBlob.arrayBuffer();
  const bytes = new Uint8Array(buf);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  const imageBase64 = btoa(binary);

  const resp = await transport.fetch('/api/clipboard-paste', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ sessionId, imageBase64 }),
  });
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: 'unknown' }));
    console.error('[clipboard-paste] failed:', err);
  }
}

type TerminalStreamOptions = {
  followTail?: boolean;
  followCheckbox?: HTMLInputElement | null;
  onStatusChange?: (status: 'connected' | 'disconnected' | 'reconnecting' | 'error') => void;
  onResume?: (showing: boolean) => void;
  onSelectedLinesChange?: (lines: string[]) => void;
};

type SelectedLine = {
  bufferLine: number;
  markerId: number;
  text: string;
};

export default class TerminalStream {
  sessionId: string;
  containerElement: HTMLElement;
  ws: WebSocket | null;
  connected: boolean;
  followTail: boolean;
  followCheckbox: HTMLInputElement | null;
  onStatusChange: (status: 'connected' | 'disconnected' | 'reconnecting' | 'error') => void;
  onResume: (showing: boolean) => void;
  terminal: Terminal | null;
  tmuxCols: number | null;
  tmuxRows: number | null;
  baseFontSize: number;
  initialized: Promise<Terminal | null>;
  resizeDebounceTimer: ReturnType<typeof setTimeout> | null;

  // WebSocket reconnection state
  private reconnectAttempt = 0;

  // Scroll suppression: when true, scroll events from xterm.js viewport are
  // ignored because they were caused by terminal.write() processing escape
  // sequences (e.g., cursor repositioning), not by user interaction.
  private writingToTerminal = false;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private maxReconnectAttempt = 10;
  private disposed = false;
  private wasDisplaced = false;
  private bootstrapped = false;
  private bootstrapComplete = false;
  private utf8Decoder = new TextDecoder();
  private lastBinaryTime = 0;
  private lastReceivedSeq: bigint = -1n;

  // Gap request debouncing: prevents redundant replay requests when multiple
  // frames arrive with gaps before the replay response fills them.
  private gapRequestPending = false;

  // Coalesced scroll: when true, a requestAnimationFrame is already queued
  // that will call scrollToBottom(). Additional writes skip scheduling to
  // avoid redundant layout reflows — one scroll per frame is sufficient.
  private scrollRAFPending = false;

  // ResizeObserver cleanup references
  private resizeObserver: ResizeObserver | null = null;
  private windowResizeHandler: (() => void) | null = null;

  // Scroll listener cleanup
  private scrollHandler: (() => void) | null = null;
  private scrollViewport: Element | null = null;

  // Multi-line selection state
  selectionMode: boolean;
  selectedLines: Map<number, SelectedLine>;
  onSelectedLinesChange: (lines: string[]) => void;
  clickHandler: ((event: Event) => void) | null;
  mouseMoveHandler: ((event: Event) => void) | null;
  mouseUpHandler: ((event: Event) => void) | null;
  isDragging: boolean;
  dragStartLine: number | null;
  dragIsSelecting: boolean;

  // Clipboard image paste interception
  private imagePasteHandler: ((e: Event) => void) | null = null;

  // Terminal recreation count — set by SessionDetailPage, read during diagnostic capture
  recreationCount = 0;

  // Control mode health
  onControlModeChange: ((attached: boolean) => void) | null = null;

  // Diagnostics
  diagnostics: StreamDiagnostics | null = null;
  latestStats: Record<string, unknown> | null = null;
  onStatsUpdate: ((stats: Record<string, unknown>) => void) | null = null;
  onDiagnosticResponse: ((msg: Record<string, unknown>) => void) | null = null;
  onDiagnosticComplete:
    | ((result: { diagDir: string; verdict: string; findings: string[] }) => void)
    | null = null;
  onSyncCorrection: ((diffRows: number[]) => void) | null = null;

  lifecycleLogging = false;

  private tsLog(event: string, detail?: Record<string, unknown>) {
    if (!this.lifecycleLogging) return;
    const ts = new Date().toISOString().slice(11, 23);
    const extra = detail ? ' ' + JSON.stringify(detail) : '';
    console.log(`[TerminalStream ${ts}] ${event}${extra}`);
  }

  // IO workspace telemetry
  latestIOWorkspaceStats: Record<string, unknown> | null = null;
  onIOWorkspaceStatsUpdate: ((stats: Record<string, unknown>) => void) | null = null;
  onIOWorkspaceDiagnosticComplete:
    | ((result: { diagDir: string; verdict: string; findings: string[] }) => void)
    | null = null;

  constructor(
    sessionId: string,
    containerElement: HTMLElement,
    options: TerminalStreamOptions = {}
  ) {
    this.sessionId = sessionId;
    this.containerElement = containerElement;
    this.ws = null;
    this.connected = false;
    this.followTail = options.followTail !== false;
    this.followCheckbox = options.followCheckbox || null;
    this.onStatusChange = options.onStatusChange || (() => {});
    this.onResume = options.onResume || (() => {});
    this.onSelectedLinesChange = options.onSelectedLinesChange || (() => {});

    this.terminal = null;
    this.tmuxCols = null;
    this.tmuxRows = null;
    this.baseFontSize = 14;
    this.resizeDebounceTimer = null;

    // Multi-line selection state
    this.selectionMode = false;
    this.selectedLines = new Map();
    this.clickHandler = null;
    this.mouseMoveHandler = null;
    this.mouseUpHandler = null;
    this.isDragging = false;
    this.dragStartLine = null;
    this.dragIsSelecting = true;

    this.tsLog('constructor', { sessionId });
    this.initialized = this.initTerminal();
  }

  async initTerminal(): Promise<Terminal | null> {
    if (!this.containerElement) {
      return null;
    }

    // Calculate initial dimensions from container with reasonable defaults
    const containerRect = this.containerElement.getBoundingClientRect();
    let cols = 120; // Default width
    let rows = 40; // Default height

    // If container has valid dimensions, calculate from it
    // Use reasonable cell size estimates for initial render
    if (containerRect.width > 0 && containerRect.height > 0) {
      const estimatedCellWidth = 9;
      const estimatedCellHeight = 17;
      cols = Math.max(20, Math.floor(containerRect.width / estimatedCellWidth) - 1);
      rows = Math.max(5, Math.floor(containerRect.height / estimatedCellHeight) - 1);
    }

    this.tmuxCols = cols;
    this.tmuxRows = rows;
    this.terminal = new Terminal({
      cols,
      rows,
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      macOptionIsMeta: true,
      allowProposedApi: true, // Required for registerDecoration API
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#d4d4d4',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#ffffff',
      },
      scrollback: 5000,
      convertEol: true,
    });

    this.terminal.loadAddon(new WebLinksAddon());
    this.terminal.loadAddon(new Unicode11Addon());
    this.terminal.unicode.activeVersion = '11';
    this.tsLog('initTerminal', { cols, rows });
    this.terminal.open(this.containerElement);
    // xterm.js doesn't reliably emit Alt/Option+Enter through onData on macOS,
    // but Codex uses Meta/Alt+Enter for inserting a blank line. Map it
    // explicitly to the common terminal encoding: ESC + CR.
    (
      this.terminal as Terminal & {
        attachCustomKeyEventHandler?: (handler: (e: KeyboardEvent) => boolean) => void;
      }
    ).attachCustomKeyEventHandler?.((e: KeyboardEvent) => {
      if (
        e.type === 'keydown' &&
        e.altKey &&
        (e.key === 'Enter' || e.code === 'Enter' || e.code === 'NumpadEnter')
      ) {
        e.preventDefault();
        this.sendInput('\x1b\r');
        return false;
      }
      if (e.type === 'keydown' && e.altKey && (e.key === 'Backspace' || e.key === 'Delete')) {
        e.preventDefault();
        this.sendInput('\x1b\x7f');
        return false;
      }
      return true;
    });
    this.terminal.onData((data) => {
      this.sendInput(data);
    });

    // Intercept paste events: if the clipboard contains an image, upload it
    // to the server (which writes to system clipboard + sends Ctrl+V) instead
    // of letting xterm.js handle it as a text paste.
    this.imagePasteHandler = (e: Event) => {
      const ce = e as ClipboardEvent;
      const items = ce.clipboardData?.items;
      if (!items) return;
      for (const item of Array.from(items)) {
        if (item.type.startsWith('image/')) {
          e.preventDefault();
          e.stopPropagation();
          const blob = item.getAsFile();
          if (blob) {
            pasteImageToSession(this.sessionId, blob);
          }
          return;
        }
      }
    };
    document.addEventListener('paste', this.imagePasteHandler, { capture: true });

    this._attachScrollListener();
    this.setupResizeHandler();

    // Immediately calculate accurate dimensions now that terminal is rendered
    // This ensures we have correct dimensions before WebSocket connects
    this.fitTerminalSync();

    // Expose for Playwright fidelity tests. Gated to dev mode and scenario
    // test builds (VITE_EXPOSE_TERMINAL) to avoid leaking the terminal object
    // in production, where XSS could abuse it to send input to tmux sessions.
    if (
      typeof window !== 'undefined' &&
      (import.meta.env.DEV || import.meta.env.VITE_EXPOSE_TERMINAL)
    ) {
      window.__schmuxTerminal = this.terminal;
      window.__schmuxStream = this;
      this.enableDiagnostics();
    }

    return this.terminal;
  }

  _attachScrollListener() {
    const tryAttach = (attempts = 0) => {
      const viewport = this.terminal?.element?.querySelector('.xterm-viewport');
      if (viewport) {
        this.scrollHandler = () => {
          this.handleUserScroll();
        };
        this.scrollViewport = viewport;
        viewport.addEventListener('scroll', this.scrollHandler);
      } else if (attempts < 10) {
        setTimeout(() => tryAttach(attempts + 1), 50 * (attempts + 1));
      }
    };
    tryAttach();
  }

  setupResizeHandler() {
    if (typeof ResizeObserver !== 'undefined') {
      const resizeObserver = new ResizeObserver(() => {
        this.handleResize();
      });
      resizeObserver.observe(this.containerElement);

      // Also watch the nearest layout ancestor to detect viewport changes
      // that don't directly resize our container (e.g. sidebar collapse).
      const layoutParent =
        this.containerElement.closest('.session-detail') || this.containerElement.parentElement;
      if (layoutParent && layoutParent !== this.containerElement) {
        resizeObserver.observe(layoutParent);
      }

      this.resizeObserver = resizeObserver;
    }

    const handleResize = () => {
      this.handleResize();
    };
    this.windowResizeHandler = handleResize;
    window.addEventListener('resize', handleResize);
  }

  handleResize() {
    // Debounce resize events to avoid excessive backend calls
    if (this.resizeDebounceTimer) {
      clearTimeout(this.resizeDebounceTimer);
    }
    this.resizeDebounceTimer = setTimeout(() => {
      this.fitTerminal();
    }, 300);
  }

  // Central measurement function - calculates terminal dimensions from the rendered terminal
  // Returns { cols, rows, cellWidth, cellHeight, containerWidth, containerHeight } or null if measurement fails
  measureTerminal(): {
    cols: number;
    rows: number;
    cellWidth: number;
    cellHeight: number;
    containerWidth: number;
    containerHeight: number;
  } | null {
    if (!this.terminal) return null;

    const containerRect = this.containerElement.getBoundingClientRect();
    if (!containerRect) return null;

    const containerWidth = containerRect.width;
    const containerHeight = containerRect.height;

    if (containerWidth <= 0 || containerHeight <= 0) return null;

    // Measure actual cell dimensions from the terminal
    const core = (
      this.terminal as unknown as {
        _core: {
          _renderService: { dimensions: { css: { cell: { width: number; height: number } } } };
        };
      }
    )._core;
    let cellWidth = 9;
    let cellHeight = 17;

    if (core?._renderService?.dimensions?.css?.cell) {
      cellWidth = core._renderService.dimensions.css.cell.width;
      cellHeight = core._renderService.dimensions.css.cell.height;
    }

    const cols = Math.max(20, Math.floor(containerWidth / cellWidth) - 3);
    const rows = Math.max(5, Math.floor(containerHeight / cellHeight) - 1);

    return { cols, rows, cellWidth, cellHeight, containerWidth, containerHeight };
  }

  // Synchronous resize - calculates and applies dimensions immediately
  // Used during initialization to ensure terminal is sized before WebSocket connects
  fitTerminalSync() {
    const measured = this.measureTerminal();
    if (!measured) return;

    const { cols, rows } = measured;

    // Update stored dimensions
    this.tmuxCols = cols;
    this.tmuxRows = rows;

    // Suppress scroll events during resize — same pattern as fitTerminal().
    // Without this, xterm.js buffer adjustments fire DOM scroll events that
    // falsely disable followTail (the user didn't scroll, the layout changed).
    // Note: uses an independent rAF (not the shared scrollRAFPending mechanism)
    // because this runs at init time before any WebSocket writes are possible,
    // so the resize/write rAF race that affects fitTerminal cannot occur here.
    this.writingToTerminal = true;
    this.terminal!.resize(cols, rows);
    if (this.followTail) {
      this.terminal!.scrollToBottom();
    }
    requestAnimationFrame(() => {
      this.writingToTerminal = false;
    });

    // Note: Don't send resize to backend here - WebSocket isn't connected yet
    // The connect() method will send resize on open
  }

  fitTerminal() {
    const measured = this.measureTerminal();
    if (!measured) return;

    this.tsLog('fitTerminal', {
      cols: measured.cols,
      rows: measured.rows,
      prevCols: this.tmuxCols,
      prevRows: this.tmuxRows,
    });

    if (this.diagnostics) {
      this.diagnostics.resizeCount++;
      this.diagnostics.lastResizeTs = Date.now();
    }

    const { cols, rows } = measured;

    // Update stored dimensions
    this.tmuxCols = cols;
    this.tmuxRows = rows;

    // Suppress scroll events during resize — same pattern as writeTerminal().
    // Without this, xterm.js buffer adjustments fire DOM scroll events that
    // falsely disable followTail (the user didn't scroll, the layout changed).
    //
    // Use the shared scrollRAFPending mechanism instead of an independent rAF.
    // An independent rAF races writeTerminal's pending rAF: whichever fires
    // first clears writingToTerminal, leaving a window where scroll events
    // slip through while the other rAF is still queued.
    this.writingToTerminal = true;
    this.terminal!.resize(cols, rows);
    if (this.followTail) {
      this.terminal!.scrollToBottom();
    }
    if (!this.scrollRAFPending) {
      this.scrollRAFPending = true;
      requestAnimationFrame(() => {
        this.scrollRAFPending = false;
        this.writingToTerminal = false;
      });
    }
    // If scrollRAFPending is already set, the pending writeTerminal rAF will
    // clear both flags. scrollToBottom() was already called synchronously above.

    // Send resize message to backend to resize tmux
    this.sendResize(cols, rows);
  }

  sendResize(cols: number, rows: number) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(
        JSON.stringify({
          type: 'resize',
          data: JSON.stringify({ cols, rows }),
        })
      );
    }
  }

  resizeTerminal() {
    this.fitTerminal();
  }

  connect() {
    if (!this.terminal || this.disposed) return;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/terminal/${this.sessionId}`;
    this.tsLog('connect', { attempt: this.reconnectAttempt, bootstrapped: this.bootstrapped });

    this.ws = transport.createWebSocket(wsUrl);
    this.ws.binaryType = 'arraybuffer';

    // Reset state for new connection attempt
    this.wasDisplaced = false;
    this.bootstrapped = false;
    this.bootstrapComplete = false;
    this.lastReceivedSeq = -1n;
    this.gapRequestPending = false;
    this.scrollRAFPending = false;
    this.utf8Decoder = new TextDecoder();

    this.ws.onopen = () => {
      this.connected = true;
      this.reconnectAttempt = 0;
      this.tsLog('ws.onopen');
      this.onStatusChange('connected');

      // Send resize immediately on connect so backend knows correct dimensions
      // before streaming content
      if (this.tmuxCols && this.tmuxRows) {
        this.sendResize(this.tmuxCols, this.tmuxRows);
      }
    };

    this.ws.onmessage = (event) => {
      if (this.terminal) {
        this.handleOutput(event.data);
      }
    };

    this.ws.onclose = (ev) => {
      this.connected = false;
      this.tsLog('ws.onclose', {
        code: ev.code,
        reason: ev.reason,
        wasDisplaced: this.wasDisplaced,
        disposed: this.disposed,
      });
      if (this.disposed) return;

      // If we were intentionally displaced (another window opened), don't retry
      if (this.wasDisplaced) {
        this.onStatusChange('disconnected');
        return;
      }

      this.onStatusChange('disconnected');
      if (this.reconnectAttempt < this.maxReconnectAttempt) {
        const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempt), 30000);
        this.reconnectAttempt++;
        this.tsLog('reconnect.scheduled', { attempt: this.reconnectAttempt, delayMs: delay });
        if (this.terminal) {
          this.terminal.writeln(
            `\r\n\x1b[33m[Connection lost, reconnecting in ${delay / 1000}s...]\x1b[0m`
          );
        }
        this.reconnectTimer = setTimeout(() => {
          this.connect();
        }, delay);
      } else {
        if (this.terminal) {
          this.terminal.writeln('\r\n\x1b[31m[Connection lost. Refresh to reconnect.]\x1b[0m');
        }
      }
    };

    this.ws.onerror = (error) => {
      this.tsLog('ws.onerror');
      console.error('WebSocket error:', error);
      if (this.terminal) {
        this.terminal.writeln('\x1b[91mWebSocket error\x1b[0m');
      }
      this.onStatusChange('error');
    };
  }

  disconnect() {
    this.tsLog('disconnect');
    this.disposed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.resizeObserver) {
      this.resizeObserver.disconnect();
      this.resizeObserver = null;
    }
    if (this.windowResizeHandler) {
      window.removeEventListener('resize', this.windowResizeHandler);
      this.windowResizeHandler = null;
    }
    if (this.scrollHandler && this.scrollViewport) {
      this.scrollViewport.removeEventListener('scroll', this.scrollHandler);
      this.scrollHandler = null;
      this.scrollViewport = null;
    }
    if (this.imagePasteHandler) {
      document.removeEventListener('paste', this.imagePasteHandler, { capture: true });
      this.imagePasteHandler = null;
    }
    // Dispose terminal before closing WebSocket so the onmessage guard
    // (if (this.terminal)) prevents writes to a disposed terminal.
    if (this.terminal) {
      this.terminal.dispose();
      this.terminal = null;
    }
    if (this.ws) {
      this.ws.close();
    }
  }

  focus() {
    this.terminal?.focus();
  }

  sendInput(data: string) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      inputLatency.markSent();
      this.ws.send(new TextEncoder().encode(data));
    }
  }

  enableDiagnostics(): void {
    if (!this.diagnostics) {
      this.diagnostics = new StreamDiagnostics();
    }
    // Set up the diagnostic response handler for the full capture flow
    this.onDiagnosticResponse = (msg: Record<string, unknown>) => {
      if (!this.terminal) return;
      const diagDir = (msg.diagDir as string) || '';
      const verdict = (msg.verdict as string) || '';
      const findings = (msg.findings as string[]) || [];
      // 1. Extract xterm.js visible viewport (matches tmux capture-pane's visible screen)
      const xtermScreen = extractViewportText(this.terminal.buffer.active, this.terminal.rows);
      // 2. Compute diff against tmux screen from backend response
      const diff = computeScreenDiff((msg.tmuxScreen as string) || '', xtermScreen);
      // 3. Get frontend ring buffer snapshot
      const frontendRingBuffer = this.diagnostics
        ? new TextDecoder().decode(this.diagnostics.ringBufferSnapshot())
        : '';
      const scrollSnapshot = this.diagnostics?.scrollSnapshot();
      // 4. Post frontend files back to backend to append to diagnostic dir
      transport
        .fetch('/api/dev/diagnostic-append', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
          body: JSON.stringify({
            diagDir,
            xtermScreen,
            screenDiff: diff.diffText,
            ringBufferFrontend: frontendRingBuffer,
            gapStats: this.diagnostics ? JSON.stringify(this.diagnostics.gapSnapshot()) : null,
            cursorXterm: JSON.stringify({
              x: this.terminal.buffer.active.cursorX,
              y: this.terminal.buffer.active.cursorY,
            }),
            scrollEvents: scrollSnapshot ? JSON.stringify(scrollSnapshot.events) : null,
            scrollStats: scrollSnapshot
              ? JSON.stringify({
                  ...scrollSnapshot.counters,
                  recreationCount: this.recreationCount,
                })
              : null,
          }),
        })
        .then(() => {
          this.onDiagnosticComplete?.({ diagDir, verdict, findings });
        })
        .catch((err) => {
          console.error('[diagnostic] append failed:', err);
          // Still notify completion with what we have from the backend
          this.onDiagnosticComplete?.({ diagDir, verdict, findings });
        });
    };
  }

  disableDiagnostics(): void {
    this.diagnostics = null;
    this.onStatsUpdate = null;
    this.onDiagnosticResponse = null;
    this.onDiagnosticComplete = null;
    this.latestStats = null;
    this.onIOWorkspaceStatsUpdate = null;
    this.onIOWorkspaceDiagnosticComplete = null;
    this.latestIOWorkspaceStats = null;
  }

  sendDiagnostic(): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type: 'diagnostic' }));
    }
  }

  sendIOWorkspaceDiagnostic(): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type: 'io-workspace-diagnostic' }));
    }
  }

  handleOutput(data: string | ArrayBuffer) {
    const renderStart = performance.now();

    // Binary frame: raw terminal bytes (first = bootstrap, subsequent = append)
    // Use streaming decode to handle UTF-8 characters split across frames
    if (data instanceof ArrayBuffer) {
      if (this.diagnostics) {
        this.diagnostics.recordFrame(new Uint8Array(data));
        if (!this.bootstrapped) {
          this.diagnostics.recordBootstrap();
        }
      }

      // Parse 8-byte sequence header
      const view = new DataView(data);
      const seq = view.getBigUint64(0, false); // big-endian

      // Dedup: skip frames already processed (replay overlap).
      // When gap replay frames arrive, they may include frames we already
      // received. Writing them again would cause duplicate terminal output.
      if (this.bootstrapComplete && seq <= this.lastReceivedSeq) {
        this.tsLog('gap.deduped', {
          seq: seq.toString(),
          lastReceived: this.lastReceivedSeq.toString(),
        });
        if (this.diagnostics) this.diagnostics.gapFramesDeduped++;
        return;
      }

      // Detect sequence gap (only after bootstrap is fully complete)
      if (this.bootstrapComplete && this.lastReceivedSeq >= 0n) {
        const expectedSeq = this.lastReceivedSeq + 1n;
        if (seq > expectedSeq) {
          this.tsLog('gap.detected', {
            expected: expectedSeq.toString(),
            got: seq.toString(),
            missed: (seq - expectedSeq).toString(),
          });
          if (this.diagnostics) this.diagnostics.gapsDetected++;
          this.sendGapRequest(expectedSeq);
        }
      }

      // Clear gap pending flag when we receive sequential data
      // (the gap has been filled by replay)
      if (
        this.gapRequestPending &&
        this.lastReceivedSeq >= 0n &&
        seq === this.lastReceivedSeq + 1n
      ) {
        this.gapRequestPending = false;
      }
      this.lastReceivedSeq = seq;
      if (this.diagnostics) this.diagnostics.lastReceivedSeq = seq;

      // Terminal data starts after the 8-byte header
      const terminalData = new Uint8Array(data, 8);

      // Track seq-only frames (empty terminal data from escbuf holdback)
      if (this.diagnostics && terminalData.length === 0) {
        this.diagnostics.emptySeqFrames++;
      }

      // Track replay frames that passed dedup: a gap request is pending and
      // this frame jumped ahead (seq > expected). If this counter is ever
      // non-zero after the fixes, the replay sent data that may overlap with
      // already-delivered live frames — the exact scenario that caused the
      // original cursor-jump desync.
      if (
        this.diagnostics &&
        this.gapRequestPending &&
        this.bootstrapComplete &&
        seq > this.lastReceivedSeq + 1n
      ) {
        this.diagnostics.gapReplayWritten++;
      }

      const text = this.utf8Decoder.decode(terminalData, { stream: true });
      this.lastBinaryTime = Date.now();
      if (!this.bootstrapped) {
        this.bootstrapped = true;
        this.tsLog('bootstrap.reset', { dataLen: text.length, seq: seq.toString() });
        this.terminal!.reset();
        this.writeTerminal(text);
      } else {
        inputLatency.recordFrameProcessed();
        inputLatency.markReceived();
        this.writeTerminal(text, () => {
          inputLatency.markRenderTime(performance.now() - renderStart);
        });
        inputLatency.recordHandleOutputTime(performance.now() - renderStart);
      }
      return;
    }

    // Text frame: JSON control messages (displaced, legacy fallback)
    let msg: Record<string, unknown>;
    try {
      msg = JSON.parse(data);
    } catch {
      this.writeTerminal(data);
      return;
    }

    switch (msg.type) {
      case 'displaced':
        this.tsLog('displaced');
        this.wasDisplaced = true;
        this.terminal!.writeln(
          `\r\n\x1b[33m${msg.content}\x1b[0m\r\n\x1b[33m[Refresh to reconnect]\x1b[0m`
        );
        break;
      case 'bootstrapComplete':
        this.bootstrapComplete = true;
        this.tsLog('bootstrapComplete');
        break;
      case 'stats':
        this.latestStats = msg;
        if (msg.inputLatency) {
          inputLatency.updateServerLatency(
            msg.inputLatency as Parameters<typeof inputLatency.updateServerLatency>[0]
          );
        }
        this.onStatsUpdate?.(msg);
        break;
      case 'inputEcho':
        inputLatency.recordServerSegments({
          dispatch: (msg.dispatchMs as number) ?? 0,
          sendKeys: (msg.sendKeysMs as number) ?? 0,
          echo: (msg.echoMs as number) ?? 0,
          frameSend: (msg.frameSendMs as number) ?? 0,
          total: (msg.serverMs as number) ?? 0,
        });
        break;
      case 'diagnostic':
        this.onDiagnosticResponse?.(msg);
        break;
      case 'io-workspace-stats':
        this.latestIOWorkspaceStats = msg;
        this.onIOWorkspaceStatsUpdate?.(msg);
        break;
      case 'io-workspace-diagnostic':
        this.onIOWorkspaceDiagnosticComplete?.({
          diagDir: (msg.diagDir as string) || '',
          verdict: (msg.verdict as string) || '',
          findings: (msg.findings as string[]) || [],
        });
        break;
      case 'controlMode':
        this.tsLog('controlMode', { attached: msg.attached });
        this.onControlModeChange?.(msg.attached as boolean);
        break;
      case 'sync':
        this.tsLog('sync.received', { forced: (msg as Record<string, unknown>).forced });
        this.handleSync(
          msg as {
            screen: string;
            cursor: { row: number; col: number; visible: boolean };
            forced?: boolean;
          }
        );
        break;
      default:
        if (msg.content) {
          this.writeTerminal(msg.content as string);
        }
    }
  }

  private handleSync(msg: {
    screen: string;
    cursor: { row: number; col: number; visible: boolean };
    forced?: boolean;
  }) {
    if (!this.terminal) return;

    // Activity guard: skip if binary data arrived within 2s
    // Bypass when forced (drops detected — correction is critical)
    if (!msg.forced && Date.now() - this.lastBinaryTime < 2000) {
      this.tsLog('sync.skipped', {
        reason: 'recentBinaryData',
        ageMs: Date.now() - this.lastBinaryTime,
      });
      this.sendSyncResult(false, []);
      return;
    }

    // Extract xterm.js visible text
    const buffer = this.terminal.buffer.active;
    const xtermLines: string[] = [];
    const start = buffer.baseY;
    for (let y = start; y < start + this.terminal.rows && y < buffer.length; y++) {
      const line = buffer.getLine(y);
      xtermLines.push(line ? line.translateToString(true).trimEnd() : '');
    }

    // Extract sync text (strip ANSI, split into lines)
    const syncScreenLines = msg.screen.split('\n');
    const syncLines = syncScreenLines.map((line) => stripAnsi(line).trimEnd());

    // Compare
    const result = compareScreens(xtermLines, syncLines);

    if (result.skip) {
      // Dimension mismatch (resize race), skip
      return;
    }

    if (!result.match) {
      this.tsLog('sync.correction', {
        diffRowCount: result.diffRows.length,
        diffRows: result.diffRows,
      });
      const rowContents = result.diffRows.map((i) => syncScreenLines[i] ?? '');
      const correction = buildSurgicalCorrection(result.diffRows, rowContents, msg.cursor);
      this.writeTerminal(correction);

      this.onSyncCorrection?.(result.diffRows);
      this.sendSyncResult(true, result.diffRows);
    } else {
      // Content matches — check cursor position
      const xtermCursor = { row: buffer.cursorY, col: buffer.cursorX };
      const cursorCorrection = buildCursorCorrection(msg.cursor, xtermCursor);
      if (cursorCorrection) {
        this.writeTerminal(cursorCorrection);
        this.sendSyncResult(true, []);
      } else {
        this.sendSyncResult(false, []);
      }
    }
  }

  private sendSyncResult(corrected: boolean, diffRows: number[]) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(
        JSON.stringify({
          type: 'syncResult',
          data: JSON.stringify({ corrected, diffRows }),
        })
      );
    }
  }

  private sendGapRequest(fromSeq: bigint) {
    // Debounce: if a gap request is already pending, don't send another.
    // Multiple frames with gaps would otherwise fire redundant replay requests.
    if (this.gapRequestPending) return;
    this.gapRequestPending = true;

    if (this.diagnostics) this.diagnostics.gapRequestsSent++;

    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(
        JSON.stringify({
          type: 'gap',
          data: JSON.stringify({ fromSeq: fromSeq.toString() }),
        })
      );
    }
  }

  setFollow(follow: boolean, trigger?: 'userScroll' | 'jumpToBottom') {
    const before = this.followTail;
    this.followTail = follow;
    if (this.followCheckbox) this.followCheckbox.checked = follow;
    this.onResume(!follow);
    if (this.diagnostics && before !== follow) {
      this.diagnostics.recordScrollEvent({
        ts: Date.now(),
        trigger,
        followBefore: before,
        followAfter: follow,
        writingToTerminal: this.writingToTerminal,
        scrollRAFPending: this.scrollRAFPending,
        viewportY: this.terminal?.buffer.active.viewportY ?? -1,
        baseY: this.terminal?.buffer.active.baseY ?? -1,
        lastReceivedSeq: this.lastReceivedSeq.toString(),
      });
    }
  }

  isAtBottom(threshold = 0): boolean {
    if (!this.terminal) return true;
    const buffer = this.terminal.buffer.active;
    return buffer.viewportY >= buffer.baseY - threshold;
  }

  // Wrapper around terminal.write that suppresses scroll events during processing
  // and coalesces scrollToBottom() to once per animation frame.
  //
  // Without scroll suppression, xterm.js cursor repositioning (e.g., TUI redraws)
  // triggers the scroll listener, falsely disabling followTail.
  //
  // Without coalescing, each write callback calls scrollToBottom() which forces a
  // synchronous layout reflow (~4-8ms). Under burst output (N events per frame),
  // this causes N forced reflows, blocking the main thread and delaying keystroke
  // echo processing. Deferring to rAF collapses N reflows into one natural reflow.
  private writeTerminal(data: string, cb?: () => void) {
    this.writingToTerminal = true;
    this.terminal!.write(data, () => {
      cb?.();
      if (!this.scrollRAFPending) {
        this.scrollRAFPending = true;
        requestAnimationFrame(() => {
          if (this.followTail) {
            this.terminal!.scrollToBottom();
          }
          this.scrollRAFPending = false;
          this.writingToTerminal = false;
        });
      } else {
        if (this.diagnostics) this.diagnostics.scrollCoalesceHits++;
      }
    });
  }

  handleUserScroll() {
    if (!this.terminal) return;
    if (this.writingToTerminal || this.scrollRAFPending) {
      if (this.diagnostics) this.diagnostics.scrollSuppressedCount++;
      return;
    }
    this.setFollow(this.isAtBottom(1), 'userScroll');
  }

  jumpToBottom() {
    if (this.terminal) {
      this.terminal.scrollToBottom();
      this.setFollow(true, 'jumpToBottom');
    }
  }

  downloadOutput() {
    if (!this.terminal) return;

    const buffer = this.terminal.buffer.active;
    const lines = [];
    for (let i = 0; i < buffer.length; i++) {
      const line = buffer.getLine(i);
      if (line) {
        lines.push(line.translateToString());
      }
    }

    const content = lines.join('\n');
    const blob = new Blob([content], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `session-${this.sessionId}.log`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  // Multi-line selection methods

  toggleSelectionMode() {
    this.selectionMode = !this.selectionMode;
    if (this.selectionMode) {
      this.attachClickHandler();
    } else {
      this.detachClickHandler();
      this.clearSelection();
    }
    return this.selectionMode;
  }

  getSelectedLines(): string[] {
    return Array.from(this.selectedLines.values()).map((sl) => sl.text);
  }

  clearSelection() {
    this.clearSelectionMarkers();
    this.notifySelectedLinesChange();
  }

  private clearSelectionMarkers() {
    if (!this.terminal) return;
    for (const selected of this.selectedLines.values()) {
      const marker = this.terminal.markers.find((m) => m.id === selected.markerId);
      if (marker) {
        marker.dispose();
      }
    }
    this.selectedLines.clear();
  }

  private attachClickHandler() {
    if (!this.terminal?.element || this.clickHandler) return;

    this.clickHandler = (event: Event) => {
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
      this.handleMouseDown(event as MouseEvent);
    };

    this.mouseMoveHandler = (event: Event) => {
      this.handleMouseMove(event as MouseEvent);
    };

    this.mouseUpHandler = (event: Event) => {
      this.handleMouseUp(event as MouseEvent);
    };

    this.terminal.element.addEventListener('mousedown', this.clickHandler, { capture: true });
    document.addEventListener('mousemove', this.mouseMoveHandler);
    document.addEventListener('mouseup', this.mouseUpHandler);
    this.terminal.element.style.cursor = 'pointer';
  }

  private detachClickHandler() {
    if (!this.terminal?.element) return;

    if (this.clickHandler) {
      this.terminal.element.removeEventListener('mousedown', this.clickHandler, { capture: true });
      this.clickHandler = null;
    }
    if (this.mouseMoveHandler) {
      document.removeEventListener('mousemove', this.mouseMoveHandler);
      this.mouseMoveHandler = null;
    }
    if (this.mouseUpHandler) {
      document.removeEventListener('mouseup', this.mouseUpHandler);
      this.mouseUpHandler = null;
    }
    this.terminal.element.style.cursor = '';
    this.isDragging = false;
    this.dragStartLine = null;
  }

  private getBufferLineFromEvent(event: MouseEvent): number | null {
    if (!this.terminal) return null;

    const screenElement = this.terminal.element?.querySelector('.xterm-screen');
    if (!screenElement) return null;

    const rect = screenElement.getBoundingClientRect();
    const y = event.clientY - rect.top;
    const cellHeight = rect.height / this.terminal.rows;
    const row = Math.floor(y / cellHeight);

    const buffer = this.terminal.buffer.active;
    const bufferLine = buffer.viewportY + row;

    if (bufferLine < 0 || bufferLine >= buffer.length) return null;
    return bufferLine;
  }

  private handleMouseDown(event: MouseEvent) {
    if (!this.terminal || !this.selectionMode) return;

    const bufferLine = this.getBufferLineFromEvent(event);
    if (bufferLine === null) return;

    this.isDragging = true;
    this.dragStartLine = bufferLine;
    // If starting on selected line, drag will deselect. Otherwise select.
    this.dragIsSelecting = !this.selectedLines.has(bufferLine);

    // Apply action to the first line immediately
    if (this.dragIsSelecting) {
      this.selectLine(bufferLine);
    } else {
      this.deselectLine(bufferLine);
    }
  }

  private handleMouseMove(event: MouseEvent) {
    if (!this.isDragging || this.dragStartLine === null) return;

    const bufferLine = this.getBufferLineFromEvent(event);
    if (bufferLine === null) return;

    const startLine = Math.min(this.dragStartLine, bufferLine);
    const endLine = Math.max(this.dragStartLine, bufferLine);

    for (let line = startLine; line <= endLine; line++) {
      if (this.dragIsSelecting) {
        this.selectLine(line);
      } else {
        this.deselectLine(line);
      }
    }
  }

  private handleMouseUp(_event: MouseEvent) {
    this.isDragging = false;
    this.dragStartLine = null;
  }

  private deselectLine(bufferLine: number) {
    if (!this.terminal) return;
    if (!this.selectedLines.has(bufferLine)) return;

    const selected = this.selectedLines.get(bufferLine);
    if (selected) {
      const marker = this.terminal.markers.find((m) => m.id === selected.markerId);
      if (marker) {
        marker.dispose();
      }
    }
    this.selectedLines.delete(bufferLine);
    this.notifySelectedLinesChange();
  }

  private selectLine(bufferLine: number) {
    if (!this.terminal) return;
    if (this.selectedLines.has(bufferLine)) return;

    const buffer = this.terminal.buffer.active;
    const line = buffer.getLine(bufferLine);
    if (!line) return;

    const lineText = line.translateToString().trim();
    const cursorBufferLine = buffer.baseY + buffer.cursorY;
    const markerOffset = bufferLine - cursorBufferLine;

    const marker = this.terminal.registerMarker(markerOffset);
    if (!marker) return;

    const screenElement = this.terminal.element?.querySelector('.xterm-screen');
    const cellWidth = screenElement
      ? screenElement.getBoundingClientRect().width / this.terminal.cols
      : 9;
    const cellHeight = screenElement
      ? screenElement.getBoundingClientRect().height / this.terminal.rows
      : 17;

    const decoration = this.terminal.registerDecoration({
      marker,
      width: this.terminal.cols,
      layer: 'top',
    });

    if (decoration) {
      decoration.onRender((element) => {
        element.style.backgroundColor = 'rgba(59, 142, 234, 0.4)';
        element.style.width = `${this.terminal!.cols * cellWidth}px`;
        element.style.height = `${cellHeight}px`;
        element.style.pointerEvents = 'none';
        element.style.boxSizing = 'border-box';
        element.style.borderLeft = '3px solid #3b8eea';
      });

      this.selectedLines.set(bufferLine, {
        bufferLine,
        markerId: marker.id,
        text: lineText,
      });

      marker.onDispose(() => {
        this.selectedLines.delete(bufferLine);
        this.notifySelectedLinesChange();
      });
    }

    this.notifySelectedLinesChange();
  }

  private notifySelectedLinesChange() {
    const lines = this.getSelectedLines();
    this.onSelectedLinesChange(lines);
  }
}
