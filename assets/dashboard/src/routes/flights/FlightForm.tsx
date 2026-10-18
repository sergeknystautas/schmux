import { useEffect, useState } from 'react';
import type { CreateFlightRequest, FlightDetailResponse } from '../../lib/types.generated';
import PromptTextarea from '../../components/PromptTextarea';
import layout from './FlightLayout.module.css';

type flightFormRepo = { url: string; name: string };
export type FlightFormTarget = { id: string; label: string };

export type FlightFormValues = CreateFlightRequest & {
  workflow_state?: FlightDetailResponse['workflow_state'];
};
export type FlightDraft = CreateFlightRequest & { executorSelected: boolean };

type FlightFormProps = {
  repos: flightFormRepo[];
  /** Configured targets that can run a chat session. */
  targets: FlightFormTarget[];
  draft?: FlightDraft;
  onDraftChange?: (draft: FlightDraft) => void;
  submitLabel: string;
  onSubmit: (req: FlightFormValues) => Promise<void>;
  onCancel: () => void;
};

/**
 * Creates or edits a flight. Repository and targets come from the
 * configured repositories and chat-capable targets; the backend validates
 * them again.
 */
export default function FlightForm({
  repos,
  targets,
  draft,
  onDraftChange,
  submitLabel,
  onSubmit,
  onCancel,
}: FlightFormProps) {
  const [repo, setRepo] = useState(draft?.repo ?? repos[0]?.url ?? '');
  const [prompt, setPrompt] = useState(draft?.prompt ?? '');
  const [shortName, setShortName] = useState(draft?.short_name ?? '');
  const [planner, setPlanner] = useState(draft?.planner_model ?? '');
  const [executor, setExecutor] = useState(draft?.executor_model ?? '');
  const [executorSelected, setExecutorSelected] = useState(draft?.executorSelected ?? false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!onDraftChange) return;
    onDraftChange({
      repo,
      prompt,
      short_name: shortName,
      planner_model: planner,
      executor_model: executor,
      executorSelected,
    });
  }, [onDraftChange, repo, prompt, shortName, planner, executor, executorSelected]);

  if (repos.length === 0) {
    return (
      <p className="empty-state-hint" data-testid="flight-form-no-repos">
        No repositories are configured. Add one in Config before creating a flight.
      </p>
    );
  }
  if (targets.length === 0) {
    return (
      <p className="empty-state-hint" data-testid="flight-form-no-targets">
        Flights need a chat-capable target. Enable chat sessions in Config and configure a target
        whose runner supports chat.
      </p>
    );
  }

  const missing =
    !repo ||
    !prompt.trim() ||
    !planner ||
    !executor ||
    !repos.some((r) => r.url === repo) ||
    !targets.some((t) => t.id === planner) ||
    !targets.some((t) => t.id === executor);

  const submit = async () => {
    if (missing || submitting) return;
    setSubmitting(true);
    setError('');
    try {
      await onSubmit({
        repo,
        // The prompt is reproduced verbatim to the planner and executor.
        prompt,
        short_name: shortName.trim(),
        planner_model: planner,
        executor_model: executor,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      onKeyDown={(e) => {
        if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
          e.preventDefault();
          void submit();
        }
      }}
      data-testid="flight-form"
    >
      <label htmlFor="flight-prompt" className="sr-only">
        Prompt
      </label>
      <div className="card card--prompt mb-md">
        <PromptTextarea
          id="flight-prompt"
          value={prompt}
          onChange={setPrompt}
          placeholder="Describe the task you want the agents to work on... (⌘↩ to submit)"
          commands={[]}
          onSelectCommand={() => {}}
          disabled={submitting}
          data-testid="flight-prompt"
        />
      </div>
      <div className="spawn-selectors mb-md">
        <div className="spawn-selector">
          <label htmlFor="flight-planner" className="spawn-selector__label">
            Planner
          </label>
          <select
            id="flight-planner"
            className="select"
            value={targets.some((t) => t.id === planner) ? planner : ''}
            disabled={submitting}
            onChange={(e) => {
              setPlanner(e.target.value);
              if (!executorSelected) setExecutor(e.target.value);
            }}
          >
            <option value="">Select model</option>
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </select>
        </div>
        <div className="spawn-selector">
          <label htmlFor="flight-executor" className="spawn-selector__label">
            Executor
          </label>
          <select
            id="flight-executor"
            className="select"
            value={targets.some((t) => t.id === executor) ? executor : ''}
            disabled={submitting}
            onChange={(e) => {
              setExecutor(e.target.value);
              setExecutorSelected(true);
            }}
          >
            <option value="">Select model</option>
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </select>
        </div>
        <div className="spawn-selector">
          <label htmlFor="flight-repo" className="spawn-selector__label">
            Repository
          </label>
          <select
            id="flight-repo"
            className="select"
            value={repos.some((r) => r.url === repo) ? repo : ''}
            disabled={submitting}
            onChange={(e) => setRepo(e.target.value)}
          >
            <option value="">Select repository</option>
            {repos.map((r) => (
              <option key={r.url} value={r.url}>
                {r.name}
              </option>
            ))}
          </select>
        </div>
        <div className="spawn-selector">
          <label htmlFor="flight-short-name" className="spawn-selector__label">
            Short name
          </label>
          <input
            id="flight-short-name"
            className="input"
            value={shortName}
            maxLength={80}
            placeholder="Optional"
            disabled={submitting}
            onChange={(e) => setShortName(e.target.value)}
          />
        </div>
      </div>
      {error && (
        <p className="form-group__error" role="alert" data-testid="flight-form-error">
          {error}
        </p>
      )}
      <div className={layout.actions}>
        <button
          type="button"
          className="btn btn--secondary"
          onClick={onCancel}
          disabled={submitting}
        >
          Cancel
        </button>
        <button
          type="submit"
          className="btn btn--primary"
          disabled={missing || submitting}
          data-testid="flight-form-submit"
        >
          {submitting ? 'Saving...' : submitLabel}
        </button>
      </div>
    </form>
  );
}
