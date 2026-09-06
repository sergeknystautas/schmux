import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import SessionSidebar from './SessionSidebar';
import type { SessionResponse } from '../lib/types';

const session: SessionResponse = {
  id: 'sess-1',
  target: 'claude',
  branch: 'main',
  created_at: new Date().toISOString(),
  running: true,
  attach_cmd: 'tmux attach -t sess-1',
  nickname: 'worker',
};

function renderSidebar(showAttach: boolean, onDispose = vi.fn()) {
  render(
    <SessionSidebar
      session={session}
      config={{
        tmux_socket_name: 'schmux',
        system_capabilities: { iterm2_available: true, fence_available: false },
      }}
      showAttach={showAttach}
      onEditNickname={vi.fn()}
      onCopyAttach={vi.fn()}
      onDispose={onDispose}
    />
  );
  return onDispose;
}

describe('SessionSidebar', () => {
  it('shows metadata and the attach command for terminal sessions', () => {
    renderSidebar(true);
    expect(screen.getByText('sess-1')).toBeInTheDocument();
    expect(screen.getByText('claude')).toBeInTheDocument();
    expect(screen.getByText('worker')).toBeInTheDocument();
    expect(screen.getByText('Attach Command')).toBeInTheDocument();
    expect(screen.getByText('tmux attach -t sess-1')).toBeInTheDocument();
    expect(screen.getByText('Open in iTerm2')).toBeInTheDocument();
  });

  it('hides the attach command and iTerm2 link when showAttach is false', () => {
    renderSidebar(false);
    expect(screen.queryByText('Attach Command')).not.toBeInTheDocument();
    expect(screen.queryByText('Open in iTerm2')).not.toBeInTheDocument();
    expect(screen.getByTestId('dispose-session')).toBeInTheDocument();
  });

  it('dispose button calls onDispose', async () => {
    const onDispose = renderSidebar(false);
    await userEvent.click(screen.getByTestId('dispose-session'));
    expect(onDispose).toHaveBeenCalled();
  });
});
