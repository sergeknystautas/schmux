// ============================================================================
// Schmux Dashboard - Shared Utilities and Components
// ============================================================================

// ============================================================================
// Theme Manager
// ============================================================================
const ThemeManager = {
    STORAGE_KEY: 'schmux-theme',

    init() {
        const savedTheme = localStorage.getItem(this.STORAGE_KEY);
        if (savedTheme) {
            document.documentElement.setAttribute('data-theme', savedTheme);
            this.updateToggleIcon(savedTheme);
        } else if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
            document.documentElement.setAttribute('data-theme', 'dark');
            this.updateToggleIcon('dark');
        }

        const themeToggle = document.getElementById('themeToggle');
        if (themeToggle) {
            themeToggle.addEventListener('click', () => this.toggle());
        }
    },

    toggle() {
        const currentTheme = document.documentElement.getAttribute('data-theme');
        const newTheme = currentTheme === 'dark' ? 'light' : 'dark';

        document.documentElement.setAttribute('data-theme', newTheme);
        localStorage.setItem(this.STORAGE_KEY, newTheme);
        this.updateToggleIcon(newTheme);
    },

    updateToggleIcon(theme) {
        const themeToggle = document.getElementById('themeToggle');
        if (!themeToggle) return;

        const icon = themeToggle.querySelector('.icon-theme');
        if (icon) {
            // Icon is handled by CSS
        }
    }
};

// ============================================================================
// Toast Notifications
// ============================================================================
const Toast = {
    container: null,

    init() {
        if (!this.container) {
            this.container = document.createElement('div');
            this.container.className = 'toast-container';
            document.body.appendChild(this.container);
        }
    },

    show(message, type = 'info', duration = 3000) {
        this.init();

        const toast = document.createElement('div');
        toast.className = `toast toast--${type}`;
        toast.textContent = message;

        this.container.appendChild(toast);

        setTimeout(() => {
            toast.style.animation = 'slideIn 0.25s ease reverse';
            setTimeout(() => {
                toast.remove();
            }, 250);
        }, duration);
    },

    success(message, duration) {
        this.show(message, 'success', duration);
    },

    error(message, duration) {
        this.show(message, 'error', duration);
    }
};

// ============================================================================
// Modal Dialog
// ============================================================================
const Modal = {
    show(title, message, onConfirm, options = {}) {
        const {
            confirmText = 'Confirm',
            cancelText = 'Cancel',
            danger = false,
            detailedMessage = ''
        } = options;

        const overlay = document.createElement('div');
        overlay.className = 'modal-overlay';
        overlay.setAttribute('role', 'dialog');
        overlay.setAttribute('aria-modal', 'true');
        overlay.setAttribute('aria-labelledby', 'modal-title');

        overlay.innerHTML = `
            <div class="modal">
                <div class="modal__header">
                    <h2 class="modal__title" id="modal-title">${title}</h2>
                </div>
                <div class="modal__body">
                    <p>${message}</p>
                    ${detailedMessage ? `<p class="text-muted">${detailedMessage}</p>` : ''}
                </div>
                <div class="modal__footer">
                    <button class="btn" id="modal-cancel">${cancelText}</button>
                    <button class="btn ${danger ? 'btn--danger' : 'btn--primary'}" id="modal-confirm">${confirmText}</button>
                </div>
            </div>
        `;

        document.body.appendChild(overlay);

        const cancelBtn = overlay.querySelector('#modal-cancel');
        const confirmBtn = overlay.querySelector('#modal-confirm');

        const close = () => {
            overlay.remove();
        };

        cancelBtn.addEventListener('click', close);

        confirmBtn.addEventListener('click', () => {
            close();
            if (onConfirm) onConfirm();
        });

        // Close on overlay click
        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) close();
        });

        // Close on Escape
        const handleEscape = (e) => {
            if (e.key === 'Escape') {
                close();
                document.removeEventListener('keydown', handleEscape);
            }
        };
        document.addEventListener('keydown', handleEscape);

        // Focus confirm button
        setTimeout(() => confirmBtn.focus(), 50);
    },

    confirm(message, onConfirm) {
        this.show('Confirm Action', message, onConfirm);
    },

    alert(title, message) {
        this.show(title, message, null, { confirmText: 'OK' });
    }
};

// ============================================================================
// Utility Functions
// ============================================================================
const Utils = {
    formatTimestamp(timestamp) {
        const date = new Date(timestamp);
        return date.toLocaleString();
    },

    formatRelativeTime(timestamp) {
        const date = new Date(timestamp);
        const now = new Date();
        const diff = now - date;

        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);

        if (seconds < 60) return 'just now';
        if (minutes < 60) return `${minutes}m ago`;
        if (hours < 24) return `${hours}h ago`;
        if (days < 7) return `${days}d ago`;
        return date.toLocaleDateString();
    },

    async copyToClipboard(text) {
        try {
            await navigator.clipboard.writeText(text);
            Toast.success('Copied to clipboard');
            return true;
        } catch (err) {
            console.error('Failed to copy:', err);
            Toast.error('Failed to copy');
            return false;
        }
    },

    debounce(func, wait) {
        let timeout;
        return function executedFunction(...args) {
            const later = () => {
                clearTimeout(timeout);
                func(...args);
            };
            clearTimeout(timeout);
            timeout = setTimeout(later, wait);
        };
    },

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
};

// ============================================================================
// WebSocket Terminal Streaming (xterm.js-based)
// ============================================================================
class TerminalStream {
    constructor(sessionId, containerElement, options = {}) {
        this.sessionId = sessionId;
        this.containerElement = containerElement;
        this.ws = null;
        this.connected = false;
        this.followTail = options.followTail !== false;
        this.followCheckbox = options.followCheckbox || null;
        this.onStatusChange = options.onStatusChange || (() => {});
        this.onResume = options.onResume || (() => {});

        // xterm.js instance
        this.terminal = null;

        // Terminal size (from config, matches tmux pane size - never changes)
        this.tmuxCols = null;
        this.tmuxRows = null;

        // Promise that resolves when terminal is initialized
        this.initialized = this.initTerminal();
    }

    async initTerminal() {
        // Check if xterm.js is loaded
        if (typeof Terminal === 'undefined') {
            this.containerElement.textContent = 'Error: xterm.js library not loaded';
            return null;
        }

        // Fetch terminal size from config (required - no defaults)
        let cols, rows;
        try {
            const resp = await fetch('/api/config');
            if (!resp.ok) {
                throw new Error(`Failed to fetch config: ${resp.status}`);
            }
            const config = await resp.json();
            if (!config.terminal || typeof config.terminal.width !== 'number' || typeof config.terminal.height !== 'number') {
                throw new Error('Config missing terminal.width or terminal.height');
            }
            cols = config.terminal.width;
            rows = config.terminal.height;
        } catch (e) {
            this.containerElement.textContent = `Error: ${e.message}`;
            console.error('Failed to load terminal size from config:', e);
            return null;
        }

        // Store tmux pane size (terminal never resizes from these dimensions)
        this.tmuxCols = cols;
        this.tmuxRows = rows;

        // Create xterm.js terminal with tmux pane size (matches server-side)
        this.terminal = new Terminal({
            cols: cols,
            rows: rows,
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

        // Open terminal in the container
        this.terminal.open(this.containerElement);

        // Set up user input handling
        this.terminal.onData((data) => {
            this.sendInput(data);
        });

        // Track scroll position by listening to the viewport element
        // xterm.js onScroll only fires for programmatic scrolls, not user scrolls
        // So we must listen to the DOM scroll event directly
        this._attachScrollListener();

        // Welcome message
        this.terminal.writeln('\x1b[90mConnecting to session...\x1b[0m');

        // Set up resize observer to handle container size changes
        this.setupResizeHandler();

        return this.terminal;
    }

    _attachScrollListener() {
        // Try to attach scroll listener, retry if element not ready yet
        const tryAttach = (attempts = 0) => {
            const viewport = this.terminal?.element?.querySelector('.xterm-viewport');
            if (viewport) {
                viewport.addEventListener('scroll', () => {
                    this.handleUserScroll();
                });
            } else if (attempts < 10) {
                // Retry with exponential backoff
                setTimeout(() => tryAttach(attempts + 1), 50 * (attempts + 1));
            }
        };
        tryAttach();
    }

    setupResizeHandler() {
        // Use ResizeObserver to detect container size changes
        if (typeof ResizeObserver !== 'undefined') {
            const resizeObserver = new ResizeObserver(() => {
                this.scaleTerminal();
            });
            resizeObserver.observe(this.containerElement);
        }

        // Also listen to window resize
        window.addEventListener('resize', () => {
            this.scaleTerminal();
        });

        // Initial scaling (try multiple times as container may not have final size yet)
        setTimeout(() => this.scaleTerminal(), 100);
        setTimeout(() => this.scaleTerminal(), 300);
        setTimeout(() => this.scaleTerminal(), 1000);
    }

    scaleTerminal() {
        if (!this.terminal) return;

        // Find the screen element (the actual terminal content, not the viewport)
        const screenElement = this.terminal.element?.querySelector('.xterm-screen');
        if (!screenElement) return;

        // Get container dimensions
        const containerRect = this.containerElement.getBoundingClientRect();
        const containerWidth = containerRect.width || 800;
        const containerHeight = containerRect.height || 600;

        // Calculate the EXPECTED screen size from terminal dimensions (don't measure!)
        // xterm.js at fontSize 14 has approximately these character dimensions
        const charWidth = 9;   // width at fontSize 14
        const charHeight = 17;  // height at fontSize 14 (line height ~1.2)
        const screenWidth = this.tmuxCols * charWidth;
        const screenHeight = this.tmuxRows * charHeight;

        // Calculate scale factor to fit screen in container
        const scaleX = containerWidth / screenWidth;
        const scaleY = containerHeight / screenHeight;
        const scale = Math.min(scaleX, scaleY, 1); // Never scale up, only down

        // Apply CSS transform to scale ONLY the screen content
        screenElement.style.transformOrigin = 'top left';
        screenElement.style.transform = `scale(${scale})`;
    }

    connect() {
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
            } else {
                console.warn('[ws] message received but terminal is null');
            }
        };

        this.ws.onclose = () => {
            this.connected = false;
            this.terminal.writeln('\x1b[90m\r\n\x1b[0m');
            this.terminal.writeln('\x1b[91mConnection closed\x1b[0m');
            this.onStatusChange('disconnected');
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            if (this.terminal) {
                this.terminal.writeln('\x1b[91mWebSocket error\x1b[0m');
            }
        };
    }

    disconnect() {
        if (this.ws) {
            this.ws.close();
        }
    }

    sendInput(data) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({ type: 'input', data: data }));
        }
    }

    handleOutput(data) {
        // Parse JSON message from backend
        let msg;
        try {
            msg = JSON.parse(data);
        } catch {
            // Legacy fallback: plain text treated as full refresh
            msg = { type: 'full', content: data };
        }

        // Handle message based on type
        switch (msg.type) {
            case 'append':
                this.terminal.write(msg.content);
                break;
            case 'full':
                // Use reset() instead of clear() for full refresh
                // reset() clears the buffer and scrollback, clear() only clears viewport
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

    setFollow(follow) {
        this.followTail = follow;
        if (this.followCheckbox) this.followCheckbox.checked = follow;
        // Show resume button when not following (scrolled up)
        this.onResume(!follow);
    }

    isAtBottom(threshold = 0) {
        // Use xterm.js buffer API (verified from official docs)
        // viewportY = line at top of viewport
        // baseY = line at top of "bottom page" (fully scrolled down position)
        // When at bottom: viewportY === baseY
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

        // Get buffer content
        const buffer = this.terminal.buffer;
        const lines = [];
        for (let i = 0; i < buffer.active.length; i++) {
            const line = buffer.active.getLine(i);
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
        Toast.success('Downloaded session log');
    }
}

// ============================================================================
// Connection Monitor
// ============================================================================
const ConnectionMonitor = {
    CHECK_INTERVAL: 5000, // 5 seconds
    connectionPill: null,
    connectionText: null,
    intervalId: null,

    init() {
        this.connectionPill = document.getElementById('connectionPill');
        this.connectionText = document.getElementById('connectionText');

        if (!this.connectionPill || !this.connectionText) {
            return;
        }

        // Initial check
        this.check();

        // Start periodic polling
        this.intervalId = setInterval(() => this.check(), this.CHECK_INTERVAL);
    },

    async check() {
        try {
            const response = await fetch('/api/healthz');
            if (response.ok) {
                this.setConnected();
            } else {
                this.setDisconnected();
            }
        } catch (error) {
            this.setDisconnected();
        }
    },

    setConnected() {
        if (!this.connectionPill || !this.connectionText) return;

        this.connectionPill.classList.remove('connection-pill--offline');
        this.connectionPill.classList.add('connection-pill--connected');
        this.connectionText.textContent = 'Connected';
    },

    setDisconnected() {
        if (!this.connectionPill || !this.connectionText) return;

        this.connectionPill.classList.remove('connection-pill--connected');
        this.connectionPill.classList.add('connection-pill--offline');
        this.connectionText.textContent = 'Disconnected';
    },

    destroy() {
        if (this.intervalId) {
            clearInterval(this.intervalId);
            this.intervalId = null;
        }
    }
};

// ============================================================================
// API Client
// ============================================================================
const API = {
    async getSessions() {
        const response = await fetch('/api/sessions');
        if (!response.ok) throw new Error('Failed to fetch sessions');
        return response.json();
    },

    async getWorkspaces() {
        const response = await fetch('/api/workspaces');
        if (!response.ok) throw new Error('Failed to fetch workspaces');
        return response.json();
    },

    async getSession(sessionId) {
        const response = await fetch(`/api/sessions/${sessionId}`);
        if (!response.ok) throw new Error('Failed to fetch session');
        return response.json();
    },

    async spawnSessions(request) {
        const response = await fetch('/api/spawn', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(request)
        });
        if (!response.ok) throw new Error('Failed to spawn sessions');
        return response.json();
    },

    async disposeSession(sessionId) {
        const response = await fetch(`/api/dispose/${sessionId}`, {
            method: 'POST'
        });
        if (!response.ok) throw new Error('Failed to dispose session');
        return response.json();
    },

    async getConfig() {
        const response = await fetch('/api/config');
        if (!response.ok) throw new Error('Failed to fetch config');
        return response.json();
    }
};

// ============================================================================
// Initialize on DOM ready
// ============================================================================
document.addEventListener('DOMContentLoaded', () => {
    ThemeManager.init();
    ConnectionMonitor.init();
});

// ============================================================================
// Exports for global use
// ============================================================================
window.ThemeManager = ThemeManager;
window.Toast = Toast;
window.Modal = Modal;
window.Utils = Utils;
window.TerminalStream = TerminalStream;
window.API = API;
window.ConnectionMonitor = ConnectionMonitor;
