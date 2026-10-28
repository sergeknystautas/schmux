import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { getErrorMessage, getFlight, updateFlight } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import { flightActivity, flightLabel, pilotGuidanceText, reviewReading } from '../lib/flights';
import { formatTimestamp } from '../lib/utils';
import type { FlightDetailResponse, FlightTransitionResponse } from '../lib/types.generated';
import FlightActions from './flights/FlightActions';
import FlightEditForm from './flights/FlightEditForm';
import type { FlightFormValues } from './flights/FlightForm';
import useFlightFormOptions from './flights/useFlightFormOptions';
import FlightAssignmentLinks from './flights/FlightAssignmentLinks';
import FlightStory from './flights/FlightStory';
import FlightActivityLine from './flights/FlightActivityLine';
import useFlightInProgress from './flights/useFlightInProgress';
import { FlightStatePills } from './flights/FlightList';
import CopyButton from '../components/CopyButton';
import layout from './flights/FlightLayout.module.css';

// The phases in workflow order. The progress bar fills the ones behind the
// flight and marks the current one; it is a picture of where the flight is,
// not a control.
const PHASES: Array<{ state: FlightDetailResponse['workflow_state']; label: string }> = [
  { state: 'backlog', label: 'Backlog' },
  { state: 'planning', label: 'Planning' },
  { state: 'executing', label: 'Executing' },
  { state: 'reviewing', label: 'Reviewing' },
  { state: 'completing', label: 'Completing' },
];

function PhaseProgress({ state }: { state: FlightDetailResponse['workflow_state'] }) {
  const current = PHASES.findIndex((p) => p.state === state);
  return (
    <ol className={layout.progress} aria-label="Flight progress" data-testid="flight-phase-strip">
      {PHASES.map((phase, i) => (
        <li
          key={phase.state}
          className={layout.progressStep}
          data-state={i === current ? 'current' : i < current ? 'done' : 'next'}
          aria-current={i === current ? 'step' : undefined}
          data-testid={`flight-phase-${phase.state}`}
        >
          <span className={layout.progressDot} aria-hidden="true" />
          {phase.label}
        </li>
      ))}
    </ol>
  );
}

// Prompts run to pages. The card is collapsed to the prompt's first lines; its
// header is the disclosure, the same chevron toggle the sidebar panes use, so
// the page is about how the work is going, not what was asked.
const PROMPT_PREVIEW_CHARS = 240;

function FlightPrompt({ prompt }: { prompt: string }) {
  const [open, setOpen] = useState(false);
  const long = prompt.length > PROMPT_PREVIEW_CHARS || prompt.split('\n').length > 3;
  const title = <h2 className="settings-section__title m-0">The work</h2>;
  return (
    <section className="card">
      {long ? (
        <button
          type="button"
          className={`card__header diag-pane__toggle ${layout.promptToggle}`}
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          data-testid="flight-detail-prompt-toggle"
        >
          <span
            className={`diag-pane__chevron${open ? ' diag-pane__chevron--open' : ''}`}
            aria-hidden="true"
          >
            ▶
          </span>
          {title}
        </button>
      ) : (
        <div className="card__header">{title}</div>
      )}
      <div className="card__body hover-copy">
        <CopyButton text={prompt} label="prompt" className="icon-btn hover-copy__btn" />
        <p
          className={`${layout.prompt} ${open || !long ? '' : layout.promptClamped}`}
          data-testid="flight-detail-prompt"
        >
          {prompt}
        </p>
      </div>
    </section>
  );
}

export default function FlightDetailPage() {
  const { flightId = '' } = useParams<{ flightId: string }>();
  const navigate = useNavigate();
  const { flightsById, sessionsById } = useSessions();
  const isInProgress = useFlightInProgress();
  const [flight, setFlight] = useState<FlightDetailResponse | null>(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(false);
  const { repos, targets } = useFlightFormOptions();

  // Snapshots can arrive in bursts, so loads overlap; only the newest request
  // (or a newer transition response) may set the page's state.
  const loadSeq = useRef(0);
  const load = useCallback(async () => {
    const seq = ++loadSeq.current;
    try {
      const next = await getFlight(flightId);
      if (seq !== loadSeq.current) return;
      setFlight(next);
      setError('');
    } catch (err) {
      if (seq !== loadSeq.current) return;
      setFlight(null);
      setError(getErrorMessage(err, 'Failed to load flight'));
    }
  }, [flightId]);
  const applyResponse = (next: FlightDetailResponse) => {
    loadSeq.current++;
    setFlight(next);
  };

  useEffect(() => {
    setFlight(null);
    setError('');
    load();
  }, [load]);

  // The dashboard snapshot carries the flight's summary. Refetch the detail
  // only when that summary changes (including after a reconnect), never on a
  // timer.
  const summary = flightsById[flightId];
  const stamp = summary ? JSON.stringify(summary) : 'absent';
  const lastStamp = useRef(stamp);
  useEffect(() => {
    if (lastStamp.current === stamp) return;
    lastStamp.current = stamp;
    load();
  }, [stamp, load]);

  const handleSave = async (req: FlightFormValues) => {
    applyResponse(await updateFlight(flightId, req));
    setEditing(false);
  };

  const handleTransition = (res: FlightTransitionResponse) => {
    if (res.removed) {
      navigate('/flights');
      return;
    }
    applyResponse(res.flight);
  };

  // The title row reads as a breadcrumb: "Flights" returns to the list.
  const crumb = (label: string) => (
    <h1 className="app-header__meta">
      <span>
        <Link to="/flights" className="text-accent">
          Flights
        </Link>
        : <span data-testid="flight-detail-title">{label}</span>
      </span>
    </h1>
  );

  if (!flight) {
    return (
      <>
        <div className={`app-header ${layout.header}`}>
          <div className="app-header__info">{crumb(flightId)}</div>
        </div>
        {error ? (
          <div className="empty-state" data-testid="flight-detail-error">
            <div className="empty-state__icon">!</div>
            <h3 className="empty-state__title">Flight unavailable</h3>
            <p className="empty-state__description">{error}</p>
          </div>
        ) : (
          <div className="loading-state">
            <div className="spinner"></div>
          </div>
        )}
      </>
    );
  }

  const sessions = flight.sessions ?? [];
  const activity = flightActivity(flight, sessionsById, isInProgress);
  const reading = reviewReading(flight.review_status);
  const guidance = pilotGuidanceText(flight.pilot_guidance);
  const metadata = (label: string, value: string | undefined, mono = false, testId?: string) =>
    value ? (
      <div className={layout.metadataRow}>
        <dt className="font-normal">{label}</dt>
        <dd className={`m-0 flex-1 text-pre-wrap ${mono ? 'mono' : ''}`} data-testid={testId}>
          {value}
        </dd>
      </div>
    ) : null;

  return (
    <>
      <div className={`app-header ${layout.header}`}>
        <div className="app-header__info">
          {crumb(flightLabel(flight))}
          <FlightStatePills flight={flight} />
          {flight.workflow_state !== 'backlog' && (
            <FlightActivityLine flight={flight} activity={activity} linkTo="session" />
          )}
        </div>
        <div className="app-header__actions">
          {!editing && (
            <button
              type="button"
              className="btn btn--ghost btn--bordered"
              onClick={() => setEditing(true)}
              data-testid="flight-detail-edit"
            >
              Edit
            </button>
          )}
          <FlightActions flight={flight} onTransition={handleTransition} />
        </div>
      </div>

      {editing ? (
        <div data-testid="flight-edit">
          <FlightEditForm
            repos={repos}
            targets={targets}
            flight={flight}
            onSubmit={handleSave}
            onCancel={() => setEditing(false)}
          />
        </div>
      ) : (
        <div className="flex-col gap-lg">
          {/* The work: the user's own words, first, folded to a few lines. */}
          <FlightPrompt prompt={flight.prompt} />

          {/* How it's going: where the work is, what the latest reading says, who is on it. */}
          <section className="card" data-testid="flight-progress">
            <PhaseProgress state={flight.workflow_state} />
            <div className="card__body flex-col gap-sm">
              {flight.name_error && (
                <p
                  className="form-group__error m-0"
                  role="alert"
                  data-testid="flight-detail-name-error"
                >
                  Name not generated: {flight.name_error}
                </p>
              )}
              {flight.last_error && (
                <p
                  className="form-group__error m-0"
                  role="alert"
                  data-testid="flight-detail-last-error"
                >
                  {flight.last_error}
                </p>
              )}
              {flight.pilot_stop && (
                <div className="flex-row gap-sm flex-wrap" data-testid="flight-detail-pilot-stop">
                  <span className="status-pill status-pill--sm status-pill--error">
                    <span className="status-pill__dot" />
                    Pilot stopped
                  </span>
                  <span className="m-0">{flight.pilot_stop.reason}</span>
                </div>
              )}
              {reading && (
                <p className="m-0" data-testid="flight-detail-review-reading">
                  {reading}
                </p>
              )}
              {guidance && (
                <p className="m-0" data-testid="flight-detail-pilot-guidance">
                  {guidance}
                </p>
              )}
              {flight.workflow_state === 'backlog' && !flight.last_error && (
                <p className="m-0 text-muted">
                  Waiting in the backlog; take off to start planning.
                </p>
              )}
            </div>
            {flight.workspace && (
              <FlightAssignmentLinks workspace={flight.workspace} sessions={sessions} />
            )}
          </section>

          {/* The story: what happened, newest first. */}
          <section className="card">
            <div className="card__header">
              <h2 className="settings-section__title">Story</h2>
            </div>
            <FlightStory flight={flight} />
          </section>

          {/* Details: identifiers and paths, for when they are needed. */}
          <section className="card">
            <div className="card__header">
              <h2 className="settings-section__title">Details</h2>
            </div>
            <div className="card__body">
              <dl className={layout.metadata} data-testid="flight-detail-metadata">
                {metadata('ID', flight.id, true)}
                {metadata('Repository', flight.repo, true)}
                {metadata('Branch', flight.workspace?.branch, true)}
                {metadata('Workspace path', flight.workspace?.path, true)}
                {metadata('Planner', flight.planner_model)}
                {metadata('Executor', flight.executor_model)}
                {metadata('Spec', flight.spec_path, true, 'flight-spec-path')}
                {metadata('Plan', flight.plan_path, true, 'flight-plan-path')}
                {flight.report_target && (
                  <div className={layout.metadataRow}>
                    <dt className="font-normal">Report</dt>
                    <dd className="mono m-0 flex-1 text-pre-wrap" data-testid="flight-report-path">
                      {flight.report_target}
                      {!flight.report_path && ' (not yet verified)'}
                    </dd>
                  </div>
                )}
                {metadata('Created', formatTimestamp(flight.created_at))}
              </dl>
            </div>
          </section>
        </div>
      )}
    </>
  );
}
