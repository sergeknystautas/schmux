import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import ToolsSection from './ToolsSection';

// Mock the contexts
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: {
      repos: [{ name: 'test-repo', url: 'https://github.com/test/repo.git' }],
      remote_access: { enabled: true },
    },
    isNotConfigured: false,
  }),
}));

vi.mock('../contexts/OverlayContext', () => ({
  useOverlay: () => ({
    overlayUnreadCount: 3,
    markOverlaysRead: vi.fn(),
  }),
}));

// Mock the API
vi.mock('../lib/api', () => ({
  getLoreProposals: vi.fn().mockResolvedValue({
    proposals: [{ status: 'pending' }, { status: 'pending' }],
  }),
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

describe('ToolsSection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  it('renders expanded by default with Tools header', async () => {
    await renderWithAct(<ToolsSection />);

    expect(screen.getByText('Tools')).toBeInTheDocument();
    expect(screen.getByTestId('tools-collapse-btn')).toBeInTheDocument();
  });

  it('shows all menu items when expanded', async () => {
    await renderWithAct(<ToolsSection />);

    expect(screen.getByText('Overlays')).toBeInTheDocument();
    expect(screen.getByText('Lore')).toBeInTheDocument();
    expect(screen.getByText('Personas')).toBeInTheDocument();
    expect(screen.getByText('Remote Hosts')).toBeInTheDocument();
    expect(screen.getByText('Tips')).toBeInTheDocument();
    expect(screen.getByText('Config')).toBeInTheDocument();
  });

  it('shows badges in expanded state', async () => {
    await renderWithAct(<ToolsSection />);

    // Overlay badge (unread count = 3)
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('collapses to icon bar when clicking collapse button', async () => {
    const user = userEvent.setup();
    await renderWithAct(<ToolsSection />);

    // Click collapse
    await user.click(screen.getByTestId('tools-collapse-btn'));

    // Labels should not be visible
    expect(screen.queryByText('Tools')).not.toBeInTheDocument();
    expect(screen.queryByText('Overlays')).not.toBeInTheDocument();

    // Expand button should be present
    expect(screen.getByTestId('tools-expand-btn')).toBeInTheDocument();

    // Icon links should be present (by aria-label)
    expect(screen.getByLabelText('Overlays')).toBeInTheDocument();
    expect(screen.getByLabelText('Config')).toBeInTheDocument();
  });

  it('expands back when clicking expand button', async () => {
    const user = userEvent.setup();
    await renderWithAct(<ToolsSection />);

    // Collapse
    await user.click(screen.getByTestId('tools-collapse-btn'));
    expect(screen.queryByText('Tools')).not.toBeInTheDocument();

    // Expand
    await user.click(screen.getByTestId('tools-expand-btn'));
    expect(screen.getByText('Tools')).toBeInTheDocument();
    expect(screen.getByText('Overlays')).toBeInTheDocument();
  });

  it('persists collapsed state to localStorage', async () => {
    const user = userEvent.setup();
    await renderWithAct(<ToolsSection />);

    // Initially not collapsed
    expect(localStorage.getItem('schmux-tools-collapsed')).toBe('false');

    // Collapse
    await user.click(screen.getByTestId('tools-collapse-btn'));
    expect(localStorage.getItem('schmux-tools-collapsed')).toBe('true');
  });

  it('reads collapsed state from localStorage on mount', async () => {
    localStorage.setItem('schmux-tools-collapsed', 'true');
    await renderWithAct(<ToolsSection />);

    // Should start collapsed
    expect(screen.queryByText('Tools')).not.toBeInTheDocument();
    expect(screen.getByTestId('tools-expand-btn')).toBeInTheDocument();
  });

  it('returns null when navCollapsed is true', async () => {
    await renderWithAct(<ToolsSection navCollapsed />);
    expect(screen.queryByTestId('tools-section')).not.toBeInTheDocument();
  });

  it('shows badge dots on icons in collapsed state', async () => {
    const user = userEvent.setup();
    await renderWithAct(<ToolsSection />);

    await user.click(screen.getByTestId('tools-collapse-btn'));

    // Badge dots should exist (as span elements with badge testid)
    const overlayLink = screen.getByLabelText('Overlays');
    const badgeDot = overlayLink.querySelector(
      '[data-testid="icon-badge"][data-severity="danger"]'
    );
    expect(badgeDot).toBeInTheDocument();
  });
});
