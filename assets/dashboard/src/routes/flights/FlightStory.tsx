import { useState } from 'react';
import type { FlightDetailResponse } from '../../lib/types.generated';
import { retryFlightMonitor } from '../../lib/api';
import { buildFlightStory, type StoryEntry } from '../../lib/flightStory';
import { formatTimestamp } from '../../lib/utils';
import layout from './FlightLayout.module.css';

interface Props {
  flight: Pick<FlightDetailResponse, 'id' | 'sessions' | 'operations' | 'monitor'>;
}

// FlightStory tells what happened to the flight, newest first, in roles and
// outcomes. Each entry is one sentence; the raw identifiers, classifier
// answers, and full texts the daemon retained sit behind its Details button.
// Failed items keep their Retry button.
export default function FlightStory({ flight }: Props) {
  const entries = buildFlightStory(flight);
  const [retrying, setRetrying] = useState<string | null>(null);
  const [error, setError] = useState('');

  const handleRetry = async (itemID: string) => {
    setRetrying(itemID);
    setError('');
    try {
      await retryFlightMonitor(flight.id, itemID);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'retry failed');
    } finally {
      setRetrying(null);
    }
  };

  const monitorError = flight.monitor?.error;
  if (entries.length === 0 && !monitorError) {
    return (
      <div className="card__body">
        <p className="m-0 text-muted">Nothing has happened yet.</p>
      </div>
    );
  }

  return (
    <div className="card__body flex-col gap-sm" data-testid="flight-story">
      {monitorError && (
        <p className="form-group__error m-0" role="alert" data-testid="flight-monitor-error">
          {monitorError}
        </p>
      )}
      {error && (
        <p className="form-group__error m-0" role="alert">
          {error}
        </p>
      )}
      <ol className={layout.story}>
        {entries.map((entry) => (
          <StoryRow
            key={entry.id}
            entry={entry}
            onRetry={() => handleRetry(entry.retryId!)}
            retrying={retrying === entry.retryId}
          />
        ))}
      </ol>
    </div>
  );
}

function StoryRow({
  entry,
  onRetry,
  retrying,
}: {
  entry: StoryEntry;
  onRetry: () => void;
  retrying: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <li
      className={layout.storyRow}
      data-testid="flight-story-entry"
      data-entry-id={entry.id}
      data-tone={entry.tone}
    >
      <div className={layout.storyLine}>
        <span className={`${layout.storyTime} text-faint`}>{formatTimestamp(entry.at)}</span>
        <span
          className={`${layout.storyHeadline} ${entry.tone === 'error' ? 'text-error' : ''}`}
          data-testid="flight-story-headline"
        >
          {entry.headline}
        </span>
        {entry.tag && (
          <span
            className={`status-pill status-pill--sm ${entry.tag.tone === 'error' ? 'status-pill--error' : 'status-pill--running'}`}
            data-testid="flight-story-tag"
          >
            <span className="status-pill__dot" />
            {entry.tag.text}
          </span>
        )}
        <span className={layout.storyActions}>
          {entry.retryId && (
            <button
              type="button"
              className="btn btn--ghost btn--bordered btn--sm"
              onClick={onRetry}
              disabled={retrying}
              data-testid="flight-story-retry"
            >
              {retrying ? 'Retrying…' : 'Retry'}
            </button>
          )}
          <button
            type="button"
            className="btn btn--ghost btn--bordered btn--sm"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            data-testid="flight-story-toggle"
          >
            {open ? 'Hide details' : 'Details'}
          </button>
        </span>
      </div>
      {open && (
        <div className={`${layout.storyDetail} flex-col gap-sm`} data-testid="flight-story-detail">
          <dl className={layout.metadata}>
            {entry.fields.map((field, i) => (
              <div className={layout.metadataRow} key={`${field.label}-${i}`}>
                <dt className="font-normal">{field.label}</dt>
                <dd className={`m-0 flex-1 text-pre-wrap ${field.mono ? 'mono' : ''}`}>
                  {field.value}
                </dd>
              </div>
            ))}
            {(entry.answers ?? []).map((answer) => (
              <div className={layout.metadataRow} key={answer.id}>
                <dt className="font-normal">{answer.id}</dt>
                <dd className="m-0 flex-1">
                  {answer.choice}
                  {answer.confidence !== undefined && answer.confidence !== null
                    ? ` (confidence ${answer.confidence.toFixed(2)})`
                    : ''}
                </dd>
              </div>
            ))}
          </dl>
          {entry.text && (
            <p className={layout.prompt} data-testid="flight-story-text">
              {entry.text}
            </p>
          )}
        </div>
      )}
    </li>
  );
}
