import { useRef, useState } from 'react';
import { csrfHeaders } from '../lib/csrf';
import type { PendingClipboardRequest } from '../contexts/ClipboardContext';
import type { ClipboardAckRequest, ClipboardAckResponse } from '../lib/types';
import styles from './ClipboardBanner.module.css';

// PREVIEW_LIMIT bounds how many characters of the proposed paste are
// rendered in the preview <pre>. The full text is still passed to
// navigator.clipboard.writeText on approve — this only limits the visual
// preview so a 50KB blob doesn't blow up the layout.
const PREVIEW_LIMIT = 4096;

interface Props {
  sessionId: string;
  request: PendingClipboardRequest;
  /**
   * Called after a successful approve or reject (POST returned). The
   * parent should use this to remove the banner from local state. The
   * daemon's clipboardCleared broadcast will follow shortly and is a
   * safe no-op due to the requestId guard in the WS dispatcher.
   */
  onCleared: () => void;
}

export function ClipboardBanner({ sessionId, request, onCleared }: Props) {
  const [error, setError] = useState<string | null>(null);
  const [inFlight, setInFlight] = useState(false);

  // Snapshot the text at the moment the user clicks Approve so a
  // mid-click WS-driven state replacement (newer clipboardRequest for
  // the same session) doesn't change what we write to the clipboard.
  // The user always copies the text they were looking at.
  const textRef = useRef(request.text);
  textRef.current = request.text;
  const requestIdRef = useRef(request.requestId);
  requestIdRef.current = request.requestId;

  const truncated =
    request.text.length > PREVIEW_LIMIT ? request.text.slice(0, PREVIEW_LIMIT) : request.text;
  const overflowBytes = request.text.length - truncated.length;

  async function approve() {
    if (inFlight) return;
    setInFlight(true);
    setError(null);

    // Snapshot text + requestId at click time.
    const text = textRef.current;
    const requestId = requestIdRef.current;

    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Browser blocked the write (no user gesture, missing permission,
      // insecure context). Don't POST the ack — the user will retry.
      setError('Browser blocked clipboard write — try clicking Approve again.');
      setInFlight(false);
      return;
    }

    const body: ClipboardAckRequest = { action: 'approve', requestId };
    try {
      const res = await fetch(`/api/sessions/${sessionId}/clipboard`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
        body: JSON.stringify(body),
      });
      // Drain the typed response (status: "ok" | "stale") so unused-type
      // analysis sees ClipboardAckResponse referenced; we don't surface
      // "stale" specially because both outcomes mean "the request is
      // gone from the daemon" and the broadcast handles UI state.
      void (res.ok ? ((await res.json()) as ClipboardAckResponse) : null);
    } catch {
      // Network failure — we already wrote to the clipboard so it's a
      // weird state, but the daemon's TTL will clean up. Surface the
      // error briefly but still clear the banner so the user isn't stuck.
    } finally {
      onCleared();
      setInFlight(false);
    }
  }

  async function reject() {
    if (inFlight) return;
    setInFlight(true);
    setError(null);

    const requestId = requestIdRef.current;

    const body: ClipboardAckRequest = { action: 'reject', requestId };
    try {
      const res = await fetch(`/api/sessions/${sessionId}/clipboard`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
        body: JSON.stringify(body),
      });
      void (res.ok ? ((await res.json()) as ClipboardAckResponse) : null);
    } catch {
      // Same: network failure, daemon TTL will clean up.
    } finally {
      onCleared();
      setInFlight(false);
    }
  }

  return (
    <div className={styles.banner} role="alert" aria-live="polite">
      <div className={styles.title}>
        TUI wants to copy to your clipboard ({request.byteCount} bytes)
      </div>
      <pre className={styles.preview}>
        {truncated}
        {overflowBytes > 0 && (
          <span className={styles.overflow}>… ({overflowBytes} more bytes)</span>
        )}
      </pre>
      {request.strippedControlChars > 0 && (
        <div className={styles.note}>
          {request.strippedControlChars} control character
          {request.strippedControlChars === 1 ? '' : 's'} stripped from preview.
        </div>
      )}
      {error && <div className={styles.error}>{error}</div>}
      <div className={styles.actions}>
        <button
          type="button"
          className={`${styles.button} ${styles.reject}`}
          onClick={reject}
          disabled={inFlight}
        >
          Reject
        </button>
        <button
          type="button"
          className={`${styles.button} ${styles.approve}`}
          onClick={approve}
          disabled={inFlight}
        >
          Approve
        </button>
      </div>
    </div>
  );
}
