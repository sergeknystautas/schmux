import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { formatLogTime } from '../lib/utils';
import type { SpawnLogRecord } from '../lib/types.generated';

vi.mock('../hooks/useLogsWebSocket', () => ({
  default: (): { records: SpawnLogRecord[]; connected: boolean } => ({
    connected: true,
    records: [
      {
        ts: '2026-06-27T15:24:06Z',
        repo: 'https://example.com/godot.git',
        branch: 'feature/fmod',
        targets: { claude: 1 },
        prompt: 'look at ~/Downloads for the fmod specs',
        status: 'failed',
        results: [{ target: 'claude', error: 'duplicate repo URLs' }],
      },
    ],
  }),
}));

vi.mock('../hooks/useFenceLogWebSocket', () => ({
  default: (sessionId: string | null) => ({
    connected: sessionId != null,
    lines: sessionId
      ? ['[fence:http] 10:52:49 ✗ CONNECT 403 www.google.com https://www.google.com:443 (0s)']
      : [],
  }),
}));

vi.mock('../hooks/useOneshotLogWebSocket', () => ({
  default: () => ({
    connected: true,
    records: [
      {
        ts: '2026-06-29T12:00:00Z',
        type: 'commit-message',
        transport: 'cli',
        model: 'claude-sonnet-4-6',
        workspace: 'schmux-9',
        prompt_chars: 1234,
        elapsed_ms: 850,
        ok: true,
      },
      {
        ts: '2026-06-29T12:01:00Z',
        type: 'repofeed-intent',
        transport: 'api',
        model: 'claude-opus',
        prompt_chars: 99,
        elapsed_ms: 120,
        ok: false,
        error: 'decode failed: unexpected end of JSON',
      },
    ],
  }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: [
      {
        id: 'ws-1',
        branch: 'main',
        sessions: [
          { id: 'sess-fenced', nickname: 'alpha', fence: true },
          { id: 'sess-plain', nickname: 'beta', fence: false },
        ],
      },
    ],
  }),
}));

import LogsPage from './LogsPage';

describe('LogsPage', () => {
  it('renders a spawn row and reveals the prompt on expand', () => {
    render(<LogsPage />);
    expect(screen.getByText('https://example.com/godot.git')).toBeInTheDocument();
    // Timestamp renders as local HH:MM:SS, matching the Fence view's format.
    expect(screen.getByText(formatLogTime('2026-06-27T15:24:06Z'))).toBeInTheDocument();
    expect(screen.queryByText(/Downloads for the fmod specs/)).toBeNull();
    fireEvent.click(screen.getByText('https://example.com/godot.git'));
    expect(screen.getByText(/Downloads for the fmod specs/)).toBeInTheDocument();
  });

  it('shows only fenced sessions and tails the picked one', () => {
    render(<LogsPage />);
    // Switch the source select to Fence.
    fireEvent.change(screen.getByLabelText('Log source'), { target: { value: 'fence' } });

    const picker = screen.getByLabelText('Fenced session') as HTMLSelectElement;
    // Placeholder selected, no log shown yet.
    expect(picker.value).toBe('');
    expect(screen.queryByText(/CONNECT 403/)).toBeNull();
    // Only the fenced session is offered.
    expect(screen.getByRole('option', { name: /alpha/ })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /beta/ })).toBeNull();

    // Pick it → the line renders as a formatted row: a "network" kind badge
    // plus the parsed message (the time/channel prefix is stripped).
    fireEvent.change(picker, { target: { value: 'sess-fenced' } });
    expect(screen.getByText('network')).toBeInTheDocument();
    expect(screen.getByText(/CONNECT 403 www.google.com/)).toBeInTheDocument();
    expect(screen.getByText('10:52:49')).toBeInTheDocument();
    expect(screen.queryByText(/\[fence:http\]/)).toBeNull();
  });

  it('renders oneshot rows with model + type and reveals the error on expand', () => {
    render(<LogsPage />);
    fireEvent.change(screen.getByLabelText('Log source'), { target: { value: 'oneshot' } });
    expect(screen.getByText('claude-sonnet-4-6')).toBeInTheDocument();
    expect(screen.getByText('commit-message')).toBeInTheDocument();
    expect(screen.getByText('schmux-9')).toBeInTheDocument();
    // Timestamp renders as local HH:MM:SS, matching the Spawn and Fence views.
    expect(screen.getByText(formatLogTime('2026-06-29T12:00:00Z'))).toBeInTheDocument();
    // The failure error is hidden until its row is expanded.
    expect(screen.queryByText(/decode failed/)).toBeNull();
    fireEvent.click(screen.getByText('repofeed-intent'));
    expect(screen.getByText(/decode failed/)).toBeInTheDocument();
  });
});
