import { useState, useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import '@xterm/xterm/css/xterm.css';
import { getRemoteHosts } from '../lib/api';
import type { RemoteHost } from '../lib/types';

interface ConnectionProgressModalProps {
  flavorId: string;
  flavorName: string;
  provisioningSessionId: string | null;
  onClose: () => void;
  onConnected: (host: RemoteHost) => void;
}

export default function ConnectionProgressModal({
  flavorId,
  flavorName,
  provisioningSessionId,
  onClose,
  onConnected,
}: ConnectionProgressModalProps) {
  const [status, setStatus] = useState<
    'provisioning' | 'connecting' | 'connected' | 'error' | 'reconnecting'
  >('provisioning');
  const [errorMessage, setErrorMessage] = useState<string>('');
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const activeRef = useRef(true);
  const pollIntervalRef = useRef<number | null>(null);

  // Use ref to avoid stale closure issues with onConnected callback
  const onConnectedRef = useRef(onConnected);
  onConnectedRef.current = onConnected;

  // Initialize terminal and WebSocket
  useEffect(() => {
    if (!provisioningSessionId || !terminalRef.current) return;

    activeRef.current = true;

    // Create xterm instance
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
      },
      rows: 20,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new Unicode11Addon());
    term.unicode.activeVersion = '11';
    term.open(terminalRef.current);
    fitAddon.fit();

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    // Focus the terminal so keyboard input goes to it immediately
    term.focus();

    // Connect WebSocket
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/provision/${provisioningSessionId}`;
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.binaryType = 'arraybuffer';

    ws.onopen = () => {
      term.write('\r\n\x1b[32mConnecting to remote host...\x1b[0m\r\n\r\n');
      // Send initial terminal size so the PTY matches the browser terminal
      const dims = fitAddon.proposeDimensions();
      if (dims && dims.cols > 0 && dims.rows > 0) {
        ws.send(
          JSON.stringify({
            type: 'resize',
            data: JSON.stringify({ cols: dims.cols, rows: dims.rows }),
          })
        );
      }
    };

    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        const data = new Uint8Array(event.data);
        term.write(data);
      } else if (typeof event.data === 'string') {
        term.write(event.data);
      }
    };

    ws.onerror = (err) => {
      console.error('WebSocket error:', err);
      term.write('\r\n\x1b[31mWebSocket connection error\x1b[0m\r\n');
    };

    ws.onclose = () => {
      term.write('\r\n\x1b[33mConnection closed\x1b[0m\r\n');
    };

    // Send terminal input to WebSocket
    const disposable = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data));
      }
    });

    // Send resize events to WebSocket so PTY matches browser terminal
    const resizeDisposable = term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN && cols > 0 && rows > 0) {
        ws.send(
          JSON.stringify({
            type: 'resize',
            data: JSON.stringify({ cols, rows }),
          })
        );
      }
    });

    // Poll for connection status
    const checkStatus = async () => {
      if (!activeRef.current) return;

      try {
        const hosts = await getRemoteHosts();
        // Match by provisioning_session_id to find the exact connection we started.
        // Fallback: any active host for this flavor, then any host at all (to catch failures).
        const host =
          hosts.find((h) => h.provisioning_session_id === provisioningSessionId) ||
          hosts.find(
            (h) => h.flavor_id === flavorId && h.status !== 'disconnected' && h.status !== 'expired'
          ) ||
          hosts.find((h) => h.flavor_id === flavorId);

        if (host) {
          if (host.status === 'connected') {
            setStatus('connected');
            if (pollIntervalRef.current) {
              clearInterval(pollIntervalRef.current);
              pollIntervalRef.current = null;
            }
            // Small delay to show success before closing
            setTimeout(() => {
              if (activeRef.current) {
                onConnectedRef.current(host);
              }
            }, 1000);
          } else if (host.status === 'connecting') {
            setStatus('connecting');
          } else if (host.status === 'reconnecting') {
            setStatus('reconnecting');
          } else if (host.status === 'disconnected' || host.status === 'expired') {
            setStatus('error');
            setErrorMessage(`Connection failed: host ${host.status}`);
            if (pollIntervalRef.current) {
              clearInterval(pollIntervalRef.current);
              pollIntervalRef.current = null;
            }
          }
        }
      } catch (err) {
        console.error('Failed to poll host status:', err);
      }
    };

    checkStatus();
    pollIntervalRef.current = window.setInterval(checkStatus, 1000);

    // Cleanup
    return () => {
      activeRef.current = false;
      disposable.dispose();
      resizeDisposable.dispose();
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
      if (xtermRef.current) {
        xtermRef.current.dispose();
        xtermRef.current = null;
      }
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
    };
  }, [provisioningSessionId, flavorId]);

  // Handle window resize
  useEffect(() => {
    const handleResize = () => {
      if (fitAddonRef.current) {
        fitAddonRef.current.fit();
      }
    };

    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  const getStatusMessage = () => {
    switch (status) {
      case 'provisioning':
        return 'Provisioning remote host...';
      case 'connecting':
        return 'Connecting to host...';
      case 'reconnecting':
        return 'Reconnecting to host...';
      case 'connected':
        return 'Connected!';
      case 'error':
        return errorMessage || 'Connection failed';
    }
  };

  const getStatusIcon = () => {
    const size = 24;
    switch (status) {
      case 'provisioning':
        return (
          <div
            className="spinner"
            style={{
              width: `${size}px`,
              height: `${size}px`,
              marginRight: 'var(--spacing-sm)',
            }}
          />
        );
      case 'connecting':
        return (
          <div
            className="spinner"
            style={{
              width: `${size}px`,
              height: `${size}px`,
              marginRight: 'var(--spacing-sm)',
            }}
          />
        );
      case 'reconnecting':
        return (
          <svg
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-warning)"
            strokeWidth="2"
            style={{ marginRight: 'var(--spacing-sm)' }}
          >
            <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
            <path d="M7 11V7a5 5 0 0 1 10 0v4" />
          </svg>
        );
      case 'connected':
        return (
          <svg
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-success)"
            strokeWidth="2"
            style={{ marginRight: 'var(--spacing-sm)' }}
          >
            <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
            <polyline points="22 4 12 14.01 9 11.01" />
          </svg>
        );
      case 'error':
        return (
          <svg
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-error)"
            strokeWidth="2"
            style={{ marginRight: 'var(--spacing-sm)' }}
          >
            <circle cx="12" cy="12" r="10" />
            <line x1="15" y1="9" x2="9" y2="15" />
            <line x1="9" y1="9" x2="15" y2="15" />
          </svg>
        );
    }
  };

  if (!provisioningSessionId) {
    return (
      <div className="modal-overlay" onClick={onClose}>
        <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '500px' }}>
          <div
            className="modal__header"
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}
          >
            <h2 className="modal__title" style={{ margin: 0, flex: 1 }}>
              Connecting to {flavorName}
            </h2>
            <div
              onClick={onClose}
              style={{
                cursor: 'pointer',
                padding: '4px',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                borderRadius: 'var(--radius-sm)',
                transition: 'background-color 0.2s, opacity 0.2s',
                opacity: 0.6,
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.opacity = '1';
                e.currentTarget.style.backgroundColor =
                  'var(--color-bg-hover, rgba(255, 255, 255, 0.1))';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.opacity = '0.6';
                e.currentTarget.style.backgroundColor = 'transparent';
              }}
            >
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
              >
                <line x1="18" y1="6" x2="6" y2="18" />
                <line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </div>
          </div>
          <div
            className="modal__body"
            style={{ padding: 'var(--spacing-lg)', textAlign: 'center' }}
          >
            <div className="spinner" style={{ margin: '0 auto var(--spacing-md)' }} />
            <p>Starting connection...</p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal"
        onClick={(e) => e.stopPropagation()}
        style={{ maxWidth: '900px', maxHeight: '80vh' }}
      >
        <div
          className="modal__header"
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--spacing-sm)',
              flex: 1,
            }}
          >
            {getStatusIcon()}
            <div>
              <h2 className="modal__title" style={{ margin: 0 }}>
                {flavorName}
              </h2>
              <p
                style={{
                  margin: '4px 0 0 0',
                  fontSize: '0.875rem',
                  color: 'var(--color-text-muted)',
                }}
              >
                {getStatusMessage()}
              </p>
            </div>
          </div>
          <div
            onClick={onClose}
            style={{
              cursor: 'pointer',
              padding: '4px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: 'var(--radius-sm)',
              transition: 'background-color 0.2s, opacity 0.2s',
              opacity: 0.6,
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.opacity = '1';
              e.currentTarget.style.backgroundColor =
                'var(--color-bg-hover, rgba(255, 255, 255, 0.1))';
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.opacity = '0.6';
              e.currentTarget.style.backgroundColor = 'transparent';
            }}
          >
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
            >
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </div>
        </div>

        <div className="modal__body" style={{ padding: 'var(--spacing-md)' }}>
          <div
            ref={terminalRef}
            style={{
              backgroundColor: '#1e1e1e',
              borderRadius: 'var(--radius-sm)',
              padding: 'var(--spacing-xs)',
              minHeight: '400px',
            }}
          />
        </div>
      </div>
    </div>
  );
}
