import React from 'react';
import type { ConfigFormAction } from './useConfigForm';
import type {
  BuiltinQuickLaunchCookbook,
  Model,
  QuickLaunchPreset,
  RunTargetResponse,
} from '../../lib/types';

type QuickLaunchTabProps = {
  quickLaunch: QuickLaunchPreset[];
  builtinQuickLaunch: BuiltinQuickLaunchCookbook[];
  detectedTargets: RunTargetResponse[];
  models: Model[];
  commandTargets: RunTargetResponse[];
  newQuickLaunchName: string;
  newQuickLaunchTarget: string;
  newQuickLaunchPrompt: string;
  selectedCookbookTemplate: BuiltinQuickLaunchCookbook | null;
  modelTargetNames: Set<string>;
  commandTargetNames: Set<string>;
  dispatch: React.Dispatch<ConfigFormAction>;
  onAddQuickLaunch: () => void;
  onRemoveQuickLaunch: (name: string) => void;
  onOpenQuickLaunchEditModal: (item: QuickLaunchPreset) => void;
};

export default function QuickLaunchTab({
  quickLaunch,
  builtinQuickLaunch,
  detectedTargets,
  models,
  commandTargets,
  newQuickLaunchName,
  newQuickLaunchTarget,
  newQuickLaunchPrompt,
  selectedCookbookTemplate,
  modelTargetNames,
  commandTargetNames,
  dispatch,
  onAddQuickLaunch,
  onRemoveQuickLaunch,
  onOpenQuickLaunchEditModal,
}: QuickLaunchTabProps) {
  return (
    <div className="wizard-step-content" data-step="3">
      <h2 className="wizard-step-content__title">Quick Launch</h2>
      <p className="wizard-step-content__description">
        Quick launch runs a target with a prompt. Models and detected tools require a prompt.
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
                    {commandTargetNames.has(item.target || '')
                      ? (() => {
                          const cmd = commandTargets.find((t) => t.name === item.target);
                          return cmd ? cmd.command : item.target;
                        })()
                      : `${item.target}${item.prompt ? ` — ${item.prompt}` : ''}`}
                  </span>
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
            <select
              className="input quick-launch-editor__select"
              value={newQuickLaunchTarget}
              onChange={(e) => {
                const value = e.target.value;
                dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchTarget', value });
                if (!newQuickLaunchName.trim()) {
                  dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchName', value });
                }
                if (commandTargetNames.has(value)) {
                  dispatch({ type: 'SET_FIELD', field: 'newQuickLaunchPrompt', value: '' });
                }
              }}
            >
              <option value="">Select target...</option>
              {selectedCookbookTemplate ? (
                <optgroup label="Models &amp; Tools">
                  {[
                    ...detectedTargets.map((target) => ({
                      value: target.name,
                      label: target.name,
                    })),
                    ...models
                      .filter((model) => model.configured)
                      .map((model) => ({
                        value: model.id,
                        label: model.display_name,
                      })),
                  ].map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </optgroup>
              ) : (
                <>
                  <optgroup label="Models &amp; Tools">
                    {[
                      ...detectedTargets.map((target) => ({
                        value: target.name,
                        label: target.name,
                      })),
                      ...models
                        .filter((model) => model.configured)
                        .map((model) => ({
                          value: model.id,
                          label: model.display_name,
                        })),
                    ].map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </optgroup>
                  <optgroup label="Command Targets">
                    {commandTargets.map((target) => (
                      <option key={target.name} value={target.name}>
                        {target.name}
                      </option>
                    ))}
                  </optgroup>
                </>
              )}
            </select>
            <button type="button" className="btn btn--primary" onClick={onAddQuickLaunch}>
              Add
            </button>
          </div>

          {(selectedCookbookTemplate || modelTargetNames.has(newQuickLaunchTarget)) && (
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
