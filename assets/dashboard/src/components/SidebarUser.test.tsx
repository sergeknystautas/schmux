import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import SidebarUser from './SidebarUser';

const mockUseAuth = vi.fn();
vi.mock('../contexts/AuthContext', () => ({
  useAuth: () => mockUseAuth(),
}));

beforeEach(() => vi.clearAllMocks());

describe('SidebarUser', () => {
  it('renders nothing when not authenticated', () => {
    mockUseAuth.mockReturnValue({ authenticated: false, user: null, logout: vi.fn() });
    const { container } = render(<SidebarUser navCollapsed={false} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing when auth disabled (null)', () => {
    mockUseAuth.mockReturnValue({ authenticated: null, user: null, logout: vi.fn() });
    const { container } = render(<SidebarUser navCollapsed={false} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows login and sign out when authenticated and expanded', async () => {
    const logout = vi.fn();
    mockUseAuth.mockReturnValue({
      authenticated: true,
      user: { login: 'octocat', name: 'Mona', avatar_url: 'a.png' },
      logout,
    });
    render(<SidebarUser navCollapsed={false} />);
    expect(screen.getByText('octocat')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /sign out/i }));
    expect(logout).toHaveBeenCalledOnce();
  });

  it('hides the login name when collapsed', () => {
    mockUseAuth.mockReturnValue({
      authenticated: true,
      user: { login: 'octocat', name: 'Mona', avatar_url: 'a.png' },
      logout: vi.fn(),
    });
    render(<SidebarUser navCollapsed={true} />);
    expect(screen.queryByText('octocat')).not.toBeInTheDocument();
  });
});
