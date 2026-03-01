import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigFormAction } from './useConfigForm';
import type { Model } from '../../lib/types';

type CodeReviewTabProps = {
  commitMessageTarget: string;
  prReviewTarget: string;
  externalDiffCommands: { name: string; command: string }[];
  externalDiffCleanupMinutes: number;
  newDiffName: string;
  newDiffCommand: string;
  commitMessageTargetMissing: boolean;
  prReviewTargetMissing: boolean;
  models: Model[];
  dispatch: React.Dispatch<ConfigFormAction>;
  onAddDiffCommand: () => void;
};

export default function CodeReviewTab({
  commitMessageTarget,
  prReviewTarget,
  externalDiffCommands,
  externalDiffCleanupMinutes,
  newDiffName,
  newDiffCommand,
  commitMessageTargetMissing,
  prReviewTargetMissing,
  models,
  dispatch,
  onAddDiffCommand,
}: CodeReviewTabProps) {
  return (
    <div className="wizard-step-content" data-step="4">
      <h2 className="wizard-step-content__title">Code Review</h2>
      <p className="wizard-step-content__description">
        Configure targets and tools used during code review workflows.
      </p>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Commit Message</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={commitMessageTarget}
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
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">PR Review</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={prReviewTarget}
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
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Built-in Options</h3>
        </div>
        <div className="settings-section__body">
          <div className="item-list">
            <div className="item-list__item">
              <span className="item-list__item-name">VS Code</span>
              <span className="item-list__item-detail">Always available in the diff dropdown</span>
            </div>
            <div className="item-list__item">
              <span className="item-list__item-name">Web view</span>
              <span className="item-list__item-detail">Always available in the diff dropdown</span>
            </div>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Custom Diff Tools</h3>
        </div>
        <div className="settings-section__body">
          {externalDiffCommands.length === 0 ? (
            <div className="empty-state-hint">No custom diff tools configured.</div>
          ) : (
            <div className="item-list item-list--two-col">
              {externalDiffCommands.map((cmd) => (
                <div className="item-list__item" key={cmd.name}>
                  <div className="item-list__item-primary item-list__item-row">
                    <span className="item-list__item-name">{cmd.name}</span>
                    <span className="item-list__item-detail item-list__item-detail--wide mono">
                      {cmd.command}
                    </span>
                  </div>
                  <button
                    className="btn btn--sm btn--danger"
                    onClick={() => dispatch({ type: 'REMOVE_DIFF_COMMAND', name: cmd.name })}
                  >
                    Remove
                  </button>
                </div>
              ))}
            </div>
          )}

          <h3>Add Custom Diff Tool</h3>
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Name</label>
              <input
                type="text"
                className="input"
                placeholder="e.g., Kaleidoscope"
                value={newDiffName}
                onChange={(e) =>
                  dispatch({ type: 'SET_FIELD', field: 'newDiffName', value: e.target.value })
                }
              />
            </div>
            <div className="form-group">
              <label className="form-group__label">Command</label>
              <input
                type="text"
                className="input"
                placeholder="e.g., ksdiff"
                value={newDiffCommand}
                onChange={(e) =>
                  dispatch({ type: 'SET_FIELD', field: 'newDiffCommand', value: e.target.value })
                }
              />
            </div>
            <div
              style={{
                display: 'flex',
                alignItems: 'flex-end',
                paddingBottom: 'var(--spacing-sm)',
              }}
            >
              <button
                type="button"
                className="btn btn--primary"
                disabled={!newDiffName.trim() || !newDiffCommand.trim()}
                onClick={onAddDiffCommand}
              >
                Add Diff Tool
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Temp Cleanup</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Cleanup after (minutes)</label>
              <input
                type="number"
                min="1"
                className="input"
                value={externalDiffCleanupMinutes}
                onChange={(e) =>
                  dispatch({
                    type: 'SET_FIELD',
                    field: 'externalDiffCleanupMinutes',
                    value: Math.max(1, Number(e.target.value) || 1),
                  })
                }
              />
              <p className="form-group__hint">
                Temp diff files will be removed after this delay (default: 60 minutes).
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
