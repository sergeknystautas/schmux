import { useCallback, useEffect, useRef, type RefObject } from 'react';

// Keys that scroll a focused sidebar row's scroll container. Space is absent:
// sidebar rows treat it as activation and preventDefault it.
const SCROLL_KEYS = new Set(['PageUp', 'PageDown', 'Home', 'End', 'ArrowUp', 'ArrowDown']);

type Options = {
  /** The sidebar's scroll container (`.nav-workspaces`). */
  containerRef: RefObject<HTMLElement | null>;
  /** The active workspace row, or null when no workspace is active. */
  activeRef: RefObject<HTMLElement | null>;
  activeWorkspaceId: string | null;
  /** Identity change means the list reordered. */
  sortedWorkspaces: readonly unknown[] | undefined;
  /** Bumped by SessionsContext each time a pending navigation is fulfilled. */
  fulfilledNavigationCount: number;
};

/**
 * Keeps the active workspace scrolled into view in the sidebar until the user
 * scrolls the sidebar themselves. Navigation and sidebar row clicks re-arm it.
 *
 * Manual scrolling is inferred from input events (wheel, touchmove, scroll
 * while a mouse/pen button is held, unmodified scroll keys), never from bare
 * `scroll` events — our own smooth scrollIntoView emits those too.
 */
export function useSidebarAutoScroll({
  containerRef,
  activeRef,
  activeWorkspaceId,
  sortedWorkspaces,
  fulfilledNavigationCount,
}: Options): { arm: () => void } {
  const armedRef = useRef(true);

  const scrollToActive = useCallback(() => {
    activeRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }, [activeRef]);

  const arm = useCallback(() => {
    armedRef.current = true;
    scrollToActive();
  }, [scrollToActive]);

  // Navigation re-arms. Also runs on mount, giving the initial scroll.
  useEffect(() => {
    arm();
  }, [arm, activeWorkspaceId, fulfilledNavigationCount]);

  // Reorders follow only while armed.
  useEffect(() => {
    if (armedRef.current) scrollToActive();
  }, [scrollToActive, sortedWorkspaces]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let pointerHeld = false;
    const disarm = () => {
      armedRef.current = false;
    };
    const onPointerDown = (e: PointerEvent) => {
      // Touch is excluded: the browser fires pointercancel when it takes over
      // panning, so touchmove covers touch instead.
      if (e.pointerType === 'mouse' || e.pointerType === 'pen') pointerHeld = true;
    };
    const onPointerRelease = () => {
      pointerHeld = false;
    };
    const onScroll = () => {
      if (pointerHeld) disarm();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      // Cmd/Ctrl+Up/Down is workspace navigation, which re-arms.
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (SCROLL_KEYS.has(e.key)) disarm();
    };

    container.addEventListener('wheel', disarm, { passive: true });
    container.addEventListener('touchmove', disarm, { passive: true });
    container.addEventListener('pointerdown', onPointerDown, { passive: true });
    container.addEventListener('scroll', onScroll, { passive: true });
    container.addEventListener('keydown', onKeyDown);
    window.addEventListener('pointerup', onPointerRelease);
    window.addEventListener('pointercancel', onPointerRelease);
    return () => {
      container.removeEventListener('wheel', disarm);
      container.removeEventListener('touchmove', disarm);
      container.removeEventListener('pointerdown', onPointerDown);
      container.removeEventListener('scroll', onScroll);
      container.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('pointerup', onPointerRelease);
      window.removeEventListener('pointercancel', onPointerRelease);
    };
  }, [containerRef]);

  return { arm };
}
