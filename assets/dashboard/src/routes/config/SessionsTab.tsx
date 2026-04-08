import React, { useMemo } from 'react';
import type { ConfigFormAction } from './useConfigForm';
import type { Model, RunnerInfo, RunTargetResponse } from '../../lib/types';
import type { Style } from '../../lib/types.generated';
import ModelCatalog from './ModelCatalog';
import UserModelsEditor from './UserModelsEditor';

type SessionsTabProps = {
  models: Model[];
  runners: Record<string, RunnerInfo>;
  enabledModels: Record<string, string>;
  commStyles: Record<string, string>;
  styles: Style[];
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
  runners,
  enabledModels,
  commStyles,
  styles,
  commandTargets,
  newCommandName,
  newCommandCommand,
  dispatch,
  onAddCommand,
  onRemoveCommand,
  onModelAction,
  onOpenRunTargetEditModal,
}: SessionsTabProps) {
  const availableRunners = useMemo(() => {
    return Object.entries(runners)
      .filter(([, info]) => info.available)
      .map(([name]) => name);
  }, [runners]);

  const enabledModelIds = useMemo(() => {
    return Object.keys(enabledModels).sort();
  }, [enabledModels]);

  // Unique base tool names (runners) from enabled models — used for comm style defaults.
  // The config stores comm_styles keyed by tool name ("claude"), not model ID ("claude-opus-4-6").
  const uniqueTools = useMemo(() => {
    const seen = new Set<string>();
    for (const runner of Object.values(enabledModels)) {
      if (runner) seen.add(runner);
    }
    return Array.from(seen).sort();
  }, [enabledModels]);

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
        runners={runners}
        enabledModels={enabledModels}
        onToggleModel={handleToggleModel}
        onChangeRunner={handleChangeRunner}
        onModelAction={onModelAction}
      />

      <UserModelsEditor availableRunners={availableRunners} />

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

      {/* Communication Styles defaults */}
      {styles.length > 0 && uniqueTools.length > 0 && (
        <>
          <h3>Communication Styles</h3>
          <p className="section-hint">
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
                  value={commStyles[toolName] || ''}
                  onChange={(e) => {
                    const val = e.target.value;
                    const next = { ...commStyles };
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
        </>
      )}
    </div>
  );
}
