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
// WebSocket Terminal Streaming
// ============================================================================
class TerminalStream {
    constructor(sessionId, outputElement, options = {}) {
        this.sessionId = sessionId;
        this.outputElement = outputElement;
        this.ws = null;
        this.connected = false;
        this.paused = false;
        this.followTail = options.followTail !== false;
        this.onStatusChange = options.onStatusChange || (() => {});
        this.onNewContent = options.onNewContent || (() => {});

        // For scroll-aware updates
        this.latestContent = '';
        this.pendingContent = false;
        this.userScrolled = false;

        // Listen for scroll events to detect user intent
        this.outputElement.addEventListener('scroll', () => this.handleScroll());
    }

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/terminal/${this.sessionId}`;

        this.ws = new WebSocket(wsUrl);

        this.ws.onopen = () => {
            this.connected = true;
            this.onStatusChange('connected');
        };

        this.ws.onmessage = (event) => {
            if (!this.paused) {
                this.updateOutput(event.data);
            }
        };

        this.ws.onclose = () => {
            this.connected = false;
            this.onStatusChange('disconnected');
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }

    disconnect() {
        if (this.ws) {
            this.ws.close();
        }
    }

    pause() {
        this.paused = true;
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send('pause');
        }
    }

    resume() {
        this.paused = false;
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send('resume');
        }
    }

    toggleFollow() {
        this.followTail = !this.followTail;
        return this.followTail;
    }

    isAtBottom(threshold = 50) {
        const { scrollTop, scrollHeight, clientHeight } = this.outputElement;
        return scrollHeight - scrollTop - clientHeight < threshold;
    }

    handleScroll() {
        // Mark user as scrolled if they've moved up from the true bottom at all
        if (!this.userScrolled && this.outputElement.scrollTop < this.outputElement.scrollHeight - this.outputElement.clientHeight - 1) {
            this.userScrolled = true;
        }

        // If user scrolls back near bottom, clear the scrolled state and resume
        if (this.userScrolled && this.isAtBottom()) {
            this.userScrolled = false;
            if (this.pendingContent) {
                this.pendingContent = false;
                this.outputElement.textContent = this.latestContent;
                this.onNewContent(false);
            }
        }
    }

    jumpToBottom() {
        this.userScrolled = false;
        this.pendingContent = false;
        this.outputElement.textContent = this.latestContent;
        this.outputElement.scrollTop = this.outputElement.scrollHeight;
        this.onNewContent(false);
    }

    updateOutput(output) {
        this.latestContent = output;

        // Only update if user hasn't scrolled up
        if (!this.userScrolled) {
            this.outputElement.textContent = output;
            if (this.followTail) {
                this.outputElement.scrollTop = this.outputElement.scrollHeight;
            }
        } else {
            // User is scrolled back - indicate new content
            if (!this.pendingContent) {
                this.pendingContent = true;
                this.onNewContent(true);
            }
        }
    }

    downloadOutput() {
        const content = this.outputElement.textContent;
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
