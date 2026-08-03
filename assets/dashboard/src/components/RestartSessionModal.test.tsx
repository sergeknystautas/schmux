import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router';
import RestartSessionModal from './RestartSessionModal';

const mockGetRestartOptions = vi.fn();
const mockRestartSession = vi.fn();
const mockNavigate = vi.fn();
const mockWaitForSession = vi.fn();

vi.mock('../lib/api', () => ({
  getRestartOptions: (...a: unknown[]) => mockGetRestartOptions(...a),
  restartSession: (...a: unknown[]) => mockRestartSession(...a),
  getErrorMessage: (_e: unknown, fallback: string) => fallback,
}));
vi.mock('react-router', async () => {
  const actual = await vi.importActual<typeof import('react-router')>('react-router');
  return { ...actual, useNavigate: () => mockNavigate };
});
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ waitForSession: mockWaitForSession }),
}));

function renderModal(onClose = vi.fn()) {
  return render(
    <MemoryRouter>
      <RestartSessionModal sessionId="sess-old" onClose={onClose} />
    </MemoryRouter>
  );
}

describe('RestartSessionModal', () => {
  beforeEach(() => {
    mockGetRestartOptions.mockReset();
    mockRestartSession.mockReset();
    mockNavigate.mockReset();
    mockWaitForSession.mockReset();
    mockGetRestartOptions.mockResolvedValue({
      current_target: 'claude',
      targets: ['claude', 'claude-opus-4-6'],
      fence: true,
      fence_available: true,
    });
    mockWaitForSession.mockResolvedValue(undefined);
    mockRestartSession.mockResolvedValue({ session_id: 'sess-new' });
  });

  it('loads options, submits chosen target/fence, and navigates', async () => {
    const user = userEvent.setup();
    renderModal();

    const select = (await screen.findByTestId('restart-modal-target')) as HTMLSelectElement;
    expect(select.value).toBe('claude'); // seeded to current target
    await user.selectOptions(select, 'claude-opus-4-6');
    await user.click(screen.getByTestId('restart-modal-fence')); // toggle fence off

    await user.click(screen.getByTestId('restart-modal-submit'));

    await waitFor(() =>
      expect(mockRestartSession).toHaveBeenCalledWith('sess-old', {
        target: 'claude-opus-4-6',
        fence: false,
      })
    );
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/sessions/sess-new'));
  });

  it('hides the fence checkbox when fence is unavailable', async () => {
    mockGetRestartOptions.mockResolvedValue({
      current_target: 'claude',
      targets: ['claude'],
      fence: false,
      fence_available: false,
    });
    renderModal();
    await screen.findByTestId('restart-modal-target');
    expect(screen.queryByTestId('restart-modal-fence')).not.toBeInTheDocument();
  });

  it('cancel closes without restarting', async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    renderModal(onClose);
    await screen.findByTestId('restart-modal-target');
    await user.click(screen.getByTestId('restart-modal-cancel'));
    expect(onClose).toHaveBeenCalled();
    expect(mockRestartSession).not.toHaveBeenCalled();
  });
});
