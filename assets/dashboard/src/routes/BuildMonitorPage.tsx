import { useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { useFeatures } from '../contexts/FeaturesContext';
import { useSessions } from '../contexts/SessionsContext';
import { useBuildMonitor } from '../contexts/BuildMonitorContext';
import { useModal } from '../components/ModalProvider';
import { getErrorMessage, launchBuildMonitorWorkspace } from '../lib/api';
import type { BuildMonitorWorkflow } from '../lib/types.generated';

type Badge = { text: string; className: string };

function neutralBadge(text: string): Badge {
  return { text, className: 'badge badge--neutral' };
}

// Completed runs are labelled by conclusion, unfinished runs by status, both
// GitHub's values (docs/api.md). Unknown values render verbatim instead of
// borrowing a known label.
const CONCLUSION_BADGES = new Map<string, Badge>([
  ['success', { text: 'Passing', className: 'badge badge--success' }],
  ['failure', { text: 'Failing', className: 'badge badge--danger' }],
  ['timed_out', { text: 'Timed out', className: 'badge badge--danger' }],
  ['startup_failure', { text: 'Startup failure', className: 'badge badge--danger' }],
  ['action_required', { text: 'Action required', className: 'badge badge--warning' }],
  ['cancelled', neutralBadge('Cancelled')],
  ['skipped', neutralBadge('Skipped')],
  ['neutral', neutralBadge('Neutral')],
  ['stale', neutralBadge('Stale')],
]);

const STATUS_BADGES = new Map<string, Badge>([
  ['in_progress', { text: 'Running', className: 'badge badge--info' }],
  ['queued', neutralBadge('Queued')],
  ['waiting', neutralBadge('Waiting')],
  ['pending', neutralBadge('Pending')],
  ['requested', neutralBadge('Pending')],
]);

function humanize(value: string): string {
  const spaced = value.replace(/_/g, ' ');
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

function workflowBadge(wf: BuildMonitorWorkflow): Badge {
  if (!wf.run_id) return neutralBadge('No runs');
  if (wf.status === 'completed') {
    const conclusion = wf.conclusion ?? '';
    return CONCLUSION_BADGES.get(conclusion) ?? neutralBadge(humanize(conclusion || 'completed'));
  }
  const status = wf.status ?? '';
  return STATUS_BADGES.get(status) ?? neutralBadge(humanize(status || 'unknown'));
}

export default function BuildMonitorPage() {
  const { features } = useFeatures();
  const { sessionsById } = useSessions();
  const { data, error, checking, checkNow } = useBuildMonitor();
  const navigate = useNavigate();
  const { alert } = useModal();
  const [launching, setLaunching] = useState<number | null>(null); // run_id being launched

  const handleLaunch = async (slug: string, runId: number) => {
    setLaunching(runId);
    try {
      const d = await launchBuildMonitorWorkspace(slug, runId);
      navigate(`/sessions/${d.session_id}`);
    } catch (err) {
      alert('Launch Failed', getErrorMessage(err, 'Failed to launch workspace'));
    } finally {
      setLaunching(null);
    }
  };

  const handleCheckNow = async () => {
    try {
      await checkNow();
    } catch (err) {
      alert('Check Failed', getErrorMessage(err, 'Build monitor check failed'));
    }
  };

  if (!features.build_monitor) {
    return (
      <div className="page-content">
        <div className="app-header">
          <div className="app-header__info">
            <h1 className="app-header__meta">Build Monitor</h1>
          </div>
        </div>
        <p className="text-muted">Build Monitor is not available in this build.</p>
      </div>
    );
  }

  if (!data.enabled) {
    return (
      <div className="page-content">
        <div className="app-header">
          <div className="app-header__info">
            <h1 className="app-header__meta">Build Monitor</h1>
          </div>
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
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Build Monitor</h1>
        </div>
        <div className="app-header__actions">
          <button
            className="btn btn--primary"
            onClick={handleCheckNow}
            disabled={checking || data.units.length === 0}
          >
            {checking ? 'Checking…' : 'Check now'}
          </button>
        </div>
      </div>

      {error && <p className="form-group__error mb-md">Failed to load build monitor: {error}</p>}

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
                {unit.remediation_workspace_id && (
                  <div className="item-list__item-detail">
                    <Link to={`/git/${unit.remediation_workspace_id}`}>Remediation workspace</Link>
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
                {unit.workflows?.map((wf) => {
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
                      {wf.head_sha && (
                        <span className="item-list__item-detail" title={wf.head_sha}>
                          {wf.head_sha.slice(0, 8)}
                        </span>
                      )}
                      {wf.conclusion === 'failure' &&
                        wf.session_id &&
                        sessionsById[wf.session_id] && (
                          <Link to={`/sessions/${wf.session_id}`}>
                            fixing in {sessionsById[wf.session_id].workspace_id}
                          </Link>
                        )}
                      {wf.conclusion === 'failure' &&
                        (!wf.session_id || !sessionsById[wf.session_id]) && (
                          <button
                            className="btn btn--secondary btn--sm"
                            disabled={!data.launch_configured || launching === wf.run_id}
                            title={
                              data.launch_configured
                                ? 'Launch a fresh workspace + agent session for this failure'
                                : 'Configure a remediation target in Settings → Experimental first'
                            }
                            onClick={() => wf.run_id && handleLaunch(unit.slug, wf.run_id)}
                          >
                            {launching === wf.run_id ? 'Launching…' : 'Launch workspace'}
                          </button>
                        )}
                      {wf.launch_error && (
                        <span className="item-list__item-detail text-error">{wf.launch_error}</span>
                      )}
                      {wf.failed_jobs && wf.failed_jobs.length > 0 && (
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
                {unit.checked_at && unit.workflows?.length === 0 && !unit.last_error && (
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
