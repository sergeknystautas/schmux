import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { ClipboardBanner } from './ClipboardBanner';
import type { PendingClipboardRequest } from '../contexts/ClipboardContext';

function makeRequest(overrides: Partial<PendingClipboardRequest> = {}): PendingClipboardRequest {
  return {
    requestId: 'req-1',
    text: 'hello',
    byteCount: 5,
    strippedControlChars: 0,
    ...overrides,
  };
}

const fetchMock = vi.fn();
const writeTextMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  writeTextMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  // Stub navigator.clipboard so the component's writeText call is observable.
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: writeTextMock },
    configurable: true,
  });
  document.cookie = 'schmux_csrf=test-token';
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ClipboardBanner', () => {
  it('renders text, byte count, and no stripped note when count is 0', () => {
    render(
      <ClipboardBanner
        sessionId="s1"
        request={makeRequest({ text: 'hello' })}
        onCleared={vi.fn()}
      />
    );

    expect(screen.getByText(/hello/)).toBeInTheDocument();
    expect(screen.getByText(/5 bytes/)).toBeInTheDocument();
    expect(screen.queryByText(/control character/)).not.toBeInTheDocument();
  });

  it('shows stripped count note when strippedControlChars > 0 (singular)', () => {
    render(
      <ClipboardBanner
        sessionId="s1"
        request={makeRequest({ strippedControlChars: 1 })}
        onCleared={vi.fn()}
      />
    );
    expect(screen.getByText(/1 control character stripped from preview/)).toBeInTheDocument();
  });

  it('shows stripped count note pluralized when > 1', () => {
    render(
      <ClipboardBanner
        sessionId="s1"
        request={makeRequest({ strippedControlChars: 3 })}
        onCleared={vi.fn()}
      />
    );
    expect(screen.getByText(/3 control characters stripped from preview/)).toBeInTheDocument();
  });

  it('uses role="alert" for accessibility', () => {
    render(<ClipboardBanner sessionId="s1" request={makeRequest()} onCleared={vi.fn()} />);
    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('Reject calls API with action=reject and clears banner', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ status: 'ok' }) });
    const onCleared = vi.fn();

    render(<ClipboardBanner sessionId="s1" request={makeRequest()} onCleared={onCleared} />);

    fireEvent.click(screen.getByRole('button', { name: /reject/i }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/sessions/s1/clipboard');
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body).toEqual({ action: 'reject', requestId: 'req-1' });

    // CSRF header included
    const headers = (init as RequestInit).headers as Record<string, string>;
    expect(headers['X-CSRF-Token']).toBe('test-token');
    expect(headers['Content-Type']).toBe('application/json');

    // Reject path must NOT call writeText (no clipboard access)
    expect(writeTextMock).not.toHaveBeenCalled();

    await waitFor(() => expect(onCleared).toHaveBeenCalledTimes(1));
  });

  it('Approve happy path: writeText then POST then onCleared', async () => {
    writeTextMock.mockResolvedValue(undefined);
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ status: 'ok' }) });
    const onCleared = vi.fn();

    render(
      <ClipboardBanner
        sessionId="s1"
        request={makeRequest({ text: 'paste-me' })}
        onCleared={onCleared}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: /approve/i }));

    await waitFor(() => expect(writeTextMock).toHaveBeenCalledWith('paste-me'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/sessions/s1/clipboard');
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body).toEqual({ action: 'approve', requestId: 'req-1' });

    await waitFor(() => expect(onCleared).toHaveBeenCalledTimes(1));
  });

  it('Approve writeText failure: no API call, error shown, banner stays', async () => {
    writeTextMock.mockRejectedValue(new Error('NotAllowed'));
    const onCleared = vi.fn();

    render(<ClipboardBanner sessionId="s1" request={makeRequest()} onCleared={onCleared} />);

    fireEvent.click(screen.getByRole('button', { name: /approve/i }));

    await waitFor(() =>
      expect(screen.getByText(/Browser blocked clipboard write/)).toBeInTheDocument()
    );
    expect(fetchMock).not.toHaveBeenCalled();
    expect(onCleared).not.toHaveBeenCalled();

    // Approve button must be re-enabled so the user can retry
    expect(screen.getByRole('button', { name: /approve/i })).not.toBeDisabled();
  });

  it('truncates preview > PREVIEW_LIMIT (4096) but writes full text to clipboard', async () => {
    writeTextMock.mockResolvedValue(undefined);
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ status: 'ok' }) });

    const big = 'x'.repeat(5000);
    render(
      <ClipboardBanner
        sessionId="s1"
        request={makeRequest({ text: big, byteCount: 5000 })}
        onCleared={vi.fn()}
      />
    );

    // Overflow note ("… (904 more bytes)") should be present
    expect(screen.getByText(/904 more bytes/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /approve/i }));
    await waitFor(() => expect(writeTextMock).toHaveBeenCalled());

    // Full 5000-char text passed to writeText, not the truncated preview
    expect(writeTextMock.mock.calls[0][0]).toHaveLength(5000);
  });

  it('disables both buttons while a request is in flight', async () => {
    let resolveFetch: (v: unknown) => void = () => {};
    fetchMock.mockReturnValue(
      new Promise((resolve) => {
        resolveFetch = resolve;
      })
    );
    writeTextMock.mockResolvedValue(undefined);

    render(<ClipboardBanner sessionId="s1" request={makeRequest()} onCleared={vi.fn()} />);

    fireEvent.click(screen.getByRole('button', { name: /approve/i }));

    await waitFor(() => expect(writeTextMock).toHaveBeenCalled());

    // Both buttons disabled during in-flight POST
    await waitFor(() => expect(screen.getByRole('button', { name: /approve/i })).toBeDisabled());
    expect(screen.getByRole('button', { name: /reject/i })).toBeDisabled();

    await act(async () => {
      resolveFetch({ ok: true, json: async () => ({ status: 'ok' }) });
    });
  });

  it('Approve uses snapshotted text even if request prop changes mid-click', async () => {
    // The textRef snapshot guarantees we copy what the user saw, not a
    // mid-click WS-driven replacement. Simulated by re-rendering with a
    // different text after the click but before writeText resolves.
    let resolveWrite: () => void = () => {};
    writeTextMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveWrite = resolve;
        })
    );
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ status: 'ok' }) });

    const { rerender } = render(
      <ClipboardBanner
        sessionId="s1"
        request={makeRequest({ text: 'original' })}
        onCleared={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: /approve/i }));
    // writeText called with 'original'
    await waitFor(() => expect(writeTextMock).toHaveBeenCalledWith('original'));

    // Mid-flight: parent supplies a new request (e.g., another OSC 52 emit)
    rerender(
      <ClipboardBanner
        sessionId="s1"
        request={makeRequest({ requestId: 'req-2', text: 'replaced' })}
        onCleared={vi.fn()}
      />
    );

    // The already-in-flight call must still be using 'original'.
    // (If we'd dereferenced request.text inside the async path,
    // we'd have copied 'replaced'.)
    expect(writeTextMock).toHaveBeenCalledTimes(1);
    expect(writeTextMock.mock.calls[0][0]).toBe('original');

    await act(async () => {
      resolveWrite();
    });
  });
});
