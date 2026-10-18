import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { getErrorMessage, getFlight, updateFlight } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import { flightLabel } from '../lib/flights';
import { formatTimestamp } from '../lib/utils';
import type { FlightDetailResponse, FlightTransitionResponse } from '../lib/types.generated';
import FlightActions from './flights/FlightActions';
import FlightEditForm from './flights/FlightEditForm';
import type { FlightFormValues } from './flights/FlightForm';
import useFlightFormOptions from './flights/useFlightFormOptions';
import FlightAssignmentLinks from './flights/FlightAssignmentLinks';
import FlightOperationHistory from './flights/FlightOperationHistory';
import { FlightStatePills } from './flights/FlightList';
import CopyButton from '../components/CopyButton';
import layout from './flights/FlightLayout.module.css';

export default function FlightDetailPage() {
  const { flightId = '' } = useParams<{ flightId: string }>();
  const navigate = useNavigate();
  const { flightsById } = useSessions();
  const [flight, setFlight] = useState<FlightDetailResponse | null>(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(false);
  const { repos, targets } = useFlightFormOptions();

  const load = useCallback(async () => {
    try {
      setFlight(await getFlight(flightId));
      setError('');
    } catch (err) {
      setFlight(null);
      setError(getErrorMessage(err, 'Failed to load flight'));
    }
  }, [flightId]);

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
    setFlight(await updateFlight(flightId, req));
    setEditing(false);
  };

  const handleTransition = (res: FlightTransitionResponse) => {
    if (res.removed) {
      navigate('/flights');
      return;
    }
    setFlight(res.flight);
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

  return (
    <>
      <div className={`app-header ${layout.header}`}>
        <div className="app-header__info">
          {crumb(flightLabel(flight))}
          <FlightStatePills flight={flight} />
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

          <section className="card">
            <div className="card__header">
              <h2 className="settings-section__title">Flight</h2>
            </div>
            <div className="card__body">
              <dl className={layout.metadata}>
                <div className={layout.metadataRow}>
                  <dt className="font-normal">ID</dt>
                  <dd className="mono m-0 flex-1 text-pre-wrap">{flight.id}</dd>
                </div>
                <div className={layout.metadataRow}>
                  <dt className="font-normal">Repository</dt>
                  <dd className="mono m-0 flex-1 text-pre-wrap">{flight.repo}</dd>
                </div>
                <div className={layout.metadataRow}>
                  <dt className="font-normal">Planner</dt>
                  <dd className="m-0 flex-1 text-pre-wrap">{flight.planner_model}</dd>
                </div>
                <div className={layout.metadataRow}>
                  <dt className="font-normal">Executor</dt>
                  <dd className="m-0 flex-1 text-pre-wrap">{flight.executor_model}</dd>
                </div>
                <div className={layout.metadataRow}>
                  <dt className="font-normal">Created</dt>
                  <dd className="m-0 flex-1">{formatTimestamp(flight.created_at)}</dd>
                </div>
                {flight.done_at && (
                  <div className={layout.metadataRow}>
                    <dt className="font-normal">Done</dt>
                    <dd className="m-0 flex-1">{formatTimestamp(flight.done_at)}</dd>
                  </div>
                )}
                {flight.cancelled_at && (
                  <div className={layout.metadataRow}>
                    <dt className="font-normal">Cancelled</dt>
                    <dd className="m-0 flex-1 text-pre-wrap">
                      {formatTimestamp(flight.cancelled_at)}
                      {flight.cancel_reason && (
                        <span data-testid="flight-cancel-reason"> — {flight.cancel_reason}</span>
                      )}
                    </dd>
                  </div>
                )}
              </dl>
            </div>
          </section>

          <section className="card">
            <div className="card__header">
              <h2 className="settings-section__title">Resources</h2>
            </div>
            <FlightAssignmentLinks workspace={flight.workspace} sessions={sessions} />
            {flight.artifact_path && (
              <div className="card__body">
                <dl className={`${layout.metadata} mt-md`}>
                  <div className={layout.metadataRow}>
                    <dt className="font-normal">Planning artifact</dt>
                    <dd
                      className="mono m-0 flex-1 text-pre-wrap"
                      data-testid="flight-artifact-path"
                    >
                      {flight.artifact_path}
                    </dd>
                  </div>
                </dl>
              </div>
            )}
          </section>

          <section className="card">
            <div className="card__header">
              <h2 className="settings-section__title">Operation history</h2>
            </div>
            <FlightOperationHistory operations={flight.operations ?? []} />
          </section>

          <section className="card">
            <div className="card__header">
              <h2 className="settings-section__title">Prompt</h2>
            </div>
            <div className="card__body hover-copy">
              <CopyButton
                text={flight.prompt}
                label="prompt"
                className="icon-btn hover-copy__btn"
              />
              <pre className="mono text-pre-wrap m-0" data-testid="flight-detail-prompt">
                {flight.prompt}
              </pre>
            </div>
          </section>
        </div>
      )}
    </>
  );
}
