import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { Profiler } from 'react';
import { render } from '@testing-library/react';
import { useClientPerfRoute } from './useClientPerf';
import { clientPerf } from '../lib/clientPerf';

function Shell({ route }: { route: string }) {
  useClientPerfRoute(route);
  return (
    <Profiler
      id="main"
      onRender={(id, phase, duration) => clientPerf.recordCommit(id, phase, duration)}
    >
      <div>{route}</div>
    </Profiler>
  );
}

describe('useClientPerfRoute', () => {
  let now = 2_000_000;
  const frames: FrameRequestCallback[] = [];
  const cancelled = new Set<number>();

  beforeEach(async () => {
    localStorage.clear();
    vi.spyOn(Date, 'now').mockImplementation(() => now);
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      frames.push(cb);
      return frames.length;
    });
    vi.spyOn(window, 'cancelAnimationFrame').mockImplementation((id) => {
      cancelled.add(id);
    });
    clientPerf.setConfigEnabled(true);
    clientPerf.setDevMode(true);
    await clientPerf.start();
  });

  afterEach(() => {
    clientPerf.stop({ clearChat: true });
    frames.length = 0;
    cancelled.clear();
    vi.restoreAllMocks();
  });

  const routeNavs = () => clientPerf.buildFile().navigation.filter((n) => n.kind === 'route');

  it('captures the initial route commit', () => {
    render(<Shell route="/sessions/x" />);
    const navs = routeNavs();
    expect(navs).toHaveLength(1);
    expect(navs[0].route).toBe('/sessions/x');
    expect(navs[0].firstCommitMs).toBe(0);
  });

  it('records route paint after navigation', () => {
    render(<Shell route="/sessions/x" />);
    const navAt = routeNavs()[0].t;
    now += 40;
    frames.shift()!(0);
    expect(routeNavs()[0].paintMs).toBeUndefined(); // the second frame is the painted one
    frames.shift()!(0);
    expect(routeNavs()[0].paintMs).toBe(40);
    expect(routeNavs()[0].t).toBe(navAt);
  });

  it('cancels the previous route paint measurement after navigation', () => {
    const { rerender } = render(<Shell route="/sessions/a" />);
    rerender(<Shell route="/sessions/b" />);
    expect(cancelled.has(1)).toBe(true);

    frames.shift(); // the cancelled A callback; the browser would not invoke it
    now += 40;
    frames.shift()!(0);
    frames.shift()!(0);
    const navs = routeNavs();
    expect(navs[0].paintMs).toBeUndefined();
    expect(navs[1].paintMs).toBe(40);
  });
});
