import { describe, expect, it, vi, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useAutoSave, type SaveStatus } from './useAutoSave';
import { initialState, type ConfigFormAction, type ConfigFormState } from './useConfigForm';

const mockUpdateConfig = vi.fn();
const mockGetConfig = vi.fn();
const mockReloadConfig = vi.fn();

vi.mock('../../lib/api', () => ({
  updateConfig: (...args: unknown[]) => mockUpdateConfig(...args),
  getConfig: (...args: unknown[]) => mockGetConfig(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
}));

vi.mock('../../contexts/ConfigContext', () => ({
  useConfig: () => ({ reloadConfig: mockReloadConfig }),
}));

const mockToastError = vi.fn();
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ error: mockToastError }),
}));

describe('useAutoSave', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdateConfig.mockResolvedValue({ status: 'ok' });
    mockGetConfig.mockResolvedValue({
      oneshot_targets: [],
      models: [],
      runners: {},
      anthropic_oauth_token_set: false,
      typesafe_api_key_set: false,
      ollama: { reachable: false, models: [], auto_detected_endpoint: '' },
      needs_restart: false,
    });
  });

  it('keeps the repo list when a save fails', async () => {
    const savedState: ConfigFormState = {
      ...initialState,
      loading: false,
      workspacePath: '/saved/ws',
      repos: [],
    };
    const currentState: ConfigFormState = {
      ...savedState,
      workspacePath: '/current/ws',
      repos: [{ name: 'my-repo', url: 'git@github.com:user/repo.git' }],
    };
    const rawDispatch = vi.fn<(action: ConfigFormAction) => void>();
    const saveStatusRef = { current: 'idle' as SaveStatus };
    const setSaveStatus = vi.fn<(status: SaveStatus) => void>();

    const { result } = renderHook(() =>
      useAutoSave(currentState, rawDispatch, saveStatusRef, setSaveStatus)
    );

    act(() => {
      result.current.setLastSavedConfig(savedState);
    });

    mockUpdateConfig.mockRejectedValueOnce(new Error('boom'));

    // An edit schedules a debounced save; flushing it runs it now.
    act(() => {
      result.current.dispatch({ type: 'SET_FIELD', field: 'workspacePath', value: '/edited/ws' });
    });

    act(() => {
      result.current.flushSave();
    });

    // The save reports its failure with a toast, then reverts. Wait for the
    // toast (the failure is the event), then assert the revert once.
    await waitFor(() => expect(mockToastError).toHaveBeenCalled());

    // The revert spreads the last saved config and keeps the repo list.
    const revert = rawDispatch.mock.calls.find((call) => call[0].type === 'LOAD_CONFIG');
    expect(revert).toBeDefined();
    const revertState = (revert as [ConfigFormAction & { state: Partial<ConfigFormState> }])[0]
      .state;
    expect(revertState?.workspacePath).toBe('/saved/ws');
    expect(revertState?.repos).toEqual(currentState.repos);
  });
});
