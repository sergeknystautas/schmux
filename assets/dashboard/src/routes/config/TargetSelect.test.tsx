import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import TargetSelect from './TargetSelect';
import type { Model, RunTargetResponse } from '../../lib/types';

const detectedTargets: RunTargetResponse[] = [
  { name: 'claude', command: 'claude', type: 'promptable', source: 'detected' },
  { name: 'codex', command: 'codex', type: 'promptable', source: 'detected' },
];

const models: Model[] = [
  {
    id: 'gpt-4',
    display_name: 'GPT-4',
    provider: 'openai',
    category: 'external',
    configured: true,
    runners: { openai: { available: true, configured: true } },
  },
  {
    id: 'unconfigured-model',
    display_name: 'Unconfigured',
    provider: 'x',
    category: 'external',
    configured: false,
    runners: { x: { available: true, configured: false } },
  },
];

const promptableTargets: RunTargetResponse[] = [
  { name: 'my-agent', command: 'my-agent', type: 'promptable', source: 'user' },
];

describe('TargetSelect', () => {
  it('renders Disabled option by default', () => {
    render(
      <TargetSelect
        value=""
        onChange={() => {}}
        detectedTargets={detectedTargets}
        models={models}
        promptableTargets={promptableTargets}
      />
    );
    expect(screen.getByText('Disabled')).toBeInTheDocument();
  });

  it('renders detected targets in optgroup', () => {
    render(
      <TargetSelect
        value=""
        onChange={() => {}}
        detectedTargets={detectedTargets}
        models={models}
        promptableTargets={promptableTargets}
      />
    );
    expect(screen.getByText('claude')).toBeInTheDocument();
    expect(screen.getByText('codex')).toBeInTheDocument();
  });

  it('renders only configured models', () => {
    render(
      <TargetSelect
        value=""
        onChange={() => {}}
        detectedTargets={[]}
        models={models}
        promptableTargets={[]}
      />
    );
    expect(screen.getByText('GPT-4')).toBeInTheDocument();
    expect(screen.queryByText('Unconfigured')).not.toBeInTheDocument();
  });

  it('renders user promptable targets', () => {
    render(
      <TargetSelect
        value=""
        onChange={() => {}}
        detectedTargets={[]}
        models={[]}
        promptableTargets={promptableTargets}
      />
    );
    expect(screen.getByText('my-agent')).toBeInTheDocument();
  });

  it('renders None option when includeNoneOption is provided', () => {
    render(
      <TargetSelect
        value=""
        onChange={() => {}}
        includeDisabledOption={false}
        includeNoneOption="None (capture only)"
        detectedTargets={[]}
        models={[]}
        promptableTargets={[]}
      />
    );
    expect(screen.getByText('None (capture only)')).toBeInTheDocument();
    expect(screen.queryByText('Disabled')).not.toBeInTheDocument();
  });

  it('calls onChange when value changes', async () => {
    const onChange = vi.fn();
    render(
      <TargetSelect
        value=""
        onChange={onChange}
        detectedTargets={detectedTargets}
        models={[]}
        promptableTargets={[]}
      />
    );
    await userEvent.selectOptions(screen.getByRole('combobox'), 'claude');
    expect(onChange).toHaveBeenCalledWith('claude');
  });

  it('calls onChange with empty string when Disabled is selected', async () => {
    const onChange = vi.fn();
    render(
      <TargetSelect
        value="claude"
        onChange={onChange}
        detectedTargets={detectedTargets}
        models={[]}
        promptableTargets={[]}
      />
    );
    await userEvent.selectOptions(screen.getByRole('combobox'), '');
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('respects disabled prop', () => {
    render(
      <TargetSelect
        value=""
        onChange={() => {}}
        disabled={true}
        detectedTargets={[]}
        models={[]}
        promptableTargets={[]}
      />
    );
    expect(screen.getByRole('combobox')).toBeDisabled();
  });
});
