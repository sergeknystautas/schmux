import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import FlightsPage from './FlightsPage';
import type { FlightSummaryResponse } from '../lib/types.generated';

const mockAlert = vi.fn();
const mockConfirm = vi.fn();
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert, confirm: mockConfirm, prompt: vi.fn() }),
}));
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

let mockFlights: FlightSummaryResponse[] = [];
let mockLoading = false;
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ flights: mockFlights, loading: mockLoading, workspaces: [] }),
}));

let mockConfig: Record<string, unknown> = {};
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: mockConfig }),
}));

const api = {
  createFlight: vi.fn(),
  updateFlight: vi.fn(),
  getFlight: vi.fn(),
  reorderFlights: vi.fn(),
};
vi.mock('../lib/api', () => ({
  createFlight: (...a: unknown[]) => api.createFlight(...a),
  updateFlight: (...a: unknown[]) => api.updateFlight(...a),
  getFlight: (...a: unknown[]) => api.getFlight(...a),
  reorderFlights: (...a: unknown[]) => api.reorderFlights(...a),
  disposeFlight: vi.fn(),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

const T = '2026-01-01T00:00:00Z';

function backlog(id: string, order: number, shortName?: string): FlightSummaryResponse {
  return {
    id,
    repo: 'git@example.com:a.git',
    short_name: shortName,
    name_error: '',
    planner_model: 'claude',
    executor_model: 'claude',
    workflow_state: 'backlog',
    cleanup_state: 'idle',
    order,
    created_at: T,
    updated_at: T,
  };
}

function renderPage() {
  return render(
    <MemoryRouter>
      <FlightsPage />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  localStorage.setItem('schmux:spawn-last-target-counts', JSON.stringify({ claude: 1 }));
  mockFlights = [];
  mockLoading = false;
  mockConfig = {
    chat_sessions: true,
    repos: [{ url: 'git@example.com:a.git', name: 'alpha' }],
    enabled_models: {},
    models: [
      { id: 'claude', display_name: 'Claude', configured: true, runners: ['claude'] },
      { id: 'plain', display_name: 'Plain', configured: true, runners: ['plain'] },
      { id: 'off', display_name: 'Off', configured: false, runners: ['claude'] },
    ],
    runners: {
      claude: { available: true, capabilities: ['chat'] },
      plain: { available: true, capabilities: [] },
    },
  };
});

describe('FlightsPage', () => {
  it('shows a loading state until the first dashboard snapshot', () => {
    mockLoading = true;
    renderPage();
    expect(screen.queryByTestId('flight-backlog')).not.toBeInTheDocument();
  });

  it('creates a flight in the focused spawn layout using chat-capable targets', async () => {
    api.createFlight.mockResolvedValue({ ...backlog('fl-new', 0), prompt: 'Fix login' });
    renderPage();

    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByRole('heading', { name: 'What do you want me to do?' })).toBeInTheDocument();
    expect(screen.queryByTestId('flight-backlog')).not.toBeInTheDocument();
    const planner = screen.getByLabelText('Planner');
    expect(
      within(planner)
        .getAllByRole('option')
        .map((o) => o.textContent)
    ).toEqual(['Select model', 'Claude']);

    await userEvent.type(screen.getByLabelText('Prompt'), 'Fix login');
    await userEvent.click(screen.getByTestId('flight-form-submit'));

    expect(api.createFlight).toHaveBeenCalledWith({
      repo: 'git@example.com:a.git',
      prompt: 'Fix login',
      planner_model: 'claude',
      executor_model: 'claude',
    });
    expect(screen.queryByTestId('flight-create')).not.toBeInTheDocument();
    expect(screen.getByTestId('flight-backlog')).toBeInTheDocument();
  });

  it('returns to the list without creating when cancelled', async () => {
    renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    await userEvent.type(screen.getByLabelText('Prompt'), 'Not ready');
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(api.createFlight).not.toHaveBeenCalled();
    expect(screen.getByTestId('flight-backlog')).toBeInTheDocument();
  });

  it('uses the last spawn model instead of the first available model', async () => {
    mockConfig.models = [
      { id: 'minimax', display_name: 'MiniMax', configured: true, runners: ['claude'] },
      ...(mockConfig.models as object[]),
    ];
    renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByLabelText('Planner')).toHaveValue('claude');
    expect(screen.getByLabelText('Executor')).toHaveValue('claude');
  });

  it('restores every draft field after cancel, leaving the page, and reloading', async () => {
    mockConfig.models = [
      ...(mockConfig.models as object[]),
      { id: 'codex', display_name: 'Codex', configured: true, runners: ['claude'] },
    ];
    mockConfig.repos = [
      { url: 'git@example.com:a.git', name: 'alpha' },
      { url: 'git@example.com:b.git', name: 'beta' },
    ];
    const page = renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    await userEvent.type(
      screen.getByLabelText('Prompt'),
      '  Keep this draft.{enter}Second line.  '
    );
    await userEvent.selectOptions(screen.getByLabelText('Repository'), 'git@example.com:b.git');
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'codex');
    await userEvent.selectOptions(screen.getByLabelText('Executor'), 'claude');
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByLabelText('Prompt')).toHaveValue('  Keep this draft.\nSecond line.  ');
    page.unmount();
    renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByLabelText('Prompt')).toHaveValue('  Keep this draft.\nSecond line.  ');
    expect(screen.queryByLabelText('Short name')).toBeNull();
    expect(screen.getByLabelText('Repository')).toHaveValue('git@example.com:b.git');
    expect(screen.getByLabelText('Planner')).toHaveValue('codex');
    expect(screen.getByLabelText('Executor')).toHaveValue('claude');
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'claude');
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'codex');
    expect(screen.getByLabelText('Executor')).toHaveValue('claude');
  });

  it('clears task text after creation while remembering repository and both models', async () => {
    mockConfig.models = [
      ...(mockConfig.models as object[]),
      { id: 'codex', display_name: 'Codex', configured: true, runners: ['claude'] },
    ];
    mockConfig.repos = [
      { url: 'git@example.com:a.git', name: 'alpha' },
      { url: 'git@example.com:b.git', name: 'beta' },
    ];
    api.createFlight.mockResolvedValue(backlog('fl-new', 0));
    renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    await userEvent.type(screen.getByLabelText('Prompt'), 'Create this');
    await userEvent.selectOptions(screen.getByLabelText('Repository'), 'git@example.com:b.git');
    await userEvent.selectOptions(screen.getByLabelText('Executor'), 'codex');
    await userEvent.click(screen.getByRole('button', { name: 'Create flight' }));
    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByLabelText('Prompt')).toHaveValue('');
    expect(screen.queryByLabelText('Short name')).toBeNull();
    expect(screen.getByLabelText('Repository')).toHaveValue('git@example.com:b.git');
    expect(screen.getByLabelText('Planner')).toHaveValue('claude');
    expect(screen.getByLabelText('Executor')).toHaveValue('codex');
  });

  it('keeps a failed creation draft after remounting', async () => {
    api.createFlight.mockRejectedValue(new Error('Create failed'));
    const page = renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    await userEvent.type(screen.getByLabelText('Prompt'), 'Keep after failure');
    await userEvent.click(screen.getByRole('button', { name: 'Create flight' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Create failed');
    page.unmount();
    renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByLabelText('Prompt')).toHaveValue('Keep after failure');
    expect(screen.getByLabelText('Planner')).toHaveValue('claude');
  });

  it('requires a new choice when the remembered model is no longer available', async () => {
    localStorage.setItem(
      'schmux:flight-new-draft',
      JSON.stringify({
        repo: 'git@example.com:a.git',
        prompt: 'Retained prompt',
        short_name: '',
        planner_model: 'removed',
        executor_model: 'removed',
        executorSelected: false,
      })
    );
    renderPage();
    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByLabelText('Prompt')).toHaveValue('Retained prompt');
    expect(screen.getByLabelText('Planner')).toHaveValue('');
    expect(screen.getByLabelText('Executor')).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Create flight' })).toBeDisabled();
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'claude');
    expect(screen.getByRole('button', { name: 'Create flight' })).toBeEnabled();
  });

  it('keeps editing an existing flight separate from the new flight draft', async () => {
    localStorage.setItem(
      'schmux:flight-new-draft',
      JSON.stringify({
        repo: 'git@example.com:a.git',
        prompt: 'Unfinished new task',
        short_name: 'Draft',
        planner_model: 'claude',
        executor_model: 'claude',
        executorSelected: false,
      })
    );
    mockFlights = [backlog('fl-a', 0)];
    api.getFlight.mockResolvedValue({ ...mockFlights[0], prompt: 'Existing task' });
    api.updateFlight.mockResolvedValue(mockFlights[0]);
    renderPage();
    await userEvent.click(screen.getByTestId('flight-edit-fl-a'));
    const prompt = await screen.findByLabelText('Prompt');
    expect(prompt).toHaveValue('Existing task');
    await userEvent.type(prompt, ' edited');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await userEvent.click(screen.getByTestId('flight-new'));
    expect(screen.getByLabelText('Prompt')).toHaveValue('Unfinished new task');
    expect(screen.queryByLabelText('Short name')).toBeNull();
  });

  it('offers no targets when chat sessions are disabled', async () => {
    mockConfig.chat_sessions = false;
    renderPage();

    await userEvent.click(screen.getByTestId('flight-new'));

    expect(screen.getByTestId('flight-form-no-targets')).toBeInTheDocument();
  });

  it('reorders by sending the complete backlog order', async () => {
    mockFlights = [backlog('fl-a', 0), backlog('fl-b', 1), backlog('fl-c', 2)];
    api.reorderFlights.mockResolvedValue(undefined);
    renderPage();

    await userEvent.click(screen.getByRole('button', { name: 'Move fl-b up' }));

    expect(api.reorderFlights).toHaveBeenCalledWith(['fl-b', 'fl-a', 'fl-c']);
  });

  it('shows a rejected reorder', async () => {
    mockFlights = [backlog('fl-a', 0), backlog('fl-b', 1)];
    api.reorderFlights.mockRejectedValue(new Error('order lists 2 flights but the backlog has 3'));
    renderPage();

    await userEvent.click(screen.getByRole('button', { name: 'Move fl-a down' }));

    expect(mockAlert).toHaveBeenCalledWith(
      'Reorder failed',
      'order lists 2 flights but the backlog has 3'
    );
  });

  it('edits a backlog flight starting from its full prompt', async () => {
    mockFlights = [backlog('fl-a', 0, 'Old name')];
    api.getFlight.mockResolvedValue({ ...mockFlights[0], prompt: 'Original prompt' });
    api.updateFlight.mockResolvedValue({ ...mockFlights[0], prompt: 'Original prompt' });
    renderPage();

    await userEvent.click(screen.getByTestId('flight-edit-fl-a'));
    const prompt = await screen.findByLabelText('Prompt');
    expect(prompt).toHaveValue('Original prompt');
    expect(screen.getByRole('heading', { name: 'Edit Old name' })).toBeInTheDocument();
    expect(screen.queryByTestId('flight-backlog')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Short name')).toBeNull();
    expect(screen.getByLabelText('Stage')).toHaveValue('backlog');
    await userEvent.selectOptions(screen.getByLabelText('Stage'), 'planning');

    await userEvent.click(screen.getByTestId('flight-form-submit'));

    expect(api.getFlight).toHaveBeenCalledWith('fl-a');
    expect(api.updateFlight).toHaveBeenCalledWith('fl-a', {
      repo: 'git@example.com:a.git',
      prompt: 'Original prompt',
      planner_model: 'claude',
      executor_model: 'claude',
      workflow_state: 'planning',
    });
    expect(screen.queryByTestId('flight-form')).not.toBeInTheDocument();
    expect(screen.getByTestId('flight-backlog')).toBeInTheDocument();
  });

  it.each(['backlog', 'planning', 'executing'])(
    'edits every field of a %s flight with a workspace',
    async (workflow_state) => {
      mockConfig.models = [
        ...(mockConfig.models as object[]),
        { id: 'codex', display_name: 'Codex', configured: true, runners: ['claude'] },
      ];
      mockConfig.repos = [
        { url: 'git@example.com:a.git', name: 'alpha' },
        { url: 'git@example.com:b.git', name: 'beta' },
      ];
      mockFlights = [
        {
          ...backlog('fl-a', 0),
          workflow_state,
          workspace: {
            id: 'wa-1',
            workspace_id: 'ws-1',
            path: '/workspaces/one',
            requested_branch: 'flight',
            outcome: 'succeeded',
            available: true,
            created_at: T,
            updated_at: T,
          },
        },
      ];
      api.getFlight.mockResolvedValue({ ...mockFlights[0], prompt: 'Original prompt' });
      api.updateFlight.mockResolvedValue({ ...mockFlights[0], workflow_state: 'planning' });
      renderPage();
      await userEvent.click(screen.getByTestId('flight-edit-fl-a'));
      const stage = await screen.findByLabelText('Stage');
      expect(stage).toHaveValue(workflow_state);
      for (const label of ['Prompt', 'Planner', 'Executor', 'Repository']) {
        expect(screen.getByLabelText(label)).toBeEnabled();
      }
      await userEvent.clear(screen.getByLabelText('Prompt'));
      await userEvent.type(screen.getByLabelText('Prompt'), 'Edited prompt');
      await userEvent.selectOptions(screen.getByLabelText('Executor'), 'codex');
      await userEvent.selectOptions(screen.getByLabelText('Repository'), 'git@example.com:b.git');
      await userEvent.selectOptions(stage, 'planning');
      await userEvent.click(screen.getByRole('button', { name: 'Save' }));
      expect(api.updateFlight).toHaveBeenCalledWith('fl-a', {
        repo: 'git@example.com:b.git',
        prompt: 'Edited prompt',
        planner_model: 'claude',
        executor_model: 'codex',
        workflow_state: 'planning',
      });
      expect(screen.queryByTestId('flight-edit')).not.toBeInTheDocument();
    }
  );

  // Delete is gone; the page-level test surface ends here. The dispose
  // confirmation flow is exercised in FlightDisposeButton.test.tsx and
  // FlightsPage.test cases for create / reorder / edit above.
});
