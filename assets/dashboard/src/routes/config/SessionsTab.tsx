import React from 'react';
import type { ConfigFormAction } from './useConfigForm';
import type { Model, RunTargetResponse } from '../../lib/types';
import ModelCatalog from './ModelCatalog';

type SessionsTabProps = {
  models: Model[];
  enabledModels: Record<string, string>;
  commandTargets: RunTargetResponse[];
  newCommandName: string;
  newCommandCommand: string;
  dispatch: React.Dispatch<ConfigFormAction>;
  onAddCommand: () => void;
  onRemoveCommand: (name: string) => void;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
  onOpenRunTargetEditModal: (target: RunTargetResponse) => void;
};

export default function SessionsTab({
  models,
  enabledModels,
  commandTargets,
  newCommandName,
  newCommandCommand,
  dispatch,
  onAddCommand,
  onRemoveCommand,
  onModelAction,
  onOpenRunTargetEditModal,
}: SessionsTabProps) {
  const handleToggleModel = (modelId: string, enabled: boolean, defaultRunner: string) => {
    dispatch({ type: 'TOGGLE_MODEL', modelId, enabled, defaultRunner });
  };

  const handleChangeRunner = (modelId: string, runner: string) => {
    dispatch({ type: 'CHANGE_RUNNER', modelId, runner });
  };

  return (
    <div className="wizard-step-content" data-step="2">
      <h2 className="wizard-step-content__title">Models</h2>
      <p className="wizard-step-content__description">
        Enable models and choose which tool runs each one. Only enabled models appear in the spawn
        wizard.
      </p>

      <ModelCatalog
        models={models}
        enabledModels={enabledModels}
        onToggleModel={handleToggleModel}
        onChangeRunner={handleChangeRunner}
        onModelAction={onModelAction}
      />

      <h3>Command Targets</h3>
      <p className="section-hint">
        Shell commands you want to run quickly, like launching a terminal or starting the app.
      </p>
      {commandTargets.length === 0 ? (
        <div className="empty-state-hint">
          No command targets configured. These run without prompts.
        </div>
      ) : (
        <div className="item-list item-list--two-col">
          {commandTargets.map((cmd) => (
            <div className="item-list__item" key={cmd.name}>
              <div className="item-list__item-primary item-list__item-row">
                <span className="item-list__item-name">{cmd.name}</span>
                <span className="item-list__item-detail item-list__item-detail--wide">
                  {cmd.command}
                </span>
              </div>
              {cmd.source === 'user' ? (
                <div className="btn-group">
                  <button
                    className="btn btn--sm btn--primary"
                    onClick={() => onOpenRunTargetEditModal(cmd)}
                  >
                    Edit
                  </button>
                  <button
                    className="btn btn--sm btn--danger"
                    onClick={() => onRemoveCommand(cmd.name)}
                  >
                    Remove
                  </button>
                </div>
              ) : (
                <button
                  className="btn btn--sm btn--danger"
                  onClick={() => onRemoveCommand(cmd.name)}
                >
                  Remove
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      <div className="add-item-form">
        <div className="add-item-form__inputs">
          <input
            type="text"
            className="input"
            placeholder="Name"
            value={newCommandName}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newCommandName', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddCommand()}
          />
          <input
            type="text"
            className="input"
            placeholder="Command (e.g., go build ./...)"
            value={newCommandCommand}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newCommandCommand', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddCommand()}
          />
        </div>
        <button type="button" className="btn btn--sm btn--primary" onClick={onAddCommand}>
          Add
        </button>
      </div>
    </div>
  );
}
