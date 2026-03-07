import React from 'react';
import type { ConfigFormAction } from './useConfigForm';
import type { BuiltinQuickLaunchCookbook, Model, QuickLaunchPreset } from '../../lib/types';
import type { Persona } from '../../lib/types.generated';

type QuickLaunchTabProps = {
  quickLaunch: QuickLaunchPreset[];
  builtinQuickLaunch: BuiltinQuickLaunchCookbook[];
  models: Model[];
  personas: Persona[];
  newQuickLaunchName: string;
  newQuickLaunchMode: 'agent' | 'command';
  newQuickLaunchTarget: string;
  newQuickLaunchPrompt: string;
  newQuickLaunchCommand: string;
  newQuickLaunchPersonaId: string;
  selectedCookbookTemplate: BuiltinQuickLaunchCookbook | null;
  dispatch: React.Dispatch<ConfigFormAction>;
  onAddQuickLaunch: () => void;
  onRemoveQuickLaunch: (name: string) => void;
  onOpenQuickLaunchEditModal: (item: QuickLaunchPreset) => void;
};

export default function QuickLaunchTab({
  quickLaunch,
  builtinQuickLaunch,
  models,
  personas,
  newQuickLaunchName,
  newQuickLaunchMode,
  newQuickLaunchTarget,
  newQuickLaunchPrompt,
  newQuickLaunchCommand,
  newQuickLaunchPersonaId,
  selectedCookbookTemplate,
  dispatch,
  onAddQuickLaunch,
  onRemoveQuickLaunch,
  onOpenQuickLaunchEditModal,
}: QuickLaunchTabProps) {
  const isAgentMode = newQuickLaunchMode === 'agent';

  return (
    <div className="wizard-step-content" data-step="3">
      <h2 className="wizard-step-content__title">Quick Launch</h2>
      <p className="wizard-step-content__description">
        Add actions to the + dropdown. Agents run a model with a prompt. Commands run a shell
        command.
      </p>

      <div className="quick-launch-editor">
        {quickLaunch.length === 0 ? (
          <div className="quick-launch-editor__empty">No quick launch items yet.</div>
        ) : (
          <div className="quick-launch-editor__list">
            {quickLaunch.map((item) => (
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
                  dispatch({ type: 'SET_FIELD', field: 'selectedCookbookTemplate', value: null });
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
              value={newQuickLaunchName}
              onChange={(e) =>
                dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchName', value: e.target.value })
              }
            />
            {isAgentMode || selectedCookbookTemplate ? (
              <>
                <select
                  className="input quick-launch-editor__select"
                  value={newQuickLaunchTarget}
                  onChange={(e) => {
                    const value = e.target.value;
                    dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchTarget', value });
                    if (!newQuickLaunchName.trim()) {
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
                    value={newQuickLaunchPersonaId}
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
                value={newQuickLaunchCommand}
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
                value={newQuickLaunchPrompt}
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
                const isAdded = quickLaunch.some((p) => p.name === template.name);
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
  );
}
