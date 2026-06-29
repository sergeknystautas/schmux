import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
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
    lines: sessionId ? ['[fence:http] 10:52 ✗ CONNECT 403 www.google.com'] : [],
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

    // Pick it → raw line renders.
    fireEvent.change(picker, { target: { value: 'sess-fenced' } });
    expect(screen.getByText(/CONNECT 403 www.google.com/)).toBeInTheDocument();
  });
});
