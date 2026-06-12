import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import userEvent from '@testing-library/user-event';
import BuildMonitorPage from './BuildMonitorPage';

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async (importOriginal) => {
  const mod = await importOriginal<typeof import('react-router-dom')>();
  return { ...mod, useNavigate: () => mockNavigate };
});

function renderPage() {
  return render(
    <MemoryRouter>
      <BuildMonitorPage />
    </MemoryRouter>
  );
}

const mockFeatures = { build_monitor: true };
const mockConfig = { build_monitor: { enabled: true } };

vi.mock('../contexts/FeaturesContext', () => ({
  useFeatures: () => ({ features: mockFeatures }),
}));
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: mockConfig }),
}));

let mockBuildMonitorUpdateCount = 0;
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ buildMonitorUpdateCount: mockBuildMonitorUpdateCount }),
}));

const mockUnits = [
  {
    slug: 'repo-a',
    repo_name: 'Repo A',
    repo: 'owner/repo-a',
    branch: 'main',
    workflows: [
      {
        name: 'CI',
        path: '.github/workflows/ci.yml',
        run_id: 1,
        run_number: 10,
        status: 'completed',
        conclusion: 'success',
        html_url: 'https://github.com/owner/repo-a/actions/runs/1',
        failed_jobs: [],
      },
    ],
    checked_at: '2026-06-08T12:00:00Z',
    last_error: '',
    configured: true,
    github_login: 'octocat',
  },
  {
    slug: 'repo-b',
    repo_name: 'Repo B',
    repo: 'owner/repo-b',
    branch: 'main',
    workflows: [
      {
        name: 'Tests',
        path: '.github/workflows/tests.yml',
        run_id: 2,
        run_number: 5,
        status: 'completed',
        conclusion: 'failure',
        html_url: 'https://github.com/owner/repo-b/actions/runs/2',
        head_sha: 'abc1234def5678',
        session_id: '',
        launch_error: '',
        failed_jobs: [
          { name: 'test', html_url: 'https://github.com/owner/repo-b/actions/runs/2/jobs/10' },
          { name: 'build', html_url: 'https://github.com/owner/repo-b/actions/runs/2/jobs/11' },
        ],
      },
      {
        name: 'Release',
        path: '.github/workflows/release.yml',
        failed_jobs: [],
      },
    ],
    checked_at: '2026-06-08T12:00:00Z',
    last_error: '',
    configured: true,
    github_login: 'octocat',
    remediation_workspace_id: '',
  },
];

beforeEach(() => {
  vi.restoreAllMocks();
  mockBuildMonitorUpdateCount = 0;
  mockNavigate.mockReset();
});

function mockFetch(
  getResponse: { enabled: boolean; units: any[]; launch_configured?: boolean },
  postResponse?: { enabled: boolean; units: any[] },
  launchResponse?: { workspace_id: string; session_id: string }
) {
  vi.spyOn(globalThis, 'fetch').mockImplementation((url: string | URL | Request) => {
    const u = url.toString();
    if (u === '/api/build-monitor') {
      return Promise.resolve(Response.json(getResponse));
    }
    if (u === '/api/build-monitor/check') {
      return Promise.resolve(Response.json(postResponse || getResponse));
    }
    if (u.includes('/launch-workspace')) {
      return Promise.resolve(
        Response.json(launchResponse || { workspace_id: 'ws-9', session_id: 'sess-9' })
      );
    }
    return Promise.reject(new Error('unknown'));
  });
}

describe('BuildMonitorPage', () => {
  it('renders one status row per workflow', async () => {
    mockFetch({ enabled: true, units: mockUnits, launch_configured: true });
    renderPage();
    expect(await screen.findByText('Passing')).toBeInTheDocument();
    expect(screen.getByText('Failing')).toBeInTheDocument();
    expect(screen.getByText('No runs yet')).toBeInTheDocument();
    expect(screen.getByText('CI')).toBeInTheDocument();
    expect(screen.getByText('Tests')).toBeInTheDocument();
    expect(screen.getByText('Release')).toBeInTheDocument();
    expect(screen.getByText('Run #10')).toBeInTheDocument();
    // Failed jobs shown
    expect(screen.getByText('test')).toBeInTheDocument();
    expect(screen.getByText('build')).toBeInTheDocument();
  });

  it('clicking Check now posts and re-renders', async () => {
    const postUnits = [
      {
        ...mockUnits[0],
        workflows: [{ ...mockUnits[0].workflows[0], conclusion: 'failure', run_number: 11 }],
      },
    ];
    mockFetch(
      { enabled: true, units: mockUnits, launch_configured: true },
      { enabled: true, units: postUnits }
    );
    const user = userEvent.setup();
    renderPage();
    expect(await screen.findByText('Passing')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /check now/i }));
    await waitFor(() => {
      expect(screen.queryByText('Passing')).not.toBeInTheDocument();
    });
    expect(screen.getByText('Failing')).toBeInTheDocument();
  });

  it('does not crash on units:null (Go nil slice marshals to null)', async () => {
    mockFetch({ enabled: true, units: null as any, launch_configured: true });
    renderPage();
    expect(await screen.findByText(/no repos enabled/i)).toBeInTheDocument();
  });

  it('does not crash when workflows and failed_jobs are absent (omitempty)', async () => {
    const unit: any = { ...mockUnits[0] };
    delete unit.workflows;
    mockFetch({ enabled: true, units: [unit], launch_configured: true });
    renderPage();
    expect(await screen.findByText('Repo A')).toBeInTheDocument();
    expect(screen.getByText(/no active workflows/i)).toBeInTheDocument();
  });

  it('shows not enabled message when disabled', async () => {
    mockFetch({ enabled: false, units: [] });
    renderPage();
    expect(await screen.findByText(/not enabled/i)).toBeInTheDocument();
  });

  it('shows error unit with re-authorize hint', async () => {
    const errorUnits = [{ ...mockUnits[0], workflows: [], last_error: 'unauthorized' }];
    mockFetch({ enabled: true, units: errorUnits, launch_configured: true });
    renderPage();
    expect(await screen.findByText(/re-authorize/i)).toBeInTheDocument();
  });

  it('flags repos that have no identity selected', async () => {
    const units = [{ ...mockUnits[0], configured: false, github_login: '' }];
    mockFetch({ enabled: true, units, launch_configured: true });
    renderPage();
    expect(await screen.findByText(/no identity selected/i)).toBeInTheDocument();
  });

  it('refetches when the build monitor update counter bumps', async () => {
    mockFetch({ enabled: true, units: mockUnits, launch_configured: true });
    const { rerender } = renderPage();
    expect(await screen.findByText('Passing')).toBeInTheDocument();

    const fetchSpy = globalThis.fetch as ReturnType<typeof vi.fn>;
    const callsBefore = fetchSpy.mock.calls.filter(
      (call: Array<string | URL | Request>) => call[0].toString() === '/api/build-monitor'
    ).length;

    mockBuildMonitorUpdateCount = 1;
    rerender(
      <MemoryRouter>
        <BuildMonitorPage />
      </MemoryRouter>
    );

    await waitFor(() => {
      const calls = fetchSpy.mock.calls.filter(
        (call: Array<string | URL | Request>) => call[0].toString() === '/api/build-monitor'
      ).length;
      expect(calls).toBe(callsBefore + 1);
    });
  });

  it('shows the short SHA on rows that have one', async () => {
    mockFetch({ enabled: true, units: mockUnits, launch_configured: true });
    renderPage();
    expect(await screen.findByText('abc1234d')).toBeInTheDocument();
  });

  it('links a failing row to its remediation session', async () => {
    const units = [
      {
        ...mockUnits[1],
        workflows: [{ ...mockUnits[1].workflows[0], session_id: 'sess-1' }],
      },
    ];
    mockFetch({ enabled: true, units, launch_configured: true });
    renderPage();
    const link = await screen.findByRole('link', { name: /fixing session/i });
    expect(link).toHaveAttribute('href', '/sessions/sess-1');
  });

  it('shows the launch error on a failing row', async () => {
    const units = [
      {
        ...mockUnits[1],
        workflows: [
          { ...mockUnits[1].workflows[0], launch_error: 'workspace creation failed: boom' },
        ],
      },
    ];
    mockFetch({ enabled: true, units, launch_configured: true });
    renderPage();
    expect(await screen.findByText(/workspace creation failed: boom/i)).toBeInTheDocument();
  });

  it('launch button posts and navigates to the new session', async () => {
    mockFetch({ enabled: true, units: mockUnits, launch_configured: true }, undefined, {
      workspace_id: 'ws-9',
      session_id: 'sess-9',
    });
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('button', { name: /launch workspace/i }));
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/sessions/sess-9');
    });
    const fetchSpy = globalThis.fetch as ReturnType<typeof vi.fn>;
    const launchCall = fetchSpy.mock.calls.find((c: Array<string | URL | Request>) =>
      c[0].toString().includes('/launch-workspace')
    );
    expect(launchCall![0].toString()).toBe(
      '/api/build-monitor/repos/repo-b/failures/2/launch-workspace'
    );
  });

  it('disables the launch button when launching is not configured', async () => {
    mockFetch({ enabled: true, units: mockUnits, launch_configured: false });
    renderPage();
    const btn = await screen.findByRole('button', { name: /launch workspace/i });
    expect(btn).toBeDisabled();
  });

  it('links the unit to its remediation workspace', async () => {
    const units = [{ ...mockUnits[1], remediation_workspace_id: 'ws-5' }];
    mockFetch({ enabled: true, units, launch_configured: true });
    renderPage();
    const link = await screen.findByRole('link', { name: /remediation workspace/i });
    expect(link).toHaveAttribute('href', '/git/ws-5');
  });
});
