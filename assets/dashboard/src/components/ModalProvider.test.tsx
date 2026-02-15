import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ModalProvider, { useModal } from './ModalProvider';

function ModalTrigger({ action }: { action: (modal: ReturnType<typeof useModal>) => void }) {
  const modal = useModal();
  return <button onClick={() => action(modal)}>trigger</button>;
}

describe('ModalProvider', () => {
  // Suppress React console warnings for act() boundaries in keyboard tests
  const originalConsoleError = console.error;
  beforeEach(() => {
    console.error = vi.fn();
  });
  afterEach(() => {
    console.error = originalConsoleError;
  });

  it('alert() renders modal with title, message, OK button (no Cancel)', async () => {
    let result: boolean | null | undefined;
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.alert('Alert Title', 'Alert message').then((r) => {
              result = r;
            });
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });

    expect(screen.getByText('Alert Title')).toBeInTheDocument();
    expect(screen.getByText('Alert message')).toBeInTheDocument();
    expect(screen.getByText('OK')).toBeInTheDocument();
    // No cancel button for alert
    expect(screen.queryByText('Cancel')).not.toBeInTheDocument();
  });

  it('confirm() renders Confirm and Cancel buttons', async () => {
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.confirm('Are you sure?');
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });

    expect(screen.getByText('Confirm Action')).toBeInTheDocument();
    expect(screen.getByText('Are you sure?')).toBeInTheDocument();
    expect(screen.getByText('Confirm')).toBeInTheDocument();
    expect(screen.getByText('Cancel')).toBeInTheDocument();
  });

  it('prompt() renders input field', async () => {
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.prompt('Enter name', { placeholder: 'Type here' });
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });

    expect(screen.getByText('Enter name')).toBeInTheDocument();
    const input = document.getElementById('modal-prompt-input') as HTMLInputElement;
    expect(input).toBeInTheDocument();
    expect(input.placeholder).toBe('Type here');
  });

  it('clicking confirm resolves with true', async () => {
    let result: boolean | null | undefined;
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.confirm('Do it?').then((r) => {
              result = r;
            });
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });
    await act(async () => {
      screen.getByText('Confirm').click();
    });

    expect(result).toBe(true);
  });

  it('clicking cancel resolves with null', async () => {
    let result: boolean | null | undefined;
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.confirm('Do it?').then((r) => {
              result = r;
            });
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });
    await act(async () => {
      screen.getByText('Cancel').click();
    });

    expect(result).toBeNull();
  });

  it('Enter key confirms non-prompt modal', async () => {
    let result: boolean | null | undefined;
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.confirm('Do it?').then((r) => {
              result = r;
            });
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });

    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    });

    expect(result).toBe(true);
  });

  it('Escape key cancels non-prompt modal with cancel button', async () => {
    let result: boolean | null | undefined;
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.confirm('Do it?').then((r) => {
              result = r;
            });
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });

    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(result).toBeNull();
  });

  it('danger mode applies btn--danger class', async () => {
    render(
      <ModalProvider>
        <ModalTrigger
          action={(modal) => {
            modal.confirm('Delete?', { danger: true, confirmText: 'Delete' });
          }}
        />
      </ModalProvider>
    );

    await act(async () => {
      screen.getByText('trigger').click();
    });

    const deleteBtn = screen.getByText('Delete');
    expect(deleteBtn.className).toContain('btn--danger');
  });

  it('useModal outside provider throws', () => {
    function Orphan() {
      useModal();
      return null;
    }

    expect(() => render(<Orphan />)).toThrow('useModal must be used within ModalProvider');
  });
});
