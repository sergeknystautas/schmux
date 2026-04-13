import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import AutolearnPage from './AutolearnPage';
import type { AutolearnLearning, LearningLayer } from '../lib/types';

// --- Mocks ---

vi.mock('../lib/api', () => ({
  getAutolearnBatches: vi.fn(),
  getAutolearnStatus: vi.fn(),
  getAutolearnEntries: vi.fn(),
  clearAutolearnEntries: vi.fn(),
  updateAutolearnLearning: vi.fn(),
  applyAutolearnMerge: vi.fn(),
  getAutolearnPendingMerge: vi.fn(),
  startAutolearnMerge: vi.fn(),
  pushAutolearnMerge: vi.fn(),
  updateAutolearnPendingMerge: vi.fn(),
  deleteAutolearnPendingMerge: vi.fn(),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

vi.mock('../lib/spawn-api', () => ({
  getAllSpawnEntries: vi.fn(),
  pinSpawnEntry: vi.fn(),
  dismissSpawnEntry: vi.fn(),
}));

const mockConfig = { repos: [{ name: 'test-repo' }] };
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: mockConfig,
  }),
}));

vi.mock('../contexts/CurationContext', () => ({
  useCuration: () => ({
    startCuration: vi.fn(),
    activeCurations: {},
    pendingCurations: {},
    onComplete: () => () => {},
    invalidateProposals: vi.fn(),
  }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    curatorEvents: {},
  }),
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({
    success: vi.fn(),
    error: vi.fn(),
  }),
}));

const mockAlert = vi.fn().mockResolvedValue(true);
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert, confirm: vi.fn().mockResolvedValue(true) }),
}));

vi.mock('../hooks/useTheme', () => ({
  default: () => ({ theme: 'dark' }),
}));

vi.mock('react-diff-viewer-continued', () => ({
  default: () => <div data-testid="mock-diff-viewer" />,
  DiffMethod: { CHARS: 'chars' },
}));

import {
  getAutolearnBatches,
  getAutolearnStatus,
  getAutolearnPendingMerge,
  updateAutolearnLearning,
} from '../lib/api';
import { getAllSpawnEntries } from '../lib/spawn-api';

const mockGetAutolearnBatches = vi.mocked(getAutolearnBatches);
const mockGetAutolearnStatus = vi.mocked(getAutolearnStatus);
const mockGetAutolearnPendingMerge = vi.mocked(getAutolearnPendingMerge);
const mockUpdateAutolearnLearning = vi.mocked(updateAutolearnLearning);
const mockGetAllSpawnEntries = vi.mocked(getAllSpawnEntries);

function makeRule(overrides: Partial<AutolearnLearning> = {}): AutolearnLearning {
  return {
    id: 'r1',
    title: 'Always run tests before committing',
    category: 'workflow',
    suggested_layer: 'repo_private' as LearningLayer,
    status: 'pending',
    sources: [],
    ...overrides,
  };
}

function renderPage() {
  return render(
    <MemoryRouter>
      <AutolearnPage />
    </MemoryRouter>
  );
}

/** Set up standard mocks that resolve with empty/enabled defaults. */
function setupEmptyMocks() {
  mockGetAutolearnBatches.mockResolvedValue({ batches: [] });
  mockGetAllSpawnEntries.mockResolvedValue([]);
  mockGetAutolearnStatus.mockResolvedValue({
    enabled: true,
    curator_configured: true,
    curate_on_dispose: 'ask',
    llm_target: '',
    issues: [],
  });
  mockGetAutolearnPendingMerge.mockResolvedValue(null);
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('AutolearnPage', () => {
  // --- 1. Loading state ---
  it('renders loading state', () => {
    // Make all API calls never resolve so loading stays visible
    mockGetAutolearnBatches.mockReturnValue(new Promise(() => {}));
    mockGetAllSpawnEntries.mockReturnValue(new Promise(() => {}));
    mockGetAutolearnStatus.mockReturnValue(new Promise(() => {}));
    mockGetAutolearnPendingMerge.mockReturnValue(new Promise(() => {}));

    renderPage();
    expect(screen.getByText('Loading autolearn...')).toBeInTheDocument();
  });

  // --- 2. Empty state ---
  it('renders empty state when no proposals', async () => {
    setupEmptyMocks();
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText('Nothing to review. New insights will appear here as agents work.')
      ).toBeInTheDocument();
    });
  });

  // --- 3. Renders cards for pending proposals ---
  it('renders cards for pending proposals', async () => {
    setupEmptyMocks();
    mockGetAutolearnBatches.mockResolvedValue({
      batches: [
        {
          id: 'p1',
          repo: 'test-repo',
          created_at: '2026-04-01T00:00:00Z',
          status: 'pending',
          learnings: [
            makeRule({ id: 'r1', title: 'Run tests before committing' }),
            makeRule({ id: 'r2', title: 'Format code with prettier' }),
          ],
        },
      ],
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Run tests before committing')).toBeInTheDocument();
      expect(screen.getByText('Format code with prettier')).toBeInTheDocument();
    });
  });

  // --- 4. Deduplicates rules with same normalized text across proposals ---
  it('deduplicates rules with same normalized text across proposals', async () => {
    setupEmptyMocks();
    mockGetAutolearnBatches.mockResolvedValue({
      batches: [
        {
          id: 'p1',
          repo: 'test-repo',
          created_at: '2026-04-01T00:00:00Z',
          status: 'pending',
          learnings: [makeRule({ id: 'r1', title: 'Run tests  always' })],
        },
        {
          id: 'p2',
          repo: 'test-repo',
          created_at: '2026-04-02T00:00:00Z',
          status: 'pending',
          learnings: [makeRule({ id: 'r2', title: 'run tests always' })],
        },
      ],
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByTestId('autolearn-page')).toBeInTheDocument();
    });

    // Only ONE card should appear since both rules normalize to "run tests always"
    const cards = screen.getAllByText(/run tests\s+always/i);
    expect(cards).toHaveLength(1);
  });

  // --- 5. Approve propagates to duplicates ---
  it('approve propagates to duplicates', async () => {
    setupEmptyMocks();
    mockGetAutolearnBatches.mockResolvedValue({
      batches: [
        {
          id: 'p1',
          repo: 'test-repo',
          created_at: '2026-04-01T00:00:00Z',
          status: 'pending',
          learnings: [makeRule({ id: 'r1', title: 'Run tests  always' })],
        },
        {
          id: 'p2',
          repo: 'test-repo',
          created_at: '2026-04-02T00:00:00Z',
          status: 'pending',
          learnings: [makeRule({ id: 'r2', title: 'run tests always' })],
        },
      ],
    });

    // updateAutolearnLearning returns the updated proposal with the rule marked approved
    mockUpdateAutolearnLearning.mockResolvedValue({
      id: 'p1',
      repo: 'test-repo',
      created_at: '2026-04-01T00:00:00Z',
      status: 'pending',
      learnings: [makeRule({ id: 'r1', title: 'Run tests  always', status: 'approved' })],
    });

    const user = userEvent.setup();
    renderPage();

    await waitFor(() => {
      expect(screen.getByTestId('autolearn-page')).toBeInTheDocument();
    });

    // Wait for the Approve button to appear
    const approveButton = await screen.findByText('Approve');
    await user.click(approveButton);

    await waitFor(() => {
      // Primary rule update
      expect(mockUpdateAutolearnLearning).toHaveBeenCalledWith('test-repo', 'p1', 'r1', {
        status: 'approved',
      });
      // Duplicate rule update (propagated)
      expect(mockUpdateAutolearnLearning).toHaveBeenCalledWith('test-repo', 'p2', 'r2', {
        status: 'approved',
      });
    });
  });

  // --- 6. Dismiss removes card and propagates to duplicates ---
  it('dismiss removes card and propagates to duplicates', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });

    setupEmptyMocks();
    mockGetAutolearnBatches.mockResolvedValue({
      batches: [
        {
          id: 'p1',
          repo: 'test-repo',
          created_at: '2026-04-01T00:00:00Z',
          status: 'pending',
          learnings: [makeRule({ id: 'r1', title: 'Run tests  always' })],
        },
        {
          id: 'p2',
          repo: 'test-repo',
          created_at: '2026-04-02T00:00:00Z',
          status: 'pending',
          learnings: [makeRule({ id: 'r2', title: 'run tests always' })],
        },
      ],
    });

    mockUpdateAutolearnLearning.mockResolvedValue({
      id: 'p1',
      repo: 'test-repo',
      created_at: '2026-04-01T00:00:00Z',
      status: 'pending',
      learnings: [],
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByTestId('autolearn-page')).toBeInTheDocument();
    });

    // Wait for the Dismiss button to appear
    const dismissButton = await screen.findByText('Dismiss');
    dismissButton.click();

    // AutolearnCard has a 200ms animation delay before calling onDismiss
    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    await waitFor(() => {
      // Primary rule dismissed
      expect(mockUpdateAutolearnLearning).toHaveBeenCalledWith('test-repo', 'p1', 'r1', {
        status: 'dismissed',
      });
      // Duplicate rule dismissed (propagated)
      expect(mockUpdateAutolearnLearning).toHaveBeenCalledWith('test-repo', 'p2', 'r2', {
        status: 'dismissed',
      });
    });

    // After dismissal, the card should be removed from the DOM
    await waitFor(() => {
      expect(screen.queryByText(/run tests\s+always/i)).not.toBeInTheDocument();
    });

    vi.useRealTimers();
  });
});
