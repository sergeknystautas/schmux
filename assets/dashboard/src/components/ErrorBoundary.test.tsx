import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import ErrorBoundary from './ErrorBoundary';

function ThrowingComponent({ message }: { message: string }): React.ReactElement {
  throw new Error(message);
}

describe('ErrorBoundary', () => {
  // Suppress React's console.error and jsdom's window error events
  // for expected error boundary triggers
  const originalConsoleError = console.error;
  const suppressError = (e: Event) => e.preventDefault();
  beforeEach(() => {
    console.error = vi.fn();
    window.addEventListener('error', suppressError);
  });
  afterEach(() => {
    console.error = originalConsoleError;
    window.removeEventListener('error', suppressError);
  });

  it('renders children when no error', () => {
    render(
      <ErrorBoundary>
        <div>Hello World</div>
      </ErrorBoundary>
    );
    expect(screen.getByText('Hello World')).toBeInTheDocument();
  });

  it('shows error UI when child throws', () => {
    render(
      <ErrorBoundary>
        <ThrowingComponent message="test crash" />
      </ErrorBoundary>
    );
    expect(screen.getByText('Dashboard Error')).toBeInTheDocument();
    expect(screen.getByText(/Something went wrong/)).toBeInTheDocument();
  });

  it('displays error message in details', () => {
    render(
      <ErrorBoundary>
        <ThrowingComponent message="specific error msg" />
      </ErrorBoundary>
    );
    expect(screen.getByText('Error details')).toBeInTheDocument();
    expect(screen.getByText(/specific error msg/)).toBeInTheDocument();
  });

  it('has a Reload button', () => {
    render(
      <ErrorBoundary>
        <ThrowingComponent message="crash" />
      </ErrorBoundary>
    );
    expect(screen.getByRole('button', { name: 'Reload' })).toBeInTheDocument();
  });
});
