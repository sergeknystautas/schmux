import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ExperimentalTab from './ExperimentalTab';
import { initialState } from './useConfigForm';
import type { ConfigFormAction } from './useConfigForm';

vi.mock('../../contexts/FeaturesContext', () => ({
  useFeatures: () => ({ features: {}, loading: false }),
}));

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

function renderTab(overrides: Partial<typeof initialState> = {}) {
  return render(
    <ExperimentalTab state={{ ...initialState, ...overrides }} dispatch={dispatch} models={[]} />
  );
}

describe('ExperimentalTab fence card', () => {
  it('renders three fence-mode radios with the current mode checked', () => {
    renderTab({ fenceAvailable: true, fenceMode: 'optional_off' });
    expect(screen.getByTestId('fence-mode-disabled')).not.toBeChecked();
    expect(screen.getByTestId('fence-mode-optional_off')).toBeChecked();
    expect(screen.getByTestId('fence-mode-optional_on')).not.toBeChecked();
  });

  it('disables the radios and shows an install hint when fence is unavailable', () => {
    renderTab({ fenceAvailable: false, fenceMode: 'optional_off' });
    expect(screen.getByTestId('fence-mode-optional_off')).toBeDisabled();
    expect(screen.getByText(/install fence/i)).toBeInTheDocument();
  });

  it('dispatches SET_FIELD fenceMode when a radio is chosen', async () => {
    dispatch.mockClear();
    renderTab({ fenceAvailable: true, fenceMode: 'optional_off' });
    await userEvent.click(screen.getByTestId('fence-mode-optional_on'));
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'fenceMode', value: 'optional_on' })
    );
  });

  it('reflects fenceCommit and dispatches SET_FIELD when toggled', async () => {
    dispatch.mockClear();
    renderTab({ fenceAvailable: true, fenceMode: 'optional_off', fenceCommit: true });
    const checkbox = screen.getByTestId('fence-commit');
    expect(checkbox).toBeChecked();
    expect(checkbox).not.toBeDisabled();
    await userEvent.click(checkbox);
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'fenceCommit', value: false })
    );
  });

  it('disables the commit checkbox when fence is unavailable or mode is disabled', () => {
    renderTab({ fenceAvailable: false, fenceMode: 'optional_off', fenceCommit: true });
    const whenUnavailable = screen.getByTestId('fence-commit');
    expect(whenUnavailable).toBeDisabled();
    expect(whenUnavailable).not.toBeChecked();

    renderTab({ fenceAvailable: true, fenceMode: 'disabled', fenceCommit: true });
    const whenDisabled = screen.getAllByTestId('fence-commit')[1];
    expect(whenDisabled).toBeDisabled();
    expect(whenDisabled).not.toBeChecked();
  });
});
