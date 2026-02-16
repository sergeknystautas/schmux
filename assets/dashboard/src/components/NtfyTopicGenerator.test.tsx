import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {
  NtfyTopicGenerateButton,
  NtfyTopicQRDisplay,
  NtfyTopicGenerator,
} from './NtfyTopicGenerator';

describe('NtfyTopicGenerateButton', () => {
  it('renders "Generate secure topic" button', () => {
    render(<NtfyTopicGenerateButton onChange={vi.fn()} />);
    expect(screen.getByText('Generate secure topic')).toBeInTheDocument();
  });

  it('clicking button calls onChange with value matching /^schmux-[0-9a-f]{32}$/', async () => {
    const onChange = vi.fn();
    render(<NtfyTopicGenerateButton onChange={onChange} />);

    await userEvent.click(screen.getByText('Generate secure topic'));

    expect(onChange).toHaveBeenCalledTimes(1);
    const topic = onChange.mock.calls[0][0];
    expect(topic).toMatch(/^schmux-[0-9a-f]{32}$/);
  });

  it('two clicks produce different topics', async () => {
    const onChange = vi.fn();
    render(<NtfyTopicGenerateButton onChange={onChange} />);

    await userEvent.click(screen.getByText('Generate secure topic'));
    await userEvent.click(screen.getByText('Generate secure topic'));

    const topic1 = onChange.mock.calls[0][0];
    const topic2 = onChange.mock.calls[1][0];
    expect(topic1).not.toBe(topic2);
  });
});

describe('NtfyTopicQRDisplay', () => {
  it('shows placeholder when topic is empty', () => {
    render(<NtfyTopicQRDisplay topic="" />);
    expect(screen.getByText(/QR code will appear here/)).toBeInTheDocument();
  });

  it('shows placeholder when topic does not match secure pattern', () => {
    const { container } = render(<NtfyTopicQRDisplay topic="my-custom-topic" />);
    expect(container.querySelector('svg')).toBeNull();
    expect(screen.getByText(/QR code will appear here/)).toBeInTheDocument();
  });

  it('shows QR code when topic matches secure pattern', () => {
    const { container } = render(
      <NtfyTopicQRDisplay topic="schmux-0123456789abcdef0123456789abcdef" />
    );
    expect(container.querySelector('svg')).not.toBeNull();
    expect(screen.queryByText(/QR code will appear here/)).toBeNull();
  });
});

describe('NtfyTopicGenerator (combined)', () => {
  it('renders button and placeholder', () => {
    render(<NtfyTopicGenerator currentTopic="" onChange={vi.fn()} />);
    expect(screen.getByText('Generate secure topic')).toBeInTheDocument();
    expect(screen.getByText(/QR code will appear here/)).toBeInTheDocument();
  });

  it('shows QR when currentTopic matches secure pattern', () => {
    const { container } = render(
      <NtfyTopicGenerator
        currentTopic="schmux-0123456789abcdef0123456789abcdef"
        onChange={vi.fn()}
      />
    );
    expect(container.querySelector('svg')).not.toBeNull();
    expect(screen.queryByText(/QR code will appear here/)).toBeNull();
  });
});
