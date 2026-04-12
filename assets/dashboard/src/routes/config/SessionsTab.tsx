import React from 'react';
import NudgenikConfig from './NudgenikConfig';
import NotificationsConfig from './NotificationsConfig';
import type { ConfigFormAction, ConfigFormState } from './useConfigForm';
import type {
  BuiltinQuickLaunchCookbook,
  Model,
  QuickLaunchPreset,
  RunTargetResponse,
} from '../../lib/types';
import type { Persona } from '../../lib/types.generated';

type SessionsTabProps = {
  state: ConfigFormState;
  dispatch: React.Dispatch<ConfigFormAction>;
  models: Model[];
  personas: Persona[];
  builtinQuickLaunch: BuiltinQuickLaunchCookbook[];
  onEditQuickLaunch: (item: QuickLaunchPreset) => void;
  onRemoveQuickLaunch: (name: string) => void;
  onAddAgent: () => void;
  onAddQuickLaunchCommand: () => void;
  onAddFromCookbook: (template: BuiltinQuickLaunchCookbook) => void;
  onOpenPastebinEditModal: (index: number, content: string) => void;
  onOpenAddPastebinModal: () => void;
  onAddCommand: () => void;
  onRemoveCommand: (name: string) => void;
  onOpenRunTargetEditModal: (target: RunTargetResponse) => void;
};

export default function SessionsTab({
  state,
  dispatch,
  models,
  personas,
  builtinQuickLaunch,
  onEditQuickLaunch,
  onRemoveQuickLaunch,
  onAddAgent,
  onAddQuickLaunchCommand,
  onAddFromCookbook,
  onOpenPastebinEditModal,
  onOpenAddPastebinModal,
  onAddCommand,
  onRemoveCommand,
  onOpenRunTargetEditModal,
}: SessionsTabProps) {
  return (
    <div className="wizard-step-content" data-step="2">
      <h2 className="wizard-step-content__title">Sessions</h2>
      <p className="wizard-step-content__description">
        Quick launch presets, spawn defaults, command targets, and session behavior.
      </p>

      {/* Quick Launch */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Quick Launch</h3>
        </div>
        <div className="settings-section__body">
          <p className="form-group__hint mb-md">
            Add actions to the + dropdown. Agents run a model with a prompt. Commands run a shell
            command.
          </p>

          <div className="quick-launch-editor">
            {state.quickLaunch.length === 0 ? (
              <div className="quick-launch-editor__empty">No quick launch items yet.</div>
            ) : (
              <div className="quick-launch-editor__list">
                {state.quickLaunch.map((item) => (
                  <div className="quick-launch-editor__item" key={item.name}>
                    <div className="quick-launch-editor__item-main">
                      <span className="quick-launch-editor__item-name">{item.name}</span>
                      <span className="quick-launch-editor__item-detail">
                        {item.command
                          ? item.command
                          : `${item.target}${item.prompt ? ` — ${item.prompt}` : ''}`}
                      </span>
                      {item.persona_id &&
                        (() => {
                          const persona = personas.find((p) => p.id === item.persona_id);
                          return persona ? (
                            <span className="quick-launch-editor__item-persona">
                              {persona.icon} {persona.name}
                            </span>
                          ) : null;
                        })()}
                    </div>
                    <div className="btn-group">
                      <button
                        className="btn btn--sm btn--primary"
                        onClick={() => onEditQuickLaunch(item)}
                      >
                        Edit
                      </button>
                      <button
                        className="btn btn--sm btn--danger"
                        onClick={() => onRemoveQuickLaunch(item.name)}
                      >
                        Remove
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}

            <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.75rem' }}>
              <button className="btn btn--primary" onClick={onAddAgent}>
                Add Agent
              </button>
              <button className="btn btn--primary" onClick={onAddQuickLaunchCommand}>
                Add Command
              </button>
            </div>

            {builtinQuickLaunch.length > 0 && (
              <div className="quick-launch-editor__cookbook">
                <h3 className="quick-launch-editor__section-title">Cookbook</h3>
                <p className="quick-launch-editor__section-description">
                  Pre-configured quick launch recipes. Click to add to your quick launch with your
                  chosen target.
                </p>
                <div className="quick-launch-editor__list">
                  {builtinQuickLaunch.map((template) => {
                    const isAdded = state.quickLaunch.some((p) => p.name === template.name);
                    return (
                      <div
                        className="quick-launch-editor__item quick-launch-editor__item--cookbook"
                        key={`cookbook-${template.name}`}
                      >
                        <div className="quick-launch-editor__item-main">
                          <span className="quick-launch-editor__item-name">{template.name}</span>
                          <span className="quick-launch-editor__item-detail quick-launch-editor__item-detail--prompt">
                            {template.prompt.slice(0, 80)}
                            {template.prompt.length > 80 ? '...' : ''}
                          </span>
                        </div>
                        {isAdded ? (
                          <span className="quick-launch-editor__item-status">Added</span>
                        ) : (
                          <button
                            className="btn btn--sm btn--primary"
                            onClick={() => onAddFromCookbook(template)}
                          >
                            Add
                          </button>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Pastebin */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Pastebin</h3>
        </div>
        <div className="settings-section__body">
          <p className="form-group__hint mb-md">
            Saved text clips you can paste into any active terminal session.
          </p>
          {state.pastebin.length === 0 ? (
            <div className="empty-state-hint">No clips yet.</div>
          ) : (
            <div className="item-list">
              {state.pastebin.map((content, index) => (
                <div className="item-list__item" key={index}>
                  <div className="item-list__item-content">
                    <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: '0.85rem' }}>
                      {content.length > 100 ? content.slice(0, 100) + '...' : content}
                    </pre>
                  </div>
                  <div className="btn-group">
                    <button
                      className="btn btn--sm btn--primary"
                      onClick={() => onOpenPastebinEditModal(index, content)}
                    >
                      Edit
                    </button>
                    <button
                      className="btn btn--sm btn--danger"
                      onClick={() => dispatch({ type: 'REMOVE_PASTEBIN', index })}
                    >
                      Remove
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
          <button className="btn btn--primary" onClick={onOpenAddPastebinModal}>
            Add clip
          </button>
        </div>
      </div>

      {/* Command Targets */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Command Targets</h3>
        </div>
        <div className="settings-section__body">
          <p className="form-group__hint mb-md">
            Shell commands you want to run quickly, like launching a terminal or starting the app.
          </p>
          {state.commandTargets.length === 0 ? (
            <div className="empty-state-hint">
              No command targets configured. These run without prompts.
            </div>
          ) : (
            <div className="item-list item-list--two-col">
              {state.commandTargets.map((cmd) => (
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
                value={state.newCommandName}
                onChange={(e) =>
                  dispatch({ type: 'SET_FIELD', field: 'newCommandName', value: e.target.value })
                }
                onKeyDown={(e) => e.key === 'Enter' && onAddCommand()}
              />
              <input
                type="text"
                className="input"
                placeholder="Command (e.g., go build ./...)"
                value={state.newCommandCommand}
                onChange={(e) =>
                  dispatch({
                    type: 'SET_FIELD',
                    field: 'newCommandCommand',
                    value: e.target.value,
                  })
                }
                onKeyDown={(e) => e.key === 'Enter' && onAddCommand()}
              />
            </div>
            <button type="button" className="btn btn--sm btn--primary" onClick={onAddCommand}>
              Add
            </button>
          </div>
        </div>
      </div>

      {/* NudgeNik */}
      <NudgenikConfig state={state} dispatch={dispatch} models={models} />

      {/* Notifications */}
      <NotificationsConfig state={state} dispatch={dispatch} models={models} />
    </div>
  );
}
