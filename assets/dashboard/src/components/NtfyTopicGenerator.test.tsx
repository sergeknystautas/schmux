import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { NtfyTopicGenerator } from './NtfyTopicGenerator';

describe('NtfyTopicGenerator', () => {
  it('renders "Generate secure topic" button', () => {
    render(<NtfyTopicGenerator currentTopic="" onChange={vi.fn()} />);
    expect(screen.getByText('Generate secure topic')).toBeInTheDocument();
  });

  it('clicking button calls onChange with value matching /^schmux-[0-9a-f]{32}$/', async () => {
    const onChange = vi.fn();
    render(<NtfyTopicGenerator currentTopic="" onChange={onChange} />);

    await userEvent.click(screen.getByText('Generate secure topic'));

    expect(onChange).toHaveBeenCalledTimes(1);
    const topic = onChange.mock.calls[0][0];
    expect(topic).toMatch(/^schmux-[0-9a-f]{32}$/);
  });

  it('QR code SVG renders after generation', async () => {
    const onChange = vi.fn();
    const { container } = render(<NtfyTopicGenerator currentTopic="" onChange={onChange} />);

    // No QR before clicking
    expect(container.querySelector('svg')).toBeNull();

    await userEvent.click(screen.getByText('Generate secure topic'));

    // After clicking, onChange is called but currentTopic prop hasn't changed yet.
    // Re-render with the generated topic to see the QR.
    const topic = onChange.mock.calls[0][0];
    const { container: container2 } = render(
      <NtfyTopicGenerator currentTopic={topic} onChange={onChange} />
    );

    // The component should show QR after generation
    expect(container2.querySelector('svg')).not.toBeNull();
  });

  it('two clicks produce different topics', async () => {
    const onChange = vi.fn();
    const { rerender } = render(<NtfyTopicGenerator currentTopic="" onChange={onChange} />);

    await userEvent.click(screen.getByText('Generate secure topic'));
    const topic1 = onChange.mock.calls[0][0];

    rerender(<NtfyTopicGenerator currentTopic={topic1} onChange={onChange} />);
    await userEvent.click(screen.getByText('Generate secure topic'));
    const topic2 = onChange.mock.calls[1][0];

    expect(topic1).not.toBe(topic2);
  });

  it('when currentTopic matches the pattern, QR code is shown immediately', () => {
    const { container } = render(
      <NtfyTopicGenerator
        currentTopic="schmux-0123456789abcdef0123456789abcdef"
        onChange={vi.fn()}
      />
    );
    expect(container.querySelector('svg')).not.toBeNull();
  });

  it('when currentTopic does not match the pattern, no QR code is shown', () => {
    const { container } = render(
      <NtfyTopicGenerator currentTopic="my-custom-topic" onChange={vi.fn()} />
    );
    expect(container.querySelector('svg')).toBeNull();
  });
});
