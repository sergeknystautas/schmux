import { useEffect, useLayoutEffect, useRef } from 'react';

// PagedLogBody owns the scroll container, the bottom sentinel, the loading
// row, the Retry row, and scroll anchoring for one Logs source. Source views
// pass their already-rendered rows as children; this component decides how
// older history is requested and how the scroll position reacts to live
// prepends.

interface PagedLogBodyProps {
  newestItemId?: number;
  itemCount: number;
  hasMore: boolean;
  loadingOlder: boolean;
  historyError: string | null;
  loadOlder: () => void;
  children: React.ReactNode;
}

const AT_TOP_THRESHOLD = 40;

interface Metrics {
  scrollHeight: number;
  scrollTop: number;
  atTop: boolean;
}

export default function PagedLogBody({
  newestItemId,
  itemCount,
  hasMore,
  loadingOlder,
  historyError,
  loadOlder,
  children,
}: PagedLogBodyProps) {
  const bodyRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const metricsRef = useRef<Metrics>({ scrollHeight: 0, scrollTop: 0, atTop: true });
  const previousNewestIdRef = useRef<number | undefined>(undefined);

  // Capture scroll geometry on every scroll event so the next layout effect
  // can correct the viewport if a live prepend happened.
  const onScroll = () => {
    const el = bodyRef.current;
    if (!el) return;
    metricsRef.current = {
      scrollHeight: el.scrollHeight,
      scrollTop: el.scrollTop,
      atTop: el.scrollTop < AT_TOP_THRESHOLD,
    };
  };

  // Expanded log rows change scrollHeight inside child components without
  // changing itemCount or firing a scroll event. Refresh the baseline after
  // those DOM mutations so a later live prepend accounts only for the height
  // inserted at the top. MutationObserver callbacks run after layout effects,
  // so the prepend adjustment itself still sees the pre-prepend baseline.
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const observer = new MutationObserver(() => {
      metricsRef.current = {
        scrollHeight: el.scrollHeight,
        scrollTop: el.scrollTop,
        atTop: el.scrollTop < AT_TOP_THRESHOLD,
      };
    });
    observer.observe(el, { childList: true, subtree: true, characterData: true });
    return () => observer.disconnect();
  }, []);

  // Adjust scrollTop after a prepend; older pages append below the viewport.
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const previous = metricsRef.current;
    const prepended =
      previousNewestIdRef.current !== undefined &&
      newestItemId !== undefined &&
      newestItemId !== previousNewestIdRef.current;

    if (prepended) {
      if (previous.atTop) {
        el.scrollTop = 0;
      } else {
        const delta = el.scrollHeight - previous.scrollHeight;
        el.scrollTop = previous.scrollTop + delta;
      }
    }

    // Refresh metrics for the next iteration.
    metricsRef.current = {
      scrollHeight: el.scrollHeight,
      scrollTop: el.scrollTop,
      atTop: el.scrollTop < AT_TOP_THRESHOLD,
    };
    previousNewestIdRef.current = newestItemId;
  }, [newestItemId, itemCount]);

  // Observe the bottom sentinel and trigger loadOlder when it scrolls into
  // view, unless a page is in flight or the previous attempt errored.
  useEffect(() => {
    if (!hasMore || loadingOlder || historyError) return;
    const sentinel = sentinelRef.current;
    if (!sentinel) return;
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          loadOlder();
        }
      }
    });
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, loadingOlder, historyError, loadOlder]);

  return (
    <div className="logs-body" data-testid="paged-log-body" ref={bodyRef} onScroll={onScroll}>
      {children}
      {historyError ? (
        <div className="logs-history-row logs-history-row--error" data-testid="logs-history-error">
          <span>{historyError}</span>
          <button type="button" className="btn btn--sm" onClick={loadOlder}>
            Retry
          </button>
        </div>
      ) : loadingOlder ? (
        <div className="logs-history-row" role="status">
          <span className="spinner" />
          <span>Loading older logs…</span>
        </div>
      ) : hasMore ? (
        <div className="logs-history-row" ref={sentinelRef} data-testid="logs-history-sentinel">
          <span>⋯ older logs not shown</span>
        </div>
      ) : null}
    </div>
  );
}
