import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import AuthGate from './AuthGate';

describe('AuthGate', () => {
  it('shows a sign-in heading and a real /auth/login link', () => {
    render(<AuthGate />);
    expect(screen.getByRole('heading', { name: /sign in to schmux/i })).toBeInTheDocument();
    const link = screen.getByRole('link', { name: /sign in with github/i });
    expect(link).toHaveAttribute('href', '/auth/login');
  });
});
