import { useEffect, useRef } from 'react';
import TerminalStream from '../lib/terminalStream';

type TerminalStreamOptions = {
  /** tmux session ID to connect to (null/undefined = don't connect) */
  sessionId: string | undefined | null;
  /** When true, the terminal auto-scrolls to the bottom on new output */
  followTail?: boolean;
  /** Called when the user scrolls away from / back to the bottom */
  onResume?: (showing: boolean) => void;
  /** Called on WebSocket status changes */
  onStatusChange?: (status: 'connected' | 'disconnected' | 'reconnecting' | 'error') => void;
  /** Called when multi-line selection changes */
  onSelectedLinesChange?: (lines: string[]) => void;
  /** Strip clear-screen sequences to preserve scrollback. Default: true */
  stripClearScreen?: boolean;
  /** Use WebGL renderer for GPU-accelerated rendering. Default: true */
  useWebGL?: boolean;
};

/**
 * Shared hook that manages a TerminalStream lifecycle: create, initialize,
 * connect, and clean up on unmount or sessionId change.
 *
 * Returns a ref to the current TerminalStream (or null) and a ref callback
 * that should be passed as `ref` to the terminal container div.
 *
 * Usage:
 *   const { containerRef, streamRef } = useTerminalStream({ sessionId });
 *   return <div ref={containerRef} className="terminal-container" />;
 */
export function useTerminalStream(options: TerminalStreamOptions) {
  const {
    sessionId,
    followTail = true,
    onResume,
    onStatusChange,
    onSelectedLinesChange,
    stripClearScreen,
    useWebGL,
  } = options;
  const containerRef = useRef<HTMLDivElement | null>(null);
  const streamRef = useRef<TerminalStream | null>(null);

  useEffect(() => {
    if (!sessionId || !containerRef.current) return;

    const stream = new TerminalStream(sessionId, containerRef.current, {
      followTail,
      onResume,
      onStatusChange,
      onSelectedLinesChange,
      stripClearScreen,
      useWebGL,
    });
    streamRef.current = stream;

    stream.initialized.then(() => {
      stream.connect();
    });

    return () => {
      stream.disconnect();
      streamRef.current = null;
    };
    // Re-create when sessionId changes. Callback refs are stable from the
    // caller side (or should be memoized by the caller).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  return { containerRef, streamRef };
}
