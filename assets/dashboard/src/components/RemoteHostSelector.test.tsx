import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import RemoteHostSelector from './RemoteHostSelector';
import type { EnvironmentSelection } from './RemoteHostSelector';
import type { RemoteProfileStatus } from '../lib/types';

// Mock API
const mockGetRemoteProfileStatuses = vi.fn<() => Promise<RemoteProfileStatus[]>>();
const mockGetRemoteHosts = vi.fn();
const mockConnectRemoteHost = vi.fn();
const mockReconnectRemoteHost = vi.fn();

vi.mock('../lib/api', () => ({
  getRemoteProfileStatuses: (...args: unknown[]) => mockGetRemoteProfileStatuses(...(args as [])),
  getRemoteHosts: (...args: unknown[]) => mockGetRemoteHosts(...(args as [])),
  connectRemoteHost: (...args: unknown[]) => mockConnectRemoteHost(...args),
  reconnectRemoteHost: (...args: unknown[]) => mockReconnectRemoteHost(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
}));

// Mock toast
const mockToastSuccess = vi.fn();
vi.mock('./ToastProvider', () => ({
  useToast: () => ({
    success: mockToastSuccess,
    error: vi.fn(),
    show: vi.fn(),
  }),
}));

// Mock modal
const mockAlert = vi.fn();
vi.mock('./ModalProvider', () => ({
  useModal: () => ({
    alert: mockAlert,
    confirm: vi.fn(),
  }),
}));

// Mock sessions context
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: [],
  }),
}));

const baseProfile = {
  id: 'profile-od',
  display_name: 'OnDemand',
  vcs: 'hg',
  workspace_path: '/data/users/$USER',
  flavors: [{ flavor: 'od' }],
};

function renderSelector(value: EnvironmentSelection = { type: 'local' }, onChange = vi.fn()) {
  return render(<RemoteHostSelector value={value} onChange={onChange} />);
}

describe('RemoteHostSelector', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // Bug 1: flavor_hosts has empty hosts, handling gracefully
  it('renders without crashing when flavor_hosts has null hosts in API response', async () => {
    mockGetRemoteProfileStatuses.mockResolvedValue([
      {
        profile: baseProfile,
        flavor_hosts: [
          {
            flavor: 'od',
            hosts: null as unknown as RemoteProfileStatus['flavor_hosts'][0]['hosts'],
          },
        ],
      },
    ]);

    renderSelector();

    // Wait for loading to finish and verify the component renders
    await waitFor(() => {
      expect(screen.queryByText('Loading remote hosts...')).not.toBeInTheDocument();
    });

    // The "+ New host" card should still render
    expect(screen.getByText(/New OnDemand host/)).toBeInTheDocument();
  });

  // Bug 1 companion: empty array also works
  it('renders without crashing when flavor_hosts has empty hosts array', async () => {
    mockGetRemoteProfileStatuses.mockResolvedValue([
      {
        profile: baseProfile,
        flavor_hosts: [{ flavor: 'od', hosts: [] }],
      },
    ]);

    renderSelector();

    await waitFor(() => {
      expect(screen.queryByText('Loading remote hosts...')).not.toBeInTheDocument();
    });

    // Should still show the "+ New host" card
    expect(screen.getByText(/New OnDemand host/)).toBeInTheDocument();
  });

  it('shows per-host cards when profile has existing hosts', async () => {
    mockGetRemoteProfileStatuses.mockResolvedValue([
      {
        profile: baseProfile,
        flavor_hosts: [
          {
            flavor: 'od',
            hosts: [
              {
                host_id: 'host-1',
                hostname: 'dev001.example.com',
                status: 'connected',
                connected: true,
              },
              {
                host_id: 'host-2',
                hostname: 'dev002.example.com',
                status: 'disconnected',
                connected: false,
              },
            ],
          },
        ],
      },
    ]);

    renderSelector();

    await waitFor(() => {
      expect(screen.queryByText('Loading remote hosts...')).not.toBeInTheDocument();
    });

    // Hostname appears in both the card title (strong) and HostStatusIndicator,
    // so use getAllByText and verify at least one exists
    expect(screen.getAllByText('dev001.example.com').length).toBeGreaterThan(0);
    expect(screen.getAllByText('dev002.example.com').length).toBeGreaterThan(0);
  });

  it('always shows "+ New host" card with dashed border for every profile', async () => {
    mockGetRemoteProfileStatuses.mockResolvedValue([
      {
        profile: baseProfile,
        flavor_hosts: [
          {
            flavor: 'od',
            hosts: [
              {
                host_id: 'host-1',
                hostname: 'dev001.example.com',
                status: 'connected',
                connected: true,
              },
            ],
          },
        ],
      },
    ]);

    renderSelector();

    await waitFor(() => {
      expect(screen.queryByText('Loading remote hosts...')).not.toBeInTheDocument();
    });

    // Both the existing host and the "+ New host" card should be visible
    const newHostCard = screen.getByText(/New OnDemand host/);
    expect(newHostCard).toBeInTheDocument();

    // The card should have a dashed border style
    const cardElement = newHostCard.closest('[role="button"]');
    expect(cardElement).toBeInTheDocument();
    expect(cardElement?.getAttribute('style')).toContain('dashed');
  });

  // Bug 6: "+ New host" card shows "Provisioning..." during connection
  it('+ New host card shows static "Provision a new instance" text, never "Provisioning..."', async () => {
    mockGetRemoteProfileStatuses.mockResolvedValue([
      {
        profile: baseProfile,
        flavor_hosts: [
          {
            flavor: 'od',
            hosts: [{ host_id: 'host-1', hostname: '', status: 'provisioning', connected: false }],
          },
        ],
      },
    ]);

    renderSelector();

    await waitFor(() => {
      expect(screen.queryByText('Loading remote hosts...')).not.toBeInTheDocument();
    });

    // The "+ New host" card should show static text, not "Provisioning..."
    expect(screen.getByText('Provision a new instance')).toBeInTheDocument();

    // Verify the "+ New host" card text is exactly "Provision a new instance"
    // and NOT the dynamic provisioning spinner from HostStatusIndicator
    const newHostCard = screen.getByText(/New OnDemand host/).closest('[role="button"]');
    expect(newHostCard).toBeInTheDocument();
    // The new host card should NOT contain a HostStatusIndicator
    // (it has static text "Provision a new instance" instead)
    const newHostCardText = newHostCard?.textContent || '';
    expect(newHostCardText).toContain('Provision a new instance');
    expect(newHostCardText).not.toContain('Provisioning...');
  });

  it('connected host card shows hostname and connected status', async () => {
    mockGetRemoteProfileStatuses.mockResolvedValue([
      {
        profile: baseProfile,
        flavor_hosts: [
          {
            flavor: 'od',
            hosts: [
              {
                host_id: 'host-1',
                hostname: 'myhost.example.com',
                status: 'connected',
                connected: true,
              },
            ],
          },
        ],
      },
    ]);

    renderSelector();

    await waitFor(() => {
      expect(screen.queryByText('Loading remote hosts...')).not.toBeInTheDocument();
    });

    // Hostname appears in both the card title (strong) and HostStatusIndicator
    const hostnameElements = screen.getAllByText('myhost.example.com');
    expect(hostnameElements.length).toBeGreaterThan(0);

    // Verify one of them is in a <strong> tag (card title)
    const strongHostname = hostnameElements.find((el) => el.tagName.toLowerCase() === 'strong');
    expect(strongHostname).toBeTruthy();
  });

  it('clicking connected host card calls onChange with profileId', async () => {
    const onChange = vi.fn();
    mockGetRemoteProfileStatuses.mockResolvedValue([
      {
        profile: baseProfile,
        flavor_hosts: [
          {
            flavor: 'od',
            hosts: [
              {
                host_id: 'host-abc',
                hostname: 'myhost.example.com',
                status: 'connected',
                connected: true,
              },
            ],
          },
        ],
      },
    ]);
    mockGetRemoteHosts.mockResolvedValue([
      {
        id: 'host-abc',
        profile_id: 'profile-od',
        flavor: 'od',
        hostname: 'myhost.example.com',
        status: 'connected',
      },
    ]);

    renderSelector({ type: 'local' }, onChange);

    await waitFor(() => {
      expect(screen.queryByText('Loading remote hosts...')).not.toBeInTheDocument();
    });

    // Click the connected host card - find via the <strong> title element
    const hostnameElements = screen.getAllByText('myhost.example.com');
    const strongEl = hostnameElements.find((el) => el.tagName.toLowerCase() === 'strong');
    const hostCard = strongEl?.closest('[role="button"]');
    expect(hostCard).toBeTruthy();

    await act(async () => {
      await userEvent.click(hostCard!);
    });

    // onChange should be called with the host selection including profileId
    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'remote',
          profileId: 'profile-od',
          hostId: 'host-abc',
        })
      );
    });
  });

  describe('persistent host', () => {
    const persistentProfile = {
      id: 'profile-persistent',
      display_name: 'Dev Server',
      vcs: 'git',
      workspace_path: '',
      host_type: 'persistent',
      flavors: [],
    };

    it('shows "Spawn on" instead of "New host" for persistent profiles', async () => {
      mockGetRemoteProfileStatuses.mockResolvedValue([
        {
          profile: persistentProfile,
          flavor_hosts: [{ flavor: '', hosts: [] }],
        },
      ]);

      renderSelector();
      await waitFor(() => {
        expect(screen.getByText(/Spawn on Dev Server/)).toBeInTheDocument();
      });
    });

    it('shows "Connect and create workspace" when not connected', async () => {
      mockGetRemoteProfileStatuses.mockResolvedValue([
        {
          profile: persistentProfile,
          flavor_hosts: [{ flavor: '', hosts: [] }],
        },
      ]);

      renderSelector();
      await waitFor(() => {
        expect(screen.getByText('Connect and create workspace')).toBeInTheDocument();
      });
    });

    it('shows "Connected" message when host is connected', async () => {
      mockGetRemoteProfileStatuses.mockResolvedValue([
        {
          profile: persistentProfile,
          flavor_hosts: [
            {
              flavor: '',
              hosts: [
                {
                  host_id: 'host-1',
                  hostname: 'dev.example.com',
                  status: 'connected',
                  connected: true,
                },
              ],
            },
          ],
        },
      ]);

      renderSelector();
      await waitFor(() => {
        expect(screen.getByText(/Connected.*will create new workspace/)).toBeInTheDocument();
      });
    });

    it('does not show flavor selector for persistent profiles', async () => {
      mockGetRemoteProfileStatuses.mockResolvedValue([
        {
          profile: persistentProfile,
          flavor_hosts: [{ flavor: '', hosts: [] }],
        },
      ]);

      renderSelector();
      await waitFor(() => {
        expect(screen.getByText(/Spawn on Dev Server/)).toBeInTheDocument();
      });
      // No flavor <select> should be present
      const selects = screen.queryAllByRole('combobox');
      expect(selects).toHaveLength(0);
    });
  });
});
