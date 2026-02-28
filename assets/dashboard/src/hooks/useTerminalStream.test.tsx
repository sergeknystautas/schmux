import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import React, { useImperativeHandle, forwardRef } from 'react';

// Mock TerminalStream before importing the hook
const mockConnect = vi.fn();
const mockDisconnect = vi.fn();
const mockInitializedPromise = Promise.resolve(null);

vi.mock('../lib/terminalStream', () => {
  return {
    default: vi.fn().mockImplementation(function (this: Record<string, unknown>) {
      this.initialized = mockInitializedPromise;
      this.connect = mockConnect;
      this.disconnect = mockDisconnect;
    }),
  };
});

import { useTerminalStream } from './useTerminalStream';
import TerminalStream from '../lib/terminalStream';

// Test wrapper that renders a real div so the containerRef gets attached
type TestHandle = {
  streamRef: ReturnType<typeof useTerminalStream>['streamRef'];
};

const TestTerminal = forwardRef<TestHandle, { sessionId: string | null | undefined }>(
  ({ sessionId }, ref) => {
    const { containerRef, streamRef } = useTerminalStream({ sessionId });
    useImperativeHandle(ref, () => ({ streamRef }), [streamRef]);
    return <div ref={containerRef} data-testid="terminal-container" />;
  }
);
TestTerminal.displayName = 'TestTerminal';

describe('useTerminalStream', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('does not create a stream when sessionId is null', () => {
    render(<TestTerminal sessionId={null} />);
    expect(TerminalStream).not.toHaveBeenCalled();
  });

  it('does not create a stream when sessionId is undefined', () => {
    render(<TestTerminal sessionId={undefined} />);
    expect(TerminalStream).not.toHaveBeenCalled();
  });

  it('creates a stream and connects when sessionId is provided', async () => {
    const { unmount } = render(<TestTerminal sessionId="test-session" />);

    await act(async () => {
      await mockInitializedPromise;
    });

    expect(TerminalStream).toHaveBeenCalledWith(
      'test-session',
      expect.any(HTMLDivElement),
      expect.objectContaining({ followTail: true })
    );
    expect(mockConnect).toHaveBeenCalled();

    // Cleanup should disconnect
    unmount();
    expect(mockDisconnect).toHaveBeenCalled();
  });

  it('disconnects when sessionId becomes null', async () => {
    const { rerender } = render(<TestTerminal sessionId="session-1" />);

    await act(async () => {
      await mockInitializedPromise;
    });

    expect(TerminalStream).toHaveBeenCalledTimes(1);

    // Set sessionId to null — cleanup should disconnect
    rerender(<TestTerminal sessionId={null} />);
    expect(mockDisconnect).toHaveBeenCalled();
  });

  it('recreates stream when sessionId changes', async () => {
    const { rerender } = render(<TestTerminal sessionId="session-1" />);

    await act(async () => {
      await mockInitializedPromise;
    });

    expect(TerminalStream).toHaveBeenCalledTimes(1);

    // Change sessionId — should disconnect old and create new
    rerender(<TestTerminal sessionId="session-2" />);

    await act(async () => {
      await mockInitializedPromise;
    });

    expect(mockDisconnect).toHaveBeenCalledTimes(1);
    expect(TerminalStream).toHaveBeenCalledTimes(2);
    expect(TerminalStream).toHaveBeenLastCalledWith(
      'session-2',
      expect.any(HTMLDivElement),
      expect.any(Object)
    );
  });

  it('exposes streamRef with current TerminalStream instance', async () => {
    const ref = React.createRef<TestHandle>();
    render(<TestTerminal ref={ref} sessionId="test-session" />);

    await act(async () => {
      await mockInitializedPromise;
    });

    expect(ref.current?.streamRef.current).toBeTruthy();
    expect(ref.current?.streamRef.current?.connect).toBeDefined();
  });
});
