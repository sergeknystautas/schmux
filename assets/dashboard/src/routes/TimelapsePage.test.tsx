import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import TimelapsePage from './TimelapsePage';

vi.mock('../lib/api', () => ({
  getTimelapseRecordings: vi.fn(),
  exportTimelapseRecording: vi.fn(),
  deleteTimelapseRecording: vi.fn(),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}));

import { getTimelapseRecordings } from '../lib/api';

const mockGetRecordings = vi.mocked(getTimelapseRecordings);

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/timelapse']}>
      <TimelapsePage />
    </MemoryRouter>
  );
}

describe('TimelapsePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the title in the standard app header', async () => {
    mockGetRecordings.mockResolvedValue([]);
    renderPage();

    const title = await waitFor(() =>
      screen.getByRole('heading', { name: 'Timelapses', level: 1 })
    );
    expect(title).toHaveClass('app-header__meta');
  });

  it('renders an empty state when there are no recordings', async () => {
    mockGetRecordings.mockResolvedValue([]);
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(/No recordings found/)).toBeInTheDocument();
    });
  });
});
