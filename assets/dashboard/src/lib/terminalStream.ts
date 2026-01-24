import { Terminal } from '@xterm/xterm';
import type { TerminalSize } from './types';

type TerminalStreamOptions = {
  followTail?: boolean;
  followCheckbox?: HTMLInputElement | null;
  onStatusChange?: (status: 'connected' | 'disconnected' | 'reconnecting' | 'error') => void;
  onResume?: (showing: boolean) => void;
  terminalSize?: TerminalSize | null;
};

type TerminalOutputMessage = {
  type?: 'append' | 'full' | string;
  content?: string;
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

    this.terminal = null;
    this.tmuxCols = null;
    this.tmuxRows = null;
    this.baseFontSize = 14;

    this.initialized = this.initTerminal();
  }

  async initTerminal(): Promise<Terminal | null> {
    if (!this.containerElement) {
      return null;
    }

    const cols = this.terminalSize?.width;
    const rows = this.terminalSize?.height;
    if (typeof cols !== 'number' || typeof rows !== 'number') {
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

    this.terminal.open(this.containerElement);
    this.terminal.onData((data) => {
      this.sendInput(data);
    });

    this._attachScrollListener();
    this.terminal.writeln('\x1b[90mConnecting to session...\x1b[0m');
    this.setupResizeHandler();

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
        this.scaleTerminal();
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
      this.scaleTerminal();
    });

    setTimeout(() => this.scaleTerminal(), 100);
    setTimeout(() => this.scaleTerminal(), 300);
    setTimeout(() => this.scaleTerminal(), 1000);
  }

  scaleTerminal() {
    if (!this.terminal) return;

    const containerRect = this.containerElement.getBoundingClientRect();
    const containerWidth = containerRect.width || 800;
    const containerHeight = containerRect.height || 600;

    // Character dimensions at base fontSize (14px)
    const charWidthAt14 = 9;
    const charHeightAt14 = 17;

    // Terminal's natural size at base fontSize
    const terminalWidthAt14 = this.tmuxCols * charWidthAt14;
    const terminalHeightAt14 = this.tmuxRows * charHeightAt14;

    // Calculate scale factor needed to fit container
    const scaleX = containerWidth / terminalWidthAt14;
    const scaleY = containerHeight / terminalHeightAt14;
    const scale = Math.min(scaleX, scaleY, 1);

    // Set fontSize to scale the terminal (no CSS transform = coordinates work)
    const newFontSize = Math.max(1, Math.round(this.baseFontSize * scale));
    this.terminal.options.fontSize = newFontSize;
    this.terminal.refresh(0, this.terminal.rows - 1);
  }

  resizeTerminal() {
    this.scaleTerminal();
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
}
