import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import FlightsTab from './FlightsTab';

describe('FlightsTab', () => {
  it('groups naming and pilot supervision and preserves naming model selection', async () => {
    const dispatch = vi.fn();
    render(
      <FlightsTab
        state={{ flightNameTarget: '', flightPilotTarget: '', typesafeAPIKeySet: false }}
        dispatch={dispatch}
        models={[{ id: 'alpha', label: 'Alpha', source: 'cli' }]}
        pilotModels={[]}
        flightNameTargetMissing={false}
        onTypesafeKeyAction={vi.fn()}
      />
    );
    const naming = screen.getByLabelText('Flight Naming');
    expect(
      within(naming)
        .getAllByRole('option')
        .map((option) => option.getAttribute('value'))
    ).toEqual(['', 'alpha']);
    expect(
      screen.getByText(
        'Model that names flights from their prompt. Leave disabled to label flights by ID.'
      )
    ).toBeInTheDocument();
    expect(screen.getByText('TypeSafe API Key')).toBeInTheDocument();
    await userEvent.selectOptions(naming, 'alpha');
    expect(dispatch).toHaveBeenCalledWith({
      type: 'SET_FIELD',
      field: 'flightNameTarget',
      value: 'alpha',
    });
  });

  it('chooses the pilot model from chat targets, with no fallback option', async () => {
    const dispatch = vi.fn();
    render(
      <FlightsTab
        state={{ flightNameTarget: '', flightPilotTarget: '', typesafeAPIKeySet: false }}
        dispatch={dispatch}
        models={[{ id: 'oneshot', label: 'One-shot', source: 'cli' }]}
        pilotModels={[{ id: 'claude', label: 'Claude' }]}
        flightNameTargetMissing={false}
        onTypesafeKeyAction={vi.fn()}
      />
    );
    const pilot = screen.getByLabelText('Pilot Model');
    expect(
      within(pilot)
        .getAllByRole('option')
        .map((option) => option.textContent)
    ).toEqual(['Select model', 'Claude']);
    await userEvent.selectOptions(pilot, 'claude');
    expect(dispatch).toHaveBeenCalledWith({
      type: 'SET_FIELD',
      field: 'flightPilotTarget',
      value: 'claude',
    });
  });

  it('retains unavailable naming and pilot models and exposes their errors', () => {
    render(
      <FlightsTab
        state={{ flightNameTarget: 'removed', flightPilotTarget: 'gone', typesafeAPIKeySet: true }}
        dispatch={vi.fn()}
        models={[]}
        pilotModels={[]}
        flightNameTargetMissing
        onTypesafeKeyAction={vi.fn()}
      />
    );
    expect(screen.getByLabelText('Flight Naming')).toHaveValue('removed');
    expect(screen.getByText('Selected target is not available.')).toBeInTheDocument();
    expect(screen.getByLabelText('Pilot Model')).toHaveValue('gone');
    expect(screen.getByText('Selected model is not available for chat.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Update TypeSafe Key' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Remove TypeSafe Key' })).toBeInTheDocument();
  });
});
