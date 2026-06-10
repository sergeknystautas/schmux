import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import AuthGateBoundary from './AuthGateBoundary';

const mockUseAuth = vi.fn();
vi.mock('../contexts/AuthContext', () => ({
  useAuth: () => mockUseAuth(),
}));
vi.mock('./AuthGate', () => ({
  default: () => <div data-testid="auth-gate" />,
}));

beforeEach(() => vi.clearAllMocks());

function child() {
  return <div data-testid="app-content" />;
}

describe('AuthGateBoundary', () => {
  it('renders the app when authenticated', () => {
    mockUseAuth.mockReturnValue({ authenticated: true, loading: false, renewing: false });
    render(<AuthGateBoundary>{child()}</AuthGateBoundary>);
    expect(screen.getByTestId('app-content')).toBeInTheDocument();
    expect(screen.queryByTestId('auth-gate')).not.toBeInTheDocument();
  });

  it('renders the app when auth is disabled (null)', () => {
    mockUseAuth.mockReturnValue({ authenticated: null, loading: false, renewing: false });
    render(<AuthGateBoundary>{child()}</AuthGateBoundary>);
    expect(screen.getByTestId('app-content')).toBeInTheDocument();
  });

  it('renders the gate when unauthenticated', () => {
    mockUseAuth.mockReturnValue({ authenticated: false, loading: false, renewing: false });
    render(<AuthGateBoundary>{child()}</AuthGateBoundary>);
    expect(screen.getByTestId('auth-gate')).toBeInTheDocument();
    expect(screen.queryByTestId('app-content')).not.toBeInTheDocument();
  });

  it('renders neither app nor gate while loading', () => {
    mockUseAuth.mockReturnValue({ authenticated: null, loading: true, renewing: false });
    render(<AuthGateBoundary>{child()}</AuthGateBoundary>);
    expect(screen.queryByTestId('app-content')).not.toBeInTheDocument();
    expect(screen.queryByTestId('auth-gate')).not.toBeInTheDocument();
  });

  it('shows Reconnecting while renewing', () => {
    mockUseAuth.mockReturnValue({ authenticated: true, loading: false, renewing: true });
    render(<AuthGateBoundary>{child()}</AuthGateBoundary>);
    expect(screen.getByText(/reconnecting/i)).toBeInTheDocument();
    expect(screen.queryByTestId('app-content')).not.toBeInTheDocument();
  });
});
