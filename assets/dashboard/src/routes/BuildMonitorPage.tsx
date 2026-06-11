import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { useFeatures } from '../contexts/FeaturesContext';
import { useSessions } from '../contexts/SessionsContext';

interface FailedJob {
  name: string;
  html_url: string;
}

interface BuildMonitorWorkflow {
  name: string;
  path: string;
  run_id?: number;
  run_number?: number;
  status?: string;
  conclusion?: string;
  html_url?: string;
  failed_jobs: FailedJob[];
}

interface BuildMonitorUnit {
  slug: string;
  repo_name: string;
  repo: string;
  branch?: string;
  workflows: BuildMonitorWorkflow[];
  checked_at?: string;
  last_error?: string;
  configured: boolean;
  github_login?: string;
}

interface BuildMonitorResponse {
  enabled: boolean;
  units: BuildMonitorUnit[];
}

// Defend against null/absent arrays in the wire shape (Go nil slices marshal
// to null; workflows and failed_jobs are omitempty).
function normalize(d: any): BuildMonitorResponse {
  return {
    enabled: !!d?.enabled,
    units: (d?.units || []).map((u: any) => ({
      ...u,
      workflows: (u.workflows || []).map((w: any) => ({
        ...w,
        failed_jobs: w.failed_jobs || [],
      })),
    })),
  };
}

function workflowBadge(wf: BuildMonitorWorkflow): { text: string; className: string } {
  if (wf.conclusion === 'success') return { text: 'Passing', className: 'badge badge--success' };
  if (wf.conclusion === 'failure') return { text: 'Failing', className: 'badge badge--danger' };
  if (wf.status === 'in_progress' || wf.status === 'queued')
    return { text: 'Running', className: 'badge badge--info' };
  return { text: 'No runs yet', className: 'badge badge--neutral' };
}

export default function BuildMonitorPage() {
  const { features } = useFeatures();
  const { buildMonitorUpdateCount } = useSessions();
  const [data, setData] = useState<BuildMonitorResponse>({ enabled: false, units: [] });
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState('');

  const fetchData = useCallback(() => {
    fetch('/api/build-monitor')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((d: BuildMonitorResponse) => setData(normalize(d)))
      .catch((e) => setError(e.message));
  }, []);

  // Initial fetch + live refetch when the daemon broadcasts build_monitor_updated.
  useEffect(() => {
    fetchData();
  }, [fetchData, buildMonitorUpdateCount]);

  const handleCheckNow = () => {
    setChecking(true);
    setError('');
    fetch('/api/build-monitor/check', { method: 'POST' })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((d: BuildMonitorResponse) => {
        setData(normalize(d));
        setChecking(false);
      })
      .catch((e) => {
        setError(e.message);
        setChecking(false);
      });
  };

  if (!features.build_monitor) {
    return (
      <div className="page-content">
        <div className="page-header">
          <h1>Build Monitor</h1>
        </div>
        <p className="text-muted">Build Monitor is not available in this build.</p>
      </div>
    );
  }

  if (!data.enabled) {
    return (
      <div className="page-content">
        <div className="page-header">
          <h1>Build Monitor</h1>
        </div>
        <p className="text-muted">
          Build Monitor is not enabled. Go to{' '}
          <Link to="/config?tab=experimental">Settings → Experimental</Link> to enable it.
        </p>
      </div>
    );
  }

  return (
    <div className="page-content">
      <div className="page-header">
        <h1>Build Monitor</h1>
        <button
          className="btn btn--primary"
          onClick={handleCheckNow}
          disabled={checking || data.units.length === 0}
        >
          {checking ? 'Checking…' : 'Check now'}
        </button>
      </div>

      {error && <p className="form-group__error mb-md">Check failed: {error}</p>}

      {data.units.length === 0 ? (
        <p className="text-muted">
          No repos enabled. Go to <Link to="/config?tab=experimental">Settings → Experimental</Link>{' '}
          to choose repos to monitor.
        </p>
      ) : (
        <div className="item-list">
          {data.units.map((unit) => (
            <div className="item-list__item" key={unit.slug}>
              <div className="item-list__item-primary">
                <div className="flex-row gap-md">
                  <span className="item-list__item-name">{unit.repo_name}</span>
                  <span className="item-list__item-detail">
                    {unit.repo}
                    {unit.branch ? ` · ${unit.branch}` : ''}
                  </span>
                </div>
                {!unit.configured && (
                  <div className="item-list__item-detail text-warning">
                    No identity selected — finish setup in{' '}
                    <Link to="/config?tab=experimental">Settings → Experimental</Link>.
                  </div>
                )}
                {unit.last_error && (
                  <div className="item-list__item-detail text-error">
                    {unit.last_error}
                    {unit.last_error.includes('unauthorized') && (
                      <>
                        {' '}
                        — <Link to="/config?tab=experimental">re-authorize</Link>
                      </>
                    )}
                  </div>
                )}
                {unit.workflows.map((wf) => {
                  const badge = workflowBadge(wf);
                  return (
                    <div className="flex-row gap-md" key={wf.path || wf.name}>
                      <span className={badge.className}>{badge.text}</span>
                      <span>{wf.name}</span>
                      {wf.html_url && (
                        <a href={wf.html_url} target="_blank" rel="noopener noreferrer">
                          Run #{wf.run_number}
                        </a>
                      )}
                      {wf.failed_jobs.length > 0 && (
                        <span className="item-list__item-detail">
                          Failed jobs:{' '}
                          {wf.failed_jobs.map((j, i) => (
                            <span key={j.name}>
                              {i > 0 && ', '}
                              <a href={j.html_url} target="_blank" rel="noopener noreferrer">
                                {j.name}
                              </a>
                            </span>
                          ))}
                        </span>
                      )}
                    </div>
                  );
                })}
                {unit.checked_at && unit.workflows.length === 0 && !unit.last_error && (
                  <div className="item-list__item-detail">No active workflows on this branch.</div>
                )}
                {!unit.checked_at && !unit.last_error && (
                  <div className="item-list__item-detail">Not checked yet.</div>
                )}
                {unit.checked_at && (
                  <div className="item-list__item-detail text-faint">
                    Checked {new Date(unit.checked_at).toLocaleString()}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
