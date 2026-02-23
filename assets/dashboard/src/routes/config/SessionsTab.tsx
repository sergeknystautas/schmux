import React from 'react';
import type { ConfigFormAction } from './useConfigForm';
import type { Model, RunTargetResponse } from '../../lib/types';

type SessionsTabProps = {
  detectedTargets: RunTargetResponse[];
  models: Model[];
  promptableTargets: RunTargetResponse[];
  commandTargets: RunTargetResponse[];
  newPromptableName: string;
  newPromptableCommand: string;
  newCommandName: string;
  newCommandCommand: string;
  dispatch: React.Dispatch<ConfigFormAction>;
  onAddPromptableTarget: () => void;
  onRemovePromptableTarget: (name: string) => void;
  onAddCommand: () => void;
  onRemoveCommand: (name: string) => void;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
  onOpenRunTargetEditModal: (target: RunTargetResponse) => void;
};

export default function SessionsTab({
  detectedTargets,
  models,
  promptableTargets,
  commandTargets,
  newPromptableName,
  newPromptableCommand,
  newCommandName,
  newCommandCommand,
  dispatch,
  onAddPromptableTarget,
  onRemovePromptableTarget,
  onAddCommand,
  onRemoveCommand,
  onModelAction,
  onOpenRunTargetEditModal,
}: SessionsTabProps) {
  return (
    <div className="wizard-step-content" data-step="2">
      <h2 className="wizard-step-content__title">Run Targets</h2>
      <p className="wizard-step-content__description">
        Configure user-supplied run targets. Detected tools appear automatically in the spawn
        wizard.
      </p>

      <h3>Detected Run Targets (Read-only)</h3>
      <p className="section-hint">
        Official tools we detected on this machine and confirmed working. These are read-only.
      </p>
      {detectedTargets.length === 0 ? (
        <div className="empty-state-hint">
          No detected run targets. Use the detect endpoint or restart the daemon to refresh
          detection.
        </div>
      ) : (
        <div className="item-list item-list--two-col">
          {detectedTargets.map((target) => (
            <div className="item-list__item" key={target.name}>
              <div className="item-list__item-primary item-list__item-row">
                <span className="item-list__item-name">{target.name}</span>
                <span className="item-list__item-detail item-list__item-detail--wide">
                  {target.command}
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      <h3>Models</h3>
      <p className="section-hint">
        Add secrets to enable third-party models for quick launch and spawning.
      </p>
      {models.length === 0 ? (
        <div className="empty-state-hint">
          No models available. Install the base tool to enable models.
        </div>
      ) : (
        <div className="item-list">
          {models.map((model) => (
            <div className="item-list__item" key={model.id}>
              <div className="item-list__item-primary">
                <span className="item-list__item-name">{model.display_name}</span>
                <span className="item-list__item-detail">
                  {model.id} · base: {model.base_tool}
                </span>
                {model.usage_url && (
                  <a
                    className="item-list__item-detail link"
                    href={model.usage_url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {model.usage_url}
                  </a>
                )}
              </div>
              {model.required_secrets && model.required_secrets.length > 0 ? (
                model.configured ? (
                  <div style={{ display: 'flex', gap: 'var(--spacing-sm)' }}>
                    <button
                      className="btn btn--primary"
                      onClick={() => onModelAction(model, 'update')}
                    >
                      Update
                    </button>
                    <button
                      className="btn btn--danger"
                      onClick={() => onModelAction(model, 'remove')}
                    >
                      Remove
                    </button>
                  </div>
                ) : (
                  <button className="btn btn--primary" onClick={() => onModelAction(model, 'add')}>
                    Add Secrets
                  </button>
                )
              ) : (
                <span className="status-badge status-badge--success">No secrets required</span>
              )}
            </div>
          ))}
        </div>
      )}

      <h3>Promptable Targets</h3>
      <p className="section-hint">
        Custom coding agents that accept prompts. We append the prompt to the command.
      </p>
      {promptableTargets.length === 0 ? (
        <div className="empty-state-hint">
          No promptable targets configured. Add one to enable custom promptable commands.
        </div>
      ) : (
        <div className="item-list item-list--two-col">
          {promptableTargets.map((target) => (
            <div className="item-list__item" key={target.name}>
              <div className="item-list__item-primary item-list__item-row">
                <span className="item-list__item-name">{target.name}</span>
                <span className="item-list__item-detail item-list__item-detail--wide">
                  {target.command}
                </span>
              </div>
              {target.source === 'user' ? (
                <div className="btn-group">
                  <button
                    className="btn btn--sm btn--primary"
                    onClick={() => onOpenRunTargetEditModal(target)}
                  >
                    Edit
                  </button>
                  <button
                    className="btn btn--sm btn--danger"
                    onClick={() => onRemovePromptableTarget(target.name)}
                  >
                    Remove
                  </button>
                </div>
              ) : (
                <button
                  className="btn btn--sm btn--danger"
                  onClick={() => onRemovePromptableTarget(target.name)}
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
            value={newPromptableName}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newPromptableName', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddPromptableTarget()}
          />
          <input
            type="text"
            className="input"
            placeholder="Command (prompt is appended as last arg)"
            value={newPromptableCommand}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newPromptableCommand', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddPromptableTarget()}
          />
        </div>
        <button
          type="button"
          className="btn btn--sm btn--primary"
          onClick={onAddPromptableTarget}
          data-testid="add-target"
        >
          Add
        </button>
      </div>

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
