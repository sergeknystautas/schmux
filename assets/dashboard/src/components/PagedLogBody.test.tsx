import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import PagedLogBody from './PagedLogBody';

// Controllable IntersectionObserver — tests call triggerIntersect() to
// simulate the sentinel scrolling into view.
let intersectCallbacks: Array<(entries: Array<{ isIntersecting: boolean }>) => void> = [];
class MockIntersectionObserver {
  private cb: (entries: Array<{ isIntersecting: boolean }>) => void;
  constructor(cb: (entries: Array<{ isIntersecting: boolean }>) => void) {
    this.cb = cb;
    intersectCallbacks.push(cb);
  }
  observe() {}
  unobserve() {}
  disconnect() {
    intersectCallbacks = intersectCallbacks.filter((c) => c !== this.cb);
  }
}
function triggerIntersect() {
  for (const cb of intersectCallbacks) cb([{ isIntersecting: true }]);
}

let mutationCallbacks: Array<() => void> = [];
class MockMutationObserver {
  private cb: () => void;
  constructor(cb: () => void) {
    this.cb = cb;
    mutationCallbacks.push(cb);
  }
  observe() {}
  disconnect() {
    mutationCallbacks = mutationCallbacks.filter((cb) => cb !== this.cb);
  }
}
function triggerMutation() {
  for (const cb of mutationCallbacks) cb();
}

// Scroll geometry: the test sets these before triggering events.
let scrollTopValue = 0;
let clientHeightValue = 0;
let scrollHeightValue = 0;
function setScrollGeometry({
  scrollTop,
  scrollHeight,
  clientHeight,
}: {
  scrollTop?: number;
  scrollHeight?: number;
  clientHeight?: number;
}) {
  if (scrollTop !== undefined) scrollTopValue = scrollTop;
  if (scrollHeight !== undefined) scrollHeightValue = scrollHeight;
  if (clientHeight !== undefined) clientHeightValue = clientHeight;
}

beforeEach(() => {
  intersectCallbacks = [];
  mutationCallbacks = [];
  scrollTopValue = 0;
  clientHeightValue = 0;
  scrollHeightValue = 0;
  vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
  vi.stubGlobal('MutationObserver', MockMutationObserver);
  Object.defineProperty(window.HTMLElement.prototype, 'scrollTop', {
    configurable: true,
    get() {
      return scrollTopValue;
    },
    set(v: number) {
      scrollTopValue = v;
    },
  });
  Object.defineProperty(window.HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get() {
      return clientHeightValue;
    },
  });
  Object.defineProperty(window.HTMLElement.prototype, 'scrollHeight', {
    configurable: true,
    get() {
      return scrollHeightValue;
    },
  });
});

function getBody(container: HTMLElement): HTMLElement {
  return container.querySelector('[data-testid="paged-log-body"]') as HTMLElement;
}

const baseProps = {
  itemCount: 0,
  hasMore: false,
  loadingOlder: false,
  historyError: null as string | null,
  loadOlder: () => {},
};

describe('PagedLogBody', () => {
  it('loads once when the history sentinel intersects', () => {
    const loadOlder = vi.fn();
    const { container, rerender } = render(
      <PagedLogBody
        newestItemId={2}
        itemCount={2}
        hasMore
        loadingOlder={false}
        historyError={null}
        loadOlder={loadOlder}
      >
        <div>new</div>
        <div>old</div>
      </PagedLogBody>
    );

    triggerIntersect();
    expect(loadOlder).toHaveBeenCalledTimes(1);

    rerender(
      <PagedLogBody
        newestItemId={2}
        itemCount={2}
        hasMore
        loadingOlder
        historyError={null}
        loadOlder={loadOlder}
      >
        <div>new</div>
        <div>old</div>
      </PagedLogBody>
    );

    // Second intersect during in-flight load should NOT re-fire.
    triggerIntersect();
    expect(loadOlder).toHaveBeenCalledTimes(1);
  });

  it('does not observe or call loadOlder when hasMore is false', () => {
    const loadOlder = vi.fn();
    render(
      <PagedLogBody
        newestItemId={1}
        itemCount={1}
        hasMore={false}
        loadingOlder={false}
        historyError={null}
        loadOlder={loadOlder}
      >
        <div>only</div>
      </PagedLogBody>
    );

    triggerIntersect();
    expect(loadOlder).not.toHaveBeenCalled();
  });

  it('renders the loading spinner while loadingOlder is true', () => {
    const { container } = render(
      <PagedLogBody
        newestItemId={1}
        itemCount={1}
        hasMore
        loadingOlder
        historyError={null}
        loadOlder={vi.fn()}
      >
        <div>only</div>
      </PagedLogBody>
    );
    expect(container.querySelector('[role="status"]')).not.toBeNull();
  });

  it('renders historyError with a Retry button that calls loadOlder', () => {
    const loadOlder = vi.fn();
    const { container, getByText } = render(
      <PagedLogBody
        newestItemId={1}
        itemCount={1}
        hasMore
        loadingOlder={false}
        historyError="boom"
        loadOlder={loadOlder}
      >
        <div>only</div>
      </PagedLogBody>
    );
    expect(container.textContent).toContain('boom');
    const btn = getByText('Retry') as HTMLButtonElement;
    fireEvent.click(btn);
    expect(loadOlder).toHaveBeenCalledTimes(1);
  });

  it('preserves scrollTop=0 when a new item prepends while at the top', () => {
    const { rerender, getByTestId } = render(
      <PagedLogBody
        newestItemId={10}
        itemCount={3}
        hasMore
        loadingOlder={false}
        historyError={null}
        loadOlder={vi.fn()}
      >
        <div>a</div>
      </PagedLogBody>
    );

    const body = getByTestId('paged-log-body');
    setScrollGeometry({ scrollTop: 0, clientHeight: 100, scrollHeight: 500 });
    fireEvent.scroll(body);
    expect(scrollTopValue).toBe(0);

    // Prepend a newer item — the body should keep scrollTop=0 because the
    // user was already at the top.
    rerender(
      <PagedLogBody
        newestItemId={20}
        itemCount={4}
        hasMore
        loadingOlder={false}
        historyError={null}
        loadOlder={vi.fn()}
      >
        <div>a</div>
      </PagedLogBody>
    );
    expect(scrollTopValue).toBe(0);
  });

  it('offsets scrollTop by the height delta when prepending away from top', () => {
    const { rerender, getByTestId } = render(
      <PagedLogBody
        newestItemId={5}
        itemCount={2}
        hasMore
        loadingOlder={false}
        historyError={null}
        loadOlder={vi.fn()}
      >
        <div>a</div>
      </PagedLogBody>
    );
    const body = getByTestId('paged-log-body');

    // Initial geometry: at scrollTop=200 (away from top), scrollHeight=1000.
    setScrollGeometry({ scrollTop: 200, clientHeight: 200, scrollHeight: 1000 });
    fireEvent.scroll(body);
    expect(scrollTopValue).toBe(200);

    // Simulate a prepend that grows scrollHeight by 100. The component reads
    // the new scrollHeight inside useLayoutEffect, where it is then compared
    // against the metricsRef captured at the user's last scroll event.
    setScrollGeometry({ scrollTop: 200, clientHeight: 200, scrollHeight: 1100 });
    rerender(
      <PagedLogBody
        newestItemId={6}
        itemCount={3}
        hasMore
        loadingOlder={false}
        historyError={null}
        loadOlder={vi.fn()}
      >
        <div>a</div>
      </PagedLogBody>
    );
    // ScrollTop should have grown by the height delta (1100 - 1000 = 100).
    expect(scrollTopValue).toBe(300);
  });

  it('does not count an expanded row as live-prepend height', () => {
    const { rerender, getByTestId } = render(
      <PagedLogBody
        newestItemId={5}
        itemCount={2}
        hasMore
        loadingOlder={false}
        historyError={null}
        loadOlder={vi.fn()}
      >
        <div>a</div>
      </PagedLogBody>
    );
    const body = getByTestId('paged-log-body');

    setScrollGeometry({ scrollTop: 200, clientHeight: 200, scrollHeight: 1000 });
    fireEvent.scroll(body);

    // A child row expands without changing itemCount or newestItemId.
    setScrollGeometry({ scrollTop: 200, scrollHeight: 1200 });
    triggerMutation();

    // The subsequent live prepend adds only 50px. Scroll anchoring must use
    // the post-expansion 1200px baseline, not the stale 1000px height.
    setScrollGeometry({ scrollTop: 200, scrollHeight: 1250 });
    rerender(
      <PagedLogBody
        newestItemId={6}
        itemCount={3}
        hasMore
        loadingOlder={false}
        historyError={null}
        loadOlder={vi.fn()}
      >
        <div>a</div>
      </PagedLogBody>
    );
    expect(scrollTopValue).toBe(250);
  });
});
