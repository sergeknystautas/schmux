import React, { useMemo } from 'react';
import TargetSelect from './TargetSelect';
import type { TargetOption } from './TargetSelect';
import ModelCatalog from './ModelCatalog';
import UserModelsEditor from './UserModelsEditor';
import type { ConfigFormAction } from './useConfigForm';
import type { Model, RunnerInfo } from '../../lib/types';

type AgentsTabProps = {
  state: {
    commitMessageTarget: string;
    prReviewTarget: string;
    branchSuggestTarget: string;
    conflictResolveTarget: string;
    enabledModels: Record<string, string>;
    anthropicOAuthTokenInput: string;
    anthropicOAuthTokenSet: boolean;
    anthropicOAuthTokenDirty: boolean;
    ollamaEndpointInput: string;
    ollamaEndpointDirty: boolean;
    ollamaReachable: boolean;
    ollamaModels: string[];
    ollamaAutoDetectedEndpoint: string;
  };
  dispatch: React.Dispatch<ConfigFormAction>;
  models: TargetOption[];
  oneshotOptions: TargetOption[];
  modelCatalog: Model[];
  runners: Record<string, RunnerInfo>;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
  onOpenRunTargetEditModal: (target: import('../../lib/types').RunTargetResponse) => void;
  commitMessageTargetMissing: boolean;
  prReviewTargetMissing: boolean;
  branchSuggestTargetMissing: boolean;
  conflictResolveTargetMissing: boolean;
};

export default function AgentsTab({
  state,
  dispatch,
  models,
  oneshotOptions,
  modelCatalog,
  runners,
  onModelAction,
  commitMessageTargetMissing,
  prReviewTargetMissing,
  branchSuggestTargetMissing,
  conflictResolveTargetMissing,
}: AgentsTabProps) {
  const availableRunners = useMemo(() => {
    return Object.entries(runners)
      .filter(([, info]) => info.available)
      .map(([name]) => name);
  }, [runners]);

  const handleToggleModel = (modelId: string, enabled: boolean, defaultRunner: string) => {
    dispatch({ type: 'TOGGLE_MODEL', modelId, enabled, defaultRunner });
  };

  const handleChangeRunner = (modelId: string, runner: string) => {
    dispatch({ type: 'CHANGE_RUNNER', modelId, runner });
  };

  return (
    <div className="wizard-step-content" data-testid="config-tab-content-agents">
      <h2 className="wizard-step-content__title">Agents</h2>
      <p className="wizard-step-content__description">
        Configure which models are available, how they are assigned to tasks, and communication
        style defaults.
      </p>

      {/* One-shot Provider Setup */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">One-Shot Providers</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label" htmlFor="anthropic-oauth-token">
              Anthropic Token
            </label>
            <input
              id="anthropic-oauth-token"
              type="password"
              className="input"
              placeholder="sk-ant-oat-..."
              value={state.anthropicOAuthTokenInput}
              onChange={(e) =>
                dispatch({
                  type: 'SET_FIELD',
                  field: 'anthropicOAuthTokenInput',
                  value: e.target.value,
                })
              }
            />
            {state.anthropicOAuthTokenInput.startsWith('sk-ant-oat') && (
              <p className="form-group__hint">
                Detected: subscription token from <code>claude setup-token</code>.
              </p>
            )}
            <p className="form-group__hint">
              {state.anthropicOAuthTokenDirty && state.anthropicOAuthTokenInput === ''
                ? 'The stored token will be cleared when you save.'
                : state.anthropicOAuthTokenSet
                  ? 'Token is set. Enter a new value to replace it, or clear this field and save to remove it.'
                  : 'No token configured. Enter an Anthropic OAuth token (sk-ant-oat-...) to enable Anthropic API targets.'}
            </p>
          </div>

          <div className="form-group">
            <label className="form-group__label" htmlFor="ollama-endpoint">
              Ollama Endpoint
            </label>
            <input
              id="ollama-endpoint"
              type="url"
              className="input"
              placeholder="http://localhost:11434"
              value={state.ollamaEndpointInput}
              onChange={(e) =>
                dispatch({ type: 'SET_FIELD', field: 'ollamaEndpointInput', value: e.target.value })
              }
            />
            <p className="form-group__hint">
              {state.ollamaEndpointDirty && state.ollamaEndpointInput === ''
                ? 'The saved endpoint will be cleared when you save.'
                : state.ollamaReachable
                  ? state.ollamaEndpointInput
                    ? `Reachable — ${state.ollamaModels.length} model${state.ollamaModels.length === 1 ? '' : 's'} available.`
                    : `Auto-detected at ${state.ollamaAutoDetectedEndpoint || 'http://localhost:11434'} — ${state.ollamaModels.length} model${state.ollamaModels.length === 1 ? '' : 's'} available. Leave blank to keep auto-detect, or enter a URL to pin it.`
                  : state.ollamaEndpointInput
                    ? 'Not reachable at the configured endpoint.'
                    : 'No Ollama endpoint configured. Leave blank to auto-detect http://localhost:11434, or set a URL explicitly.'}
            </p>
          </div>
        </div>
      </div>

      {/* 1. Task Assignments */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Task Assignments</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Commit Message</label>
            <TargetSelect
              value={state.commitMessageTarget}
              onChange={(v) =>
                dispatch({ type: 'SET_FIELD', field: 'commitMessageTarget', value: v })
              }
              options={oneshotOptions}
            />
            <p className="form-group__hint">
              Select a model for generating commit messages from the Git History DAG.
            </p>
            {commitMessageTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>

          <div className="form-group">
            <label className="form-group__label">PR Review</label>
            <TargetSelect
              value={state.prReviewTarget}
              onChange={(v) => dispatch({ type: 'SET_FIELD', field: 'prReviewTarget', value: v })}
              options={models}
            />
            <p className="form-group__hint">
              Select a model for PR review sessions, or leave disabled.
            </p>
            {prReviewTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>

          <div className="form-group">
            <label className="form-group__label">Branch Suggestion</label>
            <TargetSelect
              value={state.branchSuggestTarget}
              onChange={(v) =>
                dispatch({ type: 'SET_FIELD', field: 'branchSuggestTarget', value: v })
              }
              options={oneshotOptions}
            />
            <p className="form-group__hint">
              Select a model for branch name suggestion, or leave disabled.
            </p>
            {branchSuggestTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>

          <div className="form-group">
            <label className="form-group__label">Conflict Resolution</label>
            <TargetSelect
              value={state.conflictResolveTarget}
              onChange={(v) =>
                dispatch({ type: 'SET_FIELD', field: 'conflictResolveTarget', value: v })
              }
              options={oneshotOptions}
            />
            <p className="form-group__hint">
              Select a model for merge conflict resolution. When &quot;sync from main conflict&quot;
              encounters a conflict, this target will be spawned to resolve it.
            </p>
            {conflictResolveTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>
        </div>
      </div>

      {/* 2. Model Catalog */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Model Catalog</h3>
        </div>
        <div className="settings-section__body">
          <p className="form-group__hint mb-md">
            Enable models and choose which tool runs each one. Only enabled models appear in the
            spawn wizard.
          </p>
          <ModelCatalog
            models={modelCatalog}
            runners={runners}
            enabledModels={state.enabledModels}
            onToggleModel={handleToggleModel}
            onChangeRunner={handleChangeRunner}
            onModelAction={onModelAction}
          />
          <UserModelsEditor availableRunners={availableRunners} />
        </div>
      </div>
    </div>
  );
}
