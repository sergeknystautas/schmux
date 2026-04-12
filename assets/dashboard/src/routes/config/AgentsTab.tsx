import React, { useMemo } from 'react';
import TargetSelect from './TargetSelect';
import ModelCatalog from './ModelCatalog';
import UserModelsEditor from './UserModelsEditor';
import type { ConfigFormAction } from './useConfigForm';
import type { Model, RunnerInfo } from '../../lib/types';
import type { Style } from '../../lib/types.generated';

type AgentsTabProps = {
  state: {
    commitMessageTarget: string;
    prReviewTarget: string;
    branchSuggestTarget: string;
    conflictResolveTarget: string;
    enabledModels: Record<string, string>;
    commStyles: Record<string, string>;
  };
  dispatch: React.Dispatch<ConfigFormAction>;
  models: Model[];
  runners: Record<string, RunnerInfo>;
  styles: Style[];
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
  runners,
  styles,
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

  // Unique base tool names (runners) from enabled models — used for comm style defaults.
  // The config stores comm_styles keyed by tool name ("claude"), not model ID ("claude-opus-4-6").
  const uniqueTools = useMemo(() => {
    const seen = new Set<string>();
    for (const runner of Object.values(state.enabledModels)) {
      if (runner) seen.add(runner);
    }
    return Array.from(seen).sort();
  }, [state.enabledModels]);

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
              models={models}
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
              models={models}
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
              models={models}
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
            models={models}
            runners={runners}
            enabledModels={state.enabledModels}
            onToggleModel={handleToggleModel}
            onChangeRunner={handleChangeRunner}
            onModelAction={onModelAction}
          />
          <UserModelsEditor availableRunners={availableRunners} />
        </div>
      </div>

      {/* 3. Communication Style Defaults */}
      {styles.length > 0 && uniqueTools.length > 0 && (
        <div className="settings-section">
          <div className="settings-section__header">
            <h3 className="settings-section__title">Communication Style Defaults</h3>
          </div>
          <div className="settings-section__body">
            <p className="form-group__hint mb-md">
              Set a default communication style for each agent tool. When spawning, agents will use
              the style assigned here unless overridden.
            </p>
            <div className="item-list">
              {uniqueTools.map((toolName) => (
                <div
                  className="item-list__item"
                  key={toolName}
                  data-testid={`comm-style-${toolName}`}
                >
                  <span className="item-list__item-name" style={{ minWidth: '120px' }}>
                    {toolName}
                  </span>
                  <select
                    className="select"
                    value={state.commStyles[toolName] || ''}
                    onChange={(e) => {
                      const val = e.target.value;
                      const next = { ...state.commStyles };
                      if (val) {
                        next[toolName] = val;
                      } else {
                        delete next[toolName];
                      }
                      dispatch({ type: 'SET_FIELD', field: 'commStyles', value: next });
                    }}
                    style={{ flex: 1, maxWidth: '300px' }}
                  >
                    <option value="">None</option>
                    {styles.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.icon} {s.name}
                      </option>
                    ))}
                  </select>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
