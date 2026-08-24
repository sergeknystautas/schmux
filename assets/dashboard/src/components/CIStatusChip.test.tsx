import React from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import CIStatusChip from './CIStatusChip';

describe('CIStatusChip', () => {
  it.each([
    ['success', 'CI: passing'],
    ['failure', 'CI: failing'],
    ['in_progress', 'CI: running'],
    ['queued', 'CI: queued'],
  ])('renders %s with the expected label', (status, label) => {
    render(<CIStatusChip status={status} url="https://run" />);
    expect(screen.getByLabelText(label)).toBeInTheDocument();
  });

  it('renders nothing when status is absent', () => {
    const { container } = render(<CIStatusChip />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing for an unknown status', () => {
    const { container } = render(<CIStatusChip status="bogus" />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders queued as an outlined circle', () => {
    const { container } = render(<CIStatusChip status="queued" />);
    expect(container.querySelector('.app-header__ci-circle')).not.toBeNull();
  });

  it('renders in_progress as a pulsing dot', () => {
    const { container } = render(<CIStatusChip status="in_progress" />);
    expect(container.querySelector('.app-header__ci-dot')).not.toBeNull();
  });

  it('wraps the chip in a link when url is provided', () => {
    render(<CIStatusChip status="success" url="https://run" />);
    const link = screen.getByLabelText('CI: passing').closest('a');
    expect(link?.getAttribute('href')).toBe('https://run');
  });
});
