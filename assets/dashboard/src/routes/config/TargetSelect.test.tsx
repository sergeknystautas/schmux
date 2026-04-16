import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import TargetSelect from './TargetSelect';
import type { Model } from '../../lib/types';

const models: Model[] = [
  {
    id: 'gpt-4',
    display_name: 'GPT-4',
    provider: 'openai',
    configured: true,
    runners: ['openai'],
  },
  {
    id: 'unconfigured-model',
    display_name: 'Unconfigured',
    provider: 'x',
    configured: false,
    runners: ['x'],
  },
];

describe('TargetSelect', () => {
  it('renders Disabled option by default', () => {
    render(<TargetSelect value="" onChange={() => {}} models={models} />);
    expect(screen.getByText('Disabled')).toBeInTheDocument();
  });

  it('renders all models passed to it', () => {
    render(<TargetSelect value="" onChange={() => {}} models={models} />);
    expect(screen.getByText('GPT-4')).toBeInTheDocument();
    expect(screen.getByText('Unconfigured')).toBeInTheDocument();
  });

  it('renders None option when includeNoneOption is provided', () => {
    render(
      <TargetSelect
        value=""
        onChange={() => {}}
        includeDisabledOption={false}
        includeNoneOption="None (capture only)"
        models={[]}
      />
    );
    expect(screen.getByText('None (capture only)')).toBeInTheDocument();
    expect(screen.queryByText('Disabled')).not.toBeInTheDocument();
  });

  it('calls onChange when value changes', async () => {
    const onChange = vi.fn();
    render(<TargetSelect value="" onChange={onChange} models={models} />);
    await userEvent.selectOptions(screen.getByRole('combobox'), 'gpt-4');
    expect(onChange).toHaveBeenCalledWith('gpt-4');
  });

  it('calls onChange with empty string when Disabled is selected', async () => {
    const onChange = vi.fn();
    render(<TargetSelect value="gpt-4" onChange={onChange} models={models} />);
    await userEvent.selectOptions(screen.getByRole('combobox'), '');
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('respects disabled prop', () => {
    render(<TargetSelect value="" onChange={() => {}} disabled={true} models={[]} />);
    expect(screen.getByRole('combobox')).toBeDisabled();
  });

  it('renders the current value as a disabled unavailable option when it is not in models', () => {
    render(<TargetSelect value="stale-model" onChange={() => {}} models={models} />);
    const option = screen.getByText('stale-model (unavailable)') as HTMLOptionElement;
    expect(option).toBeInTheDocument();
    expect(option.value).toBe('stale-model');
    expect(option.disabled).toBe(true);
    expect(screen.getByRole('combobox')).toHaveValue('stale-model');
  });

  it('does not render an unavailable option when the current value is in models', () => {
    render(<TargetSelect value="gpt-4" onChange={() => {}} models={models} />);
    expect(screen.queryByText(/unavailable/)).not.toBeInTheDocument();
  });
});
