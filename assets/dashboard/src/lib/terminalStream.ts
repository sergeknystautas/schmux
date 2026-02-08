import { Terminal } from '@xterm/xterm';
import { WebLinksAddon } from '@xterm/addon-web-links';
import type { TerminalSize } from './types';

type TerminalStreamOptions = {
  followTail?: boolean;
  followCheckbox?: HTMLInputElement | null;
  onStatusChange?: (status: 'connected' | 'disconnected' | 'reconnecting' | 'error') => void;
  onResume?: (showing: boolean) => void;
  terminalSize?: TerminalSize | null;
  onSelectedLinesChange?: (lines: string[]) => void;
};

type TerminalOutputMessage = {
  type?: 'append' | 'full' | string;
  content?: string;
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
  terminalSize: TerminalSize | null;
  terminal: Terminal | null;
  tmuxCols: number | null;
  tmuxRows: number | null;
  baseFontSize: number;
  initialized: Promise<Terminal | null>;
  resizeDebounceTimer: ReturnType<typeof setTimeout> | null;

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

  constructor(sessionId: string, containerElement: HTMLElement, options: TerminalStreamOptions = {}) {
    this.sessionId = sessionId;
    this.containerElement = containerElement;
    this.ws = null;
    this.connected = false;
    this.followTail = options.followTail !== false;
    this.followCheckbox = options.followCheckbox || null;
    this.onStatusChange = options.onStatusChange || (() => {});
    this.onResume = options.onResume || (() => {});
    this.terminalSize = options.terminalSize || null;
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

    this.initialized = this.initTerminal();
  }

  async initTerminal(): Promise<Terminal | null> {
    if (!this.containerElement) {
      return null;
    }

    // Calculate initial dimensions from container, falling back to config
    const containerRect = this.containerElement.getBoundingClientRect();
    let cols = this.terminalSize?.width ?? 80;
    let rows = this.terminalSize?.height ?? 24;

    // If container has valid dimensions, calculate from it
    // Use reasonable cell size estimates for initial render
    if (containerRect.width > 0 && containerRect.height > 0) {
      const estimatedCellWidth = 9;
      const estimatedCellHeight = 17;
      cols = Math.max(20, Math.floor(containerRect.width / estimatedCellWidth) - 1);
      rows = Math.max(5, Math.floor(containerRect.height / estimatedCellHeight) - 1);
    } else if (typeof this.terminalSize?.width !== 'number' || typeof this.terminalSize?.height !== 'number') {
      const message = 'Terminal size is unavailable in config';
      this.containerElement.textContent = `Error: ${message}`;
      console.error(message);
      return null;
    }

    this.tmuxCols = cols;
    this.tmuxRows = rows;
    this.terminal = new Terminal({
      cols,
      rows,
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      allowProposedApi: true,  // Required for registerDecoration API
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
        brightWhite: '#ffffff'
      },
      scrollback: 1000,
      convertEol: true
    });

    this.terminal.loadAddon(new WebLinksAddon());
    this.terminal.open(this.containerElement);
    this.terminal.onData((data) => {
      this.sendInput(data);
    });

    this._attachScrollListener();
    this.terminal.writeln('\x1b[90mConnecting to session...\x1b[0m');
    this.setupResizeHandler();

    // Immediately calculate accurate dimensions now that terminal is rendered
    // This ensures we have correct dimensions before WebSocket connects
    this.fitTerminalSync();

    return this.terminal;
  }

  _attachScrollListener() {
    const tryAttach = (attempts = 0) => {
      const viewport = this.terminal?.element?.querySelector('.xterm-viewport');
      if (viewport) {
        viewport.addEventListener('scroll', () => {
          this.handleUserScroll();
        });
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

      // Also watch the .session-detail parent to detect viewport changes
      // This catches cases where the window grows but our container doesn't
      const sessionDetail = this.containerElement.closest('.session-detail');
      if (sessionDetail) {
        resizeObserver.observe(sessionDetail);
      }
    }

    window.addEventListener('resize', () => {
      this.handleResize();
    });
  }

  handleResize() {
    // Debounce resize events to avoid excessive backend calls
    if (this.resizeDebounceTimer) {
      clearTimeout(this.resizeDebounceTimer);
    }
    this.resizeDebounceTimer = setTimeout(() => {
      this.fitTerminal();
    }, 150);
  }

  // Synchronous resize - calculates and applies dimensions immediately
  // Used during initialization to ensure terminal is sized before WebSocket connects
  fitTerminalSync() {
    if (!this.terminal) return;

    const containerRect = this.containerElement.getBoundingClientRect();
    const containerWidth = containerRect.width;
    const containerHeight = containerRect.height;

    if (containerWidth <= 0 || containerHeight <= 0) return;

    // Measure actual cell dimensions from the terminal
    const core = (this.terminal as unknown as { _core: { _renderService: { dimensions: { css: { cell: { width: number; height: number } } } } } })._core;
    let cellWidth = 9;
    let cellHeight = 17;

    if (core?._renderService?.dimensions?.css?.cell) {
      cellWidth = core._renderService.dimensions.css.cell.width;
      cellHeight = core._renderService.dimensions.css.cell.height;
    }

    // Calculate new dimensions
    const newCols = Math.max(20, Math.floor(containerWidth / cellWidth) - 1);
    const newRows = Math.max(5, Math.floor(containerHeight / cellHeight) - 1);

    // Update stored dimensions
    this.tmuxCols = newCols;
    this.tmuxRows = newRows;

    // Resize xterm.js terminal
    this.terminal.resize(newCols, newRows);

    // Note: Don't send resize to backend here - WebSocket isn't connected yet
    // The connect() method will send resize on open
  }

  fitTerminal() {
    if (!this.terminal) return;

    const containerRect = this.containerElement.getBoundingClientRect();
    const containerWidth = containerRect.width;
    const containerHeight = containerRect.height;

    if (containerWidth <= 0 || containerHeight <= 0) return;

    // Measure actual cell dimensions from the terminal
    // xterm.js creates a hidden element to measure character size
    const core = (this.terminal as unknown as { _core: { _renderService: { dimensions: { css: { cell: { width: number; height: number } } } } } })._core;
    let cellWidth = 9;
    let cellHeight = 17;

    if (core?._renderService?.dimensions?.css?.cell) {
      cellWidth = core._renderService.dimensions.css.cell.width;
      cellHeight = core._renderService.dimensions.css.cell.height;
    }

    // Calculate new dimensions (subtract 1 to prevent overflow)
    const newCols = Math.max(20, Math.floor(containerWidth / cellWidth) - 1);
    const newRows = Math.max(5, Math.floor(containerHeight / cellHeight) - 1);

    // Only resize if dimensions actually changed
    if (newCols === this.tmuxCols && newRows === this.tmuxRows) {
      return;
    }

    // Update stored dimensions
    this.tmuxCols = newCols;
    this.tmuxRows = newRows;

    // Resize xterm.js terminal
    this.terminal.resize(newCols, newRows);

    // Send resize message to backend to resize tmux
    this.sendResize(newCols, newRows);
  }

  sendResize(cols: number, rows: number) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({
        type: 'resize',
        data: JSON.stringify({ cols, rows })
      }));
    }
  }

  resizeTerminal() {
    this.fitTerminal();
  }

  connect() {
    if (!this.terminal) return;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/terminal/${this.sessionId}`;

    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      this.connected = true;
      this.terminal.clear();
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

    this.ws.onclose = () => {
      this.connected = false;
      if (this.terminal) {
        this.terminal.writeln('\x1b[90m\r\n\x1b[0m');
        this.terminal.writeln('\x1b[91mConnection closed\x1b[0m');
      }
      this.onStatusChange('disconnected');
    };

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error);
      if (this.terminal) {
        this.terminal.writeln('\x1b[91mWebSocket error\x1b[0m');
      }
      this.onStatusChange('error');
    };
  }

  disconnect() {
    if (this.ws) {
      this.ws.close();
    }
  }

  focus() {
    this.terminal?.focus();
  }

  sendInput(data: string) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type: 'input', data }));
    }
  }

  handleOutput(data: string) {
    let msg: TerminalOutputMessage;
    try {
      msg = JSON.parse(data);
    } catch {
      msg = { type: 'full', content: data };
    }

    switch (msg.type) {
      case 'append':
        this.terminal.write(msg.content);
        break;
      case 'full':
        this.terminal.reset();
        this.terminal.write(msg.content);
        break;
      default:
        this.terminal.reset();
        this.terminal.write(msg.content || data);
    }

    if (this.followTail) {
      this.terminal.scrollToBottom();
    }
  }

  setFollow(follow: boolean) {
    this.followTail = follow;
    if (this.followCheckbox) this.followCheckbox.checked = follow;
    this.onResume(!follow);
  }

  isAtBottom(threshold = 0): boolean {
    if (!this.terminal) return true;
    const buffer = this.terminal.buffer.active;
    return buffer.viewportY >= buffer.baseY - threshold;
  }

  handleUserScroll() {
    if (!this.terminal) return;
    this.setFollow(this.isAtBottom(1));
  }

  jumpToBottom() {
    if (this.terminal) {
      this.terminal.scrollToBottom();
      this.setFollow(true);
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
    return Array.from(this.selectedLines.values()).map(sl => sl.text);
  }

  clearSelection() {
    this.clearSelectionMarkers();
    this.notifySelectedLinesChange();
  }

  private clearSelectionMarkers() {
    if (!this.terminal) return;
    for (const selected of this.selectedLines.values()) {
      const marker = this.terminal.markers.find(m => m.id === selected.markerId);
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
      const marker = this.terminal.markers.find(m => m.id === selected.markerId);
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
    const cellWidth = screenElement ? screenElement.getBoundingClientRect().width / this.terminal.cols : 9;
    const cellHeight = screenElement ? screenElement.getBoundingClientRect().height / this.terminal.rows : 17;

    const decoration = this.terminal.registerDecoration({
      marker,
      width: this.terminal.cols,
      layer: 'top'
    });

    if (decoration) {
      decoration.onRender((element) => {
        element.style.backgroundColor = 'rgba(59, 142, 234, 0.4)';
        element.style.width = `${this.terminal.cols * cellWidth}px`;
        element.style.height = `${cellHeight}px`;
        element.style.pointerEvents = 'none';
        element.style.boxSizing = 'border-box';
        element.style.borderLeft = '3px solid #3b8eea';
      });

      this.selectedLines.set(bufferLine, {
        bufferLine,
        markerId: marker.id,
        text: lineText
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
