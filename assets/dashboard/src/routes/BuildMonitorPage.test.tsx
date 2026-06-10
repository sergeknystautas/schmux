import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import userEvent from '@testing-library/user-event';
import BuildMonitorPage from './BuildMonitorPage';

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
  },
];

beforeEach(() => {
  vi.restoreAllMocks();
});

function mockFetch(
  getResponse: { enabled: boolean; units: any[] },
  postResponse?: { enabled: boolean; units: any[] }
) {
  vi.spyOn(globalThis, 'fetch').mockImplementation((url: string | URL | Request) => {
    const u = url.toString();
    if (u === '/api/build-monitor') {
      return Promise.resolve(Response.json(getResponse));
    }
    if (u === '/api/build-monitor/check') {
      return Promise.resolve(Response.json(postResponse || getResponse));
    }
    return Promise.reject(new Error('unknown'));
  });
}

describe('BuildMonitorPage', () => {
  it('renders one status row per workflow', async () => {
    mockFetch({ enabled: true, units: mockUnits });
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
    mockFetch({ enabled: true, units: mockUnits }, { enabled: true, units: postUnits });
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
    mockFetch({ enabled: true, units: null as any });
    renderPage();
    expect(await screen.findByText(/no repos enabled/i)).toBeInTheDocument();
  });

  it('does not crash when workflows and failed_jobs are absent (omitempty)', async () => {
    const unit: any = { ...mockUnits[0] };
    delete unit.workflows;
    mockFetch({ enabled: true, units: [unit] });
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
    mockFetch({ enabled: true, units: errorUnits });
    renderPage();
    expect(await screen.findByText(/re-authorize/i)).toBeInTheDocument();
  });

  it('flags repos that have no identity selected', async () => {
    const units = [{ ...mockUnits[0], configured: false, github_login: '' }];
    mockFetch({ enabled: true, units });
    renderPage();
    expect(await screen.findByText(/no identity selected/i)).toBeInTheDocument();
  });
});
