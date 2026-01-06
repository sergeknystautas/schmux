// Theme management
const ThemeManager = {
    init() {
        // Load saved theme from localStorage
        const savedTheme = localStorage.getItem('schmux-theme');
        if (savedTheme) {
            document.documentElement.setAttribute('data-theme', savedTheme);
            this.updateToggleIcon(savedTheme);
        } else if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
            // Use system preference as default
            document.documentElement.setAttribute('data-theme', 'dark');
            this.updateToggleIcon('dark');
        }

        // Set up theme toggle button
        const themeToggle = document.getElementById('themeToggle');
        if (themeToggle) {
            themeToggle.addEventListener('click', () => this.toggle());
        }
    },

    toggle() {
        const currentTheme = document.documentElement.getAttribute('data-theme');
        const newTheme = currentTheme === 'dark' ? 'light' : 'dark';

        document.documentElement.setAttribute('data-theme', newTheme);
        localStorage.setItem('schmux-theme', newTheme);
        this.updateToggleIcon(newTheme);
    },

    updateToggleIcon(theme) {
        const themeToggle = document.getElementById('themeToggle');
        if (!themeToggle) return;

        const icon = themeToggle.querySelector('span');
        if (icon) {
            icon.className = theme === 'dark' ? 'icon-moon' : 'icon-sun';
        }
    }
};

// Initialize theme manager
document.addEventListener('DOMContentLoaded', () => {
    ThemeManager.init();
});

// Utility functions
function formatTimestamp(timestamp) {
    const date = new Date(timestamp);
    return date.toLocaleString();
}

function copyToClipboard(text) {
    navigator.clipboard.writeText(text).catch(err => {
        console.error('Failed to copy:', err);
    });
}

// WebSocket connection for terminal streaming
class TerminalStream {
    constructor(sessionId, container) {
        this.sessionId = sessionId;
        this.container = container;
        this.ws = null;
        this.connected = false;
        this.paused = false;
    }

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/terminal/${this.sessionId}`;

        this.ws = new WebSocket(wsUrl);

        this.ws.onopen = () => {
            this.connected = true;
            this.container.classList.remove('disconnected');
            this.container.classList.add('connected');
        };

        this.ws.onmessage = (event) => {
            if (!this.paused) {
                this.updateTerminal(event.data);
            }
        };

        this.ws.onclose = () => {
            this.connected = false;
            this.container.classList.remove('connected');
            this.container.classList.add('disconnected');
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

    updateTerminal(output) {
        this.container.textContent = output;
        // Auto-scroll to bottom
        this.container.scrollTop = this.container.scrollHeight;
    }
}

// Export for use in HTML pages
window.TerminalStream = TerminalStream;
window.ThemeManager = ThemeManager;
