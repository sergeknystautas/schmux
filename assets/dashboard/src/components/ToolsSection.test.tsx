import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import ToolsSection from './ToolsSection';

let mockProposalVersion = 0;

// Mock the contexts
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: {
      repos: [{ name: 'test-repo', url: 'https://github.com/test/repo.git' }],
      lore: { enabled: true },
      repofeed: { enabled: true },
      timelapse: { enabled: true },
      personas_enabled: true,
      comm_styles_enabled: true,
    },
  }),
}));

vi.mock('../contexts/CurationContext', () => ({
  useCuration: () => ({
    proposalVersion: mockProposalVersion,
  }),
}));

vi.mock('../contexts/OverlayContext', () => ({
  useOverlay: () => ({
    overlayUnreadCount: 3,
    markOverlaysRead: vi.fn(),
  }),
}));

vi.mock('../contexts/FeaturesContext', () => ({
  useFeatures: () => ({
    features: {
      tunnel: true,
      github: true,
      telemetry: true,
      update: true,
      dashboardsx: true,
      model_registry: true,
      repofeed: true,
      subreddit: true,
    },
    loading: false,
  }),
}));

// Mock the API
vi.mock('../lib/api', () => ({
  getLoreProposals: vi.fn().mockResolvedValue({
    batches: [
      { status: 'pending', learnings: [{ status: 'pending' }] },
      { status: 'pending', learnings: [{ status: 'pending' }] },
    ],
  }),
}));

vi.mock('../lib/spawn-api', () => ({
  getAllSpawnEntries: vi.fn().mockResolvedValue([]),
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
    mockProposalVersion = 0;
  });

  it('renders expanded by default with Tools header', async () => {
    await renderWithAct(<ToolsSection />);

    expect(screen.getByText('Tools')).toBeInTheDocument();
    expect(screen.getByTestId('tools-collapse-btn')).toBeInTheDocument();
  });

  it('shows all menu items when expanded', async () => {
    await renderWithAct(<ToolsSection />);

    expect(screen.getByText('Overlays')).toBeInTheDocument();
    expect(screen.getByText('Autolearn')).toBeInTheDocument();
    expect(screen.getByText('Personas')).toBeInTheDocument();
    expect(screen.getByText('Comm Styles')).toBeInTheDocument();
    expect(screen.getByText('Repofeed')).toBeInTheDocument();
    expect(screen.getByText('Timelapse')).toBeInTheDocument();
    expect(screen.getByText('Environment')).toBeInTheDocument();
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

  it('re-fetches lore counts when proposalVersion changes', async () => {
    const { getLoreProposals } = await import('../lib/api');
    const mockGetLoreProposals = getLoreProposals as ReturnType<typeof vi.fn>;

    await renderWithAct(<ToolsSection />);

    // Initial fetch on mount
    expect(mockGetLoreProposals).toHaveBeenCalledTimes(1);

    // Bump proposalVersion and re-render
    cleanup();
    mockProposalVersion = 1;
    await renderWithAct(<ToolsSection />);

    // Should have fetched again due to proposalVersion change
    expect(mockGetLoreProposals).toHaveBeenCalledTimes(2);
  });

  it('removes lore badge when proposals are all dismissed', async () => {
    const { getLoreProposals } = await import('../lib/api');
    const mockGetLoreProposals = getLoreProposals as ReturnType<typeof vi.fn>;

    await renderWithAct(<ToolsSection />);

    // Initially shows badge with count 2
    expect(screen.getByText('2')).toBeInTheDocument();

    // Simulate all proposals dismissed: return empty proposals on next fetch
    cleanup();
    mockGetLoreProposals.mockResolvedValue({ batches: [] });
    mockProposalVersion = 1;
    await renderWithAct(<ToolsSection />);

    // Badge should be gone
    await waitFor(() => {
      expect(screen.queryByText('2')).not.toBeInTheDocument();
    });
  });
});
