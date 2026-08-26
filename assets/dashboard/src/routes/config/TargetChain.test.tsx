import { render, screen, fireEvent } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import TargetChain from './TargetChain';

const options = [
  { id: 'MiniMax-M3::api', label: 'MiniMax M3 (API)' },
  { id: 'GLM-5.3::api', label: 'GLM 5.3 (API)' },
];

describe('TargetChain', () => {
  it('renders primary and fallback rows with labels', () => {
    render(
      <TargetChain
        idPrefix="bs"
        value={['MiniMax-M3::api', 'GLM-5.3::api']}
        onChange={() => {}}
        options={options}
      />
    );
    expect(screen.getByText('Primary')).toBeTruthy();
    expect(screen.getByText('Fallback 1')).toBeTruthy();
  });

  it('appends a fallback row on Add fallback', () => {
    const onChange = vi.fn();
    render(
      <TargetChain
        idPrefix="bs"
        value={['MiniMax-M3::api']}
        onChange={onChange}
        options={options}
      />
    );
    fireEvent.click(screen.getByText('Add fallback'));
    expect(onChange).toHaveBeenCalledWith(['MiniMax-M3::api', '']);
  });

  it('removes a fallback row', () => {
    const onChange = vi.fn();
    render(
      <TargetChain
        idPrefix="bs"
        value={['MiniMax-M3::api', 'GLM-5.3::api']}
        onChange={onChange}
        options={options}
      />
    );
    fireEvent.click(screen.getByText('Remove'));
    expect(onChange).toHaveBeenCalledWith(['MiniMax-M3::api']);
  });

  it('shows unavailable error for a missing target id', () => {
    render(
      <TargetChain idPrefix="bs" value={['nope::api']} onChange={() => {}} options={options} />
    );
    expect(screen.getByText('Selected target is not available.')).toBeTruthy();
  });

  it('labels the button Add target when the chain is empty', () => {
    render(<TargetChain idPrefix="bs" value={[]} onChange={() => {}} options={options} />);
    expect(screen.getByText('Add target')).toBeTruthy();
  });
});
