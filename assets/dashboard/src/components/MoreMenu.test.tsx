import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import MoreMenu from './MoreMenu';

// Mock the contexts
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: {
      repos: [{ name: 'test-repo', url: 'https://github.com/test/repo.git' }],
    },
    isNotConfigured: false,
  }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    overlayUnreadCount: 0,
    markOverlaysRead: vi.fn(),
  }),
}));

// Mock the API
vi.mock('../lib/api', () => ({
  getLoreProposals: vi.fn().mockResolvedValue({ proposals: [] }),
}));

// Wrapper component with router
const TestWrapper = ({ children }: { children: React.ReactNode }) => (
  <MemoryRouter>{children}</MemoryRouter>
);

// Helper to render with act
async function renderWithAct(ui: React.ReactNode) {
  return act(async () => {
    return render(<TestWrapper>{ui}</TestWrapper>);
  });
}

describe('MoreMenu', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the More button with up arrow', async () => {
    await renderWithAct(<MoreMenu />);

    const button = screen.getByRole('button', { name: /more/i });
    expect(button).toBeInTheDocument();

    // Verify arrow icon is present (points up when closed)
    const arrow = button.querySelector('.more-menu__arrow');
    expect(arrow).toBeInTheDocument();
    expect(arrow).not.toHaveClass('more-menu__arrow--open');
  });

  it('opens dropdown on click', async () => {
    const user = userEvent.setup();

    await renderWithAct(<MoreMenu />);

    const button = screen.getByRole('button', { name: /more/i });

    // Menu should not be visible initially
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();

    // Click to open
    await user.click(button);

    // Menu should now be visible
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });

  it('shows menu items when open', async () => {
    const user = userEvent.setup();

    await renderWithAct(<MoreMenu />);

    const button = screen.getByRole('button', { name: /more/i });
    await user.click(button);

    // Verify menu items are visible
    expect(screen.getByRole('menuitem', { name: /remote hosts/i })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /tips/i })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /config/i })).toBeInTheDocument();
  });

  it('closes dropdown when clicking outside', async () => {
    const user = userEvent.setup();

    await act(async () => {
      render(
        <TestWrapper>
          <div data-testid="outside">Outside content</div>
          <MoreMenu />
        </TestWrapper>
      );
    });

    const button = screen.getByRole('button', { name: /more/i });

    // Open menu
    await user.click(button);
    expect(screen.getByRole('menu')).toBeInTheDocument();

    // Click outside
    await user.click(screen.getByTestId('outside'));

    // Menu should close
    await waitFor(() => {
      expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    });
  });

  it('closes dropdown on Escape key', async () => {
    await renderWithAct(<MoreMenu />);

    const button = screen.getByRole('button', { name: /more/i });

    // Open menu by clicking
    fireEvent.click(button);
    expect(screen.getByRole('menu')).toBeInTheDocument();

    // Press Escape
    fireEvent.keyDown(document, { key: 'Escape' });

    // Menu should close
    await waitFor(() => {
      expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    });
  });

  it('has correct aria attributes', async () => {
    const user = userEvent.setup();

    await renderWithAct(<MoreMenu />);

    const button = screen.getByRole('button', { name: /more/i });

    // Check initial aria attributes
    expect(button).toHaveAttribute('aria-haspopup', 'menu');
    expect(button).toHaveAttribute('aria-expanded', 'false');

    // Open menu
    await user.click(button);

    // aria-expanded should now be true
    expect(button).toHaveAttribute('aria-expanded', 'true');

    // Menu should have role="menu"
    expect(screen.getByRole('menu')).toBeInTheDocument();

    // Menu items should have role="menuitem"
    const menuItems = screen.getAllByRole('menuitem');
    expect(menuItems.length).toBeGreaterThan(0);
  });

  it('toggles arrow direction when opened and closed', async () => {
    const user = userEvent.setup();

    await renderWithAct(<MoreMenu />);

    const button = screen.getByRole('button', { name: /more/i });
    const arrow = button.querySelector('.more-menu__arrow');

    // Arrow should not have --open class when closed
    expect(arrow).not.toHaveClass('more-menu__arrow--open');

    // Open menu
    await user.click(button);

    // Arrow should have --open class when open
    expect(arrow).toHaveClass('more-menu__arrow--open');

    // Close menu by clicking button again
    await user.click(button);

    // Arrow should no longer have --open class
    await waitFor(() => {
      expect(arrow).not.toHaveClass('more-menu__arrow--open');
    });
  });

  it('returns null when collapsed prop is true', () => {
    render(<MoreMenu collapsed />, { wrapper: TestWrapper });
    expect(screen.queryByRole('button', { name: /more/i })).not.toBeInTheDocument();
  });
});
