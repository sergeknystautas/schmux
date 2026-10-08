import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import ClientPerformance from './ClientPerformance';
import { clientPerf } from '../lib/clientPerf';
import * as api from '../lib/api';

const navigate = vi.fn();
vi.mock('react-router', async (orig) => ({
  ...(await orig<typeof import('react-router')>()),
  useNavigate: () => navigate,
}));
const waitForSession = vi.fn().mockResolvedValue(true);
vi.mock('../contexts/SessionsContext', () => ({ useSessions: () => ({ waitForSession }) }));
let config = { client_performance: { enabled: true, repo: 'schmux', target: 'claude' } };
vi.mock('../contexts/ConfigContext', () => ({ useConfig: () => ({ config }) }));
vi.mock('./ModalProvider', () => ({
  useModal: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}));

describe('ClientPerformance pane', () => {
  beforeEach(() => {
    localStorage.clear();
    clientPerf.setConfigEnabled(true);
    clientPerf.setDevMode(true);
    clientPerf.stop();
    navigate.mockClear();
  });

  it('shows Start recording when off and the status line when on', async () => {
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    expect(screen.getByText('Client Performance')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Start recording' }));
    expect(clientPerf.isRecording()).toBe(true);
    expect(screen.getByText(/Recording 0 min · 0 stalls/)).toBeInTheDocument();
    expect(screen.getByText('Client Performance · REC')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open performance chat' })).toBeInTheDocument();
  });

  it('shows the config hint instead of the chat link when repo or target is unset', () => {
    config = { client_performance: { enabled: true, repo: '', target: 'claude' } };
    clientPerf.start();
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    expect(screen.getByRole('link', { name: /Pick a repo and target/ })).toHaveAttribute(
      'href',
      '/config?tab=advanced'
    );
    expect(screen.queryByRole('button', { name: 'Open performance chat' })).toBeNull();
    config = { client_performance: { enabled: true, repo: 'schmux', target: 'claude' } };
  });

  it('opens the chat: posts the kept ids, keeps the returned pair, navigates', async () => {
    const ensure = vi
      .spyOn(api, 'ensureClientPerformanceSession')
      .mockResolvedValue({ workspace_id: 'ws-1', session_id: 'sess-1' });
    clientPerf.start();
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    await userEvent.click(screen.getByRole('button', { name: 'Open performance chat' }));
    expect(ensure).toHaveBeenCalledWith({ workspace_id: '', session_id: '' });
    expect(clientPerf.getChat()).toEqual({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    expect(waitForSession).toHaveBeenCalledWith('sess-1');
    expect(navigate).toHaveBeenCalledWith('/sessions/sess-1');
    ensure.mockRestore();
  });

  it('shows the endpoint error in the pane', async () => {
    vi.spyOn(api, 'ensureClientPerformanceSession').mockRejectedValue(
      new Error('workspace ws-9 is not a schmux checkout')
    );
    clientPerf.start();
    render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
    await userEvent.click(screen.getByRole('button', { name: 'Open performance chat' }));
    expect(await screen.findByText('workspace ws-9 is not a schmux checkout')).toBeInTheDocument();
  });
});
