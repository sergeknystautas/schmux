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
  selectedCookbookTemplate: BuiltinQuickLaunchCookbook | null;
  onAddQuickLaunch: () => void;
  onRemoveQuickLaunch: (name: string) => void;
  onOpenQuickLaunchEditModal: (item: QuickLaunchPreset) => void;
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
  selectedCookbookTemplate,
  onAddQuickLaunch,
  onRemoveQuickLaunch,
  onOpenQuickLaunchEditModal,
  onOpenPastebinEditModal,
  onOpenAddPastebinModal,
  onAddCommand,
  onRemoveCommand,
  onOpenRunTargetEditModal,
}: SessionsTabProps) {
  const isAgentMode = state.newQuickLaunchMode === 'agent';

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
                        onClick={() => onOpenQuickLaunchEditModal(item)}
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

            <div className="quick-launch-editor__form">
              {selectedCookbookTemplate && (
                <div className="quick-launch-editor__cookbook-selected">
                  <span className="quick-launch-editor__cookbook-label">
                    Adding from Cookbook: <strong>{selectedCookbookTemplate.name}</strong>
                  </span>
                  <button
                    type="button"
                    className="quick-launch-editor__cookbook-clear"
                    onClick={() => {
                      dispatch({
                        type: 'SET_FIELD',
                        field: 'selectedCookbookTemplate',
                        value: null,
                      });
                      dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchName', value: '' });
                      dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchPrompt', value: '' });
                    }}
                  >
                    Clear
                  </button>
                </div>
              )}

              {!selectedCookbookTemplate && (
                <div className="quick-launch-editor__mode-toggle">
                  <label className="quick-launch-editor__mode-option">
                    <input
                      type="radio"
                      name="quickLaunchMode"
                      checked={isAgentMode}
                      onChange={() => {
                        dispatch({
                          type: 'SET_FIELD',
                          field: 'newQuickLaunchMode',
                          value: 'agent',
                        });
                      }}
                    />
                    Agent
                  </label>
                  <label className="quick-launch-editor__mode-option">
                    <input
                      type="radio"
                      name="quickLaunchMode"
                      checked={!isAgentMode}
                      onChange={() => {
                        dispatch({
                          type: 'SET_FIELD',
                          field: 'newQuickLaunchMode',
                          value: 'command',
                        });
                      }}
                    />
                    Command
                  </label>
                </div>
              )}

              <div className="quick-launch-editor__row">
                <input
                  type="text"
                  className="input quick-launch-editor__name"
                  placeholder="Name"
                  value={state.newQuickLaunchName}
                  onChange={(e) =>
                    dispatch({
                      type: 'SET_FIELD',
                      field: 'newQuickLaunchName',
                      value: e.target.value,
                    })
                  }
                />
                {isAgentMode || selectedCookbookTemplate ? (
                  <>
                    <select
                      className="input quick-launch-editor__select"
                      value={state.newQuickLaunchTarget}
                      onChange={(e) => {
                        const value = e.target.value;
                        dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchTarget', value });
                        if (!state.newQuickLaunchName.trim()) {
                          dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchName', value });
                        }
                      }}
                    >
                      <option value="">Select agent...</option>
                      <optgroup label="Agents">
                        {models
                          .filter((model) => model.configured)
                          .map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                      </optgroup>
                    </select>
                    {personas.length > 0 && (
                      <select
                        className="input quick-launch-editor__select"
                        value={state.newQuickLaunchPersonaId}
                        onChange={(e) =>
                          dispatch({
                            type: 'SET_FIELD',
                            field: 'newQuickLaunchPersonaId',
                            value: e.target.value,
                          })
                        }
                      >
                        <option value="">No persona</option>
                        {personas.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.icon} {p.name}
                          </option>
                        ))}
                      </select>
                    )}
                  </>
                ) : (
                  <input
                    type="text"
                    className="input quick-launch-editor__command"
                    placeholder="Shell command (e.g. make build)"
                    value={state.newQuickLaunchCommand}
                    onChange={(e) =>
                      dispatch({
                        type: 'SET_FIELD',
                        field: 'newQuickLaunchCommand',
                        value: e.target.value,
                      })
                    }
                  />
                )}
                <button type="button" className="btn btn--primary" onClick={onAddQuickLaunch}>
                  Add
                </button>
              </div>

              {(isAgentMode || selectedCookbookTemplate) && (
                <div className="quick-launch-editor__prompt">
                  <label className="form-group__label">
                    {selectedCookbookTemplate ? 'Prompt (from Cookbook)' : 'Prompt'}
                  </label>
                  <textarea
                    className="input quick-launch-editor__prompt-input"
                    placeholder={selectedCookbookTemplate ? '' : 'Prompt'}
                    value={state.newQuickLaunchPrompt}
                    onChange={(e) =>
                      dispatch({
                        type: 'SET_FIELD',
                        field: 'newQuickLaunchPrompt',
                        value: e.target.value,
                      })
                    }
                    rows={6}
                  />
                </div>
              )}
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
                    const isSelected = selectedCookbookTemplate?.name === template.name;
                    return (
                      <div
                        className={`quick-launch-editor__item quick-launch-editor__item--cookbook${isSelected ? ' quick-launch-editor__item--selected' : ''}`}
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
                            className="btn"
                            onClick={() => {
                              dispatch({
                                type: 'SET_FIELD',
                                field: 'selectedCookbookTemplate',
                                value: template,
                              });
                              dispatch({
                                type: 'SET_FIELD',
                                field: 'newQuickLaunchMode',
                                value: 'agent',
                              });
                              dispatch({
                                type: 'SET_FIELD',
                                field: 'newQuickLaunchName',
                                value: template.name,
                              });
                              dispatch({
                                type: 'SET_FIELD',
                                field: 'newQuickLaunchPrompt',
                                value: template.prompt,
                              });
                              (
                                document.querySelector(
                                  '.quick-launch-editor__select'
                                ) as HTMLSelectElement | null
                              )?.focus();
                            }}
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
