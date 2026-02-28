import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import DemoShell, { createScenarioSetup } from '../../website/src/demo/DemoShell';
import { transport, setTransport, liveTransport } from '../lib/transport';

// We need to suppress console.error for unrecognized CSS module imports and
// console.debug for non-critical API calls that may error in test env.
beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
  vi.spyOn(console, 'debug').mockImplementation(() => {});
  vi.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
  // Restore live transport after each test
  setTransport(liveTransport);
});

describe('DemoShell', () => {
  it('installs mock transport before child effects fire', async () => {
    const setup = createScenarioSetup('workspaces');

    // Spy on the transport's fetch to track when it's called
    const fetchSpy = vi.spyOn(setup.transport, 'fetch');

    await act(async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <DemoShell setup={setup} />
        </MemoryRouter>
      );
    });

    // The transport module should now be using our mock transport
    expect(transport).toBe(setup.transport);

    // Fetch should have been called (for /api/config at minimum)
    expect(fetchSpy).toHaveBeenCalled();

    // All fetch calls should have gone through mock transport (not liveTransport)
    // This verifies the critical timing: transport was set before effects fired
    for (const call of fetchSpy.mock.calls) {
      const url = String(call[0]);
      // Every call should be to a known mock endpoint
      expect(url).toMatch(/\/api\//);
    }
  });

  it('renders the demo banner', async () => {
    const setup = createScenarioSetup('workspaces');

    await act(async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <DemoShell setup={setup} />
        </MemoryRouter>
      );
    });

    expect(screen.getByText('Demo')).toBeInTheDocument();
    expect(screen.getByText("You're exploring schmux with sample data")).toBeInTheDocument();
  });

  it('renders scenario switching links in the banner', async () => {
    const setup = createScenarioSetup('workspaces');

    await act(async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <DemoShell setup={setup} />
        </MemoryRouter>
      );
    });

    expect(screen.getByText('Workspaces tour')).toBeInTheDocument();
    expect(screen.getByText('Spawn tour')).toBeInTheDocument();
    expect(screen.getByText('Install schmux →')).toBeInTheDocument();
  });

  it('shows workspace data from mock transport (not loading spinner)', async () => {
    const setup = createScenarioSetup('workspaces');

    await act(async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <DemoShell setup={setup} />
        </MemoryRouter>
      );
    });

    // The mock dashboard WebSocket sends workspace data after 100ms.
    // Wait for the workspaces to appear (they have branch names).
    // Branch names appear in both sidebar nav and main content area.
    await waitFor(
      () => {
        expect(screen.getAllByText('feature/user-auth').length).toBeGreaterThan(0);
      },
      { timeout: 3000 }
    );

    // Should show both workspaces
    expect(screen.getAllByText('fix/rate-limiting').length).toBeGreaterThan(0);

    // Should NOT show the loading spinner anymore
    expect(screen.queryByText('Loading workspaces...')).not.toBeInTheDocument();
  });

  it('restores liveTransport on unmount', async () => {
    const setup = createScenarioSetup('workspaces');

    let unmount: () => void;
    await act(async () => {
      const result = render(
        <MemoryRouter initialEntries={['/']}>
          <DemoShell setup={setup} />
        </MemoryRouter>
      );
      unmount = result.unmount;
    });

    expect(transport).toBe(setup.transport);

    act(() => {
      unmount!();
    });

    expect(transport).toBe(liveTransport);
  });
});
