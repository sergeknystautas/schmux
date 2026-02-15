import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import ToastProvider, { useToast } from './ToastProvider';

function ToastTrigger({
  type,
  message,
  duration,
}: {
  type: 'show' | 'success' | 'error';
  message: string;
  duration?: number;
}) {
  const toast = useToast();
  return (
    <button
      onClick={() => {
        if (type === 'success') toast.success(message, duration);
        else if (type === 'error') toast.error(message, duration);
        else toast.show(message, undefined, duration);
      }}
    >
      trigger
    </button>
  );
}

describe('ToastProvider', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('show() renders a toast with message', () => {
    render(
      <ToastProvider>
        <ToastTrigger type="show" message="Test toast" />
      </ToastProvider>
    );

    act(() => {
      screen.getByText('trigger').click();
    });

    expect(screen.getByText('Test toast')).toBeInTheDocument();
  });

  it('success() applies toast--success class', () => {
    render(
      <ToastProvider>
        <ToastTrigger type="success" message="Success!" />
      </ToastProvider>
    );

    act(() => {
      screen.getByText('trigger').click();
    });

    const toast = screen.getByText('Success!');
    expect(toast.className).toContain('toast--success');
  });

  it('error() applies toast--error class', () => {
    render(
      <ToastProvider>
        <ToastTrigger type="error" message="Error!" />
      </ToastProvider>
    );

    act(() => {
      screen.getByText('trigger').click();
    });

    const toast = screen.getByText('Error!');
    expect(toast.className).toContain('toast--error');
  });

  it('toast auto-dismisses after duration', () => {
    render(
      <ToastProvider>
        <ToastTrigger type="show" message="Temporary" duration={2000} />
      </ToastProvider>
    );

    act(() => {
      screen.getByText('trigger').click();
    });

    expect(screen.getByText('Temporary')).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(screen.queryByText('Temporary')).not.toBeInTheDocument();
  });

  it('renders multiple toasts simultaneously', () => {
    function MultiTrigger() {
      const toast = useToast();
      return (
        <button
          onClick={() => {
            toast.show('First');
            toast.show('Second');
          }}
        >
          trigger
        </button>
      );
    }

    render(
      <ToastProvider>
        <MultiTrigger />
      </ToastProvider>
    );

    act(() => {
      screen.getByText('trigger').click();
    });

    expect(screen.getByText('First')).toBeInTheDocument();
    expect(screen.getByText('Second')).toBeInTheDocument();
  });

  it('useToast outside provider throws', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const suppressError = (e: Event) => e.preventDefault();
    window.addEventListener('error', suppressError);

    function Orphan() {
      useToast();
      return null;
    }

    expect(() => render(<Orphan />)).toThrow('useToast must be used within ToastProvider');

    window.removeEventListener('error', suppressError);
    spy.mockRestore();
  });
});
