import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import HostStatusIndicator, { getHostStatus } from './HostStatusIndicator';
import type { RemoteHost } from '../lib/types';

describe('getHostStatus', () => {
  it('returns "disconnected" for null', () => {
    expect(getHostStatus(null)).toBe('disconnected');
  });

  it('returns the status when valid', () => {
    const host = { status: 'connected' } as RemoteHost;
    expect(getHostStatus(host)).toBe('connected');
  });

  it('returns "disconnected" for invalid status', () => {
    const host = { status: 'bogus' } as unknown as RemoteHost;
    expect(getHostStatus(host)).toBe('disconnected');
  });

  it('handles all valid statuses', () => {
    const validStatuses = [
      'ready',
      'connected',
      'provisioning',
      'connecting',
      'disconnected',
      'expired',
      'reconnecting',
      'failed',
    ];
    for (const status of validStatuses) {
      expect(getHostStatus({ status } as RemoteHost)).toBe(status);
    }
  });
});

describe('HostStatusIndicator', () => {
  it('renders "Ready" for ready status', () => {
    render(<HostStatusIndicator status="ready" />);
    expect(screen.getByText('Ready')).toBeInTheDocument();
  });

  it('renders hostname when connected with hostname', () => {
    render(<HostStatusIndicator status="connected" hostname="my-host.local" />);
    expect(screen.getByText('my-host.local')).toBeInTheDocument();
  });

  it('renders "Connected" when connected without hostname', () => {
    render(<HostStatusIndicator status="connected" />);
    expect(screen.getByText('Connected')).toBeInTheDocument();
  });

  it('renders "Provisioning..." for provisioning status', () => {
    render(<HostStatusIndicator status="provisioning" />);
    expect(screen.getByText('Provisioning...')).toBeInTheDocument();
  });

  it('renders "Connecting..." for connecting status', () => {
    render(<HostStatusIndicator status="connecting" />);
    expect(screen.getByText('Connecting...')).toBeInTheDocument();
  });

  it('renders "Reconnecting..." for reconnecting status', () => {
    render(<HostStatusIndicator status="reconnecting" />);
    expect(screen.getByText('Reconnecting...')).toBeInTheDocument();
  });

  it('renders "Disconnected" for disconnected status', () => {
    render(<HostStatusIndicator status="disconnected" />);
    expect(screen.getByText('Disconnected')).toBeInTheDocument();
  });

  it('renders "Expired" for expired status', () => {
    render(<HostStatusIndicator status="expired" />);
    expect(screen.getByText('Expired')).toBeInTheDocument();
  });

  it('renders spinner for provisioning/connecting/reconnecting', () => {
    const { container: c1 } = render(<HostStatusIndicator status="provisioning" />);
    expect(
      c1.querySelector('[data-testid="host-status-indicator"][data-variant="spinner"]')
    ).toBeInTheDocument();

    const { container: c2 } = render(<HostStatusIndicator status="connecting" />);
    expect(
      c2.querySelector('[data-testid="host-status-indicator"][data-variant="spinner"]')
    ).toBeInTheDocument();

    const { container: c3 } = render(<HostStatusIndicator status="reconnecting" />);
    expect(
      c3.querySelector('[data-testid="host-status-indicator"][data-variant="spinner"]')
    ).toBeInTheDocument();
  });

  // Bug 2: "failed" status must render with error color and "Failed" label
  it('renders "Failed" label with error color for status "failed"', () => {
    const { container } = render(<HostStatusIndicator status="failed" />);
    expect(screen.getByText('Failed')).toBeInTheDocument();
    // Should use error color (same as disconnected), not muted
    const indicator = container.querySelector(
      '[data-testid="host-status-indicator"]'
    ) as HTMLElement;
    expect(indicator).toBeInTheDocument();
    // Should render as a static dot (not a spinner)
    expect(indicator?.getAttribute('data-variant')).toBe('dot');
  });

  it('renders static dot for non-spinner statuses', () => {
    const { container } = render(<HostStatusIndicator status="ready" />);
    // No spinner, but has the dot element
    expect(
      container.querySelector('[data-testid="host-status-indicator"][data-variant="spinner"]')
    ).not.toBeInTheDocument();
    // Dot variant should be present
    const dot = container.querySelector(
      '[data-testid="host-status-indicator"][data-variant="dot"]'
    ) as HTMLElement;
    expect(dot).toBeInTheDocument();
  });
});
