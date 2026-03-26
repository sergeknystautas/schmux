/**
 * Write-race diagnostics for xterm.js
 *
 * Silently instruments xterm.js internals via monkey-patching to collect
 * performance data about write parsing, viewport sync, rendering, and
 * main thread stalls. Data is collected continuously and persisted to disk
 * when the user triggers a desync diagnostic capture ("diagnose" button).
 *
 * Tracks:
 *  - InputHandler.parse() duration per write
 *  - Viewport._sync() calls during writes
 *  - _handleScroll calls during _sync (suppress timing bug)
 *  - Full-screen refresh frequency
 *  - Actual renderer.renderRows() timing
 *  - Main thread stalls (>100ms gaps)
 *  - Buffer switch events
 */

import type { Terminal, IDisposable } from '@xterm/xterm';

export type WriteRaceEvent = {
  ts: number;
  dataLen: number;
  totalMs: number;
  parseMs: number;
  waitMs: number;
  scrollsDuringWrite: number;
  viewportSyncsDuringWrite: number;
  handleScrollsDuringWrite: number;
  handleScrollDuringSyncCount: number;
  refreshCalls: number;
  fullRefreshCalls: number;
  linesAtStart: number;
  linesAtEnd: number;
  baseYDelta: number;
  viewportYDelta: number;
};

export type StallEvent = {
  ts: number;
  gapMs: number;
  inWrite: boolean;
  lines: number;
  baseY: number;
  viewportY: number;
};

const MAX_EVENTS = 200;
const MAX_STALLS = 50;

export class WriteRaceDiagnostics {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private _core: any;
  private _terminal: Terminal;
  private _events: WriteRaceEvent[] = [];
  private _disposables: IDisposable[] = [];

  // Per-write state (reset in beginWrite, incremented by patches)
  private _inWrite = false;
  private _inSync = false;
  private _scrollCount = 0;
  private _viewportSyncCount = 0;
  private _handleScrollCount = 0;
  private _handleScrollDuringSyncCount = 0;
  private _refreshCount = 0;
  private _fullRefreshCount = 0;
  private _lastParseDurationMs = 0;

  // Main thread stall detection
  private _stallDetectorTimer: ReturnType<typeof setInterval> | null = null;
  private _lastStallCheck = 0;
  private _stalls: StallEvent[] = [];

  // Render timing
  private _totalRenderMs = 0;
  private _renderCount = 0;
  private _longestRenderMs = 0;
  private _longestRenderRows = 0;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private _origRenderRows: ((...args: any[]) => any) | null = null;

  // Buffer switch tracking
  private _bufferSwitches: { ts: number; buffer: string }[] = [];

  // Aggregates
  totalWrites = 0;
  totalWriteDurationMs = 0;
  totalParseDurationMs = 0;
  longestWriteMs = 0;
  longestWriteDataLen = 0;
  longestParseMs = 0;
  longestParseDataLen = 0;
  totalScrollsDuringWrite = 0;
  totalViewportSyncsDuringWrite = 0;
  totalHandleScrollsDuringWrite = 0;
  totalHandleScrollDuringSync = 0;
  totalRefreshCalls = 0;
  totalFullRefreshes = 0;
  totalRedundantScrollToBottom = 0;
  totalScrollToBottomChecks = 0;

  // Originals for unpatching
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private _origSync: ((...args: any[]) => any) | null = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private _origHandleScroll: ((...args: any[]) => any) | null = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private _origRefreshRows: ((...args: any[]) => any) | null = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private _origParse: ((...args: any[]) => any) | null = null;

  constructor(terminal: Terminal) {
    this._terminal = terminal;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    this._core = (terminal as any)._core;
    this._patch();
    this._attachListeners();
    this._startStallDetector();
  }

  private _patch(): void {
    // eslint-disable-next-line @typescript-eslint/no-this-alias
    const self = this;

    // Patch InputHandler.parse() for accurate parse timing
    const ih = this._core._inputHandler;
    if (ih) {
      this._origParse = ih.parse;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      ih.parse = function (this: any, ...args: any[]) {
        if (!self._inWrite) return self._origParse!.apply(this, args);
        const t0 = performance.now();
        const result = self._origParse!.apply(this, args);
        self._lastParseDurationMs = performance.now() - t0;
        return result;
      };
    }

    // Patch Viewport._sync() to count calls during writes
    const vp = this._core._viewport;
    if (vp) {
      this._origSync = vp._sync;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      vp._sync = function (this: any, ...args: any[]) {
        const wasInSync = self._inSync;
        self._inSync = true;
        if (self._inWrite) self._viewportSyncCount++;
        try {
          return self._origSync!.apply(this, args);
        } finally {
          self._inSync = wasInSync;
        }
      };

      // Patch _handleScroll to detect calls during _sync
      this._origHandleScroll = vp._handleScroll;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      vp._handleScroll = function (this: any, ...args: any[]) {
        if (self._inWrite) self._handleScrollCount++;
        if (self._inSync) self._handleScrollDuringSyncCount++;
        return self._origHandleScroll!.apply(this, args);
      };
    }

    // Patch RenderService.refreshRows() to detect full-screen refreshes
    const rs = this._core._renderService;
    if (rs) {
      this._origRefreshRows = rs.refreshRows;
      const termRows = this._terminal.rows;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      rs.refreshRows = function (this: any, start: number, end: number, ...rest: any[]) {
        if (self._inWrite) {
          self._refreshCount++;
          if (start === 0 && end >= termRows - 1) {
            self._fullRefreshCount++;
          }
        }
        return self._origRefreshRows!.apply(this, [start, end, ...rest]);
      };

      // Patch RenderService._renderRows() to measure actual render time
      if (rs._renderRows) {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const origRenderRows: (...args: any[]) => any = rs._renderRows;
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        rs._renderRows = function (this: any, start: number, end: number, ...rest: any[]) {
          const t0 = performance.now();
          const result = origRenderRows.apply(this, [start, end, ...rest]);
          const elapsed = performance.now() - t0;
          self._renderCount++;
          self._totalRenderMs += elapsed;
          const rows = end - start + 1;
          if (elapsed > self._longestRenderMs) {
            self._longestRenderMs = elapsed;
            self._longestRenderRows = rows;
          }
          return result;
        };
        self._origRenderRows = origRenderRows;
      }
    }
  }

  private _attachListeners(): void {
    this._disposables.push(
      this._terminal.onScroll(() => {
        if (this._inWrite) this._scrollCount++;
      })
    );

    // Track buffer switches (normal ↔ alternate)
    const buffers = this._core._bufferService?.buffers;
    if (buffers?.onBufferActivate) {
      this._disposables.push(
        buffers.onBufferActivate(() => {
          const active =
            this._terminal.buffer.active === this._terminal.buffer.normal ? 'normal' : 'alternate';
          this._bufferSwitches.push({ ts: Date.now(), buffer: active });
          if (this._bufferSwitches.length > 20) {
            this._bufferSwitches = this._bufferSwitches.slice(-20);
          }
        })
      );
    }
  }

  private _startStallDetector(): void {
    this._lastStallCheck = performance.now();
    this._stallDetectorTimer = setInterval(() => {
      const now = performance.now();
      const gap = now - this._lastStallCheck;
      if (gap > 100) {
        const buf = this._terminal.buffer.active;
        this._stalls.push({
          ts: Date.now(),
          gapMs: Math.round(gap),
          inWrite: this._inWrite,
          lines: buf.length,
          baseY: buf.baseY,
          viewportY: buf.viewportY,
        });
        if (this._stalls.length > MAX_STALLS) {
          this._stalls = this._stalls.slice(-MAX_STALLS);
        }
      }
      this._lastStallCheck = now;
    }, 50);
  }

  beginWrite(dataLen: number): { finish: () => WriteRaceEvent } {
    this._inWrite = true;
    this._scrollCount = 0;
    this._viewportSyncCount = 0;
    this._handleScrollCount = 0;
    this._handleScrollDuringSyncCount = 0;
    this._refreshCount = 0;
    this._fullRefreshCount = 0;
    this._lastParseDurationMs = 0;

    const buf = this._terminal.buffer.active;
    const writeCallTime = performance.now();
    const linesAtStart = buf.length;
    const baseYAtStart = buf.baseY;
    const viewportYAtStart = buf.viewportY;

    return {
      finish: (): WriteRaceEvent => {
        this._inWrite = false;
        const callbackTime = performance.now();
        const totalMs = callbackTime - writeCallTime;
        const parseMs = this._lastParseDurationMs;
        const buf = this._terminal.buffer.active;

        const event: WriteRaceEvent = {
          ts: Date.now(),
          dataLen,
          totalMs,
          parseMs,
          waitMs: Math.max(0, totalMs - parseMs),
          scrollsDuringWrite: this._scrollCount,
          viewportSyncsDuringWrite: this._viewportSyncCount,
          handleScrollsDuringWrite: this._handleScrollCount,
          handleScrollDuringSyncCount: this._handleScrollDuringSyncCount,
          refreshCalls: this._refreshCount,
          fullRefreshCalls: this._fullRefreshCount,
          linesAtStart,
          linesAtEnd: buf.length,
          baseYDelta: buf.baseY - baseYAtStart,
          viewportYDelta: buf.viewportY - viewportYAtStart,
        };

        this._events.push(event);
        if (this._events.length > MAX_EVENTS) {
          this._events = this._events.slice(-MAX_EVENTS);
        }

        // Update aggregates
        this.totalWrites++;
        this.totalWriteDurationMs += totalMs;
        this.totalParseDurationMs += parseMs;
        if (totalMs > this.longestWriteMs) {
          this.longestWriteMs = totalMs;
          this.longestWriteDataLen = dataLen;
        }
        if (parseMs > this.longestParseMs) {
          this.longestParseMs = parseMs;
          this.longestParseDataLen = dataLen;
        }
        this.totalScrollsDuringWrite += this._scrollCount;
        this.totalViewportSyncsDuringWrite += this._viewportSyncCount;
        this.totalHandleScrollsDuringWrite += this._handleScrollCount;
        this.totalHandleScrollDuringSync += this._handleScrollDuringSyncCount;
        this.totalRefreshCalls += this._refreshCount;
        this.totalFullRefreshes += this._fullRefreshCount;

        return event;
      },
    };
  }

  checkScrollToBottomRedundancy(): { redundant: boolean; viewportY: number; baseY: number } {
    const buf = this._terminal.buffer.active;
    const redundant = buf.viewportY >= buf.baseY;
    this.totalScrollToBottomChecks++;
    if (redundant) {
      this.totalRedundantScrollToBottom++;
    }
    return { redundant, viewportY: buf.viewportY, baseY: buf.baseY };
  }

  /**
   * Returns all collected data as a JSON-serializable object.
   * Called by the desync diagnostic capture flow to persist to disk.
   */
  snapshot(): Record<string, unknown> {
    const avgDuration = this.totalWrites ? this.totalWriteDurationMs / this.totalWrites : 0;
    const avgParse = this.totalWrites ? this.totalParseDurationMs / this.totalWrites : 0;
    const avgRender = this._renderCount ? this._totalRenderMs / this._renderCount : 0;

    return {
      aggregates: {
        totalWrites: this.totalWrites,
        avgWriteMs: +avgDuration.toFixed(2),
        avgParseMs: +avgParse.toFixed(2),
        longestWriteMs: +this.longestWriteMs.toFixed(2),
        longestWriteDataLen: this.longestWriteDataLen,
        longestParseMs: +this.longestParseMs.toFixed(2),
        longestParseDataLen: this.longestParseDataLen,
        avgScrollsPerWrite: this.totalWrites
          ? +(this.totalScrollsDuringWrite / this.totalWrites).toFixed(1)
          : 0,
        avgViewportSyncsPerWrite: this.totalWrites
          ? +(this.totalViewportSyncsDuringWrite / this.totalWrites).toFixed(1)
          : 0,
        totalHandleScrollDuringSync: this.totalHandleScrollDuringSync,
        totalRefreshCalls: this.totalRefreshCalls,
        totalFullRefreshes: this.totalFullRefreshes,
        fullRefreshRatio: this.totalRefreshCalls
          ? +((this.totalFullRefreshes / this.totalRefreshCalls) * 100).toFixed(0)
          : 0,
        redundantScrollToBottom: this.totalRedundantScrollToBottom,
        scrollToBottomChecks: this.totalScrollToBottomChecks,
      },
      render: {
        totalRenders: this._renderCount,
        avgRenderMs: +avgRender.toFixed(2),
        longestRenderMs: +this._longestRenderMs.toFixed(2),
        longestRenderRows: this._longestRenderRows,
      },
      stalls: this._stalls,
      bufferSwitches: this._bufferSwitches,
      recentWrites: this._events.slice(-50),
    };
  }

  dispose(): void {
    if (this._stallDetectorTimer) {
      clearInterval(this._stallDetectorTimer);
      this._stallDetectorTimer = null;
    }

    // Unpatch
    const ih = this._core._inputHandler;
    if (ih && this._origParse) ih.parse = this._origParse;

    const vp = this._core._viewport;
    if (vp) {
      if (this._origSync) vp._sync = this._origSync;
      if (this._origHandleScroll) vp._handleScroll = this._origHandleScroll;
    }

    const rs = this._core._renderService;
    if (rs && this._origRefreshRows) rs.refreshRows = this._origRefreshRows;
    if (rs && this._origRenderRows) rs._renderRows = this._origRenderRows;

    for (const d of this._disposables) d.dispose();
    this._disposables = [];
  }
}
