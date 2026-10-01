import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useSidebarAutoScroll } from './useSidebarAutoScroll';

type Props = {
  activeWorkspaceId: string | null;
  sortedWorkspaces: readonly unknown[] | undefined;
  fulfilledNavigationCount: number;
};

let container: HTMLDivElement;
let row: HTMLDivElement;
let scrollSpy: ReturnType<typeof vi.fn>;
const containerRef = { current: null as HTMLElement | null };
const activeRef = { current: null as HTMLElement | null };

// jsdom's PointerEvent support varies; set pointerType explicitly so the
// test controls exactly what the hook sees.
function pointerDown(target: EventTarget, pointerType: string) {
  const event = new Event('pointerdown', { bubbles: true });
  Object.defineProperty(event, 'pointerType', { value: pointerType });
  target.dispatchEvent(event);
}

function keyDown(target: EventTarget, key: string, init: KeyboardEventInit = {}) {
  target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, ...init }));
}

function setup(initial: Partial<Props> = {}) {
  const initialProps: Props = {
    activeWorkspaceId: 'ws-1',
    sortedWorkspaces: [],
    fulfilledNavigationCount: 0,
    ...initial,
  };
  const hook = renderHook(
    (props: Props) => useSidebarAutoScroll({ containerRef, activeRef, ...props }),
    { initialProps }
  );
  scrollSpy.mockClear(); // mount scrolls; tests assert what happens after
  return hook;
}

beforeEach(() => {
  container = document.createElement('div');
  row = document.createElement('div');
  row.tabIndex = 0;
  container.appendChild(row);
  document.body.appendChild(container);
  scrollSpy = vi.fn();
  row.scrollIntoView = scrollSpy as unknown as HTMLElement['scrollIntoView'];
  containerRef.current = container;
  activeRef.current = row;
});

afterEach(() => {
  container.remove();
});

describe('useSidebarAutoScroll', () => {
  it('scrolls the active row into view on mount', () => {
    renderHook(() =>
      useSidebarAutoScroll({
        containerRef,
        activeRef,
        activeWorkspaceId: 'ws-1',
        sortedWorkspaces: [],
        fulfilledNavigationCount: 0,
      })
    );
    expect(scrollSpy).toHaveBeenCalledWith({ behavior: 'smooth', block: 'nearest' });
  });

  it('follows reorders while armed', () => {
    const { rerender } = setup();
    rerender({ activeWorkspaceId: 'ws-1', sortedWorkspaces: [], fulfilledNavigationCount: 0 });
    expect(scrollSpy).toHaveBeenCalledTimes(1);
  });

  const disarmCases: Array<[string, () => void]> = [
    ['wheel', () => container.dispatchEvent(new Event('wheel', { bubbles: true }))],
    ['touchmove', () => container.dispatchEvent(new Event('touchmove', { bubbles: true }))],
    [
      'scroll while mouse held',
      () => {
        pointerDown(row, 'mouse');
        container.dispatchEvent(new Event('scroll'));
      },
    ],
    [
      'scroll while pen held',
      () => {
        pointerDown(row, 'pen');
        container.dispatchEvent(new Event('scroll'));
      },
    ],
    ...['PageUp', 'PageDown', 'Home', 'End', 'ArrowUp', 'ArrowDown'].map(
      (key): [string, () => void] => [`key ${key}`, () => keyDown(row, key)]
    ),
  ];

  it.each(disarmCases)('%s disarms: a later reorder does not scroll', (_name, act_) => {
    const { rerender } = setup();
    act(act_);
    rerender({ activeWorkspaceId: 'ws-1', sortedWorkspaces: [], fulfilledNavigationCount: 0 });
    expect(scrollSpy).not.toHaveBeenCalled();
  });

  const nonDisarmCases: Array<[string, () => void]> = [
    ['Cmd+ArrowDown', () => keyDown(row, 'ArrowDown', { metaKey: true })],
    ['Ctrl+ArrowUp', () => keyDown(row, 'ArrowUp', { ctrlKey: true })],
    ['Alt+PageDown', () => keyDown(row, 'PageDown', { altKey: true })],
    ['does not treat Space as a scroll key', () => keyDown(row, ' ')],
    [
      'touch pointerdown + scroll does not disarm',
      () => {
        pointerDown(row, 'touch');
        container.dispatchEvent(new Event('scroll'));
      },
    ],
    [
      'scroll after mouse released',
      () => {
        pointerDown(row, 'mouse');
        window.dispatchEvent(new Event('pointerup'));
        container.dispatchEvent(new Event('scroll'));
      },
    ],
    [
      'scroll after pointer cancelled',
      () => {
        pointerDown(row, 'mouse');
        window.dispatchEvent(new Event('pointercancel'));
        container.dispatchEvent(new Event('scroll'));
      },
    ],
    ['ignores scroll keys outside the sidebar', () => keyDown(document.body, 'PageDown')],
    [
      'ignores wheel outside the sidebar',
      () => document.body.dispatchEvent(new Event('wheel', { bubbles: true })),
    ],
  ];

  it.each(nonDisarmCases)('%s: stays armed', (_name, act_) => {
    const { rerender } = setup();
    act(act_);
    rerender({ activeWorkspaceId: 'ws-1', sortedWorkspaces: [], fulfilledNavigationCount: 0 });
    expect(scrollSpy).toHaveBeenCalledTimes(1);
  });

  it('re-arms when the active workspace changes', () => {
    const { rerender } = setup();
    act(() => container.dispatchEvent(new Event('wheel', { bubbles: true })));
    const sorted: unknown[] = [];
    rerender({ activeWorkspaceId: 'ws-2', sortedWorkspaces: sorted, fulfilledNavigationCount: 0 });
    expect(scrollSpy).toHaveBeenCalled();
    scrollSpy.mockClear();
    rerender({ activeWorkspaceId: 'ws-2', sortedWorkspaces: [], fulfilledNavigationCount: 0 });
    expect(scrollSpy).toHaveBeenCalledTimes(1); // armed again: reorder follows
  });

  it('re-arms when a pending navigation is fulfilled', () => {
    const { rerender } = setup();
    act(() => container.dispatchEvent(new Event('wheel', { bubbles: true })));
    const sorted: unknown[] = [];
    rerender({ activeWorkspaceId: 'ws-1', sortedWorkspaces: sorted, fulfilledNavigationCount: 1 });
    expect(scrollSpy).toHaveBeenCalled();
    scrollSpy.mockClear();
    rerender({ activeWorkspaceId: 'ws-1', sortedWorkspaces: [], fulfilledNavigationCount: 1 });
    expect(scrollSpy).toHaveBeenCalledTimes(1);
  });

  it('re-arms and scrolls on arm()', () => {
    const { result, rerender } = setup();
    act(() => container.dispatchEvent(new Event('wheel', { bubbles: true })));
    act(() => result.current.arm());
    expect(scrollSpy).toHaveBeenCalledTimes(1);
    rerender({ activeWorkspaceId: 'ws-1', sortedWorkspaces: [], fulfilledNavigationCount: 0 });
    expect(scrollSpy).toHaveBeenCalledTimes(2);
  });

  it('handles no active row, then scrolls the next active row', () => {
    activeRef.current = null;
    const { result, rerender } = setup({ activeWorkspaceId: null });
    expect(() => act(() => result.current.arm())).not.toThrow();
    rerender({ activeWorkspaceId: null, sortedWorkspaces: [], fulfilledNavigationCount: 0 });
    activeRef.current = row;
    rerender({ activeWorkspaceId: 'ws-1', sortedWorkspaces: [], fulfilledNavigationCount: 0 });
    expect(scrollSpy).toHaveBeenCalled();
  });

  it('removes its listeners on unmount', () => {
    const { unmount } = setup();
    const removeContainer = vi.spyOn(container, 'removeEventListener');
    const removeWindow = vi.spyOn(window, 'removeEventListener');
    unmount();
    const containerTypes = removeContainer.mock.calls.map((c) => c[0]);
    expect(containerTypes).toEqual(
      expect.arrayContaining(['wheel', 'touchmove', 'pointerdown', 'scroll', 'keydown'])
    );
    const windowTypes = removeWindow.mock.calls.map((c) => c[0]);
    expect(windowTypes).toEqual(expect.arrayContaining(['pointerup', 'pointercancel']));
  });
});
