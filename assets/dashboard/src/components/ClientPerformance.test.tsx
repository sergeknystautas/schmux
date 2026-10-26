import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import ClientPerformance from './ClientPerformance';
import { clientPerf } from '../lib/clientPerf';
import * as api from '../lib/api';
import { transport } from '../lib/transport';
import type { ClientPerformanceSessionResponse } from '../lib/types.generated';

const navigate = vi.fn();
vi.mock('react-router', async (orig) => ({
  ...(await orig<typeof import('react-router')>()),
  useNavigate: () => navigate,
}));
const waitForSession = vi.fn().mockResolvedValue(true);
const agent = { id: 'agent', workspace_id: 'ws-perf', kind: 'chat', running: true };
const workspace = {
  id: 'ws-perf',
  repo: 'https://example.com/schmux.git',
  branch: 'fix/client-performance-test',
  sessions: [agent],
};
let workspaces: (typeof workspace)[] = [];
let clientPerformance: ClientPerformanceSessionResponse | undefined;
let clientPerformanceError = '';
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    waitForSession,
    workspaces,
    clientPerformance,
    clientPerformanceError,
    sessionsById: Object.fromEntries(workspaces.flatMap((w) => w.sessions.map((s) => [s.id, s]))),
  }),
}));
const config = {
  repos: [{ name: 'schmux', url: 'https://example.com/schmux.git' }],
  client_performance: { enabled: true, repo: 'schmux', target: 'claude' },
};
vi.mock('../contexts/ConfigContext', () => ({ useConfig: () => ({ config }) }));
const confirm = vi.fn();
const alert = vi.fn();
const success = vi.fn();
vi.mock('./ModalProvider', () => ({ useModal: () => ({ alert, confirm }) }));
vi.mock('./ToastProvider', () => ({ useToast: () => ({ success }) }));
async function renderPane() {
  clientPerformance = workspaces[0]
    ? { workspace_id: workspaces[0].id, session_id: workspaces[0].sessions[0]?.id ?? '' }
    : undefined;
  let view!: ReturnType<typeof render>;
  await act(async () => {
    view = render(
      <MemoryRouter>
        <ClientPerformance />
      </MemoryRouter>
    );
  });
  return view;
}

describe('ClientPerformance workspace and recording controls', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
    clientPerf.stop({ clearChat: true });
    clientPerf.setConfigEnabled(true);
    clientPerf.setDevMode(true);
    workspaces = [];
    clientPerformance = undefined;
    clientPerformanceError = '';
    navigate.mockClear();
    waitForSession.mockClear();
    confirm.mockReset().mockResolvedValue(true);
    alert.mockReset().mockResolvedValue(null);
    success.mockClear();
  });
  afterEach(() => {
    clientPerf.stop({ clearChat: true });
    vi.restoreAllMocks();
    vi.useRealTimers();
  });
  it('launches a workspace and follows the session once it arrives', async () => {
    const launch = vi
      .spyOn(api, 'ensureClientPerformanceSession')
      .mockResolvedValue({ workspace_id: 'ws-perf', session_id: 'agent' });
    await renderPane();
    await userEvent.click(screen.getByRole('button', { name: 'Launch' }));
    expect(launch).toHaveBeenCalledWith({ workspace_id: '' });
    expect(waitForSession).toHaveBeenCalledWith('agent');
    expect(navigate).toHaveBeenCalledWith('/sessions/agent');
  });
  it('finds the agent from dashboard state after reload', async () => {
    workspaces = [workspace];
    await renderPane();
    expect(screen.getByRole('link', { name: 'View' })).toHaveAttribute('href', '/sessions/agent');
    expect(screen.getByRole('button', { name: 'Record' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Launch' })).toBeNull();
  });
  it('launches a replacement agent in the tracked workspace after its agent stops', async () => {
    workspaces = [{ ...workspace, sessions: [] }];
    const launch = vi
      .spyOn(api, 'ensureClientPerformanceSession')
      .mockResolvedValue({ workspace_id: 'ws-perf', session_id: 'replacement' });
    await renderPane();
    await userEvent.click(screen.getByRole('button', { name: 'Launch' }));
    expect(launch).toHaveBeenCalledWith({ workspace_id: 'ws-perf' });
  });
  it('Stop delivers a recording directly from the sidebar and opens the agent', async () => {
    workspaces = [workspace];
    const send = vi.spyOn(api, 'sendClientPerformanceRecording').mockResolvedValue();
    clientPerf.setChat({ workspaceId: 'ws-perf', sessionId: 'agent' });
    await clientPerf.start();
    clientPerf.recordError('sample');
    await renderPane();
    await userEvent.click(screen.getByRole('button', { name: 'Stop' }));
    expect(confirm).not.toHaveBeenCalled();
    expect(send).toHaveBeenCalledWith(
      'agent',
      expect.objectContaining({ type: 'application/json' })
    );
    expect(success).toHaveBeenCalledWith('Recording sent');
    expect(clientPerf.startedAt()).toBeNull();
    expect(navigate).toHaveBeenCalledWith('/sessions/agent');
  });
  it('keeps the completed recording after delivery failure and permits retry', async () => {
    workspaces = [workspace];
    const send = vi
      .spyOn(api, 'sendClientPerformanceRecording')
      .mockRejectedValueOnce(new Error('agent disconnected'))
      .mockResolvedValue();
    clientPerf.setChat({ workspaceId: 'ws-perf', sessionId: 'agent' });
    await clientPerf.start();
    clientPerf.recordError('keep');
    await renderPane();
    await userEvent.click(screen.getByRole('button', { name: 'Stop' }));
    expect(alert).toHaveBeenCalledWith('Send Failed', 'agent disconnected');
    expect(clientPerf.isRecording()).toBe(false);
    expect(clientPerf.buildFile().errors[0].message).toBe('keep');
    await userEvent.click(screen.getByRole('button', { name: 'Send recording' }));
    expect(send).toHaveBeenCalledTimes(2);
    expect(clientPerf.startedAt()).toBeNull();
    expect(navigate).toHaveBeenCalledWith('/sessions/agent');
  });
  it('confirms only explicit discard', async () => {
    clientPerf.setChat({ workspaceId: 'ws-perf', sessionId: 'agent' });
    await clientPerf.start();
    clientPerf.recordError('keep');
    workspaces = [workspace];
    await renderPane();
    confirm.mockResolvedValueOnce(false);
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(clientPerf.isRecording()).toBe(true);
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(clientPerf.startedAt()).toBeNull();
  });
  it('uses the shared button style and keeps the collapsed state', async () => {
    workspaces = [workspace];
    const { unmount } = await renderPane();
    expect(screen.getByRole('button', { name: 'Record' })).toHaveClass(
      'btn',
      'btn--secondary',
      'btn--sm'
    );
    expect(screen.queryByText(/Records what this browser/)).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Client Performance' }));
    unmount();
    await renderPane();
    expect(screen.queryByRole('button', { name: 'Record' })).toBeNull();
  });
  it('starts recording from the sidebar', async () => {
    workspaces = [workspace];
    await renderPane();
    await userEvent.click(screen.getByRole('button', { name: 'Record' }));
    expect(await screen.findByText('Recording 0:00')).toBeInTheDocument();
    expect(clientPerf.isRecording()).toBe(true);
    expect(clientPerf.getChat()).toEqual({ workspaceId: 'ws-perf', sessionId: 'agent' });
  });

  it('starts recording without a performance endpoint request', async () => {
    const fetch = vi.spyOn(transport, 'fetch').mockResolvedValue(new Response('{}'));
    const launch = vi.spyOn(api, 'ensureClientPerformanceSession');
    workspaces = [workspace];
    await renderPane();
    expect(alert).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Record' }));
    expect(await screen.findByText('Recording 0:00')).toBeInTheDocument();
    expect(clientPerf.isRecording()).toBe(true);
    expect(launch).not.toHaveBeenCalled();
    expect(fetch.mock.calls.some(([input]) => String(input).includes('/client-performance/'))).toBe(
      false
    );
  });

  it('shows association errors received from the dashboard snapshot', async () => {
    clientPerformanceError = 'Cannot read agent state';
    await renderPane();
    expect(alert).toHaveBeenCalledWith('Client Performance', 'Cannot read agent state');
  });

  it('shows errors from an explicit Launch action', async () => {
    vi.spyOn(api, 'ensureClientPerformanceSession').mockRejectedValue(new Error('Launch rejected'));
    await renderPane();
    await userEvent.click(screen.getByRole('button', { name: 'Launch' }));
    expect(alert).toHaveBeenCalledWith('Launch Failed', 'Launch rejected');
  });
});
