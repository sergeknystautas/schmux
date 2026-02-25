import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { IOWorkspaceMetricsPanel, type IOWorkspaceStats } from './IOWorkspaceMetricsPanel';

const stats: IOWorkspaceStats = {
  totalCommands: 42,
  totalDurationMs: 1500,
  triggerCounts: { poller: 30, watcher: 12 },
  counters: { git_status: 25, git_fetch: 17 },
};

describe('IOWorkspaceMetricsPanel', () => {
  it('renders pill with command count and duration', () => {
    render(<IOWorkspaceMetricsPanel stats={stats} />);
    expect(screen.getByText('42 git cmds')).toBeInTheDocument();
    expect(screen.getByText('1.5s')).toBeInTheDocument();
  });

  it('renders zero state when stats is null', () => {
    render(<IOWorkspaceMetricsPanel stats={null} />);
    expect(screen.getByText('0 git cmds')).toBeInTheDocument();
    expect(screen.getByText('0ms')).toBeInTheDocument();
  });

  it('renders Capture button when onCapture provided', () => {
    const onCapture = vi.fn();
    render(<IOWorkspaceMetricsPanel stats={stats} onCapture={onCapture} />);
    expect(screen.getByText('Capture')).toBeInTheDocument();
  });

  it('calls onCapture when Capture button clicked', async () => {
    const onCapture = vi.fn();
    render(<IOWorkspaceMetricsPanel stats={stats} onCapture={onCapture} />);
    await userEvent.click(screen.getByText('Capture'));
    expect(onCapture).toHaveBeenCalledTimes(1);
  });

  it('expands dropdown on pill click showing counters', async () => {
    render(<IOWorkspaceMetricsPanel stats={stats} />);
    await userEvent.click(screen.getByText('42 git cmds'));
    expect(screen.getByText('Total commands')).toBeInTheDocument();
    expect(screen.getByText('git_status')).toBeInTheDocument();
    expect(screen.getByText('git_fetch')).toBeInTheDocument();
    expect(screen.getByText('poller')).toBeInTheDocument();
  });

  it('does not render Capture button when onCapture not provided', () => {
    render(<IOWorkspaceMetricsPanel stats={stats} />);
    expect(screen.queryByText('Capture')).not.toBeInTheDocument();
  });
});
