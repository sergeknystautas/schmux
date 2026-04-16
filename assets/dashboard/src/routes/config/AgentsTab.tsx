import React, { useMemo } from 'react';
import TargetSelect from './TargetSelect';
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
  };
  dispatch: React.Dispatch<ConfigFormAction>;
  models: Model[];
  oneshotModels: Model[];
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
  oneshotModels,
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
              models={oneshotModels}
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
              models={models}
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
              models={oneshotModels}
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
              models={oneshotModels}
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
