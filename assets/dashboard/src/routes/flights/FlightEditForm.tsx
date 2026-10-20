import { useState } from 'react';
import type { FlightDetailResponse } from '../../lib/types.generated';
import type { FlightFormTarget, FlightFormValues } from './FlightForm';

type FlightWorkflowState = FlightDetailResponse['workflow_state'];

const STAGES: FlightWorkflowState[] = ['backlog', 'planning', 'executing', 'completing', 'done'];

type FlightEditFormProps = {
  repos: { url: string; name: string }[];
  targets: FlightFormTarget[];
  flight: Pick<
    FlightDetailResponse,
    'repo' | 'prompt' | 'planner_model' | 'executor_model' | 'workflow_state'
  >;
  onSubmit: (values: FlightFormValues) => Promise<void>;
  onCancel: () => void;
};

/**
 * Edits a flight's recorded fields. Every field is a plain input in the app's
 * standard form layout; saving writes the fields and nothing else.
 */
export default function FlightEditForm({
  repos,
  targets,
  flight,
  onSubmit,
  onCancel,
}: FlightEditFormProps) {
  const [stage, setStage] = useState<FlightWorkflowState>(flight.workflow_state);
  const [repo, setRepo] = useState(flight.repo);
  const [planner, setPlanner] = useState(flight.planner_model);
  const [executor, setExecutor] = useState(flight.executor_model);
  const [prompt, setPrompt] = useState(flight.prompt);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  // The recorded value stays selectable even when config no longer lists it,
  // so an edit never silently changes a field the user did not touch.
  const repoOptions = repos.some((r) => r.url === repo)
    ? repos
    : [{ url: repo, name: repo }, ...repos];
  const targetOptions = (value: string) =>
    targets.some((t) => t.id === value) ? targets : [{ id: value, label: value }, ...targets];

  const missing = !repo || !prompt.trim() || !planner || !executor;

  const submit = async () => {
    if (missing || submitting) return;
    setSubmitting(true);
    setError('');
    try {
      await onSubmit({
        repo,
        prompt,
        planner_model: planner,
        executor_model: executor,
        workflow_state: stage,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form
      className="entity-form"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      data-testid="flight-form"
    >
      <div className="form-row">
        <div className="form-group">
          <label className="form-group__label" htmlFor="flight-stage">
            Stage
          </label>
          <select
            id="flight-stage"
            className="select"
            value={stage}
            disabled={submitting}
            onChange={(e) => setStage(e.target.value as FlightWorkflowState)}
          >
            {STAGES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="form-row">
        <div className="form-group">
          <label className="form-group__label" htmlFor="flight-repo">
            Repository
          </label>
          <select
            id="flight-repo"
            className="select"
            value={repo}
            disabled={submitting}
            onChange={(e) => setRepo(e.target.value)}
          >
            {repoOptions.map((r) => (
              <option key={r.url} value={r.url}>
                {r.name}
              </option>
            ))}
          </select>
        </div>
        <div className="form-group">
          <label className="form-group__label" htmlFor="flight-planner">
            Planner
          </label>
          <select
            id="flight-planner"
            className="select"
            value={planner}
            disabled={submitting}
            onChange={(e) => setPlanner(e.target.value)}
          >
            {targetOptions(planner).map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </select>
        </div>
        <div className="form-group">
          <label className="form-group__label" htmlFor="flight-executor">
            Executor
          </label>
          <select
            id="flight-executor"
            className="select"
            value={executor}
            disabled={submitting}
            onChange={(e) => setExecutor(e.target.value)}
          >
            {targetOptions(executor).map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="flight-prompt">
          Prompt
        </label>
        <textarea
          id="flight-prompt"
          className="textarea entity-form__textarea--mono"
          rows={14}
          value={prompt}
          disabled={submitting}
          onChange={(e) => setPrompt(e.target.value)}
        />
      </div>

      {error && (
        <p className="form-group__error" role="alert" data-testid="flight-form-error">
          {error}
        </p>
      )}
      <div className="form-actions">
        <button type="button" className="btn" onClick={onCancel} disabled={submitting}>
          Cancel
        </button>
        <button
          type="submit"
          className="btn btn--primary"
          disabled={missing || submitting}
          data-testid="flight-form-submit"
        >
          {submitting ? 'Saving...' : 'Save'}
        </button>
      </div>
    </form>
  );
}
