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

import LogsPage from './LogsPage';

describe('LogsPage', () => {
  it('renders a spawn row and reveals the prompt on expand', () => {
    render(<LogsPage />);
    expect(screen.getByText('https://example.com/godot.git')).toBeInTheDocument();
    expect(screen.getByText('failed')).toBeInTheDocument();
    expect(screen.queryByText(/Downloads for the fmod specs/)).toBeNull();
    fireEvent.click(screen.getByText('https://example.com/godot.git'));
    expect(screen.getByText(/Downloads for the fmod specs/)).toBeInTheDocument();
  });
});
